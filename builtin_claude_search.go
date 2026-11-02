package main

// Search the archive of every Claude Code session (HOMECFG-84) and open the
// one picked in VS Code at the part that matched (FZT-71; this was at's
// claude-search.ps1 and lib/claude-search.py). The index is
// \\NelsonNAS\claude-transcripts\index.sqlite, which nas-backup's weekly
// archive job builds from the transcripts the home PCs copy to that share; it
// is read here without writing anything, so sessions newer than the last
// weekly run aren't in it.

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
	_ "modernc.org/sqlite"
)

func init() {
	register(&builtin{name: "claude-search", usage: "[words]", terminal: true,
		about: "search every past Claude session and open the one picked in VS Code", run: runClaudeSearch})
}

var (
	transcriptShare = `\\NelsonNAS\claude-transcripts`
	searchIndexURI  = "file:////NelsonNAS/claude-transcripts/index.sqlite?mode=ro&immutable=1"
)

const (
	searchLimit = 300
	toolLines   = 40
)

type searchHit struct {
	rowid                 int64
	ts, pc, kind, snippet string
}

func openSearchIndex() (*sql.DB, error) {
	if _, err := os.Stat(filepath.Join(transcriptShare, "index.sqlite")); err != nil {
		return nil, fmt.Errorf("no search index at %s\\index.sqlite: the NAS is unreachable, or the weekly archive job hasn't run yet", transcriptShare)
	}
	return sql.Open("sqlite", searchIndexURI)
}

func searchArchive(ctx context.Context, db *sql.DB, words string) ([]searchHit, string, error) {
	var built sql.NullString
	_ = db.QueryRowContext(ctx, "SELECT max(at) FROM runs").Scan(&built)
	query := fmt.Sprintf("SELECT rowid, substr(ts, 1, 16), pc, kind, snippet(parts, 0, '[', ']', '...', 16) "+
		"FROM parts WHERE parts MATCH ? ORDER BY bm25(parts) LIMIT %d", searchLimit)
	rows, err := db.QueryContext(ctx, query, words)
	if err != nil {
		// Not valid FTS syntax (a stray quote, a dash): search the words as plain terms.
		var quoted []string
		for _, w := range strings.Fields(words) {
			quoted = append(quoted, `"`+strings.ReplaceAll(w, `"`, `""`)+`"`)
		}
		if rows, err = db.QueryContext(ctx, query, strings.Join(quoted, " ")); err != nil {
			return nil, built.String, err
		}
	}
	defer rows.Close()
	var hits []searchHit
	for rows.Next() {
		var h searchHit
		if err := rows.Scan(&h.rowid, &h.ts, &h.pc, &h.kind, &h.snippet); err != nil {
			return nil, built.String, err
		}
		hits = append(hits, h)
	}
	return hits, built.String, rows.Err()
}

func oneLine(text string, width int) string {
	s := strings.Join(strings.Fields(text), " ")
	if r := []rune(s); len(r) > width {
		s = string(r[:width])
	}
	return s
}

func runClaudeSearch(ctx context.Context, out io.Writer, args []string) (string, error) {
	words := strings.Join(args, " ")
	if words == "" {
		fmt.Fprint(out, "Search past Claude sessions for: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if words = strings.TrimSpace(line); words == "" {
			return "", nil
		}
	}
	db, err := openSearchIndex()
	if err != nil {
		return "", err
	}
	defer db.Close()
	hits, built, err := searchArchive(ctx, db, words)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "", fmt.Errorf("nothing in the archive matches %q (index built %s)", words, built)
	}

	items := []core.Item{{Fields: []string{"When", "PC", "Kind", "Match"}, Depth: -1}}
	for _, h := range hits {
		items = append(items, core.Item{
			Fields:     []string{strings.Replace(h.ts, "T", " ", 1), h.pc, h.kind, oneLine(h.snippet, 160)},
			ParentIdx:  -1,
			PropertyOf: -1,
			Action:     &core.ItemAction{Type: "command", Target: strconv.FormatInt(h.rowid, 10)},
		})
	}
	pick, err := tui.Run(items, tui.Config{
		Layout: "reverse", Border: true, HeaderLines: 1,
		Title: fmt.Sprintf("%d matches for %s, best first (index built %s)", len(hits), words, built),
	})
	if err != nil {
		return "", err
	}
	if pick.Kind != "select" || pick.Item.Action == nil {
		return "", nil
	}
	rowid, _ := strconv.ParseInt(pick.Item.Action.Target, 10, 64)
	target, err := writeSession(ctx, db, rowid)
	if err != nil {
		return "", err
	}
	if err := exec.Command(codeProgram(), "-g", target).Start(); err != nil {
		return "", fmt.Errorf("VS Code didn't open %s: %w", target, err)
	}
	return "", nil
}

