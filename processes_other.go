//go:build !windows

package main

import "errors"

func listProcesses() ([]process, error) {
	return nil, errors.New("work-on-app reads the Windows process table")
}
