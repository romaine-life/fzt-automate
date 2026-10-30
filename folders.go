package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/romaine-life/fzt/core"
)

// folderPrefix marks a menu item whose children come from a command, run each
// time the folder is entered: action "folder:openwindows" runs openwindows.
// The menu cache drops empty children, so the action is what survives sync.
const folderPrefix = "folder:"

// commandFolders is the tree provider for command folders. Each line the
// command prints is one child: name, then optionally a tab and a description,
// then a tab and the action run when the child is picked.
type commandFolders struct {
	commands map[string]string // folder path, as core.ItemFullPath builds it -> command
}

// newCommandFolders turns every "folder:" item into a folder with no children
// yet, and returns the provider that fills them. The folder keeps its action,
// so saving the menu keeps it a command folder.
func newCommandFolders(items []core.Item) *commandFolders {
	p := &commandFolders{commands: map[string]string{}}
	for i := range items {
		a := items[i].Action
		if a == nil || a.Type != "command" || !strings.HasPrefix(a.Target, folderPrefix) {
			continue
		}
		items[i].HasChildren = true
		p.commands[itemPath(items, i)] = strings.TrimPrefix(a.Target, folderPrefix)
	}
	return p
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
	var stderr bytes.Buffer
	cmd := shellCommand(command)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return []core.Item{{Fields: []string{command + " failed", msg}, PropertyOf: -1}}
	}

	var children []core.Item
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		item := core.Item{Fields: f[:1], PropertyOf: -1}
		if len(f) > 1 && f[1] != "" {
			item.Fields = append(item.Fields, f[1])
		}
		if len(f) > 2 && f[2] != "" {
			item.Action = &core.ItemAction{Type: "command", Target: f[2]}
		}
		children = append(children, item)
	}
	return children
}

// shellCommand runs a command line the way the at wrapper would: PowerShell
// on Windows (pwsh, else Windows PowerShell), sh elsewhere.
func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS != "windows" {
		return exec.Command("sh", "-c", command)
	}
	shell := "pwsh"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "powershell"
	}
	return exec.Command(shell, "-NoProfile", "-NonInteractive", "-Command", command)
}
