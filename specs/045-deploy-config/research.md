# Research: Deploy Config Support

**Branch**: `045-deploy-config` | **Date**: 2026-08-13 | **Spec**: [spec.md](spec.md)

## R1: SDK API 形态 — 各语言惯用法（不强行统一）

**Decision**: Go SDK 采用泛型函数 `Read[T any](block, key string, defaults T) (T, error)`；JS SDK 采用泛型函数 `readConfig<T extends object>(block: string, key: string, defaults: T): T`。两者形态不同，精神一致。

**Rationale**:
- Go 惯用法是显式 error return（参考整个 `common/gopkg` 与标准库风格）；泛型函数 `func F[T any](...) (T, error)` 是 Go 1.18+ 的地道写法。builder 风格 `Config(...).Read(out)` 在 Go 中不常见且额外分配。
- JS/TS 惯用法是抛异常 + 同步返回值（参考 `node-config`、`cosmiconfig` 等社区库均在启动期同步读取并失败时抛出）。
- 用户明确要求"使用各自语言的风格和读取配置的社区习惯，不要过度迁移"。统一成同一形态会牺牲地道性。
- 两者都保证：不修改调用方传入的 `defaults`（Go 内部深拷贝；JS 内部 `structuredClone`）。

**Alternatives Considered**:
- 用户示例的 builder 风格 `configsdk.Config(block,key,default).Read(out)`：Go 中不地道，且 `out` 需先分配再传入，比直接返回值更繁琐。拒绝。
- 两语言统一签名：违反"不要过度迁移"。拒绝。

**Sources**:
- Go 配置惯用法：https://eli.thegreenplace.net/2020/optional-json-fields-in-go ；https://stackoverflow.com/questions/47395430/merge-fields-two-structs-of-same-type
- JS 配置惯用法：https://github.com/node-config/node-config （`config.get()` 同步、失败抛异常）

## R2: 深度合并语义 — 对象递归合并，数组/标量替换

**Decision**: 深度合并对 object/map 类型递归合并（配置中存在的键覆盖默认值对应键，不存在的键保留默认值）；对数组和标量，配置值整体替换默认值（不按索引合并数组）。`null` 作为显式值覆盖默认值；`undefined`（仅 JS）不覆盖。

**Rationale**:
- 这是配置场景的**社区共识**。多个独立来源（jsonic.io、skillstuff.com、node-config `extendDeep`）一致得出：配置中的数组（如 allowed origins、feature flag 列表）应当**整体替换**而非拼接或按索引合并——"override array should replace the base array, not concatenate or index-merge"。
- Go 的 `encoding/json` / `yaml.v3` 的 `Unmarshal` 天然实现此语义：结构体字段递归保留缺失字段、切片整体替换。
- Spec FR-015 与澄清（Assumptions）已确定此语义，本决策落实实现路径。

**Alternatives Considered**:
- 数组按索引合并（`lodash.merge` 默认）：配置场景下会产生意外重复/错位。拒绝。
- 数组拼接（`deepmerge` 默认）：累积默认值，配置场景几乎总是错误。拒绝。

**Sources**:
- https://jsonic.io/guides/json-config-management （"use arrayMerge: (_dest, src) => src"）
- https://jsonic.io/guides/json-deep-merge （"arrays are replaced, source wins"）
- https://skillstuff.com/merging-objects-without-side-effects/ （"Replace arrays by default — safest"）

## R3: 深度合并实现路径 — "unmarshal over defaults" 惯用法

**Decision**: 
- **Go**：将调用方 `defaults` 序列化为中间 map → 解析配置文件为 map → 递归深合并（对象合并、其余替换）→ 序列化合并结果反序列化为输出 `T`。此路径完全掌控合并语义，不依赖具体解析器对"已填充结构体"的合并行为差异。
- **JS**：`structuredClone(defaults)` 深拷贝 → `js-yaml` 解析配置文件为对象 → 递归深合并（含原型污染防护）→ 返回。

