package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dominion/pkg/s3"

	"github.com/minio/minio-go/v7"
	"github.com/spf13/pflag"
)

const (
	// ManifestFilename is the generated release manifest file name.
	ManifestFilename = "manifest.json"
	// SHA256SumsFilename is the generated checksum list file name.
	SHA256SumsFilename = "SHA256SUMS"

	flagVersion = "version"
	flagS3URL   = "s3-url"
	flagDistDir = "dist-dir"
	flagDryRun  = "dry-run"

	envGitCommit       = "GIT_COMMIT"
	envElectronVersion = "ELECTRON_VERSION"
	envFFmpegVersion   = "FFMPEG_VERSION"
	envFFmpegFilename  = "FFMPEG_FILENAME"
)

// S3Destination is a parsed s3://bucket/prefix destination.
type S3Destination struct {
	Bucket string
	Prefix string
}

type options struct {
	version         string
	s3URL           string
	distDir         string
	dryRun          bool
	gitCommit       string
	electronVersion string
	ffmpegVersion   string
	ffmpegFilename  string
	buildTime       time.Time
	destination     *S3Destination
}

type fputObjectFunc func(ctx context.Context, client *minio.Client, bucket string, object string, path string, opts minio.PutObjectOptions) (minio.UploadInfo, error)

var (
	stdout      io.Writer      = os.Stdout
	now                        = time.Now
	lookupEnv                  = os.LookupEnv
	newS3Client                = s3.NewS3Client
	fPutObject  fputObjectFunc = func(ctx context.Context, client *minio.Client, bucket string, object string, path string, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
		return client.FPutObject(ctx, bucket, object, path, opts)
	}
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseOptions(args, now)
	if err != nil {
		return err
	}

	manifest, err := BuildManifest(&ManifestOptions{
		Version:         opts.version,
		DistDir:         opts.distDir,
		GitCommit:       opts.gitCommit,
		BuildTime:       opts.buildTime,
		ElectronVersion: opts.electronVersion,
		FFmpegVersion:   opts.ffmpegVersion,
		FFmpegFilename:  opts.ffmpegFilename,
	})
	if err != nil {
		return err
	}

	if err := writeReleaseFiles(opts.distDir, manifest); err != nil {
		return err
	}

	files := releaseFileList(manifest)
	if opts.dryRun {
		printDryRun(opts, files)
		return nil
	}

	client, err := newS3Client("")
	if err != nil {
		return err
	}
	return uploadFiles(context.Background(), client, opts.destination, opts.distDir, files, fPutObject)
}

func parseOptions(args []string, clock func() time.Time) (*options, error) {
	fs := pflag.NewFlagSet("publish_s3", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)

	opts := new(options)
	fs.StringVar(&opts.version, flagVersion, "", "agent version")
	fs.StringVar(&opts.s3URL, flagS3URL, "", "release destination s3://bucket/prefix/")
	fs.StringVar(&opts.distDir, flagDistDir, "", "directory containing build artifacts")
	fs.BoolVar(&opts.dryRun, flagDryRun, false, "print planned upload operations without uploading")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if len(fs.Args()) != 0 {
		return nil, fmt.Errorf("publish_s3 does not accept positional args")
	}

	opts.version = strings.TrimSpace(opts.version)
	opts.s3URL = strings.TrimSpace(opts.s3URL)
	opts.distDir = strings.TrimSpace(opts.distDir)
	if opts.version == "" {
		return nil, fmt.Errorf("%s must not be empty", flagVersion)
	}
	if opts.s3URL == "" {
		return nil, fmt.Errorf("%s must not be empty", flagS3URL)
	}
	if opts.distDir == "" {
		return nil, fmt.Errorf("%s must not be empty", flagDistDir)
	}

	destination, err := ParseS3URL(opts.s3URL)
	if err != nil {
		return nil, err
	}
	opts.destination = destination
	opts.buildTime = clock()
	opts.gitCommit = envOrDefault(envGitCommit, UnknownGitCommit)
	opts.electronVersion = envOrDefault(envElectronVersion, DefaultElectronVersion)
	opts.ffmpegVersion = envOrDefault(envFFmpegVersion, DefaultFFmpegVersion)
	opts.ffmpegFilename = envOrDefault(envFFmpegFilename, DefaultFFmpegFilename)

	return opts, nil
}

// ParseS3URL extracts bucket and normalized prefix from an s3:// URL.
func ParseS3URL(rawURL string) (*S3Destination, error) {
	if !strings.HasPrefix(rawURL, "s3://") {
		return nil, fmt.Errorf("%s must start with s3://", flagS3URL)
	}

	withoutScheme := strings.TrimPrefix(rawURL, "s3://")
	bucket, prefix, _ := strings.Cut(withoutScheme, "/")
	bucket = strings.TrimSpace(bucket)
	prefix = strings.TrimLeft(prefix, "/")
	if bucket == "" {
		return nil, fmt.Errorf("%s must include bucket", flagS3URL)
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	return &S3Destination{Bucket: bucket, Prefix: prefix}, nil
}

func writeReleaseFiles(distDir string, manifest *Manifest) error {
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal release manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := os.WriteFile(filepath.Join(distDir, ManifestFilename), manifestBytes, 0644); err != nil {
		return fmt.Errorf("write %s: %w", ManifestFilename, err)
	}

	checksums := SHA256Sums(manifest.Artifacts)
	if err := os.WriteFile(filepath.Join(distDir, SHA256SumsFilename), []byte(checksums), 0644); err != nil {
		return fmt.Errorf("write %s: %w", SHA256SumsFilename, err)
	}

	return nil
}

func releaseFileList(manifest *Manifest) []string {
	files := []string{ManifestFilename, SHA256SumsFilename}
	for _, artifact := range manifest.Artifacts {
		files = append(files, artifact.Filename)
	}
	return files
}

func uploadFiles(ctx context.Context, client *minio.Client, destination *S3Destination, distDir string, files []string, uploader fputObjectFunc) error {
	for _, file := range files {
		object := destination.Prefix + file
		path := filepath.Join(distDir, file)
		if _, err := uploader(ctx, client, destination.Bucket, object, path, minio.PutObjectOptions{}); err != nil {
			return fmt.Errorf("upload %q to s3://%s/%s: %w", path, destination.Bucket, object, err)
		}
	}
	return nil
}

func printDryRun(opts *options, files []string) {
	fmt.Fprintf(stdout, "DRY RUN: would upload %d files to %s\n", len(files), opts.s3URL)
	for _, file := range files {
		fmt.Fprintf(stdout, "  %s -> s3://%s/%s%s\n", filepath.Join(opts.distDir, file), opts.destination.Bucket, opts.destination.Prefix, file)
	}
}

func envOrDefault(key string, fallback string) string {
	value, ok := lookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
