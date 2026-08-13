# Contract: Proto & Domain Changes

**Feature**: 045-deploy-config | **Spec**: [spec.md](../spec.md)

本契约定义 config 特性对 `projects/infra/deploy/deploy.proto` 及其 domain/storage 映射的扩展。配置数据在期望状态中以**层级结构**承载——`ConfigBlock`（块）包含若干 `ConfigEntry`（条目），对齐 service.yaml `configs[].data[]` 层级与控制面 per-block ConfigMap 物化映射。完整数据流见 [data-model.md](../data-model.md)。

---

## 1. deploy.proto 变更

### 新增 message：`ConfigBlock` 与 `ConfigEntry`

```protobuf
// ConfigBlock 是一个配置块，对齐 service.yaml 顶层 configs[]。控制面据此为
// 每个块创建一个独立的 ConfigMap object（见 contracts/runtime-contract.md §2）。
message ConfigBlock {
  // block 是 service.yaml 中声明的配置块名（SDK 第一寻址参数 / ConfigMap 名后缀）。
  string block = 1;

  // entries 是该块内的数据条目列表，每条目对应 ConfigMap 的一个 data key。
  repeated ConfigEntry entries = 2;
}

// ConfigEntry 是配置块内的一个数据条目，对齐 service.yaml configs[].data[]。
message ConfigEntry {
  // key 是条目名（块内唯一），SDK 第二寻址参数、运行时文件名、ConfigMap data key。
  string key = 1;

  // type 是格式类型 "json"|"yaml"，保留以备审计；
  // 运行时 SDK 一律用 YAML 解析，此字段不被 SDK 使用（见 specs/045-deploy-config/research.md R4）。
  string type = 2;

  // value 是原始数据文本（service.yaml configs[].data[].value 原样），
  // 成为 ConfigMap data 的内容。
  string value = 3;
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
  // config_blocks carries the configuration blocks selected for this artifact,
  // structured as block → entries (mirrors service.yaml configs[]). Each block
  // is materialized as one ConfigMap object (see
  // specs/045-deploy-config/contracts/runtime-contract.md §2). Empty when the
  // deployment selects no config blocks.
  repeated ConfigBlock config_blocks = 12;
}
```

> **字段号 12**：复用原 `config_entries = 12` 的字段号。本特性尚未发布、无生产数据（`specs/045-deploy-config/spec.md` Status: Draft；本分支未合并；`config_entries` 字段系本特性自身引入，无遗留持久化或线上 wire 数据），故直接将 12 号位的字段类型由 `repeated ConfigEntry` 改为 `repeated ConfigBlock` 并重命名为 `config_blocks`，无需保留 wire 兼容、无需 reserved 处理。
>
> **层级化建模理由**：配置的天然结构是"块包含条目"——service.yaml 源为 `configs[].data[]`、控制面物化目标为 per-block ConfigMap（每块一个 object，块内条目作 data key）。期望状态直接建模这个层级，使 proto → domain → storage → converter → model 全链路与 service.yaml 源、K8s 物化目标三处结构一致，消除 builder 层 `configEntriesByBlock` 重新分组的中间步骤（见 [runtime-contract.md §2](runtime-contract.md)）。`ConfigEntry` 不再冗余携带 `block` 字段——块归属由父节点 `ConfigBlock` 表达。

---

## 2. domain 层变更（`projects/infra/deploy/domain/spec.go`）

### 类型定义

```go
// ConfigBlock 携带一个配置块及其条目，对齐 service.yaml 顶层 configs[]
// （见 specs/045-deploy-config/data-model.md §4）。
type ConfigBlock struct {
    Block   string
    Entries []*ConfigEntry
}

// ConfigEntry 是配置块内的一个数据条目。
type ConfigEntry struct {
    Key   string
    Type  string
    Value string
}

// Validate 校验 ConfigBlock 自身与每个条目（VR-CB / VR-CE，
// specs/045-deploy-config/data-model.md §4）。
func (b *ConfigBlock) Validate() error

// Validate 校验 ConfigEntry 字段非空（VR-CE-1）。
func (e *ConfigEntry) Validate() error
```

### `ArtifactSpec` 字段与校验

```go
type ArtifactSpec struct {
    // ... 现有字段 ...
    SecretBindings []*SecretBinding
    ConfigBlocks   []*ConfigBlock // 期望状态层级的配置块（块包含条目）
}
```

`ArtifactSpec.Validate()` 中 config 校验规则：

