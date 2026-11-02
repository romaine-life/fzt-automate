package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func init() {
	register(&builtin{name: "authromaine", terminal: true,
		about: "sign in to auth.romaine.life (device flow) for the menu's sync; the token lasts about 24 h", run: runAuthRomaine})
}

var authBase = "https://auth.romaine.life"

// A fresh misc_identifier each time: a common concrete singular noun not used before.
var authNouns = []string{"pelican", "kayak", "maple", "harbor", "quilt", "meadow", "willow", "pebble", "tundra", "orchard",
	"glacier", "dune", "reef", "fjord", "prairie", "canyon", "harvest", "bramble", "cedar", "birch",
	"heron", "badger", "ferret", "sparrow", "cricket", "beacon", "trellis", "satchel", "goblet", "cradle"}

func authJSON(ctx context.Context, method, path string, body any, out any) (int, string, error) {
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, authBase+path, r)
	if err != nil {
		return 0, "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 300 && out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, string(data), err
		}
	}
	return resp.StatusCode, string(data), nil
}

// runAuthRomaine gets an auth.romaine.life token by the CLI device flow and
// saves it where fzt-automate and devctl read it ({"token": ...} in
// auth-token.json in the config folder).
func runAuthRomaine(ctx context.Context, out io.Writer, args []string) (string, error) {
	var guidance struct {
		Previous []string `json:"previous_misc_identifiers"`
	}
	if _, _, err := authJSON(ctx, http.MethodGet, "/api/cli/requester-guidance", nil, &guidance); err != nil {
		return "", fmt.Errorf("auth.romaine.life didn't answer: %w", err)
	}
	var fresh []string
	for _, n := range authNouns {
		if !contains(guidance.Previous, n) {
			fresh = append(fresh, n)
		}
	}
	noun := fmt.Sprintf("noun%d", rand.Intn(99999))
	if len(fresh) > 0 {
		noun = fresh[rand.Intn(len(fresh))]
	}
	host, _ := os.Hostname()
	var dev struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		URI        string `json:"verification_uri_complete"`
		Interval   int    `json:"interval"`
		ExpiresIn  int    `json:"expires_in"`
	}
	status, body, err := authJSON(ctx, http.MethodPost, "/api/cli/device", map[string]string{
		"where_happening": fmt.Sprintf("fzt-automate on %s, refreshing the at menu's auth token.", host),
		"intended_use":    "Sync my fzt-automate menu tree from fzt-frontend.romaine.life into the local cache.",
		"misc_identifier": noun,
	}, &dev)
	if err != nil || status >= 300 {
		return "", fmt.Errorf("starting the sign-in failed: HTTP %d %s %v", status, strings.TrimSpace(body), err)
	}

	fmt.Fprintf(out, "\nApprove sign-in in your browser (sign in as your personal identity):\n  %s\n  code: %s\n", dev.URI, dev.UserCode)
	if err := openURL(dev.URI); err != nil {
		fmt.Fprintf(out, "(the browser didn't open: %v)\n", err)
	}
	fmt.Fprintln(out, "Waiting for approval...")

	interval := time.Duration(dev.Interval) * time.Second
	if interval < 3*time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dev.ExpiresIn) * time.Second)
	grant := map[string]string{"grant_type": "urn:ietf:params:oauth:grant-type:device_code", "device_code": dev.DeviceCode}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
		var tok struct {
			Token string `json:"token"`
		}
		status, body, err := authJSON(ctx, http.MethodPost, "/api/cli/token", grant, &tok)
		switch {
		case err == nil && status < 300 && tok.Token != "":
			return saveAuthToken(tok.Token)
		case strings.Contains(body, "authorization_pending") || strings.Contains(body, "slow_down"):
		case strings.Contains(body, "expired_token") || strings.Contains(body, "denied"):
			return "", fmt.Errorf("the sign-in was refused or expired: %s", strings.TrimSpace(body))
		}
		// Anything else is taken as passing: keep asking until the deadline.
	}
	return "", errors.New("timed out waiting for the browser's approval")
}

func saveAuthToken(token string) (string, error) {
	dir := configDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, _ := json.Marshal(map[string]string{"token": token})
	path := filepath.Join(dir, "auth-token.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return "Token saved to " + path + ". The menu syncs with it on its next open (or S).", nil
}
