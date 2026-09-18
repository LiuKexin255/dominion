# Contract: 仓库通用常量库（const lib）

> `common/gopkg/constants`（Go）与 `common/js/constants`（`@dominion/common-js-constants`）的行为契约。
> 决策依据：[research.md](../research.md) R4；收录原则裁定见 spec Clarifications 2026-09-11。

## 1. 内容（首批：deploy 平台保留环境变量名）

两语言包同源收录（常量名与值一一对齐）：

| 常量 | 值 | 说明 |
|---|---|---|
| `EnvServiceApp` / `ENV_SERVICE_APP` | `SERVICE_APP` | 服务所属 app 注入 |
| `EnvDominionEnvironment` / `ENV_DOMINION_ENVIRONMENT` | `DOMINION_ENVIRONMENT` | 部署环境名 |
| `EnvPodNamespace` / `ENV_POD_NAMESPACE` | `POD_NAMESPACE` | Pod 命名空间 |
| `EnvTLSCertFile` / `ENV_TLS_CERT_FILE` | `TLS_CERT_FILE` | TLS 证书文件 |
| `EnvTLSKeyFile` / `ENV_TLS_KEY_FILE` | `TLS_KEY_FILE` | TLS 私钥文件 |
| `EnvTLSCAFile` / `ENV_TLS_CA_FILE` | `TLS_CA_FILE` | TLS CA 文件 |
| `EnvTLSServerName` / `ENV_TLS_SERVER_NAME` | `TLS_SERVER_NAME` | TLS 服务名 |
| `EnvS3AccessKey` / `ENV_S3_ACCESS_KEY` | `S3_ACCESS_KEY` | S3 Access Key |
| `EnvS3SecretKey` / `ENV_S3_SECRET_KEY` | `S3_SECRET_KEY` | S3 Secret Key |
| `EnvDominionSecretDir` / `ENV_DOMINION_SECRET_DIR` | `DOMINION_SECRET_DIR` | secret 投影目录 |
| `EnvDominionConfigDir` / `ENV_DOMINION_CONFIG_DIR` | `DOMINION_CONFIG_DIR` | config 投影目录 |
| `EnvDominionArtifactDir` / `ENV_DOMINION_ARTIFACT_DIR` | `DOMINION_ARTIFACT_DIR` | **产物放置目录（本 feature 新增，deploy-env.md §1）** |

（命名形态：Go 侧 `EnvXxx` 导出常量；JS 侧 `ENV_XXX` 大写下划线导出——两包 README 各自给出对齐表；具体命名风格以仓库各语言惯例为准，tasks 阶段定稿。）

## 2. 收录原则（治理边界）

- **目的 = 常量一致性与避免冗余，非机械收集**。
- 收录条件：**跨领域、尚无既有权威来源**的仓库级常量。
- 不收录：已是公共库的包自身领域常量（common 既有包保持其权威来源地位，不改为引用本库）。

## 3. 采用范围（本 feature 切换面）

| 使用方 | 切换点 |
|---|---|
| `projects/infra/deploy/runtime/k8s/builder.go` | 保留变量定义常量块（L23-70）替换为常量库引用（含新增 `DOMINION_ARTIFACT_DIR` 注入实现） |
| `tools/release/deploy` 文档/校验 | 保留名清单引用（README §环境变量保留清单） |
| `projects/game/agent_v2` | `presets.ts` 的 `DOMINION_ENVIRONMENT`、`dsh.ts` 的 `DOMINION_SECRET_DIR` 与新增 `DOMINION_ARTIFACT_DIR` 消费点 |

common 既有公共包（gopkg/js 各库）内部的同类定义**不在本 feature 范围**（spec FR-005 / Edge Cases）。

## 4. 包形态

- Go：`common/gopkg/constants`（无依赖纯常量包；gazelle 生成 BUILD）；依赖方向 `projects/infra/deploy → common/gopkg/constants`（与既有 common 依赖同向，无环）。
- JS：`common/js/constants`（package name `@dominion/common-js-constants`；版本统一经根 `pnpm-workspace.yaml` catalog；对齐 `common/js/*` 既有包结构：src/index.ts + package.json + BUILD.bazel）。
