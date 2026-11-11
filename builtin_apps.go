package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func init() {
	register(&builtin{name: "fzt-picker", about: "restart fzt-picker, the file-dialog injector, on its current release", run: runFztPicker})
	register(&builtin{name: "show", usage: "<program> [arguments]", terminal: true,
		about: "run a program in its own tab and keep its output there until Enter", run: runShow})
	register(&builtin{name: "explore", terminal: true, about: "pick a folder, then open it in Explorer", run: runExplore})
	register(&builtin{name: "launch-claude", usage: "[start-folder]", terminal: true,
		about: "pick a folder, then start the Claude app state.json names there in this tab", run: runLaunchClaude})
	register(&builtin{name: "launch-opencode", usage: "[start-folder]", terminal: true,
		about: "pick a folder, then start OpenCode with the Anthropic API key in this tab", run: runLaunchOpenCode})
	register(&builtin{name: "ollama-running", about: "folder: the ollama models running now; picking one stops it", rows: ollamaRunningRows})
}

func runFztPicker(ctx context.Context, out io.Writer, args []string) (string, error) {
	if len(args) != 0 {
		return "", usageError(builtins["fzt-picker"])
	}
	path, err := toolPath("fzt-picker", "")
	if err != nil {
		return "", err
	}
	if err := stopProcesses("fzt-picker.exe"); err != nil {
		return "", err
	}
	// Started by the shell, as Start-Process did, so it outlives the menu.
	if err := openURL(path); err != nil {
		return "", fmt.Errorf("fzt-picker didn't start: %w", err)
	}
	return "fzt-picker restarted. Apps already running keep the hook they loaded until they restart.", nil
}

// runShow runs a program with the tab's terminal, then holds the tab until Enter,
// so a command that prints and ends (ollama list) can be read.
func runShow(ctx context.Context, out io.Writer, args []string) (string, error) {
	if len(args) == 0 {
		return "", usageError(builtins["show"])
	}
	args, err := expandArgs(args)
	if err != nil {
		return "", err
	}
	cmd, err := programCommand(args)
	if err != nil {
		return "", err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	runErr := cmd.Run()
	fmt.Fprint(out, "\n[press Enter to close]")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	return "", runErr
}

// pickFolder runs fzt-picker's folder picker in this terminal and returns the
// folder picked, empty when it was closed without one.
func pickFolder(ctx context.Context, extra ...string) (string, error) {
	picker, err := toolPath("fzt-picker", "fzt-picker-frontend.exe")
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, picker, append([]string{"--folders-only"}, extra...)...)
	cmd.Stdin, cmd.Stderr = os.Stdin, os.Stderr
	var picked bytes.Buffer
	cmd.Stdout = &picked
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && strings.TrimSpace(picked.String()) == "" {
			return "", nil // closed without a pick
		}
		return "", err
	}
	return strings.TrimSpace(picked.String()), nil
}

func runExplore(ctx context.Context, out io.Writer, args []string) (string, error) {
	dir, err := pickFolder(ctx)
	if err != nil || dir == "" {
		return "", err
	}
	if err := openURL(dir); err != nil {
		return "", err
	}
	return "", nil
}

// runLaunchClaude starts the Claude app in the folder picked, in this tab:
// tmux restarts the pane with it in place of this program, so the tab is
// Claude's and closes with it.
func runLaunchClaude(ctx context.Context, out io.Writer, args []string) (string, error) {
	if len(args) > 1 {
		return "", usageError(builtins["launch-claude"])
	}
	if os.Getenv("TMUX_PANE") == "" {
		return "", errors.New("not in a tmux pane: run it from the at menu")
	}
	start := `D:\repos`
	if len(args) == 1 {
		start = args[0]
	}
	dir, err := pickFolder(ctx, "--title", "Launch Claude in", "--start-dir", start)
	if err != nil || dir == "" {
		return "", err
	}
	return "", startClaude(dir, "", false)
}

func ollamaRunningRows(ctx context.Context, args []string) ([]folderRow, error) {
	out, err := exec.CommandContext(ctx, "ollama", "ps").Output()
	if err != nil {
		return nil, fmt.Errorf("ollama ps failed: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var rows []folderRow
	for _, l := range lines[1:] { // the first is the header
		if f := strings.Fields(l); len(f) > 0 {
			rows = append(rows, folderRow{name: f[0], description: "stop it", action: joinArgs("ollama", "stop", f[0])})
		}
	}
	if len(rows) == 0 {
		return []folderRow{{name: "no models running"}}, nil
	}
	return rows, nil
}
