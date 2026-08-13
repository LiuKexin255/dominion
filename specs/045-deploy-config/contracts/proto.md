# Contract: Proto & Domain Changes

**Feature**: 045-deploy-config | **Spec**: [spec.md](../spec.md)

本契约定义 config 特性对 `projects/infra/deploy/deploy.proto` 及其 domain/storage 映射的扩展。完整数据流见 [data-model.md](../data-model.md)。

---

## 1. deploy.proto 变更

### 新增 message：`ConfigEntry`

```protobuf
// ConfigEntry carries inline configuration data for a deployed artifact.
// 控制面据此创建 ConfigMap 并投影为容器内文件。
message ConfigEntry {
  // block 是 service.yaml 中声明的配置块名（SDK 第一寻址参数）。
  string block = 1;

  // key 是配置块内数据条目名（SDK 第二寻址参数，运行时文件名）。
  string key = 2;

  // type 是格式类型 "json"|"yaml"，保留以备审计；
  // 运行时 SDK 一律用 YAML 解析，此字段不被 SDK 使用（见 research.md R4）。
  string type = 3;

  // value 是原始数据文本（service.yaml configs[].data[].value 原样），
  // 成为 ConfigMap data 的内容。
  string value = 4;
}
```

### ArtifactSpec 新增字段

```protobuf
message ArtifactSpec {
  string name = 1;
  string app = 2;
  string image = 3;
  repeated ArtifactPortSpec ports = 4;
  int32 replicas = 5;
  bool tls_enabled = 6;
  ArtifactHTTPSpec http = 7;
  WorkloadKind workload_kind = 8;
  map<string, string> env = 9;
  bool oss_enabled = 10;
  repeated SecretBinding secret_bindings = 11;
  repeated ConfigEntry config_entries = 12;   // NEW（下一可用字段号）
}
```

> **字段号 12**：经核查 `ArtifactSpec` 现有字段 1-11，12 为下一可用号（无 reserved 冲突）。

---

## 2. domain 层变更（`projects/infra/deploy/domain/spec.go`）

### 新增类型 `ConfigEntry`

```go
// ConfigEntry 携带部署产物的内联配置数据。
type ConfigEntry struct {
    Block string
    Key   string
    Type  string
    Value string
}

// Validate 校验所有字段非空。
func (e *ConfigEntry) Validate() error
```

### `ArtifactSpec` 新增字段 + 校验

```go
type ArtifactSpec struct {
    // ... 现有字段 ...
    SecretBindings []*SecretBinding
    ConfigEntries  []*ConfigEntry // NEW
}

// 在 ArtifactSpec.Validate() 中新增：
// - 每个 ConfigEntry 调用其 Validate()
// - 检测 {Block, Key} 组合重复（防 ConfigMap key / 投影路径冲突）
```

---

## 3. handler 层映射（`projects/infra/deploy/handler.go`）

新增 `toProtoConfigEntries` / `fromProtoConfigEntries`（1:1 字段映射，模式同 `toProtoSecretBindings` lines 605-640）：

```go
func toProtoConfigEntries(entries []*domain.ConfigEntry) []*ArtifactSpec_ConfigEntry
func fromProtoConfigEntries(proto []*ArtifactSpec_ConfigEntry) []*domain.ConfigEntry
```

在 `toProtoArtifacts`（line ~491）/ `fromProtoArtifacts`（line ~516）中接入 `ConfigEntries` 字段。

---

## 4. storage 层映射（`projects/infra/deploy/storage/mongo.go`）

新增 `mongoConfigEntry` struct + 三处映射（模式同 secret 的 `mongoSecretBinding` lines 142-146, 598-611, 780-793）：

```go
type mongoConfigEntry struct {
    Block string `bson:"block"`
    Key   string `bson:"key"`
    Type  string `bson:"type"`
    Value string `bson:"value"`
}
```

- `mongoArtifactSpec` 新增字段 `ConfigEntries []*mongoConfigEntry \`bson:"config_entries,omitempty"\``
- `configEntriesToMongo` / `configEntriesFromMongo`：1:1 转换
- `artifactSpecsToMongo` / `artifactSpecsFromMongo`：父映射接入

---

## 5. environment 层深拷贝（`projects/infra/deploy/domain/environment.go`）

在 `cloneArtifacts`（line ~429-465）中，仿 secret 的深拷贝（lines 454-460）新增：

```go
if len(artifact.ConfigEntries) > 0 {
    spec.ConfigEntries = make([]*ConfigEntry, len(artifact.ConfigEntries))
    for j, ce := range artifact.ConfigEntries {
        cp := *ce
        spec.ConfigEntries[j] = &cp
    }
}
```

---

## 6. runtime/k8s 转换（`projects/infra/deploy/runtime/k8s/converter.go`）

`convertArtifactToDeployment`（line ~83-97）与 `convertArtifactToStatefulWorkload`（line ~67-81）均新增：

```go
ConfigEntries: artifact.ConfigEntries,
```

（模式同 `SecretBindings: artifact.SecretBindings` at lines 79, 95）
