//go:build windows

package capture

import (
	"testing"
)

func TestCapturedImage_Fields(t *testing.T) {
	// given: a CapturedImage value with known fields
	ci := CapturedImage{
		Data:     []byte{0x01, 0x02, 0x03},
		WidthPx:  800,
		HeightPx: 600,
		Encoding: "PNG",
	}

	// then: all fields accessible and equal
	if len(ci.Data) != 3 {
		t.Errorf("Data length: expected 3, got %d", len(ci.Data))
	}
	if ci.WidthPx != 800 {
		t.Errorf("WidthPx: expected 800, got %d", ci.WidthPx)
	}
	if ci.HeightPx != 600 {
		t.Errorf("HeightPx: expected 600, got %d", ci.HeightPx)
	}
	if ci.Encoding != "PNG" {
		t.Errorf("Encoding: expected 'PNG', got %q", ci.Encoding)
	}
}