**Rationale**:
- "unmarshal over defaults"（先用默认值填充，再 unmarshal 配置覆盖）是 Go 社区事实标准（见 R1 来源）。但 `yaml.v3` 对 map 字段是"替换"还是"合并"存在解析器差异；为保证 FR-015（对象/map 递归合并）严格成立，采用 map 层递归合并而非纯结构体 unmarshal 覆盖。
- 该实现仅运行于启动期，性能非关键（配置读取一次性），多一次 marshal/unmarshal 往返可接受。
- 必须深拷贝 `defaults` 以免修改调用方原始对象（Go 经 marshal 往返天然深拷贝；JS 用 `structuredClone`）。

**Alternatives Considered**:
- 纯 `Unmarshal(configBytes, &outPreFilledWithDefaults)`：最简，但 map 字段合并语义依赖解析器实现，不可靠。作为常见结构体场景的退化路径保留，但默认采用 map 合并保证严格语义。
- 引入 `mergo`/`koanf`/`viper`：重型库解决多源配置问题，本场景（单文件覆盖默认值）属于过度设计。拒绝。

## R4: 运行时一律 YAML 解析；`type` 仅用于部署期校验

**Decision**: service.yaml 数据条目的 `type: json|yaml` 字段**仅在部署期**做格式校验（FR-003：json 值须可被 JSON 解析、yaml 值须可被 YAML 解析）。运行时 SDK **始终用 YAML 解析器**读取文件，无需探测文件类型。

**Rationale**:
- JSON 是 YAML 1.2 的严格子集——**合法 JSON 内容用 YAML 解析器得到完全相同的结果**。由于部署期已校验 value 是其声明类型的合法文本，运行时文件内容与声明类型一致，YAML 解析器对 json-type 文件（合法 JSON）与 yaml-type 文件（合法 YAML）均正确解析。
- 简化 SDK：无需文件扩展名探测、无需类型发现机制、无需 sidecar manifest。
- Go 用仓库已有的 `gopkg.in/yaml.v3`；JS 新增 `js-yaml`（加入 `pnpm-workspace.yaml` catalog）。

**Alternatives Considered**:
- 文件扩展名 `.json`/`.yaml` 编码类型 + SDK 按扩展名选解析器：更"精确"但增加 SDK 复杂度，且对合法输入无实际收益。拒绝。
- SDK 先 `JSON.parse` 失败再回退 YAML：脆弱（YAML 但恰好是合法 JSON 时语义歧义）。拒绝。

**Sources**:
- YAML 1.2 规范明确 JSON 为子集：https://yaml.org/spec/1.2.2/ （"YAML can be viewed as a natural superset of JSON"）

## R5: config 数据进入期望状态 + 控制面创建 ConfigMap

**Decision**: 
- proto `ArtifactSpec` 新增 `repeated ConfigEntry config_entries = 12`，携带被选中配置块的内联数据（block/key/type/value）。
- 控制面（`projects/infra/deploy`）在 reconcile 时**创建 ConfigMap**（按 workload 命名 `{workload}-config`），将其投影为 pod volume。
- 这是控制面首次**创建**数据型 K8s 资源（当前仅引用预存的 TLS CA ConfigMap，不创建）。

**Rationale**:
- 与 secret 的本质区别：secret 引用**外部已存在**的 K8s Secret（控制面只投影）；config 数据**内联在 service.yaml**，无外部来源，必须由控制面据期望状态创建。
- FR-018 要求被选中的配置块数据（block/key/type/value）纳入期望状态以便重启后重建——控制面据此创建 ConfigMap，故期望状态携带完整 config 数据而非仅选择名。
- ConfigMap 是 K8s 挂载非敏感数据的标准载体（区别于 secret 的 Secret 投影）。
- 创建/清理 ConfigMap 复用 executor 现有 apply/prune 模式（`executor.go` 的 `applyInner`/prune 列表），新增一条 ConfigMap apply 路径。

