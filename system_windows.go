//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// stopProcesses ends every process of this user's named image (fzt-picker.exe).
func stopProcesses(image string) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return fmt.Errorf("listing processes: %w", err)
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if !strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), image) || e.ProcessID == uint32(os.Getpid()) {
			continue
		}
		p, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, e.ProcessID)
		if err != nil {
			return fmt.Errorf("can't stop %s (pid %d): %w", image, e.ProcessID, err)
		}
		_ = windows.TerminateProcess(p, 1)
		_, _ = windows.WaitForSingleObject(p, 5000)
		windows.CloseHandle(p)
	}
	return nil
}

// startDetached starts a program that outlives this one and the tab it ran
// in: no console, out of tmux's job.
func startDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_BREAKAWAY_FROM_JOB}
	return cmd.Start()
}

var (
	hid                       = windows.NewLazySystemDLL("hid.dll")
	procHidDGetHidGuid        = hid.NewProc("HidD_GetHidGuid")
	procHidDGetAttributes     = hid.NewProc("HidD_GetAttributes")
	procHidDGetPreparsedData  = hid.NewProc("HidD_GetPreparsedData")
	procHidDFreePreparsedData = hid.NewProc("HidD_FreePreparsedData")
	procHidPGetCaps           = hid.NewProc("HidP_GetCaps")
)

type hidAttributes struct {
	size                     uint32
	vendorID, productID, ver uint16
}

type hidCaps struct {
	usage, usagePage                       uint16
	inputLen, outputLen, featureLen        uint16
	reserved                               [17]uint16
	linkNodes, inButtonCaps, inValueCaps   uint16
	inDataIndices, outButtonCaps           uint16
	outValueCaps, outDataIndices           uint16
	featButtonCaps, featValueCaps, featIdx uint16
}

// hidInterface is one HID interface's path and what it reports.
type hidInterface struct {
	path             string
	vendor, product  uint16
	usagePage, usage uint16
}

func hidInterfaces() ([]hidInterface, error) {
	var guid windows.GUID
	procHidDGetHidGuid.Call(uintptr(unsafe.Pointer(&guid)))
	paths, err := windows.CM_Get_Device_Interface_List("", &guid, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, fmt.Errorf("listing HID devices: %w", err)
	}
	var out []hidInterface
	for _, p := range paths {
		h, err := windows.CreateFile(windows.StringToUTF16Ptr(p), 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
		if err != nil {
			continue
		}
		info := hidInterface{path: p}
		attrs := hidAttributes{size: uint32(unsafe.Sizeof(hidAttributes{}))}
		if r, _, _ := procHidDGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attrs))); r != 0 {
			info.vendor, info.product = attrs.vendorID, attrs.productID
		}
		var pre uintptr
		if r, _, _ := procHidDGetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&pre))); r != 0 {
			var caps hidCaps
			procHidPGetCaps.Call(pre, uintptr(unsafe.Pointer(&caps)))
			info.usagePage, info.usage = caps.usagePage, caps.usage
			procHidDFreePreparsedData.Call(pre)
		}
		windows.CloseHandle(h)
		out = append(out, info)
	}
	return out, nil
}

// writeHID sends output reports to one HID interface.
func writeHID(path string, reports ...[]byte) error {
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(path), windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	for _, r := range reports {
		var n uint32
		if err := windows.WriteFile(h, r, &n, nil); err != nil {
			return err
		}
		if int(n) != len(r) {
			return errors.New("the receiver took part of a report")
		}
	}
	return nil
}
