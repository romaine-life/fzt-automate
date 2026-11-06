package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
)

// fakeLive applies the popup's menu changes to a state, as the menu
// goroutine would.
type fakeLive struct {
	mu     sync.Mutex
	s      core.State
	abort  func()
	reload []core.Item
	closed bool
}

func (f *fakeLive) Update(fn func(*core.State)) { f.mu.Lock(); fn(&f.s); f.mu.Unlock() }
func (f *fakeLive) Reload(items []core.Item)    { f.mu.Lock(); f.reload = items; f.mu.Unlock() }
func (f *fakeLive) OnAbort(fn func())           { f.mu.Lock(); f.abort = fn; f.mu.Unlock() }
func (f *fakeLive) Close()                      { f.mu.Lock(); f.closed = true; f.mu.Unlock() }

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

// testBuiltin registers a built-in for one test.
func testBuiltin(t *testing.T, b *builtin) {
	t.Helper()
	register(b)
	t.Cleanup(func() { delete(builtins, b.name) })
}

// pick runs an entry as the popup would and waits for it to finish.
func pick(t *testing.T, m *popupMenu, live *fakeLive, item core.Item) core.State {
	t.Helper()
	m.onPick(tui.Pick{Kind: "select", Item: item}, live)
	return live.settled(t)
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
		{"an action", cmdItem("lightson", "lights on", true), "", false, pickCommand, "lights on"},
		{"a leaf runs by name", core.Item{Fields: []string{"devctl"}}, "", false, pickCommand, "devctl"},
		{"a folder runs nothing", core.Item{Fields: []string{"git"}, HasChildren: true}, "", false, pickNone, ""},
		{"a command folder runs nothing", cmdItem("windows", "folder:windows", false), "", false, pickNone, ""},
		{"input is one quoted argument", cmdItem("ask", "claude -p", false), "it's", true, pickCommand, "claude -p 'it''s'"},
	}
	for _, c := range cases {
		kind, line := classify(c.item, c.text, c.input)
		if kind != c.kind || line != c.line {
			t.Errorf("%s: got %v %q, want %v %q", c.name, kind, line, c.kind, c.line)
		}
	}
}

func TestResolve(t *testing.T) {
	testBuiltin(t, &builtin{name: "zz-test", run: func(context.Context, io.Writer, []string) (string, error) { return "", nil }})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "oldcmd.ps1"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AT_SCRIPTS", dir)
	t.Setenv("FZT_TEST_VAR", `C:\Program Files`)

	s, err := resolve("zz-test 'two words' x")
	if err != nil || s.kind != stepBuiltin || s.b.name != "zz-test" || strings.Join(s.args, "|") != "two words|x" {
		t.Fatalf("built-in: %+v %v", s, err)
	}
	if s, err = resolve("oldcmd -Minutes 5"); err != nil || s.kind != stepScript || s.line != "oldcmd -Minutes 5" {
		t.Fatalf("script: %+v %v", s, err)
	}
	if s, err = resolve(`"${FZT_TEST_VAR}\app.exe" --goto 'a b'`); err != nil || s.kind != stepProgram || strings.Join(s.args, "|") != `C:\Program Files\app.exe|--goto|a b` {
		t.Fatalf("program: %+v %v", s, err)
	}
	if _, err = resolve("${FZT_TEST_UNSET_VAR}\\x.exe"); err == nil || !strings.Contains(err.Error(), "FZT_TEST_UNSET_VAR is not set") {
		t.Fatalf("an unset variable must fail, got %v", err)
	}
	if _, err = resolve("app 'unclosed"); err == nil {
		t.Fatal("an unclosed quote must fail")
	}
}

func TestSplitArgsRoundTrips(t *testing.T) {
	for _, args := range [][]string{
		{"launch", "Slay the Spire 2", "steam://rungameid/2868840"},
		{"launch", "Assassin's Creed", `{6D809377-6AF0-444B-8957-A3773F02200E}\RustDesk\rustdesk.exe`},
		{"focus", "123"},
		{"x", ""},
	} {
		got, err := splitArgs(joinArgs(args...))
		if err != nil || strings.Join(got, "|") != strings.Join(args, "|") || len(got) != len(args) {
			t.Errorf("%q: got %q %v", args, got, err)
		}
	}
	// The menu's lines from before FZT-71 read the same way.
	got, _ := splitArgs(`launch 'Google Chrome' 'Chrome'`)
	if strings.Join(got, "|") != "launch|Google Chrome|Chrome" {
		t.Fatalf("got %q", got)
	}
}

