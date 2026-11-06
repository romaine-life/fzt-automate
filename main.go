// fzt-automate — shell automation tool powered by fzt.
//
// Loads its menu cache from a config directory, presents an interactive tree
// picker, and prints the selected leaf name to stdout. The shell wrapper
// executes it as a function.
//
// Config directory is determined in this order:
//  1. $FZT_CONFIG_DIR if set
//  2. %LOCALAPPDATA%\fzt-automate (Windows)
//  3. $XDG_CONFIG_HOME/fzt-automate (Linux/Mac with XDG)
//  4. $HOME/.config/fzt-automate (fallback)
//
// Usage:
//
//	fzt-automate
//	fzt-automate --title "What would you like to do?"
//	fzt-automate --sync    # pull the menu into the cache, without the picker (renews the token from the TPM key if needed)
//	fzt-automate --run     # the menu as a tab's program (Ctrl+T Ctrl+A): runs
//	                       # its pick itself, the not-yet-moved scripts too
//	fzt-automate --popup --window @3 --client /dev/pty0 [--open tmux] < rows
//	                       # the popup at over a tab, run by tmux's display-palette
//	                       # with its rows on stdin: runs picks itself (popup.go)
//	fzt-automate <built-in> [arguments]
//	                       # one of the at commands it runs itself (builtins.go),
//	                       # as a pick would: `fzt-automate lights off 30`
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	frontend "github.com/romaine-life/fzt-frontend"
	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
	"github.com/romaine-life/fzt/render"
)

// configDir returns the directory holding fzt-automate's menu cache and state.
// Honors $FZT_CONFIG_DIR if set, otherwise uses the OS convention.
//
// On Windows, uses %USERPROFILE%\.fzt-automate rather than %LOCALAPPDATA%
// because Windows Terminal (a Store-packaged app) virtualizes LOCALAPPDATA
// for its child processes — files placed there by non-WT processes aren't
// visible to WT-spawned shells. USERPROFILE isn't virtualized.
func configDir() string {
	if d := os.Getenv("FZT_CONFIG_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("USERPROFILE"), ".fzt-automate")
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "fzt-automate")
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "fzt-automate")
}

func main() {
	if render.Version == "UNSET" {
		fmt.Fprintln(os.Stderr, "fzt-automate: version not set — build with 'go run ./build' (not 'go build .')")
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		if b, ok := builtins[os.Args[1]]; ok {
			os.Exit(runSubcommand(b, os.Args[2:]))
		}
		if os.Args[1] == "--help" || os.Args[1] == "-h" {
			fmt.Print("fzt-automate: the at menu. Its built-in commands:\n\n" + builtinList())
			os.Exit(0)
		}
	}

	title := "What would you like to do?"
	header := "Name\tDescription"
	syncOnly := false
	popup := false
	runAll := false // --run: the one-shot menu runs every pick itself, scripts and URLs too
	var window, client, open string

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--title":
			if i+1 < len(args) {
				title = args[i+1]
				i++
			}
		case "--header":
			if i+1 < len(args) {
				header = args[i+1]
				i++
			}
		case "--sync":
			syncOnly = true
		case "--popup":
			popup = true
		case "--run":
			runAll = true
		case "--window", "--client", "--open":
			if i+1 < len(args) {
				switch args[i] {
				case "--window":
					window = args[i+1]
				case "--client":
					client = args[i+1]
				case "--open":
					open = args[i+1]
				}
				i++
			}
		case "--version":
			fmt.Println(render.Version)
			os.Exit(0)
		}
	}

	cfgDir := configDir()
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "fzt-automate: creating config dir %s: %v\n", cfgDir, err)
		os.Exit(1)
	}
	cacheFile := filepath.Join(cfgDir, "menu-cache.yaml")

	if syncOnly {
		n, version, err := frontend.SyncMenu(cfgDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fzt-automate: sync: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("synced %d items, menu version %d\n", n, version)
		os.Exit(0)
	}

	var tmuxRows []string
	if popup {
		rows, err := readTmuxRows(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fzt-automate: reading tmux's rows: %v\n", err)
			os.Exit(1)
		}
		tmuxRows = rows
	}
	if popup || runAll {
		// Started by tmux, not a shell: the at commands not yet moved off
		// PowerShell reach PATH through AT_SCRIPTS, which tmux holds (set-fzt.ps1).
		if dir := os.Getenv("AT_SCRIPTS"); dir != "" {
			os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
	}

	folders := &commandFolders{}
	load := func() ([]core.Item, error) { return loadMenu(cacheFile, header, tmuxRows, folders) }
	items, err := load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fzt-automate: %v\n", err)
		os.Exit(1)
	}

	// Identity for whoami display is the device's registered name: opening the
	// menu never waits on the network.
	identity := frontend.SignedInEmail(cfgDir)

	menuVersion := readMenuVersion(cfgDir)

	cfg := tui.Config{
		Layout:             "reverse",
		Border:             true,
		Tiered:             true,
		DepthPenalty:       5,
		HeaderLines:        1,
		Nth:                []int{1},
		AcceptNth:          []int{1},
		Title:              title,
		TreeMode:           true,
		EnvTags:            []string{"terminal"},
		FrontendName:       "automate",
		FrontendVersion:    render.Version,
		InitialDisplay:     identity,
		Provider:           folders,
		ConfigDir:          cfgDir,
		InitialMenuVersion: menuVersion,
		UpdateRepo:         "romaine-life/fzt-automate",
		UpdateAssetPrefix:  "fzt-automate",
		UpdateBinaryName:   "fzt-automate",
		FrontendCommands: []core.CommandItem{
			{Name: "unload", Description: "Clear local menu cache", Action: "unload"},
			{Name: "sync", Description: "Sync menu from cloud", Action: "sync"},
			{Name: "save", Description: "Save changes to cloud", Action: "save"},
			frontend.EditCommands(),
			{Name: "update", Description: "Update fzt-automate to latest release", Action: "update"},
			{Name: "states", Description: "Toggle state inspector banner (suppresses action execution)", Action: "toggle-states"},
			{Name: "shortcuts", Description: "Keyboard shortcuts", Children: []core.CommandItem{
				{Name: "shift", Description: "modifier key (all shortcuts)"},
				{Name: "shift+enter", Description: "confirm action"},
				{Name: "shift+back", Description: "return to home"},
				{Name: "S", Description: "sync menu from cloud"},
				{Name: "W", Description: "save changes to cloud"},
				{Name: "A", Description: "add item after cursor"},
				{Name: "F", Description: "create folder at cursor"},
				{Name: "R", Description: "edit item properties"},
				{Name: "I", Description: "edit item properties"},
				{Name: "D", Description: "delete highlighted item"},
				{Name: "H", Description: "navigate left (vim)"},
				{Name: "J", Description: "navigate down (vim)"},
				{Name: "K", Description: "navigate up (vim)"},
				{Name: "L", Description: "navigate right (vim)"},
			}},
		},
	}

	if popup {
		if open != "" {
			cfg.StartFolder = []string{open}
		}
		m := &popupMenu{cfgDir: cfgDir, window: window, client: client, out: os.Stdout, load: func() ([]core.Item, int, error) {
			items, err := load()
			return items, readMenuVersion(cfgDir), err
		}}
		if err := runPopup(m, items, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "fzt-automate: %v\n", err)
			os.Exit(1)
		}
		return
	}

	pick, err := tui.Run(items, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fzt-automate: %v\n", err)
		os.Exit(1)
	}

	switch {
	case pick.Kind == "cancel":
		os.Exit(130)
	case pick.Kind == "command" && pick.Output == "unloaded":
		fmt.Fprintln(os.Stderr, "menu cache cleared")
		tui.PauseIfInteractive()
		os.Exit(130)
	case pick.Kind == "command" && pick.Output == "synced":
		fmt.Fprintln(os.Stderr, "synced — reopen to see menu")
		tui.PauseIfInteractive()
		os.Exit(130)
	}

	line := pickLine(pick)
	isURL := pick.Item.Action != nil && pick.Item.Action.Type == "url"
	switch {
	case runAll && isURL:
		if err := openURL(line); err != nil {
			fmt.Fprintf(os.Stderr, "fzt-automate: %v\n", err)
			os.Exit(1)
		}
		return
	case !isURL && line != "":
		if code, ran := runHere(line, runAll); ran {
			os.Exit(code)
		}
	}
	fmt.Println(line)
}

