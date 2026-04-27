package capture

import "fmt"

// BuildHWNDArgs generates ffmpeg gdigrab arguments for HWND-based capture.
// Output: -f gdigrab -framerate <fps> -i hwnd=<HWND>
func BuildHWNDArgs(cfg CaptureConfig) []string {
	return []string{
		"-f", "gdigrab",
		"-framerate", fmt.Sprintf("%d", cfg.FrameRate),
		"-i", fmt.Sprintf("hwnd=%d", cfg.HWND),
	}
}
