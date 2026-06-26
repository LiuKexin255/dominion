//go:build windows

package operation

import (
	"errors"
	"image"
	"syscall"
	"unsafe"
)

// Win32 cursor visibility and drawing constants.
//
// CURSOR_SHOWING / CURSOR_SUPPRESSED:
// https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-cursorinfo
// DI_NORMAL:
// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-drawiconex
const (
	cursorShowing    uint32 = 0x00000001 // CURSOR_SHOWING — cursor is visible
	cursorSuppressed uint32 = 0x00000002 // CURSOR_SUPPRESSED — hidden by touch/pen
	diNormal         uint32 = 0x00000003 // DI_NORMAL — combine image and mask
	biRGB            uint32 = 0          // BI_RGB — uncompressed DIB
)

// GDI procs for cursor drawing. user32 procs hang off the existing user32DLL
// (execute_windows.go); gdi32 is a lazy DLL local to this file.
var (
	gdi32DLL               = syscall.NewLazyDLL("gdi32.dll")
	procGetObject          = gdi32DLL.NewProc("GetObject")
	procCreateCompatibleDC = gdi32DLL.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32DLL.NewProc("CreateDIBSection")
	procSelectObject       = gdi32DLL.NewProc("SelectObject")
	procDeleteObject       = gdi32DLL.NewProc("DeleteObject")
	procDeleteDC           = gdi32DLL.NewProc("DeleteDC")

	procGetCursorInfo = user32DLL.NewProc("GetCursorInfo")
	procGetIconInfo   = user32DLL.NewProc("GetIconInfo")
	procDrawIconEx    = user32DLL.NewProc("DrawIconEx")
	procGetDC         = user32DLL.NewProc("GetDC")
	procReleaseDC     = user32DLL.NewProc("ReleaseDC")
)

// point mirrors the Win32 POINT structure (two LONG coordinates).
type point struct {
	X, Y int32
}

// cursorInfo mirrors the Win32 CURSORINFO structure.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-cursorinfo
type cursorInfo struct {
	CbSize      uint32
	Flags       uint32
	HCursor     uintptr
	PtScreenPos point
}

