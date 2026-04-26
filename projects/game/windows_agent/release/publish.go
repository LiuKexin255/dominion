package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"dominion/pkg/s3"

	"github.com/minio/minio-go/v7"
)

const (
	// s3Scheme is the URL scheme expected for S3 URLs.
	s3Scheme = "s3"
)

// Publish validates the distribution directory, generates manifest.json and SHA256SUMS,
// then uploads all artifacts to the specified S3 location.
//
// s3URL must be in the format "s3://bucket/prefix".
// distDir must be an existing directory containing artifact files.
func publish(version, s3URL, distDir string) error {
	if err := validateDistDir(distDir); err != nil {
		return fmt.Errorf("validate dist dir: %w", err)
	}

	bucket, prefix, err := parseS3URL(s3URL)
	if err != nil {
		return fmt.Errorf("parse S3 URL: %w", err)
	}

	manifest, err := GenerateManifest(version, distDir)
	if err != nil {
		return fmt.Errorf("generate manifest: %w", err)
	}

	if err := WriteManifest(manifest, distDir); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	sumsContent, err := GenerateSHA256SUMS(distDir)
	if err != nil {
		return fmt.Errorf("generate SHA256SUMS: %w", err)
	}

	if err := WriteSHA256SUMS(sumsContent, distDir); err != nil {
		return fmt.Errorf("write SHA256SUMS: %w", err)
	}

	client, err := s3.NewS3Client("")
	if err != nil {
		return fmt.Errorf("create S3 client: %w", err)
	}

	ctx := context.Background()

	if err := uploadDir(ctx, client, bucket, prefix, distDir); err != nil {
		return fmt.Errorf("upload to S3: %w", err)
	}

	return nil
}

// parseS3URL parses an S3 URL into bucket and prefix components.
//
// Expected format: "s3://bucket/prefix"
// The prefix may be empty, in which case files are uploaded to the bucket root.
func parseS3URL(s3URL string) (bucket, prefix string, err error) {
	u, err := url.Parse(s3URL)
	if err != nil {
		return "", "", fmt.Errorf("parse URL %q: %w", s3URL, err)
	}

	if u.Scheme != s3Scheme {
		return "", "", fmt.Errorf("unsupported scheme %q, expected %q", u.Scheme, s3Scheme)
	}

	bucket = u.Host
	if bucket == "" {
		return "", "", fmt.Errorf("missing bucket name in S3 URL %q", s3URL)
	}

	prefix = strings.TrimPrefix(u.Path, "/")
	prefix = strings.TrimSuffix(prefix, "/")
	return bucket, prefix, nil
}

// validateDistDir checks that the distribution directory exists and contains files.
func validateDistDir(distDir string) error {
	info, err := os.Stat(distDir)
	if err != nil {
		return fmt.Errorf("stat dist dir %q: %w", distDir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("dist dir %q is not a directory", distDir)
	}

	entries, err := os.ReadDir(distDir)
	if err != nil {
		return fmt.Errorf("read dist dir %q: %w", distDir, err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("dist dir %q is empty", distDir)
	}

	return nil
}

// uploadDir uploads all files in distDir to the S3 bucket under the given prefix.
func uploadDir(ctx context.Context, client *minio.Client, bucket, prefix, distDir string) error {
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return fmt.Errorf("read dist dir %q: %w", distDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(distDir, entry.Name())
		s3Key := s3ObjectKey(prefix, entry.Name())

		if err := uploadFile(ctx, client, bucket, s3Key, filePath); err != nil {
			return fmt.Errorf("upload %q: %w", entry.Name(), err)
		}
	}

	return nil
}

// uploadFile uploads a single file to S3.
func uploadFile(ctx context.Context, client *minio.Client, bucket, key, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file %q: %w", filePath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file %q: %w", filePath, err)
	}

	_, err = client.PutObject(ctx, bucket, key, f, info.Size(), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("put object %q to bucket %q: %w", key, bucket, err)
	}

	return nil
}

// s3ObjectKey constructs an S3 object key from a prefix and filename.
func s3ObjectKey(prefix, filename string) string {
	if prefix == "" {
		return filename
	}
	return prefix + "/" + filename
}
