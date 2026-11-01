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
//	fzt-automate --sync    # pull the menu into the cache, without the picker
//	fzt-automate --popup --window @3 --client /dev/pty0 [--open tmux] < rows
//	                       # the popup at over a tab, run by tmux's display-palette
//	                       # with its rows on stdin: runs picks itself (popup.go)
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/romaine-life/fzt/core"
	"github.com/romaine-life/fzt/render"
	frontend "github.com/romaine-life/fzt-frontend"
	"github.com/romaine-life/fzt-terminal/tui"
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

	title := "What would you like to do?"
	header := "Name\tDescription"
	syncOnly := false
	popup := false
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
		// The popup runs in tmux's environment, not a shell's: the at commands
		// reach PATH through AT_SCRIPTS, which tmux holds (set-fzt.ps1).
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

	// Identity for whoami display comes from the auth.romaine.life token's
	// email claim. There is no local .identity file anymore — the loaded
	// token (written by `authromaine`) is the single source of identity.
	identity := ""
	if token, err := frontend.ReadAuthToken(cfgDir); err == nil {
		identity = frontend.EmailFromToken(token)
	}

	menuVersion := readMenuVersion(cfgDir)

	cfg := tui.Config{
		Layout:          "reverse",
		Border:          true,
		Tiered:          true,
		DepthPenalty:    5,
		HeaderLines:     1,
		Nth:             []int{1},
		AcceptNth:       []int{1},
		Title:           title,
		TreeMode:        true,
		EnvTags:         []string{"terminal"},
		FrontendName:    "automate",
		FrontendVersion: render.Version,
		InitialDisplay:  identity,
		Provider:        folders,
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

	fmt.Println(pickLine(pick))
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
