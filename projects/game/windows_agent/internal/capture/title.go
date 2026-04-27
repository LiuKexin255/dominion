package capture

import "fmt"

// BuildTitleArgs generates ffmpeg gdigrab arguments for title-based capture.
// Output: -f gdigrab -framerate <fps> -i title=<title>
func BuildTitleArgs(cfg CaptureConfig) []string {
	return []string{
		"-f", "gdigrab",
		"-framerate", fmt.Sprintf("%d", cfg.FrameRate),
		"-i", fmt.Sprintf("title=%s", cfg.Title),
	}
}
