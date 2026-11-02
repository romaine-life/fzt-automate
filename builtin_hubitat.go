package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func init() {
	register(&builtin{name: "hubitat-device", usage: "create <name> [type] [auto-off seconds] | update <id> <auto-off seconds>",
		about: "create or change a Hubitat virtual device, through the LocalDeviceCreatorAPI app", run: runHubitatDevice})
}

// hubitatCreatorConfig is ~/.hubitat/config.json: the hub and the
// LocalDeviceCreatorAPI app (shell-config scripts/hubitat/README.md).
type hubitatCreatorConfig struct {
	HubIP       string `json:"hubIp"`
	AppID       any    `json:"creatorAppId"`
	AccessToken string `json:"creatorAccessToken"`
}

func runHubitatDevice(ctx context.Context, out io.Writer, args []string) (string, error) {
	b := builtins["hubitat-device"]
	if len(args) < 2 {
		return "", usageError(b)
	}
	payload := map[string]any{}
	var op string
	switch args[0] {
	case "create":
		if len(args) > 4 {
			return "", usageError(b)
		}
		op = "createDevice"
		payload["deviceName"] = args[1]
		payload["deviceType"] = "Virtual Switch"
		if len(args) > 2 {
			payload["deviceType"] = args[2]
		}
		if len(args) > 3 {
			n, err := strconv.Atoi(args[3])
			if err != nil || n <= 0 {
				return "", usageError(b)
			}
			payload["autoOffSeconds"] = n
		}
	case "update":
		id, err := strconv.Atoi(args[1])
		if err != nil || len(args) != 3 {
			return "", usageError(b)
		}
		n, err := strconv.Atoi(args[2])
		if err != nil || n <= 0 {
			return "", usageError(b)
		}
		op = "updateDevice"
		payload["deviceId"] = id
		payload["autoOffSeconds"] = n
	default:
		return "", usageError(b)
	}

	path := filepath.Join(os.Getenv("USERPROFILE"), ".hubitat", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no Hubitat config at %s", path)
	}
	var cfg hubitatCreatorConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("http://%s/apps/api/%v/%s?access_token=%s", cfg.HubIP, cfg.AppID, op, cfg.AccessToken)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("the hub didn't answer: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Success  bool   `json:"success"`
		DeviceID any    `json:"deviceId"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("the hub answered HTTP %d with no result", resp.StatusCode)
	}
	if !result.Success {
		return "", fmt.Errorf("the hub refused: %s", result.Error)
	}
	if op == "createDevice" {
		return fmt.Sprintf("created %q as device %v", args[1], result.DeviceID), nil
	}
	return fmt.Sprintf("updated device %v", result.DeviceID), nil
}
