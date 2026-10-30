package main

import (
	"runtime"
	"testing"

	"github.com/romaine-life/fzt/core"
)

func TestCommandFolders(t *testing.T) {
	command := `printf 'one\tfirst\trun-one\ntwo\n'`
	if runtime.GOOS == "windows" {
		command = `"one` + "`t" + `first` + "`t" + `run-one"; "two"`
	}
	items := []core.Item{
		{Fields: []string{"apps"}, HasChildren: true, ParentIdx: -1, Children: []int{1, 2}},
		{Fields: []string{"windows"}, Depth: 1, ParentIdx: 0, Action: &core.ItemAction{Type: "command", Target: "folder:" + command}},
		{Fields: []string{"notepad"}, Depth: 1, ParentIdx: 0, Action: &core.ItemAction{Type: "command", Target: "notepad"}},
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
	if one.Fields[0] != "one" || one.Fields[1] != "first" || one.Action.Target != "run-one" {
		t.Errorf("first child: %+v", one)
	}
	if len(two.Fields) != 1 || two.Fields[0] != "two" || two.Action != nil {
		t.Errorf("second child: %+v", two)
	}
	if p.LoadChildren(itemPath(items, 0)) != nil {
		t.Error("a menu folder that isn't a command folder loaded children")
	}
}
