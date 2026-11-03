package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func init() {
	register(&builtin{name: "launch-codex", usage: "[start-folder]", terminal: true,
		about: "pick a folder, then start Codex in this window", run: runLaunchCodex})
	register(&builtin{name: "resume-codex", terminal: true,
		about: "find and resume a past Codex session, across all folders", run: runResumeCodex})
}

func codexProgram() (string, error) {
	p, err := exec.LookPath("codex")
	if err != nil {
		return "", errors.New("Codex isn't installed here; run sync-home")
	}
	if runtime.GOOS != "windows" || strings.EqualFold(filepath.Ext(p), ".exe") {
		return p, nil
	}
	return nativeCodexProgram(filepath.Dir(p))
}

func nativeCodexProgram(shims string) (string, error) {
	base := filepath.Join(shims, "node_modules", "@openai", "codex")
	for _, pattern := range []string{
		filepath.Join(base, "node_modules", "@openai", "codex-win32-*", "vendor", "*", "bin", "codex.exe"),
		filepath.Join(base, "vendor", "*", "codex", "codex.exe"),
	} {
		matches, _ := filepath.Glob(pattern)
		if len(matches) == 1 {
			return matches[0], nil
		}
	}
	return "", errors.New("Codex's native npm executable is missing; run sync-home")
}

func runLaunchCodex(ctx context.Context, out io.Writer, args []string) (string, error) {
	if len(args) > 1 {
		return "", usageError(builtins["launch-codex"])
	}
	if os.Getenv("TMUX_PANE") == "" {
		return "", errors.New("not in a tmux pane: run it from the at menu")
	}
	program, err := codexProgram()
	if err != nil {
		return "", err
	}
	start := pickDir(ctx)
	if start == "" {
		start = `D:\repos`
	}
	if len(args) == 1 {
		start = args[0]
	}
	dir, err := pickFolder(ctx, "--title", "Launch Codex in", "--start-dir", start)
	if err != nil || dir == "" {
		return "", err
	}
	return "", startCodex(ctx, program, dir)
}

func runResumeCodex(ctx context.Context, out io.Writer, args []string) (string, error) {
	if len(args) != 0 {
		return "", usageError(builtins["resume-codex"])
	}
	program, err := codexProgram()
	if err != nil {
		return "", err
	}
	dir := pickDir(ctx)
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	// Codex's own picker includes named sessions, previews, and all projects;
	// its recorded cwd is restored when the selected session resumes.
	return "", startCodex(ctx, program, dir, "resume", "--all")
}

func startCodex(ctx context.Context, program, dir string, args ...string) error {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return errors.New("not in a tmux pane: run it from the at menu")
	}
	launchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if msg, err := exec.CommandContext(launchCtx, "tmux", "set-option", "-p", "-t", pane, "remain-on-exit", "failed").CombinedOutput(); err != nil {
		return fmt.Errorf("couldn't retain Codex launch failures: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	// The shared Codex daemon keeps the environment of its first terminal.
	// Keep this runtime in its pane so lifecycle hooks inherit this TMUX_PANE.
	argv := append([]string{"respawn-pane", "-k", "-t", pane, "-c", dir}, tmuxProgram(append([]string{program, "--no-daemon"}, args...))...)
	if msg, err := exec.CommandContext(launchCtx, "tmux", argv...).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux couldn't start Codex: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	return nil
}
