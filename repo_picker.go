package main

// The agent launchers (launch-claude, launch-codex, launch-opencode) start in a
// repo picked from Forgejo, so every PC offers the same repos, not only what it
// has cloned (FZTA-17). The list is Nelson's starred repos, then the repos this
// PC's latest Claude and Codex sessions ran in, then the rest by latest change.
// A pick opens this PC's checkout, else clones into D:\repos. Recent comes from
// the session records, not Forgejo, so with Forgejo down the picker still lists
// this PC's checkouts and says why the rest are missing.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
)

const (
	forgejoTimeout = 8 * time.Second
	recentWindow   = 30 * 24 * time.Hour
	recentLimit    = 8
	// codexRecentFiles bounds how many of the newest Codex sessions are read.
	codexRecentFiles = 300
)

// Seams for tests.
var (
	forgejoAPI   = forgejoBase + "api/v1"
	forgejoToken = gitCredentialToken
)

type forgejoRepo struct {
	FullName string    `json:"full_name"`
	Name     string    `json:"name"`
	About    string    `json:"description"`
	Mirror   bool      `json:"mirror"`
	Archived bool      `json:"archived"`
	Updated  time.Time `json:"updated_at"`
}

// repoChoice is one repo row: a Forgejo repo, a checkout on this PC, or both.
type repoChoice struct {
	name     string // the repo's name, the key everything is matched on
	fullName string // owner/name on Forgejo, "" for a checkout Forgejo didn't list
	about    string
	dir      string // this PC's checkout, "" when it has none
	starred  bool
	recent   time.Time
	updated  time.Time
}

// gitCredentialToken reads the token git holds for Forgejo, the one the pr tools
// and git over HTTPS use, without ever prompting or opening a sign-in window.
func gitCredentialToken(ctx context.Context) (string, error) {
	host := strings.TrimSuffix(strings.TrimPrefix(forgejoBase, "https://"), "/")
	cmd := exec.CommandContext(ctx, "git", "-c", "credential.interactive=false", "credential", "fill")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=" + host + "\n\n")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git has no sign-in for %s", host)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "password="); ok && v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("git has no sign-in for %s", host)
}

// forgejoList reads every page of a Forgejo list endpoint.
func forgejoList(ctx context.Context, token, endpoint string, wrapped bool) ([]forgejoRepo, error) {
	var all []forgejoRepo
	for page := 1; ; page++ {
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s%s%slimit=50&page=%d", forgejoAPI, endpoint, sep, page), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "token "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, unreachable{err}
		}
		var repos []forgejoRepo
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("Forgejo answered %s", resp.Status)
		}
		if wrapped {
			var body struct {
				Data []forgejoRepo `json:"data"`
			}
			err = json.NewDecoder(resp.Body).Decode(&body)
			repos = body.Data
		} else {
			err = json.NewDecoder(resp.Body).Decode(&repos)
		}
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("Forgejo's answer can't be read: %w", err)
		}
		all = append(all, repos...)
		if len(repos) < 50 {
			return all, nil
		}
	}
}

// unreachable is a request that never got an answer. Its message fits the
// picker's title; the full error goes to the failure log.
type unreachable struct{ err error }

func (u unreachable) Error() string { return "Forgejo can't be reached" }
func (u unreachable) Unwrap() error { return u.err }

// forgejoRepos lists the repos Nelson can reach and the ones he starred. Mirrors
// (outside actions kept for CI) and archived repos are left out.
func forgejoRepos(ctx context.Context) (repos []forgejoRepo, starred map[string]bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, forgejoTimeout)
	defer cancel()
	token, err := forgejoToken(ctx)
	if err != nil {
		return nil, nil, err
	}
	var all, stars []forgejoRepo
	var starErr error
	var wg sync.WaitGroup
	wg.Go(func() { stars, starErr = forgejoList(ctx, token, "/user/starred", false) })
	all, err = forgejoList(ctx, token, "/repos/search?sort=updated&order=desc", true)
	wg.Wait()
	if err == nil {
		err = starErr
	}
	if err != nil {
		if ctx.Err() != nil {
			err = fmt.Errorf("Forgejo didn't answer within %s", forgejoTimeout)
		}
		return nil, nil, err
	}
	starred = map[string]bool{}
	for _, r := range stars {
		starred[strings.ToLower(r.Name)] = true
	}
	for _, r := range all {
		if !r.Mirror && !r.Archived {
			repos = append(repos, r)
		}
	}
	return repos, starred, nil
}

