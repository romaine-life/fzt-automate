package main

import (
	"reflect"
	"testing"

	"github.com/romaine-life/fzt/core"
)

// Restarting takes opening the reboot folder and picking its one row: the row
// exists only once the folder is opened, so no search from the menu reaches it.
func TestRebootIsAFolderWithOneRestartRow(t *testing.T) {
	items := []core.Item{{Fields: []string{"reboot"}, ParentIdx: -1, Action: &core.ItemAction{Type: "command", Target: "folder:reboot"}}}
	p := newCommandFolders(items)
	if !items[0].HasChildren {
		t.Fatal("the reboot entry isn't a folder")
	}
	if !p.Volatile(itemPath(items, 0)) {
		t.Fatal("the reboot folder's row is loaded before it is opened")
	}
	rows := p.LoadChildren(itemPath(items, 0))
	if len(rows) != 1 {
		t.Fatalf("want one row, got %+v", rows)
	}
	row := rows[0]
	if row.Fields[0] != "restart now" || row.Action == nil || row.Action.Target != rebootAction || !row.Quick || row.HasChildren {
		t.Fatalf("restart row: %+v", row)
	}

	// The row runs Windows' shutdown directly, with no delay (a delay implies /f).
	s, err := resolve(row.Action.Target)
	if err != nil {
		t.Fatal(err)
	}
	if s.kind != stepProgram || !reflect.DeepEqual(s.args, []string{"shutdown", "/r", "/t", "0"}) {
		t.Fatalf("restart runs %+v", s)
	}
}

func TestRebootTakesNoArguments(t *testing.T) {
	if _, err := rebootRows(nil, []string{"now"}); err == nil {
		t.Fatal("reboot took an argument")
	}
}
