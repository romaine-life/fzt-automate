package main

// Every Claude launcher (launch-claude, work-on-app claude, resume-claude) starts
// the app home-sync's machine/state.json names as claudeApp: at-claude, the
// terminal harness, or Claude Code itself (ATC-6). It is read at each launch, so
// a change to state.json takes effect at the next one.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	appAtClaude   = "at-claude"
	appClaudeCode = "claude-code"
)

// readClaudeApp gives state.json's claudeApp.
func readClaudeApp(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("can't read which Claude app to start: %w", err)
	}
	var state struct {
		ClaudeApp string `json:"claudeApp"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return "", fmt.Errorf("can't read %s: %w", file, err)
	}
	if state.ClaudeApp == "" {
		return "", fmt.Errorf("%s doesn't say which Claude app to start: claudeApp is %s or %s", file, appAtClaude, appClaudeCode)
	}
	return state.ClaudeApp, nil
}

// claudeArgv is the program and arguments that start app, continuing sessionID
// when it is set, with Claude in Chrome connected when chrome is.
func claudeArgv(app, sessionID string, chrome bool) ([]string, error) {
	switch app {
	case appClaudeCode:
		argv := []string{"claude"}
		if sessionID != "" {
			argv = append(argv, "--resume", sessionID)
		}
		if chrome {
			argv = append(argv, "--chrome")
		}
		return argv, nil
	case appAtClaude:
		program, err := toolPath("at-claude", "")
		if err != nil {
			return nil, err
		}
		argv := []string{program}
		if sessionID != "" {
			argv = append(argv, "-resume", sessionID)
		}
		if chrome {
			argv = append(argv, "-claude-arg=--chrome")
		}
		return argv, nil
	}
	return nil, fmt.Errorf("claudeApp in state.json is %q: it can be %s or %s", app, appAtClaude, appClaudeCode)
}

// startClaude has tmux restart this pane in dir as the Claude app state.json
// names, continuing sessionID when it is set.
func startClaude(dir, sessionID string, chrome bool) error {
	app, err := readClaudeApp(homeState)
	if err != nil {
		return err
	}
	argv, err := claudeArgv(app, sessionID, chrome)
	if err != nil {
		return err
	}
	return respawnAs(dir, app, argv)
}

// respawnAs has tmux restart this pane as argv in dir, so the tab is the
// program's and closes with it; name is how an error refers to it.
func respawnAs(dir, name string, argv []string) error {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return fmt.Errorf("not in a tmux pane: run it from the at menu")
	}
	tmuxArgs := append([]string{"respawn-pane", "-k", "-t", pane, "-c", dir}, tmuxProgram(argv)...)
	if msg, err := exec.Command("tmux", tmuxArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux couldn't start %s: %s", name, strings.TrimSpace(string(msg)))
	}
	return nil
}
