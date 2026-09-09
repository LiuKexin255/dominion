# Boundary Audit Checklist: dsh Preset Roster Demo (V4-2)

**Purpose**: US3 边界审计（spec.md US3 / tasks.md T020）——service 层零 roster API 与 preset 文件系统引用、插件层零 RPC/传输概念
**Created**: 2026-09-09
**Feature**: [spec.md](../spec.md)（US3 AS1/AS2）· 契约依据: [preset-authoring-plugin.md](../contracts/preset-authoring-plugin.md) §2 边界声明
**审计对象基线**: commit `491cc4a`（058 phase 4，T001-T019 完成）+ 本次审计的一处注释修正

## 判定基准（spec US3 原文限定语）

- **AS1**（service 层）："无任何 roster API 直接调用与文件系统读写（**对 preset 而言**）"——即审计的是 preset 领域的 roster API 调用与文件系统读写，传输层自身的基础设施文件访问不在其列。
- **AS2**（插件层）："无任何 RPC/传输层概念（proto、gateway 路由）进入插件；插件只依赖框架 ctx 与 roster 服务。"
- **承载矩阵**：V4-2 = "service 层零 roster/fs 引用，review 审计（checklist 项）"（[discussion-2026-09-08.md](../discussion-2026-09-08.md) §7）。

## 审计命令（可复现）

```bash
# service 层（tasks.md T020 原文命令）
rg "agentPresets|node:fs" experimental/dsh/demo/agent/src/
rg -n "agentPresets" experimental/dsh/demo/agent/src/     # 零命中（exit 1）
rg -n "node:fs" experimental/dsh/demo/agent/src/          # 2 文件命中

# service 层补充面
rg -n "fs\." experimental/dsh/demo/agent/src/server.ts            # 3 处：proto/TLS
rg -n "fs\." experimental/dsh/demo/agent/src/composition.test.ts  # 3 处命中（另有跨行 readdirSync 1 处，:158-159）：runfiles shim
rg -n "roster|dsh-agent-presets|agent-presets" experimental/dsh/demo/agent/src/ -i  # 全部为注释/文档引用

# 插件层
rg -n "proto|grpc|gateway|rpc|RPC|http" common/js/dsh-plugins/preset-authoring/src/ --type ts   # 3 处注释
rg -n '"(INVALID_ARGUMENT|ALREADY_EXISTS|NOT_FOUND|FAILED_PRECONDITION|INTERNAL)"' common/js/dsh-plugins/preset-authoring/src/ --type ts  # 错误码字符串字面量
rg -n "^import|from \"" common/js/dsh-plugins/preset-authoring/src/*.ts   # import 面
cat common/js/dsh-plugins/preset-authoring/package.json                   # 依赖面
```

## Service 层命中判定（`experimental/dsh/demo/agent/src/`）

### `agentPresets`（roster API 的 ctx 服务 key）：零命中

`session.ts` 与 `server.ts` 均不直接引用 roster API；preset 领域操作全部经插件服务接口——`ctx.get("presetAuthoring")`（`session.ts:211`、`server.ts:454`）与 `PresetAuthoringService` 类型面。**AS1 通过（roster 面）**。

### `node:fs`：2 文件命中，均不越界

| 命中 | 判定 | 理由 |
|---|---|---|
| `server.ts`（import 于 :24） | 不越界 | 3 处使用全部为 047 既有传输层基础设施：`loadProto()` 的 proto 存在性检查（proto/TLS 加载）与 TLS 证书对的 `existsSync`/`readFileSync`。属 gRPC 传输自身初始化，与 preset 领域无关——按 AS1 限定语"对 preset 而言"判定不越界。proto 加载走 service root 下的 `chat.proto`（部署产物），非 preset 文件。 |
| `composition.test.ts`（import 于 :3） | 不越界 | 4 处调用（3 处 `fs\.` 命中 + 1 处跨行 `readdirSync`，:158-159）全部在 `repairRunfilesNativeBindingLinks()`：bazel runfiles 布局下 pnpm hidden hoist 层的 node_modules 链接修复 shim（`existsSync`/`readdirSync`/`symlinkSync`，幂等 best-effort）。属测试环境基础设施，与 preset 领域文件操作无关。 |

### `.test.ts` 是否属审计范围（论证）

tasks.md T020 的 grep 范围是 `experimental/dsh/demo/agent/src/` 全目录（含测试）；spec US3 Independent Test 限定审计对象为"service 层源码（RPC handler 与会话管理）"。判定口径：

- **主对象**是源码文件（`server.ts` RPC handler、`session.ts` 会话管理）——上表两项源码命中均已判定。
- **测试文件**在 grep 范围内一并审计，但角色是审计的观察者而非 service 层本身：composition.test.ts 无 `agentPresets` 引用（零命中可证），其 preset 领域操作全部经被测 service 面（`AgentSessions`，内部消费 `ctx.presetAuthoring`）；它 boot 真直组组合并读取组合表面（`ctx.agents`/`ctx.tools`）属被测系统状态观察，不构成 service 层对 roster API 或 preset 文件的直接引用。其 fs 使用（runfiles shim）非 preset 领域。
- 测试文件的存在是 V1/V2 验证点的单测承载（research.md R11），不应为 grep 表面干净而移除。

