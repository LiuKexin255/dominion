//go:build windows

package operation

import (
	"runtime"
	"testing"
	"unsafe"
)

// Test_mouseInputLayoutLocksWin32INPUTContract guards against the regression
// where mouseInput was a flat struct and Go laid it out at 32 bytes on amd64
// while Win32 INPUT is 40 bytes. With the wrong size and field offsets,
// SendInput read dwFlags from the offset of the Go Time field (always 0) and
// silently dispatched no-op mouse events — the desktop logged "Operation
// executed" but no click reached the target window.
//
// unsafe.Offsetof reports a field's offset within its immediate struct only,
// so the absolute offset of Mi.Dx within mouseInput is the sum of
// Offsetof(Mi) and Offsetof(Dx within mouseEvent). Asserting both struct
// sizes plus these per-struct offsets fully constrains the layout.
//
// Expected values mirror Win32 INPUT / MOUSEINPUT from winuser.h under MSVC
// alignment rules:
//
//	amd64: sizeof(INPUT)=40, type@0, mi.dx@8, mi.dy@12, mi.mouseData@16,
//	      mi.dwFlags@20, mi.time@24, mi.dwExtraInfo@32.
//	386:   sizeof(INPUT)=28, type@0, mi.dx@4, mi.dy@8, mi.mouseData@12,
//	      mi.dwFlags@16, mi.time@20, mi.dwExtraInfo@24.
//
// Ref: https://learn.microsoft.com/windows/win32/api/winuser/ns-winuser-input
func Test_mouseInputLayoutLocksWin32INPUTContract(t *testing.T) {
	type expect struct {
		goarch             string
		mouseInputSize     uintptr
		mouseEventSize     uintptr
		miOffset           uintptr
		dxWithinMouseEvent uintptr
		dyWithinMouseEvent uintptr
		mouseDataWithin    uintptr
		dwFlagsWithin      uintptr
		timeWithin         uintptr
		dwExtraInfoWithin  uintptr
	}
	expectations := []expect{
		{
			goarch:             "amd64",
			mouseInputSize:     40,
			mouseEventSize:     32,
			miOffset:           8,
			dxWithinMouseEvent: 0,
			dyWithinMouseEvent: 4,
			mouseDataWithin:    8,
			dwFlagsWithin:      12,
			timeWithin:         16,
			dwExtraInfoWithin:  24,
		},
		{
			goarch:             "386",
			mouseInputSize:     28,
			mouseEventSize:     24,
			miOffset:           4,
			dxWithinMouseEvent: 0,
			dyWithinMouseEvent: 4,
			mouseDataWithin:    8,
			dwFlagsWithin:      12,
			timeWithin:         16,
			dwExtraInfoWithin:  20,
		},
	}

	var want expect
	matched := false
	for _, e := range expectations {
		if e.goarch == runtime.GOARCH {
			want = e
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("no layout expectation registered for GOARCH=%q; add it to the table", runtime.GOARCH)
	}

	miOffset := unsafe.Offsetof(mouseInput{}.Mi)
	absDx := miOffset + unsafe.Offsetof(mouseEvent{}.Dx)
	absDy := miOffset + unsafe.Offsetof(mouseEvent{}.Dy)
	absMouseData := miOffset + unsafe.Offsetof(mouseEvent{}.MouseData)
	absDwFlags := miOffset + unsafe.Offsetof(mouseEvent{}.DwFlags)
	absTime := miOffset + unsafe.Offsetof(mouseEvent{}.Time)
	absDwExtraInfo := miOffset + unsafe.Offsetof(mouseEvent{}.DwExtraInfo)

	if got := unsafe.Sizeof(mouseInput{}); got != want.mouseInputSize {
		t.Errorf("sizeof(mouseInput) = %d on %q, want %d (Win32 INPUT size); a mismatch means SendInput reads the wrong number of bytes per input element",
			got, runtime.GOARCH, want.mouseInputSize)
	}
	if got := unsafe.Sizeof(mouseEvent{}); got != want.mouseEventSize {
		t.Errorf("sizeof(mouseEvent) = %d on %q, want %d (Win32 MOUSEINPUT size)", got, runtime.GOARCH, want.mouseEventSize)
	}
	if got := unsafe.Offsetof(mouseInput{}.Type); got != 0 {
		t.Errorf("offset of Type = %d, want 0", got)
	}
	if got := miOffset; got != want.miOffset {
		t.Errorf("offset of Mi within mouseInput = %d on %q, want %d; a mismatch means the post-Type union padding is wrong (the original bug)", got, runtime.GOARCH, want.miOffset)
	}
	if got := absDx; got != want.miOffset+want.dxWithinMouseEvent {
		t.Errorf("absolute offset of Mi.Dx = %d on %q, want %d", got, runtime.GOARCH, want.miOffset+want.dxWithinMouseEvent)
	}
	if got := absDy; got != want.miOffset+want.dyWithinMouseEvent {
		t.Errorf("absolute offset of Mi.Dy = %d on %q, want %d", got, runtime.GOARCH, want.miOffset+want.dyWithinMouseEvent)
	}
	if got := absMouseData; got != want.miOffset+want.mouseDataWithin {
		t.Errorf("absolute offset of Mi.MouseData = %d on %q, want %d", got, runtime.GOARCH, want.miOffset+want.mouseDataWithin)
	}
	if got := absDwFlags; got != want.miOffset+want.dwFlagsWithin {
		t.Errorf("absolute offset of Mi.DwFlags = %d on %q, want %d; a mismatch means Windows reads dwFlags from the wrong field and dispatches no-op mouse events (the original bug)", got, runtime.GOARCH, want.miOffset+want.dwFlagsWithin)
	}
	if got := absTime; got != want.miOffset+want.timeWithin {
		t.Errorf("absolute offset of Mi.Time = %d on %q, want %d", got, runtime.GOARCH, want.miOffset+want.timeWithin)
	}
	if got := absDwExtraInfo; got != want.miOffset+want.dwExtraInfoWithin {
		t.Errorf("absolute offset of Mi.DwExtraInfo = %d on %q, want %d", got, runtime.GOARCH, want.miOffset+want.dwExtraInfoWithin)
	}
}
