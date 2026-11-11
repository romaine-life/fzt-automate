//go:build windows

package main

// The desktop's windows and Start-menu programs, through Win32 and the shell's
// COM interfaces, for the windows and programs folders and launch (FZT-71;
// this was romaine-life/at's lib/windows.cs, compiled into each pwsh run).

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")

	procGetWindow                   = user32.NewProc("GetWindow")
	procGetWindowLongW              = user32.NewProc("GetWindowLongW")
	procGetWindowTextW              = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW        = user32.NewProc("GetWindowTextLengthW")
	procIsIconic                    = user32.NewProc("IsIconic")
	procShowWindow                  = user32.NewProc("ShowWindow")
	procSetForegroundWindow         = user32.NewProc("SetForegroundWindow")
	procSendInput                   = user32.NewProc("SendInput")
	procSHGetPropertyStoreForWindow = shell32.NewProc("SHGetPropertyStoreForWindow")
	procSHGetKnownFolderItem        = shell32.NewProc("SHGetKnownFolderItem")
	procGetApplicationUserModelId   = kernel32.NewProc("GetApplicationUserModelId")
	procPropVariantClear            = ole32.NewProc("PropVariantClear")
	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
)

const clsctxAll = 0x17

const (
	gwOwner            = 4
	gwlExStyle         = -20
	wsExToolWindow     = 0x80
	wsExNoActivate     = 0x08000000
	dwmwaCloaked       = 14
	dwmCloakedShell    = 2
	swRestore          = 9
	inputMouse         = 0
	mouseeventfMove    = 1
	vtLPWSTR           = 31
	sigdnNormalDisplay = 0
)

var (
	iidIPropertyStore          = windows.GUID{Data1: 0x886D8EEB, Data2: 0x8CF2, Data3: 0x4446, Data4: [8]byte{0x8D, 0x02, 0xCD, 0xBA, 0x1D, 0xBD, 0xCF, 0x99}}
	clsidVirtualDesktopManager = windows.GUID{Data1: 0xaa509086, Data2: 0x5ca9, Data3: 0x4c25, Data4: [8]byte{0x8f, 0x95, 0x58, 0x9d, 0x3c, 0x07, 0xb4, 0x8a}}
	iidIVirtualDesktopManager  = windows.GUID{Data1: 0xa5cd92ff, Data2: 0x29be, Data3: 0x454c, Data4: [8]byte{0x8d, 0x04, 0xd8, 0x28, 0x79, 0xfb, 0x3f, 0x1b}}
	iidIShellItem              = windows.GUID{Data1: 0x43826d1e, Data2: 0xe718, Data3: 0x42ee, Data4: [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
	iidIShellItem2             = windows.GUID{Data1: 0x7e9fb0d3, Data2: 0x919f, Data3: 0x4307, Data4: [8]byte{0xab, 0x2e, 0x9b, 0x18, 0x60, 0x31, 0x0c, 0x93}}
	iidIEnumShellItems         = windows.GUID{Data1: 0x70629033, Data2: 0xe363, Data3: 0x4a28, Data4: [8]byte{0xa5, 0x67, 0x0d, 0xb7, 0x80, 0x06, 0xe6, 0xd7}}
	bhidEnumItems              = windows.GUID{Data1: 0x94f60519, Data2: 0x2850, Data3: 0x4924, Data4: [8]byte{0xaa, 0x5a, 0xd1, 0x5e, 0x84, 0x86, 0x80, 0x39}}
	pkeyAppUserModelID         = propertyKey{fmtid: windows.GUID{Data1: 0x9F4C2855, Data2: 0x9F79, Data3: 0x4B39, Data4: [8]byte{0xA8, 0xD0, 0xE1, 0xD4, 0x2D, 0xE1, 0xD5, 0xF3}}, pid: 5}
)

type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

type propVariant struct {
	vt       uint16
	reserved [3]uint16
	val      unsafe.Pointer
	val2     uintptr
}

type mouseInput struct {
	dx, dy                 int32
	mouseData, flags, time uint32
	extra                  uintptr
}

type input struct {
	typ   uint32
	mouse mouseInput
}

type windowHandle = windows.HWND

// desktopWindow is one window the taskbar shows.
type desktopWindow struct {
	handle      windowHandle
	title       string
	pid         uint32
	processPath string // empty when the process can't be opened (elevated, protected)
	appID       string // the window's AppUserModelID, else its process's; empty when neither has one
}

// comCall calls method index of a COM object's vtable.
func comCall(obj unsafe.Pointer, index int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func comRelease(obj unsafe.Pointer) { comCall(obj, 2) }

// withCOM runs fn on one OS thread with COM started on it.
func withCOM(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && !errors.Is(err, windows.Errno(1)) {
		// S_FALSE (already started on this thread) arrives as Errno(1).
		return fmt.Errorf("starting COM: %w", err)
	}
	defer windows.CoUninitialize()
	return fn()
}

var (
	enumMu      sync.Mutex
	enumFound   []windows.HWND
	enumWindows = windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		enumFound = append(enumFound, hwnd)
		return 1
	})
)

// listWindows is the windows the taskbar would show, on every virtual
// desktop, in z-order: the most recently active first.
func listWindows() ([]desktopWindow, error) {
	var out []desktopWindow
	err := withCOM(func() error {
		var vdm unsafe.Pointer
		if r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidVirtualDesktopManager)), 0, clsctxAll,
			uintptr(unsafe.Pointer(&iidIVirtualDesktopManager)), uintptr(unsafe.Pointer(&vdm))); r != 0 || vdm == nil {
			vdm = nil // without it, a cloaked window counts as hidden
		} else {
			defer comRelease(vdm)
		}
		enumMu.Lock()
		enumFound = nil
		err := windows.EnumWindows(enumWindows, nil)
		handles := enumFound
		enumFound = nil
		enumMu.Unlock()
		if err != nil {
			return fmt.Errorf("listing windows: %w", err)
		}
		for _, h := range handles {
			if isTaskbarWindow(h, vdm) {
				out = append(out, describeWindow(h))
			}
		}
		return nil
	})
	return out, err
}

