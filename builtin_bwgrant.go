package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func init() {
	register(&builtin{name: "bw-grant", usage: "[minutes] | lock", terminal: true,
		about: "unlock the Bitwarden CLI for a Claude session for a few minutes (15 by default), then relock it", run: runBwGrant})
	register(&builtin{name: "bw-relock", usage: "<expiry>", about: "relock Bitwarden at the expiry a grant set, unless a later grant moved it", run: runBwRelock})
}

// The session key goes to ~/.bw-session, which a reader sets BW_SESSION from.
// The CLI can't scope a session to one item, so while it's unlocked the whole
// vault is readable: the time limit is the only control.
func bwFiles() (session, expires string) {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".bw-session"), filepath.Join(home, ".bw-session.expires")
}

func bwProgram() (string, error) {
	if p, err := exec.LookPath("bw"); err == nil {
		return p, nil
	}
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Packages", "Bitwarden.CLI_*", "bw.exe"))
	if len(matches) > 0 {
		return matches[0], nil
	}
	return "", errors.New("the Bitwarden CLI isn't installed: winget install --id Bitwarden.CLI --exact")
}

func bwLock(bw string) (string, error) {
	_ = exec.Command(bw, "lock").Run()
	session, expires := bwFiles()
	os.Remove(session)
	os.Remove(expires)
	return "Bitwarden locked.", nil
}

func runBwGrant(ctx context.Context, out io.Writer, args []string) (string, error) {
	bw, err := bwProgram()
	if err != nil {
		return "", err
	}
	minutes := 15
	switch {
	case len(args) == 1 && args[0] == "lock":
		return bwLock(bw)
	case len(args) == 1:
		if minutes, err = strconv.Atoi(args[0]); err != nil || minutes < 1 {
			return "", usageError(builtins["bw-grant"])
		}
	case len(args) > 1:
		return "", usageError(builtins["bw-grant"])
	}

	statusOut, err := exec.CommandContext(ctx, bw, "status").Output()
	if err != nil {
		return "", fmt.Errorf("bw status failed: %w", err)
	}
	var status struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(statusOut, &status)
	// Signing in uses the personal API key: the CLI can't do a passkey or security-key second step.
	if status.Status == "unauthenticated" {
		login := exec.CommandContext(ctx, bw, "login", "--apikey")
		login.Stdin, login.Stdout, login.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := login.Run(); err != nil {
			return "", errors.New("Bitwarden sign-in failed")
		}
	}
	unlock := exec.CommandContext(ctx, bw, "unlock", "--raw")
	var key bytes.Buffer
	unlock.Stdin, unlock.Stdout, unlock.Stderr = os.Stdin, &key, os.Stderr
	if err := unlock.Run(); err != nil || strings.TrimSpace(key.String()) == "" {
		return "", errors.New("Bitwarden did not unlock")
	}
	sessionFile, expiresFile := bwFiles()
	if err := os.WriteFile(sessionFile, bytes.TrimSpace(key.Bytes()), 0o600); err != nil {
		return "", err
	}
	expires := time.Now().Add(time.Duration(minutes) * time.Minute)
	stamp := expires.Format(time.RFC3339)
	if err := os.WriteFile(expiresFile, []byte(stamp), 0o600); err != nil {
		return "", err
	}
	_ = exec.CommandContext(ctx, bw, "sync", "--session", strings.TrimSpace(key.String())).Run()

	relock := exec.Command(selfProgram(), "bw-relock", stamp)
	if err := startDetached(relock); err != nil {
		bwLock(bw)
		return "", fmt.Errorf("the relock timer didn't start, so Bitwarden is locked again: %w", err)
	}
	return fmt.Sprintf("Bitwarden unlocked until %s. Relock early: bw-grant lock", expires.Format("3:04 PM")), nil
}

// runBwRelock waits until the expiry its grant set, then locks unless a later
// grant has moved the expiry on.
func runBwRelock(ctx context.Context, out io.Writer, args []string) (string, error) {
	if len(args) != 1 {
		return "", usageError(builtins["bw-relock"])
	}
	at, err := time.Parse(time.RFC3339, args[0])
	if err != nil {
		return "", usageError(builtins["bw-relock"])
	}
	time.Sleep(time.Until(at) + 5*time.Second)
	_, expiresFile := bwFiles()
	if data, err := os.ReadFile(expiresFile); err == nil {
		if later, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); err == nil && later.After(at) {
			return "", nil
		}
	}
	bw, err := bwProgram()
	if err != nil {
		return "", err
	}
	return bwLock(bw)
}
