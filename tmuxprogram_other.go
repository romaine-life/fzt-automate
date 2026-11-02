//go:build !windows

package main

// tmuxProgram is a program and its arguments as new-window takes them: two or
// more are started directly, one goes to default-shell, so a lone program is
// started through env, which runs it with no shell.
func tmuxProgram(argv []string) []string {
	if len(argv) == 1 {
		return []string{"/usr/bin/env", argv[0]}
	}
	return argv
}
