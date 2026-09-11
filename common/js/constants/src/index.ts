// Platform-reserved environment variable names injected by the deploy
// platform, aligned one-to-one with the Go package `common/gopkg/constants`
// (specs/060-agent-v2-team-optimize/contracts/const-lib.md §1).
//
// Inclusion principle (same contract §2): this package exists for constant
// consistency and redundancy avoidance, not as a mechanical collector;
// packages that already authoritatively own their domain constants stay that
// source and are not mirrored here.

export const ENV_SERVICE_APP = 'SERVICE_APP';
export const ENV_DOMINION_ENVIRONMENT = 'DOMINION_ENVIRONMENT';
export const ENV_POD_NAMESPACE = 'POD_NAMESPACE';
export const ENV_TLS_CERT_FILE = 'TLS_CERT_FILE';
export const ENV_TLS_KEY_FILE = 'TLS_KEY_FILE';
export const ENV_TLS_CA_FILE = 'TLS_CA_FILE';
export const ENV_TLS_SERVER_NAME = 'TLS_SERVER_NAME';
export const ENV_S3_ACCESS_KEY = 'S3_ACCESS_KEY';
export const ENV_S3_SECRET_KEY = 'S3_SECRET_KEY';
export const ENV_DOMINION_SECRET_DIR = 'DOMINION_SECRET_DIR';
export const ENV_DOMINION_CONFIG_DIR = 'DOMINION_CONFIG_DIR';
export const ENV_DOMINION_ARTIFACT_DIR = 'DOMINION_ARTIFACT_DIR';
