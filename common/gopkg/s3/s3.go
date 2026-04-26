// Package s3 provides a client for connecting to SeaweedFS S3 gateway.
package s3

import (
	"fmt"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	// Endpoint is the SeaweedFS S3 gateway address.
	Endpoint = "s3.liukexin.com"

	// S3AccessKeyEnv is the environment variable name for the S3 access key.
	S3AccessKeyEnv = "S3_ACCESS_KEY"

	// S3SecretKeyEnv is the environment variable name for the S3 secret key.
	S3SecretKeyEnv = "S3_SECRET_KEY"

	// DefaultRegion is the default S3 region used when no region is specified.
	DefaultRegion = "us-east-1"
)

var (
	// lookupEnv is the function used to read environment variables.
	// It can be overridden in tests to mock environment variable access.
	lookupEnv = os.LookupEnv

	// newMinioClient is the function used to create a new minio client.
	// It can be overridden in tests to avoid connecting to a real S3 service.
	newMinioClient = minio.New
)

// clientConfig holds the configuration for creating an S3 client.
type clientConfig struct {
	region    string
	accessKey string
	secretKey string
}

// ClientOption configures a clientConfig.
type ClientOption func(*clientConfig)

// WithRegion sets the S3 region.
func WithRegion(region string) ClientOption {
	return func(c *clientConfig) {
		c.region = region
	}
}

// WithAccessKey sets the S3 access key.
func WithAccessKey(key string) ClientOption {
	return func(c *clientConfig) {
		c.accessKey = key
	}
}

// WithSecretKey sets the S3 secret key.
func WithSecretKey(key string) ClientOption {
	return func(c *clientConfig) {
		c.secretKey = key
	}
}

// NewS3Client creates a new minio.Client configured for SeaweedFS S3 gateway.
//
// Region, access key, and secret key can be provided via ClientOption functions.
// If any value is not provided via option, it is read from environment variables:
//   - S3_ACCESS_KEY for the access key
//   - S3_SECRET_KEY for the secret key
//
// If region is empty and no environment variable provides it, it defaults to us-east-1.
func NewS3Client(opts ...ClientOption) (*minio.Client, error) {
	cfg := &clientConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.accessKey == "" {
		key, ok := lookupEnv(S3AccessKeyEnv)
		if !ok || key == "" {
			return nil, fmt.Errorf("read S3 access key: environment variable %q is not set or empty", S3AccessKeyEnv)
		}
		cfg.accessKey = key
	}

	if cfg.secretKey == "" {
		key, ok := lookupEnv(S3SecretKeyEnv)
		if !ok || key == "" {
			return nil, fmt.Errorf("read S3 secret key: environment variable %q is not set or empty", S3SecretKeyEnv)
		}
		cfg.secretKey = key
	}

	if cfg.region == "" {
		cfg.region = DefaultRegion
	}

	client, err := newMinioClient(Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.accessKey, cfg.secretKey, ""),
		Secure:       true,
		Region:       cfg.region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client for endpoint %q: %w", Endpoint, err)
	}

	return client, nil
}
