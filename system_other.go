//go:build !windows

package main

func stopProcesses(string) error { return errWindowsOnly }

type hidInterface struct {
	path             string
	vendor, product  uint16
	usagePage, usage uint16
}

func hidInterfaces() ([]hidInterface, error) { return nil, errWindowsOnly }

func writeHID(string, ...[]byte) error { return errWindowsOnly }
