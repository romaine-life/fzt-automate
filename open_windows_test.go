//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// The menu runs in a console tmux closes with it, and a program that joined
// that console ends then (FZTA-19). Opened directly, the test program joins
// this test's console; opened as launch opens it, it can't.
func TestLaunchedProgramsDontJoinTheMenusConsole(t *testing.T) {
	// Access denied means this test already has one.
	if r, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("AllocConsole").Call(); r == 0 && err != windows.ERROR_ACCESS_DENIED {
		t.Fatalf("this test needs a console to share: %v", err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "attach-console.exe")
	if out, err := exec.Command("go", "build", "-ldflags=-H=windowsgui", "-o", exe, "./testdata/attach-console").CombinedOutput(); err != nil {
		t.Fatalf("building the test program: %v\n%s", err, out)
	}
	t.Setenv("FZT_TEST_HELPER", "1")

	run := func(how string, open func(args []string) error) string {
		t.Helper()
		result := filepath.Join(dir, how+".txt")
		event := fmt.Sprintf(`Local\fzta-attach-console-%d-%s`, os.Getpid(), how)
		ev, err := windows.CreateEvent(nil, 1, 0, windows.StringToUTF16Ptr(event))
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(ev)
		if err := open([]string{result, event}); err != nil {
			t.Fatalf("%s: %v", how, err)
		}
		if s, _ := windows.WaitForSingleObject(ev, 30000); s != windows.WAIT_OBJECT_0 {
			t.Fatalf("%s: the test program didn't report within 30 s", how)
		}
		got, err := os.ReadFile(result)
		if err != nil {
			t.Fatal(err)
		}
		return string(got)
	}

	direct := run("direct", func(args []string) error {
		return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(exe), windows.StringToUTF16Ptr(commandLine(args)), nil, windows.SW_SHOWNORMAL)
	})
	if direct != "attached" {
		t.Fatalf("opened directly, the test program should join this console, so the test can see the difference: %s", direct)
	}
	launched := run("launched", func(args []string) error { return startProgram(exe, args) })
	if !strings.HasPrefix(launched, "not attached") {
		t.Fatalf("a program launch started joined the menu's console: %s", launched)
	}
}

func TestShellOpenReportsWhatTheShellRefused(t *testing.T) {
	t.Setenv("FZT_TEST_HELPER", "1")
	missing := filepath.Join(t.TempDir(), "missing.exe")
	err := startProgram(missing, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot find") {
		t.Fatalf("got %v, want the shell's own reason", err)
	}
}
