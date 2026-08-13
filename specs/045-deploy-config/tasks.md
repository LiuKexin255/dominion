# Tasks: Deploy Config Support

**Input**: Design documents from `/specs/045-deploy-config/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: 单测按宪章原则 IV 并入各实现任务（每次代码变更执行 `bazel build` + `bazel test`，不单列 task）；大型测试单独分配验收 task（原则 VI）。

**Organization**: 按用户故事组织（spec.md US1/US2/US3），每故事可独立实现与测试。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 归属用户故事（US1/US2/US3）
- 所有任务含具体文件路径

---

## Phase 1: Setup（依赖）

**Purpose**: 为 JS SDK 准备 js-yaml 依赖（Go SDK 使用 go.mod 已有的 `gopkg.in/yaml.v3`，无需额外依赖）。

### 文档清单

- **代码规范文档**: 无（本 phase 仅修改 `pnpm-workspace.yaml` 与锁文件，不涉及 JS/TS 代码；相关操作命令见 `AGENTS.md`）
- **官方文档**: [pnpm catalog](https://pnpm.io/settings#catalog)
- **技术文章**: 无

### Tasks

- [ ] T001 Add `js-yaml` and `@types/js-yaml` to the `catalog` in `pnpm-workspace.yaml`（版本统一管理）；执行 `bazel run @pnpm -- --dir <repo_root> up` 更新 lockfile；执行 `bazel mod tidy`

**Checkpoint**: js-yaml 可被 JS 包引用。

---

## Phase 2: User Story 1 — service.yaml 配置块声明与校验 (Priority: P1) 🎯 MVP

**Goal**: 服务可在 service.yaml 顶层声明配置块（含 json/yaml 类型化数据条目），deploy 工具校验其格式与唯一性（FR-001~005, FR-020）。

**Independent Test**: 构造含 `configs` 的 service.yaml，`ParseServiceConfig` 接受合法配置、拒绝非法格式（FR-003）与重复名（FR-004）；不含 `configs` 的现有 service.yaml 行为不变（FR-020）。

### 文档清单

- **代码规范文档**: `style/golang.md`（+ [Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)）
- **官方文档**: [JSON Schema draft 2020-12](https://json-schema.org/draft/2020-12/json-schema-core)（schema 文件使用此版本）；[gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3)（格式校验解析）
- **技术文章**: 无

### Tasks

- [ ] T002 [P] [US1] Add `configs` property to `tools/release/deploy/pkg/schema/service.schema.json` per `specs/045-deploy-config/contracts/yaml-schema.md` §1（顶层 array，items 含 name/data，data items 含 name/value/type；name pattern `^[a-z][a-z0-9_-]{0,63}$`，type enum json|yaml）
- [ ] T003 [US1] Add Go structs `ServiceConfigBlock`、`ServiceConfigEntry` and `ServiceConfig.Configs` field (yaml tag `configs`) in `tools/release/deploy/pkg/config/config.go` per `specs/045-deploy-config/data-model.md` "Go Struct Changes"（依赖 T002）
- [ ] T004 [US1] Add config validation in `ParseServiceConfig` (`tools/release/deploy/pkg/config/config.go`): reject duplicate config block names、reject duplicate entry names within a block (FR-004)、validate value format per type — json 可被 `encoding/json` 解析、yaml 可被 `yaml.v3` 解析 (FR-003)；add testdata fixtures under `tools/release/deploy/pkg/config/testdata/`（如 `service.configs.yaml`）；add unit tests covering 合法/非法格式/重复名/无 configs 向后兼容（依赖 T003）。单测用例须显式覆盖以下 spec Edge Case 与约束：**空字符串 value 被拒绝**（schema `minLength 1`，spec Edge Case 已对齐）、**多配置块同文件独立解析互不干扰**、**版本门禁（R8）**——`version: "3.0"` 含 configs 的 service.yaml 经 `ParseV3ServiceConfig` 解析通过，非 3.0 版本被 `ParseV3ServiceConfig` 拒绝（`pkg/config/v3.go` 无需代码改动，仅补测试）

**Checkpoint**: service.yaml 配置块声明可被解析与校验，向后兼容。

---

## Phase 3: User Story 2 — deploy.yaml 配置块选择 + 控制面物化 (Priority: P1)

**Goal**: deploy.yaml artifact 选择配置块名，deploy CLI 校验选择有效性（FR-006~008），控制面将配置数据物化为 ConfigMap 并投影到容器（FR-009~011, FR-018），服务运行时经 `DOMINION_CONFIG_DIR` 发现（FR-010）。

**Independent Test**: deploy.yaml 选择合法配置块名 → `compiler.Compile` 接受并生成 `ConfigEntries`；选择未定义名 → 拒绝（FR-007）；控制面 reconcile 创建 ConfigMap 并投影为 `/mnt/dominion/config/{block}/{key}`。

### 文档清单

- **代码规范文档**: `style/golang.md`（+ [Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)）；`style/mongo.md`（T012 mongo storage 映射）；`style/api.md`（T008 proto 注释与风格，其引用的 AIP 链接为规范参考索引、非必读）；**先例文档（本 phase 严格仿照的 secret 特性）**：`specs/002-deploy-secret-config/research.md`（R3 env 注入顺序、R5 卷命名与挂载、R6 无 secret 行为、R7 proto 设计、R8 保留变量）、`specs/002-deploy-secret-config/contracts/secret-config.md`（secret 运行时契约先例）
- **官方文档**: [Kubernetes ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/)；[Kubernetes Projected Volumes](https://kubernetes.io/docs/concepts/storage/projected-volumes/)；[client-go corev1](https://pkg.go.dev/k8s.io/api/core/v1)
- **技术文章**: 无

> **参考先例**: 全链路改动模式严格仿照 secret 特性，具体文档已在"代码规范文档"分类中显式列出（002 research.md R3/R5-R8 与 contracts/secret-config.md）。task 描述中引用的本特性 contracts 章节为必读上下文。

### Tasks — CLI 侧（选择与校验）

- [ ] T005 [P] [US2] Add `configs` property (array of strings, `uniqueItems: true`) to `services[].artifact` in `tools/release/deploy/pkg/schema/deploy.schema.json` per `specs/045-deploy-config/contracts/yaml-schema.md` §2（重复选择由 schema 拒绝，VR-CS-2）
- [ ] T006 [US2] Add `DeployArtifact.Config` field (yaml tag `configs`, `[]string`) in `tools/release/deploy/pkg/config/config.go` per `specs/045-deploy-config/data-model.md`（依赖 T005）
- [ ] T007 [US2] Add config selection validation + compilation in `Compile()` (`tools/release/deploy/v2/compiler/compiler.go`): reject deploy-selected names not in service config pool (FR-007); compile selected blocks' entries into `deploy.ConfigEntry{Block,Key,Type,Value}` on `compiledArtifact`（单向选择，仿 secret 校验模式 lines 106-139 但无需反向"全部声明须绑定"；重复选择已由 schema `uniqueItems` 拒绝，compiler 无需去重）；add unit tests 覆盖：选择合法名 → 生成 ConfigEntries、选择未定义名 → 拒绝（FR-007）、**配置块池中未被任何 artifact 选择的块不产生 ConfigEntries（spec Edge Case：不影响部署）**（依赖 T006，且依赖 T008 proto `ConfigEntry` 定义存在以编译）

### Tasks — 控制面侧（proto → executor 物化链，顺序依赖）

> 此链按 proto → domain → handler/storage → converter → model → builder → executor 顺序，每步须 `bazel build`/`bazel test` 通过（原则 IV）。

- [ ] T008 [US2] Add `ConfigEntry` message (block=1, key=2, type=3, value=4) and `repeated ConfigEntry config_entries = 12` on `ArtifactSpec` in `projects/infra/deploy/deploy.proto` per `specs/045-deploy-config/contracts/proto.md`；regenerate proto Go code（bazel 生成）
- [ ] T009 [US2] Add `domain.ConfigEntry` type + `Validate()` (各字段非空) + `ArtifactSpec.ConfigEntries` field + duplicate `{Block,Key}` detection in `ArtifactSpec.Validate()` in `projects/infra/deploy/domain/spec.go` per `specs/045-deploy-config/contracts/proto.md` §2；add unit tests（依赖 T008）
- [ ] T010 [US2] Add ConfigEntries deep-copy in `cloneArtifacts` (`projects/infra/deploy/domain/environment.go`, 仿 secret lines 454-460)（依赖 T009）
- [ ] T011 [US2] Add `toProtoConfigEntries` / `fromProtoConfigEntries` and wire into `toProtoArtifacts` / `fromProtoArtifacts` in `projects/infra/deploy/handler.go` per `specs/045-deploy-config/contracts/proto.md` §3（依赖 T008、T009）
- [ ] T012 [US2] Add `mongoConfigEntry` struct + `configEntriesToMongo`/`configEntriesFromMongo` + wire into `mongoArtifactSpec`/`artifactSpecsToMongo`/`artifactSpecsFromMongo` in `projects/infra/deploy/storage/mongo.go` per `specs/045-deploy-config/contracts/proto.md` §4（依赖 T009）
- [ ] T013 [US2] Add `ConfigEntries` pass-through in `convertArtifactToDeployment` and `convertArtifactToStatefulWorkload` (`projects/infra/deploy/runtime/k8s/converter.go`, 仿 SecretBindings lines 79/95)（依赖 T009、T014——写入的 workload 结构体字段由 T014 在 `model.go` 新增，T013 须在 T014 之后）
- [ ] T014 [US2] Add `ConfigEntries` field to `DeploymentWorkload` and `StatefulWorkload` in `projects/infra/deploy/runtime/k8s/model.go`（依赖 T009）
- [ ] T015 [US2] Add config constants (`configVolumeName="dominion-config"`, `configMountPath="/mnt/dominion/config"`, `envConfigDir="DOMINION_CONFIG_DIR"`) and config projection block in `BuildDeployment` + `BuildStatefulSet` (`projects/infra/deploy/runtime/k8s/builder.go`) per `specs/045-deploy-config/contracts/runtime-contract.md` §2（projected ConfigMap volume + KeyToPath `{block}/{key}` + VolumeMount ro + env 注入；仅当 `len(workload.ConfigEntries)>0`）；add `BuildConfigMap(workload, cfg)` 生成 `{workload}-config` ConfigMap（data key `{block}-{key}`）；add unit tests（依赖 T014）
- [ ] T016 [US2] Add ConfigMap apply path (Get→Create-if-NotFound→Update) + prune list entry + `expectedApplyResources` entry in `projects/infra/deploy/runtime/k8s/executor.go` per `specs/045-deploy-config/contracts/runtime-contract.md` §2（ConfigMap 须在 Deployment/StatefulSet 前 apply）；add `envConfigDir`/`DOMINION_CONFIG_DIR` to `ReservedEnvironmentVariableNames`（依赖 T015）；add unit tests 覆盖：**SC-006/FR-016——同一 artifact 同时设置用户 env 与 config 时，builder 产物（Deployment/StatefulSet）同时包含用户 env 与 config 卷/挂载/`DOMINION_CONFIG_DIR` 注入，两者共存互不影响**
- [ ] T017 [US2] Update `tools/release/deploy/README.md`: document config feature usage（service.yaml `configs` 声明 + deploy.yaml `configs` 选择 + `DOMINION_CONFIG_DIR` 运行时约定）and add `DOMINION_CONFIG_DIR` to reserved env var list

**Checkpoint**: deploy.yaml 选择配置块 → CLI 校验 → 控制面创建 ConfigMap → 容器挂载 + `DOMINION_CONFIG_DIR` 注入。

---

## Phase 4: User Story 3 — 配置读取 SDK（Go + JS）(Priority: P2)

**Goal**: 提供 Go SDK `Read[T]` 与 JS SDK `readConfig<T>`，按 (block, key) 读取配置文件，深度合并到调用方默认值之上（FR-012~015, FR-019）。

**Independent Test**: 用 mock 配置文件目录单测 SDK：深度合并矩阵（对象合并/数组标量替换/默认值保留）、`defaults` 不被修改、错误情况（env 未设置/文件缺失/解析失败）；json-type 与 yaml-type 文件均正确解析。

> **可并行**: US3 仅依赖运行时约定（contracts/runtime-contract.md），不依赖 US2 代码，可与 Phase 3 并行开发。

### 文档清单

- **代码规范文档**: `style/golang.md`（+ [Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)）；`style/javascript.md`（+ [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)、[vitest Mocking Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)、`specs/019-js-test-reliability/` 测试执行模型）
- **官方文档**: [gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3)；[js-yaml](https://github.com/nodeca/js-yaml#api)；[structuredClone (MDN)](https://developer.mozilla.org/en-US/docs/Web/API/structuredClone)
- **技术文章**: [Optional JSON fields in Go — Eli Bendersky](https://eli.thegreenplace.net/2020/optional-json-fields-in-go)（unmarshal over defaults 惯用法）；[Deep Merge JSON Objects — jsonic.io](https://jsonic.io/guides/json-deep-merge)（JS 深度合并 + 数组替换 + 原型污染防护）

### Tasks

- [ ] T018 [P] [US3] Implement Go SDK in `common/gopkg/config/`: `doc.go`（package doc 说明 deploy config 完整用法 per FR-019，引用 `specs/045-deploy-config/contracts/sdk-go.md`）、`config.go`（`Read[T any](block, key string, defaults T) (T, error)` per `specs/045-deploy-config/contracts/sdk-go.md`：经 `DOMINION_CONFIG_DIR` 定位 `{block}/{key}`、yaml.v3 解析、map 层递归深合并、不修改 defaults）；`config_test.go`（合并矩阵 + 错误情况 + defaults 不变 + json/yaml 双格式 + **多配置块/多条目独立定位：不同 block 或 key 的读取互不干扰（spec Edge Case：多块各自通过 (block,key) 寻址）**）；run `bazel run //:gazelle common/gopkg/config` 生成 BUILD.bazel（依赖 T001 无关；Go 独立）
- [ ] T019 [P] [US3] Implement JS SDK in `common/js/config/`: `package.json`（`@dominion/common-js-config`，依赖 `js-yaml` from catalog）、`tsconfig.json`、`.swcrc`、`src/index.ts`（`readConfig<T>(block,key,defaults): T` per `specs/045-deploy-config/contracts/sdk-js.md`：`readFileSync` + `DOMINION_CONFIG_DIR` + `yaml.load` + `structuredClone` + 递归深合并 + 原型污染防护）、`src/merge.ts`（深合并实现）、`test/config.test.ts`（vitest，合并矩阵 + 原型污染防护 + defaults 不变 + **多配置块/多条目独立定位互不干扰**）、`README.md`（deploy config 用法 per FR-019）；`BUILD.bazel` via gazelle（依赖 T001 js-yaml catalog）

