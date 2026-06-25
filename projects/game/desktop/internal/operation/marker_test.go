package operation_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"dominion/projects/game/desktop/internal/operation"
)

// encodeSolid builds a PNG-encoded width×height image filled entirely with c.
func encodeSolid(width, height int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// asRGBA converts any color.Color to its RGBA representation for value
// comparison, regardless of the underlying decoded image pixel type.
func asRGBA(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func TestApplyMarker_RingPixels(t *testing.T) {
	src := encodeSolid(100, 100, color.White)

	out, err := operation.ApplyMarker(src, 50, 50)
	if err != nil {
		t.Fatalf("ApplyMarker: %v", err)
	}

	marked, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}

	red := color.RGBA{R: 255, G: 0, B: 0, A: 255}

	// Cardinal pixels of a radius-12 ring centered at (50, 50); the
	// midpoint-circle algorithm always plots them as the first octant step.
	ringPoints := []image.Point{
		image.Pt(62, 50),
		image.Pt(38, 50),
		image.Pt(50, 62),
		image.Pt(50, 38),
	}
	for _, p := range ringPoints {
		if got := asRGBA(marked.At(p.X, p.Y)); got != red {
			t.Errorf("ring pixel (%d,%d) = %v, want red %v", p.X, p.Y, got, red)
		}
	}

	// Center must remain white: the marker is a ring, not a filled dot.
	if got := asRGBA(marked.At(50, 50)); got != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Errorf("center pixel = %v, want white (ring not filled)", got)
	}
}

func TestApplyMarker_InvalidPNG(t *testing.T) {
	out, err := operation.ApplyMarker([]byte("not a png"), 50, 50)
	if err == nil {
		t.Error("expected error for invalid PNG, got nil")
	}
	if out != nil {
		t.Errorf("expected nil bytes for invalid PNG, got %d bytes", len(out))
	}
}

func TestApplyMarker_OutOfBoundsClamped(t *testing.T) {
	src := encodeSolid(100, 100, color.White)

	// (200, 200) on a 100×100 image clamps to (99, 99); must not panic.
	out, err := operation.ApplyMarker(src, 200, 200)
	if err != nil {
		t.Fatalf("ApplyMarker out-of-bounds: %v", err)
	}

	marked, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}

	red := color.RGBA{R: 255, G: 0, B: 0, A: 255}

	// Clamped center (99, 99); the west cardinal pixel (99-12, 99) is
	// in-bounds and must be red.
	if got := asRGBA(marked.At(87, 99)); got != red {
		t.Errorf("pixel (87,99) = %v, want red (ring at clamped center)", got)
	}
}
