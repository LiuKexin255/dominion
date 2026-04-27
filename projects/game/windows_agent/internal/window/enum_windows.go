//go:build windows

package window

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	maxWindowTextLength = 256
	maxClassNameLength  = 256
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
)

// EnumerateWindows returns all visible top-level windows.
func EnumerateWindows() ([]WindowInfo, error) {
	var windows []WindowInfo
	var enumErr error

	callback := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}

		rect, err := GetWindowRect(hwnd)
		if err != nil {
			enumErr = err
			return 0
		}

		var processID uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&processID)))

		windows = append(windows, WindowInfo{
			HWND:      hwnd,
			Title:     windowText(hwnd),
			ClassName: className(hwnd),
			ProcessID: processID,
			Rect:      rect,
		})
		return 1
	})

	result, _, err := procEnumWindows.Call(callback, 0)
	if result == 0 {
		if enumErr != nil {
			return nil, enumErr
		}
		return nil, fmt.Errorf("EnumWindows: %w", lastError(err))
	}
	return windows, nil
}

func windowText(hwnd uintptr) string {
	buffer := make([]uint16, maxWindowTextLength)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.UTF16ToString(buffer)
}

func className(hwnd uintptr) string {
	buffer := make([]uint16, maxClassNameLength)
	procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.UTF16ToString(buffer)
}

func lastError(err error) error {
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return syscall.EINVAL
	}
	return err
}
