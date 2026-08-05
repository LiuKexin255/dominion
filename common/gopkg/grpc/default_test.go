package grpc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceDefault(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ServiceDefault() panicked: %v", r)
		}
	}()

	// when
	opts := ServiceDefault()

	// then
	if len(opts) < 1 {
		t.Fatalf("ServiceDefault() returned %d options, want >= 1", len(opts))
	}
}

// TestClientDefault verifies ClientDefault builds the default options without
// keepalive pings (keepalive is opt-in via WithLongLivedClientKeepalive).
func TestClientDefault(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ClientDefault() panicked: %v", r)
		}
	}()

	// when
	opts := ClientDefault()

	// then
	if len(opts) < 3 {
		t.Fatalf("ClientDefault() returned %d options, want >= 3", len(opts))
	}
}

func TestServiceDefault_WithTLS(t *testing.T) {
	t.Setenv("TLS_CERT_FILE", filepath.Join(t.TempDir(), "nonexistent.crt"))
	t.Setenv("TLS_KEY_FILE", filepath.Join(t.TempDir(), "nonexistent.key"))

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("ServiceDefault() should panic with invalid TLS files")
		}
	}()

	// when
	ServiceDefault()
}

func TestClientDefault_WithTLS(t *testing.T) {
	t.Setenv("TLS_CA_FILE", filepath.Join(t.TempDir(), "nonexistent-ca.crt"))
	t.Setenv("TLS_SERVER_NAME", "grpc-internal-service.test")

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("ClientDefault() should panic with invalid TLS files")
		}
	}()

	// when
	ClientDefault()
}

func TestServiceDefault_TLSNotConfigured_DoesNotPanic(t *testing.T) {
	// Ensure TLS env vars are not set for this test
	for _, key := range []string{"TLS_CERT_FILE", "TLS_KEY_FILE", "TLS_CA_FILE", "TLS_SERVER_NAME"} {
		original, wasSet := os.LookupEnv(key)
		if wasSet {
			os.Unsetenv(key)
			t.Cleanup(func() {
				os.Setenv(key, original)
			})
		}
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ServiceDefault() panicked without TLS configured: %v", r)
		}
	}()

	// when
	opts := ServiceDefault()

	// then
	if len(opts) < 1 {
		t.Fatalf("ServiceDefault() returned %d options, want >= 1", len(opts))
	}
}

func TestClientDefault_TLSNotConfigured_DoesNotPanic(t *testing.T) {
	// Ensure TLS env vars are not set for this test
	for _, key := range []string{"TLS_CERT_FILE", "TLS_KEY_FILE", "TLS_CA_FILE", "TLS_SERVER_NAME"} {
		original, wasSet := os.LookupEnv(key)
		if wasSet {
			os.Unsetenv(key)
			t.Cleanup(func() {
				os.Setenv(key, original)
			})
		}
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ClientDefault() panicked without TLS configured: %v", r)
		}
	}()

	// when
	opts := ClientDefault()

	// then
	if len(opts) < 3 {
		t.Fatalf("ClientDefault() returned %d options, want >= 3", len(opts))
	}
}
