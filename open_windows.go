//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// openURL hands a URL (or any registered protocol, such as spotify:) to the
// desktop, as Start-Process does.
func openURL(target string) error {
	return shellOpen(withScheme(target), "", "")
}

// shellOpen opens a program, file or link as a double-click would, through a
// copy of this program with no console. Opened from the menu itself, a
// program that attaches to its parent's console (mGBA) joins the menu's, and
// ends when tmux closes it with the menu. params and dir may be empty.
func shellOpen(target, params, dir string) error {
	cmd := exec.Command(selfProgram(), shellOpenArg, target, params, dir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

// shellOpenMain is the opener: `--shell-open <target> <params> <dir>`.
func shellOpenMain(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: fzt-automate "+shellOpenArg+" <target> <arguments> <folder>")
		return 2
	}
	optional := func(s string) *uint16 {
		if s == "" {
			return nil
		}
		return windows.StringToUTF16Ptr(s)
	}
	err := withCOM(func() error {
		return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(args[0]), optional(args[1]), optional(args[2]), windows.SW_SHOWNORMAL)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
