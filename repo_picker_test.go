package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteNameReadsEveryURLForm(t *testing.T) {
	for url, want := range map[string]string{
		"https://forgejo.romaine.life/romaine-life/fzt-automate.git": "fzt-automate",
		"https://github.com/romaine-life/tank-operator":              "tank-operator",
		"git@github.com:nelsong6/at-tmux.git":                        "at-tmux",
		"ssh://git@forgejo.romaine.life:2222/nelsong6/torrent.git":   "torrent",
	} {
		if got := remoteName(url); got != want {
			t.Errorf("remoteName(%q) = %q, want %q", url, got, want)
		}
	}
}

// checkout makes a folder with a .git whose origin is remote ("" for none).
func checkout(t *testing.T, dir, remote string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[core]\n\tbare = false\n"
	if remote != "" {
		config += "[remote \"upstream\"]\n\turl = https://example.com/other/wrong.git\n" +
			"[remote \"origin\"]\n\turl = " + remote + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLocalCheckoutsMatchByRemoteAndKeepReposFirst(t *testing.T) {
	root := t.TempDir()
	renamed := checkout(t, filepath.Join(root, "old-name"), "https://forgejo.romaine.life/nelsong6/New-Name.git")
	plain := checkout(t, filepath.Join(root, "plain"), "")
	inRepos := checkout(t, filepath.Join(root, "shell-config"), "https://forgejo.romaine.life/romaine-life/shell-config.git")
	if err := os.MkdirAll(filepath.Join(root, "not-a-repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	managed := checkout(t, filepath.Join(other, "shell-config"), "https://forgejo.romaine.life/romaine-life/shell-config.git")
	profile := checkout(t, filepath.Join(other, "profile-5"), "https://forgejo.romaine.life/romaine-life/shell-config-profile-5.git")

	got := localCheckouts(root, []string{managed, profile})
	want := map[string]string{"new-name": renamed, "plain": plain, "shell-config": inRepos, "shell-config-profile-5": profile}
	if len(got) != len(want) {
		t.Fatalf("checkouts = %v", got)
	}
	for name, dir := range want {
		if got[name] != dir {
			t.Errorf("%s = %q, want %q", name, got[name], dir)
		}
	}
}

func TestManagedCheckoutsAreThisPCsShellConfigAndProfile(t *testing.T) {
	dir := t.TempDir()
	profile := checkout(t, filepath.Join(dir, "profiles", "shell-config-profile-5"), "")
	state := map[string]any{
		"shellConfig": `D:\shell-config`,
		"machines": map[string]any{
			"nelsonpc":   map[string]any{"computer": "NELSONPC", "profiles": map[string]string{"core": filepath.Join(profile, "pwsh", "profile.ps1")}},
			"upstairspc": map[string]any{"computer": "UPSTAIRSPC", "profiles": map[string]string{"core": filepath.Join(dir, "elsewhere", "pwsh", "profile.ps1")}},
		},
	}
	b, _ := json.Marshal(state)
	file := filepath.Join(dir, "state.json")
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := managedCheckouts(file, "nelsonpc"); strings.Join(got, "|") != `D:\shell-config|`+profile {
		t.Fatalf("managed = %v", got)
	}
}

func touch(t *testing.T, file string, at time.Time, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeRecentCountsWorktreesButNotLookalikeNames(t *testing.T) {
	projects := t.TempDir()
	now := time.Now()
	checkouts := map[string]string{"at": `D:\repos\at`, "fzt": `D:\repos\fzt`}
	touch(t, filepath.Join(projects, "D--repos-at", "a.jsonl"), now.Add(-3*time.Hour), "")
	touch(t, filepath.Join(projects, "D--repos-at--claude-worktrees-AT-1-fix", "b.jsonl"), now.Add(-time.Hour), "")
	touch(t, filepath.Join(projects, "D--repos-at-claude", "c.jsonl"), now, "")                 // at-claude, not at
	touch(t, filepath.Join(projects, "D--repos-fzt", "d.jsonl"), now.Add(-60*24*time.Hour), "") // too old

	got := claudeRecent(projects, checkouts, now.Add(-recentWindow))
	if len(got) != 1 || got["at"].Sub(now.Add(-time.Hour)).Abs() > time.Second {
		t.Fatalf("recent = %v", got)
	}
}

func TestCodexRecentLeavesOutExecRuns(t *testing.T) {
	sessions := t.TempDir()
	now := time.Now()
	meta := func(cwd, source string) string {
		b, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"cwd": cwd, "source": source}})
		return string(b) + "\n{}\n"
	}
	day := filepath.Join(sessions, "2026", "11", "11")
	repos := filepath.Join("D:", "repos")
	touch(t, filepath.Join(day, "rollout-1.jsonl"), now.Add(-2*time.Hour), meta(filepath.Join(repos, "fzt", ".claude", "worktrees", "x"), "cli"))
	touch(t, filepath.Join(day, "rollout-2.jsonl"), now, meta(filepath.Join(repos, "at"), "exec"))
	touch(t, filepath.Join(day, "rollout-3.jsonl"), now, meta(repos, "cli"))

	got := codexRecent(sessions, map[string]string{"at": filepath.Join(repos, "at"), "fzt": filepath.Join(repos, "fzt")}, now.Add(-recentWindow))
	if len(got) != 1 || got["fzt"].IsZero() {
		t.Fatalf("recent = %v", got)
	}
}

func TestForgejoReposReadsEveryPageAndLeavesOutMirrorsAndArchived(t *testing.T) {
	var all []forgejoRepo
	for i := range 60 {
		all = append(all, forgejoRepo{FullName: fmt.Sprintf("nelsong6/r%d", i), Name: fmt.Sprintf("r%d", i)})
	}
	all = append(all, forgejoRepo{FullName: "actions/checkout", Name: "checkout", Mirror: true},
		forgejoRepo{FullName: "nelsong6/old", Name: "old", Archived: true})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var page int
		fmt.Sscan(r.URL.Query().Get("page"), &page)
		switch r.URL.Path {
		case "/repos/search":
			lo, hi := min((page-1)*50, len(all)), min(page*50, len(all))
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": all[lo:hi]})
		case "/user/starred":
			stars := []forgejoRepo{}
			if page == 1 {
				stars = append(stars, forgejoRepo{FullName: "nelsong6/R7", Name: "R7"})
			}
			json.NewEncoder(w).Encode(stars)
		}
	}))
	defer srv.Close()
	defer func(api string, token func(context.Context) (string, error)) {
		forgejoAPI, forgejoToken = api, token
	}(forgejoAPI, forgejoToken)
	forgejoAPI = srv.URL
	forgejoToken = func(context.Context) (string, error) { return "secret", nil }

	repos, starred, err := forgejoRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 60 || !starred["r7"] || len(starred) != 1 {
		t.Fatalf("repos = %d, starred = %v", len(repos), starred)
	}

	forgejoToken = func(context.Context) (string, error) { return "wrong", nil }
	if _, _, err := forgejoRepos(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want Forgejo's refusal", err)
	}
}

