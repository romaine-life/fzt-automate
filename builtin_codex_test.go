package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeCodexProgram(t *testing.T) {
	dir := t.TempDir()
	if _, err := nativeCodexProgram(dir); err == nil {
		t.Fatal("missing executable accepted")
	}
	exe := filepath.Join(dir, "node_modules", "@openai", "codex", "node_modules", "@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := nativeCodexProgram(dir)
	if err != nil || got != exe {
		t.Fatal(got, err)
	}
}

func TestCodexNeedsPaneBeforeLaunching(t *testing.T) {
	t.Setenv("TMUX_PANE", "")
	if _, err := runLaunchCodex(context.Background(), io.Discard, nil); err == nil {
		t.Fatal("launched outside tmux")
	}
	if err := startCodex(context.Background(), "missing", "."); err == nil {
		t.Fatal("respawned outside tmux")
	}
}
