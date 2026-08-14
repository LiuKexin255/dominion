# Data Model: Deploy Config Support

**Branch**: `045-deploy-config` | **Date**: 2026-08-13 | **Spec**: [spec.md](spec.md)

## Entities

### 1. Config Block（service.yaml 层，顶层声明）

service.yaml 顶层声明的命名配置单元，是所有 artifact 共享的配置定义池中的一个条目。

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| name | `string` | 非空，匹配 `^[a-z][a-z0-9_-]{0,63}$`，在配置块列表内唯一 | 配置块名，SDK 第一寻址参数 |
| data | `[]ConfigDataEntry` | 至少 1 项 | 该配置块的数据条目列表 |

**Storage**: `service.yaml` → 顶层 `configs[]`

**Validation Rules**:
- VR-CB-1: name 空字符串 → 拒绝
- VR-CB-2: name 不匹配 pattern → 拒绝（与 secret 逻辑名同规则，见 `specs/002-deploy-secret-config/data-model.md` VR-SD-2）
- VR-CB-3: 配置块列表内 name 重复 → 拒绝（FR-004）
- VR-CB-4: data 为空或缺失 → 拒绝

**State Transitions**: 无。声明是静态的。

### 2. Config Data Entry（service.yaml 层，配置块内）

配置块内的一个键值单元，是 SDK 读取与深度合并的最小单位。

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| name | `string` | 非空，匹配 `^[a-z][a-z0-9_-]{0,63}$`，在所属配置块内唯一 | 条目名，SDK 第二寻址参数，运行时文件名 |
| value | `string` | 非空字符串，内容须为按 type 解释的合法文本 | 原始数据（非 base64），JSON 或 YAML 文本 |
| type | `enum` | `json` 或 `yaml` | 格式类型，用于部署期格式校验（FR-003） |

**Storage**: `service.yaml` → `configs[].data[]`

**Validation Rules**:
- VR-DE-1: name 空字符串 → 拒绝
- VR-DE-2: name 不匹配 pattern → 拒绝
- VR-DE-3: 同一配置块内 name 重复 → 拒绝（FR-004）
- VR-DE-4: value 缺失或非字符串 → 拒绝（FR-002）
- VR-DE-5: type 为 `json` 但 value 不可被 JSON 解析器解析 → 拒绝（FR-003）
- VR-DE-6: type 为 `yaml` 但 value 不可被 YAML 解析器解析 → 拒绝（FR-003）
- VR-DE-7: value 为空字符串 → 拒绝（schema `minLength 1`；空串对 `type: json` 亦非合法 JSON，无法通过 FR-003；spec Edge Case 已对齐，无"空文档"语义）

**State Transitions**: 无。

### 3. Config Selection（deploy.yaml 层）

deploy.yaml 中 artifact 对配置块名的引用列表，决定哪些配置块的数据在运行时提供给该产物。单向选择（deploy 从池中选子集）。

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| configs | `[]string` | 每项非空，匹配 `^[a-z][a-z0-9_-]{0,63}$`，列表内唯一（schema `uniqueItems`），且每个名须存在于 service 配置块池 | 选中的配置块名列表 |

**Storage**: `deploy.yaml` → `services[].artifact.configs[]`

**Validation Rules**:
- VR-CS-1: 列表中每个名须存在于关联 service.yaml 的配置块池（`serviceConfig.Configs`）→ 否则拒绝（FR-007），拒绝发生在 `compiler.Compile` 提交期望状态前
- VR-CS-2: 列表内重复选择 → schema `uniqueItems` 拒绝（与 FR-004 配置块名唯一性对称；避免编译出重复块名的 `ConfigBlock`，与控制面 VR-CB-6 冲突）
- VR-CS-3: deploy.yaml 对配置 MUST NOT 覆盖或修改配置块中的数据（FR-008）—— schema 层 config 仅为字符串列表，无 value/type 字段

**State Transitions**: 无。选择是静态的。

