package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// stepKind is what an entry's action line names.
type stepKind int

const (
	stepBuiltin stepKind = iota // run inside fzt-automate
	stepProgram                 // a program and its arguments, started directly
	stepScript                  // an at command not yet moved off PowerShell (FZT-71)
)

// step is an action line resolved to what runs.
type step struct {
	kind stepKind
	b    *builtin
	args []string // the built-in's arguments, or the program and its arguments
	line string   // the line itself, for a script
	dir  string   // the folder it runs in, when known (tabdir.go)
}

// resolve reads an action line. Its first word is a built-in, else an at
// command still in the at release (a .ps1 in AT_SCRIPTS, run as before until
// it moves), else a program: tool:, tool-config: and ${NAME} words are
// resolved, and the program is found on PATH.
func resolve(line string) (step, error) {
	args, err := splitArgs(line)
	if err != nil {
		return step{}, err
	}
	if len(args) == 0 {
		return step{}, errors.New("the action is empty")
	}
	if b, ok := builtins[args[0]]; ok {
		return step{kind: stepBuiltin, b: b, args: args[1:]}, nil
	}
	if isScript(args[0]) {
		return step{kind: stepScript, line: line}, nil
	}
	args, err = expandArgs(args)
	if err != nil {
		return step{}, err
	}
	return step{kind: stepProgram, args: args}, nil
}

// isScript says whether name is a command in the at release on AT_SCRIPTS.
func isScript(name string) bool {
	dir := os.Getenv("AT_SCRIPTS")
	if dir == "" || filepath.Base(name) != name {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, name+".ps1"))
	return err == nil
}

// programCommand starts a program with no shell. Its name is looked up on PATH
// when it isn't a path.
func programCommand(args []string) (*exec.Cmd, error) {
	path, err := exec.LookPath(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s isn't installed here (not on PATH)", args[0])
	}
	return exec.Command(path, args[1:]...), nil
}

// scriptCommand runs an at command not yet moved off PowerShell, as before.
func scriptCommand(line string) *exec.Cmd {
	if runtime.GOOS != "windows" {
		return exec.Command("sh", "-c", line)
	}
	return exec.Command(powershell(), "-NoProfile", "-NonInteractive", "-Command", line)
}

// powershell is pwsh where installed, else Windows PowerShell.
func powershell() string {
	if _, err := exec.LookPath("pwsh"); err == nil {
		return "pwsh"
	}
	return "powershell"
}

// selfProgram is this fzt-automate, which a takeover of a terminal built-in runs.
func selfProgram() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "fzt-automate"
}
