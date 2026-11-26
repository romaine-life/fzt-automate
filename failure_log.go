package main

import (
	"os"
	"path/filepath"

	"github.com/romaine-life/logline"
)

// failureLogName is the failure log's file in the config directory: one file every
// fzt-automate appends to, written as the estate's standard line (logline, AHT-204).
// logline opens it for each line, so simultaneous menus keep every line whole, and
// moves it to <name>.old past 1 MiB.
const failureLogName = "fzt-automate.log"

// logFailure records a failed launch or command in dir's failure log (the config
// directory when dir is empty) and returns message for the caller to show. name is
// the entry, cwd the folder it ran in (this process's when empty). A log that can't be
// written gives way to the failure, which the caller shows either way.
func logFailure(dir, name, cwd, message string) string {
	if dir == "" {
		dir = configDir()
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	logline.OpenFile(filepath.Join(dir, failureLogName)).Error("command failed",
		"name", bound(name, 16*1024), "directory", bound(cwd, 16*1024), "error", bound(message, 64*1024))
	return message
}

// bound cuts text past n bytes: an action line can be much larger than its label.
func bound(text string, n int) string {
	if len(text) > n {
		return text[:n] + " [truncated]"
	}
	return text
}
