package main

// claude-search off Windows: a work laptop has no session index to ask (it lives at
// home), so it searches its own Claude sessions, the transcripts under
// ~/.claude/projects, newest match first.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// localTranscripts is where Claude Code keeps this machine's sessions; a variable so
// tests can point it elsewhere.
var localTranscripts = filepath.Join(os.Getenv("HOME"), ".claude", "projects")

// searchLocal finds the transcript lines whose text holds every word, ignoring case.
func searchLocal(ctx context.Context, words string) (searchResult, error) {
	result := searchResult{Current: time.Now()}
	want := strings.Fields(strings.ToLower(words))
	host, _ := os.Hostname()
	files, err := filepath.Glob(filepath.Join(localTranscripts, "*", "*.jsonl"))
	if err != nil {
		return result, err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		hits, err := searchTranscript(file, host, want)
		if err != nil {
			continue
		}
		result.Hits = append(result.Hits, hits...)
	}
	sort.SliceStable(result.Hits, func(i, j int) bool { return result.Hits[i].When.After(result.Hits[j].When) })
	if len(result.Hits) > searchLimit {
		result.Hits = result.Hits[:searchLimit]
	}
	return result, nil
}

func searchTranscript(file, host string, want []string) ([]searchHit, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var hits []searchHit
	session := strings.TrimSuffix(filepath.Base(file), ".jsonl")
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := sc.Bytes()
		lower := strings.ToLower(string(line))
		all := true
		for _, w := range want {
			if !strings.Contains(lower, w) {
				all = false
				break
			}
		}
		if !all {
			continue
		}
		var entry map[string]any
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		text := entryText(entry)
		matched := strings.ToLower(text)
		for _, w := range want {
			if !strings.Contains(matched, w) {
				text = ""
				break
			}
		}
		if text == "" {
			continue
		}
		when, _ := time.Parse(time.RFC3339Nano, str(entry["timestamp"]))
		who := "Nelson"
		if str(entry["type"]) == "assistant" {
			who = "Claude"
		}
		hits = append(hits, searchHit{When: when, PC: host, Session: session, Who: who, Text: text, File: file, Line: n})
	}
	return hits, sc.Err()
}

// entryText is the words a person or Claude wrote in one transcript line: its text
// blocks, not tool calls or results.
func entryText(entry map[string]any) string {
	kind := str(entry["type"])
	if kind != "user" && kind != "assistant" {
		return ""
	}
	msg, _ := entry["message"].(map[string]any)
	switch c := msg["content"].(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, raw := range c {
			if b, ok := raw.(map[string]any); ok && b["type"] == "text" {
				parts = append(parts, str(b["text"]))
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
