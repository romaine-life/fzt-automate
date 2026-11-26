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
//
// The log ships to the NAS's pile, which every home PC's everyday NAS sign-in reads,
// and every field can hold text from outside: name can be a whole action line with the
// arguments typed at run time, and message a failed command's output. So each is
// masked with logline.Redact, the estate's one list of credential shapes, before it is
// cut to its bound (masking first, so a cut can't leave part of a credential
// unrecognised). The returned message is the original, for the screen.
func logFailure(dir, name, cwd, message string) string {
	if dir == "" {
		dir = configDir()
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	logline.OpenFile(filepath.Join(dir, failureLogName)).Error("command failed",
		"name", masked(name, 16*1024), "directory", masked(cwd, 16*1024), "error", masked(message, 64*1024))
	return message
}

// masked is text with its credentials masked, cut past n bytes: an action line can be
// much larger than its label.
func masked(text string, n int) string {
	text = logline.Redact(text)
	if len(text) > n {
		return text[:n] + " [truncated]"
	}
	return text
}
