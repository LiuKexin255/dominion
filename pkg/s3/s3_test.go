package s3

import (
	"errors"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestNewS3Client(t *testing.T) {
	tests := []struct {
		name      string
		region    string
		env       map[string]string
		clientErr error
		wantErr   bool
		errSubstr string
	}{
		{
			name:   "success with explicit region",
			region: "eu-west-1",
			env: map[string]string{
				S3AccessKeyEnv: "my-access-key",
				S3SecretKeyEnv: "my-secret-key",
			},
		},
		{
			name:   "success with default region",
			region: "",
			env: map[string]string{
				S3AccessKeyEnv: "my-access-key",
				S3SecretKeyEnv: "my-secret-key",
			},
		},
		{
			name: "missing access key",
			env: map[string]string{
				S3SecretKeyEnv: "my-secret-key",
			},
			wantErr:   true,
			errSubstr: S3AccessKeyEnv,
		},
		{
			name: "missing secret key",
			env: map[string]string{
				S3AccessKeyEnv: "my-access-key",
			},
			wantErr:   true,
			errSubstr: S3SecretKeyEnv,
		},
		{
			name: "empty access key",
			env: map[string]string{
				S3AccessKeyEnv: "",
				S3SecretKeyEnv: "my-secret-key",
			},
			wantErr:   true,
			errSubstr: S3AccessKeyEnv,
		},
		{
			name: "empty secret key",
			env: map[string]string{
				S3AccessKeyEnv: "my-access-key",
				S3SecretKeyEnv: "",
			},
			wantErr:   true,
			errSubstr: S3SecretKeyEnv,
		},
		{
			name:      "no env vars at all",
			env:       nil,
			wantErr:   true,
			errSubstr: S3AccessKeyEnv,
		},
		{
			name:   "minio client creation error",
			region: "us-east-1",
			env: map[string]string{
				S3AccessKeyEnv: "my-access-key",
				S3SecretKeyEnv: "my-secret-key",
			},
			clientErr: errors.New("connection refused"),
			wantErr:   true,
			errSubstr: "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			originalNewMinioClient := newMinioClient
			lookupEnv = func(key string) (string, bool) {
				if tt.env == nil {
					return "", false
				}
				value, ok := tt.env[key]
				return value, ok
			}
			newMinioClient = func(endpoint string, opts *minio.Options) (*minio.Client, error) {
				if tt.clientErr != nil {
					return nil, tt.clientErr
				}
				return new(minio.Client), nil
			}
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
				newMinioClient = originalNewMinioClient
			})

			// when
			got, err := NewS3Client(tt.region)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewS3Client(%q) expected error", tt.region)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("NewS3Client(%q) error = %v, want substring %q", tt.region, err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewS3Client(%q) unexpected error: %v", tt.region, err)
			}
			if got == nil {
				t.Fatalf("NewS3Client(%q) = nil, want client", tt.region)
			}
		})
	}
}