// isTaskbarWindow: visible, unowned, not a tool window, titled, and either not
// cloaked or cloaked by the shell because it is on another virtual desktop (a
// suspended store app is cloaked too, but has no desktop).
func isTaskbarWindow(h windows.HWND, vdm unsafe.Pointer) bool {
	if !windows.IsWindowVisible(h) {
		return false
	}
	if owner, _, _ := procGetWindow.Call(uintptr(h), gwOwner); owner != 0 {
		return false
	}
	style, _, _ := procGetWindowLongW.Call(uintptr(h), uintptr(gwlExStyle&0xffffffff))
	if uint32(style)&(wsExToolWindow|wsExNoActivate) != 0 {
		return false
	}
	if n, _, _ := procGetWindowTextLengthW.Call(uintptr(h)); n == 0 {
		return false
	}
	var cloaked uint32
	if windows.DwmGetWindowAttribute(h, dwmwaCloaked, unsafe.Pointer(&cloaked), 4) != nil || cloaked == 0 {
		return true
	}
	if cloaked != dwmCloakedShell || vdm == nil {
		return false
	}
	var desktop windows.GUID
	// IVirtualDesktopManager::GetWindowDesktopId is method 4.
	if comCall(vdm, 4, uintptr(h), uintptr(unsafe.Pointer(&desktop))) != 0 {
		return false
	}
	return desktop != (windows.GUID{})
}

func describeWindow(h windows.HWND) desktopWindow {
	w := desktopWindow{handle: h, title: windowTitle(h), appID: windowAppID(h)}
	windows.GetWindowThreadProcessId(h, &w.pid)
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, w.pid)
	if err != nil {
		return w
	}
	defer windows.CloseHandle(p)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if windows.QueryFullProcessImageName(p, 0, &buf[0], &size) == nil {
		w.processPath = windows.UTF16ToString(buf[:size])
	}
	if w.appID == "" {
		id := make([]uint16, 256)
		n := uint32(len(id))
		if r, _, _ := procGetApplicationUserModelId.Call(uintptr(p), uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&id[0]))); r == 0 {
			w.appID = windows.UTF16ToString(id)
		}
	}
	return w
}

