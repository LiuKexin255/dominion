package operation

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

const markerRadius = 12

var markerColor = color.RGBA{R: 255, G: 0, B: 0, A: 255}

// ApplyMarker overlays a red ring of fixed radius centered at (x, y) on the
// provided PNG-encoded image and returns a new PNG with the ring drawn.
//
// Coordinates outside the image bounds are clamped to the nearest in-bounds
// pixel before drawing; ring pixels that fall outside the image after
// clamping are silently skipped.
//
// Returns the PNG-encoded result, or an error if pngData cannot be decoded
// or the result cannot be re-encoded.
func ApplyMarker(pngData []byte, x, y int) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, err
	}

	b := img.Bounds()

	if x < b.Min.X {
		x = b.Min.X
	} else if x >= b.Max.X {
		x = b.Max.X - 1
	}
	if y < b.Min.Y {
		y = b.Min.Y
	} else if y >= b.Max.Y {
		y = b.Max.Y - 1
	}

	var buf draw.Image
	switch i := img.(type) {
	case *image.RGBA:
		buf = i
	case *image.NRGBA:
		buf = i
	default:
		rgba := image.NewRGBA(b)
		draw.Draw(rgba, b, img, b.Min, draw.Src)
		buf = rgba
	}

	drawRing(buf, x, y, markerRadius, markerColor)

	var out bytes.Buffer
	if err := png.Encode(&out, buf); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// drawRing plots a one-pixel ring of radius r centered at (x0, y0) using the
// midpoint-circle algorithm, mirroring each computed point across all eight
// octants. Pixels outside img.Bounds() are silently skipped by setPixel.
func drawRing(img draw.Image, x0, y0, r int, c color.Color) {
	x := r
	y := 0
	decision := 1 - r
	for x >= y {
		setPixel(img, x0+x, y0+y, c)
		setPixel(img, x0+y, y0+x, c)
		setPixel(img, x0-y, y0+x, c)
		setPixel(img, x0-x, y0+y, c)
		setPixel(img, x0-x, y0-y, c)
		setPixel(img, x0-y, y0-x, c)
		setPixel(img, x0+y, y0-x, c)
		setPixel(img, x0+x, y0-y, c)

		y++
		if decision <= 0 {
			decision += 2*y + 1
		} else {
			x--
			decision += 2*(y-x) + 1
		}
	}
}

// setPixel writes c to (x, y) on img if (x, y) lies within img.Bounds().
func setPixel(img draw.Image, x, y int, c color.Color) {
	b := img.Bounds()
	if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
		return
	}
	img.Set(x, y, c)
}
