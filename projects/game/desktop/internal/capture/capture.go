//go:build windows

package capture

import (
	"context"
	"fmt"
	"image"
	"unsafe"
)

// CapturedImage holds the PNG-encoded screenshot of a window's client area.
type CapturedImage struct {
	Data     []byte `json:"data"`
	WidthPx  int    `json:"widthPx"`
	HeightPx int    `json:"heightPx"`
	Encoding string `json:"encoding"`
}

// CaptureWindow captures the client area of the specified window as a PNG image.
// It first tries PrintWindow with PW_CLIENTONLY, then falls back to BitBlt with SRCCOPY.
// Returns a CapturedImage with PNG-encoded data.
func CaptureWindow(ctx context.Context, hwnd uintptr) (*CapturedImage, error) {
	// Check context cancellation.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Get client rect.
	r := getClientRect(hwnd)
	width := int(r.Right - r.Left)
	height := int(r.Bottom - r.Top)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("capture: invalid client rect %dx%d", width, height)
	}

	// ClientToScreen: convert client area to screen coordinates for BitBlt fallback.
	pt := point{X: r.Left, Y: r.Top}
	clientToScreen(hwnd, &pt)
	screenX := int(pt.X)
	screenY := int(pt.Y)

	// Get screen DC for fallback.
	screenDC := getDC(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("capture: GetDC(0) failed")
	}

	// Create memory DC and compatible bitmap.
	memDC := createCompatibleDC(screenDC)
	if memDC == 0 {
		releaseDC(0, screenDC)
		return nil, fmt.Errorf("capture: CreateCompatibleDC failed")
	}
	defer deleteDC(memDC)

	bitmap := createCompatibleBitmap(screenDC, int32(width), int32(height))
	if bitmap == 0 {
		releaseDC(0, screenDC)
		return nil, fmt.Errorf("capture: CreateCompatibleBitmap failed")
	}
	defer deleteObject(bitmap)

	oldBitmap := selectObject(memDC, bitmap)
	defer selectObject(memDC, oldBitmap)

	// Strategy 1: PrintWindow with client-only flag.
	printOK := printWindow(hwnd, memDC, PW_CLIENTONLY)

	if !printOK {
		// Strategy 2: BitBlt fallback.
		if !bitBlt(memDC, 0, 0, int32(width), int32(height), screenDC, int32(screenX), int32(screenY), SRCCOPY) {
			releaseDC(0, screenDC)
			return nil, fmt.Errorf("capture: both PrintWindow and BitBlt failed")
		}
	}
	releaseDC(0, screenDC)

	// Build Go image from bitmap data.
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bmi := buildBitmapInfo(width, height)
	procGetDIBits := gdi32.NewProc("GetDIBits")
	scanLines, _, _ := procGetDIBits.Call(
		memDC,
		bitmap,
		0,
		uintptr(height),
		uintptr(unsafe.Pointer(&img.Pix[0])),
		uintptr(unsafe.Pointer(&bmi)),
		0, // DIB_RGB_COLORS
	)
	if scanLines == 0 {
		return nil, fmt.Errorf("capture: GetDIBits failed")
	}
	normalizeBGRA(img.Pix)

	// Encode as PNG.
	pngBytes, err := EncodePNG(img)
	if err != nil {
		return nil, fmt.Errorf("capture: %w", err)
	}

	return &CapturedImage{
		Data:     pngBytes,
		WidthPx:  width,
		HeightPx: height,
		Encoding: "PNG",
	}, nil
}

// bitmapInfoHeader builds a BITMAPINFOHEADER for GetDIBits.
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func buildBitmapInfo(width, height int) bitmapInfoHeader {
	return bitmapInfoHeader{
		Size:        40,
		Width:       int32(width),
		Height:      -int32(height), // top-down DIB
		Planes:      1,
		BitCount:    32,
		Compression: 0, // BI_RGB
	}
}
