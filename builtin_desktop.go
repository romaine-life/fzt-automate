package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func init() {
	register(&builtin{name: "windows", about: "folder: every open window, on every virtual desktop, most recent first", rows: windowRows})
	register(&builtin{name: "focus", usage: "<window>", about: "switch to a window the windows folder listed", run: runFocus})
	register(&builtin{name: "programs", about: "folder: every program in the Start menu", rows: programRows})
	register(&builtin{name: "launch", usage: "<name> <appid>", about: "switch to a Start-menu program's window, or open it", run: runLaunch})
}

// uniqueNames numbers repeats ("Notepad (2)"): fzt returns the picked name, so
// two rows can't share one.
func uniqueNames() func(string) string {
	seen := map[string]int{}
	return func(name string) string {
		name = strings.ReplaceAll(name, "\t", " ")
		seen[name]++
		if n := seen[name]; n > 1 {
			return fmt.Sprintf("%s (%d)", name, n)
		}
		return name
	}
}

func windowRows(context.Context, []string) ([]folderRow, error) {
	ws, err := listWindows()
	if err != nil {
		return nil, err
	}
	unique := uniqueNames()
	var rows []folderRow
	for _, w := range ws {
		program := fmt.Sprintf("pid %d", w.pid)
		if w.processPath != "" {
			program = strings.TrimSuffix(filepath.Base(w.processPath), filepath.Ext(w.processPath))
		}
		rows = append(rows, folderRow{name: unique(w.title), description: program, action: joinArgs("focus", windowID(w.handle))})
	}
	return rows, nil
}

func runFocus(_ context.Context, _ io.Writer, args []string) (string, error) {
	if len(args) != 1 {
		return "", usageError(builtins["focus"])
	}
	h, err := parseWindowID(args[0])
	if err != nil {
		return "", err
	}
	if !windowExists(h) {
		return "", errors.New("that window has closed since the list was made")
	}
	if !focusWindow(h) {
		return "", errors.New("Windows refused to switch to that window")
	}
	return "", nil
}

// Web links, uninstallers, and the help files and documents some installers
// add to the Start menu are not programs.
var (
	webLink     = regexp.MustCompile(`(?i)^https?://`)
	uninstaller = regexp.MustCompile(`(?i)\\unins[^\\]*\.exe$`)
)

func isProgram(a startApp) bool {
	id := a.appID
	switch {
	case webLink.MatchString(id):
		return false
	case strings.Contains(id, `\`) && !strings.HasSuffix(strings.ToLower(id), ".exe"):
		return false
	case strings.HasPrefix(strings.ToLower(a.name), "uninstall") || uninstaller.MatchString(id):
		return false
	}
	return true
}

func programRows(context.Context, []string) ([]folderRow, error) {
	apps, err := startApps()
	if err != nil {
		return nil, err
	}
	unique := uniqueNames()
	var rows []folderRow
	for _, a := range apps {
		if isProgram(a) {
			rows = append(rows, folderRow{name: unique(a.name), action: joinArgs("launch", a.name, a.appID)})
		}
	}
	return rows, nil
}

// runLaunch switches to the program's newest window, or opens it. The surest
// match first: many desktop apps give their windows no AppID, and some run
// under another program than the Start menu starts (Steam, Discord), so the
// last tries are the program's own description and the window title.
func runLaunch(_ context.Context, _ io.Writer, args []string) (string, error) {
	if len(args) != 2 {
		return "", usageError(builtins["launch"])
	}
	name, appID := args[0], args[1]
	ws, err := listWindows()
	if err != nil {
		return "", err
	}
	w := findWindow(ws, func(w desktopWindow) bool { return w.appID == appID })
	if exe := appIDPath(appID); w == nil && exe != "" {
		w = findWindow(ws, func(w desktopWindow) bool { return strings.EqualFold(w.processPath, exe) })
	}
	if w == nil {
		w = findWindow(ws, func(w desktopWindow) bool {
			return w.title == name || (w.processPath != "" && fileDescription(w.processPath) == name)
		})
	}
	if w == nil {
		if err := openApp(appID); err != nil {
			return "", fmt.Errorf("%s didn't open: %w", name, err)
		}
		return "opened " + name, nil
	}
	if !focusWindow(w.handle) {
		return "", fmt.Errorf("%s is open, but Windows refused to switch to it", name)
	}
	return "", nil
}

func findWindow(ws []desktopWindow, match func(desktopWindow) bool) *desktopWindow {
	for i := range ws {
		if match(ws[i]) {
			return &ws[i]
		}
	}
	return nil
}

func parseWindowID(s string) (windowHandle, error) {
	n, err := strconv.ParseUint(s, 0, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("%q is not a window", s)
	}
	return windowHandle(n), nil
}

func windowID(h windowHandle) string { return strconv.FormatUint(uint64(h), 10) }
