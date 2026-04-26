package capture

import (
	"fmt"
	"strings"
	"testing"

	"dominion/projects/game/windows_agent/internal/window"
)

// containsAll checks that every item in want appears in args in order.
func containsAll(args []string, want ...string) bool {
	idx := 0
	for _, a := range args {
		if idx < len(want) && a == want[idx] {
			idx++
		}
	}
	return idx == len(want)
}

func TestSelectStrategy(t *testing.T) {
	tests := []struct {
		name string
		win  *window.WindowInfo
		want CaptureMode
	}{
		{
			name: "HWND preferred when present",
			win:  &window.WindowInfo{HWND: 12345, Title: "Game", Rect: window.Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}},
			want: ModeHWND,
		},
		{
			name: "Title fallback when HWND is zero",
			win:  &window.WindowInfo{HWND: 0, Title: "Game", Rect: window.Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}},
			want: ModeTitle,
		},
		{
			name: "DesktopCrop fallback when both HWND and Title empty",
			win:  &window.WindowInfo{HWND: 0, Title: "", Rect: window.Rect{Left: 100, Top: 100, Right: 1920, Bottom: 1080}},
			want: ModeDesktopCrop,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got := SelectStrategy(tt.win)

			// then
			if got != tt.want {
				t.Fatalf("SelectStrategy(%+v) = %v, want %v", tt.win, got, tt.want)
			}
		})
	}
}

func TestCalculateScale(t *testing.T) {
	tests := []struct {
		name       string
		srcWidth   int
		srcHeight  int
		maxWidth   int
		maxHeight  int
		wantWidth  int
		wantHeight int
	}{
		{
			name:     "1920x1080 scaled to 1280x720",
			srcWidth: 1920, srcHeight: 1080,
			maxWidth: 1280, maxHeight: 720,
			wantWidth: 1280, wantHeight: 720,
		},
		{
			name:     "800x600 kept as-is",
			srcWidth: 800, srcHeight: 600,
			maxWidth: 1280, maxHeight: 720,
			wantWidth: 800, wantHeight: 600,
		},
		{
			name:     "1280x720 exact match kept as-is",
			srcWidth: 1280, srcHeight: 720,
			maxWidth: 1280, maxHeight: 720,
			wantWidth: 1280, wantHeight: 720,
		},
		{
			name:     "2560x1440 scaled to 1280x720",
			srcWidth: 2560, srcHeight: 1440,
			maxWidth: 1280, maxHeight: 720,
			wantWidth: 1280, wantHeight: 720,
		},
		{
			name:     "3840x2160 scaled to 1280x720",
			srcWidth: 3840, srcHeight: 2160,
			maxWidth: 1280, maxHeight: 720,
			wantWidth: 1280, wantHeight: 720,
		},
		{
			name:     "1920x600 width exceeds height under max",
			srcWidth: 1920, srcHeight: 600,
			maxWidth: 1280, maxHeight: 720,
			wantWidth: 1280, wantHeight: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			gotWidth, gotHeight := CalculateScale(tt.srcWidth, tt.srcHeight, tt.maxWidth, tt.maxHeight)

			// then
			if gotWidth != tt.wantWidth || gotHeight != tt.wantHeight {
				t.Fatalf("CalculateScale(%d, %d, %d, %d) = (%d, %d), want (%d, %d)",
					tt.srcWidth, tt.srcHeight, tt.maxWidth, tt.maxHeight,
					gotWidth, gotHeight, tt.wantWidth, tt.wantHeight)
			}
		})
	}
}

func TestBuildHWNDArgs(t *testing.T) {
	cfg := CaptureConfig{
		Mode:      ModeHWND,
		HWND:      12345,
		FrameRate: 30,
	}

	args := BuildHWNDArgs(cfg)

	if !containsAll(args, "-f", "gdigrab") {
		t.Fatalf("BuildHWNDArgs: expected -f gdigrab, got %v", args)
	}
	if !containsAll(args, "-framerate", "30") {
		t.Fatalf("BuildHWNDArgs: expected -framerate 30, got %v", args)
	}
	if !containsAll(args, "-i", "hwnd=12345") {
		t.Fatalf("BuildHWNDArgs: expected -i hwnd=12345, got %v", args)
	}
}

