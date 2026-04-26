package tls

import (
	"context"
	stdtls "crypto/tls"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientTransportCredentials(t *testing.T) {
	validCAFile := testdataPath(t, "server_valid.crt")

	tests := []struct {
		name      string
		env       map[string]string
		wantOK    bool
		wantPanic bool
	}{
		{name: "returns nil without tls env"},
		{
			name: "returns nil when server name is missing",
			env: map[string]string{
				"TLS_CA_FILE": validCAFile,
			},
		},
		{
			name: "builds credentials from tls env",
			env: map[string]string{
				"TLS_CA_FILE":     validCAFile,
				"TLS_SERVER_NAME": "grpc-internal-service.test",
			},
			wantOK: true,
		},
		{
			name: "panics when CA file is missing",
			env: map[string]string{
				"TLS_CA_FILE":     filepath.Join(t.TempDir(), "missing-ca.crt"),
				"TLS_SERVER_NAME": "grpc-internal-service.test",
			},
			wantPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
			})

			lookupEnv = func(name string) (string, bool) {
				value, ok := tt.env[name]
				return value, ok
			}

			defer func() {
				recovered := recover()
				if tt.wantPanic {
					if recovered == nil {
						t.Fatal("ClientTransportCredentials() did not panic")
					}
					return
				}
				if recovered != nil {
					t.Fatalf("ClientTransportCredentials() panicked: %v", recovered)
				}
			}()

			got := ClientTransportCredentials()
			if tt.wantOK && got == nil {
				t.Fatal("ClientTransportCredentials() = nil, want credentials")
			}
			if !tt.wantOK && got != nil {
				t.Fatalf("ClientTransportCredentials() = %#v, want nil", got)
			}
		})
	}
}

func TestServerTransportCredentials(t *testing.T) {
	validCertFile := testdataPath(t, "server_valid.crt")
	validKeyFile := testdataPath(t, "server_valid.key")

	tests := []struct {
		name      string
		env       map[string]string
		wantOK    bool
		wantPanic bool
	}{
		{name: "returns nil without tls env"},
		{
			name: "panics when cert file is invalid",
			env: map[string]string{
				"TLS_CERT_FILE": filepath.Join(t.TempDir(), "missing.crt"),
				"TLS_KEY_FILE":  validKeyFile,
			},
			wantPanic: true,
		},
		{
			name: "builds credentials from tls env",
			env: map[string]string{
				"TLS_CERT_FILE": validCertFile,
				"TLS_KEY_FILE":  validKeyFile,
			},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
			})

			lookupEnv = func(name string) (string, bool) {
				value, ok := tt.env[name]
				return value, ok
			}

			defer func() {
				recovered := recover()
				if tt.wantPanic {
					if recovered == nil {
						t.Fatal("ServerTransportCredentials() did not panic")
					}
					return
				}
				if recovered != nil {
					t.Fatalf("ServerTransportCredentials() panicked: %v", recovered)
				}
			}()

			got := ServerTransportCredentials()
			if tt.wantOK && got == nil {
				t.Fatal("ServerTransportCredentials() = nil, want credentials")
			}
			if !tt.wantOK && got != nil {
				t.Fatalf("ServerTransportCredentials() = %#v, want nil", got)
			}
		})
	}
}