### 4. Config Block（proto / domain 层，期望状态）

通过 protobuf 传递到 deploy 控制面的配置数据，以**层级结构**承载——块包含条目，对齐 service.yaml `configs[].data[]` 层级与控制面 per-block ConfigMap 物化（每个块一个 ConfigMap object）。层级化使期望状态、service.yaml 源、K8s 物化目标三处结构一致（见 [proto.md §1](contracts/proto.md)）。

#### 4a. Config Block

| Field | Type | Proto Field # | Description |
|-------|------|---------------|-------------|
| block | `string` | 1 | 配置块名（SDK 第一寻址参数 / ConfigMap 名后缀来源——object 名中经 RFC 1123 清洗，SDK 寻址与文件路径用原始名，见 [runtime-contract.md §2](contracts/runtime-contract.md)） |
| entries | `[]ConfigEntry` | 2 | 块内数据条目列表，每条目对应 ConfigMap 的一个 data key |

**Parent**: `ArtifactSpec.config_blocks` (proto field **12**；见 [proto.md §1](contracts/proto.md))

#### 4b. Config Data Entry

| Field | Type | Proto Field # | Description |
|-------|------|---------------|-------------|
| key | `string` | 1 | 数据条目名（块内唯一），SDK 第二寻址参数、运行时文件名、ConfigMap data key |
| type | `string` | 2 | 格式类型（`json`/`yaml`），保留以备审计；运行时不使用（一律 YAML 解析，见 research.md R4） |
| value | `string` | 3 | 原始数据文本（ConfigMap data 的内容） |

**Parent**: `ConfigBlock.entries` (proto field **2**)

> **block 字段归属**：`ConfigEntry` 不携带 `block` 字段——块归属由父节点 `ConfigBlock` 表达，避免冗余与块归属不一致的可能。

**Validation Rules**（domain `ArtifactSpec.Validate()` → `ConfigBlock.Validate()` → `ConfigEntry.Validate()`）:
- VR-CE-1: 每个 ConfigEntry 的 key/type/value 均非空
- VR-CB-5: 每个 ConfigBlock 的 block 非空、entries 至少 1 项
- VR-CE-2: **块内**条目名（key）不重复。块内 key 重复即同一 ConfigMap 同一 data key 冲突（map 写覆盖）；不同块的条目属于不同 ConfigMap（`{workload}-config-{sanitize(block)}`，命名见 [runtime-contract.md §2](contracts/runtime-contract.md)），天然隔离，无需跨块检测。与 service.yaml 层 VR-DE-3（块内条目名唯一）语义对称
- VR-CB-6: **ConfigBlocks 列表内块名不重复**。两个同名块会映射到同一 ConfigMap `{workload}-config-{sanitize(block)}`，结构上必然冲突。domain 校验作为防御纵深保留（防非 CLI 客户端绕过 schema `uniqueItems`，见 [yaml-schema.md §2](contracts/yaml-schema.md)），与 service.yaml 层 VR-CB-3 对称。注意本规则是**清洗前**（原始块名）的唯一性；不同原始块名清洗后碰撞（如 `service_config` 与 `service-config`）由 builder 层 `BuildConfigMaps` fail-fast 兜底（见 [runtime-contract.md §2](contracts/runtime-contract.md)）

### 5. Config Runtime Contract（K8s 层）

运行时实际注入到容器的配置挂载。

| Resource | Description |
|----------|-------------|
| ConfigMap `{workload}-config-{sanitize(block)}` | 控制面创建，**每个配置块一个 ConfigMap object**；object 名中 block 成分经 RFC 1123 清洗（`sanitizeNamePart`，如 `service_config` → `service-config`，见 [runtime-contract.md §2](contracts/runtime-contract.md)）；data key 为条目名（块内唯一，原样不清洗），value 为原始数据文本 |
| Volume `dominion-config` | ProjectedVolume（单一卷），每个块一个 `ConfigMapProjection` source；每条目 `KeyToPath{Key: 条目名, Path: "{block}/{key}"}` 还原目录层级（**真实块名**，路径允许 `_`，与 object 名清洗无关） |
| VolumeMount `/mnt/dominion/config` | 只读挂载 |
| EnvVar `DOMINION_CONFIG_DIR` | 值 `/mnt/dominion/config`，平台强制注入（保留变量） |

