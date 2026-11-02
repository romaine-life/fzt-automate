//go:build !windows

package main

import "os/exec"

func stopProcesses(string) error { return errWindowsOnly }

func startDetached(cmd *exec.Cmd) error { return cmd.Start() }

type hidInterface struct {
	path             string
	vendor, product  uint16
	usagePage, usage uint16
}

func hidInterfaces() ([]hidInterface, error) { return nil, errWindowsOnly }

func writeHID(string, ...[]byte) error { return errWindowsOnly }
