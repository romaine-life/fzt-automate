package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// system:<name> names an app and the program it runs.
func TestResolveSystemApp(t *testing.T) {
	t.Setenv("FZT_TEST_VAR", `C:\Program Files`)
	s, err := resolve(`system:spotify "${FZT_TEST_VAR}\spt.exe" -c 'a b'`)
	if err != nil || s.kind != stepProgram || s.app != "spotify" || strings.Join(s.args, "|") != `C:\Program Files\spt.exe|-c|a b` {
		t.Fatalf("system app: %+v %v", s, err)
	}
	if s, err = resolve("spt.exe"); err != nil || s.app != "" {
		t.Fatalf("a plain program is no app: %+v %v", s, err)
	}
}

// A system app runs a program; nothing else has one window to keep.
func TestSystemAppRunsAProgram(t *testing.T) {
	testBuiltin(t, &builtin{name: "zz-app", terminal: true, run: func(context.Context, io.Writer, []string) (string, error) { return "", nil }})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "oldcmd.ps1"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AT_SCRIPTS", dir)
	for line, want := range map[string]string{
		"system:x zz-app": "a built-in",
		"system:x oldcmd": "not yet moved",
		"system:x":        "empty",
	} {
		if _, err := resolve(line); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", line, err, want)
		}
	}
}

// The pick goes to the app's one window in the system session, whatever the
// entry says about quick, and stays there when its program ends.
func TestSystemAppOpensItsWindow(t *testing.T) {
	s, err := resolve("system:spotify spotify_player.exe -c cfg")
	if err != nil {
		t.Fatal(err)
	}
	if !isTakeover(s, cmdItem("spotify", "system:spotify spotify_player.exe -c cfg", true)) {
		t.Fatal("a quick system app ran in the panel")
	}
	s.dir = `D:\repos\somewhere`
	args, err := takeoverArgs("@3", "/dev/pty0", s)
	if err != nil {
		t.Fatal(err)
	}
	want := "new-window -d -M spotify -t system: -o remain-on-exit=relaunch -W /dev/pty0 spotify_player.exe -c cfg"
	if got := strings.Join(args, " "); got != want {
		t.Fatalf("takeover = %q, want %q", got, want)
	}
}
