package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const stateSample = `{"tools":{
 "at":{"repo":"romaine-life/at","assets":{"at-commands.zip":"."}},
 "guitar-tabs":{"repo":"romaine-life/guitar-tabs","assets":{"guitar-tabs-windows-amd64.exe":"guitar-tabs.exe"}},
 "at-home-tools":{"repo":"romaine-life/at-home-tools","assets":{"activity.exe":"activity.exe","homesvc-linux-amd64":"homesvc-linux-amd64"}}
},"appRepos":{"Shows-Remote.exe":"nelsong6/shows-remote"}}`

func sampleRepos(t *testing.T) map[string]string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(file, []byte(stateSample), 0o644); err != nil {
		t.Fatal(err)
	}
	repos, err := appRepos(file)
	if err != nil {
		t.Fatal(err)
	}
	return repos
}

func TestAppReposCoversInstalledAndListedPrograms(t *testing.T) {
	repos := sampleRepos(t)
	want := map[string]string{
		"guitar-tabs.exe":  "romaine-life/guitar-tabs",
		"activity.exe":     "romaine-life/at-home-tools",
		"shows-remote.exe": "nelsong6/shows-remote",
	}
	if len(repos) != len(want) {
		t.Fatalf("repos = %v", repos)
	}
	for exe, repo := range want {
		if repos[exe] != repo {
			t.Errorf("%s = %q, want %q", exe, repos[exe], repo)
		}
	}
}

func TestAppReposSaysWhenTheStateCannotBeRead(t *testing.T) {
	if _, err := appRepos(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("no error for a missing state file")
	}
}

func TestFindAppDirectlyInThePane(t *testing.T) {
	procs := []process{{pid: 10, parent: 1, exe: "guitar-tabs.exe", started: 100}}
	if repo, _ := findApp(procs, 10, sampleRepos(t)); repo != "romaine-life/guitar-tabs" {
		t.Fatalf("repo = %q", repo)
	}
}

func TestFindAppBelowAShell(t *testing.T) {
	procs := []process{
		{pid: 10, parent: 1, exe: "pwsh.exe", started: 100},
		{pid: 11, parent: 10, exe: "conhost.exe", started: 101},
		{pid: 12, parent: 10, exe: "SHOWS-REMOTE.EXE", started: 102},
	}
	if repo, _ := findApp(procs, 10, sampleRepos(t)); repo != "nelsong6/shows-remote" {
		t.Fatalf("repo = %q", repo)
	}
}

func TestFindAppPrefersTheNewestChild(t *testing.T) {
	procs := []process{
		{pid: 10, parent: 1, exe: "pwsh.exe", started: 100},
		{pid: 11, parent: 10, exe: "activity.exe", started: 101},
		{pid: 12, parent: 10, exe: "guitar-tabs.exe", started: 102},
	}
	if repo, _ := findApp(procs, 10, sampleRepos(t)); repo != "romaine-life/guitar-tabs" {
		t.Fatalf("repo = %q", repo)
	}
}

// The at menu starts an app as a child of fzt-automate, the pane's own
// process, which home-sync also installs (TMUX-171).
func TestFindAppSkipsTheLauncherItRunsUnder(t *testing.T) {
	procs := []process{
		{pid: 10, parent: 1, exe: "fzt-automate.exe", started: 100},
		{pid: 11, parent: 10, exe: "pwsh.exe", started: 101},
		{pid: 12, parent: 11, exe: "guitar-tabs.exe", started: 102},
	}
	repos := sampleRepos(t)
	repos["fzt-automate.exe"] = "romaine-life/fzt-automate"
	if repo, _ := findApp(procs, 10, repos); repo != "romaine-life/guitar-tabs" {
		t.Fatalf("repo = %q", repo)
	}
}

func TestFindAppNamesTheJobBeforeTheShell(t *testing.T) {
	procs := []process{
		{pid: 10, parent: 1, exe: "pwsh.exe", started: 100},
		{pid: 11, parent: 10, exe: "vim.exe", started: 101},
	}
	repo, running := findApp(procs, 10, sampleRepos(t))
	if repo != "" || running != "vim.exe" {
		t.Fatalf("repo = %q, running = %q", repo, running)
	}
}

func TestFindAppIgnoresAChildOfAReusedParentID(t *testing.T) {
	procs := []process{
		{pid: 10, parent: 1, exe: "pwsh.exe", started: 100},
		{pid: 11, parent: 10, exe: "guitar-tabs.exe", started: 50},
	}
	repo, running := findApp(procs, 10, sampleRepos(t))
	if repo != "" || running != "pwsh.exe" {
		t.Fatalf("repo = %q, running = %q", repo, running)
	}
}

func TestFindAppNamesWhatRunsWhenNoAppMatches(t *testing.T) {
	procs := []process{{pid: 10, parent: 1, exe: "pwsh.exe", started: 100}}
	repo, running := findApp(procs, 10, sampleRepos(t))
	if repo != "" || running != "pwsh.exe" {
		t.Fatalf("repo = %q, running = %q", repo, running)
	}
}

func TestRepoCheckoutUsesAnExistingCheckout(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "guitar-tabs")
	if err := os.MkdirAll(filepath.Join(want, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := repoCheckout(context.Background(), io.Discard, root, "romaine-life/guitar-tabs")
	if err != nil || got != want {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRepoCheckoutRefusesAFolderThatIsNotACheckout(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "guitar-tabs"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := repoCheckout(context.Background(), io.Discard, root, "romaine-life/guitar-tabs")
	if err == nil || !strings.Contains(err.Error(), "isn't a git checkout") {
		t.Fatalf("err = %v", err)
	}
}

func TestWorkOnAppNeedsItsOriginTab(t *testing.T) {
	t.Setenv(originWindowEnv, "")
	_, err := runWorkOnApp(context.Background(), io.Discard, []string{"claude"})
	if err == nil || !strings.Contains(err.Error(), "which tab") {
		t.Fatalf("err = %v", err)
	}
}

func TestWorkOnAppTakesOnlyKnownAgents(t *testing.T) {
	if _, err := runWorkOnApp(context.Background(), io.Discard, []string{"gemini"}); err == nil {
		t.Fatal("no usage error")
	}
}

func TestTakeoverTellsTheNewTabWhereItCameFrom(t *testing.T) {
	args, err := takeoverArgs("@3", "/dev/pty0", step{kind: stepBuiltin, b: builtins["work-on-app"], args: []string{"claude"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args, " "); !strings.Contains(got, "-e "+originWindowEnv+"=@3") {
		t.Fatalf("args = %s", got)
	}
}
