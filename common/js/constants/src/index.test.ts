/**
 * Value pinning for the cross-language const-lib contract
 * (specs/060-agent-v2-team-optimize/contracts/const-lib.md §1): the Go package
 * `common/gopkg/constants` mirrors these exact strings.
 */

import { describe, expect, it } from 'vitest';

import {
  ENV_DOMINION_ARTIFACT_DIR,
  ENV_DOMINION_CONFIG_DIR,
  ENV_DOMINION_ENVIRONMENT,
  ENV_DOMINION_SECRET_DIR,
  ENV_POD_NAMESPACE,
  ENV_S3_ACCESS_KEY,
  ENV_S3_SECRET_KEY,
  ENV_SERVICE_APP,
  ENV_TLS_CA_FILE,
  ENV_TLS_CERT_FILE,
  ENV_TLS_KEY_FILE,
  ENV_TLS_SERVER_NAME,
} from './index.js';

describe('const-lib environment variable names', () => {
  it('match the values fixed by the contract', () => {
    expect(ENV_SERVICE_APP).toBe('SERVICE_APP');
    expect(ENV_DOMINION_ENVIRONMENT).toBe('DOMINION_ENVIRONMENT');
    expect(ENV_POD_NAMESPACE).toBe('POD_NAMESPACE');
    expect(ENV_TLS_CERT_FILE).toBe('TLS_CERT_FILE');
    expect(ENV_TLS_KEY_FILE).toBe('TLS_KEY_FILE');
    expect(ENV_TLS_CA_FILE).toBe('TLS_CA_FILE');
    expect(ENV_TLS_SERVER_NAME).toBe('TLS_SERVER_NAME');
    expect(ENV_S3_ACCESS_KEY).toBe('S3_ACCESS_KEY');
    expect(ENV_S3_SECRET_KEY).toBe('S3_SECRET_KEY');
    expect(ENV_DOMINION_SECRET_DIR).toBe('DOMINION_SECRET_DIR');
    expect(ENV_DOMINION_CONFIG_DIR).toBe('DOMINION_CONFIG_DIR');
    expect(ENV_DOMINION_ARTIFACT_DIR).toBe('DOMINION_ARTIFACT_DIR');
  });
});
