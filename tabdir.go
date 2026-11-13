package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// A pick runs in the folder of the tab the menu was opened over, when that tab
// has said which folder it is in: a shell reports it with OSC 7 (tmux's
// pane_path), and a Claude Code session through its hooks (@claude-cwd).
// tmux on Windows can't read a process's folder itself (pane_current_path is
// empty there).

type dirKey struct{}

func withDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, dirKey{}, dir)
}

// pickDir is the folder a built-in runs in: the tab's, else empty.
func pickDir(ctx context.Context) string {
	dir, _ := ctx.Value(dirKey{}).(string)
	return dir
}

// tabEnv is the environment a folder's program gets in the popup: the active
// pane of the tab it is over as TMUX_PANE, as a program in that pane has it,
// and the popup's client as TMUX_CLIENT (TMUX-206). tmux is asked once, when a
// folder first runs a program.
func tabEnv(window, client string) func() []string {
	return sync.OnceValue(func() []string {
		var env []string
		if window != "" {
			if out, err := exec.Command("tmux", "display", "-p", "-t", window, "#{pane_id}").Output(); err == nil {
				if pane := strings.TrimSpace(string(out)); pane != "" {
					env = append(env, "TMUX_PANE="+pane)
				}
			}
		}
		if client != "" {
			env = append(env, "TMUX_CLIENT="+client)
		}
		return env
	})
}

// tabFolder asks tmux for the folder of the active pane in window.
func tabFolder(window string) string {
	if window == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display", "-p", "-t", window, "#{@codex-cwd}\t#{@claude-cwd}\t#{pane_path}").Output()
	if err != nil {
		return ""
	}
	return cleanTabFolder(string(out))
}

// cleanTabFolder reads tmux's "<pane_path>\t<@claude-cwd>": the shell's report
// first, a file:// URL in OSC 7's form, with doubled slashes collapsed.
func cleanTabFolder(line string) string {
	for _, p := range strings.Split(strings.TrimRight(line, "\r\n"), "\t") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "file://") {
			p = strings.TrimPrefix(p, "file://")
			if i := strings.IndexByte(p, '/'); i >= 0 {
				p = p[i:] // drop the host
			}
			if len(p) > 2 && p[0] == '/' && p[2] == ':' {
				p = p[1:] // /D:/repos
			}
		}
		for strings.Contains(p, "//") {
			p = strings.ReplaceAll(p, "//", "/")
		}
		return filepath.Clean(filepath.FromSlash(p))
	}
	return ""
}
