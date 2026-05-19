package tls

import (
	"context"
	stdtls "crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"

	"google.golang.org/grpc/credentials"
)

const (
	// envTLSServerName is the environment variable for the TLS server name.
	envTLSServerName = "TLS_SERVER_NAME"
	// envTLSCAFile is the environment variable for the TLS CA bundle path.
	envTLSCAFile = "TLS_CA_FILE"
	// envTLSCertFile is the environment variable for the TLS certificate path.
	envTLSCertFile = "TLS_CERT_FILE"
	// envTLSKeyFile is the environment variable for the TLS private key path.
	envTLSKeyFile = "TLS_KEY_FILE"

	// defaultServerCertFile is the fixed server certificate path used by default.
	defaultServerCertFile = "/etc/tls/tls.crt"
	// defaultServerKeyFile is the fixed server private-key path used by default.
	defaultServerKeyFile = "/etc/tls/tls.key"
	// defaultClientCAFile is the fixed client CA bundle path used by default.
	defaultClientCAFile = "/etc/tls/ca.crt"
	// defaultMinVersion is the minimum TLS version used when config leaves it unset.
	defaultMinVersion = stdtls.VersionTLS12
)

var (
	// readFile reads certificate material from disk.
	readFile = os.ReadFile
	// loadX509KeyPair loads the server certificate and private key from disk.
	loadX509KeyPair = stdtls.LoadX509KeyPair
	// lookupEnv reads process environment variables and allows tests to stub runtime TLS inputs.
	lookupEnv = os.LookupEnv

	logFieldCAFile     = "ca_file"
	logFieldServerName = "server_name"
	logFieldCertFile   = "cert_file"
	logFieldKeyFile    = "key_file"
)

// ClientConfig defines the trusted inputs for client-side gRPC TLS credentials.
type ClientConfig struct {
	CAFile     string
	ServerName string
	MinVersion uint16
}

// ServerConfig defines the trusted inputs for server-side gRPC TLS credentials.
type ServerConfig struct {
	CertFile   string
	KeyFile    string
	MinVersion uint16
}

// NewClientTransportCredentials builds fail-closed gRPC client TLS credentials.
func NewClientTransportCredentials(config *ClientConfig) (credentials.TransportCredentials, error) {
	tlsConfig, err := newClientTLSConfig(config)
	if err != nil {
		logs.Error(context.Background(), "client TLS config build failed", event.Err(err))
		return nil, err
	}

	logs.Info(context.Background(), "client TLS credentials created", event.String(logFieldCAFile, config.CAFile), event.String(logFieldServerName, config.ServerName))
	return credentials.NewTLS(tlsConfig), nil
}

// NewServerTransportCredentials builds fail-closed gRPC server TLS credentials.
func NewServerTransportCredentials(config *ServerConfig) (credentials.TransportCredentials, error) {
	tlsConfig, err := newServerTLSConfig(config)
	if err != nil {
		logs.Error(context.Background(), "server TLS config build failed", event.Err(err))
		return nil, err
	}

	logs.Info(context.Background(), "server TLS credentials created", event.String(logFieldCertFile, config.CertFile), event.String(logFieldKeyFile, config.KeyFile))
	return credentials.NewTLS(tlsConfig), nil
}

// ClientTransportCredentials builds client-side gRPC TLS credentials from environment variables.
// It returns nil when TLS is not configured or the environment is incomplete/invalid.
func ClientTransportCredentials() credentials.TransportCredentials {
	serverName, hasServerName := lookupTrimmedEnv(envTLSServerName)
	caFile, hasCAFile := lookupTrimmedEnv(envTLSCAFile)
	if !hasServerName && !hasCAFile {
		logs.Info(context.Background(), "TLS not configured for client, using plain connection")
		return nil
	}
	if serverName == "" {
		logs.Info(context.Background(), "TLS not configured for client, using plain connection")
		return nil
	}

	transportCredentials, err := NewClientTransportCredentials(&ClientConfig{
		CAFile:     caFile,
		ServerName: serverName,
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create client TLS credentials: %v", err))
	}

	logs.Info(context.Background(), "client TLS configured from environment", event.String(logFieldServerName, serverName))
	return transportCredentials
}

// ServerTransportCredentials builds server-side gRPC TLS credentials from environment variables.
// It returns nil when TLS is not configured or the environment is invalid.
func ServerTransportCredentials() credentials.TransportCredentials {
	certFile, hasCertFile := lookupTrimmedEnv(envTLSCertFile)
	keyFile, hasKeyFile := lookupTrimmedEnv(envTLSKeyFile)
	_, hasCAFile := lookupTrimmedEnv(envTLSCAFile)
	_, hasServerName := lookupTrimmedEnv(envTLSServerName)
	if !hasCertFile && !hasKeyFile && !hasCAFile && !hasServerName {
		logs.Info(context.Background(), "TLS not configured for server, using plain connection")
		return nil
	}

	transportCredentials, err := NewServerTransportCredentials(&ServerConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create server TLS credentials: %v", err))
	}

	logs.Info(context.Background(), "server TLS configured from environment", event.String(logFieldCertFile, certFile))
	return transportCredentials
}

func newClientTLSConfig(config *ClientConfig) (*stdtls.Config, error) {
	if config == nil {
		return nil, fmt.Errorf("client config is required")
	}

	serverName := strings.TrimSpace(config.ServerName)
	if serverName == "" {
		return nil, fmt.Errorf("server name is required")
	}

	caFile := strings.TrimSpace(config.CAFile)
	if caFile == "" {
		caFile = defaultClientCAFile
	}

	caPEM, err := readFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file %q: %w", caFile, err)
	}

	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse CA file %q: no certificates found", caFile)
	}

	return &stdtls.Config{
		MinVersion: resolveMinVersion(config.MinVersion),
		RootCAs:    rootCAs,
		ServerName: serverName,
	}, nil
}

func newServerTLSConfig(config *ServerConfig) (*stdtls.Config, error) {
	if config == nil {
		return nil, fmt.Errorf("server config is required")
	}

	certFile := strings.TrimSpace(config.CertFile)
	if certFile == "" {
		certFile = defaultServerCertFile
	}

	keyFile := strings.TrimSpace(config.KeyFile)
	if keyFile == "" {
		keyFile = defaultServerKeyFile
	}

	certificate, err := loadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server certificate %q and key %q: %w", certFile, keyFile, err)
	}

	return &stdtls.Config{
		Certificates: []stdtls.Certificate{certificate},
		MinVersion:   resolveMinVersion(config.MinVersion),
	}, nil
}

func resolveMinVersion(minVersion uint16) uint16 {
	if minVersion == 0 {
		return defaultMinVersion
	}

	return minVersion
}

func lookupTrimmedEnv(name string) (string, bool) {
	value, ok := lookupEnv(name)
	if !ok {
		return "", false
	}

	return strings.TrimSpace(value), true
}
