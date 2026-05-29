//go:build windows

package capture

import (
	"syscall"
	"unsafe"
)

// Windows API constants.
const (
	PW_CLIENTONLY = 0x00000001
	SRCCOPY       = 0x00CC0020
)

const (
	dwmwaCloaked        = 14
	dwmNotCloaked       = 0
	dwmCloakedShell     = 1
	dwmCloakedApp       = 2
	dwmCloakedInherited = 4
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	dwmapi = syscall.NewLazyDLL("dwmapi.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetClientRect            = user32.NewProc("GetClientRect")
	procIsIconic                 = user32.NewProc("IsIconic")
	procPrintWindow              = user32.NewProc("PrintWindow")
	procBitBlt                   = gdi32.NewProc("BitBlt")
	procGetDC                    = user32.NewProc("GetDC")
	procReleaseDC                = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC       = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap   = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject             = gdi32.NewProc("SelectObject")
	procDeleteDC                 = gdi32.NewProc("DeleteDC")
	procDeleteObject             = gdi32.NewProc("DeleteObject")
	procClientToScreen           = user32.NewProc("ClientToScreen")
	procDwmGetWindowAttribute    = dwmapi.NewProc("DwmGetWindowAttribute")
)

// RECT represents a Windows RECT structure.
type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// point represents a Windows POINT structure.
type point struct {
	X int32
	Y int32
}

func enumWindows(callback syscall.Handle, lparam uintptr) error {
	ret, _, err := procEnumWindows.Call(uintptr(callback), lparam)
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
	buf := make([]uint16, 256)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func getWindowThreadProcessId(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func getClientRect(hwnd uintptr) rect {
	var r rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
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

func getDC(hwnd uintptr) uintptr {
	ret, _, _ := procGetDC.Call(hwnd)
	return ret
}

func releaseDC(hwnd uintptr, hdc uintptr) {
	procReleaseDC.Call(hwnd, hdc)
}

func createCompatibleDC(hdc uintptr) uintptr {
	ret, _, _ := procCreateCompatibleDC.Call(hdc)
	return ret
}

func createCompatibleBitmap(hdc uintptr, width, height int32) uintptr {
	ret, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(width), uintptr(height))
	return ret
}

func selectObject(hdc uintptr, hgdiobj uintptr) uintptr {
	ret, _, _ := procSelectObject.Call(hdc, hgdiobj)
	return ret
}

func deleteDC(hdc uintptr) {
	procDeleteDC.Call(hdc)
}

func deleteObject(hgdiobj uintptr) {
	procDeleteObject.Call(hgdiobj)
}

func clientToScreen(hwnd uintptr, lpPoint *point) {
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(lpPoint)))
}

func printWindow(hwnd uintptr, hdc uintptr, flags uint32) bool {
	ret, _, _ := procPrintWindow.Call(hwnd, hdc, uintptr(flags))
	return ret != 0
}

func bitBlt(hdcDest uintptr, x, y, cx, cy int32, hdcSrc uintptr, x1, y1 int32, rop uint32) bool {
	ret, _, _ := procBitBlt.Call(
		hdcDest,
		uintptr(x), uintptr(y), uintptr(cx), uintptr(cy),
		hdcSrc,
		uintptr(x1), uintptr(y1),
		uintptr(rop),
	)
	return ret != 0
}