**Checkpoint**: 两 SDK 单测全绿，深度合并语义符合 data-model.md 矩阵。

---

## Phase 5: 大型测试验收 — experimental 端到端集成

**Purpose**: 以 `experimental/{golang,ts}/grpc_hello_world` 为被测对象，经真实控制面验证 config 全链路（声明→选择→物化→SDK 读取→深度合并）。deploy 控制面自身无法自举大型测试（README 豁免），故通过部署 experimental 服务验证（宪章原则 VI）。

### 文档清单

- **代码规范文档**: `style/golang.md`（+ [Google Go Style Guide](https://google.github.io/styleguide/go/guide)）；`style/javascript.md`（+ [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）；`style/large_test.md`（+ 加载 `testplan` SKILL 执行 `guitar run`）
- **官方文档**: 无
- **技术文章**: 无

### Tasks — Go 被测对象

- [ ] T020 [P] Add `configs` block to `experimental/golang/grpc_hello_world/service/service.yaml`（配置块 `service_config`，条目 `greeting` type yaml，含 message/times 字段）per `specs/045-deploy-config/quickstart.md` 场景 1
- [ ] T021 [P] Use Go SDK in `experimental/golang/grpc_hello_world/service/main.go`: define `Greeting` struct + defaults、call `config.Read("service_config","greeting",default)`、use merged result in `SayHello`；同时读取用户 env `GREETING_SUFFIX`（`os.Getenv`，为空则不加后缀）拼入问候语（SC-006/FR-016 端到端验证用，与 config 读取互不影响）；add `//common/gopkg/config` to `runtime_deps` in `service/BUILD.bazel`（依赖 T018）
- [ ] T022 Add config selection to `experimental/golang/grpc_hello_world/testplan/deploy.yaml`（service artifact `configs: [service_config]`，并同时设置用户 env `GREETING_SUFFIX`）；add assertion in `testplan/interface_test.go` that `SayHello` returns config-overridden greeting（proving 挂载+读取+深度合并），且响应包含 `GREETING_SUFFIX` 内容——**config 与用户 env 同存互不干扰（SC-006/FR-016）**；add case/suite to `testplan/interface_test.yaml` per `style/large_test.md`（既有计划新增 case，不新建 YAML）（依赖 T020、T021、Phase 2、Phase 3）

### Tasks — TS 被测对象

- [ ] T023 [P] Add `configs` block to `experimental/ts/grpc_hello_world/service.yaml`（同 Go 结构）per `specs/045-deploy-config/quickstart.md` 场景 2
- [ ] T024 [P] Use JS SDK in `experimental/ts/grpc_hello_world/src/server.ts`: define `Greeting` interface + defaults、call `readConfig<Greeting>("service_config","greeting",default)`、use in `SayHello`；同时读取用户 env `GREETING_SUFFIX`（`process.env`，为空则不加后缀）拼入问候语（SC-006/FR-016 端到端验证用）；add `@dominion/common-js-config` to `package.json`、add `//common/js/config` to `runtime_deps` in `BUILD.bazel`（依赖 T019）
- [ ] T025 Add config selection to `experimental/ts/grpc_hello_world/testplan/deploy.yaml`（service artifact `configs: [service_config]`，并同时设置用户 env `GREETING_SUFFIX`）；add assertion in `testplan/interface_test.go`（Go gateway 测试）that TS service returns config-overridden greeting，且响应包含 `GREETING_SUFFIX` 内容——**config 与用户 env 同存互不干扰（SC-006/FR-016）**；add case/suite to `testplan/interface_test.yaml`（依赖 T023、T024、Phase 2、Phase 3）

### Tasks — 大型测试执行（验收门禁，原则 VI）

- [ ] T026 Execute Go large test via testplan SKILL: `guitar run experimental/golang/grpc_hello_world/testplan/interface_test.yaml`（完整 部署→测试→清理 闭环）；**所有用例须全部通过**，失败则修复后重跑直至全绿（依赖 T022 及此前全部 phase）
- [ ] T027 Execute TS large test via testplan SKILL: `guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml`（完整闭环）；**所有用例须全部通过**（依赖 T025 及此前全部 phase）

**Checkpoint**: 全链路验证通过——config 声明→选择→ConfigMap 物化→挂载→SDK 读取→深度合并生效。

---

## Dependencies & Execution Order

### Phase 依赖

- **Phase 1 (Setup)**: 无依赖，立即开始。仅 T001。
- **Phase 2 (US1)**: 无依赖（service.yaml 声明独立）。T002→T003→T004。
- **Phase 3 (US2)**: US1 的 schema/struct 不阻塞 US2（deploy 侧独立），但 T007 (compiler) 引用 proto `ConfigEntry`，须 T008 先行；建议 US1 完成后做 US2 以便 service.yaml fixture 复用。控制面链 T008→T009→{T010,T011,T012,T014}→T013→T015→T016→T017 严格顺序（T013 依赖 T014：converter 写入的 workload 结构体字段由 T014 在 `model.go` 新增）。
- **Phase 4 (US3)**: **可与 Phase 3 并行**——SDK 仅依赖运行时约定（contracts/runtime-contract.md），不依赖 deploy 代码。T018（Go）与 T019（JS）彼此并行。T019 依赖 T001（js-yaml）。
- **Phase 5 (大型测试)**: 依赖 Phase 2 + Phase 3 + Phase 4 全部完成。

### 用户故事独立性

- **US1 (P1)**: service.yaml 声明 + 校验，独立可测（单测 ParseServiceConfig）。
- **US2 (P1)**: deploy 选择 + 控制面物化，独立可测（单测 compiler + 控制面单测/集成）。
- **US3 (P2)**: SDK 读取，独立可测（mock 文件目录单测）；端到端验证依赖 US1+US2。

### 并行机会

- Phase 2 的 T002（schema）与 Phase 1 并行。
- Phase 3 的 T005（deploy schema）可与 Phase 2 并行（不同文件）。
- Phase 4（US3）整体可与 Phase 3（US2）并行（不同代码库）。
- Phase 4 内 T018（Go SDK）与 T019（JS SDK）并行。
- Phase 5 内 T020/T021（Go demo）与 T023/T024（TS demo）并行。

---

## Parallel Example: Phase 4 (US3 SDK)

```text
# 两 SDK 不同语言/包，完全并行：
Task T018: "Implement Go SDK in common/gopkg/config/"
Task T019: "Implement JS SDK in common/js/config/"
```

## Parallel Example: Phase 3 控制面链中间层

```text
# T009 完成后，以下可并行（不同文件，均依赖 T009 的 domain.ConfigEntry）：
Task T010: "cloneArtifacts deep-copy in domain/environment.go"
Task T011: "to/fromProto in handler.go"
Task T012: "mongo mappers in storage/mongo.go"
Task T014: "workload field in runtime/k8s/model.go"
# T013（converter 透传）依赖 T014 的 workload 字段，须在 T014 之后单独执行：
Task T013: "converter pass-through in runtime/k8s/converter.go"（依赖 T009、T014）
```

---

## Implementation Strategy

### MVP First（US1 only）

1. Phase 1: T001（js-yaml catalog）
2. Phase 2: T002→T003→T004（service.yaml 配置块声明与校验）
3. **STOP and VALIDATE**: 单测 `bazel test //tools/release/deploy/pkg/config/...` 通过；含/不含 configs 的 service.yaml 行为正确（FR-020 向后兼容）

### Incremental Delivery

1. Phase 2 (US1) → 验证声明与校验
2. Phase 3 (US2) → 验证选择校验 + 控制面物化（单测 + 集成）
3. Phase 4 (US3) → 验证 SDK 深度合并（单测）【可与上一步并行】
4. Phase 5 → 大型测试端到端验收（guitar run，全用例通过）

### 验证门禁（宪章）

- 每个 task：`bazel build` + `bazel test`（受影响 target）通过（原则 IV）。
- Phase 5：testplan SKILL 执行 `guitar run`，完整 部署→测试→清理，所有用例通过（原则 VI）。
- 所有代码注释引用 spec/contracts 路径（原则 I）。

---

## Notes

- [P] tasks = 不同文件、无未完成依赖。
- [Story] label 映射到用户故事。
- 控制面 proto 链（T008-T016）须严格顺序——proto 改动触发代码生成，下游 domain/handler/storage 须同步更新否则无法编译。
- 大型测试须用 testplan SKILL 实际执行 `guitar run`，**禁止仅以 `bazel build` 测试 target 替代**（原则 VI）。
- experimental 项目改造须遵循 `style/large_test.md`：既有测试计划新增 case/suite，不新建 YAML；测试按模块组织。