// runHere runs a one-shot pick in this terminal when it is a built-in or a
// program, and an at command not yet moved off PowerShell too when scripts is
// set (--run); otherwise that is printed for the at wrapper, as before.
func runHere(line string, scripts bool) (int, bool) {
	s, err := resolve(line)
	if err != nil {
		fmt.Fprintln(os.Stderr, logFailure("", line, "", fmt.Sprintf("fzt-automate: %v", err)))
		return 1, true
	}
	var cmd *exec.Cmd
	switch s.kind {
	case stepBuiltin:
		return runSubcommand(s.b, s.args), true
	case stepScript:
		if !scripts {
			return 0, false
		}
		cmd = scriptCommand(s.line)
		cmd.Env = append(os.Environ(), "AT_PICK=1")
	case stepProgram:
		if cmd, err = programCommand(s.args); err != nil {
			fmt.Fprintln(os.Stderr, logFailure("", line, "", fmt.Sprintf("fzt-automate: %v", err)))
			return 1, true
		}
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		message := logFailure("", line, cmd.Dir, fmt.Sprintf("fzt-automate: %v", err))
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if strings.Contains(message, "\nCouldn't save failure log:") {
				fmt.Fprintln(os.Stderr, message)
			}
			return exit.ExitCode(), true
		}
		fmt.Fprintln(os.Stderr, message)
		return 1, true
	}
	return 0, true
}

// pickLine is the line the at wrapper runs for a pick: the picked item's
// action (a URL opens in the browser; a command survives renames), else what
// the picker printed for it.
func pickLine(pick tui.Pick) string {
	if pick.Kind == "input" && pick.Item.Input != "" {
		// An entry that declares an input runs with the typed text as its argument.
		_, line := classify(pick.Item, pick.Text, true)
		return line
	}
	if pick.Kind == "select" && pick.Item.Action != nil {
		return pick.Item.Action.Target
	}
	return pick.Output
}

// loadMenu reads the menu cache (none yet is an empty menu), adds tmux's rows
// as the tmux folder, points the command-folder provider at it and puts the
// header row in front.
func loadMenu(cacheFile, header string, tmuxRows []string, folders *commandFolders) ([]core.Item, error) {
	var items []core.Item
	if _, err := os.Stat(cacheFile); err == nil {
		if items, err = core.LoadYAML(cacheFile); err != nil {
			return nil, err
		}
	}
	items = graftTmux(items, tmuxRows)
	// Before the header goes in front: ParentIdx counts from the first menu item.
	folders.reset(items)
	if header != "" {
		items = append([]core.Item{{Fields: strings.Split(header, "\t"), Depth: -1}}, items...)
	}
	return items, nil
}

// readMenuVersion is the menu version saved by the last sync, for conflict
// detection on save.
func readMenuVersion(cfgDir string) int {
	data, err := os.ReadFile(filepath.Join(cfgDir, ".menu-version"))
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return v
}
