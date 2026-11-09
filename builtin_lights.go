package main

// The living room lights, through home-hub (nelsong6/home-hub HUB-2), which runs the
// living room's motion lighting, hold-offs and night mode: one PUT /living-room, signed
// in as this machine. This was calls to the Hubitat's admin pages (FZT-71), and before
// that romaine-life/at's lib/living-room-lights.ps1.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func init() {
	register(&builtin{name: "lights", usage: "on | off [30|60|120] | night [warm|red|amber|blue] [minutes]",
		about: "the living room lights: motion-controlled, held off for a while, or night mode", run: runLights})
}

var homeHubBase = "https://home-hub.home.romaine.life"

var nightColors = []string{"warm", "red", "amber", "blue"}

// livingRoom is home-hub's answer: the mode it's in now and until when.
type livingRoom struct {
	Mode  string    `json:"mode"`
	Until time.Time `json:"until"`
	Color string    `json:"color"`
}

func runLights(ctx context.Context, out io.Writer, args []string) (string, error) {
	b := builtins["lights"]
	if len(args) == 0 {
		return "", usageError(b)
	}
	req := map[string]any{"mode": args[0]}
	switch args[0] {
	case "on":
		if len(args) != 1 {
			return "", usageError(b)
		}
	case "off":
		minutes := 30
		if len(args) == 2 {
			minutes, _ = strconv.Atoi(args[1])
		}
		if (minutes != 30 && minutes != 60 && minutes != 120) || len(args) > 2 {
			return "", usageError(b)
		}
		req["minutes"] = minutes
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
		req["color"], req["minutes"] = color, minutes
	default:
		return "", usageError(b)
	}

	room, err := setLivingRoom(ctx, req)
	if err != nil {
		return "", fmt.Errorf("nothing was changed: %w", err)
	}
	until := room.Until.Local().Format("3:04 PM")
	switch room.Mode {
	case "night":
		return fmt.Sprintf("Night mode: %s for %d min (until %s). Motion turns the color bulbs on dim; everything else stays off.", room.Color, req["minutes"], until), nil
	case "held-off":
		return fmt.Sprintf("Living room held off for %d min (until %s)", req["minutes"], until), nil
	default:
		return "Living room lights ON (motion-controlled; timers reset)", nil
	}
}

// setLivingRoom asks home-hub for a mode and returns the living room as it now is.
func setLivingRoom(ctx context.Context, req map[string]any) (livingRoom, error) {
	credentials, err := homeToken(ctx, "home-hub")
	if err != nil {
		return livingRoom{}, fmt.Errorf("this machine couldn't sign in to home-hub: %w", err)
	}
	body, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPut, homeHubBase+"/living-room", bytes.NewReader(body))
	if err != nil {
		return livingRoom{}, err
	}
	r.Header.Set("Authorization", "Bearer "+credentials.Token)
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return livingRoom{}, fmt.Errorf("can't reach home-hub at %s: %w", homeHubBase, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return livingRoom{}, fmt.Errorf("home-hub answered %s: %s", resp.Status, strings.TrimSpace(string(answer)))
	}
	var room livingRoom
	if err := json.Unmarshal(answer, &room); err != nil {
		return livingRoom{}, errors.New("home-hub's answer was unreadable")
	}
	return room, nil
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
