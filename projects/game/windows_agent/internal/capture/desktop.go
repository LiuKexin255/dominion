package capture

import "fmt"

// BuildDesktopCropArgs generates ffmpeg gdigrab arguments for desktop capture
// with a crop filter to isolate the window region.
// Output: -f gdigrab -framerate <fps> -i desktop -vf crop=w:h:x:y
func BuildDesktopCropArgs(cfg CaptureConfig) []string {
	w := cfg.Rect.Right - cfg.Rect.Left
	h := cfg.Rect.Bottom - cfg.Rect.Top
	cropFilter := fmt.Sprintf("crop=%d:%d:%d:%d", w, h, cfg.Rect.Left, cfg.Rect.Top)

	return []string{
		"-f", "gdigrab",
		"-framerate", fmt.Sprintf("%d", cfg.FrameRate),
		"-i", "desktop",
		"-vf", cropFilter,
	}
}
