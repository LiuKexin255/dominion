package encoder

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	// given
	want := EncoderConfig{
		FrameRate: 30,
		MaxWidth:  1280,
		MaxHeight: 720,
		Bitrate:   "1M",
		Preset:    "ultrafast",
		Tune:      "zerolatency",
	}

	// when
	got := DefaultConfig()

	// then
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultConfig() = %#v, want %#v", got, want)
	}
}

func TestBuildFFmpegArgs(t *testing.T) {
	tests := []struct {
		name       string
		config     EncoderConfig
		ffmpegPath string
		want       []string
	}{
		{
			name:       "title input uses defaults",
			config:     EncoderConfig{},
			ffmpegPath: `C:\agent\resources\bin\ffmpeg.exe`,
			want: []string{
				`C:\agent\resources\bin\ffmpeg.exe`, "-f", "gdigrab", "-framerate", "30", "-i", "title=windows-agent",
				"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-b:v", "1M", "-maxrate", "1M", "-bufsize", "2M",
				"-vf", "scale=1280:720:force_original_aspect_ratio=decrease", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1",
			},
		},
		{
			name: "hwnd input uses custom settings",
			config: EncoderConfig{
				HWND:      12345,
				FrameRate: 60,
				MaxWidth:  1920,
				MaxHeight: 1080,
				Bitrate:   "4M",
				Preset:    "veryfast",
				Tune:      "film",
			},
			ffmpegPath: "ffmpeg.exe",
			want: []string{
				"ffmpeg.exe", "-f", "gdigrab", "-framerate", "60", "-i", "hwnd=12345",
				"-c:v", "libx264", "-preset", "veryfast", "-tune", "film", "-b:v", "4M", "-maxrate", "4M", "-bufsize", "8M",
				"-vf", "scale=1920:1080:force_original_aspect_ratio=decrease", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got := BuildFFmpegArgs(tt.config, tt.ffmpegPath)

			// then
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("BuildFFmpegArgs(%#v, %q) = %#v, want %#v", tt.config, tt.ffmpegPath, got, tt.want)
			}
		})
	}
}

func TestBuildFFmpegArgsRequiredFlags(t *testing.T) {
	// given
	args := BuildFFmpegArgs(DefaultConfig(), "ffmpeg.exe")
	wantFlags := []string{"-f", "gdigrab", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1"}

	// when
	for _, want := range wantFlags {
		found := false
		for _, arg := range args {
			if arg == want {
				found = true
			}
		}

		// then
		if !found {
			t.Fatalf("BuildFFmpegArgs() missing required flag %q in %#v", want, args)
		}
	}
}

func TestResolveFFmpegPath(t *testing.T) {
	// given
	agentDir := filepath.Join("tmp", "agent")
	want := filepath.Join(agentDir, "resources", "bin", "ffmpeg.exe")

	// when
	got, err := ResolveFFmpegPath(agentDir)

	// then
	if err != nil {
		t.Fatalf("ResolveFFmpegPath(%q) unexpected error: %v", agentDir, err)
	}
	if got != want {
		t.Fatalf("ResolveFFmpegPath(%q) = %q, want %q", agentDir, got, want)
	}
}

func TestValidateFFmpeg(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		wantErr bool
	}{
		{
			name: "existing file is valid",
			setup: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "ffmpeg.exe")
				if err := os.WriteFile(path, []byte("fake"), 0755); err != nil {
					t.Fatalf("write fake ffmpeg: %v", err)
				}
				return path
			},
		},
		{
			name: "missing file is invalid",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "missing-ffmpeg.exe")
			},
			wantErr: true,
		},
		{
			name: "directory is invalid",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			path := tt.setup(t)

			// when
			err := ValidateFFmpeg(path)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateFFmpeg(%q) expected error", path)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateFFmpeg(%q) unexpected error: %v", path, err)
			}
		})
	}
}

func TestEncoderConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  EncoderConfig
		wantErr bool
	}{
		{name: "zero values use defaults", config: EncoderConfig{}},
		{name: "positive custom values are valid", config: EncoderConfig{FrameRate: 1, MaxWidth: 320, MaxHeight: 240}},
		{name: "negative frame rate is invalid", config: EncoderConfig{FrameRate: -1}, wantErr: true},
		{name: "negative width is invalid", config: EncoderConfig{MaxWidth: -1}, wantErr: true},
		{name: "negative height is invalid", config: EncoderConfig{MaxHeight: -1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := validateConfig(tt.config)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("validateConfig(%#v) expected error", tt.config)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateConfig(%#v) unexpected error: %v", tt.config, err)
			}
		})
	}
}
