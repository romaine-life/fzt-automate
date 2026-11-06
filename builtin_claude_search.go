package main

// Search every Claude Code session (HOMECFG-84) and open the one picked in VS
// Code at the part that matched (FZT-71; this was at's claude-search.ps1 and
// lib/claude-search.py). The session-index service on the NAS answers the
// search as this machine and keeps its index current as the PCs copy
// transcripts up (AHT-15); the session itself is read from the share.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/romaine-life/auth/client"
	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
)

func init() {
	register(&builtin{name: "claude-search", usage: "[words]", terminal: true,
		about: "search every past Claude session and open the one picked in VS Code", run: runClaudeSearch})
}

const (
	transcriptShare = `\\NelsonNAS\claude-transcripts`
	sessionIndex    = "https://session-index.home.romaine.life"
	searchLimit     = 300
	searchTimeout   = 90 * time.Second
	toolLines       = 40
)

type searchHit struct {
	When    time.Time `json:"when"`
	PC      string    `json:"pc"`
	Session string    `json:"session"`
	Who     string    `json:"who"`
	Text    string    `json:"text"`
	File    string    `json:"file"`
	Line    int       `json:"line"`
}

type searchResult struct {
	Hits    []searchHit `json:"hits"`
	Current time.Time   `json:"current"`
}

func searchArchive(ctx context.Context, service, token, words string) (searchResult, error) {
	var r searchResult
	q := url.Values{"q": {words}, "limit": {strconv.Itoa(searchLimit)}}
	req, err := http.NewRequestWithContext(ctx, "GET", service+"/search?"+q.Encode(), nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return r, fmt.Errorf("can't reach the session index: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return r, fmt.Errorf("the session index answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return r, fmt.Errorf("reading the session index's answer: %w", err)
	}
	return r, nil
}

func oneLine(text string, width int) string {
	s := strings.Join(strings.Fields(text), " ")
	if r := []rune(s); len(r) > width {
		s = string(r[:width])
	}
	return s
}

// homeToken is client.HomeToken; a seam for tests.
var homeToken = client.HomeToken

func sessionIndexToken(ctx context.Context) (string, error) {
	credentials, err := homeToken(ctx, "session-index")
	if err != nil {
		return "", fmt.Errorf("this machine couldn't sign in: %w", err)
	}
	return credentials.Token, nil
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
	token, err := sessionIndexToken(ctx)
	if err != nil {
		return "", err
	}
	searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	result, err := searchArchive(searchCtx, sessionIndex, token, words)
	if err != nil {
		return "", err
	}
	current := result.Current.Local().Format("2006-01-02 15:04")
	if len(result.Hits) == 0 {
		return "", fmt.Errorf("nothing in the archive matches %q (index current at %s)", words, current)
	}

	items := []core.Item{{Fields: []string{"When", "PC", "Who", "Match"}, Depth: -1}}
	for i, h := range result.Hits {
		items = append(items, core.Item{
			Fields:     []string{h.When.Local().Format("2006-01-02 15:04"), h.PC, h.Who, oneLine(h.Text, 160)},
			ParentIdx:  -1,
			PropertyOf: -1,
			Action:     &core.ItemAction{Type: "command", Target: strconv.Itoa(i)},
		})
	}
	pick, err := tui.Run(items, tui.Config{
		Layout: "reverse", Border: true, HeaderLines: 1,
		Title: fmt.Sprintf("%d matches for %s, best first (index current at %s)", len(result.Hits), words, current),
	})
	if err != nil {
		return "", err
	}
	if pick.Kind != "select" || pick.Item.Action == nil {
		return "", nil
	}
	picked, err := strconv.Atoi(pick.Item.Action.Target)
	if err != nil || picked < 0 || picked >= len(result.Hits) {
		return "", fmt.Errorf("the picker returned %q, which isn't a match", pick.Item.Action.Target)
	}
	target, err := writeSession(result.Hits[picked])
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
func writeSession(h searchHit) (string, error) {
	rel, pc, session, target := h.File, h.PC, h.Session, h.Line
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
