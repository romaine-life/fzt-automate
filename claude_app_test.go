package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestReadClaudeApp(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		file := filepath.Join(dir, "state.json")
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return file
	}
	if app, err := readClaudeApp(write(`{"claudeApp": "at-claude", "tools": {}}`)); err != nil || app != "at-claude" {
		t.Errorf("readClaudeApp = %q, %v", app, err)
	}
	if _, err := readClaudeApp(write(`{"tools": {}}`)); err == nil || !strings.Contains(err.Error(), "doesn't say which Claude app") {
		t.Errorf("no claudeApp: %v", err)
	}
	if _, err := readClaudeApp(write(`{`)); err == nil {
		t.Error("unreadable state.json accepted")
	}
	if _, err := readClaudeApp(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing state.json accepted")
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

// Every Claude launcher reads state.json before it does anything else, so a
// missing claudeApp stops it with the reason rather than starting a default.
func TestLaunchersReadClaudeApp(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(file, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := homeState
	homeState = file
	defer func() { homeState = old }()
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