func TestNewServerTransportCredentials(t *testing.T) {
	validCertFile := testdataPath(t, "server_valid.crt")
	validKeyFile := testdataPath(t, "server_valid.key")
	invalidPEMFile := testdataPath(t, "invalid.pem")
	validCertPEM := mustReadFile(t, validCertFile)
	validKeyPEM := mustReadFile(t, validKeyFile)

	tests := []struct {
		name            string
		config          *ServerConfig
		stubLoadKeyPair bool
		wantCertFile    string
		wantKeyFile     string
		wantMinVersion  uint16
		wantErr         bool
		wantPanic       bool
		errContains     string
	}{
		{
			name:           "loads explicit cert and key files",
			config:         &ServerConfig{CertFile: validCertFile, KeyFile: validKeyFile},
			wantMinVersion: stdtls.VersionTLS12,
		},
		{
			name:            "uses fixed default cert and key paths",
			config:          &ServerConfig{},
			stubLoadKeyPair: true,
			wantCertFile:    defaultServerCertFile,
			wantKeyFile:     defaultServerKeyFile,
			wantMinVersion:  stdtls.VersionTLS12,
		},
		{
			name:           "honors custom minimum TLS version",
			config:         &ServerConfig{CertFile: validCertFile, KeyFile: validKeyFile, MinVersion: stdtls.VersionTLS13},
			wantMinVersion: stdtls.VersionTLS13,
		},
		{
			name:        "returns error when cert file is missing",
			config:      &ServerConfig{CertFile: filepath.Join(t.TempDir(), "missing.crt"), KeyFile: validKeyFile},
			wantErr:     true,
			errContains: "load server certificate",
		},
		{
			name:        "returns error when key file is missing",
			config:      &ServerConfig{CertFile: validCertFile, KeyFile: filepath.Join(t.TempDir(), "missing.key")},
			wantErr:     true,
			errContains: "load server certificate",
		},
		{
			name:        "returns error for invalid PEM",
			config:      &ServerConfig{CertFile: invalidPEMFile, KeyFile: invalidPEMFile},
			wantErr:     true,
			errContains: "load server certificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLoadX509KeyPair := loadX509KeyPair
			t.Cleanup(func() {
				loadX509KeyPair = originalLoadX509KeyPair
			})

			var gotCertFile string
			var gotKeyFile string
			if tt.stubLoadKeyPair {
				loadX509KeyPair = func(certFile string, keyFile string) (stdtls.Certificate, error) {
					gotCertFile = certFile
					gotKeyFile = keyFile

					return stdtls.X509KeyPair(validCertPEM, validKeyPEM)
				}
			}

			// when
			got, err := NewServerTransportCredentials(tt.config)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewServerTransportCredentials(%#v) expected error", tt.config)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("NewServerTransportCredentials(%#v) error = %v, want contains %q", tt.config, err, tt.errContains)
				}

				return
			}
			if err != nil {
				t.Fatalf("NewServerTransportCredentials(%#v) unexpected error: %v", tt.config, err)
			}
			if got == nil {
				t.Fatalf("NewServerTransportCredentials(%#v) = nil, want credentials", tt.config)
			}

			serverTLSConfig, err := newServerTLSConfig(tt.config)
			if err != nil {
				t.Fatalf("newServerTLSConfig(%#v) unexpected error: %v", tt.config, err)
			}
			if serverTLSConfig.MinVersion != tt.wantMinVersion {
				t.Fatalf("newServerTLSConfig(%#v) MinVersion = %d, want %d", tt.config, serverTLSConfig.MinVersion, tt.wantMinVersion)
			}
			if tt.stubLoadKeyPair {
				if gotCertFile != tt.wantCertFile {
					t.Fatalf("newServerTLSConfig(%#v) cert file = %q, want %q", tt.config, gotCertFile, tt.wantCertFile)
				}
				if gotKeyFile != tt.wantKeyFile {
					t.Fatalf("newServerTLSConfig(%#v) key file = %q, want %q", tt.config, gotKeyFile, tt.wantKeyFile)
				}
			}
		})
	}
}

func TestNewClientTransportCredentials(t *testing.T) {
	validCAFile := testdataPath(t, "server_valid.crt")
	invalidPEMFile := testdataPath(t, "invalid.pem")
	validCAPEM := mustReadFile(t, validCAFile)

	tests := []struct {
		name         string
		config       *ClientConfig
		stubReadFile bool
		wantCAFile   string
		wantMinTLS   uint16
		wantErr      bool
		wantPanic    bool
		errContains  string
	}{
		{
			name:       "loads explicit CA file and server name",
			config:     &ClientConfig{CAFile: validCAFile, ServerName: "grpc-internal-service.test"},
			wantMinTLS: stdtls.VersionTLS12,
		},
		{
			name:         "uses fixed default CA path",
			config:       &ClientConfig{ServerName: "grpc-internal-service.test"},
			stubReadFile: true,
			wantCAFile:   defaultClientCAFile,
			wantMinTLS:   stdtls.VersionTLS12,
		},
		{
			name:       "honors custom minimum TLS version",
			config:     &ClientConfig{CAFile: validCAFile, ServerName: "grpc-internal-service.test", MinVersion: stdtls.VersionTLS13},
			wantMinTLS: stdtls.VersionTLS13,
		},
		{
			name:        "returns error when server name is empty",
			config:      &ClientConfig{CAFile: validCAFile},
			wantErr:     true,
			errContains: "server name is required",
		},
		{
			name:        "returns error when CA file is missing",
			config:      &ClientConfig{CAFile: filepath.Join(t.TempDir(), "missing-ca.crt"), ServerName: "grpc-internal-service.test"},
			wantErr:     true,
			errContains: "read CA file",
		},
		{
			name:        "returns error for invalid CA PEM",
			config:      &ClientConfig{CAFile: invalidPEMFile, ServerName: "grpc-internal-service.test"},
			wantErr:     true,
			errContains: "parse CA file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalReadFile := readFile
			t.Cleanup(func() {
				readFile = originalReadFile
			})

			var gotCAFile string
			if tt.stubReadFile {
				readFile = func(name string) ([]byte, error) {
					gotCAFile = name
					return validCAPEM, nil
				}
			}

			// when
			got, err := NewClientTransportCredentials(tt.config)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewClientTransportCredentials(%#v) expected error", tt.config)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("NewClientTransportCredentials(%#v) error = %v, want contains %q", tt.config, err, tt.errContains)
				}

				return
			}
			if err != nil {
				t.Fatalf("NewClientTransportCredentials(%#v) unexpected error: %v", tt.config, err)
			}
			if got == nil {
				t.Fatalf("NewClientTransportCredentials(%#v) = nil, want credentials", tt.config)
			}

			clientTLSConfig, err := newClientTLSConfig(tt.config)
			if err != nil {
				t.Fatalf("newClientTLSConfig(%#v) unexpected error: %v", tt.config, err)
			}
			if clientTLSConfig.MinVersion != tt.wantMinTLS {
				t.Fatalf("newClientTLSConfig(%#v) MinVersion = %d, want %d", tt.config, clientTLSConfig.MinVersion, tt.wantMinTLS)
			}
			if clientTLSConfig.ServerName != "grpc-internal-service.test" {
				t.Fatalf("newClientTLSConfig(%#v) ServerName = %q, want %q", tt.config, clientTLSConfig.ServerName, "grpc-internal-service.test")
			}
			if tt.stubReadFile && gotCAFile != tt.wantCAFile {
				t.Fatalf("newClientTLSConfig(%#v) CA file = %q, want %q", tt.config, gotCAFile, tt.wantCAFile)
			}
		})
	}
}