func TestQuickProgramShowsTheResultAndMarksAnOverrun(t *testing.T) {
	dir := t.TempDir()
	m := &popupMenu{cfgDir: dir, overruns: map[string]bool{}}
	live := &fakeLive{}

	item := cmdItem("printer", helperLine(t, "print", "one", "two", "three", "four"), true)
	s := pick(t, m, live, item)
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
	item.Action.Target = helperLine(t, "print", "Living room lights on")
	s = pick(t, m, live, item)
	if s.Overruns[item.Path] || s.Panel.Lines[0] != "Living room lights on" {
		t.Fatalf("after a short result: overrun %v panel %+v", s.Overruns[item.Path], s.Panel)
	}
}

func TestQuickProgramFailureAndSilence(t *testing.T) {
	m := &popupMenu{cfgDir: t.TempDir(), overruns: map[string]bool{}}
	live := &fakeLive{}
	if s := pick(t, m, live, cmdItem("bad", helperLine(t, "exit", "3"), true)); s.Panel.Style != core.PanelFailed || s.Panel.Lines[0] != "bad failed (exit 3)" {
		t.Fatalf("failure panel = %+v", s.Panel)
	}
	if s := pick(t, m, live, cmdItem("quiet", helperLine(t, "exit", "0"), true)); s.Panel.Style != core.PanelOK || s.Panel.Lines[0] != "quiet done" {
		t.Fatalf("silent success panel = %+v", s.Panel)
	}
	if s := pick(t, m, live, cmdItem("gone", "fzt-test-no-such-program", true)); s.Panel.Style != core.PanelFailed || !strings.Contains(s.Panel.Lines[0], "isn't installed here") {
		t.Fatalf("missing program panel = %+v", s.Panel)
	}
}

func TestQuickBuiltinRunsInPlace(t *testing.T) {
	var got []string
	testBuiltin(t, &builtin{name: "zz-ok", run: func(_ context.Context, _ io.Writer, args []string) (string, error) {
		got = args
		return "lights off for 30 min", nil
	}})
	testBuiltin(t, &builtin{name: "zz-fail", run: func(_ context.Context, out io.Writer, _ []string) (string, error) {
		io.WriteString(out, "warning: a hub call failed")
		return "", errors.New("off did not fully apply")
	}})
	m := &popupMenu{cfgDir: t.TempDir(), overruns: map[string]bool{}}
	live := &fakeLive{}
	// Unmarked, a built-in that doesn't need a terminal is still quick.
	if s := pick(t, m, live, cmdItem("lightsoff", "zz-ok off 30", false)); s.Panel.Style != core.PanelOK || s.Panel.Lines[0] != "lights off for 30 min" || strings.Join(got, " ") != "off 30" {
		t.Fatalf("panel = %+v, args %q", s.Panel, got)
	}
	s := pick(t, m, live, cmdItem("lightsoff", "zz-fail", false))
	if s.Panel.Style != core.PanelFailed || strings.Join(s.Panel.Lines, "|") != "warning: a hub call failed|off did not fully apply" {
		t.Fatalf("failure panel = %+v", s.Panel)
	}
}

func TestEscapeStopsAQuickRun(t *testing.T) {
	m := &popupMenu{cfgDir: t.TempDir(), overruns: map[string]bool{}}
	stopped := make(chan struct{})
	testBuiltin(t, &builtin{name: "zz-slow", run: func(ctx context.Context, _ io.Writer, _ []string) (string, error) {
		<-ctx.Done()
		close(stopped)
		return "", ctx.Err()
	}})
	for _, line := range []string{helperLine(t, "sleep", "30"), "zz-slow"} {
		live := &fakeLive{}
		m.onPick(tui.Pick{Kind: "select", Item: cmdItem("slow", line, true)}, live)
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
				t.Fatalf("%s: the run never froze the menu with an abort set", line)
			}
			time.Sleep(20 * time.Millisecond)
		}
		if s := live.settled(t); s.Panel.Lines[0] != "stopped slow" || s.Panel.Style != core.PanelFailed {
			t.Fatalf("%s: panel = %+v", line, s.Panel)
		}
	}
	<-stopped
}