**Generated By**: `projects/infra/deploy/runtime/k8s/builder.go` → `BuildDeployment` / `BuildStatefulSet`（投影）与 `BuildConfigMaps`（per-block ConfigMap，直接迭代 `workload.ConfigBlocks`，按 ConfigBlocks 列表顺序——compiler 已保留 service.yaml 声明顺序，无需 builder 层重新分组）；ConfigMap 由 `executor.go` 的 per-block apply 路径创建。

**Conditions**:
- 仅当 artifact 有至少一个 config block 时创建 ConfigMap(s) / volume / mount / env
- 每个 block 一个 ConfigMap（`{workload}-config-{sanitize(block)}`，builder 侧 fail-fast：63 字符上限、清洗后为空、清洗后碰撞，见 [runtime-contract.md §2](contracts/runtime-contract.md)）
- 无 config blocks 时不创建任何资源（与 secret 行为一致，见 002 R6）

## Entity Relationships

```
ServiceConfig (service.yaml)
  └── Configs: []ConfigBlock                  ← 顶层配置块池（所有 artifact 共享）
        └── Data: []ConfigDataEntry           ← {name, value, type}
                  │
                  │ (selected by name)
                  ▼
DeployConfig (deploy.yaml)
  └── Services[].Artifact
        └── Config: []string                  ← 选择的配置块名列表
                  │
                  │ (validated + compiled in compiler.Compile)
                  ▼
ArtifactSpec (proto / domain)
  └── ConfigBlocks: []ConfigBlock              ← 期望状态层级化（块包含条目）
        └── Entries: []ConfigEntry             ← {key, type, value}（block 归属父节点）
                   │
                   │ (converted to workload in converter.go)
                   ▼
DeploymentWorkload / StatefulWorkload
  └── ConfigBlocks field                       ← 直接持有层级结构，无需 builder 层分组
                   │
                   │ (built into K8s resources)
                   ▼
K8s Deployment/StatefulSet
  ├── ConfigMap: {workload}-config-{sanitize(block)} ← executor 创建，每块一个（data: 条目名 → value）
  ├── Volume: dominion-config (projected)        ← 每块一个 ConfigMap source，KeyToPath 还原 {block}/{key}
  ├── VolumeMount: /mnt/dominion/config (ro)
  └── EnvVar: DOMINION_CONFIG_DIR=/mnt/dominion/config
                  │
                  │ (read at runtime by SDK)
                  ▼
SDK (Go common/gopkg/config | JS common/js/config)
  └── Read(block, key, defaults)              ← 读取 {DOMINION_CONFIG_DIR}/{block}/{key}
        └── 深度合并配置 over defaults → 返回类型化结果
```

## Go Struct Changes

### `tools/release/deploy/pkg/config/config.go`

```go
// ServiceConfig — 顶层新增字段
type ServiceConfig struct {
    // ... 现有字段 ...
    Configs []*ServiceConfigBlock `yaml:"configs,omitempty"` // NEW: 顶层配置块池
}

// ServiceConfigBlock — 新增类型
type ServiceConfigBlock struct {
    Name string                `yaml:"name"`
    Data []*ServiceConfigEntry `yaml:"data"`
}

// ServiceConfigEntry — 新增类型
type ServiceConfigEntry struct {
    Name  string `yaml:"name"`
    Value string `yaml:"value"`
    Type  string `yaml:"type"` // "json" | "yaml"
}

// DeployArtifact — 新增字段
type DeployArtifact struct {
    // ... 现有字段 ...
    Config []string `yaml:"configs,omitempty"` // NEW: 选择的配置块名列表
}
```

