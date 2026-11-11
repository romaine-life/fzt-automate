package main

import (
	"path/filepath"
	"testing"
)

func TestLaunchFindsTheProgramsOwnWindowFirst(t *testing.T) {
	lab := filepath.Join(t.TempDir(), "string-lab.exe")
	tools := t.TempDir()
	release := filepath.Join(tools, "mgba", "b9178", "mGBA.exe")
	describe := func(path string) string {
		if path == `C:\Discord\app-1\Discord.exe` {
			return "Discord"
		}
		return ""
	}
	ws := []desktopWindow{
		{handle: 1, title: "String Lab", processPath: `C:\elsewhere\other.exe`},
		{handle: 2, title: "String Lab", processPath: lab},
		{handle: 3, title: "#general - Discord", processPath: `C:\Discord\app-1\Discord.exe`},
		{handle: 4, title: "Slay the Spire 2", processPath: `C:\Steam\game.exe`},
		{handle: 5, title: "mGBA - POWER LINES", processPath: release},
	}

	if w := windowFor(ws, "String Lab", []string{lab}, describe); w == nil || w.handle != 2 {
		t.Fatalf("the lab's own window first, got %+v", w)
	}
	// A home-sync tool is started through its current folder, a link to the
	// release's folder, which is the path Windows reports for the running program.
	paths := []string{filepath.Join(tools, "mgba", "current", "mGBA.exe"), release}
	if w := windowFor(ws, "Power Lines", paths, describe); w == nil || w.handle != 5 {
		t.Fatalf("mGBA by the path behind its current folder, got %+v", w)
	}
	// Discord runs under another program than its updater, so it's found by description.
	if w := windowFor(ws, "Discord", []string{`C:\Discord\Update.exe`}, describe); w == nil || w.handle != 3 {
		t.Fatalf("Discord by its description, got %+v", w)
	}
	// A link has no process of its own: the title finds the game.
	if w := windowFor(ws, "Slay the Spire 2", []string{"steam://rungameid/2868840"}, describe); w == nil || w.handle != 4 {
		t.Fatalf("the game by its title, got %+v", w)
	}
	if w := windowFor(ws, "Power Lines", []string{filepath.Join(t.TempDir(), "mGBA.exe")}, describe); w != nil {
		t.Fatalf("nothing open matches, got %+v", w)
	}
}

func TestLaunchNeedsANameAndAProgram(t *testing.T) {
	if _, err := runLaunch(nil, nil, []string{"only a name"}); err == nil {
		t.Fatal("a launch with no program must fail")
	}
}
