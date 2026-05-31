//go:build !windows

package capture

import (
	"context"
	"testing"
)

func Test_CaptureWindowBounds_Stub(t *testing.T) {
	// given: a non-Windows platform with stub implementation
	// when: calling CaptureWindowBounds
	_, err := CaptureWindowBounds(1)

	// then: returns "not supported" error
	if err == nil {
		t.Fatal("CaptureWindowBounds() expected error, got nil")
	}
	if err.Error() != "not supported on this platform" {
		t.Errorf("CaptureWindowBounds() error = %q, want %q", err.Error(), "not supported on this platform")
	}
}

func TestCaptureWindow_Stub(t *testing.T) {
	// given: a non-Windows platform with stub implementation
	// when: calling CaptureWindow
	img, err := CaptureWindow(context.Background(), 1)

	// then: returns nil image and "not supported" error
	if img != nil {
		t.Fatal("CaptureWindow() expected nil image, got non-nil")
	}
	if err == nil {
		t.Fatal("CaptureWindow() expected error, got nil")
	}
	if err.Error() != "not supported on this platform" {
		t.Errorf("CaptureWindow() error = %q, want %q", err.Error(), "not supported on this platform")
	}
}
