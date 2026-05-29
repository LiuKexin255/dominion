// Package capture provides Windows window enumeration and screenshot capture.
package capture

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
)

// EncodePNG encodes an image as PNG bytes.
func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// DecodePNG decodes PNG bytes to an image.
func DecodePNG(data []byte) (image.Image, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode png: %w", err)
	}
	return img, nil
}

// ValidatePNG decodes PNG bytes and verifies image dimensions match expected values.
// Returns an error if decode fails or dimensions do not match.
func ValidatePNG(data []byte, expectedWidth, expectedHeight int) error {
	img, err := DecodePNG(data)
	if err != nil {
		return err
	}
	bounds := img.Bounds()
	gotWidth := bounds.Dx()
	gotHeight := bounds.Dy()
	if gotWidth != expectedWidth || gotHeight != expectedHeight {
		return fmt.Errorf("png size mismatch: expected %dx%d, got %dx%d",
			expectedWidth, expectedHeight, gotWidth, gotHeight)
	}
	return nil
}
