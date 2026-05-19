package s3

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"dominion/common/gopkg/logs"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestNewS3Client(t *testing.T) {
	tests := []struct {
		name          string
		opts          []ClientOption
		env           map[string]string
		clientErr     error
		wantErr       bool
		errSubstr     string
		wantRegion    string
		wantAccessKey string
		wantSecretKey string
	}{
		{
			name: "success with explicit region via option",
			opts: []ClientOption{WithRegion("eu-west-1")},
			env: map[string]string{
				S3AccessKeyEnv: "my-access-key",
				S3SecretKeyEnv: "my-secret-key",
			},
			wantRegion:    "eu-west-1",
			wantAccessKey: "my-access-key",
			wantSecretKey: "my-secret-key",
		},
		{
			name: "success with default region",
			env: map[string]string{
				S3AccessKeyEnv: "my-access-key",
				S3SecretKeyEnv: "my-secret-key",
			},
			wantRegion:    DefaultRegion,
			wantAccessKey: "my-access-key",
			wantSecretKey: "my-secret-key",
		},
		{
			name: "missing access key from env",
			env: map[string]string{
				S3SecretKeyEnv: "my-secret-key",
			},
			wantErr:   true,
			errSubstr: S3AccessKeyEnv,
		},
		{
			name: "missing secret key from env",
			env: map[string]string{
				S3AccessKeyEnv: "my-access-key",
			},
			wantErr:   true,
			errSubstr: S3SecretKeyEnv,
		},
		{
			name: "empty access key from env",
			env: map[string]string{
				S3AccessKeyEnv: "",
				S3SecretKeyEnv: "my-secret-key",
			},
			wantErr:   true,
			errSubstr: S3AccessKeyEnv,
		},
		{
			name: "empty secret key from env",
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
			name: "minio client creation error",
			opts: []ClientOption{WithRegion("us-east-1")},
			env: map[string]string{
				S3AccessKeyEnv: "my-access-key",
				S3SecretKeyEnv: "my-secret-key",
			},
			clientErr: errors.New("connection refused"),
			wantErr:   true,
			errSubstr: "connection refused",
		},
		{
			name:          "success with explicit credentials via options",
			opts:          []ClientOption{WithAccessKey("opt-access"), WithSecretKey("opt-secret")},
			env:           nil,
			wantRegion:    DefaultRegion,
			wantAccessKey: "opt-access",
			wantSecretKey: "opt-secret",
		},
		{
			name: "success with all options",
			opts: []ClientOption{
				WithRegion("ap-northeast-1"),
				WithAccessKey("opt-access"),
				WithSecretKey("opt-secret"),
			},
			env:           nil,
			wantRegion:    "ap-northeast-1",
			wantAccessKey: "opt-access",
			wantSecretKey: "opt-secret",
		},
		{
			name: "options override env variables",
			opts: []ClientOption{
				WithAccessKey("opt-access"),
				WithSecretKey("opt-secret"),
			},
			env: map[string]string{
				S3AccessKeyEnv: "env-access",
				S3SecretKeyEnv: "env-secret",
			},
			wantRegion:    DefaultRegion,
			wantAccessKey: "opt-access",
			wantSecretKey: "opt-secret",
		},
		{
			name:      "options override env with empty values",
			opts:      []ClientOption{WithAccessKey(""), WithSecretKey("")},
			env:       nil,
			wantErr:   true,
			errSubstr: S3AccessKeyEnv,
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

			var capturedOpts *minio.Options
			newMinioClient = func(endpoint string, opts *minio.Options) (*minio.Client, error) {
				capturedOpts = opts
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
			got, err := NewS3Client(tt.opts...)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewS3Client(%+v) expected error", tt.opts)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("NewS3Client(%+v) error = %v, want substring %q", tt.opts, err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewS3Client(%+v) unexpected error: %v", tt.opts, err)
			}
			if got == nil {
				t.Fatalf("NewS3Client(%+v) = nil, want client", tt.opts)
			}

			// Verify region was passed correctly
			if capturedOpts == nil {
				t.Fatal("newMinioClient was not called")
			}
			if capturedOpts.Region != tt.wantRegion {
				t.Fatalf("region = %q, want %q", capturedOpts.Region, tt.wantRegion)
			}

			// Verify credentials were passed correctly
			if capturedOpts.Creds == nil {
				t.Fatal("creds = nil, want non-nil")
			}
			credsValue, err := capturedOpts.Creds.GetWithContext(&credentials.CredContext{})
			if err != nil {
				t.Fatalf("get credentials: %v", err)
			}
			if credsValue.AccessKeyID != tt.wantAccessKey {
				t.Fatalf("access key = %q, want %q", credsValue.AccessKeyID, tt.wantAccessKey)
			}
			if credsValue.SecretAccessKey != tt.wantSecretKey {
				t.Fatalf("secret key = %q, want %q", credsValue.SecretAccessKey, tt.wantSecretKey)
			}
		})
	}
}