func TestHandshake_WithMatchingServerName(t *testing.T) {
	serverCredentials, err := NewServerTransportCredentials(&ServerConfig{
		CertFile: testdataPath(t, "server_valid.crt"),
		KeyFile:  testdataPath(t, "server_valid.key"),
	})
	if err != nil {
		t.Fatalf("NewServerTransportCredentials() unexpected error: %v", err)
	}

	clientCredentials, err := NewClientTransportCredentials(&ClientConfig{
		CAFile:     testdataPath(t, "server_valid.crt"),
		ServerName: "grpc-internal-service.test",
	})
	if err != nil {
		t.Fatalf("NewClientTransportCredentials() unexpected error: %v", err)
	}

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	serverHandshakeResult := make(chan error, 1)
	go func() {
		serverRawConn, _, serverErr := serverCredentials.ServerHandshake(serverConn)
		if serverRawConn != nil {
			defer serverRawConn.Close()
		}
		serverHandshakeResult <- serverErr
	}()

	clientRawConn, _, err := clientCredentials.ClientHandshake(context.Background(), "grpc-internal-service.test:443", clientConn)
	if err != nil {
		t.Fatalf("ClientHandshake() unexpected error: %v", err)
	}
	if clientRawConn == nil {
		t.Fatal("ClientHandshake() = nil, want conn")
	}
	defer clientRawConn.Close()

	if serverErr := <-serverHandshakeResult; serverErr != nil {
		t.Fatalf("ServerHandshake() unexpected error: %v", serverErr)
	}
}

func TestHandshake_RejectsMismatchedServerName(t *testing.T) {
	serverCredentials, err := NewServerTransportCredentials(&ServerConfig{
		CertFile: testdataPath(t, "server_wrong_san.crt"),
		KeyFile:  testdataPath(t, "server_wrong_san.key"),
	})
	if err != nil {
		t.Fatalf("NewServerTransportCredentials() unexpected error: %v", err)
	}

	clientCredentials, err := NewClientTransportCredentials(&ClientConfig{
		CAFile:     testdataPath(t, "server_wrong_san.crt"),
		ServerName: "grpc-internal-service.test",
	})
	if err != nil {
		t.Fatalf("NewClientTransportCredentials() unexpected error: %v", err)
	}

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	serverHandshakeResult := make(chan error, 1)
	go func() {
		serverRawConn, _, serverErr := serverCredentials.ServerHandshake(serverConn)
		if serverRawConn != nil {
			defer serverRawConn.Close()
		}
		serverHandshakeResult <- serverErr
	}()

	clientRawConn, _, err := clientCredentials.ClientHandshake(context.Background(), "grpc-internal-service.test:443", clientConn)
	if clientRawConn != nil {
		defer clientRawConn.Close()
	}
	if err == nil {
		t.Fatal("ClientHandshake() succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), "wrong-service.test") {
		t.Fatalf("ClientHandshake() error = %v, want mismatch details", err)
	}

	<-serverHandshakeResult
}

func testdataPath(t *testing.T, name string) string {
	t.Helper()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() unexpected error: %v", err)
	}

	return filepath.Join(workingDirectory, "testdata", name)
}

func mustReadFile(t *testing.T, name string) []byte {
	t.Helper()

	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) unexpected error: %v", name, err)
	}

	return content
}
