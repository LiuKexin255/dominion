package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	outputManifestFile = "manifest.json"
	sha256SumsFile     = "SHA256SUMS"
)

// OutputArtifact describes one staged artifact in the public JSON manifest.
type OutputArtifact struct {
	Filename string `json:"filename"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// OutputManifest describes staged release metadata for download clients.
type OutputManifest struct {
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Artifacts []*OutputArtifact `json:"artifacts"`
}

// StageArtifacts copies artifact files into stagingDir using manifest filenames.
func StageArtifacts(stagingDir string, artifacts []*Artifact, artifactPaths map[string]string) error {
	for _, artifact := range artifacts {
		sourcePath, ok := artifactPaths[artifact.Target]
		if !ok {
			return fmt.Errorf("artifact label %q not found", artifact.Target)
		}

		info, err := os.Stat(sourcePath)
		if err != nil {
			return fmt.Errorf("stat artifact %q file %q: %w", artifact.Target, sourcePath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("artifact %q file %q is not a regular file", artifact.Target, sourcePath)
		}

		if err := copyFile(filepath.Join(stagingDir, artifact.Filename), sourcePath); err != nil {
			return fmt.Errorf("stage artifact %q: %w", artifact.Target, err)
		}
	}

	return nil
}

// GenerateOutputManifest writes manifest.json for staged artifacts and returns it.
func GenerateOutputManifest(manifest *ReleaseManifest, stagingDir string) (*OutputManifest, error) {
	output := &OutputManifest{
		Name:      manifest.Name,
		Version:   manifest.Version,
		Artifacts: nil,
	}

	for _, artifact := range manifest.Artifacts {
		path := filepath.Join(stagingDir, artifact.Filename)
		size, err := fileSize(path)
		if err != nil {
			return nil, fmt.Errorf("size staged artifact %q: %w", artifact.Filename, err)
		}
		sha256Hex, err := fileSHA256(path)
		if err != nil {
			return nil, fmt.Errorf("hash staged artifact %q: %w", artifact.Filename, err)
		}

		output.Artifacts = append(output.Artifacts, &OutputArtifact{
			Filename: artifact.Filename,
			Platform: artifact.Platform,
			Arch:     artifact.Arch,
			Size:     size,
			SHA256:   sha256Hex,
		})
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal output manifest: %w", err)
	}
	path := filepath.Join(stagingDir, outputManifestFile)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, fmt.Errorf("write output manifest %q: %w", path, err)
	}

	return output, nil
}

// GenerateSHA256SUMS writes a sorted SHA256SUMS file for all staged files.
func GenerateSHA256SUMS(stagingDir string) error {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return fmt.Errorf("read staging dir %q: %w", stagingDir, err)
	}

	var filenames []string
	for _, entry := range entries {
		if entry.Name() == sha256SumsFile {
			continue
		}
		if entry.Type().IsRegular() {
			filenames = append(filenames, entry.Name())
		}
	}
	sort.Strings(filenames)

	var b strings.Builder
	for _, filename := range filenames {
		sha256Hex, err := fileSHA256(filepath.Join(stagingDir, filename))
		if err != nil {
			return fmt.Errorf("hash staged file %q: %w", filename, err)
		}
		fmt.Fprintf(&b, "%s  %s\n", sha256Hex, filename)
	}

	path := filepath.Join(stagingDir, sha256SumsFile)
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("write SHA256SUMS %q: %w", path, err)
	}

	return nil
}

// fileSHA256 computes a file SHA256 hash and returns it as a hex string.
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

// fileSize returns a file size in bytes.
func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat file %q: %w", path, err)
	}
	return info.Size(), nil
}

func copyFile(dstPath, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source %q: %w", srcPath, err)
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create destination %q: %w", dstPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy %q to %q: %w", srcPath, dstPath, err)
	}

	return nil
}
