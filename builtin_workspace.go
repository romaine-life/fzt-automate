package main

// The VS Code workspace file and its profiles (FZT-71; these were at's ws*
// commands and shell-config's scripts/workspace/ws-edit and ws-load). The
// workspace is D:/workspace/perpetual.code-workspace (or $WORKSPACE_FILE); its
// first folder is the anchor, kept by every edit. A profile is
// D:\profiles\shell-config-profile-<PROFILE_NUM>\ws-profiles\<name>\<name>.code-workspace,
// whose folders after the first are its repos.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func init() {
	register(&builtin{name: "workspaces", about: "folder: the workspace and every profile, each opened in VS Code", rows: workspaceRows})
	register(&builtin{name: "ws-profiles", usage: "load|inject|clone", about: "folder: the workspace profiles, picking one loads, adds or replaces its repos", rows: wsProfileRows})
	register(&builtin{name: "ws-entries", about: "folder: the workspace's repos, picking one removes it", rows: wsEntryRows})
	register(&builtin{name: "workspace", usage: "load|inject|clone <profile> | remove <path> | reset",
		about: "change the workspace's repos", run: runWorkspace})
}

func workspaceFile() string {
	if f := os.Getenv("WORKSPACE_FILE"); f != "" {
		return f
	}
	return "D:/workspace/perpetual.code-workspace"
}

// profilesRoot is a profile number's ws-profiles folder.
var profilesRoot = func(n string) string {
	return filepath.Join(`D:\profiles`, "shell-config-profile-"+n, "ws-profiles")
}

func profilesDir() (string, error) {
	n := os.Getenv("PROFILE_NUM")
	if n == "" {
		return "", errors.New("PROFILE_NUM is not set, so this PC's profile isn't known")
	}
	return profilesRoot(n), nil
}

// codeProgram is VS Code's own program; code.cmd would start cmd.exe for it.
func codeProgram() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Microsoft VS Code", "Code.exe")
}

// workspaceProfiles is every profile's name and file, by name.
func workspaceProfiles() (map[string]string, error) {
	dir, err := profilesDir()
	if err != nil {
		return nil, err
	}
	found := map[string]string{}
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".code-workspace") {
			found[strings.TrimSuffix(d.Name(), ".code-workspace")] = p
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("no workspace profiles in %s", dir)
	}
	return found, nil
}

func sortedNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func workspaceRows(context.Context, []string) ([]folderRow, error) {
	rows := []folderRow{{name: "perpetual", description: "the workspace", action: joinArgs(codeProgram(), workspaceFile())}}
	profiles, err := workspaceProfiles()
	if err != nil {
		return rows, nil // the workspace still opens on a PC with no profiles
	}
	for _, n := range sortedNames(profiles) {
		rows = append(rows, folderRow{name: n, description: "profile", action: joinArgs(codeProgram(), profiles[n])})
	}
	return rows, nil
}

func wsProfileRows(_ context.Context, args []string) ([]folderRow, error) {
	if len(args) != 1 || (args[0] != "load" && args[0] != "inject" && args[0] != "clone") {
		return nil, usageError(builtins["ws-profiles"])
	}
	profiles, err := workspaceProfiles()
	if err != nil {
		return nil, err
	}
	var rows []folderRow
	for _, n := range sortedNames(profiles) {
		rows = append(rows, folderRow{name: n, action: joinArgs("workspace", args[0], n)})
	}
	return rows, nil
}

func wsEntryRows(context.Context, []string) ([]folderRow, error) {
	ws, err := readWorkspace(workspaceFile())
	if err != nil {
		return nil, err
	}
	var rows []folderRow
	for _, p := range ws.paths()[min(1, len(ws.folders)):] {
		rows = append(rows, folderRow{name: filepath.Base(p), description: p, action: joinArgs("workspace", "remove", p)})
	}
	if len(rows) == 0 {
		return []folderRow{{name: "no repos in the workspace"}}, nil
	}
	return rows, nil
}

// workspace is a .code-workspace file with its keys kept in order, so an edit
// changes only its folders.
type workspace struct {
	keys    []string
	values  map[string]json.RawMessage
	folders []map[string]any
}