1. 每个 `ConfigBlock` 调用其 `Validate()`（含 Block 非空、Entries 非空、每 entry 调 `ConfigEntry.Validate()`）。
2. **块名不重复**：`ConfigBlocks` 列表内 `Block` 重复 → 拒绝。两个同名块会映射到同一个 ConfigMap `{workload}-config-{block}`，结构上必然冲突，故作为防御纵深检测（对齐 service.yaml 层 VR-CB-3，防非 CLI 客户端绕过 schema；见 [yaml-schema.md §2](yaml-schema.md)）。
3. **块内条目名不重复**：同一 `ConfigBlock.Entries` 内 `Key` 重复 → 拒绝（VR-CE-2）。块内 key 重复即同一 ConfigMap 同一 data key 冲突（map 写覆盖）；不同块属于不同 ConfigMap（`{workload}-config-{block}`），天然隔离，仅检测块内即足够。

---

## 3. handler 层映射（`projects/infra/deploy/handler.go`）

新增 `toProtoConfigBlocks` / `fromProtoConfigBlocks`（层级 1:1 映射，模式同 `toProtoSecretBindings` lines 605-640）：

```go
func toProtoConfigBlocks(blocks []*domain.ConfigBlock) []*ConfigBlock
func fromProtoConfigBlocks(blocks []*ConfigBlock) []*domain.ConfigBlock
```

映射规则：外层 `ConfigBlock{Block, Entries}` → proto `ConfigBlock{Block, Entries}`；内层 `ConfigEntry{Key, Type, Value}` → proto `ConfigEntry{Key, Type, Value}`。`from` 对 nil 元素跳过（模式同 `fromProtoSecretBindings`）。

在 `toProtoArtifacts`（line ~491）/ `fromProtoArtifacts`（line ~516）中，于 `SecretBindings` 之后接入 `ConfigBlocks` 字段。

---

## 4. storage 层映射（`projects/infra/deploy/storage/mongo.go`）

新增层级化的 BSON 表示 + 映射函数（模式同 secret 的 `mongoSecretBinding` lines 141-146, 598-611, 780-793）：

```go
// mongoConfigBlock 是 domain.ConfigBlock 的 BSON 表示。
type mongoConfigBlock struct {
    Block   string              `bson:"block"`
    Entries []*mongoConfigEntry `bson:"entries,omitempty"`
}

// mongoConfigEntry 是 domain.ConfigEntry 的 BSON 表示（block 字段移除，归属父节点）。
type mongoConfigEntry struct {
    Key   string `bson:"key"`
    Type  string `bson:"type"`
    Value string `bson:"value"`
}
```

- `mongoArtifactSpec` 新增字段 `ConfigBlocks []*mongoConfigBlock \`bson:"config_blocks,omitempty"\``（取代原 `config_entries,omitempty`）。
- `configBlocksToMongo` / `configBlocksFromMongo`：层级 1:1 转换（外层 block + 内层 entries）。
- `artifactSpecsToMongo` / `artifactSpecsFromMongo`：父映射接入 `ConfigBlocks` 字段。

> **旧文档兼容读取**：无需兼容。理由：
> - 本特性未发布，仓库内不存在携带 `config_entries`（扁平）字段的历史持久化文档；该字段是本特性引入的，迁移期间没有外部数据依赖。
> - "无 config 字段的旧 ArtifactSpec 文档读取为 nil" 这一通用语义仍然成立——`config_blocks,omitempty` 对缺失字段读取为 nil slice，与现有 secret 引入时的旧文档兼容行为一致（`storage/mongo_test.go` 中"old document without config defaults to nil"用例语义保留，仅字段名更新）。
> - 期望状态由 deploy CLI 重新 `apply` 重建；若存在过渡期数据，重新 apply 即生成层级化的 `config_blocks`。

---

## 5. environment 层深拷贝（`projects/infra/deploy/domain/`）

### 统一风格：逐字段构造的 clone helper

深拷贝由 `domain/spec.go` 中紧邻类型定义的 `cloneConfigBlocks`（非导出）完成，`cloneArtifacts`（`domain/environment.go` line ~429）内仅一行委托：

```go
// cloneConfigBlocks 深拷贝 ConfigBlock 列表（外层逐字段 + 内层逐条目元素拷贝），
// nil/空输入返回 nil。形态与 storage.configBlocksToMongo
// （projects/infra/deploy/storage/mongo.go）逐行对称——domain clone / storage 双向
// / handler 双向统一为逐字段构造风格（见 specs/045-deploy-config/contracts/proto.md
// §5"风格统一决策"）。
// 新增 ConfigBlock/ConfigEntry 字段时 MUST 同步以下映射点（任一遗漏即静默丢字段，
// 由"测试防线"捕获）：
//   - cloneConfigBlocks（projects/infra/deploy/domain/spec.go，本函数）
//   - configBlocksToMongo / configBlocksFromMongo（projects/infra/deploy/storage/mongo.go）
//   - toProtoConfigBlocks / fromProtoConfigBlocks（projects/infra/deploy/handler.go）
func cloneConfigBlocks(blocks []*ConfigBlock) []*ConfigBlock {
    if len(blocks) == 0 {
        return nil
    }
    result := make([]*ConfigBlock, len(blocks))
    for i, cb := range blocks {
        block := &ConfigBlock{Block: cb.Block}
        if len(cb.Entries) > 0 {
            block.Entries = make([]*ConfigEntry, len(cb.Entries))
            for j, ce := range cb.Entries {
                block.Entries[j] = &ConfigEntry{
                    Key:   ce.Key,
                    Type:  ce.Type,
                    Value: ce.Value,
                }
            }
        }
        result[i] = block
    }
    return result
}
```

