//go:build !windows

package main

import "errors"

// The desktop built-ins drive Windows; elsewhere they say so.

type windowHandle uintptr

type desktopWindow struct {
	handle      windowHandle
	title       string
	pid         uint32
	processPath string
	appID       string
}

type startApp struct {
	name, appID string
}

var errWindowsOnly = errors.New("this works on Windows only")

func listWindows() ([]desktopWindow, error) { return nil, errWindowsOnly }
func windowExists(windowHandle) bool        { return false }
func focusWindow(windowHandle) bool         { return false }
func startApps() ([]startApp, error)        { return nil, errWindowsOnly }
func appIDPath(string) string               { return "" }
func fileDescription(string) string         { return "" }
func openApp(string) error                  { return errWindowsOnly }
func startProgram(string, []string) error   { return errWindowsOnly }
func finalPath(path string) string          { return path }
