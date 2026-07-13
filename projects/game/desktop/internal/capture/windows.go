//go:build windows

package capture

import (
	"syscall"
	"unsafe"
)

const (
	dwmwaCloaked           = 14
	dwmNotCloaked          = 0
	dwmCloakedShell        = 1
	dwmCloakedApp          = 2
	dwmCloakedInherited    = 4
	dwmwaExtendedFrameBounds = 9

	windowTextBufLen = 256
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	dwmapi                       = syscall.NewLazyDLL("dwmapi.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsIconic                 = user32.NewProc("IsIconic")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procKeybdEvent               = user32.NewProc("keybd_event")
	procDwmGetWindowAttribute    = dwmapi.NewProc("DwmGetWindowAttribute")
)

// RECT represents a Windows RECT structure.
type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func enumWindows(callback uintptr, lparam uintptr) error {
	ret, _, err := procEnumWindows.Call(callback, lparam)
	if ret == 0 {
		return err
	}
	return nil
}

func isWindowVisible(hwnd uintptr) bool {
	ret, _, _ := procIsWindowVisible.Call(hwnd)
	return ret != 0
}

func getWindowTextW(hwnd uintptr) string {
	buf := make([]uint16, windowTextBufLen)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func getWindowThreadProcessId(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func isIconic(hwnd uintptr) bool {
	ret, _, _ := procIsIconic.Call(hwnd)
	return ret != 0
}

func dwmGetWindowAttribute(hwnd uintptr, dwAttribute uint32) uint32 {
	var val uint32
	procDwmGetWindowAttribute.Call(
		hwnd,
		uintptr(dwAttribute),
		uintptr(unsafe.Pointer(&val)),
		uintptr(unsafe.Sizeof(val)),
	)
	return val
}

func getWindowRect(hwnd uintptr) (rect, error) {
	var r rect
	ret, _, err := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return rect{}, err
	}
	return r, nil
}

func isWindow(hwnd uintptr) bool {
	ret, _, _ := procIsWindow.Call(hwnd)
	return ret != 0
}

func dwmGetExtendedFrameBounds(hwnd uintptr) (rect, error) {
	var r rect
	ret, _, err := procDwmGetWindowAttribute.Call(
		hwnd,
		uintptr(dwmwaExtendedFrameBounds),
		uintptr(unsafe.Pointer(&r)),
		uintptr(unsafe.Sizeof(r)),
	)
	if ret != 0 {
		return rect{}, err
	}
	return r, nil
}

// Win32 constants for forcing a window to the foreground.
//
// SW_RESTORE restores a minimized window (SetForegroundWindow alone does not
// un-minimize). VK_MENU + KEYEVENTF_KEYUP drive the synthetic ALT keystroke
// that satisfies the foreground-lock restriction, allowing a non-foreground
// caller to take the foreground.
const (
	swRestore      uintptr = 9 // SW_RESTORE
	vkMenu         uintptr = 0x12
	keyeventfKeyup uintptr = 0x0002
)

// setForegroundReliably brings hwnd to the foreground. Windows consumes the
// first pointer event that activates a background window, so the target of a
// synthetic click (SendInput) must already be foreground before any button
// event is dispatched — otherwise the click is eaten for activation and never
// reaches the application.
//
// A minimized window is restored first, and a synthetic ALT keystroke bypasses
// the foreground-lock restriction so the call takes effect even when the
// caller is not the current foreground process. Returns true when
// SetForegroundWindow reports success.
func setForegroundReliably(hwnd uintptr) bool {
	if isIconic(hwnd) {
		procShowWindow.Call(hwnd, swRestore)
	}
	procKeybdEvent.Call(vkMenu, 0, 0, 0)
	procKeybdEvent.Call(vkMenu, 0, keyeventfKeyup, 0)
	ret, _, _ := procSetForegroundWindow.Call(hwnd)
	return ret != 0
}

// getForegroundHwnd returns the handle of the current foreground window, or 0
// if there is none.
func getForegroundHwnd() uintptr {
	ret, _, _ := procGetForegroundWindow.Call()
	return ret
}
