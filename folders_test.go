package main

import (
	"context"
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

func TestAFolderThatCantListSaysSo(t *testing.T) {
	testBuiltin(t, &builtin{name: "zz-norows"})
	items := []core.Item{{Fields: []string{"x"}, ParentIdx: -1, Action: &core.ItemAction{Type: "command", Target: "folder:zz-norows"}}}
	p := newCommandFolders(items)
	got := p.LoadChildren(itemPath(items, 0))
	if len(got) != 1 || got[0].Fields[0] != "zz-norows failed" {
		t.Fatalf("got %+v", got)
	}
}
