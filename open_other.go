//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

// openURL hands a URL to the desktop.
func openURL(target string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, withScheme(target)).Start()
}
