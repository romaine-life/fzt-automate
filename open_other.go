//go:build !windows

package main

import (
	"fmt"
	"os"
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

func shellOpenMain([]string) int {
	fmt.Fprintln(os.Stderr, errWindowsOnly)
	return 1
}
