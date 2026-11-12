// A windowed program that tries to join its parent's console, as mGBA does
// when it starts, writes what happened to <result file>, then sets <event>.
package main

import (
	"os"

	"golang.org/x/sys/windows"
)

const attachParentProcess = ^uintptr(0)

func main() {
	result := "attached"
	if r, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole").Call(attachParentProcess); r == 0 {
		result = "not attached: " + err.Error()
	}
	_ = os.WriteFile(os.Args[1], []byte(result), 0o644)
	if ev, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, windows.StringToUTF16Ptr(os.Args[2])); err == nil {
		_ = windows.SetEvent(ev)
	}
}
