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

// pickKind is how the popup runs a pick.
type pickKind int

const (
	pickNone     pickKind = iota // nothing to run (a folder, an informational row)
	pickURL                      // opened by the desktop, from here
	pickQuick                    // run here, result in the panel
	pickTakeover                 // a new tmux window that owns its program
)

// classify decides how a pick runs and the command line it runs. URLs and
// `launch` actions are quick by their kind; any other command is a takeover
// unless its entry is marked quick, so nothing lands in the three-line panel
// by accident. text is what was typed for an entry that declares an input.
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
		line += " " + psQuote(text)
	}
	if item.Quick || firstWord(line) == "launch" {
		return pickQuick, line
	}
	return pickTakeover, line
}

func firstWord(line string) string {
	if f := strings.Fields(line); len(f) > 0 {
		return f[0]
	}
	return ""
}

// psQuote makes text one PowerShell argument.
func psQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", "''") + "'"
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
		live.Update(func(s *core.State) {
			s.Overruns = m.overruns
			if os.Getenv("AT_SCRIPTS") == "" {
				s.SetPanel("at commands can't run here: tmux has no AT_SCRIPTS (set-fzt.ps1 sets it)", core.PanelFailed)
			}
		})
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
			live.Update(func(s *core.State) { s.SetPanel(fmt.Sprintf("%s didn't open: %v", name, err), core.PanelFailed) })
			return
		}
		live.Update(func(s *core.State) { s.SetPanel("opened "+name, core.PanelOK) })
	case pickQuick:
		m.runQuick(p.Item, name, line, live)
	case pickTakeover:
		m.runTakeover(name, line, live)
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

// runQuick runs a quick command in the background, the menu frozen until it ends. Escape
// stops it. The exit code is the verdict; the panel shows three lines, and the
// full output goes to the last-output file.
func (m *popupMenu) runQuick(item core.Item, name, line string, live liveMenu) {
	cmd := shellCommand(line)
	cmd.Env = append(os.Environ(), "AT_PICK=1")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	m.runFrozen(cmd, "running "+name+"…", name, live, func(err error, stopped bool) (string, int) {
		m.saveLastOutput(line, out.String())
		text := strings.TrimSpace(out.String())
		var exit *exec.ExitError
		switch {
		case stopped:
			return "stopped " + name, core.PanelFailed
		case errors.As(err, &exit):
			if text == "" {
				text = fmt.Sprintf("%s failed (exit %d)", name, exit.ExitCode())
			}
			return text, core.PanelFailed
		case err != nil:
			return fmt.Sprintf("%s failed: %v", name, err), core.PanelFailed
		case text == "":
			return name + " done", core.PanelOK
		}
		return text, core.PanelOK
	}, item.Path, nil)
}

// runTakeover opens the command as a new tab right after the one the popup is
// over. tmux creates it in the background and switches to it once its program
// has drawn (tmux-windows-fork `new-window -W`, FZT-64), so the screen never
// shows a blank pane; until then the menu is frozen with the panel saying so,
// and Escape stops waiting, leaving the tab where it is. Once tmux has
// switched, the popup closes.
func (m *popupMenu) runTakeover(name, line string, live liveMenu) {
	args, err := takeoverArgs(m.window, m.client, line)
	if err != nil {
		live.Update(func(s *core.State) { s.SetPanel(fmt.Sprintf("%s can't open: %v", name, err), core.PanelFailed) })
		return
	}
	cmd := exec.Command("tmux", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	m.runFrozen(cmd, "starting "+name+"…", name, live, func(err error, stopped bool) (string, int) {
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
	}, "", func(err error, stopped bool) {
		if err == nil && !stopped {
			live.Close()
		}
	})
}

// runFrozen runs cmd with the menu frozen and the panel showing running.
// Escape kills cmd. When it ends, result gives the panel's text and style;
// path, when set, is the entry whose overrun mark the result updates.
// done, when set, runs once the panel shows the result.
func (m *popupMenu) runFrozen(cmd *exec.Cmd, running, name string, live liveMenu, result func(err error, stopped bool) (string, int), path string, done func(err error, stopped bool)) {
	live.Update(func(s *core.State) {
		s.Freeze()
		s.SetPanel(running, core.PanelRunning)
	})
	if err := cmd.Start(); err != nil {
		live.Update(func(s *core.State) {
			s.SetPanel(fmt.Sprintf("%s didn't start: %v", name, err), core.PanelFailed)
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
		_ = cmd.Process.Kill()
	})
	go func() {
		err := cmd.Wait()
		live.OnAbort(nil)
		wasStopped := false
		select {
		case <-stopped:
			wasStopped = true
		default:
		}
		text, style := result(err, wasStopped)
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

// takeoverProgram is what a takeover window runs: the command in a shell
// with the profile, closing with it when it succeeds and dropping to a prompt
// when it fails, so its error stays on screen (as the one-shot at does).
func takeoverProgram(line string) []string {
	return []string{powershell(), "-NoLogo", "-NoExit", "-Command",
		line + "; if ($? -and -not $LASTEXITCODE) { exit }"}
}

// takeoverArgs builds the tmux command that opens a takeover after window,
// for the client showing it.
func takeoverArgs(window, client, line string) ([]string, error) {
	if window == "" || client == "" {
		return nil, errors.New("tmux didn't say which tab the menu is over (--window, --client)")
	}
	args := []string{"new-window", "-d", "-a", "-t", window, "-e", "AT_PICK=1"}
	args = append(args, tmuxSwitchOnDraw(client)...)
	return append(args, takeoverProgram(line)...), nil
}
