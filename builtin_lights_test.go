package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHub records each call the lights make, as "METHOD path body".
type fakeHub struct {
	mu        sync.Mutex
	calls     []string
	nightOn   bool
	stopped   bool
	failPaths map[string]bool
}

func (f *fakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.RequestURI()
	f.calls = append(f.calls, strings.TrimSpace(r.Method+" "+path+" "+string(body)))
	if f.failPaths[path] {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	switch {
	case strings.HasPrefix(path, "/device/fullJson/115"):
		state := "off"
		if f.nightOn {
			state = "on"
		}
		fmt.Fprintf(w, `{"device":{"deprecated":1,"DEPRECATED":2,"currentStates":{"switch":{"value":%q}}}}`, state)
	case path == "/installedapp/configure/json/94":
		if f.stopped {
			io.WriteString(w, `{"label":"Living room (Stopped)"}`)
		} else {
			io.WriteString(w, `{"label":"Living room"}`)
		}
	case path == "/logs/json":
		io.WriteString(w, `{"jobs":[{"id":"j1","link":"/installedapp/configure/136"},{"id":7,"link":"/installedapp/configure/99"}]}`)
	}
}

func runLightsAgainst(t *testing.T, f *fakeHub, args ...string) (string, string, error, []string) {
	t.Helper()
	srv := httptest.NewServer(f)
	defer srv.Close()
	old, oldPause := hubitatBase, hubPause
	hubitatBase, hubPause = srv.URL, func(context.Context, time.Duration) {}
	defer func() { hubitatBase, hubPause = old, oldPause }()
	var out bytes.Buffer
	done, err := runLights(context.Background(), &out, args)
	return done, out.String(), err, f.calls
}

const (
	reach      = "GET /device/fullJson/115"
	jobs       = "GET /logs/json"
	delJob     = "GET /hub/advanced/deleteScheduledJob?id=j1"
	ruleState  = "GET /installedapp/configure/json/94"
	markersOff = `POST /device/runmethod {"id":98,"method":"off"}|POST /device/runmethod {"id":101,"method":"off"}|POST /device/runmethod {"id":102,"method":"off"}`
)

func TestLightsOnSendsWhatTheScriptDid(t *testing.T) {
	done, _, err, calls := runLightsAgainst(t, &fakeHub{nightOn: true, stopped: true}, "on")
	if err != nil || done != "Living room lights ON (motion-controlled; timers reset)" {
		t.Fatalf("%q %v", done, err)
	}
	want := strings.Join([]string{
		reach, reach, `POST /device/runmethod {"id":115,"method":"off"}`,
		jobs, delJob, markersOff,
		ruleState, "POST /installedapp/btn id=94&name=stopRule&settings%5BstopRule%5D=clicked&stopRule.type=button",
		"POST /installedapp/btn id=94&name=runAction&settings%5BrunAction%5D=clicked&runAction.type=button",
	}, "|")
	if got := strings.Join(calls, "|"); got != want {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.ReplaceAll(got, "|", "\n"), strings.ReplaceAll(want, "|", "\n"))
	}
}

func TestLightsOffHoldsTheLightsOff(t *testing.T) {
	done, _, err, calls := runLightsAgainst(t, &fakeHub{}, "off", "60")
	if err != nil || done != "Living room suppressed for 60 min" {
		t.Fatalf("%q %v", done, err)
	}
	want := strings.Join([]string{
		reach, reach, jobs, delJob,
		`POST /device/runmethod {"id":96,"method":"off"}`, markersOff,
		`POST /device/runmethod {"id":101,"method":"off"}`, `POST /device/runmethod {"id":101,"method":"on"}`,
		ruleState,
	}, "|")
	if got := strings.Join(calls, "|"); got != want {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.ReplaceAll(got, "|", "\n"), strings.ReplaceAll(want, "|", "\n"))
	}
	// Plain "off" is 30 minutes, as lightsoff was.
	if done, _, _, _ := runLightsAgainst(t, &fakeHub{}, "off"); done != "Living room suppressed for 30 min" {
		t.Fatalf("off: %q", done)
	}
}

func TestLightsNight(t *testing.T) {
	done, _, err, calls := runLightsAgainst(t, &fakeHub{}, "night", "red", "45")
	if err != nil || !strings.HasPrefix(done, "Night mode: red for 45 min (until ") {
		t.Fatalf("%q %v", done, err)
	}
	want := strings.Join([]string{
		reach, jobs, delJob, markersOff,
		`POST /device/runmethod {"args":[{"type":"ENUM","value":"red"},{"type":"NUMBER","value":45}],"id":115,"method":"night"}`,
	}, "|")
	if got := strings.Join(calls, "|"); got != want {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.ReplaceAll(got, "|", "\n"), strings.ReplaceAll(want, "|", "\n"))
	}
	if done, _, _, _ := runLightsAgainst(t, &fakeHub{}, "night"); !strings.HasPrefix(done, "Night mode: warm for 120 min") {
		t.Fatalf("night's defaults: %q", done)
	}
}

func TestLightsReportAFailedCall(t *testing.T) {
	f := &fakeHub{failPaths: map[string]bool{"/device/runmethod": true}}
	done, out, err, _ := runLightsAgainst(t, f, "off", "30")
	if err == nil || done != "" || !strings.Contains(err.Error(), "off 30 did not fully apply: 6 hub call(s) failed") || !strings.Contains(out, "warning: runmethod \"off\" failed for device 96") {
		t.Fatalf("done %q err %v out %s", done, err, out)
	}
}

func TestLightsWithoutTheHubChangeNothing(t *testing.T) {
	old := hubitatBase
	hubitatBase = "http://127.0.0.1:1"
	defer func() { hubitatBase = old }()
	_, err := runLights(context.Background(), io.Discard, []string{"on"})
	if err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("got %v", err)
	}
}

func TestLightsUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"off", "45"}, {"night", "green"}, {"night", "warm", "0"}, {"on", "x"}, {"dim"}} {
		if _, err := runLights(context.Background(), io.Discard, args); err == nil || !strings.HasPrefix(err.Error(), "usage: lights") {
			t.Errorf("%q: got %v", args, err)
		}
	}
}
