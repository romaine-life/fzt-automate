package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/romaine-life/fzt/core"
)

// fakeLive applies the system at's menu changes to a state, as the menu
// goroutine would.
type fakeLive struct {
	mu     sync.Mutex
	s      core.State
	abort  func()
	reload []core.Item
}

func (f *fakeLive) Update(fn func(*core.State)) { f.mu.Lock(); fn(&f.s); f.mu.Unlock() }
func (f *fakeLive) Reload(items []core.Item)    { f.mu.Lock(); f.reload = items; f.mu.Unlock() }
func (f *fakeLive) OnAbort(fn func())           { f.mu.Lock(); f.abort = fn; f.mu.Unlock() }

// settled waits for the command to finish: the menu unfrozen again.
func (f *fakeLive) settled(t *testing.T) core.State {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		s := f.s
		f.mu.Unlock()
		if !s.Frozen && s.Panel.Style != core.PanelRunning {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the command never finished")
	return core.State{}
}

func cmdItem(name, action string, quick bool) core.Item {
	return core.Item{Fields: []string{name}, Path: "lights › " + name, Quick: quick, Action: &core.ItemAction{Type: "command", Target: action}}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		item  core.Item
		text  string
		input bool
		kind  pickKind
		line  string
	}{
		{"url", core.Item{Fields: []string{"Gmail"}, Action: &core.ItemAction{Type: "url", Target: "https://mail.google.com"}}, "", false, pickURL, "https://mail.google.com"},
		{"launch is quick by kind", cmdItem("chrome", "launch 'Google Chrome' 'Chrome'", false), "", false, pickQuick, "launch 'Google Chrome' 'Chrome'"},
		{"marked quick", cmdItem("lightson", "lightson", true), "", false, pickQuick, "lightson"},
		{"unmarked is a takeover", cmdItem("spt", "spt", false), "", false, pickTakeover, "spt"},
		{"a leaf runs by name", core.Item{Fields: []string{"devctl"}}, "", false, pickTakeover, "devctl"},
		{"a folder runs nothing", core.Item{Fields: []string{"git"}, HasChildren: true}, "", false, pickNone, ""},
		{"a command folder runs nothing", cmdItem("windows", "folder:openwindows", false), "", false, pickNone, ""},
		{"input is one quoted argument", cmdItem("ollamastop", "ollamastop", true), "it's", true, pickQuick, "ollamastop 'it''s'"},
	}
	for _, c := range cases {
		kind, line := classify(c.item, c.text, c.input)
		if kind != c.kind || line != c.line {
			t.Errorf("%s: got %v %q, want %v %q", c.name, kind, line, c.kind, c.line)
		}
	}
}

func echoLines(lines ...string) string {
	if runtime.GOOS == "windows" {
		var parts []string
		for _, l := range lines {
			parts = append(parts, "'"+l+"'")
		}
		return "Write-Output " + strings.Join(parts, ",")
	}
	return "printf '%s\\n' " + strings.Join(lines, " ")
}

func TestQuickRunShowsTheResultAndMarksAnOverrun(t *testing.T) {
	dir := t.TempDir()
	m := &systemMenu{cfgDir: dir, overruns: map[string]bool{}}
	live := &fakeLive{}

	item := cmdItem("lightson", echoLines("one", "two", "three", "four"), true)
	m.runQuick(item, "lightson", item.Action.Target, live)
	s := live.settled(t)
	if s.Panel.Style != core.PanelOK || len(s.Panel.Lines) != 3 || !s.Panel.Clipped {
		t.Fatalf("panel = %+v", s.Panel)
	}
	if !s.Overruns[item.Path] {
		t.Fatal("a four-line result must mark the entry")
	}
	data, _ := os.ReadFile(filepath.Join(dir, overrunsFile))
	var saved map[string]bool
	if json.Unmarshal(data, &saved) != nil || !saved[item.Path] {
		t.Fatalf("overruns.json = %s", data)
	}
	if out, _ := os.ReadFile(filepath.Join(dir, lastOutputFile)); !strings.Contains(string(out), "four") {
		t.Fatalf("last-output.txt lacks the full output: %s", out)
	}

	// A fixed command clears the mark.
	item.Action.Target = echoLines("Living room lights on")
	m.runQuick(item, "lightson", item.Action.Target, live)
	s = live.settled(t)
	if s.Overruns[item.Path] || s.Panel.Lines[0] != "Living room lights on" {
		t.Fatalf("after a short result: overrun %v panel %+v", s.Overruns[item.Path], s.Panel)
	}
}

