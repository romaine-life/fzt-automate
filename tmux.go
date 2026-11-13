package main

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/romaine-life/fzt/core"
)

// tmuxSwitchOnDraw is the at-tmux new-window option that switches
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
// hint of a row that asks for text, id and tags, separated by tabs.
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
// built by tmux on each press, so they are never saved to the cloud menu.
// Inside it, search goes by tag (core.TagSearchOn). A top-level menu entry
// named tmux is that folder: its own entries (the strip, TMUX-206) come first
// and are saved as menu entries, and tmux's rows join them.
func graftTmux(items []core.Item, rows []string) []core.Item {
	if len(rows) == 0 {
		return items
	}
	folder := -1
	for i, it := range items {
		if it.ParentIdx == -1 && len(it.Fields) > 0 && it.Fields[0] == tmuxFolder {
			folder = i
			break
		}
	}
	if folder == -1 {
		folder = len(items)
		items = append(items, core.Item{
			Fields:     []string{tmuxFolder, "Tabs, windows and everything tmux offers"},
			ParentIdx:  -1,
			Path:       tmuxFolder,
			PropertyOf: -1,
			Injected:   true,
		})
	}
	items[folder].HasChildren = true
	// Searched by tag inside, with tag mode off elsewhere (TMUX-196).
	items[folder].TagDefault = true
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
		fields, styled := tmuxFields(f[1], f[2])
		idx := len(items)
		items = append(items, core.Item{
			Fields:       fields,
			StyledFields: styled,
			Depth:      depth + 1,
			ParentIdx:  parent,
			Path:       items[parent].Path + " › " + fields[0],
			Input:      f[3],
			Action:     &core.ItemAction{Type: tmuxAction, Target: f[4]},
			PropertyOf: -1,
			Injected:   true,
		})
		if len(f) > 5 {
			// For tag search (FZT-67): separated by spaces or commas, as fzt's --tags-col.
			items[idx].Tags = strings.FieldsFunc(f[5], func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
		}
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

// tmuxFields is a row's name and description, the description only if it has
// one. tmux colours some of them (a window by its Claude state, TMUX-205) with
// SGR codes: the fields are the plain text, which search matches, and the
// colours go to StyledFields for drawing, which is nil for a row with none.
func tmuxFields(name, desc string) ([]string, [][]core.StyledRune) {
	raw := []string{name}
	if desc != "" {
		raw = append(raw, desc)
	}
	fields := make([]string, len(raw))
	coloured := false
	for i, r := range raw {
		fields[i] = core.StripANSI(r)
		if fields[i] != r {
			coloured = true
		}
	}
	if !coloured {
		return fields, nil
	}
	styled := make([][]core.StyledRune, len(raw))
	for i, r := range raw {
		styled[i] = core.ParseANSI(r)
	}
	return fields, styled
}

// tmuxPick is what display-palette reads back for a tmux row: its id, and
// the typed text for a row that asks for it.
func tmuxPick(item core.Item, text string, hasInput bool) string {
	if hasInput {
		return item.Action.Target + "\t" + text
	}
	return item.Action.Target
}
