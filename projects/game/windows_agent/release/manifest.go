package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// DefaultPlatform is the platform published by this release tool.
	DefaultPlatform = "windows"
	// DefaultArch is the architecture published by this release tool.
	DefaultArch = "amd64"
	// DefaultElectronVersion is used when no build metadata override is provided.
	DefaultElectronVersion = "35.0.0"
	// DefaultFFmpegVersion is used when no build metadata override is provided.
	DefaultFFmpegVersion = "7.1"
	// DefaultFFmpegFilename is the artifact name used for ffmpeg checksum metadata.
	DefaultFFmpegFilename = "ffmpeg.exe"
	// UnknownGitCommit is used when the build does not provide git metadata.
	UnknownGitCommit = "unknown"
)

// Artifact describes one file included in a release.
type Artifact struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// Manifest describes a windows agent release payload.
type Manifest struct {
	Version         string      `json:"version"`
	GitCommit       string      `json:"git_commit"`
	BuildTime       string      `json:"build_time"`
	Platform        string      `json:"platform"`
	Arch            string      `json:"arch"`
	ElectronVersion string      `json:"electron_version"`
	FFmpegVersion   string      `json:"ffmpeg_version"`
	FFmpegSHA256    string      `json:"ffmpeg_sha256"`
	Artifacts       []*Artifact `json:"artifacts"`
}

// ManifestOptions contains release metadata needed to generate manifest.json.
type ManifestOptions struct {
	Version         string
	DistDir         string
	GitCommit       string
	BuildTime       time.Time
	ElectronVersion string
	FFmpegVersion   string
	FFmpegFilename  string
}

// BuildManifest scans a dist directory and builds deterministic release metadata.
func BuildManifest(opts *ManifestOptions) (*Manifest, error) {
	entries, err := os.ReadDir(opts.DistDir)
	if err != nil {
		return nil, fmt.Errorf("read dist dir %q: %w", opts.DistDir, err)
	}

	var artifacts []*Artifact
	for _, entry := range entries {
		if entry.IsDir() || isGeneratedReleaseFile(entry.Name()) {
			continue
		}

		path := filepath.Join(opts.DistDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat artifact %q: %w", path, err)
		}
		checksum, err := SHA256File(path)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, &Artifact{
			Filename: entry.Name(),
			Size:     info.Size(),
			SHA256:   checksum,
		})
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Filename < artifacts[j].Filename
	})

	manifest := &Manifest{
		Version:         opts.Version,
		GitCommit:       defaultString(opts.GitCommit, UnknownGitCommit),
		BuildTime:       opts.BuildTime.UTC().Format(time.RFC3339),
		Platform:        DefaultPlatform,
		Arch:            DefaultArch,
		ElectronVersion: defaultString(opts.ElectronVersion, DefaultElectronVersion),
		FFmpegVersion:   defaultString(opts.FFmpegVersion, DefaultFFmpegVersion),
		Artifacts:       artifacts,
	}

	ffmpegFilename := defaultString(opts.FFmpegFilename, DefaultFFmpegFilename)
	for _, artifact := range artifacts {
		if artifact.Filename == ffmpegFilename {
			manifest.FFmpegSHA256 = artifact.SHA256
			break
		}
	}

	return manifest, nil
}

func isGeneratedReleaseFile(filename string) bool {
	return filename == ManifestFilename || filename == SHA256SumsFilename
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
