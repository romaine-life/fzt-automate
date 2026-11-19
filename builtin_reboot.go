package main

import (
	"context"
	"os"
	"strings"
)

func init() {
	register(&builtin{name: "reboot", about: "folder: restarting this PC, one pick inside it so no search can start it", rows: rebootRows})
}

// rebootAction restarts the PC the way Start's Restart does: no delay, and an
// app with unsaved work can still hold it at Windows' "restart anyway" screen
// (a /t over 0 would imply /f and close those apps without asking).
const rebootAction = "shutdown /r /t 0"

// rebootRows is the restart's confirmation. The menu's search reaches every
// row of the static tree, so a restart entry there would be one stray Enter
// away; a command folder's rows exist only once it is opened, so restarting
// takes opening "reboot" and then picking its one row.
func rebootRows(ctx context.Context, args []string) ([]folderRow, error) {
	if len(args) != 0 {
		return nil, usageError(builtins["reboot"])
	}
	name, err := os.Hostname()
	if err != nil || name == "" {
		name = "this PC"
	} else {
		name = strings.ToLower(name)
	}
	return []folderRow{{
		name:        "restart now",
		description: "Restart " + name + "; your tmux windows come back when you sign in",
		action:      rebootAction,
	}}, nil
}
