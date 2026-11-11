package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLaunchClaudePreflight(t *testing.T) {
	t.Setenv("TMUX_PANE", "")
	if _, err := runLaunchClaude(context.Background(), io.Discard, []string{"one", "two"}); err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("invalid arguments: %v", err)
	}
	if _, err := runLaunchClaude(context.Background(), io.Discard, nil); err == nil || !strings.Contains(err.Error(), "at menu") {
		t.Fatalf("missing pane: %v", err)
	}
}
