package encoder

const (
	defaultFrameRate = 30
	defaultMaxWidth  = 1280
	defaultMaxHeight = 720
	defaultBitrate   = "1M"
	defaultPreset    = "ultrafast"
	defaultTune      = "zerolatency"
)

// EncoderConfig configures ffmpeg gdigrab capture and H.264 fragmented MP4 output.
type EncoderConfig struct {
	HWND      uintptr // target window handle
	FrameRate int     // fps, default 30
	MaxWidth  int     // max width, default 1280
	MaxHeight int     // max height, default 720
	Bitrate   string  // bitrate string, default "1M"
	Preset    string  // x264 preset, default "ultrafast"
	Tune      string  // x264 tune, default "zerolatency"
}

// DefaultConfig returns the default encoder configuration for low-latency streaming.
func DefaultConfig() EncoderConfig {
	return EncoderConfig{
		FrameRate: defaultFrameRate,
		MaxWidth:  defaultMaxWidth,
		MaxHeight: defaultMaxHeight,
		Bitrate:   defaultBitrate,
		Preset:    defaultPreset,
		Tune:      defaultTune,
	}
}

func normalizeConfig(config EncoderConfig) EncoderConfig {
	defaultConfig := DefaultConfig()
	if config.FrameRate == 0 {
		config.FrameRate = defaultConfig.FrameRate
	}
	if config.MaxWidth == 0 {
		config.MaxWidth = defaultConfig.MaxWidth
	}
	if config.MaxHeight == 0 {
		config.MaxHeight = defaultConfig.MaxHeight
	}
	if config.Bitrate == "" {
		config.Bitrate = defaultConfig.Bitrate
	}
	if config.Preset == "" {
		config.Preset = defaultConfig.Preset
	}
	if config.Tune == "" {
		config.Tune = defaultConfig.Tune
	}
	return config
}