func TestTakeoverWithoutItsTabSaysSo(t *testing.T) {
	m := &popupMenu{cfgDir: t.TempDir(), overruns: map[string]bool{}}
	live := &fakeLive{}
	m.runTakeover("spt", step{kind: stepProgram, args: []string{"spt"}}, live)
	if s := live.s; s.Frozen || live.closed || s.Panel.Style != core.PanelFailed || !strings.Contains(s.Panel.Lines[0], "which tab") {
		t.Fatalf("panel = %+v frozen %v closed %v", s.Panel, s.Frozen, live.closed)
	}
}

func TestTakeoverStartsItsProgramWithNoShell(t *testing.T) {
	testBuiltin(t, &builtin{name: "zz-term", terminal: true, run: func(context.Context, io.Writer, []string) (string, error) { return "", nil }})
	prefix := "new-window -d -a -t @3 -o remain-on-exit=failed -W /dev/pty0 "

	args, err := takeoverArgs("@3", "/dev/pty0", step{kind: stepProgram, args: []string{`C:\Program Files\uv.exe`, "run", "tuner.py"}})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("takeover = %q", got)
	}
	program := args[len(strings.Fields(prefix)):]
	if len(program) < 2 {
		t.Fatalf("one argument would go to default-shell: %q", program)
	}

	b, _ := resolve("zz-term x")
	args, _ = takeoverArgs("@3", "/dev/pty0", b)
	if !strings.Contains(strings.Join(args, " "), "zz-term x") || strings.Contains(strings.Join(args, " "), "pwsh") {
		t.Fatalf("a terminal built-in runs as this program's subcommand: %q", args)
	}
}

func TestTakeoverOfAnOldCommandKeepsAFailureOnScreen(t *testing.T) {
	args, _ := takeoverArgs("@3", "/dev/pty0", step{kind: stepScript, line: "spt"})
	got := strings.Join(args, " ")
	if !strings.Contains(got, "-e AT_PICK=1") || !strings.Contains(got, "-NoExit") || !strings.Contains(got, "spt; if ($? -and -not $LASTEXITCODE) { exit }") {
		t.Fatalf("takeover = %q", got)
	}
}

// Rerunning an old command on a restore would repeat what it did; only those
// are marked, so every other pick's program comes back.
func TestOnlyAnOldCommandComesBackAsAShell(t *testing.T) {
	const mark = "-o @session-set-shell=1"
	args, _ := takeoverArgs("@3", "/dev/pty0", step{kind: stepScript, line: "gitprune"})
	if got := strings.Join(args, " "); !strings.Contains(got, mark) {
		t.Fatalf("takeover = %q", got)
	}
	args, _ = takeoverArgs("@3", "/dev/pty0", step{kind: stepProgram, args: []string{"guitar-tabs", ""}})
	if got := strings.Join(args, " "); strings.Contains(got, mark) {
		t.Fatalf("a program is marked: %q", got)
	}
}

// A quick pick leaves the popup open; Escape at the top closes it.
func TestPopupStaysOpenUntilClosed(t *testing.T) {
	m := &popupMenu{cfgDir: t.TempDir(), overruns: map[string]bool{}}
	live := &fakeLive{}
	pick(t, m, live, cmdItem("quiet", helperLine(t, "exit", "0"), true))
	if live.closed {
		t.Fatal("a quick pick closed the popup")
	}
	m.onPick(tui.Pick{Kind: "cancel"}, live)
	if !live.closed {
		t.Fatal("Escape at the top didn't close the popup")
	}
}

func TestSyncReloadsTheMenuAndItsVersion(t *testing.T) {
	want := []core.Item{{Fields: []string{"lightsoff"}}}
	m := &popupMenu{load: func() ([]core.Item, int, error) { return want, 42, nil }}
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
