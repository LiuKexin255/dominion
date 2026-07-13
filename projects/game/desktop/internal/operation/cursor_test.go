//go:build windows

package operation

import (
	"runtime"
	"testing"
	"unsafe"
)

// procCreateBitmap is a test-only GDI proc for creating disposable bitmaps
// to exercise freeIconBitmaps. Defined here (not in cursor.go) so production
// code carries no test-only symbols.
var procCreateBitmap = gdi32DLL.NewProc("CreateBitmap")

// Test_cursorInfoLayoutLocksWin32Contract guards the cursorInfo struct layout
// against regressions. GetCursorInfo writes into the raw bytes of the struct,
// so any field-offset or size mismatch silently corrupts the cursor handle or
// screen position.
//
// Expected values mirror Win32 CURSORINFO from winuser.h under MSVC alignment:
//
//	amd64: sizeof=24, cbSize@0, flags@4, hCursor@8, ptScreenPos@16.
//	386:   sizeof=20, cbSize@0, flags@4, hCursor@8, ptScreenPos@12.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-cursorinfo
func Test_cursorInfoLayoutLocksWin32Contract(t *testing.T) {
	type expect struct {
		goarch         string
		size           uintptr
		cbSizeOff      uintptr
		flagsOff       uintptr
		hCursorOff     uintptr
		ptScreenPosOff uintptr
	}
	expectations := []expect{
		{goarch: "amd64", size: 24, cbSizeOff: 0, flagsOff: 4, hCursorOff: 8, ptScreenPosOff: 16},
		{goarch: "386", size: 20, cbSizeOff: 0, flagsOff: 4, hCursorOff: 8, ptScreenPosOff: 12},
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
		t.Fatalf("no layout expectation for GOARCH=%q", runtime.GOARCH)
	}

	if got := unsafe.Sizeof(cursorInfo{}); got != want.size {
		t.Errorf("sizeof(cursorInfo) = %d on %q, want %d (Win32 CURSORINFO)", got, runtime.GOARCH, want.size)
	}
	if got := unsafe.Offsetof(cursorInfo{}.CbSize); got != want.cbSizeOff {
		t.Errorf("offset of CbSize = %d, want %d", got, want.cbSizeOff)
	}
	if got := unsafe.Offsetof(cursorInfo{}.Flags); got != want.flagsOff {
		t.Errorf("offset of Flags = %d, want %d", got, want.flagsOff)
	}
	if got := unsafe.Offsetof(cursorInfo{}.HCursor); got != want.hCursorOff {
		t.Errorf("offset of HCursor = %d on %q, want %d; a mismatch corrupts the cursor handle", got, runtime.GOARCH, want.hCursorOff)
	}
	if got := unsafe.Offsetof(cursorInfo{}.PtScreenPos); got != want.ptScreenPosOff {
		t.Errorf("offset of PtScreenPos = %d on %q, want %d; a mismatch corrupts the cursor position", got, runtime.GOARCH, want.ptScreenPosOff)
	}
}

// Test_iconInfoLayoutLocksWin32Contract guards the iconInfo struct layout.
// GetIconInfo writes hotspot and bitmap handles into the raw bytes, so an
// offset mismatch silently places the hotspot or GDI handle in the wrong field.
//
// Expected values mirror Win32 ICONINFO from winuser.h:
//
//	amd64: sizeof=32, fIcon@0, xHotspot@4, yHotspot@8, hbmMask@16, hbmColor@24.
//	386:   sizeof=20, fIcon@0, xHotspot@4, yHotspot@8, hbmMask@12, hbmColor@16.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-iconinfo
func Test_iconInfoLayoutLocksWin32Contract(t *testing.T) {
	type expect struct {
		goarch      string
		size        uintptr
		hbmMaskOff  uintptr
		hbmColorOff uintptr
	}
	expectations := []expect{
		{goarch: "amd64", size: 32, hbmMaskOff: 16, hbmColorOff: 24},
		{goarch: "386", size: 20, hbmMaskOff: 12, hbmColorOff: 16},
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
		t.Fatalf("no layout expectation for GOARCH=%q", runtime.GOARCH)
	}

	if got := unsafe.Sizeof(iconInfo{}); got != want.size {
		t.Errorf("sizeof(iconInfo) = %d on %q, want %d (Win32 ICONINFO)", got, runtime.GOARCH, want.size)
	}
	if got := unsafe.Offsetof(iconInfo{}.HbmMask); got != want.hbmMaskOff {
		t.Errorf("offset of HbmMask = %d on %q, want %d", got, runtime.GOARCH, want.hbmMaskOff)
	}
	if got := unsafe.Offsetof(iconInfo{}.HbmColor); got != want.hbmColorOff {
		t.Errorf("offset of HbmColor = %d on %q, want %d", got, runtime.GOARCH, want.hbmColorOff)
	}
}

