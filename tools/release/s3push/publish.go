package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"dominion/common/gopkg/s3"

	"github.com/minio/minio-go/v7"
)

// s3Bucket is the S3 bucket for release artifacts.
// Placeholder value; the developer replaces it with "artifacts" before production use.
const s3Bucket = "artifacts"

// Uploader uploads a file to S3.
type Uploader interface {
	Upload(ctx context.Context, bucket, objectKey string, reader io.Reader, objectSize int64) error
}

// S3Uploader uploads files to S3 using a minio client.
type S3Uploader struct {
	client *minio.Client
}

// NewS3Uploader creates an S3Uploader by connecting via pkg/s3.
func NewS3Uploader() (*S3Uploader, error) {
	client, err := s3.NewS3Client()
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}
	return &S3Uploader{client: client}, nil
}

// Upload puts a file into S3 with content type application/octet-stream.
func (u *S3Uploader) Upload(ctx context.Context, bucket, objectKey string, reader io.Reader, objectSize int64) error {
	_, err := u.client.PutObject(ctx, bucket, objectKey, reader, objectSize, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("put object %q to bucket %q: %w", objectKey, bucket, err)
	}
	return nil
}

// ObjectKey constructs the S3 object key for a release file.
// Format: {name}/{version}/{filename}
func ObjectKey(name, version, filename string) string {
	return name + "/" + version + "/" + filename
}

// Publish uploads all staging files to S3 in a fixed order:
// artifact files first, then manifest.json, then SHA256SUMS.
func Publish(ctx context.Context, stagingDir string, manifest *ReleaseManifest, uploader Uploader) error {
	artifacts, manifestFile, sha256File, err := listStagingFiles(stagingDir)
	if err != nil {
		return fmt.Errorf("list staging files: %w", err)
	}

	// Build the full upload sequence: artifacts → manifest.json → SHA256SUMS.
	var uploadOrder []string
	uploadOrder = append(uploadOrder, artifacts...)
	uploadOrder = append(uploadOrder, manifestFile)
	uploadOrder = append(uploadOrder, sha256File)

	for _, filename := range uploadOrder {
		key := ObjectKey(manifest.Name, manifest.Version, filename)
		path := filepath.Join(stagingDir, filename)

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open staging file %q: %w", key, err)
		}

		info, err := f.Stat()
		if err != nil {
			f.Close()
			return fmt.Errorf("stat staging file %q: %w", key, err)
		}

		err = uploader.Upload(ctx, s3Bucket, key, f, info.Size())
		f.Close()
		if err != nil {
			return fmt.Errorf("upload %q: %w", key, err)
		}
	}

	return nil
}

// listStagingFiles separates staging directory entries into artifact files,
// the manifest.json file, and the SHA256SUMS file.
func listStagingFiles(stagingDir string) (artifacts []string, manifestFile string, sha256File string, err error) {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return nil, "", "", fmt.Errorf("read staging dir %q: %w", stagingDir, err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() {
			continue
		}
		switch name {
		case outputManifestFile:
			manifestFile = name
		case sha256SumsFile:
			sha256File = name
		default:
			artifacts = append(artifacts, name)
		}
	}

	return artifacts, manifestFile, sha256File, nil
}
