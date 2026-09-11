# @dominion/common-js-constants (`common/js/constants`)

仓库通用常量库（JS 侧）。Go 侧对应包为
[`common/gopkg/constants`](../../gopkg/constants)，两包常量名与值一一对齐。

## 收录原则

见 [`specs/060-agent-v2-team-optimize/contracts/const-lib.md`](../../../specs/060-agent-v2-team-optimize/contracts/const-lib.md) §2：目的是常量一致性与避免冗余，**并非机械收集**。
只收录**跨领域、尚无既有权威来源**的仓库级常量；已是公共库的包自身是其领域常量的权威来源
（如 `common/js/resolver`），不重复收录。

## 保留环境变量对齐表（首批）

| JS (`common/js/constants`) | Go (`common/gopkg/constants`) | 值 |
|---|---|---|
| `ENV_SERVICE_APP` | `EnvServiceApp` | `SERVICE_APP` |
| `ENV_DOMINION_ENVIRONMENT` | `EnvDominionEnvironment` | `DOMINION_ENVIRONMENT` |
| `ENV_POD_NAMESPACE` | `EnvPodNamespace` | `POD_NAMESPACE` |
| `ENV_TLS_CERT_FILE` | `EnvTLSCertFile` | `TLS_CERT_FILE` |
| `ENV_TLS_KEY_FILE` | `EnvTLSKeyFile` | `TLS_KEY_FILE` |
| `ENV_TLS_CA_FILE` | `EnvTLSCAFile` | `TLS_CA_FILE` |
| `ENV_TLS_SERVER_NAME` | `EnvTLSServerName` | `TLS_SERVER_NAME` |
| `ENV_S3_ACCESS_KEY` | `EnvS3AccessKey` | `S3_ACCESS_KEY` |
| `ENV_S3_SECRET_KEY` | `EnvS3SecretKey` | `S3_SECRET_KEY` |
| `ENV_DOMINION_SECRET_DIR` | `EnvDominionSecretDir` | `DOMINION_SECRET_DIR` |
| `ENV_DOMINION_CONFIG_DIR` | `EnvDominionConfigDir` | `DOMINION_CONFIG_DIR` |
| `ENV_DOMINION_ARTIFACT_DIR` | `EnvDominionArtifactDir` | `DOMINION_ARTIFACT_DIR` |

## 依赖

无运行时依赖（纯常量，无 Node API 使用）。
