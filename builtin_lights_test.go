package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/romaine-life/auth/client"
)

// fakeHomeHub is home-hub's PUT /living-room: it records each request and answers with
// the mode asked for, or with code when set.
type fakeHomeHub struct {
	mu    sync.Mutex
	got   []map[string]any
	auth  []string
	code  int
	until time.Time
}

func (f *fakeHomeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method != http.MethodPut || r.URL.Path != "/living-room" {
		http.NotFound(w, r)
		return
	}
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	var req map[string]any
	json.NewDecoder(r.Body).Decode(&req)
	f.got = append(f.got, req)
	if f.code != 0 {
		http.Error(w, "the hub still runs the living room", f.code)
		return
	}
	mode := map[string]string{"on": "motion", "off": "held-off", "night": "night"}[req["mode"].(string)]
	json.NewEncoder(w).Encode(map[string]any{"mode": mode, "until": f.until, "color": req["color"]})
}

func lightsAgainst(t *testing.T, f *fakeHomeHub, args ...string) (string, error) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	oldBase, oldToken := homeHubBase, homeToken
	t.Cleanup(func() { homeHubBase, homeToken = oldBase, oldToken })
	homeHubBase = srv.URL
	homeToken = func(_ context.Context, role string) (client.Credentials, error) {
		if role != "home-hub" {
			t.Errorf("asked for a %q token", role)
		}
		return client.Credentials{Token: "tok"}, nil
	}
	var out bytes.Buffer
	return runLights(context.Background(), &out, args)
}

func TestLightsSendsEachModeToHomeHub(t *testing.T) {
	until := time.Date(2026, 11, 8, 23, 30, 0, 0, time.Local)
	for _, c := range []struct {
		args []string
		want string
		says string
	}{
		{[]string{"on"}, `{"mode":"on"}`, "Living room lights ON"},
		{[]string{"off"}, `{"minutes":30,"mode":"off"}`, "held off for 30 min (until 11:30 PM)"},
		{[]string{"off", "120"}, `{"minutes":120,"mode":"off"}`, "held off for 120 min"},
		{[]string{"night"}, `{"color":"warm","minutes":120,"mode":"night"}`, "Night mode: warm for 120 min (until 11:30 PM)"},
		{[]string{"night", "red", "45"}, `{"color":"red","minutes":45,"mode":"night"}`, "Night mode: red for 45 min"},
		{[]string{"night", "60"}, `{"color":"warm","minutes":60,"mode":"night"}`, "warm for 60 min"},
	} {
		f := &fakeHomeHub{until: until}
		said, err := lightsAgainst(t, f, c.args...)
		if err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		got, _ := json.Marshal(f.got[0])
		if len(f.got) != 1 || string(got) != c.want || f.auth[0] != "Bearer tok" {
			t.Errorf("%v sent %s with %q, want %s", c.args, got, f.auth, c.want)
		}
		if !strings.Contains(said, c.says) {
			t.Errorf("%v said %q, want %q in it", c.args, said, c.says)
		}
	}
}

func TestLightsRefusesBadArgumentsWithoutCalling(t *testing.T) {
	for _, args := range [][]string{{}, {"dim"}, {"on", "now"}, {"off", "45"}, {"off", "30", "x"}, {"night", "green"}, {"night", "0"}, {"night", "warm", "2000"}} {
		f := &fakeHomeHub{}
		if _, err := lightsAgainst(t, f, args...); err == nil || len(f.got) != 0 {
			t.Errorf("%v: err %v, %d calls", args, err, len(f.got))
		}
	}
}

func TestLightsSaysWhatHomeHubRefused(t *testing.T) {
	f := &fakeHomeHub{code: http.StatusConflict}
	_, err := lightsAgainst(t, f, "on")
	if err == nil || !strings.Contains(err.Error(), "nothing was changed") || !strings.Contains(err.Error(), "the hub still runs the living room") {
		t.Fatalf("got %v", err)
	}
}
