package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
)

// SHA256File computes the hexadecimal SHA256 checksum for a file.
func SHA256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file %q for checksum: %w", path, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("read file %q for checksum: %w", path, err)
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// SHA256Sums renders artifact checksums in the standard sha256sum format.
func SHA256Sums(artifacts []*Artifact) string {
	var builder strings.Builder
	for _, artifact := range artifacts {
		fmt.Fprintf(&builder, "%s  %s\n", artifact.SHA256, artifact.Filename)
	}
	return builder.String()
}
