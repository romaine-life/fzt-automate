package main

// The popup at (--popup): the menu as a popup over the tab you're on, opened
// by tmux's display-palette (Win+backtick at the root, Ctrl+B inside the tmux
// folder; SHELL-247). It stays open until you close it. A quick pick runs in
// the background with its result in the panel above the search box; a
// takeover opens as a new tab right after yours, and the popup closes once
// tmux has switched to it; a row from the tmux folder goes back to tmux,
// which runs it on your tab once the popup has closed.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
)

// pickKind is what a pick is before its action is resolved.
type pickKind int

const (
	pickNone    pickKind = iota // nothing to run (a folder, an informational row)
	pickURL                     // opened by the desktop, from here
	pickCommand                 // an action line: a built-in, a program, or an at command not yet moved
)

// classify gives the URL or the action line a pick runs. text is what was
// typed for an entry that declares an input; it is added as one argument.
func classify(item core.Item, text string, hasInput bool) (pickKind, string) {
	if item.Action != nil && item.Action.Type == "url" {
		return pickURL, item.Action.Target
	}
	line := ""
	switch {
	case item.Action != nil && item.Action.Target != "":
		line = item.Action.Target
	case item.Action == nil && !item.HasChildren && len(item.Fields) > 0:
		// A leaf with no action or url runs by its name (romaine-life/at README).
		line = item.Fields[0]
	}
	if line == "" || strings.HasPrefix(line, folderPrefix) {
		return pickNone, ""
	}
	if hasInput {
		// Quoted the same way for an action line and for PowerShell.
		line += " " + "'" + strings.ReplaceAll(text, "'", "''") + "'"
	}
	return pickCommand, line
}

// isTakeover says whether a resolved pick opens as its own tab. A built-in
// knows whether it talks to the person; a program or an at command not yet
// moved is a takeover unless its entry is marked quick, so nothing lands in
// the three-line panel by accident.
func isTakeover(s step, item core.Item) bool {
	if s.kind == stepBuiltin {
		return s.b.terminal
	}
	return !item.Quick
}

func itemName(item core.Item) string {
	if len(item.Fields) > 0 {
		return item.Fields[0]
	}
	return "command"
}

// liveMenu is the part of *tui.Live the popup uses; tests stand in for it.
type liveMenu interface {
	Update(func(*core.State))
	Reload([]core.Item)
	OnAbort(func())
	Close()
}

// popupMenu is the state the popup keeps between picks.
type popupMenu struct {
	cfgDir   string
	load     func() ([]core.Item, int, error) // the menu and its version, as a fresh start reads them
	overruns map[string]bool
	window   string    // the window of the tab the popup is over
	client   string    // the terminal showing it
	out      io.Writer // where a tmux row's pick goes back to display-palette
}

const (
	overrunsFile   = "overruns.json"
	lastOutputFile = "last-output.txt"
)

func runPopup(m *popupMenu, items []core.Item, cfg tui.Config) error {
	m.overruns = map[string]bool{}
	if data, err := os.ReadFile(filepath.Join(m.cfgDir, overrunsFile)); err == nil {
		_ = json.Unmarshal(data, &m.overruns)
	}
	cfg.ShowPanel = true
	return tui.RunLoop(items, cfg, func(live *tui.Live) {
		live.Update(func(s *core.State) { s.Overruns = m.overruns })
	}, func(p tui.Pick, live *tui.Live) { m.onPick(p, live) })
}

func (m *popupMenu) onPick(p tui.Pick, live liveMenu) {
	switch p.Kind {
	case "cancel":
		live.Close()
		return
	case "command":
		m.onCommand(p.Output, live)
		return
	case "select", "input":
	default:
		return
	}
	if a := p.Item.Action; a != nil && a.Type == tmuxAction {
		fmt.Fprintln(m.out, tmuxPick(p.Item, p.Text, p.Kind == "input"))
		live.Close()
		return
	}
	kind, line := classify(p.Item, p.Text, p.Kind == "input")
	name := itemName(p.Item)
	switch kind {
	case pickURL:
		if err := openURL(line); err != nil {
			text := logFailure(m.cfgDir, name, "", fmt.Sprintf("%s didn't open: %v", name, err))
			live.Update(func(s *core.State) { s.SetPanel(text, core.PanelFailed) })
			return
		}
		live.Update(func(s *core.State) { s.SetPanel("opened "+name, core.PanelOK) })
	case pickCommand:
		s, err := resolve(line)
		if err != nil {
			text := logFailure(m.cfgDir, name, "", fmt.Sprintf("%s can't run: %v", name, err))
			live.Update(func(st *core.State) { st.SetPanel(text, core.PanelFailed) })
			return
		}
		s.dir = tabFolder(m.window)
		if isTakeover(s, p.Item) {
			m.runTakeover(name, s, live)
		} else {
			m.runQuick(p.Item, name, line, s, live)
		}
	}
}