func TestWithRegion(t *testing.T) {
	cfg := &clientConfig{}
	WithRegion("test-region")(cfg)
	if cfg.region != "test-region" {
		t.Fatalf("region = %q, want %q", cfg.region, "test-region")
	}
}

func TestWithAccessKey(t *testing.T) {
	cfg := &clientConfig{}
	WithAccessKey("test-key")(cfg)
	if cfg.accessKey != "test-key" {
		t.Fatalf("accessKey = %q, want %q", cfg.accessKey, "test-key")
	}
}

func TestWithSecretKey(t *testing.T) {
	cfg := &clientConfig{}
	WithSecretKey("test-secret")(cfg)
	if cfg.secretKey != "test-secret" {
		t.Fatalf("secretKey = %q, want %q", cfg.secretKey, "test-secret")
	}
}

func TestClientOption_multiple(t *testing.T) {
	cfg := &clientConfig{}
	opt1 := WithRegion("r1")
	opt2 := WithAccessKey("k1")
	opt3 := WithSecretKey("s1")

	for _, opt := range []ClientOption{opt1, opt2, opt3} {
		opt(cfg)
	}

	if cfg.region != "r1" {
		t.Fatalf("region = %q, want %q", cfg.region, "r1")
	}
	if cfg.accessKey != "k1" {
		t.Fatalf("accessKey = %q, want %q", cfg.accessKey, "k1")
	}
	if cfg.secretKey != "s1" {
		t.Fatalf("secretKey = %q, want %q", cfg.secretKey, "s1")
	}
}

func TestNewS3Client_Logging(t *testing.T) {
	secrets := map[string]string{
		S3AccessKeyEnv: "test-access-key",
		S3SecretKeyEnv: "test-secret-key",
	}

	t.Run("success info log contains endpoint and region", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		logs.SetDefault(logger)

		originalLookupEnv := lookupEnv
		originalNewMinioClient := newMinioClient
		lookupEnv = func(key string) (string, bool) {
			value, ok := secrets[key]
			return value, ok
		}
		newMinioClient = func(_ string, _ *minio.Options) (*minio.Client, error) {
			return new(minio.Client), nil
		}
		t.Cleanup(func() {
			lookupEnv = originalLookupEnv
			newMinioClient = originalNewMinioClient
		})

		_, err := NewS3Client()
		if err != nil {
			t.Fatalf("NewS3Client() unexpected error: %v", err)
		}

		logOutput := buf.String()

		// Verify info log message
		if !strings.Contains(logOutput, "s3 client initializing") {
			t.Fatalf("log output missing info message, got: %q", logOutput)
		}
		// Verify endpoint and region are logged
		if !strings.Contains(logOutput, Endpoint) {
			t.Fatalf("log output missing endpoint %q, got: %q", Endpoint, logOutput)
		}
		if !strings.Contains(logOutput, DefaultRegion) {
			t.Fatalf("log output missing region %q, got: %q", DefaultRegion, logOutput)
		}
		// CRITICAL: Verify access key and secret key are NOT in log output
		if strings.Contains(logOutput, "test-access-key") {
			t.Fatalf("log output MUST NOT contain access key, got: %q", logOutput)
		}
		if strings.Contains(logOutput, "test-secret-key") {
			t.Fatalf("log output MUST NOT contain secret key, got: %q", logOutput)
		}
	})

	t.Run("error log on client creation failure", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		logs.SetDefault(logger)

		originalLookupEnv := lookupEnv
		originalNewMinioClient := newMinioClient
		lookupEnv = func(key string) (string, bool) {
			value, ok := secrets[key]
			return value, ok
		}
		expectedErr := errors.New("connection refused")
		newMinioClient = func(_ string, _ *minio.Options) (*minio.Client, error) {
			return nil, expectedErr
		}
		t.Cleanup(func() {
			lookupEnv = originalLookupEnv
			newMinioClient = originalNewMinioClient
		})

		_, err := NewS3Client()
		if err == nil {
			t.Fatal("NewS3Client() expected error, got nil")
		}

		logOutput := buf.String()

		// Info log should still appear before client creation
		if !strings.Contains(logOutput, "s3 client initializing") {
			t.Fatalf("log output missing info message, got: %q", logOutput)
		}
		// Error log should appear on failure
		if !strings.Contains(logOutput, "s3 client creation failed") {
			t.Fatalf("log output missing error message, got: %q", logOutput)
		}
		// Verify error detail is logged
		if !strings.Contains(logOutput, "connection refused") {
			t.Fatalf("log output missing error detail, got: %q", logOutput)
		}
		// Verify endpoint is in log output
		if !strings.Contains(logOutput, Endpoint) {
			t.Fatalf("log output missing endpoint %q, got: %q", Endpoint, logOutput)
		}
		// CRITICAL: Verify access key and secret key are NOT in log output
		if strings.Contains(logOutput, "test-access-key") {
			t.Fatalf("log output MUST NOT contain access key, got: %q", logOutput)
		}
		if strings.Contains(logOutput, "test-secret-key") {
			t.Fatalf("log output MUST NOT contain secret key, got: %q", logOutput)
		}
	})
}
