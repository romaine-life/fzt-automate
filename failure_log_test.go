package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/romaine-life/fzt/core"
)

func failureHistory(t *testing.T, dir string) []map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "failures", "*.log*"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var entry map[string]string
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("invalid failure record in %s: %v", path, err)
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

func TestPopupFailuresLeaveHistory(t *testing.T) {
	dir := t.TempDir()
	m := &popupMenu{cfgDir: dir, overruns: map[string]bool{}}
	live := &fakeLive{}
	pick(t, m, live, cmdItem("missing", "fzt-test-no-such-program", true))
	pick(t, m, live, cmdItem("bad exit", helperLine(t, "exit", "3"), true))
	m.runTakeover("missing context", step{kind: stepProgram, args: []string{"x"}}, live)
	entries := failureHistory(t, dir)
	if len(entries) != 3 {
		t.Fatalf("got %d records, want 3: %+v", len(entries), entries)
	}
	for i, want := range []string{"isn't installed here", "exit 3", "which tab"} {
		if !strings.Contains(entries[i]["Error"], want) {
			t.Fatalf("record %d = %+v, want %q", i, entries[i], want)
		}
	}
	if live.closed || live.s.Panel.Style != core.PanelFailed {
		t.Fatal("logging changed the failure display or closed the menu")
	}
}

func TestFailureHistorySurvivesAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"first program failed", "second program failed"} {
		cmd := exec.Command(exe, "failure-log", reason)
		cmd.Env = append(os.Environ(), "FZT_TEST_HELPER=1", "FZT_CONFIG_DIR="+dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("logging process: %v: %s", err, out)
		}
	}
	entries := failureHistory(t, dir)
	if len(entries) != 2 || entries[0]["Error"] == entries[1]["Error"] {
		t.Fatalf("history was lost or overwritten: %+v", entries)
	}
}

func TestTakeoverCannotStartTmuxKeepsErrorAndLogs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	m := &popupMenu{cfgDir: dir, window: "@123", client: "not-a-real-client"}
	live := &fakeLive{}
	m.runTakeover("program", step{kind: stepProgram, args: []string{"missing"}}, live)
	if live.closed || live.s.Frozen || !strings.Contains(live.s.Panel.Lines[0], "didn't start") {
		t.Fatalf("error did not stay in the output box: %+v", live.s)
	}
	entries := failureHistory(t, dir)
	if len(entries) != 1 || !strings.Contains(entries[0]["Error"], "didn't start") {
		t.Fatalf("failure to start tmux was not logged: %+v", entries)
	}
}

func TestFailureHistoryRotatesAndExpires(t *testing.T) {
	dir := t.TempDir()
	logs := filepath.Join(dir, "failures")
	if err := os.Mkdir(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(logs, "at-999999.log")
	if err := os.WriteFile(old, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(old, before, before); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if got := logFailure(dir, "a\nprogram", "folder", strings.Repeat("x", 65536)); strings.Contains(got, "Couldn't save") {
			t.Fatal(got)
		}
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("expired failure log remains: %v", err)
	}
	paths, _ := filepath.Glob(filepath.Join(logs, "*.log*"))
	if len(paths) != 2 {
		t.Fatalf("got %d rolling files, want 2", len(paths))
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.Size() > failureLogBytes {
			t.Fatalf("unbounded failure log: %v, %v", info, err)
		}
	}
	failureHistory(t, dir)
}

func TestFailureLogWriteErrorIsReported(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := logFailure(dir, "program", "", "original error")
	if !strings.HasPrefix(got, "original error\nCouldn't save failure log:") {
		t.Fatalf("lost original error or logging failure: %s", got)
	}
}
