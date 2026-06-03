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

	var items []core.Item
	if _, err := os.Stat(cacheFile); err == nil {
		items, err = core.LoadYAML(cacheFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fzt-automate: %v\n", err)
			os.Exit(1)
		}
	}

	// Identity for whoami display comes from the auth.romaine.life token's
	// email claim. There is no local .identity file anymore — the loaded
	// token (written by `authromaine`) is the single source of identity.
	identity := ""
	if token, err := frontend.ReadAuthToken(cfgDir); err == nil {
		identity = frontend.EmailFromToken(token)
	}

	// Read persisted menu version for conflict detection on save
	menuVersion := 0
	versionFile := filepath.Join(cfgDir, ".menu-version")
	if data, err := os.ReadFile(versionFile); err == nil {
		menuVersion, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}

	if header != "" {
		headerFields := strings.Split(header, "\t")
		headerItem := core.Item{Fields: headerFields, Depth: -1}
		items = append([]core.Item{headerItem}, items...)
	}

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
		ConfigDir:          cfgDir,
		InitialMenuVersion: menuVersion,
		UpdateRepo:         "romaine-life/fzt-automate",
		UpdateAssetPrefix:  "fzt-automate",
		UpdateBinaryName:   "fzt-automate",
		FrontendCommands: []core.CommandItem{
			{Name: "unload", Description: "Clear local menu cache", Action: "unload"},
			{Name: "sync", Description: "Sync menu from cloud", Action: "sync"},
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

	result, err := tui.Run(items, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fzt-automate: %v\n", err)
		os.Exit(1)
	}

	if result == "" {
		os.Exit(130)
	}

	if result == "unloaded" {
		fmt.Fprintln(os.Stderr, "menu cache cleared")
		tui.PauseIfInteractive()
		os.Exit(130)
	}

	if result == "synced" {
		fmt.Fprintln(os.Stderr, "synced — reopen to see menu")
		tui.PauseIfInteractive()
		os.Exit(130)
	}

	// Look up the selected item to check for action overrides.
	// URL action = bookmark (open in browser). Command action = stable identifier (survives renames).
	for _, item := range items {
		if len(item.Fields) > 0 && item.Fields[0] == result {
			if item.Action != nil {
				fmt.Println(item.Action.Target)
				os.Exit(0)
			}
			break
		}
	}

	fmt.Println(result)
}