// onCommand handles the `:` palette results that end a one-shot menu.
func (m *popupMenu) onCommand(action string, live liveMenu) {
	switch action {
	case "synced", "unloaded":
		items, version, err := m.load()
		if err != nil {
			live.Update(func(s *core.State) { s.SetPanel("the menu didn't reload: "+err.Error(), core.PanelFailed) })
			return
		}
		live.Reload(items)
		live.Update(func(s *core.State) { s.MenuVersion = version })
		msg := "menu synced"
		if action == "unloaded" {
			msg = "menu cache cleared"
		}
		live.Update(func(s *core.State) { s.SetPanel(msg, core.PanelOK) })
	}
}

// job is a quick pick or a takeover's tmux call while it runs: wait ends with
// its verdict, stop ends it early.
type job struct {
	wait func() error
	stop func()
}

func startCmd(cmd *exec.Cmd) (job, error) {
	if err := cmd.Start(); err != nil {
		return job{}, err
	}
	return job{wait: cmd.Wait, stop: func() { _ = cmd.Process.Kill() }}, nil
}

// runQuick runs a quick pick in the background, the menu frozen until it
// ends. Escape stops it. A built-in runs here and returns its sentence; a
// program's exit code is the verdict and its output the panel's text. The
// panel shows three lines, and the full output goes to the last-output file.
func (m *popupMenu) runQuick(item core.Item, name, line string, s step, live liveMenu) {
	var out bytes.Buffer
	var resultText string
	var start func() (job, error)
	switch s.kind {
	case stepBuiltin:
		start = func() (job, error) {
			ctx, cancel := context.WithCancel(withDir(context.Background(), s.dir))
			done := make(chan error, 1)
			go func() {
				text, err := s.b.run(ctx, &out, s.args)
				resultText = text
				done <- err
			}()
			return job{wait: func() error { defer cancel(); return <-done }, stop: cancel}, nil
		}
	default:
		start = func() (job, error) {
			var cmd *exec.Cmd
			if s.kind == stepScript {
				cmd = scriptCommand(s.line)
				cmd.Env = append(os.Environ(), "AT_PICK=1")
			} else {
				var err error
				if cmd, err = programCommand(s.args); err != nil {
					return job{}, err
				}
			}
			cmd.Dir = s.dir
			cmd.Stdout = &out
			cmd.Stderr = &out
			return startCmd(cmd)
		}
	}
	m.runFrozen(start, "running "+name+"…", name, live, func(err error, stopped bool) (string, int) {
		full := strings.TrimSpace(strings.TrimSpace(out.String()) + "\n" + resultText)
		m.saveLastOutput(line, full)
		var exit *exec.ExitError
		switch {
		case stopped:
			return "stopped " + name, core.PanelFailed
		case errors.As(err, &exit):
			if full == "" {
				full = fmt.Sprintf("%s failed (exit %d)", name, exit.ExitCode())
			}
			return full, core.PanelFailed
		case err != nil:
			if s.kind == stepBuiltin {
				return strings.TrimSpace(out.String() + "\n" + err.Error()), core.PanelFailed
			}
			return fmt.Sprintf("%s failed: %v", name, err), core.PanelFailed
		case full == "":
			return name + " done", core.PanelOK
		}
		return full, core.PanelOK
	}, item.Path, s.dir, nil)
}