**Alternatives Considered**:
- deploy CLI 在提交期望状态前直接创建 ConfigMap：破坏控制面单一所有权（reconcile 模型要求控制面拥有所有资源）。拒绝。
- config 数据不进期望状态、控制面回读 service.yaml：控制面不持有 service.yaml 解析能力（那是 CLI 的职责），且违背期望状态自包含原则。拒绝。

## R6: 运行时挂载约定（沿用 secret 先例）

**Decision**:
- 挂载路径：`/mnt/dominion/config`（只读）。
- 发现变量：`DOMINION_CONFIG_DIR`（值 `/mnt/dominion/config`），平台强制注入，加入保留变量列表。
- 文件布局：`{DOMINION_CONFIG_DIR}/{block}/{key}`。
- ConfigMap data key 为扁平的 `{block}-{key}`（ConfigMap key 不允许含 `/`），通过 `KeyToPath{Key: "{block}-{key}", Path: "{block}/{key}"}` 投影出目录结构（`KeyToPath.Path` 允许含 `/`）。

**Rationale**:
- 与 `DOMINION_SECRET_DIR` / `/mnt/dominion/secret` 完全对称（见 `builder.go:58-63` 与 `specs/002-deploy-secret-config/research.md` R3/R5），降低认知成本。
- 嵌套 `{block}/{key}` 避免扁平命名的碰撞风险（`block="a",key="b-c"` 与 `block="a-b",key="c"` 在扁平 `-` 拼接下碰撞）。
- block 名在配置块列表内唯一（FR-004）、key 名在所属配置块内唯一（FR-004），故 `{block}/{key}` 全局唯一，无路径冲突。
- ConfigMap key 限制：K8s ConfigMap data key 须匹配 `[-._a-zA-Z0-9]+`，不允许 `/`；故用 `{block}-{key}` 作 key，`KeyToPath.Path` 还原目录。

**Alternatives Considered**:
- 扁平布局 `{block}-{key}`：有碰撞风险。拒绝。
- 单文件每块（块内所有 key 合并为一文档）：不支持块内混合 type（用户模型 type 粒度为 per-key）。拒绝。

## R7: 配置选择校验位置 — compiler.Compile

**Decision**: 在 `tools/release/deploy/v2/compiler/compiler.go` 的 `Compile()` 中，紧随 secret 绑定校验（当前 lines 106-139）之后，新增 config 选择校验：deploy 中 artifact 选择的每个 config 名必须在 service.yaml 顶层配置块池中存在，否则拒绝。deploy.yaml `configs` 列表内的重复选择由 schema `uniqueItems` 在更早的 schema 校验层拒绝（见 `contracts/yaml-schema.md` §2），compiler 无需去重。

**Rationale**:
- `Compile()` 已有 serviceConfig + deployService 的完整信息，且已有 secret 双向校验的成熟模式。
- 与 secret 不同，config 是**单向选择**（deploy 从池中选择子集，池中未被选中的块不影响部署），故仅需校验"所选名存在"，无需 secret 的"全部声明须绑定"反向校验。
- 列表内重复选择若放行，将编译出重复 `{block,key}` 的 `ConfigEntry`，与控制面 domain 校验（VR-CE-2）冲突，故在 schema 层（`uniqueItems`）与 FR-004 的重复名拒绝保持对称。
- 校验失败 fail-fast 返回 error，阻止期望状态生成与提交（FR-007）。

**Alternatives Considered**:
- 在 `ParseDeployConfig` 中校验：此时无 serviceConfig，无法知道配置块池。拒绝（与 002 R4 同理）。
- 在控制面校验：太晚，应 CLI 端 fail-fast。拒绝。

## R8: 版本门禁 — config 仅在 v3 service.yaml 生效