`cloneArtifacts` 中接入（替换原内联两层拷贝）：`spec.ConfigBlocks = cloneConfigBlocks(artifact.ConfigBlocks)`。nil 语义与 storage `configBlocksToMongo` 一致（nil/空 → nil，对齐 `style/golang.md`"函数返回空数组时返回 nil"）。

> **两层拷贝理由**：`ConfigBlock.Entries` 是指针 slice，浅拷贝会导致多个 Environment 共享底层 entry，破坏期望状态的隔离性。外层逐字段 + 内层逐元素拷贝与 secret（一层）同等深度保证，只是层级多一层。

> **风格统一决策（用户修改意见）**：domain 深拷贝原为内联结构体拷贝（`blockCp := *cb` / `entryCp := *ce`），与 storage/handler 的逐字段构造风格分裂。按用户意见统一为**逐字段构造**。两种写法均无 Go 编译期新增字段检查，差异在失效模式与可测性：
> - **结构体拷贝**（原 domain 写法）：新增标量字段自动带上（维护成本低）；新增指针/slice/map 字段**静默别名共享**——编译不报错、测试难以捕获（须为该字段专写突变隔离断言），且直接破坏期望状态隔离性。
> - **逐字段构造**（storage/handler 现状）：新增任何字段须显式列出（成本略高）；遗漏在 review diff 中可见，并可被 round-trip 测试系统性捕获。
> - 结论：取逐字段构造。三处（domain clone / storage 双向 / handler 双向）风格统一；失效模式从"难以捕获的别名污染"转为"可被测试捕获的丢字段"。新增字段成本差异仅 domain clone 一处——handler 与 storage 是跨类型映射，新字段无论如何都须显式列出——以一处显式成本换取统一与可控。

> **测试防线（新增字段漏同步的兜底）**：以下三层测试的 fixture MUST 为全部字段提供非零区分性取值并断言 `reflect.DeepEqual`，任一映射点遗漏字段即测试失败：
> - S1 `TestCloneArtifacts_DeepCopyConfigBlocks`（`domain/environment_test.go`）：clone 与原始全字段相等 + 突变隔离（修改 clone 的 entry 不影响 original）。
> - S2 `Test_toProtoArtifacts_fromProtoArtifacts_configBlocksRoundTrip`（`handler_test.go`）：domain→proto→domain 全字段 round-trip 相等 + nil/空输入保持 nil。
> - storage `TestArtifactSpecs_ConfigBlocks_BSONRoundTrip`（`storage/mongo_test.go`）：domain→BSON→domain 全字段 round-trip 相等（已存在）。

> **范围界定（secret 先例不动）**：`SecretBinding` 深拷贝（`cloneArtifacts` 内 `cp := *sb`，lines 454-460）维持结构体拷贝。理由：SecretBinding 是**单层纯标量结构**（三个 string 字段，无指针/slice/map），结构体拷贝语义等价于全拷贝、无别名共享风险、新增字段自动带上；ConfigBlock/ConfigEntry 是**两层指针容器结构**（`Entries []*ConfigEntry`），结构体拷贝仅为浅拷贝，必须逐字段/逐元素。即"结构体拷贝 vs 逐字段构造"由结构形态（是否含嵌套容器）决定，而非风格偏好；若未来 SecretBinding 新增嵌套字段，按同原则迁移，届时再评估提升为 `style/golang.md` 通用条文。

---

## 6. runtime/k8s 转换（`projects/infra/deploy/runtime/k8s/{converter,model}.go`）

### model.go：workload 字段

`DeploymentWorkload` 与 `StatefulWorkload` 的 `ConfigEntries []*domain.ConfigEntry` 字段改为 `ConfigBlocks []*domain.ConfigBlock`。

`configMapWorkload` 接口（model.go）访问器由 `configEntries() []*domain.ConfigEntry` 改为 `configBlocks() []*domain.ConfigBlock`——消费侧直接持有层级结构，**不再需要 builder 层 `configEntriesByBlock` 分组**（见 [runtime-contract.md §2](runtime-contract.md)）。

### converter.go：透传

`convertArtifactToDeployment`（line ~83-97）与 `convertArtifactToStatefulWorkload`（line ~67-81）均将透传字段改为：

```go
ConfigBlocks: artifact.ConfigBlocks,
```

（模式同 `SecretBindings: artifact.SecretBindings` at lines 79, 95）
