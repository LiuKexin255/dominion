package capture

import (
	"fmt"

	"dominion/projects/game/windows_agent/internal/window"
)

// CaptureMode enumerates the supported ffmpeg gdigrab capture strategies.
type CaptureMode int

const (
	// ModeHWND captures by window handle: -i hwnd=<HWND>.
	ModeHWND CaptureMode = iota
	// ModeTitle captures by window title: -i title=<title>.
	ModeTitle
	// ModeDesktopCrop captures the full desktop and crops to the window region.
	ModeDesktopCrop
)

// Rect describes a capture region in screen coordinates.
type Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// CaptureConfig holds all parameters needed to build ffmpeg gdigrab arguments.
type CaptureConfig struct {
	Mode      CaptureMode
	HWND      uintptr // Window handle for HWND mode.
	Title     string  // Window title for title mode.
	Rect      Rect    // Capture region for desktop-crop mode.
	FrameRate int     // Frames per second (default 30).
	MaxWidth  int     // Maximum output width (default 1280).
	MaxHeight int     // Maximum output height (default 720).
}

// DefaultCaptureConfig returns a CaptureConfig with standard defaults.
func DefaultCaptureConfig() CaptureConfig {
	return CaptureConfig{
		FrameRate: 30,
		MaxWidth:  1280,
		MaxHeight: 720,
	}
}

// SelectStrategy determines the best capture mode for a window.
// Priority: HWND > Title > DesktopCrop.
func SelectStrategy(w *window.WindowInfo) CaptureMode {
	if w.HWND != 0 {
		return ModeHWND
	}
	if w.Title != "" {
		return ModeTitle
	}
	return ModeDesktopCrop
}

// BuildArgs dispatches to the appropriate builder based on CaptureMode.
func BuildArgs(cfg CaptureConfig) ([]string, error) {
	switch cfg.Mode {
	case ModeHWND:
		return BuildHWNDArgs(cfg), nil
	case ModeTitle:
		return BuildTitleArgs(cfg), nil
	case ModeDesktopCrop:
		return BuildDesktopCropArgs(cfg), nil
	default:
		return nil, fmt.Errorf("unknown capture mode: %d", cfg.Mode)
	}
}
