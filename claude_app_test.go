package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// setClaudeApp makes tmux's @claude-app read as app for one test.
func setClaudeApp(t *testing.T, app string, err error) {
	old := claudeAppSetting
	claudeAppSetting = func() (string, error) { return app, err }
	t.Cleanup(func() { claudeAppSetting = old })
}

func TestReadClaudeApp(t *testing.T) {
	setClaudeApp(t, "at-claude", nil)
	if app, err := readClaudeApp(); err != nil || app != "at-claude" {
		t.Errorf("readClaudeApp = %q, %v", app, err)
	}
	setClaudeApp(t, "", nil)
	if _, err := readClaudeApp(); err == nil || !strings.Contains(err.Error(), "doesn't say which Claude app") {
		t.Errorf("no @claude-app: %v", err)
	}
	setClaudeApp(t, "", errors.New("no server"))
	if _, err := readClaudeApp(); err == nil || !strings.Contains(err.Error(), "no server") {
		t.Errorf("tmux failed: %v", err)
	}
}

func TestClaudeArgv(t *testing.T) {
	for _, c := range []struct {
		session string
		chrome  bool
		want    []string
	}{
		{"", false, []string{"claude"}},
		{"abc", false, []string{"claude", "--resume", "abc"}},
		{"abc", true, []string{"claude", "--resume", "abc", "--chrome"}},
	} {
		got, err := claudeArgv(appClaudeCode, c.session, c.chrome)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("claude-code %q %v: %q, %v; want %q", c.session, c.chrome, got, err, c.want)
		}
	}
	if _, err := claudeArgv("claude", "", false); err == nil || !strings.Contains(err.Error(), "at-claude or claude-code") {
		t.Errorf("unknown app: %v", err)
	}

	if runtime.GOOS != "windows" {
		return
	}
	programFiles := t.TempDir()
	t.Setenv("ProgramFiles", programFiles)
	program := filepath.Join(programFiles, "at-home", "at-claude", "current", "at-claude.exe")
	os.MkdirAll(filepath.Dir(program), 0o755)
	if err := os.WriteFile(program, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(t.TempDir(), "tools.json")
	if err := os.WriteFile(record, []byte(`[{"tool": "at-claude", "entry": "at-claude.exe"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldRecord := homeSyncTools
	homeSyncTools = record
	defer func() { homeSyncTools = oldRecord }()
	for _, c := range []struct {
		session string
		chrome  bool
		want    []string
	}{
		{"", false, []string{program}},
		{"abc", false, []string{program, "-resume", "abc"}},
		{"abc", true, []string{program, "-resume", "abc", "-claude-arg=--chrome"}},
	} {
		got, err := claudeArgv(appAtClaude, c.session, c.chrome)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("at-claude %q %v: %q, %v; want %q", c.session, c.chrome, got, err, c.want)
		}
	}
}

// Every Claude launcher reads @claude-app before it does anything else, so an
// unset one stops it with the reason rather than starting a default.
func TestLaunchersReadClaudeApp(t *testing.T) {
	setClaudeApp(t, "", nil)
	t.Setenv("TMUX_PANE", "%1")
	if err := startClaude(t.TempDir(), "abc", true); err == nil || !strings.Contains(err.Error(), "doesn't say which Claude app") {
		t.Errorf("startClaude: %v", err)
	}
	for _, args := range [][]string{{"abc"}, {"abc", "--chrome"}} {
		if _, err := runResumeClaude(context.Background(), io.Discard, args); err == nil || !strings.Contains(err.Error(), "doesn't say which Claude app") {
			t.Errorf("resume-claude %q: %v", args, err)
		}
	}
	for _, args := range [][]string{{"--chrome"}, {"abc", "--other"}, {"abc", "--chrome", "x"}} {
		if _, err := runResumeClaude(context.Background(), io.Discard, args); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("resume-claude %q: %v, want usage", args, err)
		}
	}
}

// launch-claude with at-claude starts it at once on its own repo list, in the
// repos folder (ATC-194): the list loads in the window, which deals the
// session's avatars meanwhile. Nothing is picked here.
func TestLaunchClaudeLetsAtClaudePick(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("at-claude is found through home-sync's install, which only Windows has")
	}
	setClaudeApp(t, appAtClaude, nil)
	t.Setenv("TMUX_PANE", "%7")
	programFiles := t.TempDir()
	t.Setenv("ProgramFiles", programFiles)
	program := filepath.Join(programFiles, "at-home", "at-claude", "current", "at-claude.exe")
	os.MkdirAll(filepath.Dir(program), 0o755)
	if err := os.WriteFile(program, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(t.TempDir(), "tools.json")
	if err := os.WriteFile(record, []byte(`[{"tool": "at-claude", "entry": "at-claude.exe"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldRecord, oldTmux := homeSyncTools, runTmux
	t.Cleanup(func() { homeSyncTools, runTmux = oldRecord, oldTmux })
	homeSyncTools = record
	var ran []string
	runTmux = func(args ...string) ([]byte, error) { ran = args; return nil, nil }

	if _, err := runLaunchClaude(context.Background(), io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	want := append([]string{"respawn-pane", "-k", "-t", "%7", "-c", reposRoot}, tmuxProgram([]string{program, "-pick-repo"})...)
	if !slices.Equal(ran, want) {
		t.Errorf("tmux %q, want %q", ran, want)
	}
}