> **字段命名说明**：service.yaml 顶层字段为 `configs`（复数，与 `artifacts`/`ports` 等列表字段命名一致；Go 字段名 `Configs`）。deploy.yaml artifact 字段为 `configs`（复数，与 `services`/`hostnames`/`matches` 等列表字段命名一致；Go 字段名 `Config`）。两者分别在不同文件的不同 schema 节点，无歧义。

### `projects/infra/deploy/deploy.proto`

```protobuf
message ArtifactSpec {
  // ... 现有字段 1-11 ...
  repeated SecretBinding secret_bindings = 11;
  repeated ConfigBlock config_blocks = 12;   // NEW（块包含条目，对齐 service.yaml configs[]）
}

// ConfigBlock carries a configuration block with its entries.
message ConfigBlock {
  string block = 1;            // 配置块名
  repeated ConfigEntry entries = 2;  // 块内条目
}

// ConfigEntry carries inline configuration data for a deployed artifact.
message ConfigEntry {
  string key = 1;    // 数据条目名（块内唯一）
  string type = 2;   // 格式类型 "json"|"yaml"（审计用，运行时不使用）
  string value = 3;  // 原始数据文本
}
```

> 字段号 12 复用：本特性未发布、无生产数据，直接将 12 号位类型由扁平 `repeated ConfigEntry` 改为层级 `repeated ConfigBlock`，无需 wire 兼容（详见 [proto.md §1](contracts/proto.md)）。

### `projects/infra/deploy/domain/spec.go`

```go
// ConfigBlock — 新增类型（期望状态层级化）
type ConfigBlock struct {
    Block   string
    Entries []*ConfigEntry
}

func (b *ConfigBlock) Validate() error { /* block 非空、entries 非空、每 entry 调 Validate */ }

// ConfigEntry — 新增类型（block 归属父节点 ConfigBlock）
type ConfigEntry struct {
    Key   string
    Type  string
    Value string
}

func (e *ConfigEntry) Validate() error { /* key/type/value 非空 */ }

// ArtifactSpec — 新增字段
type ArtifactSpec struct {
    // ... 现有字段 ...
    SecretBindings []*SecretBinding
    ConfigBlocks   []*ConfigBlock // NEW：期望状态层级的配置块（块包含条目）
}
```

> `ArtifactSpec.Validate()` config 校验：每个 block 调 `Validate()`、块名不重复（VR-CB-6）、块内 entry key 不重复（VR-CE-2：块内 key 唯一即同 ConfigMap 同 data key 不冲突）。详见 [proto.md §2](contracts/proto.md)。

### `projects/infra/deploy/runtime/k8s/model.go`

```go
// DeploymentWorkload / StatefulWorkload — 新增字段
type DeploymentWorkload struct {
    // ... 现有字段 ...
    SecretBindings []*domain.SecretBinding
    ConfigBlocks   []*domain.ConfigBlock // NEW：workload 持有层级化配置块
}
```

`configMapWorkload` 接口访问器为 `configBlocks() []*domain.ConfigBlock`——消费侧直接持有层级结构，无 builder 层 `configEntriesByBlock` 分组逻辑（期望状态已层级化建模，详见 [runtime-contract.md §2](contracts/runtime-contract.md)）。

### `tools/release/deploy/v2/compiler/compiler.go`（产出形态）

CLI 编译直接产出 `[]*ConfigBlock`（按 service.yaml 块结构映射，选中块整体进入，**条目顺序保留 service.yaml `data[]` 声明顺序**），不再展平为扁平 `[]*ConfigEntry`：

```go
for _, selectedName := range deployService.Artifact.Config {
    block := configBlockSet[selectedName]  // FR-007 存在性校验
    cb := &deploy.ConfigBlock{Block: block.Name}
    for _, entry := range block.Data {
        cb.Entries = append(cb.Entries, &deploy.ConfigEntry{
            Key: entry.Name, Type: entry.Type, Value: entry.Value,
        })
    }
    compiledArtifact.ConfigBlocks = append(compiledArtifact.ConfigBlocks, cb)
}
```