// writeSession writes the session holding a hit as readable text and returns
// "<file>:<line>" for the part that matched.
func writeSession(ctx context.Context, db *sql.DB, rowid int64) (string, error) {
	var rel, pc, session string
	var target int
	if err := db.QueryRowContext(ctx, "SELECT file, line, pc, session FROM parts WHERE rowid = ?", rowid).Scan(&rel, &target, &pc, &session); err != nil {
		return "", fmt.Errorf("no result %d in the index", rowid)
	}
	src, err := os.Open(filepath.Join(append([]string{transcriptShare}, strings.Split(rel, "/")...)...))
	if err != nil {
		return "", err
	}
	defer src.Close()
	outPath := filepath.Join(os.TempDir(), "claude-search", fmt.Sprintf("%s-%s.md", pc, session))
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", err
	}
	w, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer w.Close()
	bw := bufio.NewWriter(w)
	head := fmt.Sprintf("# Claude session %s on %s\n\n<!-- %s -->\n\n", session, pc, rel)
	bw.WriteString(head)
	written, jump := 5, 1
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for n := 1; sc.Scan(); n++ {
		if n == target {
			jump = written
		}
		var entry map[string]any
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		if text := renderEntry(entry); text != "" {
			bw.WriteString(text + "\n\n")
			written += strings.Count(text, "\n") + 2
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if err := bw.Flush(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", outPath, jump), nil
}

// renderEntry is readable text for one transcript line, or empty.
func renderEntry(entry map[string]any) string {
	kind, _ := entry["type"].(string)
	if kind != "user" && kind != "assistant" {
		return ""
	}
	msg, _ := entry["message"].(map[string]any)
	var blocks []any
	switch c := msg["content"].(type) {
	case string:
		blocks = []any{map[string]any{"type": "text", "text": c}}
	case []any:
		blocks = c
	}
	who := "Nelson"
	if kind == "assistant" {
		who = "Claude"
	}
	var parts []string
	for _, raw := range blocks {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		str := func(k string) string { s, _ := b[k].(string); return strings.TrimSpace(s) }
		switch b["type"] {
		case "text":
			if t := str("text"); t != "" {
				parts = append(parts, fmt.Sprintf("## %s\n\n%s", who, t))
			}
		case "thinking":
			if t := str("thinking"); t != "" {
				parts = append(parts, "### thinking\n\n"+t)
			}
		case "tool_use":
			in, _ := json.MarshalIndent(b["input"], "", " ")
			parts = append(parts, fmt.Sprintf("### tool: %s\n\n%s", str("name"), in))
		case "tool_result":
			var body string
			switch c := b["content"].(type) {
			case string:
				body = c
			case []any:
				var texts []string
				for _, x := range c {
					if m, ok := x.(map[string]any); ok {
						t, _ := m["text"].(string)
						texts = append(texts, t)
					}
				}
				body = strings.Join(texts, "\n")
			}
			lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
			more := ""
			if len(lines) > toolLines {
				more = fmt.Sprintf("\n... (%d more lines)", len(lines)-toolLines)
				lines = lines[:toolLines]
			}
			parts = append(parts, "### result\n\n"+strings.Join(lines, "\n")+more)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	ts, _ := entry["timestamp"].(string)
	return fmt.Sprintf("<!-- %s -->\n%s", ts, strings.Join(parts, "\n\n"))
}
