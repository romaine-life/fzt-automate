package main

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/romaine-life/fzt/core"
)

// tmuxSwitchOnDraw is the tmux-windows-fork new-window option that switches
// client to the new window once its pane has drawn, and waits until then.
func tmuxSwitchOnDraw(client string) []string {
	return []string{"-W", client}
}

var hasScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// withScheme gives a bookmark saved without one ("photos.google.com") https.
func withScheme(target string) string {
	if hasScheme.MatchString(target) {
		return target
	}
	return "https://" + target
}

const (
	tmuxFolder = "tmux"
	// tmuxAction is the action type of a row tmux supplied: its target is the
	// row's id, which tmux runs on the tab the menu was opened over.
	tmuxAction = "tmux"
)

// readTmuxRows reads display-palette's rows: depth, name, description, the
// hint of a row that asks for text, and id, separated by tabs.
func readTmuxRows(r io.Reader) ([]string, error) {
	var rows []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if line := strings.TrimRight(sc.Text(), "\r"); line != "" {
			rows = append(rows, line)
		}
	}
	return rows, sc.Err()
}

// graftTmux appends tmux's rows to the menu as its tmux folder. The rows are
// built by tmux on each press, so the folder is never saved to the cloud menu.
func graftTmux(items []core.Item, rows []string) []core.Item {
	if len(rows) == 0 {
		return items
	}
	folder := len(items)
	items = append(items, core.Item{
		Fields:      []string{tmuxFolder, "Tabs, windows and everything tmux offers"},
		ParentIdx:   -1,
		HasChildren: true,
		Path:        tmuxFolder,
		PropertyOf:  -1,
		Injected:    true,
	})
	stack := []int{folder} // stack[d] is the parent of a row at depth d
	for _, line := range rows {
		f := strings.Split(line, "\t")
		if len(f) < 5 {
			continue
		}
		depth, err := strconv.Atoi(f[0])
		if err != nil || depth < 0 || depth >= len(stack) {
			continue
		}
		stack = stack[:depth+1]
		parent := stack[depth]
		fields := []string{f[1]}
		if f[2] != "" {
			fields = append(fields, f[2])
		}
		idx := len(items)
		items = append(items, core.Item{
			Fields:     fields,
			Depth:      depth + 1,
			ParentIdx:  parent,
			Path:       items[parent].Path + " › " + f[1],
			Input:      f[3],
			Action:     &core.ItemAction{Type: tmuxAction, Target: f[4]},
			PropertyOf: -1,
			Injected:   true,
		})
		items[parent].Children = append(items[parent].Children, idx)
		if parent != folder {
			// A row with rows under it is a sub-menu: entering it is the pick.
			items[parent].HasChildren = true
			items[parent].Action = nil
			items[parent].Input = ""
		}
		stack = append(stack, idx)
	}
	return items
}

// tmuxPick is what display-palette reads back for a tmux row: its id, and
// the typed text for a row that asks for it.
func tmuxPick(item core.Item, text string, hasInput bool) string {
	if hasInput {
		return item.Action.Target + "\t" + text
	}
	return item.Action.Target
}