// Test_cursorVisible covers the hidden/suppressed early-return decision
// logic. DrawCursor skips silently (returns nil) whenever cursorVisible is
// false, so a regression here would either draw a hidden cursor or skip a
// visible one.
func Test_cursorVisible(t *testing.T) {
	tests := []struct {
		name  string
		flags uint32
		want  bool
	}{
		{name: "hidden (flags=0)", flags: 0, want: false},
		{name: "showing", flags: cursorShowing, want: true},
		{name: "suppressed", flags: cursorSuppressed, want: false},
		{name: "showing+suppressed", flags: cursorShowing | cursorSuppressed, want: false},
		{name: "arbitrary high bits ignored", flags: cursorShowing | 0x80000000, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cursorVisible(tt.flags); got != tt.want {
				t.Errorf("cursorVisible(0x%X) = %v, want %v", tt.flags, got, tt.want)
			}
		})
	}
}

// Test_freeIconBitmaps_freesBothHandles verifies that freeIconBitmaps
// releases both GDI bitmap handles that GetIconInfo creates. After deletion,
// GetObject returns 0 for the handle (it is no longer valid), proving the
// cleanup ran and no GDI handle leaks.
func Test_freeIconBitmaps_freesBothHandles(t *testing.T) {
	// Given: two real 1×1 monochrome GDI bitmaps simulating GetIconInfo output.
	hbmMask := createTestBitmap(t)
	hbmColor := createTestBitmap(t)
	ii := iconInfo{HbmMask: hbmMask, HbmColor: hbmColor}

	// When: freeIconBitmaps runs.
	freeIconBitmaps(&ii)

	// Then: both handles are invalid (GetObject returns 0 after deletion).
	var bm bitmap
	if n, _, _ := procGetObject.Call(hbmMask, uintptr(unsafe.Sizeof(bm)), uintptr(unsafe.Pointer(&bm))); n != 0 {
		t.Error("hbmMask was not freed — GDI handle leak")
	}
	if n, _, _ := procGetObject.Call(hbmColor, uintptr(unsafe.Sizeof(bm)), uintptr(unsafe.Pointer(&bm))); n != 0 {
		t.Error("hbmColor was not freed — GDI handle leak")
	}
}

// Test_freeIconBitmaps_skipsZeroHandles verifies that freeIconBitmaps does
// not crash or call DeleteObject when both handles are zero (e.g. a cursor
// with no mask/color bitmaps).
func Test_freeIconBitmaps_skipsZeroHandles(t *testing.T) {
	ii := iconInfo{HbmMask: 0, HbmColor: 0}
	freeIconBitmaps(&ii)
}

// createTestBitmap creates a disposable 1×1 monochrome GDI bitmap for
// testing GDI cleanup. It calls t.Fatal if creation fails.
func createTestBitmap(t *testing.T) uintptr {
	t.Helper()
	ret, _, _ := procCreateBitmap.Call(1, 1, 1, 1, 0)
	if ret == 0 {
		t.Fatal("CreateBitmap(1,1,1,1,nil) returned 0")
	}
	return ret
}
