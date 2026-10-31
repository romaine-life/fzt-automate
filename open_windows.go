//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

// openURL hands a URL (or any registered protocol, such as spotify:) to the
// desktop, as Start-Process does.
func openURL(target string) error {
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(withScheme(target)), nil, nil, windows.SW_SHOWNORMAL)
}