// runTakeover opens the pick as a new tab right after the one the popup is
// over. tmux creates it in the background and switches to it once its program
// has drawn (at-tmux `new-window -W`, FZT-64), so the screen never
// shows a blank pane; until then the menu is frozen with the panel saying so,
// and Escape stops waiting, leaving the tab where it is. Once tmux has
// switched, the popup closes.
func (m *popupMenu) runTakeover(name string, s step, live liveMenu) {
	args, err := takeoverArgs(m.window, m.client, s)
	if err != nil {
		text := logFailure(m.cfgDir, name, s.dir, fmt.Sprintf("%s can't open: %v", name, err))
		live.Update(func(st *core.State) { st.SetPanel(text, core.PanelFailed) })
		return
	}
	cmd := exec.Command("tmux", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	m.runFrozen(func() (job, error) { return startCmd(cmd) }, "starting "+name+"…", name, live, func(err error, stopped bool) (string, int) {
		switch {
		case stopped:
			return "stopped waiting for " + name + "; its tab is after yours", core.PanelFailed
		case err != nil:
			if text := strings.TrimSpace(stderr.String()); text != "" {
				return text, core.PanelFailed
			}
			return fmt.Sprintf("%s didn't open: %v", name, err), core.PanelFailed
		}
		return "opened " + name, core.PanelOK
	}, "", s.dir, func(err error, stopped bool) {
		if err == nil && !stopped {
			live.Close()
		}
	})
}

// runFrozen runs a job with the menu frozen and the panel showing running.
// Escape stops it. When it ends, result gives the panel's text and style;
// path, when set, is the entry whose overrun mark the result updates.
// done, when set, runs once the panel shows the result.
func (m *popupMenu) runFrozen(start func() (job, error), running, name string, live liveMenu, result func(err error, stopped bool) (string, int), path, dir string, done func(err error, stopped bool)) {
	live.Update(func(s *core.State) {
		s.Freeze()
		s.SetPanel(running, core.PanelRunning)
	})
	j, err := start()
	if err != nil {
		text := logFailure(m.cfgDir, name, dir, fmt.Sprintf("%s didn't start: %v", name, err))
		live.Update(func(s *core.State) {
			s.SetPanel(text, core.PanelFailed)
			s.Unfreeze()
		})
		return
	}
	stopped := make(chan struct{}, 1)
	live.OnAbort(func() {
		select {
		case stopped <- struct{}{}:
		default:
		}
		j.stop()
	})
	go func() {
		err := j.wait()
		live.OnAbort(nil)
		wasStopped := false
		select {
		case <-stopped:
			wasStopped = true
		default:
		}
		text, style := result(err, wasStopped)
		if err != nil && !wasStopped {
			text = logFailure(m.cfgDir, name, dir, text)
		}
		live.Update(func(s *core.State) {
			s.SetPanel(text, style)
			if path != "" {
				m.markOverrun(path, s.Panel.Clipped)
			}
			s.Overruns = m.overruns
			s.Unfreeze()
		})
		if done != nil {
			done(err, wasStopped)
		}
	}()
}

func (m *popupMenu) saveLastOutput(line, output string) {
	body := fmt.Sprintf("%s\n%s\n\n%s", line, time.Now().Format(time.RFC3339), output)
	_ = os.WriteFile(filepath.Join(m.cfgDir, lastOutputFile), []byte(body), 0o644)
}

// markOverrun records whether an entry's last run printed past the panel, so
// its row stays marked until it is fixed or moved to the takeover side.
func (m *popupMenu) markOverrun(path string, clipped bool) {
	if m.overruns[path] == clipped {
		return
	}
	if clipped {
		m.overruns[path] = true
	} else {
		delete(m.overruns, path)
	}
	if data, err := json.Marshal(m.overruns); err == nil {
		_ = os.WriteFile(filepath.Join(m.cfgDir, overrunsFile), data, 0o644)
	}
}

// takeoverProgram is what a takeover tab runs. A built-in that talks to the
// person runs as this fzt-automate's subcommand; a program runs as itself. An
// at command not yet moved runs as before: in a shell with the profile,
// closing with it when it succeeds and dropping to a prompt when it fails.
func takeoverProgram(s step) []string {
	switch s.kind {
	case stepBuiltin:
		return append([]string{selfProgram(), s.b.name}, s.args...)
	case stepScript:
		return []string{powershell(), "-NoLogo", "-NoExit", "-Command",
			s.line + "; if ($? -and -not $LASTEXITCODE) { exit }"}
	}
	return s.args
}

// takeoverArgs builds the tmux command that opens a takeover after window,
// for the client showing it. Given two or more arguments, tmux starts the
// program itself, with no shell. A tab whose program fails stays open with
// its output (remain-on-exit failed, set as the window is made, before the
// program can end). An old command's tab comes back from a tmux restore as a
// shell (@session-set-shell): its prompt after a failure is a shell, and
// rerunning it would repeat what it did.
func takeoverArgs(window, client string, s step) ([]string, error) {
	if window == "" || client == "" {
		return nil, errors.New("tmux didn't say which tab the menu is over (--window, --client)")
	}
	args := []string{"new-window", "-d", "-a", "-t", window, "-o", "remain-on-exit=failed"}
	if s.dir != "" {
		args = append(args, "-c", s.dir)
	}
	if s.kind == stepScript {
		args = append(args, "-e", "AT_PICK=1", "-o", "@session-set-shell=1")
	}
	if s.kind == stepBuiltin {
		args = append(args, "-e", originWindowEnv+"="+window)
	}
	args = append(args, tmuxSwitchOnDraw(client)...)
	return append(args, tmuxProgram(takeoverProgram(s))...), nil
}
