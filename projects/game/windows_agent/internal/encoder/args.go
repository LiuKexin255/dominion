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

	return []string{
		ffmpegPath,
		"-f", "gdigrab",
		"-framerate", strconv.Itoa(config.FrameRate),
		"-i", input,
		"-c:v", "libx264",
		"-preset", config.Preset,
		"-tune", config.Tune,
		"-b:v", config.Bitrate,
		"-maxrate", config.Bitrate,
		"-bufsize", bufsize,
		"-vf", "scale=" + strconv.Itoa(config.MaxWidth) + ":" + strconv.Itoa(config.MaxHeight) + ":force_original_aspect_ratio=decrease",
		"-movflags", fmp4Movflags,
		"-f", "mp4",
		"pipe:1",
	}
}