> **CLI 侧 schema/解析不变**：`ServiceConfig.Configs []*ServiceConfigBlock`（service.yaml 层）本就是层级结构，`DeployArtifact.Config []string`（deploy.yaml 选择）不变——仅 compiler 产出形态从展平变为层级。

## Validation Matrix

| Scenario | Schema Layer | Config Layer (Parse) | Compiler Layer | Domain/Builder Layer |
|----------|-------------|----------------------|----------------|----------------------|
| 空配置块/条目名 | `minLength` + `pattern` | Go validation | — | — |
| 重复配置块名 | — | Go validation (ParseServiceConfig) | — | — |
| 同块内重复条目名 | — | Go validation (ParseServiceConfig) | — | — |
| value 非字符串 | schema `type: string` | Go validation | — | — |
| type 非 json/yaml | schema `enum` | Go validation | — | — |
| value 为空字符串 | schema `minLength 1` 拒绝 | — | — | — |
| value 格式与 type 不符 | — | Go validation (json/yaml 解析校验, FR-003) | — | — |
| deploy 选择未定义配置块名 | — | — | Go validation in Compile() (FR-007) | — |
| deploy configs 列表内重复选择 | schema `uniqueItems` 拒绝 | — | — | — |
| 期望状态层 ConfigBlock 块名重复 | — | — | — | domain `ArtifactSpec.Validate()`（VR-CB-6，防御纵深，防非 CLI 客户端绕过 schema `uniqueItems`；同名块映射同一 ConfigMap `{workload}-config-{sanitize(block)}` 冲突，对齐 service.yaml 层 VR-CB-3） |
| 不同原始块名清洗后碰撞（如 `service_config` 与 `service-config`） | — | — | — | builder `BuildConfigMaps` fail-fast（[runtime-contract.md §2](contracts/runtime-contract.md)；schema/domain 唯一性均为清洗前唯一性，不排除此情况） |
| 期望状态层块内 ConfigEntry key 重复 | — | — | — | domain `ArtifactSpec.Validate()`（VR-CE-2，防御纵深；层级化后为块内 key 唯一检测，即同 ConfigMap 同 data key 冲突，对齐 service.yaml 层 VR-DE-3） |
| DOMINION_CONFIG_DIR 在用户 env | — | — | — | K8s env override（保留变量） |
| 无 config 声明/选择 | Pass-through | Pass-through | Pass-through | 跳过 ConfigMap/volume/mount |

## Reserved Environment Variables (updated)

新增 `DOMINION_CONFIG_DIR` 到保留变量列表（`executor.go` `ReservedEnvironmentVariableNames` + `builder.go` 常量 + README 文档）：

```
SERVICE_APP, DOMINION_ENVIRONMENT, POD_NAMESPACE, TLS_CERT_FILE,
TLS_KEY_FILE, TLS_CA_FILE, TLS_SERVER_NAME, S3_ACCESS_KEY,
S3_SECRET_KEY, DOMINION_SECRET_DIR, DOMINION_CONFIG_DIR   ← NEW
```

## Deep Merge Semantics (SDK 实现规范)

| 输入情况 | 结果 |
|----------|------|
| 配置中存在某对象键、默认值对应键也是对象 | 递归合并 |
| 配置中存在某键、默认值对应键是标量/数组 | 配置值整体替换（数组不按索引合并） |
| 配置中不存在某键 | 保留默认值 |
| 配置显式 `null`（JSON）/ `null`（YAML） | 覆盖默认值为 null/零值 |
| 默认值对象、配置值为非对象（标量/数组） | 类型不匹配——替换后反序列化为目标类型时按解析器规则处理（通常为类型错误） |
| 调用方 defaults | **不被修改**（Go 内部深拷贝；JS `structuredClone`） |
