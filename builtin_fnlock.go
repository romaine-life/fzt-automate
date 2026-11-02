package main

import (
	"context"
	"errors"
	"fmt"
	"io"
)

func init() {
	register(&builtin{name: "fnlock", about: "turn Fn lock on for the Logitech K400+ keyboards", run: runFnLock})
}

// The K400+'s Unifying receiver (046d:c52b) takes HID++ short reports on its
// vendor interface (usage page 0xFF00, usage 1). Feature 0x09, function 0x19,
// value 0 turns Fn lock on, for the keyboards paired as devices 1 and 2.
const (
	logitechVendor   = 0x046d
	unifyingReceiver = 0xc52b
)

var fnLockReports = [][]byte{
	{0x10, 0x01, 0x09, 0x19, 0x00, 0x00, 0x00},
	{0x10, 0x02, 0x09, 0x19, 0x00, 0x00, 0x00},
}

func runFnLock(ctx context.Context, out io.Writer, args []string) (string, error) {
	ifaces, err := hidInterfaces()
	if err != nil {
		return "", err
	}
	for _, i := range ifaces {
		if i.vendor == logitechVendor && i.product == unifyingReceiver && i.usagePage == 0xFF00 && i.usage == 1 {
			if err := writeHID(i.path, fnLockReports...); err != nil {
				return "", fmt.Errorf("the receiver didn't take the Fn lock reports: %w", err)
			}
			return "Fn lock enabled on all K400+ keyboards", nil
		}
	}
	return "", errors.New("no K400+ receiver is plugged in")
}
