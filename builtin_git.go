package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
)

func init() {
	register(&builtin{name: "gitprune", about: "delete every local branch but main and the one checked out, in the tab's repo", run: runGitPrune})
	register(&builtin{name: "gitrepo", about: "open the tab's repo on GitHub", run: runGitRepo})
}

var errNoTabFolder = errors.New("the tab you're on hasn't said which folder it's in")

// git runs git in dir and returns its output, its error message on failure.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", errors.New(text)
	}
	return text, nil
}

func runGitPrune(ctx context.Context, out io.Writer, args []string) (string, error) {
	dir := pickDir(ctx)
	if dir == "" {
		return "", errNoTabFolder
	}
	list, err := git(ctx, dir, "branch", "--format=%(refname:short)")
	if err != nil {
		return "", err
	}
	current, _ := git(ctx, dir, "branch", "--show-current")
	var deleted []string
	for _, b := range strings.Split(list, "\n") {
		b = strings.TrimSpace(b)
		if b == "" || b == "main" || b == current {
			continue
		}
		if _, err := git(ctx, dir, "branch", "-D", b); err != nil {
			return "", fmt.Errorf("deleting %s: %w", b, err)
		}
		deleted = append(deleted, b)
	}
	if len(deleted) == 0 {
		return "no branches to delete", nil
	}
	return fmt.Sprintf("deleted %d: %s", len(deleted), strings.Join(deleted, ", ")), nil
}

var scpRemote = regexp.MustCompile(`^[^@/]+@([^:/]+):(.+)$`)

// webURL is a remote's address in a browser: an ssh remote
// (git@github.com:owner/repo) becomes https, and .git goes.
func webURL(remote string) string {
	if m := scpRemote.FindStringSubmatch(remote); m != nil {
		remote = "https://" + m[1] + "/" + m[2]
	}
	remote = strings.TrimPrefix(remote, "ssh://git@")
	if !strings.Contains(remote, "://") {
		remote = "https://" + remote
	}
	return strings.TrimSuffix(remote, ".git")
}

func runGitRepo(ctx context.Context, out io.Writer, args []string) (string, error) {
	dir := pickDir(ctx)
	if dir == "" {
		return "", errNoTabFolder
	}
	remote, err := git(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", fmt.Errorf("%s has no origin remote", dir)
	}
	u := webURL(remote)
	if err := openURL(u); err != nil {
		return "", err
	}
	return "opened " + u, nil
}
