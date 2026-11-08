package main

// The living room lights, as calls to the Hubitat hub (FZT-71; this was
// romaine-life/at's lib/living-room-lights.ps1). Virtual "suppress" switches
// that Rule Machine handlers act on, motion rule 94, and night mode on device
// 115 "Living Room Night", whose app (147) pauses rule 94 and drives the four
// Zigbee color bulbs itself. Design and hub ids: romaine-life/lights docs/
// (night-mode.md, house-inventory.md).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func init() {
	register(&builtin{name: "lights", usage: "on | off [30|60|120] | night [warm|red|amber|blue] [minutes]",
		about: "the living room lights: motion-controlled, held off for a while, or night mode", run: runLights})
}

// The router's DNS answers for the hub's DHCP reservation.
var hubitatBase = "http://hubitat"

const (
	motionRuleApp = 94
	nightDevice   = 115
	allLights     = 96 // group "Living room - All"
)

// Each hold-off length has its marker switch; turning one on starts its timer.
var offMarkers = map[int]int{30: 98, 60: 101, 120: 102}

// The apps whose scheduled jobs are the hold-off timers.
var suppressionApps = []int{123, 136, 141}

var nightColors = []string{"warm", "red", "amber", "blue"}

// hubPause waits between calls the hub needs time for, or until the command is stopped.
var hubPause = func(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// hub is one lights command's calls. Every call that fails is counted, so the
// command never reports success for something the hub didn't take.
type hub struct {
	ctx      context.Context
	out      io.Writer
	client   *http.Client
	failures int
	// sleep is hubPause for this command.
	sleep func(time.Duration)
}

func (h *hub) warn(format string, args ...any) {
	h.failures++
	fmt.Fprintf(h.out, "warning: "+format+"\n", args...)
}

func (h *hub) do(method, path, contentType string, body []byte, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(h.ctx, timeout)
	defer cancel()
	for signedIn := false; ; signedIn = true {
		req, err := http.NewRequestWithContext(ctx, method, hubitatBase+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := h.client.Do(req)
		if err != nil {
			return nil, err
		}
		if !hubLoginRedirect(resp) {
			return readHubReply(resp)
		}
		resp.Body.Close()
		if signedIn {
			return nil, hubLoginError{"the hub still asks for its login after signing in"}
		}
		if err := hubSignIn(ctx, h.client); err != nil {
			return nil, err
		}
	}
}

func readHubReply(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return data, nil
}

type hubArg struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// runMethod calls a device command. The hub (2.5.x) takes a JSON body with
// {type, value} arguments; a form body or a bare array returns 500 and the
// command is dropped.
func (h *hub) runMethod(device int, method string, args ...hubArg) {
	payload := map[string]any{"id": device, "method": method}
	if len(args) > 0 {
		payload["args"] = args
	}
	body, _ := json.Marshal(payload)
	if _, err := h.do(http.MethodPost, "/device/runmethod", "application/json", body, 10*time.Second); err != nil {
		h.warn("runmethod %q failed for device %d: %v", method, device, err)
	}
}

// pressButton presses a button in an app's settings page.
func (h *hub) pressButton(app int, button string) {
	b := url.QueryEscape(button)
	body := fmt.Sprintf("id=%d&name=%s&settings%%5B%s%%5D=clicked&%s.type=button", app, b, b, b)
	if _, err := h.do(http.MethodPost, "/installedapp/btn", "application/x-www-form-urlencoded", []byte(body), 10*time.Second); err != nil {
		h.warn("app %d button %q failed: %v", app, button, err)
	}
}

func (h *hub) ensureMotionRuleStarted() {
	page, err := h.do(http.MethodGet, fmt.Sprintf("/installedapp/configure/json/%d", motionRuleApp), "", nil, 10*time.Second)
	if err != nil {
		h.warn("could not read motion rule %d's state: %v", motionRuleApp, err)
		return
	}
	if bytes.Contains(page, []byte("(Stopped)")) {
		h.pressButton(motionRuleApp, "stopRule")
		h.sleep(500 * time.Millisecond)
	}
}

// clearSuppressionJobs deletes the hold-off timers. A failure here is not
// counted: the marker switched on afterwards still schedules the timer asked for.
func (h *hub) clearSuppressionJobs() {
	data, err := h.do(http.MethodGet, "/logs/json", "", nil, 10*time.Second)
	if err != nil {
		return
	}
	var logs struct {
		Jobs []struct {
			ID   any    `json:"id"`
			Link string `json:"link"`
		} `json:"jobs"`
	}
	if json.Unmarshal(data, &logs) != nil {
		return
	}
	for _, app := range suppressionApps {
		link := fmt.Sprintf("/installedapp/configure/%d", app)
		for _, j := range logs.Jobs {
			if j.Link != link || j.ID == nil {
				continue
			}
			id := fmt.Sprint(j.ID)
			_, _ = h.do(http.MethodGet, "/hub/advanced/deleteScheduledJob?id="+url.QueryEscape(id), "", nil, 10*time.Second)
		}
	}
}

func (h *hub) clearSuppressionMarkers() {
	for _, d := range []int{98, 101, 102} {
		h.runMethod(d, "off")
	}
}

// nightModeOn reads device 115's switch; unreadable counts as on, so it is ended.
func (h *hub) nightModeOn() bool {
	data, err := h.do(http.MethodGet, fmt.Sprintf("/device/fullJson/%d", nightDevice), "", nil, 10*time.Second)
	if err != nil {
		fmt.Fprintf(h.out, "warning: could not read night mode's state: %v\n", err)
		return true
	}
	// The payload carries both "deprecated" and "DEPRECATED"; only the switch is read.
	var dev struct {
		Device struct {
			CurrentStates map[string]struct {
				Value any `json:"value"`
			} `json:"currentStates"`
		} `json:"device"`
	}
	if err := json.Unmarshal(data, &dev); err != nil {
		fmt.Fprintf(h.out, "warning: could not read night mode's state: %v\n", err)
		return true
	}
	return fmt.Sprint(dev.Device.CurrentStates["switch"].Value) == "on"
}

// stopNightMode ends night mode before anything else takes the lights. The app
// turns the color bulbs off, restages them to normal white about 3 s later and
// resumes rule 94; waiting for that keeps a following "on" from coming up in
// the night color.
func (h *hub) stopNightMode() {
	if h.nightModeOn() {
		h.runMethod(nightDevice, "off")
		h.sleep(5 * time.Second)
	}
}

func (h *hub) startSuppression(marker int) {
	h.stopNightMode()
	h.clearSuppressionJobs()
	h.runMethod(allLights, "off")
	h.clearSuppressionMarkers()
	h.runMethod(marker, "off")
	h.sleep(500 * time.Millisecond)
	h.runMethod(marker, "on")
	h.ensureMotionRuleStarted()
}

func runLights(ctx context.Context, out io.Writer, args []string) (string, error) {
	b := builtins["lights"]
	if len(args) == 0 {
		return "", usageError(b)
	}
	h := &hub{ctx: ctx, out: out, client: newHubClient(), sleep: func(d time.Duration) { hubPause(ctx, d) }}
	var do func() string
	switch args[0] {
	case "on":
		if len(args) != 1 {
			return "", usageError(b)
		}
		do = func() string {
			h.stopNightMode()
			h.clearSuppressionJobs()
			h.clearSuppressionMarkers()
			h.ensureMotionRuleStarted()
			h.pressButton(motionRuleApp, "runAction")
			return "Living room lights ON (motion-controlled; timers reset)"
		}
	case "off":
		minutes := 30
		if len(args) == 2 {
			minutes, _ = strconv.Atoi(args[1])
		}
		marker, ok := offMarkers[minutes]
		if !ok || len(args) > 2 {
			return "", usageError(b)
		}
		do = func() string {
			h.startSuppression(marker)
			return fmt.Sprintf("Living room suppressed for %d min", minutes)
		}
	case "night":
		color, minutes := "warm", 120
		rest := args[1:]
		if len(rest) > 0 && !isNumber(rest[0]) {
			color, rest = rest[0], rest[1:]
		}
		if len(rest) > 0 {
			minutes, _ = strconv.Atoi(rest[0])
			rest = rest[1:]
		}
		if !contains(nightColors, color) || minutes < 1 || minutes > 1440 || len(rest) > 0 {
			return "", usageError(b)
		}
		do = func() string {
			// Night replaces any hold-off: clear it so rule 94 isn't left gated
			// after night mode ends and resumes it.
			h.clearSuppressionJobs()
			h.clearSuppressionMarkers()
			h.runMethod(nightDevice, "night", hubArg{"ENUM", color}, hubArg{"NUMBER", minutes})
			ends := time.Now().Add(time.Duration(minutes) * time.Minute).Format("3:04 PM")
			return fmt.Sprintf("Night mode: %s for %d min (until %s). Motion turns the color bulbs on dim; everything else stays off.", color, minutes, ends)
		}
	default:
		return "", usageError(b)
	}

	if _, err := h.do(http.MethodGet, fmt.Sprintf("/device/fullJson/%d", nightDevice), "", nil, 5*time.Second); err != nil {
		var login hubLoginError
		if errors.As(err, &login) {
			return "", fmt.Errorf("nothing was changed: %v", err)
		}
		return "", fmt.Errorf("can't reach the Hubitat hub at %s, nothing was changed. This only works on the home network. (%v)", hubitatBase, err)
	}
	done := do()
	if ctx.Err() != nil {
		return "", errors.New("stopped part way: some of the lights may have changed")
	}
	if h.failures > 0 {
		return "", fmt.Errorf("%s did not fully apply: %d hub call(s) failed (warnings above)", strings.Join(args, " "), h.failures)
	}
	return done, nil
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