func TestRepoChoicesOrderStarredRecentThenLatestChange(t *testing.T) {
	now := time.Now()
	repos := []forgejoRepo{
		{FullName: "o/zeta", Name: "zeta", Updated: now},
		{FullName: "o/Beta", Name: "Beta", Updated: now.Add(-time.Hour)},
		{FullName: "o/alpha", Name: "alpha", Updated: now.Add(-2 * time.Hour)},
		{FullName: "o/older", Name: "older", Updated: now.Add(-3 * time.Hour)},
		{FullName: "o/used", Name: "used", Updated: now.Add(-4 * time.Hour)},
		{FullName: "o/used2", Name: "used2", Updated: now.Add(-5 * time.Hour)},
	}
	starred := map[string]bool{"zeta": true, "beta": true}
	checkouts := map[string]string{"used": filepath.Join("repos", "used"), "used2": filepath.Join("repos", "used2"), "local-only": filepath.Join("repos", "local-only")}
	recent := map[string]time.Time{"used": now.Add(-time.Hour), "used2": now.Add(-time.Minute)}

	var names []string
	for _, c := range repoChoices(repos, starred, true, checkouts, recent) {
		names = append(names, c.name)
	}
	if got := strings.Join(names, " "); got != "Beta zeta used2 used alpha older" {
		t.Fatalf("order = %s", got)
	}

	names = nil
	for _, c := range repoChoices(nil, nil, false, checkouts, recent) {
		names = append(names, c.name)
	}
	if got := strings.Join(names, " "); got != "used2 used local-only" {
		t.Fatalf("without Forgejo = %s", got)
	}
}

func TestPickItemsSayWhereEachRepoIs(t *testing.T) {
	now := time.Now()
	items := pickItems([]repoChoice{
		{name: "here", fullName: "nelsong6/here", dir: `D:\repos\here`, starred: true, recent: now.Add(-2 * time.Hour)},
		{name: "away", fullName: "romaine-life/away", about: "an app"},
	}, now)
	if len(items) != 5 {
		t.Fatalf("items = %d", len(items))
	}
	if items[1].Action.Target != pickRepos || items[2].Action.Target != pickBrowse || items[4].Action.Target != "1" {
		t.Fatal("rows don't carry their actions")
	}
	if got := items[3].Fields[1]; got != "★ starred · last session 2 h ago · nelsong6" {
		t.Errorf("here = %q", got)
	}
	if got := items[4].Fields[1]; got != `not on this PC: clones into D:\repos · romaine-life · an app` {
		t.Errorf("away = %q", got)
	}
}

func TestInCheckoutTakesTheCheckoutAndItsWorktreesOnly(t *testing.T) {
	dir := filepath.Join("D:", "repos", "at")
	for folder, want := range map[string]bool{
		dir:                              true,
		dir + string(filepath.Separator): true,
		strings.ToUpper(dir):             true,
		filepath.Join(dir, ".claude", "worktrees", "AT-1"): true,
		filepath.Join("D:", "repos", "at-claude"):          false,
		filepath.Join(dir, "docs"):                         false,
		filepath.Join(dir, ".claude", "worktrees"):         false,
	} {
		if got := inCheckout(folder, dir); got != want {
			t.Errorf("inCheckout(%q) = %v", folder, got)
		}
	}
}
