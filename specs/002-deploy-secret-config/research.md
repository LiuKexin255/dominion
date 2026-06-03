# Research: Deploy Secret Configuration

**Branch**: `002-deploy-secret-config` | **Date**: 2026-06-02

## R1: Secret 挂载方式 — Projected Volume vs Secret Volume

**Decision**: 使用 Kubernetes Projected Volume（`ProjectedVolumeSource`），每个 binding 对应一个 `SecretProjection` + `KeyToPath` 映射。

**Rationale**:
- 项目已有 Projected Volume 模式（TLS 证书挂载使用 `tlsVolumeName` + `ProjectedVolumeSource`），新功能应保持一致。
- Projected Volume 允许在同一卷中组合多个 Secret 来源，完美匹配"多个逻辑名绑定到不同 K8s Secret"的场景。
- 每个绑定映射为 `SecretProjection{LocalObjectReference: {Name: k8sSecretName}, Items: [{Key: k8sKey, Path: logicalName}]}`。

**Alternatives Considered**:
- 每个 secret 独立 Volume：增加 Volume/VolumeMount 数量，管理复杂。
- Init Container 拷贝：引入额外容器和启动依赖，不可取。

## R2: Secret 名称校验规则

**Decision**: Logical secret name 须匹配 `^[a-z][a-z0-9_-]{0,63}$`。不允许空字符串、路径分隔符（`/`, `\`）、点号（`..`）。

**Rationale**:
- Logical name 直接作为文件名挂载到 `/mnt/dominion/secret/{name}`，必须是合法文件名。
- 限制为小写字母+数字+下划线+连字符，与 Kubernetes resource name 风格一致。
- 最大 63 字符，与 K8s 标签值长度限制一致。

**Alternatives Considered**:
- 更宽松的文件名规则（允许大写、空格等）：增加路径冲突风险和调试复杂度。

## R3: DOMINION_SECRET_DIR 注入方式

**Decision**: 在 `builder.go` 的 `BuildDeployment` / `BuildStatefulSet` 中，当 artifact 存在 secret bindings 时，在 containerEnv 尾部追加 `DOMINION_SECRET_DIR=/mnt/dominion/secret` 环境变量。将 `DOMINION_SECRET_DIR` 加入保留变量名列表。

**Rationale**:
- 遵循现有模式：`SERVICE_APP`、`DOMINION_ENVIRONMENT`、`POD_NAMESPACE`、`S3_ACCESS_KEY` 等均为平台注入。
- 在尾部追加确保用户 env 中如果包含同名 key，会在前面被设置，但实际由平台注入值覆盖（这与现有 `LOG_LEVEL` 的处理方式不同——LOG_LEVEL 仅在用户未设置时注入；而保留变量是强制注入的）。
- 查看 `builder.go` 代码，`buildSortedUserEnv` 先处理用户 env，然后平台注入的 env 追加在后面。Kubernetes 中后定义的同名 env 会覆盖前面的。这保证了 `DOMINION_SECRET_DIR` 不可被用户覆盖。

**Alternatives Considered**:
- 使用 `envFrom` 注入：不适用于单目录路径场景。
- 仅注入环境变量，不检查用户覆盖：违反 FR-010 要求。

## R4: 绑定完整性校验位置

**Decision**: 在 `compiler.Compile` 函数中校验，在生成 `EnvironmentDesiredState` 之前。

**Rationale**:
- `compiler.Compile` 已有类似的校验模式（检查 artifact 存在性、端口匹配等）。
- 此处已有 `serviceConfig` 和 `deployService` 的完整信息，是执行双向校验的最佳位置。
- 校验失败直接返回 error，阻止 desired state 生成和提交。

**Alternatives Considered**:
- 在 `config.ParseDeployConfig` 中校验：此时没有 service config 信息，无法知道 artifact 声明了哪些 secret。
- 在 deploy service 后端校验：太晚，应该 fail-fast 在 CLI 端。

## R5: Secret Volume 命名和挂载路径

**Decision**: Volume 名称 `dominion-secrets`，挂载路径 `/mnt/dominion/secret`，只读挂载。

**Rationale**:
- 使用 `dominion-` 前缀与现有 `tls` volume 区分。
- `/mnt/dominion/secret` 是 spec 中指定的路径。
- 只读挂载（`ReadOnly: true`）遵循 spec 中"secret 文件是只读的"假设。

**Alternatives Considered**:
- 无有意义替代。

## R6: 无 Secrets 时的行为

**Decision**: 当 artifact 不声明 secrets 或声明空列表时，不创建 secret volume，不注入 `DOMINION_SECRET_DIR`，所有现有行为不变。

**Rationale**:
- FR-007 明确要求保留现有行为。
- 避免 volume mount 空目录的无意义操作。

**Alternatives Considered**:
- 始终挂载空目录：增加不必要的 K8s 资源。

## R7: Proto Message 设计

**Decision**: 在 `ArtifactSpec` 中新增 `repeated SecretBinding secret_bindings = 12`（field 11 已 reserved）。新增 `SecretBinding` message 包含 `logical_name`、`secret_name`、`key` 字段。

**Rationale**:
- 与现有 proto 设计一致：环境特定配置通过 proto message 传递到控制面。
- `ArtifactSpec` 已有 `tls_enabled`、`oss_enabled` 等布尔开关，secret bindings 作为重复消息字段自然扩展。
- 控制面 reconciliation 可以通过 `secret_bindings` 检查 K8s Secret 实际存在性（FR-012）。

**Alternatives Considered**:
- 将 binding 信息编码到 env map 中：丢失结构化信息，不利于运行时校验。

## R8: 保留环境变量更新

**Decision**: 在 `deploy.schema.json` 中将 `DOMINION_SECRET_DIR` 添加到 env key 的禁止列表（通过 schema 的 pattern 或 additionalProperties 限制）；在 `README.md` 文档中更新保留变量名列表。

**Rationale**:
- 现有保留变量名校验方式：`deploy.schema.json` 中没有显式枚举禁止的 key，而是在文档中列出。但从实际代码看，builder.go 中平台注入的 env 追加在用户 env 之后，K8s 后定义覆盖前者，因此已有隐式保护。为明确起见，应在文档中声明。

**Alternatives Considered**:
- 在 schema 中用 `not` pattern 校验：可行但增加 schema 复杂度，且当前其他保留变量也未在 schema 中强制校验。
