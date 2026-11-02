//go:build windows

package main

import "golang.org/x/sys/windows"

// tmuxProgram is a program and its arguments as new-window takes them. The
// Windows build joins two or more arguments with spaces into the command line
// it starts (spawn.c), so each is quoted the way CreateProcess splits them.
// One argument would go to default-shell instead, so a lone program gets an
// empty second one, which adds only a space to the line.
func tmuxProgram(argv []string) []string {
	out := make([]string, 0, len(argv)+1)
	for _, a := range argv {
		out = append(out, windows.EscapeArg(a))
	}
	if len(out) == 1 {
		out = append(out, "")
	}
	return out
}
