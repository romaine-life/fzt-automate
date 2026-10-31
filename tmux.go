package main

import "regexp"

// tmuxSwitchOnDraw is the tmux-windows-fork new-window option that switches
// client to the new window once its pane has drawn, and waits until then.
func tmuxSwitchOnDraw(client string) []string {
	return []string{"-W", client}
}

var hasScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// withScheme gives a bookmark saved without one ("photos.google.com") https.
func withScheme(target string) string {
	if hasScheme.MatchString(target) {
		return target
	}
	return "https://" + target
}
