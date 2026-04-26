package encoder

import (
	"fmt"
	"os"
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

// ValidateFFmpeg checks that the ffmpeg binary at the given path exists and is executable.
// On Linux, this does not execute the Windows binary.
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
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("ffmpeg is not executable: %s", ffmpegPath)
	}
	return nil
}
