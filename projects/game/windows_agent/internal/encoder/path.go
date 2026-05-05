package encoder

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const ffmpegRelativePath = "resources/bin/ffmpeg.exe"

// ResolveFFmpegPath resolves the ffmpeg executable path from the agent directory.
// Expected layout: resources/bin/ffmpeg.exe in the same directory as the agent binary.
func ResolveFFmpegPath(agentDir string) (string, error) {
	if agentDir == "" {
		return "", fmt.Errorf("agent directory is empty")
	}
	return filepath.Join(agentDir, ffmpegRelativePath), nil
}

func ValidateFFmpeg(ffmpegPath string) error {
	if ffmpegPath == "" {
		return fmt.Errorf("ffmpeg path is empty")
	}
	info, err := os.Stat(ffmpegPath)
	if err != nil {
		return fmt.Errorf("stat ffmpeg: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("ffmpeg path is a directory: %s", ffmpegPath)
	}
	if runtime.GOOS != "windows" {
		return nil
	}
	cmd := exec.Command(ffmpegPath, "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg is not executable: %s: %w", ffmpegPath, err)
	}
	if len(out) == 0 {
		return fmt.Errorf("ffmpeg produced no output: %s", ffmpegPath)
	}
	return nil
}
