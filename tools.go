package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Published tools (shell-config-profile-home claude/topics/home-tools.md):
// \\NelsonNAS\tools\<tool>\<version>\ holds one release each, and
// <tool>\current.json names the live one. Synology Drive keeps a copy of the
// share at ~\tools on each PC.
var (
	toolsNas   = `\\NelsonNAS\tools`
	toolsLocal = filepath.Join(os.Getenv("USERPROFILE"), "tools")
)

type toolCurrent struct {
	Version string `json:"version"`
	Entry   string `json:"entry"`
}

func readToolCurrent(root, name string) (toolCurrent, bool) {
	var c toolCurrent
	data, err := os.ReadFile(filepath.Join(root, name, "current.json"))
	if err != nil || json.Unmarshal(data, &c) != nil || c.Version == "" {
		return c, false
	}
	return c, true
}

// toolPath is a file in a tool's current release, its program when file is
// empty. The NAS names the release (the Drive copy only when the NAS can't be
// read, since Drive can fall behind); the file comes from the Drive copy when
// it holds that release, else from the NAS.
func toolPath(name, file string) (string, error) {
	c, ok := readToolCurrent(toolsNas, name)
	if !ok {
		if c, ok = readToolCurrent(toolsLocal, name); !ok {
			return "", fmt.Errorf("tool %s is not published in %s", name, toolsNas)
		}
	}
	if file == "" {
		file = c.Entry
	}
	for _, root := range []string{toolsLocal, toolsNas} {
		p := filepath.Join(root, name, c.Version, file)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s %s has no %s in %s or %s", name, c.Version, file, toolsLocal, toolsNas)
}

// toolConfigDir is a tool's shared, unversioned settings folder, from the NAS
// (the Drive copy when the NAS can't be read).
func toolConfigDir(name string) (string, error) {
	for _, root := range []string{toolsNas, toolsLocal} {
		dir := filepath.Join(root, name, "config")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
	}
	return "", fmt.Errorf("tool %s has no config folder in %s", name, toolsNas)
}