func TestBuildTitleArgs(t *testing.T) {
	cfg := CaptureConfig{
		Mode:      ModeTitle,
		Title:     "My Game Window",
		FrameRate: 30,
	}

	args := BuildTitleArgs(cfg)

	if !containsAll(args, "-f", "gdigrab") {
		t.Fatalf("BuildTitleArgs: expected -f gdigrab, got %v", args)
	}
	if !containsAll(args, "-framerate", "30") {
		t.Fatalf("BuildTitleArgs: expected -framerate 30, got %v", args)
	}
	if !containsAll(args, "-i", "title=My Game Window") {
		t.Fatalf("BuildTitleArgs: expected -i title=My Game Window, got %v", args)
	}
}

func TestBuildDesktopCropArgs(t *testing.T) {
	cfg := CaptureConfig{
		Mode:      ModeDesktopCrop,
		FrameRate: 30,
		Rect: Rect{
			Left:   100,
			Top:    200,
			Right:  1920,
			Bottom: 1080,
		},
	}

	args := BuildDesktopCropArgs(cfg)

	if !containsAll(args, "-f", "gdigrab") {
		t.Fatalf("BuildDesktopCropArgs: expected -f gdigrab, got %v", args)
	}
	if !containsAll(args, "-framerate", "30") {
		t.Fatalf("BuildDesktopCropArgs: expected -framerate 30, got %v", args)
	}
	if !containsAll(args, "-i", "desktop") {
		t.Fatalf("BuildDesktopCropArgs: expected -i desktop, got %v", args)
	}
	// Expected crop: 1820x880 starting at 100,200.
	expectedCrop := fmt.Sprintf("crop=%d:%d:%d:%d", 1820, 880, 100, 200)
	found := false
	for i, a := range args {
		if a == "-vf" && i+1 < len(args) && args[i+1] == expectedCrop {
			found = true
		}
	}
	if !found {
		t.Fatalf("BuildDesktopCropArgs: expected -vf %s, got %v", expectedCrop, args)
	}
}

func TestDefaultCaptureConfig(t *testing.T) {
	cfg := DefaultCaptureConfig()

	if cfg.FrameRate != 30 {
		t.Fatalf("DefaultCaptureConfig().FrameRate = %d, want 30", cfg.FrameRate)
	}
	if cfg.MaxWidth != 1280 {
		t.Fatalf("DefaultCaptureConfig().MaxWidth = %d, want 1280", cfg.MaxWidth)
	}
	if cfg.MaxHeight != 720 {
		t.Fatalf("DefaultCaptureConfig().MaxHeight = %d, want 720", cfg.MaxHeight)
	}
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name    string
		cfg     CaptureConfig
		wantErr bool
		check   func(args []string) bool
	}{
		{
			name: "HWND mode dispatches correctly",
			cfg: CaptureConfig{
				Mode:      ModeHWND,
				HWND:      999,
				FrameRate: 30,
			},
			check: func(args []string) bool {
				return containsAll(args, "-i", "hwnd=999")
			},
		},
		{
			name: "Title mode dispatches correctly",
			cfg: CaptureConfig{
				Mode:      ModeTitle,
				Title:     "Test",
				FrameRate: 30,
			},
			check: func(args []string) bool {
				return containsAll(args, "-i", "title=Test")
			},
		},
		{
			name: "DesktopCrop mode dispatches correctly",
			cfg: CaptureConfig{
				Mode:      ModeDesktopCrop,
				FrameRate: 30,
				Rect:      Rect{Left: 0, Top: 0, Right: 800, Bottom: 600},
			},
			check: func(args []string) bool {
				return containsAll(args, "-i", "desktop") && containsCrop(args)
			},
		},
		{
			name:    "Unknown mode returns error",
			cfg:     CaptureConfig{Mode: CaptureMode(99)},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			args, err := BuildArgs(tt.cfg)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("BuildArgs(%+v) expected error, got nil", tt.cfg)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("BuildArgs(%+v) unexpected error: %v", tt.cfg, err)
			}
			if !tt.wantErr && !tt.check(args) {
				t.Fatalf("BuildArgs(%+v) check failed, got args: %v", tt.cfg, args)
			}
		})
	}
}

func containsCrop(args []string) bool {
	for i, a := range args {
		if a == "-vf" && i+1 < len(args) && strings.HasPrefix(args[i+1], "crop=") {
			return true
		}
	}
	return false
}
