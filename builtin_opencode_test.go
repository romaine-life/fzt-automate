package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenCodeTmuxFailureExplainsTimeoutAndEmptyOutput(t *testing.T) {
	err := openCodeTmuxError(context.Background(), "start OpenCode", nil, errors.New("cannot start tmux"))
	if !strings.Contains(err.Error(), "cannot start tmux") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := openCodeTmuxError(ctx, "start OpenCode", nil, errors.New("process killed")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestOpenCodeProgram(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if _, err := openCodeProgram(); err == nil {
		t.Fatal("missing install accepted")
	}
	name := "opencode"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	shim := filepath.Join(dir, name)
	if err := os.WriteFile(shim, []byte("unused"), 0755); err != nil {
		t.Fatal(err)
	}
	want := shim
	if runtime.GOOS == "windows" {
		if _, err := openCodeProgram(); err == nil {
			t.Fatal("broken npm install accepted")
		}
		want = filepath.Join(dir, "node_modules", "@opencode", "cli", "bin", "opencode.exe")
		if err := os.MkdirAll(filepath.Dir(want), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(want, []byte("unused"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := openCodeProgram(); err != nil || got != want {
		t.Fatalf("program = %q, %v; want executable %q", got, err, want)
	}
}

func TestOpenCodeLaunchPreflight(t *testing.T) {
	t.Setenv("TMUX_PANE", "")
	if _, err := runLaunchOpenCode(context.Background(), io.Discard, []string{"one", "two"}); err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("invalid arguments: %v", err)
	}
	if _, err := runLaunchOpenCode(context.Background(), io.Discard, nil); err == nil || !strings.Contains(err.Error(), "at menu") {
		t.Fatalf("missing pane: %v", err)
	}
}

func TestOpenCodeRequiresAPIKeyLogin(t *testing.T) {
	file := filepath.Join(t.TempDir(), "auth.json")
	if err := checkOpenCodeAuth(file); err == nil {
		t.Fatal("missing login accepted")
	}
	for _, data := range []string{
		`{}`, `{"anthropic":{"type":"oauth","key":"sensitive-test-value"}}`,
		`{"anthropic":{"type":"api","key":" "}}`, `sensitive-test-value`,
	} {
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		err := checkOpenCodeAuth(file)
		if err == nil || strings.Contains(err.Error(), "sensitive-test-value") {
			t.Fatalf("invalid login accepted or leaked: %v", err)
		}
	}
	if err := os.WriteFile(file, []byte(`{"anthropic":{"type":"api","key":"test-key"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkOpenCodeAuth(file); err != nil {
		t.Fatal(err)
	}
}