**结论：测试文件命中均不越界，审计范围纳入但按观察者口径判定。**

## 插件层命中判定（`common/js/dsh-plugins/preset-authoring/src/`）

### RPC/proto/gateway 概念 grep：3 处命中，均为边界声明注释，不越界

| 命中 | 判定 | 理由 |
|---|---|---|
| `index.ts:11` "carries no RPC/transport concepts" | 不越界 | 插件文件头的边界声明本身（contracts §开头与 FR-005 要求的引用溯源），声明的是约束而非传输概念的使用。 |
| `store.ts:9` "maps onto gRPC statuses" | 不越界 | 注释描述**service 层**对插件稳定错误码的映射职责（contracts §6 "service 映射 gRPC status"），是边界契约的说明；插件自身不 import 任何 gRPC 符号。 |
| `materialize.ts:35` 同上 | 不越界 | 同上，错误码映射职责边界的说明。 |

### 错误码字符串（`INVALID_ARGUMENT`/`ALREADY_EXISTS`/`NOT_FOUND`/`FAILED_PRECONDITION`/`INTERNAL`）：不越界

这些字符串是 contracts §6 规定的**稳定域错误码词汇表**——插件抛 `PresetAuthoringError`（携带码），service 层（`server.ts` 的 `PresetAuthoringError` 映射）一对一翻译为 gRPC status。与 gRPC status 同名是刻意的映射便利设计，属领域错误词汇而非传输概念：插件无 proto 引用、无 gateway 路由、无 RPC handler、无传输层依赖。

忠实命令 `rg -n '"(INVALID_ARGUMENT|ALREADY_EXISTS|NOT_FOUND|FAILED_PRECONDITION|INTERNAL)"' ...`（仅匹配带引号的字符串字面量）的实际输出与判定对照：

- **实现文件命中 = 词汇表定义与抛出点**：五码 union 定义于 `materialize.ts:37-42`（`PresetAuthoringErrorCode`，五码齐全）与 `store.ts:28`（`PresetStoreErrorCode`：ALREADY_EXISTS/NOT_FOUND）；其余命中为各抛出点（如 `materialize.ts:126/133/147/153/175/...`、`index.ts:115` mapStoreError 的 INTERNAL 回退、`index.ts:153/168/171`）。`index.ts:144` 附近注释 "unexpected failures are INTERNAL (mapRosterError)" 同样描述该映射职责边界（注释无引号、不在本命令命中面，归 RPC/proto grep 的边界声明注释类，见上表）。
- **test 文件命中 = 断言**：`materialize.test.ts`/`store.test.ts`/`index.test.ts` 的全部命中均为 `toMatchObject({ code: ... })` / `toBe(...)` 形式的错误码断言，非传输概念使用。

### 依赖与 import 面佐证

- `package.json` dependencies：`@deepseek-ai/dsh-agent-presets`（roster）、`@deepseek-ai/schemastery`、`js-yaml`；peerDependency `@deepseek-ai/cordis`——**零传输库**（无 grpc/proto-loader/http 框架）。
- import 面：框架 ctx（cordis）、roster 官方服务（`@deepseek-ai/dsh-agent-presets`——AS2 明确允许）、schemastery、js-yaml、`node:fs/promises`/`node:crypto`/`node:path`（物化算法的文件操作，contracts §4 copy-then-patch 的实现载体）。文件系统操作收敛于插件内正是 FR-005 的边界设计：插件是 preset 文件操作的唯一所有者，service 层零 preset 文件访问。

## 审计发现与处置

- **发现 1（注释表述）**：`server.ts` 文件头注释原表述 "zero roster API and zero filesystem references (FR-005)" 缺失 FR-005/AS1 的"对 preset 而言"限定语，与该文件自身 proto/TLS 的 fs 使用事实相矛盾（FR-005 原文为"MUST NOT 直接引用 roster API 或读写 **preset 文件**"）。已修正为限定表述（"zero preset-file filesystem accesses ... the only filesystem uses here are the transport's own proto and TLS loading"），使注释与 spec 限定语及代码事实自洽。代码行为无越界，本处置仅纠正注释的终态表述准确性（`.specify/memory/constitution.md` 原则 VII）。
- **发现 2+**：无代码行为越界。未发现任何 service 层 roster API 直接调用、preset 文件读写或插件层传输概念。

## 验证（回改后复审）

注释修正后重跑审计：`agentPresets` 零命中、`node:fs` 命中面与判定不变。

```bash
bazel test --cache_test_results=no //experimental/dsh/demo/... //common/js/dsh-plugins/...
# Executed 16 out of 16 tests: 16 tests pass.
```

## 结论

- **AS1（service 层）**：通过——`agentPresets` 零命中（零 roster API 直接调用）；`node:fs` 2 处命中均为传输层/测试基础设施，非 preset 领域（AS1 限定语"对 preset 而言"）；preset 领域操作全部经 `presetAuthoring` 服务接口。
- **AS2（插件层）**：通过——零 proto/gateway/RPC/传输依赖；插件只依赖框架 ctx 与 roster 服务（加其职责所需的 fs/yaml）。
- **V4-2**：**通过**——边界交付物完成（对 agent_v2 迁移的模板价值，tasks.md Phase 5 Checkpoint）。