func windowTitle(h windows.HWND) string {
	n, _, _ := procGetWindowTextLengthW.Call(uintptr(h))
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}

func windowAppID(h windows.HWND) string {
	var store unsafe.Pointer
	if r, _, _ := procSHGetPropertyStoreForWindow.Call(uintptr(h), uintptr(unsafe.Pointer(&iidIPropertyStore)), uintptr(unsafe.Pointer(&store))); r != 0 || store == nil {
		return ""
	}
	defer comRelease(store)
	var v propVariant
	// IPropertyStore::GetValue is method 5.
	if comCall(store, 5, uintptr(unsafe.Pointer(&pkeyAppUserModelID)), uintptr(unsafe.Pointer(&v))) != 0 {
		return ""
	}
	defer procPropVariantClear.Call(uintptr(unsafe.Pointer(&v)))
	if v.vt != vtLPWSTR || v.val == nil {
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(v.val))
}

func windowExists(h windows.HWND) bool { return windows.IsWindow(h) }

// focusWindow brings the window to the front, restoring it if minimized, and
// says whether Windows accepted the switch. fzt-automate runs under the tmux
// server, not the window being typed in, so Windows' foreground lock can
// refuse it; a mouse move of zero makes this process the last to send input,
// which the lock allows, and the switch is asked again.
func focusWindow(h windows.HWND) bool {
	if !windows.IsWindow(h) {
		return false
	}
	if r, _, _ := procIsIconic.Call(uintptr(h)); r != 0 {
		procShowWindow.Call(uintptr(h), swRestore)
	}
	if r, _, _ := procSetForegroundWindow.Call(uintptr(h)); r != 0 {
		return true
	}
	in := input{typ: inputMouse, mouse: mouseInput{flags: mouseeventfMove}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	r, _, _ := procSetForegroundWindow.Call(uintptr(h))
	return r != 0
}

// startApp is one program in the Start menu, as the shell's AppsFolder lists it.
type startApp struct {
	name, appID string
}

// startApps lists the shell's AppsFolder (what Get-StartApps reads).
func startApps() ([]startApp, error) {
	var apps []startApp
	err := withCOM(func() error {
		var folder unsafe.Pointer
		if r, _, _ := procSHGetKnownFolderItem.Call(uintptr(unsafe.Pointer(windows.FOLDERID_AppsFolder)), 0, 0,
			uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&folder))); r != 0 {
			return fmt.Errorf("opening the Start menu's apps folder: HRESULT %#x", uint32(r))
		}
		defer comRelease(folder)
		var enum unsafe.Pointer
		// IShellItem::BindToHandler is method 3.
		if r := comCall(folder, 3, 0, uintptr(unsafe.Pointer(&bhidEnumItems)), uintptr(unsafe.Pointer(&iidIEnumShellItems)), uintptr(unsafe.Pointer(&enum))); r != 0 {
			return fmt.Errorf("listing the Start menu's apps: HRESULT %#x", uint32(r))
		}
		defer comRelease(enum)
		for {
			var item unsafe.Pointer
			var fetched uint32
			// IEnumShellItems::Next is method 3; S_FALSE (1) at the end.
			if r := comCall(enum, 3, 1, uintptr(unsafe.Pointer(&item)), uintptr(unsafe.Pointer(&fetched))); r != 0 || fetched == 0 {
				return nil
			}
			if app, ok := describeApp(item); ok {
				apps = append(apps, app)
			}
			comRelease(item)
		}
	})
	return apps, err
}

