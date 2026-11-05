package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchArchiveAsksTheServiceSignedIn(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Write([]byte(`{"current":"2026-11-05T06:00:00Z","hits":[{"when":"2026-10-31T18:00:00Z","pc":"nelsonpc","session":"s","who":"nelson","text":"[hit]","file":"nelsonpc/projects/p/s.jsonl","line":4}]}`))
	}))
	defer srv.Close()
	r, err := searchArchive(context.Background(), srv.URL, "tok", "win backtick")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL.Path != "/search" || got.URL.Query().Get("q") != "win backtick" || got.URL.Query().Get("limit") != "300" || got.Header.Get("Authorization") != "Bearer tok" {
		t.Fatal(got.URL, got.Header)
	}
	if len(r.Hits) != 1 || r.Hits[0].Line != 4 || r.Hits[0].File == "" || r.Current.IsZero() {
		t.Fatalf("%+v", r)
	}
}

func TestSearchArchiveSaysWhatFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "the index is still starting", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, err := searchArchive(context.Background(), srv.URL, "tok", "x")
	if err == nil || !strings.Contains(err.Error(), "still starting") {
		t.Fatal(err)
	}
}
