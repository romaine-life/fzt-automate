package main

// Resume a Claude Code session on this PC (HOMECFG-129). Each session is listed
// by its name: the one Nelson gave it (claude -n, /rename), else the window name
// at-home-tools' claude-pane gave it from the first prompt
// (~/.local/state/claude-title/<session id>), else Claude's generated title.
// The pick restarts this tab as the Claude app state.json names, continuing the
// session in its folder, as launch-claude does. Given a session id, it resumes
// that one in this tab's folder, as tmux's session restore, reopen-window and
// claude-relaunch-with-chrome do. claude-search is the other half: full-text search of
// every PC's archive, opened for reading.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
)

func init() {
	register(&builtin{name: "resume-claude", usage: "[<session id> [--chrome]]", terminal: true,
		about: "pick a past Claude session on this PC by its name, or name one, then resume it in this tab", run: runResumeClaude})
}

type claudeSession struct {
	id, name, folder, prompt string
	active                   time.Time
}

const (
	// The first prompt is near the top, and the titles are rewritten all through a
	// transcript, so its tail has the newest.
	sessionHead = 2 << 20
	sessionTail = 256 << 10
)

func claudeHome() string {
	home, _ := os.UserHomeDir()
	return home
}

// claudeSessions lists the sessions in projects (~/.claude/projects), newest first.
func claudeSessions(projects, titles string) ([]claudeSession, error) {
	files, err := filepath.Glob(filepath.Join(projects, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	read := make([]claudeSession, len(files))
	found := make([]bool, len(files))
	next := make(chan int)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i := range next {
				read[i], found[i] = readSession(files[i], titles)
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	var out []claudeSession
	for i, s := range read {
		if found[i] {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].active.After(out[j].active) })
	return out, nil
}

// readSession reads one transcript; ok is false for one with no prompt typed.
func readSession(file, titles string) (claudeSession, bool) {
	fh, err := os.Open(file)
	if err != nil {
		return claudeSession{}, false
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		return claudeSession{}, false
	}
	s := claudeSession{id: strings.TrimSuffix(filepath.Base(file), ".jsonl"), active: info.ModTime()}
	var custom, ai string
	scan := func(r io.Reader, first bool) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), sessionHead)
		for sc.Scan() {
			line := sc.Bytes()
			if !bytes.HasPrefix(line, []byte("{")) || !bytes.Contains(line, []byte(`"type":"`)) {
				continue // the tail starts mid-line
			}
			if !bytes.Contains(line, []byte(`-title"`)) && !(first && bytes.Contains(line, []byte(`"type":"user"`))) {
				continue
			}
			var e struct {
				Type        string          `json:"type"`
				CustomTitle string          `json:"customTitle"`
				AITitle     string          `json:"aiTitle"`
				Cwd         string          `json:"cwd"`
				IsMeta      bool            `json:"isMeta"`
				Message     json.RawMessage `json:"message"`
			}
			if json.Unmarshal(line, &e) != nil {
				continue
			}
			switch e.Type {
			case "custom-title":
				custom = e.CustomTitle
			case "ai-title":
				ai = e.AITitle
			case "user":
				if first && s.prompt == "" && !e.IsMeta {
					if p := typedPrompt(e.Message); p != "" {
						s.prompt, s.folder = p, e.Cwd
						if info.Size() > sessionTail {
							return // the tail has the titles
						}
					}
				}
			}
		}
	}
	scan(io.LimitReader(fh, sessionHead), true)
	if s.prompt == "" {
		return claudeSession{}, false
	}
	if info.Size() > sessionTail {
		if _, err := fh.Seek(-sessionTail, io.SeekEnd); err == nil {
			scan(fh, false)
		}
	}
	s.name = custom
	if s.name == "" {
		if b, err := os.ReadFile(filepath.Join(titles, s.id)); err == nil {
			s.name = strings.TrimSpace(string(b))
		}
	}
	if s.name == "" {
		s.name = ai
	}
	if s.name == "" {
		s.name = oneLine(s.prompt, 40)
	}
	return s, true
}

// typedPrompt is the text of a prompt Nelson typed, or "" for a tool result or a
// command's output, which Claude Code records as user lines too.
func typedPrompt(raw json.RawMessage) string {
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	var text string
	if json.Unmarshal(m.Content, &text) != nil {
		var blocks []struct{ Type, Text string }
		if json.Unmarshal(m.Content, &blocks) != nil {
			return ""
		}
		for _, b := range blocks {
			if b.Type != "text" {
				return ""
			}
			text += b.Text
		}
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "<") {
		return ""
	}
	return text
}

// folderLabel names a session's folder: its last part, or the repo for one of
// Claude Code's worktrees (<repo>\.claude\worktrees\<name>).
func folderLabel(folder string) string {
	f := strings.ReplaceAll(folder, `\`, "/")
	if repo, _, ok := strings.Cut(f, "/.claude/worktrees/"); ok {
		return path.Base(repo) + " (worktree)"
	}
	return path.Base(f)
}

func ago(t, now time.Time) string {
	switch d := now.Sub(t); {
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}

func runResumeClaude(ctx context.Context, out io.Writer, args []string) (string, error) {
	switch {
	case len(args) == 0:
	case !strings.HasPrefix(args[0], "-") && (len(args) == 1 || len(args) == 2 && args[1] == "--chrome"):
		dir, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return "", startClaude(dir, args[0], len(args) == 2)
	default:
		return "", usageError(builtins["resume-claude"])
	}
	home := claudeHome()
	sessions, err := claudeSessions(filepath.Join(home, ".claude", "projects"), filepath.Join(home, ".local", "state", "claude-title"))
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", errors.New("no Claude sessions on this PC")
	}
	now := time.Now()
	items := []core.Item{{Fields: []string{"Name", "Folder · last active · first prompt"}, Depth: -1}}
	for i, s := range sessions {
		items = append(items, core.Item{
			Fields: []string{oneLine(s.name, 40),
				fmt.Sprintf("%s · %s · %s", oneLine(folderLabel(s.folder), 24), ago(s.active, now), oneLine(s.prompt, 120))},
			ParentIdx:  -1,
			PropertyOf: -1,
			Action:     &core.ItemAction{Type: "command", Target: fmt.Sprint(i)},
		})
	}
	pick, err := tui.Run(items, tui.Config{
		Layout: "reverse", Border: true, HeaderLines: 1,
		Title: fmt.Sprintf("Resume a Claude session (%d on this PC, newest first)", len(sessions)),
	})
	if err != nil {
		return "", err
	}
	if pick.Kind != "select" || pick.Item.Action == nil {
		return "", nil
	}
	var i int
	fmt.Sscan(pick.Item.Action.Target, &i)
	s := sessions[i]
	if fi, err := os.Stat(s.folder); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s's folder %s is gone, so Claude can't resume it there", s.name, s.folder)
	}
	return "", startClaude(s.folder, s.id, false)
}