**Decision**: config 块仅在 `version: "3.0"` 的 service.yaml 中被接受（与 secret 特性 002 一致）。v2 service.yaml 中出现 `configs:` 字段会被 v2 解析路径忽略或拒绝（取决于 schema additionalProperties 策略）。

**Rationale**:
- 用户的示例使用 `version: "3.0"`；secret（002）同样仅 v3 生效（`pkg/config/v3.go` 的 `ParseV3ServiceConfig`）。
- v3 是当前迁移方向（deploy v3 入口 `v3/apply.go` 强制 `ParseV3ServiceConfig`）。
- `ParseV3ServiceConfig` 委托 `ParseServiceConfig`（config 解析与校验所在，见 `data-model.md` "Go Struct Changes"），故 v3 路径自动获得 config 解析与 FR-003/FR-004 校验；`pkg/config/v3.go` 本身无需代码改动，但需单测验证版本门禁（非 3.0 的 service.yaml 被 `ParseV3ServiceConfig` 拒绝）与 config 兼容（3.0 + configs 解析通过）。
- config schema（`service.schema.json`）在顶层 `additionalProperties: false` 下新增 `config` 字段，对所有版本生效；但实际 config 解析与编译走 v3 路径。FR-020（向后兼容）保证：不含 config 的现有 service.yaml（v2 或 v3）行为不变。

**Alternatives Considered**:
- 同时支持 v2：需回溯到 v2 解析路径，增加维护成本，且 v2 是被淘汰方向。拒绝。

## R9: 大型测试策略 — 以 experimental 服务为被测对象

**Decision**: deploy 控制面自身无法自举大型测试（见 `README` 说明），但 config 特性可通过**部署 experimental 服务**（经真实控制面）端到端验证：
- **Go 被测对象**：`experimental/golang/grpc_hello_world/service` — 新增 config 块，`SayHello` 问候语来自 config（带默认值），testplan 验证覆盖生效。
- **TS 被测对象**：`experimental/ts/grpc_hello_world` — 同理。
- 覆盖全链路：声明（service.yaml）→ 选择（deploy.yaml）→ 校验（compiler）→ 期望状态（proto）→ reconcile（控制面创建 ConfigMap）→ 挂载（builder）→ SDK 读取 + 深度合并。

**Rationale**:
- experimental 项目的 testplan 已具备完整 deploy→test→cleanup 闭环（`testtool.MustEndpoint` + `go_largetest`），是验证部署期特性的现成基础设施。
- grpc_hello_world 的 `SayHello` RPC 天然适合展示 config 覆盖（问候语可参数化）。
- 修改 experimental 项目使其符合测试要求是用户明确允许的。

**Alternatives Considered**:
- 为 deploy 控制面本身写大型测试：控制面无法自举，不可行。
- 仅靠 `bazel build` 验证：违反宪章原则 VI（构建检查不构成大型测试验收）。

## R10: SDK 包位置与依赖

**Decision**:
- Go SDK：`common/gopkg/config`（package `config`），依赖 `gopkg.in/yaml.v3`（已在 go.mod）。
- JS SDK：`common/js/config`（包名 `@dominion/common-js-config`），新增依赖 `js-yaml`（加入 `pnpm-workspace.yaml` catalog）。

**Rationale**:
- `common/gopkg/*` 与 `common/js/*` 是现有共享运行时库的标准位置（如 `common/gopkg/otel`、`common/js/logs`），命名惯例为 `common/{lang}/{name}`。
- JS 包遵循 `@dominion/common-js-{name}` 命名（见 `common/js/resolver`、`common/js/logs`）。
- SDK 文档（FR-019）以 Go package doc comment 与 JS 包 README 说明 deploy 配置中 config 的完整使用方式（service.yaml 声明 + deploy.yaml 选择 + 运行时读取）。

**Alternatives Considered**:
- 放入 deploy 工具仓库：SDK 是**服务运行时**消费的库（被部署的服务使用），不属于 deploy 工具。应放共享运行时库。
