package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEveryBuiltinHasItsShape(t *testing.T) {
	for name, b := range builtins {
		if (b.run == nil) == (b.rows == nil) {
			t.Errorf("%s needs exactly one of run and rows", name)
		}
		if b.rows != nil && b.terminal {
			t.Errorf("%s fills a folder, so it can't need a terminal", name)
		}
		if b.about == "" {
			t.Errorf("%s has no description", name)
		}
	}
}

func TestToolPaths(t *testing.T) {
	nas, local := t.TempDir(), t.TempDir()
	oldNas, oldLocal, oldConfig := toolsNas, toolsLocal, configNas
	toolsNas, toolsLocal, configNas = nas, local, nas
	defer func() { toolsNas, toolsLocal, configNas = oldNas, oldLocal, oldConfig }()

	write := func(path, body string) {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(nas, "spt", "current.json"), `{"version":"v2","entry":"spotify_player.exe"}`)
	write(filepath.Join(nas, "spt", "v2", "spotify_player.exe"), "")
	write(filepath.Join(nas, "spt", "v2", "other.exe"), "")
	write(filepath.Join(local, "spt", "v2", "spotify_player.exe"), "")
	write(filepath.Join(nas, "spt-config", "app.toml"), "")
	unknown := "not published"
	if runtime.GOOS == "windows" {
		unknown = "isn't a tool home-sync installs"
		programFiles := t.TempDir()
		t.Setenv("ProgramFiles", programFiles)
		record := filepath.Join(t.TempDir(), "tools.json")
		oldRecord := homeSyncTools
		homeSyncTools = record
		defer func() { homeSyncTools = oldRecord }()
		if _, err := expandArg("tool:spt"); err == nil || !strings.Contains(err.Error(), "home-sync tools") {
			t.Fatalf("no home-sync list: %v", err)
		}
		write(record, `[
			{"tool": "spt", "repo": "romaine-life/spotify-player", "tag": "v2", "entry": "spotify_player.exe"},
			{"tool": "guitar-tabs", "repo": "romaine-life/guitar-tabs", "tag": "v2", "entry": "guitar-tabs.exe"},
			{"tool": "brand-new-app", "repo": "romaine-life/brand-new-app", "tag": "v0.1.0", "entry": "brand-new-app.exe"},
			{"tool": "older-home-sync", "repo": "romaine-life/older", "tag": "v1"}
		]`)

		// A tool this code has never heard of runs from home-sync's list alone, so no
		// list of tools in fzt-automate can come back (HOMECFG-213).
		newApp := filepath.Join(programFiles, "at-home", "brand-new-app", "current", "brand-new-app.exe")
		write(newApp, "managed")
		if got, err := expandArg("tool:brand-new-app"); err != nil || got != newApp {
			t.Fatalf("tool:brand-new-app = %q %v", got, err)
		}
		if _, err := expandArg("tool:older-home-sync"); err == nil || !strings.Contains(err.Error(), "home-sync tools") {
			t.Fatalf("a list from before home-sync named programs: %v", err)
		}

		write(filepath.Join(nas, "guitar-tabs", "current.json"), `{"version":"v2","entry":"guitar-tabs.exe"}`)
		write(filepath.Join(nas, "guitar-tabs", "v2", "guitar-tabs.exe"), "stale")
		write(filepath.Join(local, "guitar-tabs", "v2", "guitar-tabs.exe"), "stale")
		guitarTabs := filepath.Join(programFiles, "at-home", "guitar-tabs", "current", "guitar-tabs.exe")
		write(guitarTabs, "managed")
		if got, err := expandArg("tool:guitar-tabs"); err != nil || got != guitarTabs {
			t.Fatalf("managed guitar-tabs = %q %v", got, err)
		}
		if err := os.Remove(guitarTabs); err != nil {
			t.Fatal(err)
		}
		if _, err := expandArg("tool:guitar-tabs"); err == nil || !strings.Contains(err.Error(), "home-sync tools") {
			t.Fatalf("missing managed guitar-tabs must not use stale copies: %v", err)
		}
		managed := filepath.Join(programFiles, "at-home", "spt", "current")
		write(filepath.Join(managed, "spotify_player.exe"), "")
		write(filepath.Join(managed, "other.exe"), "")
		for arg, file := range map[string]string{"tool:spt": "spotify_player.exe", "tool:spt/other.exe": "other.exe"} {
			if got, err := expandArg(arg); err != nil || got != filepath.Join(managed, file) {
				t.Fatalf("managed %s = %q %v", arg, got, err)
			}
		}
		if err := os.Remove(filepath.Join(managed, "spotify_player.exe")); err != nil {
			t.Fatal(err)
		}
		if _, err := expandArg("tool:spt"); err == nil || !strings.Contains(err.Error(), "home-sync tools") {
			t.Fatalf("missing managed executable must not use stale NAS/Drive copies: %v", err)
		}
	} else {

		// The Drive copy when it holds the release, else the NAS.
		if got, err := expandArg("tool:spt"); err != nil || got != filepath.Join(local, "spt", "v2", "spotify_player.exe") {
			t.Fatalf("tool:spt = %q %v", got, err)
		}
		if got, err := expandArg("tool:spt/other.exe"); err != nil || got != filepath.Join(nas, "spt", "v2", "other.exe") {
			t.Fatalf("tool:spt/other.exe = %q %v", got, err)
		}
	}
	if got, err := expandArg("tool-config:spt"); err != nil || got != filepath.Join(nas, "spt-config") {
		t.Fatalf("tool-config:spt = %q %v", got, err)
	}
	if _, err := expandArg("tool:nothing"); err == nil || !strings.Contains(err.Error(), unknown) {
		t.Fatalf("an unknown tool: %v", err)
	}
}

