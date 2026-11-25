package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestALaptopSearchesItsOwnSessionsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	old := localTranscripts
	localTranscripts = dir
	defer func() { localTranscripts = old }()
	project := filepath.Join(dir, "-d-repos")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := `{"type":"user","timestamp":"2026-11-24T10:00:00Z","message":{"content":"the Gold Kite plan"}}
{"type":"assistant","timestamp":"2026-11-24T10:01:00Z","message":{"content":[{"type":"text","text":"the gold kite is ready"},{"type":"tool_use","name":"Bash"}]}}
{"type":"assistant","timestamp":"2026-11-24T10:02:00Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"echo gold kite"}}]}}
{"type":"user","timestamp":"2026-11-24T10:03:00Z","message":{"content":"only gold here"}}
`
	if err := os.WriteFile(filepath.Join(project, "s1.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := searchLocal(context.Background(), "gold KITE")
	if err != nil {
		t.Fatal(err)
	}
	// The tool call names the words too, but only what was written counts.
	if len(r.Hits) != 2 || r.Hits[0].Who != "Claude" || r.Hits[0].Line != 2 || r.Hits[1].Who != "Nelson" {
		t.Fatalf("hits = %+v", r.Hits)
	}
	if r.Hits[0].Session != "s1" || !filepath.IsAbs(r.Hits[0].File) {
		t.Fatalf("hit = %+v", r.Hits[0])
	}
}