var remoteURL = regexp.MustCompile(`(?m)^\[remote "origin"\][^\[]*?^\s*url\s*=\s*(\S+)`)

// remoteName is the repo name in a git remote URL: https, ssh or scp form.
func remoteName(u string) string {
	u = strings.TrimSuffix(strings.TrimRight(u, "/"), ".git")
	if i := strings.LastIndexAny(u, "/:"); i >= 0 {
		u = u[i+1:]
	}
	return u
}

// checkoutName names the repo checked out in dir, from its origin remote, else
// its folder; ok is false for a folder that isn't a checkout.
func checkoutName(dir string) (string, bool) {
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		if b, err := os.ReadFile(filepath.Join(gitDir, "config")); err == nil {
			if m := remoteURL.FindSubmatch(b); m != nil {
				if name := remoteName(string(m[1])); name != "" {
					return name, true
				}
			}
		}
	}
	return filepath.Base(dir), true
}

// localCheckouts maps each repo name, lower case, to this PC's checkout of it:
// the folders in root, then the checkouts home-sync keeps elsewhere (extra),
// which fill only names root lacks.
func localCheckouts(root string, extra []string) map[string]string {
	found := map[string]string{}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if name, ok := checkoutName(dir); ok {
			if _, dup := found[strings.ToLower(name)]; !dup {
				found[strings.ToLower(name)] = dir
			}
		}
	}
	for _, dir := range extra {
		if name, ok := checkoutName(dir); ok {
			if _, dup := found[strings.ToLower(name)]; !dup {
				found[strings.ToLower(name)] = dir
			}
		}
	}
	return found
}

// managedCheckouts are the checkouts home-sync keeps outside D:\repos on this
// PC: shell-config, and the numbered profile its PowerShell profile loads from.
func managedCheckouts(stateFile, computer string) []string {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil
	}
	var state struct {
		ShellConfig string `json:"shellConfig"`
		Machines    map[string]struct {
			Computer string            `json:"computer"`
			Profiles map[string]string `json:"profiles"`
		} `json:"machines"`
	}
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	var dirs []string
	if state.ShellConfig != "" {
		dirs = append(dirs, state.ShellConfig)
	}
	for _, m := range state.Machines {
		if !strings.EqualFold(m.Computer, computer) {
			continue
		}
		for _, file := range m.Profiles {
			for dir := filepath.Dir(file); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
				if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
					dirs = append(dirs, dir)
					break
				}
			}
		}
	}
	return dirs
}

// claudeProjectKey is the folder name Claude Code files a working folder's
// sessions under: every character but a letter or digit becomes '-'.
func claudeProjectKey(dir string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, dir)
}

// claudeRecent gives, for each checkout, when a Claude session last ran in it
// or in one of its worktrees, from ~/.claude/projects.
func claudeRecent(projects string, checkouts map[string]string, since time.Time) map[string]time.Time {
	keys := map[string]string{}
	for name, dir := range checkouts {
		keys[claudeProjectKey(dir)] = name
	}
	worktree := claudeProjectKey(string(filepath.Separator) + ".claude" + string(filepath.Separator) + "worktrees" + string(filepath.Separator))
	recent := map[string]time.Time{}
	entries, _ := os.ReadDir(projects)
	for _, e := range entries {
		key := e.Name()
		if i := strings.Index(key, worktree); i > 0 {
			key = key[:i]
		}
		name, ok := keys[key]
		if !ok || !e.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(projects, e.Name(), "*.jsonl"))
		for _, f := range files {
			if info, err := os.Stat(f); err == nil && info.ModTime().After(since) && info.ModTime().After(recent[name]) {
				recent[name] = info.ModTime()
			}
		}
	}
	return recent
}

