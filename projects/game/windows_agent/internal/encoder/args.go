package encoder

import "strconv"

const (
	defaultWindowTitle = "windows-agent"
	fmp4Movflags       = "frag_keyframe+empty_moov+default_base_moof"
)

// BuildFFmpegArgs generates the complete ffmpeg command line for gdigrab + H.264 fMP4.
// Output goes to pipe:1 (stdout).
func BuildFFmpegArgs(config EncoderConfig, ffmpegPath string) []string {
	config = normalizeConfig(config)
	input := "title=" + defaultWindowTitle
	if config.HWND != 0 {
		input = "hwnd=" + strconv.FormatUint(uint64(config.HWND), 10)
	}

	bufsize := config.Bitrate
	if bitrateNumber, err := strconv.Atoi(config.Bitrate[:len(config.Bitrate)-1]); err == nil && len(config.Bitrate) > 1 {
		bufsize = strconv.Itoa(bitrateNumber*2) + config.Bitrate[len(config.Bitrate)-1:]
	}

	targetW := config.MaxWidth
	targetH := config.MaxHeight
	if config.CaptureWidth > 0 && config.CaptureWidth < targetW {
		targetW = config.CaptureWidth
	}
	if config.CaptureHeight > 0 && config.CaptureHeight < targetH {
		targetH = config.CaptureHeight
	}

	return []string{
		ffmpegPath,
		"-f", "gdigrab",
		"-draw_mouse", "0",
		"-framerate", strconv.Itoa(config.FrameRate),
		"-i", input,
		"-c:v", "libx264",
		"-profile:v", "baseline",
		"-pix_fmt", "yuv420p",
		"-preset", config.Preset,
		"-tune", config.Tune,
		"-b:v", config.Bitrate,
		"-maxrate", config.Bitrate,
		"-bufsize", bufsize,
		"-g", strconv.Itoa(config.FrameRate * 2),
		"-keyint_min", strconv.Itoa(config.FrameRate),
		"-x264-params", "sliced-threads=0:slices=1",
		"-vf", "scale=" + strconv.Itoa(targetW) + ":" + strconv.Itoa(targetH) + ":force_original_aspect_ratio=decrease,scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-movflags", fmp4Movflags,
		"-f", "mp4",
		"pipe:1",
	}
}
