package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Legacy publications remain on the NAS. On Windows,
// home-sync owns executable releases under Program Files instead.
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

// homeSyncTools is home-sync's record of the tools it installed on this PC, each
// with its program, copied from state.json: the one place a tool's program is
// named (HOMECFG-213). A variable so tests can point it elsewhere.
var homeSyncTools = filepath.Join(os.Getenv("USERPROFILE"), ".home-sync", "tools.json")

// toolPath is a file in a tool's current release, its program when file is
// empty. On Windows every tool is home-sync's: its record names the program, and
// the file is in the tool's current folder under Program Files, never a stale
// copy elsewhere. Elsewhere the NAS names the release (the Drive copy only when
// the NAS can't be read, since Drive can fall behind); the file comes from the
// Drive copy when it holds that release, else from the NAS.
func toolPath(name, file string) (string, error) {
	if runtime.GOOS == "windows" {
		return managedToolPath(name, file)
	}
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

func managedToolPath(name, file string) (string, error) {
	data, err := os.ReadFile(homeSyncTools)
	if err != nil {
		return "", fmt.Errorf("can't read home-sync's list of installed tools: %v; run home-sync tools", err)
	}
	var installed []struct {
		Tool  string `json:"tool"`
		Entry string `json:"entry"`
	}
	if err := json.Unmarshal(data, &installed); err != nil {
		return "", fmt.Errorf("home-sync's list of installed tools (%s) is unreadable: %v", homeSyncTools, err)
	}
	for _, tool := range installed {
		if tool.Tool != name {
			continue
		}
		if file == "" {
			if tool.Entry == "" {
				return "", fmt.Errorf("home-sync's list doesn't name %s's program yet; run home-sync tools", name)
			}
			file = tool.Entry
		}
		p := filepath.Join(os.Getenv("ProgramFiles"), "at-home", name, "current", file)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
		return "", fmt.Errorf("installed tool %s is missing %s; run home-sync tools", name, p)
	}
	return "", fmt.Errorf("%s isn't a tool home-sync installs on this PC (not in %s); run home-sync tools", name, homeSyncTools)
}

// configNas holds each tool's shared settings as its own share, <name>-config, which the
// home cluster can mount too (spt-config, SPT-35).
var configNas = `\\NelsonNAS`

// toolConfigDir is a tool's shared, unversioned settings folder, from the NAS
// (the Drive copy when the NAS can't be read).
func toolConfigDir(name string) (string, error) {
	share := filepath.Join(configNas, name+"-config")
	for _, dir := range []string{share, filepath.Join(toolsLocal, name, "config")} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
	}
	return "", fmt.Errorf("tool %s has no config folder %s", name, share)
}
