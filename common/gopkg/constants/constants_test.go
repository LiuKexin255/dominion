package constants

import "testing"

// TestEnvironmentVariableNames pins the reserved environment variable names to
// the cross-language const-lib contract
// (specs/060-agent-v2-team-optimize/contracts/const-lib.md §1); the JavaScript
// package common/js/constants must hold the exact same strings.
func TestEnvironmentVariableNames(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "service app", got: EnvServiceApp, want: "SERVICE_APP"},
		{name: "dominion environment", got: EnvDominionEnvironment, want: "DOMINION_ENVIRONMENT"},
		{name: "pod namespace", got: EnvPodNamespace, want: "POD_NAMESPACE"},
		{name: "tls cert file", got: EnvTLSCertFile, want: "TLS_CERT_FILE"},
		{name: "tls key file", got: EnvTLSKeyFile, want: "TLS_KEY_FILE"},
		{name: "tls ca file", got: EnvTLSCAFile, want: "TLS_CA_FILE"},
		{name: "tls server name", got: EnvTLSServerName, want: "TLS_SERVER_NAME"},
		{name: "s3 access key", got: EnvS3AccessKey, want: "S3_ACCESS_KEY"},
		{name: "s3 secret key", got: EnvS3SecretKey, want: "S3_SECRET_KEY"},
		{name: "dominion secret dir", got: EnvDominionSecretDir, want: "DOMINION_SECRET_DIR"},
		{name: "dominion config dir", got: EnvDominionConfigDir, want: "DOMINION_CONFIG_DIR"},
		{name: "dominion artifact dir", got: EnvDominionArtifactDir, want: "DOMINION_ARTIFACT_DIR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}
