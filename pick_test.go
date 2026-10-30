package main

import (
	"runtime"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
)

// A menu entry and a window share a name: picking the window runs the
// window's action, not the menu entry's.
func TestPickRunsThePickedItemWhenNamesCollide(t *testing.T) {
	command := `printf 'Steam\t\tfocus steam\n'`
	if runtime.GOOS == "windows" {
		command = `"Steam` + "`t`t" + `focus steam"`
	}
	items := []core.Item{
		{Fields: []string{"Steam"}, ParentIdx: -1, PropertyOf: -1, Action: &core.ItemAction{Type: "url", Target: "https://store.steampowered.com"}},
		{Fields: []string{"windows"}, ParentIdx: -1, PropertyOf: -1, Action: &core.ItemAction{Type: "command", Target: "folder:" + command}},
	}
	cfg := tui.Config{Layout: "reverse", TreeMode: true, Tiered: true, DepthPenalty: 5, Nth: []int{1}, AcceptNth: []int{1}, HidePalette: true, Provider: newCommandFolders(items)}
	sess := tui.NewTreeSession(items, cfg, 80, 24)

	var r core.ActionResult
	for _, k := range []any{'w', 'i', 'n', tcell.KeyTab, tcell.KeyDown, tcell.KeyEnter} {
		key, ch := tcell.KeyRune, rune(0)
		switch v := k.(type) {
		case tcell.Key:
			key = v
		case rune:
			ch = v
		}
		_, r = sess.HandleKeyResult(key, ch, false)
	}
	if r.Action != "select:Steam" {
		t.Fatalf("want the Steam window picked, got %q", r.Action)
	}
	if got := pickLine(tui.Pick{Kind: "select", Item: r.Item, Output: "Steam"}); got != "focus steam" {
		t.Fatalf("picking the Steam window ran %q, want focus steam", got)
	}
}
