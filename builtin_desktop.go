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
	register(&builtin{name: "programs", about: "folder: every program in Windows' installed-apps list", rows: programRows})
	register(&builtin{name: "installed-app", usage: "<name> <appid>", about: "switch to an app from Windows' installed-apps list, or open it (the programs folder's rows)", run: runInstalledApp})
	register(&builtin{name: "launch", usage: "<name> <program> [arguments...]", about: "switch to a program's window, or start it: a tool:, a path or a link", run: runLaunch})
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
			rows = append(rows, folderRow{name: unique(a.name), action: joinArgs("installed-app", a.name, a.appID)})
		}
	}
	return rows, nil
}

// runLaunch switches to the program's newest window, or starts it with its
// arguments and returns at once, so a desktop app holds no tab and the menu
// isn't left waiting for it to close. The program is a path (tool: words
// arrive resolved) or a link such as steam://. Some programs run under another
// than the one started (Discord's updater, a Steam game), so a window also
// matches by its title or its program's own description.
func runLaunch(_ context.Context, _ io.Writer, args []string) (string, error) {
	if len(args) < 2 {
		return "", usageError(builtins["launch"])
	}
	name, program := args[0], args[1]
	ws, err := listWindows()
	if err != nil {
		return "", err
	}
	if w := windowFor(ws, name, []string{program, finalPath(program)}, fileDescription); w != nil {
		if !focusWindow(w.handle) {
			return "", fmt.Errorf("%s is open, but Windows refused to switch to it", name)
		}
		return "", nil
	}
	if err := startProgram(program, args[2:]); err != nil {
		return "", fmt.Errorf("%s didn't start: %w", name, err)
	}
	return "started " + name, nil
}

// windowFor is the newest window of a program: one its own process shows, else
// one titled name, or whose program describes itself as name. paths are the
// program as given and as Windows names it once folder links are followed (a
// home-sync tool's current folder is a link to its release's folder).
func windowFor(ws []desktopWindow, name string, paths []string, describe func(string) string) *desktopWindow {
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			continue
		}
		if w := findWindow(ws, func(w desktopWindow) bool { return strings.EqualFold(w.processPath, p) }); w != nil {
			return w
		}
	}
	return findWindow(ws, func(w desktopWindow) bool {
		return w.title == name || (w.processPath != "" && describe(w.processPath) == name)
	})
}

// runInstalledApp switches to an app from Windows' installed-apps list, or
// opens it. Only the programs folder, which lists those apps, uses it. The
// surest match first: many desktop apps give their windows no AppID.
func runInstalledApp(_ context.Context, _ io.Writer, args []string) (string, error) {
	if len(args) != 2 {
		return "", usageError(builtins["installed-app"])
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
