// Package constants collects repository-wide constants that are shared across
// domains and have no existing authoritative source.
//
// Purpose and inclusion principle
// (specs/060-agent-v2-team-optimize/contracts/const-lib.md §2): the package
// exists for constant consistency and redundancy avoidance, not as a
// mechanical collector. Packages that already authoritatively own their
// domain constants (for example common/gopkg/config) stay that source; their
// constants are deliberately not mirrored here.
//
// The JavaScript counterpart is common/js/constants; both packages hold the
// same names and values.
package constants

// Platform-reserved environment variable names; the set is fixed by the
// const-lib contract
// (specs/060-agent-v2-team-optimize/contracts/const-lib.md §1). The deploy
// platform appends its value after user env entries, so a user declaration
// with the same name is overridden by the platform value
// (specs/060-agent-v2-team-optimize/contracts/deploy-env.md §1 注入位置与顺序).
const (
	// EnvServiceApp is the environment variable holding the app the service
	// belongs to.
	EnvServiceApp = "SERVICE_APP"
	// EnvDominionEnvironment is the environment variable holding the
	// deployment environment name.
	EnvDominionEnvironment = "DOMINION_ENVIRONMENT"
	// EnvPodNamespace is the environment variable holding the Pod namespace.
	EnvPodNamespace = "POD_NAMESPACE"
	// EnvTLSCertFile is the environment variable holding the TLS certificate
	// file path.
	EnvTLSCertFile = "TLS_CERT_FILE"
	// EnvTLSKeyFile is the environment variable holding the TLS private key
	// file path.
	EnvTLSKeyFile = "TLS_KEY_FILE"
	// EnvTLSCAFile is the environment variable holding the TLS CA file path.
	EnvTLSCAFile = "TLS_CA_FILE"
	// EnvTLSServerName is the environment variable holding the TLS server
	// name.
	EnvTLSServerName = "TLS_SERVER_NAME"
	// EnvS3AccessKey is the environment variable holding the S3 access key.
	EnvS3AccessKey = "S3_ACCESS_KEY"
	// EnvS3SecretKey is the environment variable holding the S3 secret key.
	EnvS3SecretKey = "S3_SECRET_KEY"
	// EnvDominionSecretDir is the environment variable holding the directory
	// the platform projects secrets into.
	EnvDominionSecretDir = "DOMINION_SECRET_DIR"
	// EnvDominionConfigDir is the environment variable holding the directory
	// the platform projects configs into.
	EnvDominionConfigDir = "DOMINION_CONFIG_DIR"
	// EnvDominionArtifactDir is the environment variable holding the directory
	// the platform places the service artifact in (/dominion/{app}/{service}).
	EnvDominionArtifactDir = "DOMINION_ARTIFACT_DIR"
)
