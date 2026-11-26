package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/romaine-life/fzt/core"
	"github.com/romaine-life/logline"
)

// failureHistory reads dir's failure log and its rolled copy, oldest first, every line
// as the estate's standard: a "command failed" error line, its fields by key.
func failureHistory(t *testing.T, dir string) []map[string]string {
	t.Helper()
	var entries []map[string]string
	path := filepath.Join(dir, failureLogName)
	for _, file := range []string{path + ".old", path} {
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
			e, err := logline.Parse(line)
			if err != nil {
				t.Fatalf("not a standard line in %s: %v: %.200q", file, err, line)
			}
			if e.Level != logline.Error || e.Message != "command failed" {
				t.Fatalf("got %s %q, want an error line \"command failed\"", e.Level, e.Message)
			}
			entry := map[string]string{}
			for _, f := range e.Fields {
				entry[f.Key] = f.Value
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
		if !strings.Contains(entries[i]["error"], want) {
			t.Fatalf("record %d = %+v, want %q", i, entries[i], want)
		}
	}
	if entries[0]["name"] != "missing" {
		t.Fatalf("record 0 = %+v, want the entry's name", entries[0])
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
	if len(entries) != 2 || entries[0]["error"] != "first program failed" || entries[1]["error"] != "second program failed" {
		t.Fatalf("history was lost or overwritten: %+v", entries)
	}
	if entries[0]["name"] != "test program" || entries[0]["directory"] != "test folder" {
		t.Fatalf("fields: %+v", entries[0])
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
	if len(entries) != 1 || !strings.Contains(entries[0]["error"], "didn't start") {
		t.Fatalf("failure to start tmux was not logged: %+v", entries)
	}
}

func TestFailureHistoryStaysOneLineBoundedAndRolls(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		if got := logFailure(dir, "a\nprogram", "folder", strings.Repeat("x", 65537)); len(got) != 65537 {
			t.Fatalf("logFailure changed the message it returns: %d bytes", len(got))
		}
	}
	path := filepath.Join(dir, failureLogName)
	for _, file := range []string{path, path + ".old"} {
		info, err := os.Stat(file)
		if err != nil || info.Size() > logline.Limit+80*1024 {
			t.Fatalf("unbounded failure log: %v, %v", info, err)
		}
	}
	entries := failureHistory(t, dir)
	if len(entries) == 0 || len(entries) >= 40 {
		t.Fatalf("got %d records across the roll", len(entries))
	}
	last := entries[len(entries)-1]
	if last["name"] != "a\nprogram" || !strings.HasSuffix(last["error"], " [truncated]") || len(last["error"]) != 64*1024+len(" [truncated]") {
		t.Fatalf("record not kept whole and bounded: name %q, error %d bytes", last["name"], len(last["error"]))
	}
}

func TestFailureLogThatCantBeWrittenStillReturnsTheError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := logFailure(dir, "program", "", "original error"); got != "original error" {
		t.Fatalf("lost the original error: %q", got)
	}
}
