package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// PlatformWindows represents the Windows platform identifier.
	PlatformWindows = "windows/amd64"

	// SHA256SUMSFile is the standard filename for checksum files.
	SHA256SUMSFile = "SHA256SUMS"

	// ManifestFile is the filename for the release manifest.
	ManifestFile = "manifest.json"
)

// Manifest describes a release including metadata and artifact information.
type Manifest struct {
	Version     string         `json:"version"`
	GitCommit   string         `json:"gitCommit"`
	BuildTime   string         `json:"buildTime"`
	Platform    string         `json:"platform"`
	Artifacts   []ArtifactInfo `json:"artifacts"`
	Wails       ToolInfo       `json:"wails"`
	FFmpeg      ToolInfo       `json:"ffmpeg"`
	InputHelper ToolInfo       `json:"inputHelper"`
}

// ArtifactInfo describes a single release artifact file.
type ArtifactInfo struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// ToolInfo describes a bundled tool version and its provenance.
type ToolInfo struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	SHA256  string `json:"sha256"`
}

// GenerateManifest creates a Manifest by inspecting artifacts in distDir.
//
// It walks distDir for files, computes SHA256 hashes, and collects file sizes.
// Git commit information is read from the environment or defaults to "unknown".
func GenerateManifest(version, distDir string) (*Manifest, error) {
	var artifacts []ArtifactInfo

	entries, err := os.ReadDir(distDir)
	if err != nil {
		return nil, fmt.Errorf("read dist dir %q: %w", distDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("get file info for %q: %w", entry.Name(), err)
		}

		filePath := filepath.Join(distDir, entry.Name())
		hash, err := fileSHA256(filePath)
		if err != nil {
			return nil, fmt.Errorf("hash file %q: %w", entry.Name(), err)
		}

		artifacts = append(artifacts, ArtifactInfo{
			Filename: entry.Name(),
			Size:     info.Size(),
			SHA256:   hash,
		})
	}

	gitCommit := getGitCommit()

	manifest := &Manifest{
		Version:   version,
		GitCommit: gitCommit,
		BuildTime: time.Now().UTC().Format(time.RFC3339),
		Platform:  PlatformWindows,
		Artifacts: artifacts,
		Wails: ToolInfo{
			Version: "bundled",
			Source:  "wails",
		},
		FFmpeg: ToolInfo{
			Version: "bundled",
			Source:  "ffmpeg",
		},
		InputHelper: ToolInfo{
			Version: "bundled",
			Source:  "input-helper",
		},
	}

	return manifest, nil
}

// GenerateSHA256SUMS creates a SHA256SUMS file content in standard format.
//
// Each line follows the pattern: `<sha256_hash>  <filename>`
// It walks distDir for files and computes their SHA256 hashes.
func GenerateSHA256SUMS(distDir string) (string, error) {
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return "", fmt.Errorf("read dist dir %q: %w", distDir, err)
	}

	var lines []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(distDir, entry.Name())
		hash, err := fileSHA256(filePath)
		if err != nil {
			return "", fmt.Errorf("hash file %q: %w", entry.Name(), err)
		}

		lines = append(lines, fmt.Sprintf("%s  %s", hash, entry.Name()))
	}

	return strings.Join(lines, "\n") + "\n", nil
}

// WriteManifest serializes the manifest to JSON and writes it to the given directory.
func WriteManifest(m *Manifest, dir string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	path := filepath.Join(dir, ManifestFile)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write manifest to %q: %w", path, err)
	}

	return nil
}

// WriteSHA256SUMS writes the SHA256SUMS content to the given directory.
func WriteSHA256SUMS(content, dir string) error {
	path := filepath.Join(dir, SHA256SUMSFile)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write SHA256SUMS to %q: %w", path, err)
	}

	return nil
}

// fileSHA256 computes the SHA256 hash of a file and returns it as a hex string.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file %q: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file %q: %w", path, err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// getGitCommit returns the current git commit hash.
//
// It first checks the GIT_COMMIT environment variable, then falls back
// to reading the .git/HEAD file. Returns "unknown" if neither source
// provides a value.
func getGitCommit() string {
	if commit, ok := os.LookupEnv("GIT_COMMIT"); ok && commit != "" {
		return commit
	}

	data, err := os.ReadFile(".git/HEAD")
	if err != nil {
		return "unknown"
	}

	ref := strings.TrimSpace(string(data))
	if strings.HasPrefix(ref, "ref: ") {
		refPath := strings.TrimPrefix(ref, "ref: ")
		commitData, err := os.ReadFile(filepath.Join(".git", refPath))
		if err != nil {
			return "unknown"
		}
		return strings.TrimSpace(string(commitData))
	}

	return ref
}