func TestQuickRunFailureAndSilence(t *testing.T) {
	m := &systemMenu{cfgDir: t.TempDir(), overruns: map[string]bool{}}
	live := &fakeLive{}
	m.runQuick(cmdItem("bad", "exit 3", true), "bad", "exit 3", live)
	if s := live.settled(t); s.Panel.Style != core.PanelFailed || s.Panel.Lines[0] != "bad failed (exit 3)" {
		t.Fatalf("failure panel = %+v", s.Panel)
	}
	quiet := "exit 0"
	m.runQuick(cmdItem("quiet", quiet, true), "quiet", quiet, live)
	if s := live.settled(t); s.Panel.Style != core.PanelOK || s.Panel.Lines[0] != "quiet done" {
		t.Fatalf("silent success panel = %+v", s.Panel)
	}
}

func TestEscapeStopsAQuickRun(t *testing.T) {
	m := &systemMenu{cfgDir: t.TempDir(), overruns: map[string]bool{}}
	live := &fakeLive{}
	slow := "sleep 30"
	if runtime.GOOS == "windows" {
		slow = "Start-Sleep 30"
	}
	m.runQuick(cmdItem("slow", slow, true), "slow", slow, live)
	deadline := time.Now().Add(10 * time.Second)
	for {
		live.mu.Lock()
		abort, frozen := live.abort, live.s.Frozen
		live.mu.Unlock()
		if abort != nil && frozen {
			abort()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the run never froze the menu with an abort set")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s := live.settled(t); s.Panel.Lines[0] != "stopped slow" || s.Panel.Style != core.PanelFailed {
		t.Fatalf("panel = %+v", s.Panel)
	}
}

func TestTakeoverOutsideTmuxSaysSo(t *testing.T) {
	t.Setenv("TMUX_PANE", "")
	m := &systemMenu{cfgDir: t.TempDir(), overruns: map[string]bool{}}
	live := &fakeLive{}
	m.runTakeover("spt", "spt", live)
	if s := live.s; s.Frozen || s.Panel.Style != core.PanelFailed || !strings.Contains(s.Panel.Lines[0], "isn't running in tmux") {
		t.Fatalf("panel = %+v frozen %v", s.Panel, s.Frozen)
	}
}

func TestTakeoverProgramKeepsAFailureOnScreen(t *testing.T) {
	got := strings.Join(takeoverProgram("spt"), " ")
	if !strings.Contains(got, "-NoExit") || !strings.HasSuffix(got, "spt; if ($? -and -not $LASTEXITCODE) { exit }") {
		t.Fatalf("takeover program = %q", got)
	}
}

func TestSyncReloadsTheMenuAndItsVersion(t *testing.T) {
	want := []core.Item{{Fields: []string{"lightsoff"}}}
	m := &systemMenu{load: func() ([]core.Item, int, error) { return want, 42, nil }}
	live := &fakeLive{}
	m.onCommand("synced", live)
	if len(live.reload) != 1 || live.s.MenuVersion != 42 || live.s.Panel.Lines[0] != "menu synced" {
		t.Fatalf("reload %v version %d panel %+v", live.reload, live.s.MenuVersion, live.s.Panel)
	}
}

func TestWithScheme(t *testing.T) {
	for in, want := range map[string]string{
		"photos.google.com":    "https://photos.google.com",
		"https://romaine.life": "https://romaine.life",
		"spotify-ctl:toggle":   "spotify-ctl:toggle",
		"spotify:":             "spotify:",
	} {
		if got := withScheme(in); got != want {
			t.Errorf("withScheme(%q) = %q, want %q", in, got, want)
		}
	}
}
