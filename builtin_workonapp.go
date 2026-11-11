package main

// work-on-app opens an agent in the repo of the app running in the tab it was
// started from (TMUX-102). The running program is the only thing Nelson needs to
// know: which repo holds it comes from home-sync's machine/state.json.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	originWindowEnv = "AT_ORIGIN_WINDOW"
	reposRoot       = `D:\repos`
	forgejoBase     = "https://forgejo.romaine.life/"
)

// homeState is home-sync's machine/state.json, read live from the NAS.
var homeState = `\\NelsonNAS\files\shell-config-profile-home\machine\state.json`

func init() {
	register(&builtin{name: "work-on-app", usage: "claude|codex", terminal: true,
		about: "start an agent in the repo of the app running in the tab you came from", run: runWorkOnApp})
}

// process is one running program. started orders children, and rules out a
// child whose parent id was reused.
type process struct {
	pid, parent uint32
	exe         string
	started     uint64
}

// appRepos maps a program's file name, lower case, to its owner/name repo:
// everything home-sync installs (tools), and what it doesn't (appRepos).
func appRepos(file string) (map[string]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("can't read which repo each app is in: %w", err)
	}
	var state struct {
		Tools map[string]struct {
			Repo   string            `json:"repo"`
			Assets map[string]string `json:"assets"`
		} `json:"tools"`
		AppRepos map[string]string `json:"appRepos"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("can't read %s: %w", file, err)
	}
	repos := map[string]string{}
	for _, tool := range state.Tools {
		for _, saved := range tool.Assets {
			if strings.EqualFold(filepath.Ext(saved), ".exe") {
				repos[strings.ToLower(saved)] = tool.Repo
			}
		}
	}
	for exe, repo := range state.AppRepos {
		repos[strings.ToLower(exe)] = repo
	}
	return repos, nil
}

// findApp walks down from the pane's own process, newest child first, and
// gives the first known program. As in at-tmux's pane_current_command, the
// pane's own process counts only when nothing below it does: it is the shell
// or launcher the app runs in, and an app the at menu starts runs under
// fzt-automate. The second result names what the pane is running when no app
// was recognized.
func findApp(procs []process, root uint32, repos map[string]string) (repo, running string) {
	children := map[uint32][]process{}
	var top *process
	for i, p := range procs {
		if p.pid == root {
			top = &procs[i]
		} else {
			children[p.parent] = append(children[p.parent], p)
		}
	}
	if top == nil {
		return "", ""
	}
	jobs := func(p process) []process {
		var kids []process
		for _, k := range children[p.pid] {
			if k.started >= p.started && !strings.EqualFold(k.exe, "conhost.exe") {
				kids = append(kids, k)
			}
		}
		sort.SliceStable(kids, func(i, j int) bool { return kids[i].started > kids[j].started })
		return kids
	}
	for queue := jobs(*top); len(queue) > 0; queue = queue[1:] {
		p := queue[0]
		if repo, ok := repos[strings.ToLower(p.exe)]; ok {
			return repo, ""
		}
		if running == "" {
			running = p.exe
		}
		queue = append(queue, jobs(p)...)
	}
	if repo, ok := repos[strings.ToLower(top.exe)]; ok {
		return repo, ""
	}
	if running == "" {
		running = top.exe
	}
	return "", running
}

// paneProcessID asks tmux for the process of the active pane in window.
func paneProcessID(window string) (uint32, error) {
	out, err := exec.Command("tmux", "display", "-p", "-t", window, "#{pane_pid}").Output()
	if err != nil {
		return 0, fmt.Errorf("tmux can't say what tab %s is running: %w", window, err)
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("tmux gave no process for tab %s", window)
	}
	return uint32(pid), nil
}

// repoCheckout gives the local checkout of an owner/name repo, cloning it when
// this PC doesn't have one, so the key works the same on every home PC.
func repoCheckout(ctx context.Context, out io.Writer, root, repo string) (string, error) {
	dir := filepath.Join(root, path.Base(repo))
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return dir, nil
	}
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("%s exists but isn't a git checkout", dir)
	}
	fmt.Fprintf(out, "Cloning %s into %s\n", repo, dir)
	cmd := exec.CommandContext(ctx, "git", "clone", forgejoBase+repo+".git", dir)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("couldn't clone %s: %w", repo, err)
	}
	return dir, nil
}

func agentName(agent string) string {
	if agent == "codex" {
		return "Codex"
	}
	return "Claude"
}

// workFolder is the folder the agent starts in: the app's repo, else one the
// person picks, with the picker saying why it appeared.
func workFolder(ctx context.Context, out io.Writer, agent, window string) (string, error) {
	repos, err := appRepos(homeState)
	if err != nil {
		return "", err
	}
	pid, err := paneProcessID(window)
	if err != nil {
		return "", err
	}
	procs, err := listProcesses()
	if err != nil {
		return "", err
	}
	repo, running := findApp(procs, pid, repos)
	if repo != "" {
		return repoCheckout(ctx, out, reposRoot, repo)
	}
	start := tabFolder(window)
	if start == "" {
		start = reposRoot
	}
	if running == "" {
		running = "nothing"
	}
	title := fmt.Sprintf("No app recognized (that tab is running %s). Start %s in", running, agentName(agent))
	return pickFolder(ctx, "--title", title, "--start-dir", start)
}

func runWorkOnApp(ctx context.Context, out io.Writer, args []string) (string, error) {
	if len(args) != 1 || (args[0] != "claude" && args[0] != "codex") {
		return "", usageError(builtins["work-on-app"])
	}
	agent := args[0]
	window := os.Getenv(originWindowEnv)
	if window == "" {
		return "", errors.New("tmux didn't say which tab this was started from: use the at menu or the Ctrl+T chord")
	}
	dir, err := workFolder(ctx, out, agent, window)
	if err != nil || dir == "" {
		return "", err
	}
	if agent == "claude" {
		return "", startClaude(dir, "", false)
	}
	program, err := codexProgram()
	if err != nil {
		return "", err
	}
	return "", startCodex(ctx, program, dir)
}
