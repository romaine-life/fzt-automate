//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandLineQuotesWhatNeedsIt(t *testing.T) {
	got := commandLine([]string{"--script", `C:\Program Files\at-home\power-lines\current\strings.lua`, "-C", "savegamePath=power-lines", `a"b`})
	want := `--script "C:\Program Files\at-home\power-lines\current\strings.lua" -C savegamePath=power-lines a\"b`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestFinalPathFollowsAFolderLink(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "b9178")
	if err := os.Mkdir(release, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(release, "mGBA.exe")
	if err := os.WriteFile(exe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(dir, "current")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", current, release).CombinedOutput(); err != nil {
		t.Fatalf("making the junction: %v %s", err, out)
	}
	got := finalPath(filepath.Join(current, "mGBA.exe"))
	if !strings.EqualFold(got, finalPath(exe)) || strings.Contains(strings.ToLower(got), `\current\`) {
		t.Fatalf("got %s, want the release folder's %s", got, finalPath(exe))
	}
	if missing := filepath.Join(dir, "gone.exe"); finalPath(missing) != missing {
		t.Fatal("a path that can't be read stays as given")
	}
}