// codexRecent gives, for each checkout, when an interactive Codex session last
// started in it or one of its worktrees, from the newest files in
// ~/.codex/sessions/<year>/<month>/<day>; `codex exec` runs are left out.
func codexRecent(sessions string, checkouts map[string]string, since time.Time) map[string]time.Time {
	files, _ := filepath.Glob(filepath.Join(sessions, "*", "*", "*", "*.jsonl"))
	type file struct {
		path string
		mod  time.Time
	}
	var newest []file
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.ModTime().After(since) {
			newest = append(newest, file{f, info.ModTime()})
		}
	}
	sort.Slice(newest, func(i, j int) bool { return newest[i].mod.After(newest[j].mod) })
	if len(newest) > codexRecentFiles {
		newest = newest[:codexRecentFiles]
	}
	recent := map[string]time.Time{}
	for _, f := range newest {
		cwd, source := codexSessionStart(f.path)
		if cwd == "" || source == "exec" {
			continue
		}
		for name, dir := range checkouts {
			if inCheckout(cwd, dir) && f.mod.After(recent[name]) {
				recent[name] = f.mod
			}
		}
	}
	return recent
}

// codexSessionStart reads a Codex session's first line, its session_meta.
func codexSessionStart(file string) (cwd, source string) {
	fh, err := os.Open(file)
	if err != nil {
		return "", ""
	}
	defer fh.Close()
	r := bufio.NewReaderSize(fh, 64<<10)
	line, _ := r.ReadString('\n')
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			Cwd    string `json:"cwd"`
			Source any    `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &meta) != nil || meta.Type != "session_meta" {
		return "", ""
	}
	source, _ = meta.Payload.Source.(string)
	return meta.Payload.Cwd, source
}

// inCheckout says whether folder is the checkout dir or one of its worktrees.
func inCheckout(folder, dir string) bool {
	folder, dir = filepath.Clean(folder), filepath.Clean(dir)
	if strings.EqualFold(folder, dir) {
		return true
	}
	prefix := filepath.Join(dir, ".claude", "worktrees") + string(filepath.Separator)
	return len(folder) > len(prefix) && strings.EqualFold(folder[:len(prefix)], prefix)
}

// repoChoices merges what Forgejo lists with this PC's checkouts and sessions,
// in picker order: starred by name, then recent, newest first, then the rest
// by latest change. With Forgejo's list (listed), only its repos appear; without
// it, this PC's checkouts do.
func repoChoices(repos []forgejoRepo, starred map[string]bool, listed bool, checkouts map[string]string, recent map[string]time.Time) []repoChoice {
	var out []repoChoice
	if listed {
		for _, r := range repos {
			key := strings.ToLower(r.Name)
			out = append(out, repoChoice{name: r.Name, fullName: r.FullName, about: r.About,
				dir: checkouts[key], starred: starred[key], recent: recent[key], updated: r.Updated})
		}
	} else {
		for key, dir := range checkouts {
			out = append(out, repoChoice{name: filepath.Base(dir), dir: dir, recent: recent[key]})
		}
	}
	rank := func(c repoChoice) int {
		switch {
		case c.starred:
			return 0
		case !c.recent.IsZero():
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		switch rank(a) {
		case 0:
			return strings.ToLower(a.name) < strings.ToLower(b.name)
		case 1:
			return a.recent.After(b.recent)
		}
		if !a.updated.Equal(b.updated) {
			return a.updated.After(b.updated)
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
	return out
}

func (c repoChoice) describe(now time.Time) string {
	var parts []string
	if c.starred {
		parts = append(parts, "★ starred")
	}
	if !c.recent.IsZero() {
		parts = append(parts, "last session "+ago(c.recent, now))
	}
	if c.dir == "" {
		parts = append(parts, "not on this PC: clones into "+reposRoot)
	}
	if c.fullName != "" {
		parts = append(parts, path.Dir(c.fullName))
	}
	if c.about != "" {
		parts = append(parts, oneLine(c.about, 80))
	}
	return strings.Join(parts, " · ")
}

const (
	pickRepos  = "repos"
	pickBrowse = "browse"
)

// pickItems builds the picker's rows: the header, the two fixed rows, then
// one row per repo, its action the repo's index.
func pickItems(choices []repoChoice, now time.Time) []core.Item {
	row := func(name, about, action string) core.Item {
		return core.Item{Fields: []string{name, about}, ParentIdx: -1, PropertyOf: -1,
			Action: &core.ItemAction{Type: "command", Target: action}}
	}
	items := []core.Item{
		{Fields: []string{"Repo", "★ starred on Forgejo · last session here · owner · about"}, Depth: -1},
		row(reposRoot, "start in the repos folder itself, no repo", pickRepos),
		row("browse folders…", "pick any folder", pickBrowse),
	}
	for i, c := range choices {
		items = append(items, row(c.name, c.describe(now), fmt.Sprint(i)))
	}
	return items
}

// chooseRepoFolder shows the picker and gives the folder to start agent in: a
// repo's checkout (cloned first when this PC has none), D:\repos, or a folder
// browsed to from browseStart. "" when the picker was closed.
func chooseRepoFolder(ctx context.Context, out io.Writer, agent, browseStart string) (string, error) {
	home, _ := os.UserHomeDir()
	computer, _ := os.Hostname()
	checkouts := localCheckouts(reposRoot, managedCheckouts(homeState, computer))

	var repos []forgejoRepo
	var starred map[string]bool
	var listErr error
	var wg sync.WaitGroup
	wg.Go(func() { repos, starred, listErr = forgejoRepos(ctx) })
	since := time.Now().Add(-recentWindow)
	recent := claudeRecent(filepath.Join(home, ".claude", "projects"), checkouts, since)
	for name, t := range codexRecent(filepath.Join(home, ".codex", "sessions"), checkouts, since) {
		if t.After(recent[name]) {
			recent[name] = t
		}
	}
	wg.Wait()
	recent = newestOnly(recent, recentLimit)

	choices := repoChoices(repos, starred, listErr == nil, checkouts, recent)
	title := fmt.Sprintf("Start %s in (%d repos)", agent, len(choices))
	if listErr != nil {
		title = fmt.Sprintf("Start %s in: %s, so only this PC's repos are listed", agent, listErr)
		detail := listErr.Error()
		if u, ok := listErr.(unreachable); ok {
			detail += ": " + u.err.Error()
		}
		logFailure("", "repo picker for "+agent, "", detail)
	}
	pick, err := tui.Run(pickItems(choices, time.Now()), tui.Config{
		Layout: "reverse", Border: true, HeaderLines: 1, Title: title,
	})
	if err != nil {
		return "", err
	}
	if pick.Kind != "select" || pick.Item.Action == nil {
		return "", nil
	}
	switch target := pick.Item.Action.Target; target {
	case pickRepos:
		return reposRoot, nil
	case pickBrowse:
		return pickFolder(ctx, "--title", "Start "+agent+" in", "--start-dir", browseStart)
	default:
		var i int
		if _, err := fmt.Sscan(target, &i); err != nil || i < 0 || i >= len(choices) {
			return "", errors.New("the picker returned a row it wasn't given")
		}
		c := choices[i]
		if c.dir != "" {
			return c.dir, nil
		}
		return repoCheckout(ctx, out, reposRoot, c.fullName)
	}
}

// newestOnly keeps the limit most recent entries.
func newestOnly(recent map[string]time.Time, limit int) map[string]time.Time {
	names := make([]string, 0, len(recent))
	for n := range recent {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return recent[names[i]].After(recent[names[j]]) })
	kept := map[string]time.Time{}
	for _, n := range names[:min(limit, len(names))] {
		kept[n] = recent[n]
	}
	return kept
}

// launchFolder is where a launcher starts agent: given a folder (args), the
// folder browser opens there, as before; otherwise the repo picker, whose
// "browse folders" row opens the browser at browseStart.
func launchFolder(ctx context.Context, out io.Writer, agent, browseStart string, args []string) (string, error) {
	if len(args) == 1 {
		return pickFolder(ctx, "--title", "Start "+agent+" in", "--start-dir", args[0])
	}
	return chooseRepoFolder(ctx, out, agent, browseStart)
}
