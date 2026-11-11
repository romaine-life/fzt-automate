package main

import (
	"strings"
	"testing"
)

// A pick in the --run tab becomes the pane's program, started as the popup's
// takeover would start it, so the pane's start command is the pick's
// (FZTA-12: reload-app reran the menu).
func TestRunTabBecomesItsPick(t *testing.T) {
	program := step{kind: stepProgram, args: []string{"recorder.exe", "--x"}}
	args := respawnPickArgs("%7", `D:\repos`, program)
	want := append([]string{"respawn-pane", "-k", "-t", "%7", "-c", `D:\repos`}, tmuxProgram(takeoverProgram(program))...)
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("program: %q, want %q", args, want)
	}

	testBuiltin(t, &builtin{name: "zz-term", terminal: true})
	b, _ := resolve("zz-term x")
	if got := strings.Join(respawnPickArgs("%7", "", b), " "); !strings.HasPrefix(got, "respawn-pane -k -t %7 ") || !strings.Contains(got, "zz-term x") || strings.Contains(got, "-c") {
		t.Fatalf("built-in: %q", got)
	}
}

// An old command is marked on its pane, as its takeover tab is, before the
// pane restarts as it.
func TestRunTabMarksAnOldCommand(t *testing.T) {
	args := respawnPickArgs("%7", `D:\repos`, step{kind: stepScript, line: "gitprune"})
	got := strings.Join(args, " ")
	if !strings.HasPrefix(got, "set-option -p -t %7 @session-set-shell 1 ; respawn-pane -k -t %7 ") ||
		!strings.Contains(got, "-e AT_PICK=1") || !strings.Contains(got, "-NoExit") {
		t.Fatalf("old command: %q", got)
	}
	if got := strings.Join(respawnPickArgs("%7", "", step{kind: stepProgram, args: []string{"spt"}}), " "); strings.Contains(got, "session-set-shell") {
		t.Fatalf("a program is marked: %q", got)
	}
}
