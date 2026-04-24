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

// NewS3Client creates a new minio.Client configured for SeaweedFS S3 gateway.
// It reads S3_ACCESS_KEY and S3_SECRET_KEY from environment variables.
// If region is empty, it defaults to us-east-1.
func NewS3Client(region string) (*minio.Client, error) {
	accessKey, ok := lookupEnv(S3AccessKeyEnv)
	if !ok || accessKey == "" {
		return nil, fmt.Errorf("read S3 access key: environment variable %q is not set or empty", S3AccessKeyEnv)
	}

	secretKey, ok := lookupEnv(S3SecretKeyEnv)
	if !ok || secretKey == "" {
		return nil, fmt.Errorf("read S3 secret key: environment variable %q is not set or empty", S3SecretKeyEnv)
	}

	if region == "" {
		region = DefaultRegion
	}

	client, err := newMinioClient(Endpoint, &minio.Options{
		Creds:         credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure:        true,
		Region:        region,
		BucketLookup:  minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client for endpoint %q: %w", Endpoint, err)
	}

	return client, nil
}
