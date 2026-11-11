package main

// Every Claude launcher (launch-claude, work-on-app claude, resume-claude) starts
// the app tmux's @claude-app names: at-claude, the terminal harness, or Claude Code
// itself (ATC-6, ATC-7). It is read at each launch, so a change to the setting takes
// effect at the next one.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	appAtClaude   = "at-claude"
	appClaudeCode = "claude-code"
)

// claudeAppSetting gives tmux's @claude-app, empty when it isn't set.
var claudeAppSetting = func() (string, error) {
	out, err := exec.Command("tmux", "show", "-gqv", "@claude-app").Output()
	if err != nil {
		return "", fmt.Errorf("can't ask tmux which Claude app to start: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// readClaudeApp gives tmux's @claude-app.
func readClaudeApp() (string, error) {
	app, err := claudeAppSetting()
	if err != nil {
		return "", err
	}
	if app == "" {
		return "", fmt.Errorf("tmux's @claude-app doesn't say which Claude app to start: it is %s or %s", appAtClaude, appClaudeCode)
	}
	return app, nil
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
	return nil, fmt.Errorf("tmux's @claude-app is %q: it can be %s or %s", app, appAtClaude, appClaudeCode)
}

// startClaude has tmux restart this pane in dir as the Claude app @claude-app
// names, continuing sessionID when it is set.
func startClaude(dir, sessionID string, chrome bool) error {
	app, err := readClaudeApp()
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
