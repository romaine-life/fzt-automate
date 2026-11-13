package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/romaine-life/fzt/core"
)

func TestCommandFolders(t *testing.T) {
	testBuiltin(t, &builtin{name: "zz-rows", rows: func(context.Context, []string) ([]folderRow, error) {
		return []folderRow{{name: "one", description: "first", action: "run-one"}, {name: "two"}}, nil
	}})
	program := helperLine(t, "print", "three\tthird\trun-three")
	items := []core.Item{
		{Fields: []string{"apps"}, HasChildren: true, ParentIdx: -1, Children: []int{1, 2, 3}},
		{Fields: []string{"windows"}, Depth: 1, ParentIdx: 0, Action: &core.ItemAction{Type: "command", Target: "folder:zz-rows"}},
		{Fields: []string{"notepad"}, Depth: 1, ParentIdx: 0, Action: &core.ItemAction{Type: "command", Target: "notepad"}},
		{Fields: []string{"printed"}, Depth: 1, ParentIdx: 0, Action: &core.ItemAction{Type: "command", Target: "folder:" + program}},
	}

	p := newCommandFolders(items)
	windows := items[1]
	if !windows.HasChildren || windows.Action == nil {
		t.Fatalf("windows should be a folder that keeps its action, got %+v", windows)
	}
	if items[2].Action == nil || items[2].Action.Target != "notepad" {
		t.Fatalf("notepad's action changed: %+v", items[2].Action)
	}

	children := p.LoadChildren(itemPath(items, 1))
	if len(children) != 2 {
		t.Fatalf("want 2 children, got %+v", children)
	}
	one, two := children[0], children[1]
	if one.Fields[0] != "one" || one.Fields[1] != "first" || one.Action.Target != "run-one" || !one.Quick {
		t.Errorf("first child: %+v", one)
	}
	if len(two.Fields) != 1 || two.Fields[0] != "two" || two.Action != nil {
		t.Errorf("second child: %+v", two)
	}

	printed := p.LoadChildren(itemPath(items, 3))
	if len(printed) != 1 || printed[0].Fields[1] != "third" || printed[0].Action.Target != "run-three" {
		t.Errorf("a program's rows: %+v", printed)
	}
	if p.LoadChildren(itemPath(items, 0)) != nil {
		t.Error("a menu folder that isn't a command folder loaded children")
	}
}

// A row whose action is "folder:<command>" is a folder too, filled when it is
// entered and read again each time (TMUX-206: strip › follow-ups › a follow-up).
func TestAFolderRowCanBeAFolder(t *testing.T) {
	calls := 0
	testBuiltin(t, &builtin{name: "zz-outer", rows: func(context.Context, []string) ([]folderRow, error) {
		return []folderRow{{name: "follow-ups", description: "2 open", action: "folder:zz-inner a/b"}, {name: "leaf", action: "run-leaf"}}, nil
	}})
	testBuiltin(t, &builtin{name: "zz-inner", rows: func(_ context.Context, args []string) ([]folderRow, error) {
		calls++
		return []folderRow{{name: "start", description: strings.Join(args, " "), action: "run-start"}}, nil
	}})
	items := []core.Item{{Fields: []string{"strip"}, ParentIdx: -1, Action: &core.ItemAction{Type: "command", Target: "folder:zz-outer"}}}
	p := newCommandFolders(items)
	outer := p.LoadChildren(itemPath(items, 0))
	if len(outer) != 2 || !outer[0].HasChildren || outer[1].HasChildren {
		t.Fatalf("outer rows: %+v", outer)
	}
	path := filepath.Join(itemPath(items, 0), "follow-ups")
	if !p.Volatile(path) {
		t.Fatal("the inner folder isn't read again on entry")
	}
	inner := p.LoadChildren(path)
	if len(inner) != 1 || inner[0].Fields[1] != "a/b" || inner[0].Action.Target != "run-start" {
		t.Fatalf("inner rows: %+v", inner)
	}
	p.LoadChildren(path)
	if calls != 2 {
		t.Fatalf("the inner folder was read %d times for two entries", calls)
	}
}

// In the popup a folder's program learns the tab and client it was opened over.
func TestAFolderProgramGetsTheTab(t *testing.T) {
	program := helperLine(t, "env", "TMUX_PANE", "TMUX_CLIENT")
	items := []core.Item{{Fields: []string{"strip"}, ParentIdx: -1, Action: &core.ItemAction{Type: "command", Target: "folder:" + program}}}
	p := newCommandFolders(items)
	p.env = func() []string { return []string{"TMUX_PANE=%7", "TMUX_CLIENT=client-42"} }
	got := p.LoadChildren(itemPath(items, 0))
	if len(got) != 2 || got[0].Fields[1] != "%7" || got[1].Fields[1] != "client-42" {
		t.Fatalf("rows: %+v", got)
	}
}

func TestAFolderThatCantListSaysSo(t *testing.T) {
	testBuiltin(t, &builtin{name: "zz-norows"})
	items := []core.Item{{Fields: []string{"x"}, ParentIdx: -1, Action: &core.ItemAction{Type: "command", Target: "folder:zz-norows"}}}
	p := newCommandFolders(items)
	got := p.LoadChildren(itemPath(items, 0))
	if len(got) != 1 || got[0].Fields[0] != "zz-norows failed" {
		t.Fatalf("got %+v", got)
	}
}
