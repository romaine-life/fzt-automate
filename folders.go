package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/romaine-life/fzt/core"
)

// folderPrefix marks a menu item whose children come from a command, run each
// time the folder is entered: action "folder:openwindows" runs openwindows.
// The menu cache drops empty children, so the action is what survives sync.
const folderPrefix = "folder:"

// commandFolders is the tree provider for command folders. Each line the
// command prints is one child: name, then optionally a tab and a description,
// then a tab and the action run when the child is picked. A child whose action
// is itself "folder:<command>" is a folder of that command's lines (TMUX-206).
type commandFolders struct {
	commands map[string]string // folder path, as core.ItemFullPath builds it -> command
	// env, when set, is what a folder's program gets added to its environment:
	// in the popup, the tab it is over (TMUX_PANE) and its client (TMUX_CLIENT).
	env func() []string
}

// newCommandFolders turns every "folder:" item into a folder with no children
// yet, and returns the provider that fills them. The folder keeps its action,
// so saving the menu keeps it a command folder.
func newCommandFolders(items []core.Item) *commandFolders {
	p := &commandFolders{}
	p.reset(items)
	return p
}

// reset points the provider at a new menu: a long-lived menu keeps one
// provider across reloads.
func (p *commandFolders) reset(items []core.Item) {
	p.commands = map[string]string{}
	for i := range items {
		a := items[i].Action
		if a == nil || a.Type != "command" || !strings.HasPrefix(a.Target, folderPrefix) {
			continue
		}
		items[i].HasChildren = true
		p.commands[itemPath(items, i)] = strings.TrimPrefix(a.Target, folderPrefix)
	}
}

// Volatile: a command folder lists live state (the open windows), so it is
// read again every time it is entered.
func (p *commandFolders) Volatile(parentPath string) bool {
	_, ok := p.commands[parentPath]
	return ok
}

// itemPath matches core.ItemFullPath for menu names, which are never drive letters.
func itemPath(items []core.Item, i int) string {
	var parts []string
	for ; i >= 0; i = items[i].ParentIdx {
		parts = append([]string{items[i].Fields[0]}, parts...)
	}
	return filepath.Join(parts...)
}

func (p *commandFolders) LoadChildren(parentPath string) []core.Item {
	command, ok := p.commands[parentPath]
	if !ok {
		return nil
	}
	var env []string
	if p.env != nil {
		env = p.env()
	}
	rows, err := folderRows(command, env)
	if err != nil {
		return []core.Item{{Fields: []string{command + " failed", err.Error()}, PropertyOf: -1}}
	}
	var children []core.Item
	for _, r := range rows {
		// A folder's rows pick something (a window, a program): quick, never a takeover.
		item := core.Item{Fields: []string{r.name}, PropertyOf: -1, Quick: true}
		if r.description != "" {
			item.Fields = append(item.Fields, r.description)
		}
		if r.action != "" {
			item.Action = &core.ItemAction{Type: "command", Target: r.action}
			if sub, ok := strings.CutPrefix(r.action, folderPrefix); ok {
				item.HasChildren = true
				p.commands[filepath.Join(parentPath, r.name)] = sub
			}
		}
		children = append(children, item)
	}
	return children
}

// folderRows lists a command folder's children: a built-in's rows, or the lines
// a program or not-yet-moved at command prints, each "name", then optionally a
// tab and a description, then a tab and the action run when it is picked. A
// program or at command gets env added to its environment.
func folderRows(command string, env []string) ([]folderRow, error) {
	s, err := resolve(command)
	if err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	switch s.kind {
	case stepBuiltin:
		if s.b.rows == nil {
			return nil, fmt.Errorf("%s doesn't list anything, so it can't fill a folder", s.b.name)
		}
		return s.b.rows(context.Background(), s.args)
	case stepScript:
		cmd = scriptCommand(s.line)
	default:
		if cmd, err = programCommand(s.args); err != nil {
			return nil, err
		}
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New(msg)
		}
		return nil, err
	}
	var rows []folderRow
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		f := append(strings.Split(line, "\t"), "", "")
		rows = append(rows, folderRow{name: f[0], description: f[1], action: f[2]})
	}
	return rows, nil
}