func describeApp(item unsafe.Pointer) (startApp, bool) {
	var name *uint16
	// IShellItem::GetDisplayName is method 5.
	if comCall(item, 5, sigdnNormalDisplay, uintptr(unsafe.Pointer(&name))) != 0 || name == nil {
		return startApp{}, false
	}
	app := startApp{name: windows.UTF16PtrToString(name)}
	windows.CoTaskMemFree(unsafe.Pointer(name))
	var item2 unsafe.Pointer
	// IUnknown::QueryInterface is method 0.
	if comCall(item, 0, uintptr(unsafe.Pointer(&iidIShellItem2)), uintptr(unsafe.Pointer(&item2))) != 0 {
		return startApp{}, false
	}
	defer comRelease(item2)
	var id *uint16
	// IShellItem2::GetString is method 17.
	if comCall(item2, 17, uintptr(unsafe.Pointer(&pkeyAppUserModelID)), uintptr(unsafe.Pointer(&id))) != 0 || id == nil {
		return startApp{}, false
	}
	app.appID = windows.UTF16PtrToString(id)
	windows.CoTaskMemFree(unsafe.Pointer(id))
	return app, true
}

// appIDPath is the program a Start-menu AppID names, when it names one: a
// desktop app's AppID is its path, or a known folder's id and the path under it.
func appIDPath(appID string) string {
	if !strings.HasSuffix(strings.ToLower(appID), ".exe") {
		return ""
	}
	if len(appID) > 39 && appID[0] == '{' && appID[37] == '}' && appID[38] == '\\' {
		g, err := windows.GUIDFromString(appID[:38])
		if err != nil {
			return ""
		}
		dir, err := windows.KnownFolderPath((*windows.KNOWNFOLDERID)(&g), 0)
		if err != nil {
			return ""
		}
		return filepath.Join(dir, appID[39:])
	}
	if filepath.IsAbs(appID) {
		return appID
	}
	return ""
}

// fileDescription is a program's FileDescription version resource.
func fileDescription(path string) string {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 {
		return ""
	}
	data := make([]byte, size)
	if windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&data[0])) != nil {
		return ""
	}
	var langs *[2]uint16
	var n uint32
	if windows.VerQueryValue(unsafe.Pointer(&data[0]), `\VarFileInfo\Translation`, unsafe.Pointer(&langs), &n) != nil || n < 4 {
		return ""
	}
	key := fmt.Sprintf(`\StringFileInfo\%04x%04x\FileDescription`, langs[0], langs[1])
	var text *uint16
	if windows.VerQueryValue(unsafe.Pointer(&data[0]), key, unsafe.Pointer(&text), &n) != nil || n == 0 {
		return ""
	}
	return windows.UTF16PtrToString(text)
}

// startProgram starts a program with its arguments, or opens a link, and
// doesn't wait for it: the shell starts it as a double-click would, in the
// program's own folder.
func startProgram(program string, args []string) error {
	var params, dir *uint16
	if len(args) > 0 {
		params = windows.StringToUTF16Ptr(commandLine(args))
	}
	if filepath.IsAbs(program) {
		dir = windows.StringToUTF16Ptr(filepath.Dir(program))
	}
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(program), params, dir, windows.SW_SHOWNORMAL)
}

// finalPath is the path Windows reports for a running program's file: folder
// links followed. Go's filepath.EvalSymlinks no longer follows junctions, so
// the file is opened and Windows is asked. Anything that can't be read stays
// as given.
func finalPath(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(path), 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return path
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	if err != nil || n == 0 || int(n) > len(buf) {
		return path
	}
	final := windows.UTF16ToString(buf[:n])
	if strings.HasPrefix(final, `\\?\UNC\`) {
		return `\\` + final[len(`\\?\UNC\`):]
	}
	return strings.TrimPrefix(final, `\\?\`)
}

// commandLine joins arguments the way a program's command-line parser splits
// them again.
func commandLine(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = windows.EscapeArg(a)
	}
	return strings.Join(quoted, " ")
}

// openApp starts an app from Windows' installed-apps list by its AppID,
// through the shell's AppsFolder.
func openApp(appID string) error {
	target := `shell:AppsFolder\` + appID
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(target), nil, nil, windows.SW_SHOWNORMAL)
}
