package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
)

// display-palette's rows: depth, name, description, hint, id, tags.
const paletteRows = "0\tsplit-window\tSplit a pane\t\t0\tsplit window,pane\n" +
	"0\trename-window\tRename the window\tnew name\t1\n" +
	"0\tmove-tab\tMove this tab\t\t2\n" +
	"1\tsession-a\t\t\t3\n" +
	"1\tsession-b\t\t\t4\n" +
	"0\treopen-tab\tReopen the tab closed last\t\t5\n"

func TestGraftTmuxBuildsTheFolder(t *testing.T) {
	rows, err := readTmuxRows(strings.NewReader(strings.ReplaceAll(paletteRows, "\n", "\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	menu := []core.Item{{Fields: []string{"lights"}, ParentIdx: -1, PropertyOf: -1}}
	items := graftTmux(menu, rows)

	folder := items[1]
	if folder.Fields[0] != "tmux" || !folder.HasChildren || !folder.Injected || folder.Depth != 0 || !folder.TagDefault {
		t.Fatalf("tmux folder = %+v", folder)
	}
	var names []string
	for _, c := range folder.Children {
		names = append(names, items[c].Fields[0])
	}
	if strings.Join(names, ",") != "split-window,rename-window,move-tab,reopen-tab" {
		t.Fatalf("tmux folder holds %v", names)
	}
	if got := strings.Join(items[2].Tags, "|"); got != "split|window|pane" {
		t.Fatalf("split-window tags = %q", got)
	}
	if len(items[3].Tags) != 0 {
		t.Fatalf("a row with no tags got %v", items[3].Tags)
	}
	rename, move := items[3], items[4]
	if rename.Input != "new name" || rename.Action.Target != "1" || rename.Path != "tmux › rename-window" {
		t.Fatalf("rename-window = %+v", rename)
	}
	if !move.HasChildren || move.Action != nil || len(move.Children) != 2 || items[move.Children[0]].Depth != 2 {
		t.Fatalf("move-tab sub-menu = %+v", move)
	}
	for _, it := range items[1:] {
		if !it.Injected {
			t.Fatalf("%s would be saved to the cloud menu", it.Fields[0])
		}
		if it.Palette {
			t.Fatalf("%s would run as a : palette command, not go back to tmux", it.Fields[0])
		}
	}
}

// A menu entry named tmux is the folder: its entries stay first and saved,
// tmux's rows join them (TMUX-206).
func TestGraftTmuxJoinsTheMenusTmuxEntry(t *testing.T) {
	rows, _ := readTmuxRows(strings.NewReader(paletteRows))
	menu := []core.Item{
		{Fields: []string{"tmux", "Tabs"}, ParentIdx: -1, PropertyOf: -1, HasChildren: true, Children: []int{1}, Path: "tmux"},
		{Fields: []string{"strip"}, Depth: 1, ParentIdx: 0, PropertyOf: -1, Path: "tmux › strip"},
	}
	items := graftTmux(menu, rows)
	folders := 0
	for _, it := range items {
		if it.ParentIdx == -1 && it.Fields[0] == "tmux" {
			folders++
		}
	}
	if folders != 1 {
		t.Fatalf("%d tmux folders", folders)
	}
	folder := items[0]
	if folder.Injected || items[1].Injected || !folder.TagDefault {
		t.Fatalf("the menu's tmux entry = %+v, strip = %+v", folder, items[1])
	}
	var names []string
	for _, c := range folder.Children {
		names = append(names, items[c].Fields[0])
	}
	if strings.Join(names, ",") != "strip,split-window,rename-window,move-tab,reopen-tab" {
		t.Fatalf("tmux folder holds %v", names)
	}
	if items[2].Path != "tmux › split-window" || !items[2].Injected {
		t.Fatalf("a tmux row = %+v", items[2])
	}
}

func TestGraftTmuxWithoutRowsAddsNothing(t *testing.T) {
	menu := []core.Item{{Fields: []string{"lights"}, ParentIdx: -1, PropertyOf: -1}}
	if got := graftTmux(menu, nil); len(got) != 1 {
		t.Fatalf("no rows added %d items", len(got)-1)
	}
}

// A tmux row goes back to display-palette as its id (and typed text), and
// the popup closes so tmux can run it on the tab.
func TestTmuxPickGoesBackToTmux(t *testing.T) {
	rows, _ := readTmuxRows(strings.NewReader(paletteRows))
	items := graftTmux(nil, rows)
	for _, c := range []struct {
		pick tui.Pick
		want string
	}{
		{tui.Pick{Kind: "select", Item: items[1]}, "0\n"},
		{tui.Pick{Kind: "input", Item: items[2], Text: "logs"}, "1\tlogs\n"},
	} {
		var out bytes.Buffer
		m := &popupMenu{out: &out, overruns: map[string]bool{}}
		live := &fakeLive{}
		m.onPick(c.pick, live)
		if out.String() != c.want || !live.closed {
			t.Fatalf("%s: printed %q closed %v, want %q and closed", c.pick.Item.Fields[0], out.String(), live.closed, c.want)
		}
	}
}

// Enter on a tmux row reaches the popup as a pick (FZT-73): a row that asks
// for text opens the text box, and one that doesn't is selected. Both were
// dropped as unknown : palette commands.
func TestEnterOnATmuxRowPicksIt(t *testing.T) {
	enter := func(query string) (*core.State, string) {
		rows, _ := readTmuxRows(strings.NewReader(paletteRows))
		cfg := core.Config{Layout: "reverse", HidePalette: true, TreeMode: true, Tiered: true, DepthPenalty: 5, AcceptNth: []int{1}}
		s, searchCols := core.NewState(graftTmux(nil, rows), cfg)
		ctx := s.TopCtx()
		ctx.TreeExpanded = make(map[int]bool)
		ctx.QueryExpanded = make(map[int]bool)
		ctx.TreeCursor = -1
		for _, ch := range query {
			core.HandleUnifiedKeyResult(s, tcell.KeyRune, ch, false, cfg, searchCols)
		}
		r := core.HandleUnifiedKeyResult(s, tcell.KeyEnter, 0, false, cfg, searchCols)
		return s, tui.ProcessAction(s, r.Action)
	}
	if s, action := enter("rename-window"); action != "" || s.PromptPlaceholder != "new name" {
		t.Fatalf("Enter on rename-window: action %q, text box hint %q; want its text box", action, s.PromptPlaceholder)
	}
	if _, action := enter("reopen-tab"); !strings.HasPrefix(action, "select:") {
		t.Fatalf("Enter on reopen-tab: action %q, want it selected", action)
	}
}

// Inside the tmux folder, where Ctrl+B opens the menu, a query searches by
// tag with tag mode off: "pane" resolves to split-window's tag. At the root the
// same query is a name search (TMUX-196).
func TestTmuxFolderSearchesByTag(t *testing.T) {
	search := func(inside bool) *core.TreeContext {
		rows, _ := readTmuxRows(strings.NewReader(paletteRows))
		menu := []core.Item{{Fields: []string{"lights"}, ParentIdx: -1, PropertyOf: -1}}
		cfg := core.Config{Layout: "reverse", HidePalette: true, TreeMode: true, Tiered: true, DepthPenalty: 5, AcceptNth: []int{1}}
		s, searchCols := core.NewState(graftTmux(menu, rows), cfg)
		ctx := s.TopCtx()
		ctx.TreeExpanded = make(map[int]bool)
		ctx.QueryExpanded = make(map[int]bool)
		ctx.TreeCursor = -1
		if inside && !core.EnterFolder(s, []string{"tmux"}, cfg, searchCols) {
			t.Fatal("no tmux folder to enter")
		}
		for _, ch := range "pane" {
			core.HandleUnifiedKeyResult(s, tcell.KeyRune, ch, false, cfg, searchCols)
		}
		return s.TopCtx()
	}
	ctx := search(true)
	if !core.TagSearchActive(ctx) || ctx.TagTerms[0].Tag != "pane" || len(ctx.Filtered) != 1 || ctx.Filtered[0].Fields[0] != "split-window" {
		t.Fatalf("inside the tmux folder, pane: tag terms %v, %d results; want the tag pane and split-window", ctx.TagTerms, len(ctx.Filtered))
	}
	if ctx := search(false); core.TagSearchActive(ctx) {
		t.Fatalf("at the root, pane searched by tag with tag mode off: %v", ctx.TagTerms)
	}
}

// --open tmux/nav/window opens the popup inside the nav folder's window list
// (TMUX-202), and Enter picks a window there.
func TestOpenAtANestedFolder(t *testing.T) {
	const navRows = "0\tnav\tGo to a window or session\t\t0\n" +
		"1\twindow\t\t\t1\n" +
		"2\tw1\t\t\t2\n" +
		"2\tw2\t\t\t3\n" +
		"2\tw3\t\t\t4\n" +
		"1\tsession\t\t\t5\n" +
		"2\ts1\t\t\t6\n"
	if path := startFolder(""); path != nil {
		t.Fatalf("startFolder with no folder = %v; want nothing", path)
	}
	rows, _ := readTmuxRows(strings.NewReader(navRows))
	cfg := core.Config{Layout: "reverse", HidePalette: true, TreeMode: true, Tiered: true, DepthPenalty: 5, AcceptNth: []int{1}}
	cfg.StartFolder = startFolder("tmux/nav/window")
	s, searchCols := core.NewState(graftTmux(nil, rows), cfg)
	ctx := s.TopCtx()
	ctx.TreeExpanded = make(map[int]bool)
	ctx.QueryExpanded = make(map[int]bool)
	ctx.TreeCursor = -1
	if !core.EnterFolder(s, cfg.StartFolder, cfg, searchCols) {
		t.Fatalf("no folder %v", cfg.StartFolder)
	}
	visible := core.TreeVisibleItems(s)
	var names []string
	for i, v := range visible {
		names = append(names, v.Item.Fields[0])
		if v.Item.Fields[0] == "w2" {
			ctx.TreeCursor = i
		}
	}
	if !strings.Contains(strings.Join(names, ","), "window,w1,w2,w3") {
		t.Fatalf("the window folder is not open: %v", names)
	}
	r := core.HandleUnifiedKeyResult(s, tcell.KeyEnter, 0, false, cfg, searchCols)
	if action := tui.ProcessAction(s, r.Action); !strings.HasPrefix(action, "select:") {
		t.Fatalf("Enter on w2: action %q, want it picked", action)
	}
	if got := tmuxPick(visible[ctx.TreeCursor].Item, "", false); got != "3" {
		t.Fatalf("w2 goes back to tmux as %q, want its id 3", got)
	}
}

// A row tmux coloured (a window by its Claude state, TMUX-205) is drawn in
// its colours and searched and picked by its plain text.
func TestTmuxRowColours(t *testing.T) {
	const coloured = "0\twindow-nav\tGo to a window\t\t0\n" +
		"1\t1\t\x1b[38;2;57;186;230mbuild\x1b[39m\t\t1\n" +
		"1\t2\t[edit]\t\t2\n"
	rows, _ := readTmuxRows(strings.NewReader(coloured))
	items := graftTmux(nil, rows)
	build, edit := items[2], items[3]
	if build.Fields[1] != "build" || build.Path != "tmux › window-nav › 1" {
		t.Fatalf("a coloured row's text = %q, path %q", build.Fields[1], build.Path)
	}
	if len(build.StyledFields) != 2 || len(build.StyledFields[1]) != len("build") {
		t.Fatalf("a coloured row's styles = %v", build.StyledFields)
	}
	if fg, _, _ := build.StyledFields[1][0].Style.Decompose(); fg != tcell.NewRGBColor(57, 186, 230) {
		t.Fatalf("build is drawn in %v, want #39bae6", fg)
	}
	if edit.StyledFields != nil {
		t.Fatalf("a row with no colour has styles %v", edit.StyledFields)
	}
}