func TestCleanTabFolder(t *testing.T) {
	for in, want := range map[string]string{
		"\tD://repos//tmux-windows-fork\n":  filepath.FromSlash("D:/repos/tmux-windows-fork"),
		"file://NELSONPC/D:/repos/at\tD:/x": filepath.FromSlash("D:/repos/at"),
		"\t\n":                              "",
	} {
		if got := cleanTabFolder(in); got != want {
			t.Errorf("cleanTabFolder(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWebURL(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:romaine-life/at.git":           "https://github.com/romaine-life/at",
		"https://github.com/romaine-life/fzt.git":      "https://github.com/romaine-life/fzt",
		"ssh://git@github.com/nelsong6/lights.git":     "https://github.com/nelsong6/lights",
		"https://github.com/romaine-life/fzt-automate": "https://github.com/romaine-life/fzt-automate",
	} {
		if got := webURL(in); got != want {
			t.Errorf("webURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitPruneKeepsMainAndTheCurrentBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "one")
	run("branch", "old-1")
	run("branch", "old-2")
	run("checkout", "-q", "-b", "working")

	got, err := runGitPrune(withDir(context.Background(), dir), io.Discard, nil)
	if err != nil || got != "deleted 2: old-1, old-2" {
		t.Fatalf("%q %v", got, err)
	}
	if got, _ := runGitPrune(withDir(context.Background(), dir), io.Discard, nil); got != "no branches to delete" {
		t.Fatalf("again: %q", got)
	}
	if _, err := runGitPrune(context.Background(), io.Discard, nil); err != errNoTabFolder {
		t.Fatalf("with no folder: %v", err)
	}
}

func TestWorkspaceEdits(t *testing.T) {
	dir := t.TempDir()
	wsFile := filepath.Join(dir, "perpetual.code-workspace")
	os.WriteFile(wsFile, []byte(`{
	"folders": [
		{"path": "D:/workspace"},
		{"path": "D:/repos/a"},
	],
	"settings": {"window.title": "perpetual"},
}`), 0o644)
	profile := filepath.Join(dir, "profiles", "ws-profiles", "apps", "apps.code-workspace")
	os.MkdirAll(filepath.Dir(profile), 0o755)
	os.WriteFile(profile, []byte(`{"folders":[{"path":"D:/anchor"},{"path":"D:\\repos\\b"},{"path":"D:/repos/a"}]}`), 0o644)
	t.Setenv("WORKSPACE_FILE", wsFile)
	t.Setenv("PROFILE_NUM", "test")
	// profilesDir names D:\profiles; point the test at its own.
	old := profilesRoot
	profilesRoot = func(string) string { return filepath.Join(dir, "profiles", "ws-profiles") }
	defer func() { profilesRoot = old }()

	paths := func() string {
		ws, err := readWorkspace(wsFile)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(ws.paths(), "|")
	}
	ctx := context.Background()
	if _, err := runWorkspace(ctx, io.Discard, []string{"inject", "apps"}); err != nil {
		t.Fatal(err)
	}
	if got := paths(); got != "D:/workspace|D:/repos/a|D:/repos/b" {
		t.Fatalf("after inject: %s", got)
	}
	if _, err := runWorkspace(ctx, io.Discard, []string{"remove", `D:\repos\a`}); err != nil || paths() != "D:/workspace|D:/repos/b" {
		t.Fatalf("after remove: %s %v", paths(), err)
	}
	if _, err := runWorkspace(ctx, io.Discard, []string{"reset"}); err != nil || paths() != "D:/workspace" {
		t.Fatalf("after reset: %s %v", paths(), err)
	}
	if _, err := runWorkspace(ctx, io.Discard, []string{"load", "apps"}); err != nil || paths() != "D:/workspace|D:/repos/b|D:/repos/a" {
		t.Fatalf("after load: %s %v", paths(), err)
	}
	data, _ := os.ReadFile(wsFile)
	if !strings.Contains(string(data), `"window.title": "perpetual"`) || strings.Index(string(data), "folders") > strings.Index(string(data), "settings") {
		t.Fatalf("the rest of the file must stay as it was, in order:\n%s", data)
	}
	rows, _ := wsProfileRows(ctx, []string{"clone"})
	if len(rows) != 1 || rows[0].action != "workspace clone apps" {
		t.Fatalf("profile rows: %+v", rows)
	}
}