// iconInfo mirrors the Win32 ICONINFO structure.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-iconinfo
type iconInfo struct {
	FIcon    uint32 // BOOL: 0 = cursor, 1 = icon
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

// bitmap mirrors the Win32 BITMAP structure returned by GetObject.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/api/wingdi/nf-wingdi-getobject
type bitmap struct {
	BmType       int32
	BmWidth      int32
	BmHeight     int32
	BmWidthBytes uint32
	BmPlanes     uint16
	BmBitsPixel  uint16
	BmBits       uintptr
}

// bitmapInfoHeader mirrors the Win32 BITMAPINFOHEADER structure used by
// CreateDIBSection. For a 32-bit BI_RGB DIB no color table follows the
// header, so the header alone serves as the BITMAPINFO parameter.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/api/wingdi/ns-wingdi-bitmapinfoheader
type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// DrawCursor overlays the real OS cursor onto img at the cursor's current
// screen position relative to the captured window's top-left corner
// (winLeft, winTop). If the cursor is hidden or touch/pen-suppressed, img is
// returned unchanged with a nil error.
//
// The cursor is drawn via a GDI memory-DC round-trip: img pixels are copied
// into a 32-bit DIB section, the cursor is composited onto it with
// DrawIconEx using explicit pixel dimensions from GetObject (avoiding the
// 0x0008 flag that would force the system default icon size and break DPI
// correctness), and the result is copied back.
//
// winLeft, winTop are the screen coordinates of the captured window's
// top-left corner (from capture.CaptureWindowBounds), used to translate the
// cursor's virtual-desktop position into image space:
//
//	drawX = ptScreenPos.X − xHotspot − winLeft
//	drawY = ptScreenPos.Y − yHotspot − winTop
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-drawiconex
func DrawCursor(img *image.RGBA, winLeft, winTop int32) error {
	var ci cursorInfo
	ci.CbSize = uint32(unsafe.Sizeof(ci))
	ret, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	if ret == 0 {
		return errors.New("GetCursorInfo failed")
	}

	if !cursorVisible(ci.Flags) || ci.HCursor == 0 {
		return nil
	}

	var ii iconInfo
	ret, _, _ = procGetIconInfo.Call(ci.HCursor, uintptr(unsafe.Pointer(&ii)))
	if ret == 0 {
		return errors.New("GetIconInfo failed")
	}
	defer freeIconBitmaps(&ii)

	cursorW, cursorH, err := cursorDimensions(&ii)
	if err != nil {
		return err
	}

	drawX := ci.PtScreenPos.X - int32(ii.XHotspot) - winLeft
	drawY := ci.PtScreenPos.Y - int32(ii.YHotspot) - winTop

	return drawCursorViaDIB(img, ci.HCursor, drawX, drawY, cursorW, cursorH)
}

// cursorVisible reports whether the cursor should be drawn: true only when
// CURSOR_SHOWING is set and CURSOR_SUPPRESSED is clear.
func cursorVisible(flags uint32) bool {
	return flags&cursorShowing != 0 && flags&cursorSuppressed == 0
}

// freeIconBitmaps releases the GDI bitmap handles that GetIconInfo creates.
// Both hbmMask and hbmColor are owned by the caller after GetIconInfo and
// must be freed to avoid GDI handle leaks. Zero handles are skipped.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/api/wingdi/nf-wingdi-deleteobject
func freeIconBitmaps(ii *iconInfo) {
	if ii.HbmMask != 0 {
		procDeleteObject.Call(ii.HbmMask)
	}
	if ii.HbmColor != 0 {
		procDeleteObject.Call(ii.HbmColor)
	}
}

// cursorDimensions returns the cursor's pixel width and height via GetObject.
// For color cursors hbmColor is non-zero and carries the dimensions directly.
// For monochrome cursors hbmColor is zero and hbmMask is a stacked AND+XOR
// mask whose height is double the cursor height.
func cursorDimensions(ii *iconInfo) (width, height int32, err error) {
	var bm bitmap
	if ii.HbmColor != 0 {
		n, _, _ := procGetObject.Call(ii.HbmColor, uintptr(unsafe.Sizeof(bm)), uintptr(unsafe.Pointer(&bm)))
		if n == 0 {
			return 0, 0, errors.New("GetObject(hbmColor) failed")
		}
		return bm.BmWidth, bm.BmHeight, nil
	}
	n, _, _ := procGetObject.Call(ii.HbmMask, uintptr(unsafe.Sizeof(bm)), uintptr(unsafe.Pointer(&bm)))
	if n == 0 {
		return 0, 0, errors.New("GetObject(hbmMask) failed")
	}
	return bm.BmWidth, bm.BmHeight / 2, nil
}

// drawCursorViaDIB performs the GDI memory-DC round-trip: it copies img into
// a 32-bit top-down DIB section, draws the cursor with DrawIconEx, and copies
// the result back into img.
func drawCursorViaDIB(img *image.RGBA, hCursor uintptr, drawX, drawY, cursorW, cursorH int32) error {
	b := img.Bounds()
	imgW := b.Dx()
	imgH := b.Dy()

	hdcScreen, _, _ := procGetDC.Call(0)
	if hdcScreen == 0 {
		return errors.New("GetDC failed")
	}
	defer procReleaseDC.Call(0, hdcScreen)

	hdcMem, _, _ := procCreateCompatibleDC.Call(hdcScreen)
	if hdcMem == 0 {
		return errors.New("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(hdcMem)

	var bmi bitmapInfoHeader
	bmi.BiSize = uint32(unsafe.Sizeof(bmi))
	bmi.BiWidth = int32(imgW)
	bmi.BiHeight = -int32(imgH)
	bmi.BiPlanes = 1
	bmi.BiBitCount = 32
	bmi.BiCompression = biRGB

	var bits uintptr
	hbmDIB, _, _ := procCreateDIBSection.Call(
		hdcMem,
		uintptr(unsafe.Pointer(&bmi)),
		0, // DIB_RGB_COLORS
		uintptr(unsafe.Pointer(&bits)),
		0, 0,
	)
	if hbmDIB == 0 {
		return errors.New("CreateDIBSection failed")
	}
	defer procDeleteObject.Call(hbmDIB)

	blitRGBAToDIB(img, bits, imgW, imgH)

	oldBmp, _, _ := procSelectObject.Call(hdcMem, hbmDIB)
	defer procSelectObject.Call(hdcMem, oldBmp)

	ret, _, _ := procDrawIconEx.Call(
		hdcMem,
		uintptr(drawX), uintptr(drawY),
		hCursor,
		uintptr(cursorW), uintptr(cursorH),
		0, 0,
		uintptr(diNormal),
	)
	if ret == 0 {
		return errors.New("DrawIconEx failed")
	}

	blitDIBToRGBA(bits, img, imgW, imgH)
	return nil
}

// blitRGBAToDIB copies img's RGBA pixels into a top-down 32-bit GDI DIB,
// converting RGBA byte order to GDI's native BGRA and forcing the reserved
// byte to 0xFF (opaque).
func blitRGBAToDIB(img *image.RGBA, bits uintptr, w, h int) {
	dib := unsafe.Slice((*byte)(unsafe.Pointer(bits)), h*w*4)
	dibStride := w * 4
	for y := 0; y < h; y++ {
		src := img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y+y)
		dst := y * dibStride
		for x := 0; x < w; x++ {
			s := src + x*4
			d := dst + x*4
			dib[d+0] = img.Pix[s+2] // B
			dib[d+1] = img.Pix[s+1] // G
			dib[d+2] = img.Pix[s+0] // R
			dib[d+3] = 0xFF         // reserved (opaque)
		}
	}
}

// blitDIBToRGBA copies a top-down 32-bit GDI DIB back into img, converting
// BGRA to RGBA and forcing alpha to 0xFF (screenshots are fully opaque so
// the alpha channel is not meaningful).
func blitDIBToRGBA(bits uintptr, img *image.RGBA, w, h int) {
	dib := unsafe.Slice((*byte)(unsafe.Pointer(bits)), h*w*4)
	dibStride := w * 4
	for y := 0; y < h; y++ {
		src := y * dibStride
		dst := img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y+y)
		for x := 0; x < w; x++ {
			s := src + x*4
			d := dst + x*4
			img.Pix[d+0] = dib[s+2] // R
			img.Pix[d+1] = dib[s+1] // G
			img.Pix[d+2] = dib[s+0] // B
			img.Pix[d+3] = 0xFF     // A (opaque)
		}
	}
}
