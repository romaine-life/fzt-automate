package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTranscript(t *testing.T, projects, project, id string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(projects, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestClaudeSessions(t *testing.T) {
	projects, titles := t.TempDir(), t.TempDir()
	const (
		meta    = `{"type":"user","isMeta":true,"cwd":"D:\\x","message":{"role":"user","content":"Caveat: local commands"}}`
		command = `{"type":"user","cwd":"D:\\x","message":{"role":"user","content":"<command-name>/model</command-name>"}}`
		tool    = `{"type":"user","cwd":"D:\\x","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`
	)
	user := func(cwd, text string) string {
		return `{"type":"user","cwd":"` + strings.ReplaceAll(cwd, `\`, `\\`) + `","message":{"role":"user","content":"` + text + `"}}`
	}
	old := writeTranscript(t, projects, "D--repos-a", "aaa", meta, command, user(`D:\repos\a`, "the strip is wrong"),
		`{"type":"ai-title","aiTitle":"Strip shows wrong PR"}`, tool,
		`{"type":"custom-title","customTitle":"his-name"}`, `{"type":"ai-title","aiTitle":"Later title"}`)
	os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	writeTranscript(t, projects, "D--repos-b", "bbb", user(`D:\repos\b`, "fix the lights"),
		`{"type":"ai-title","aiTitle":"Fix the lights"}`)
	os.WriteFile(filepath.Join(titles, "bbb"), []byte("lights-scene-fix\n"), 0o644)
	writeTranscript(t, projects, "D--repos-c", "ccc", user(`D:\repos\c`, "only a prompt"))
	writeTranscript(t, projects, "D--repos-d", "ddd", meta, tool) // nothing typed: not listed
	blocks := `{"type":"user","cwd":"D:\\e","message":{"role":"user","content":[{"type":"text","text":"pasted"},{"type":"text","text":" text"}]}}`
	writeTranscript(t, projects, "D--e", "eee", blocks)

	got, err := claudeSessions(projects, titles)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]claudeSession{}
	for _, s := range got {
		byID[s.id] = s
	}
	if len(got) != 4 || got[len(got)-1].id != "aaa" {
		t.Fatalf("got %+v, want 4 sessions with the oldest (aaa) last", got)
	}
	cases := map[string][3]string{
		"aaa": {"his-name", `D:\repos\a`, "the strip is wrong"},
		"bbb": {"lights-scene-fix", `D:\repos\b`, "fix the lights"},
		"ccc": {"only a prompt", `D:\repos\c`, "only a prompt"},
		"eee": {"pasted text", `D:\e`, "pasted text"},
	}
	for id, want := range cases {
		s := byID[id]
		if s.name != want[0] || s.folder != want[1] || s.prompt != want[2] {
			t.Errorf("%s: got name %q folder %q prompt %q, want %q", id, s.name, s.folder, s.prompt, want)
		}
	}
}

func TestReadSessionTail(t *testing.T) {
	projects := t.TempDir()
	filler := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 1000) + `"}}`
	lines := []string{`{"type":"user","cwd":"D:\\big","message":{"role":"user","content":"a long session"}}`,
		`{"type":"ai-title","aiTitle":"Early title"}`}
	for range (sessionHead / 1000) + 50 {
		lines = append(lines, filler)
	}
	lines = append(lines, `{"type":"ai-title","aiTitle":"Final title"}`)
	f := writeTranscript(t, projects, "D--big", "big", lines...)
	s, ok := readSession(f, t.TempDir())
	if !ok || s.name != "Final title" || s.prompt != "a long session" {
		t.Errorf("got %+v %v", s, ok)
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 11, 2, 18, 0, 0, 0, time.Local)
	cases := map[time.Duration]string{
		5 * time.Minute:     "5 min ago",
		3 * time.Hour:       "3 h ago",
		3 * 24 * time.Hour:  "3 days ago",
		30 * 24 * time.Hour: "2026-10-03",
	}
	for d, want := range cases {
		if got := ago(now.Add(-d), now); got != want {
			t.Errorf("%v: got %q, want %q", d, got, want)
		}
	}
}
