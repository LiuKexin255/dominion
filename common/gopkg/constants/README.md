# common/gopkg/constants

仓库通用常量库（Go 侧）。JS 侧对应包为
[`common/js/constants`](../../js/constants)（`@dominion/common-js-constants`），两包常量名与值
一一对齐。

## 收录原则

见 [`specs/060-agent-v2-team-optimize/contracts/const-lib.md`](../../../specs/060-agent-v2-team-optimize/contracts/const-lib.md) §2：目的是常量一致性与避免冗余，**并非机械收集**。
只收录**跨领域、尚无既有权威来源**的仓库级常量；已是公共库的包自身是其领域常量的权威来源
（如 `common/gopkg/config`），不重复收录。

## 保留环境变量对齐表（首批）

| Go (`common/gopkg/constants`) | JS (`common/js/constants`) | 值 |
|---|---|---|
| `EnvServiceApp` | `ENV_SERVICE_APP` | `SERVICE_APP` |
| `EnvDominionEnvironment` | `ENV_DOMINION_ENVIRONMENT` | `DOMINION_ENVIRONMENT` |
| `EnvPodNamespace` | `ENV_POD_NAMESPACE` | `POD_NAMESPACE` |
| `EnvTLSCertFile` | `ENV_TLS_CERT_FILE` | `TLS_CERT_FILE` |
| `EnvTLSKeyFile` | `ENV_TLS_KEY_FILE` | `TLS_KEY_FILE` |
| `EnvTLSCAFile` | `ENV_TLS_CA_FILE` | `TLS_CA_FILE` |
| `EnvTLSServerName` | `ENV_TLS_SERVER_NAME` | `TLS_SERVER_NAME` |
| `EnvS3AccessKey` | `ENV_S3_ACCESS_KEY` | `S3_ACCESS_KEY` |
| `EnvS3SecretKey` | `ENV_S3_SECRET_KEY` | `S3_SECRET_KEY` |
| `EnvDominionSecretDir` | `ENV_DOMINION_SECRET_DIR` | `DOMINION_SECRET_DIR` |
| `EnvDominionConfigDir` | `ENV_DOMINION_CONFIG_DIR` | `DOMINION_CONFIG_DIR` |
| `EnvDominionArtifactDir` | `ENV_DOMINION_ARTIFACT_DIR` | `DOMINION_ARTIFACT_DIR` |
