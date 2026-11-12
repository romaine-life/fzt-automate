package main

import (
	"context"
	"encoding/json"
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

// npm's Windows command is a .cmd shim. Start the executable shipped by that
// package directly, just as Unix starts its executable, with no shell wrapper.
func openCodeProgram() (string, error) {
	program, err := exec.LookPath("opencode")
	if err != nil {
		return "", errors.New("OpenCode isn't installed here (not on PATH); run sync-home")
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(program), ".exe") {
		program = filepath.Join(filepath.Dir(program), "node_modules", "@opencode", "cli", "bin", "opencode.exe")
		if info, err := os.Stat(program); err != nil || info.IsDir() {
			return "", errors.New("OpenCode's npm executable is missing; run sync-home to repair @opencode/cli")
		}
	}
	return program, nil
}

func checkOpenCodeAuth(file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return errors.New("OpenCode's Anthropic API login can't be read; run sync-home to provision it")
	}
	var auth struct {
		Anthropic struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		} `json:"anthropic"`
	}
	// Never include the file contents or parser error: they may contain the key.
	if json.Unmarshal(data, &auth) != nil || auth.Anthropic.Type != "api" || strings.TrimSpace(auth.Anthropic.Key) == "" {
		return errors.New("OpenCode has no Anthropic API key login; run sync-home to provision it")
	}
	return nil
}

func runLaunchOpenCode(ctx context.Context, out io.Writer, args []string) (string, error) {
	if len(args) > 1 {
		return "", usageError(builtins["launch-opencode"])
	}
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return "", errors.New("not in a tmux pane: run it from the at menu")
	}
	program, err := openCodeProgram()
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	if err := checkOpenCodeAuth(filepath.Join(dataHome, "opencode", "auth.json")); err != nil {
		return "", err
	}
	dir, err := launchFolder(ctx, out, "Claude in OpenCode (API key)", reposRoot, args)
	if err != nil || dir == "" {
		return "", err
	}
	// Keep a program that starts but exits unsuccessfully visible too. A failure
	// before respawn returns through the built-in's normal error display/history.
	launchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if msg, err := exec.CommandContext(launchCtx, "tmux", "set-option", "-p", "-t", pane, "remain-on-exit", "failed").CombinedOutput(); err != nil {
		return "", openCodeTmuxError(launchCtx, "retain OpenCode launch failures", msg, err)
	}
	fmt.Fprintln(out, "Starting Claude in OpenCode using the Anthropic API key…")
	tmuxArgs := append([]string{"respawn-pane", "-k", "-t", pane, "-c", dir}, tmuxProgram([]string{program})...)
	if msg, err := exec.CommandContext(launchCtx, "tmux", tmuxArgs...).CombinedOutput(); err != nil {
		return "", openCodeTmuxError(launchCtx, "start OpenCode", msg, err)
	}
	return "", nil
}

func openCodeTmuxError(ctx context.Context, action string, output []byte, err error) error {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if detail := strings.TrimSpace(string(output)); detail != "" {
		return fmt.Errorf("tmux couldn't %s: %w: %s", action, err, detail)
	}
	return fmt.Errorf("tmux couldn't %s: %w", action, err)
}