var trailingComma = regexp.MustCompile(`,(\s*[}\]])`)

func readWorkspace(path string) (*workspace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workspace file not found: %s", path)
	}
	// VS Code writes JSONC: trailing commas are allowed.
	data = trailingComma.ReplaceAll(data, []byte("$1"))
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("%s is not a JSON object", path)
	}
	ws := &workspace{values: map[string]json.RawMessage{}}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		key, _ := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		ws.keys = append(ws.keys, key)
		ws.values[key] = v
	}
	if raw, ok := ws.values["folders"]; ok {
		if err := json.Unmarshal(raw, &ws.folders); err != nil {
			return nil, fmt.Errorf("%s: folders: %w", path, err)
		}
	}
	return ws, nil
}

func (ws *workspace) paths() []string {
	var out []string
	for _, f := range ws.folders {
		p, _ := f["path"].(string)
		out = append(out, p)
	}
	return out
}

func (ws *workspace) write(path string) error {
	folders, _ := json.Marshal(ws.folders)
	if _, ok := ws.values["folders"]; !ok {
		ws.keys = append(ws.keys, "folders")
	}
	ws.values["folders"] = folders
	var b bytes.Buffer
	b.WriteString("{")
	for i, k := range ws.keys {
		if i > 0 {
			b.WriteString(",")
		}
		key, _ := json.Marshal(k)
		b.Write(key)
		b.WriteString(":")
		b.Write(ws.values[k])
	}
	b.WriteString("}")
	var out bytes.Buffer
	if err := json.Indent(&out, b.Bytes(), "", "  "); err != nil {
		return err
	}
	out.WriteString("\n")
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func normalizePath(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// anchor is the folders up to and including the first, which edits keep.
func (ws *workspace) anchor() []map[string]any {
	return ws.folders[:min(1, len(ws.folders))]
}

func (ws *workspace) add(p string) bool {
	p = normalizePath(p)
	for _, existing := range ws.paths() {
		if normalizePath(existing) == p {
			return false
		}
	}
	ws.folders = append(ws.folders, map[string]any{"path": p})
	return true
}

// profileRepos is a profile's folders after its first.
func profileRepos(name string) ([]string, error) {
	profiles, err := workspaceProfiles()
	if err != nil {
		return nil, err
	}
	file, ok := profiles[name]
	if !ok {
		return nil, fmt.Errorf("no workspace profile named %s", name)
	}
	src, err := readWorkspace(file)
	if err != nil {
		return nil, err
	}
	repos := src.paths()
	if len(repos) < 2 {
		return nil, fmt.Errorf("profile %s has no repos", name)
	}
	return repos[1:], nil
}

func runWorkspace(ctx context.Context, out io.Writer, args []string) (string, error) {
	b := builtins["workspace"]
	if len(args) == 0 {
		return "", usageError(b)
	}
	file := workspaceFile()
	ws, err := readWorkspace(file)
	if err != nil {
		return "", err
	}
	var done string
	switch {
	case args[0] == "reset" && len(args) == 1:
		ws.folders = ws.anchor()
		done = "Workspace cleared."
	case args[0] == "remove" && len(args) == 2:
		target := normalizePath(args[1])
		kept := ws.anchor()
		for _, f := range ws.folders[len(kept):] {
			if p, _ := f["path"].(string); normalizePath(p) != target {
				kept = append(kept, f)
			}
		}
		ws.folders = kept
		done = "Removed " + filepath.Base(args[1]) + " from the workspace."
	case (args[0] == "load" || args[0] == "inject" || args[0] == "clone") && len(args) == 2:
		repos, err := profileRepos(args[1])
		if err != nil {
			return "", err
		}
		if args[0] != "inject" {
			ws.folders = ws.anchor()
		}
		for _, r := range repos {
			ws.add(r)
		}
		done = fmt.Sprintf("Workspace has %s's %d repos.", args[1], len(repos))
		if args[0] == "inject" {
			done = fmt.Sprintf("Added %s's repos to the workspace.", args[1])
		}
	default:
		return "", usageError(b)
	}
	if err := ws.write(file); err != nil {
		return "", err
	}
	return done, nil
}
