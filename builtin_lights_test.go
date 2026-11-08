package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
	// loginOn answers as Hub Login Security does: admin pages redirect to /login
	// until the form sign-in sets the session cookie.
	loginOn bool
	signIns int
}

func (f *fakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.loginOn && r.URL.Path == "/login" && r.Method == http.MethodPost {
		r.ParseForm()
		f.mu.Lock()
		f.signIns++
		f.mu.Unlock()
		if r.Form.Get("username") == "nelson" && r.Form.Get("password") == "secret" && r.Form.Get("submit") == "Login" {
			http.SetCookie(w, &http.Cookie{Name: "HUBSESSION", Value: "s1", Path: "/"})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/login?loginError=true", http.StatusFound)
		return
	}
	if c, err := r.Cookie("HUBSESSION"); f.loginOn && (err != nil || c.Value != "s1") {
		http.Redirect(w, r, "/login?loginRedirect="+url.QueryEscape(r.URL.Path), http.StatusFound)
		return
	}
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

// withHubLogin points the lights at a login file holding content ("" for none).
func withHubLogin(t *testing.T, content string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "login.json")
	if content != "" {
		os.WriteFile(file, []byte(content), 0o600)
	}
	old := hubLoginPath
	hubLoginPath = func() string { return file }
	t.Cleanup(func() { hubLoginPath = old })
}

func TestLightsSignInWhenTheHubAsks(t *testing.T) {
	withHubLogin(t, `{"username":"nelson","password":"secret"}`)
	f := &fakeHub{loginOn: true}
	done, _, err, calls := runLightsAgainst(t, f, "on")
	off := &fakeHub{}
	_, _, _, want := runLightsAgainst(t, off, "on")
	if err != nil || done == "" || strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("err %v done %q\ncalls %v\nwant %v", err, done, calls, want)
	}
	if f.signIns != 1 {
		t.Fatalf("signed in %d times; the session should last the command", f.signIns)
	}
	if off.signIns != 0 {
		t.Fatal("signed in to a hub that didn't ask")
	}
}

func TestLightsWithARefusedLoginChangeNothing(t *testing.T) {
	for _, tc := range []struct{ login, want string }{
		{`{"username":"nelson","password":"wrong"}`, "the hub refused the login"},
		{"", "home-sync writes it from OpenBao"},
		{`{"username":"nelson"}`, "no hub username and password"},
	} {
		withHubLogin(t, tc.login)
		f := &fakeHub{loginOn: true}
		done, _, err, calls := runLightsAgainst(t, f, "on")
		if err == nil || done != "" || !strings.Contains(err.Error(), "nothing was changed: ") || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "wrong") {
			t.Fatalf("login %q: err %v", tc.login, err)
		}
		if len(calls) != 0 {
			t.Fatalf("login %q: changed %v", tc.login, calls)
		}
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
