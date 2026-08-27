# Data Model: JS 项目全量切换 ESM

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-24

本特性无运行时数据实体；"数据模型"为**包依赖图与模块格式状态**——重构的对象与验收审计（SC-001/SC-004）的依据。

## 1. 实体

### JSWorkspacePackage（JS 工作区包）

pnpm workspace 内拥有独立 `package.json` 的 JS/TS 单元。

| 字段 | 说明 |
|---|---|
| path | 包目录（相对仓库根） |
| name | package.json `name` |
| role | `lib`（被工作区消费的库）/ `service`（容器交付）/ `cli`（js_binary 运行）/ `pin`（依赖钉扎）/ `frontend`（浏览器侧） |
| format | `cjs` → 迁移后 `esm`（frontend 恒为 `esm`） |
| entry | 服务入口（`src/bootstrap.js` 等）/ 库导出入口（`exports["."]`）/ bin |
| uses_grpc_instrumentation | bootstrap 是否装配 `createGrpcInstrumentation()`（决定 R3 hook 的生效路径，全部经 `common/js/otel` init 收敛） |
| packaging | `artifact_pkg_js`（tar+镜像）/ `js_binary` / 无 |

### ModuleConsumptionEdge（模块消费边）

`consumer → provider`，provider 为工作区包（经 pnpm `workspace:*` 链接，运行时走包 `exports`）或 npm 依赖（走 node_modules 链接）。

- 边的模块格式约束：`cjs consumer → esm provider` 依赖 Node 24 `require(esm)`（迁移中间态兜底）；终态全部边为 `esm → esm` 或 `esm → cjs(第三方)`（Node 原生互操作）。
- 终态不变量：**不存在任何 `require(esm)` 消费边**（`cjs → esm` 工作区边清零）。

### InstrumentedCjsModule（被插桩的 CJS 目标）

依赖 RITM/IITM hook 的 CJS 第三方包。当前集合：`{"@grpc/grpc-js"}`（唯一 instrumentation：`@opentelemetry/instrumentation-grpc`）。约束：生产侧必须经 IITM hook 加载（[contracts/otel-instrumentation-esm-contract.md](contracts/otel-instrumentation-esm-contract.md)）；新增成员时同步扩展 `common/js/otel` 的装配与规范文档。

## 2. 包清单与依赖图（迁移对象全景）

拓扑序（波 1 → 波 3，见 [research.md R9](research.md#r9)）：

### 波 1 — 基础库

| 包 | name | role | 工作区内依赖（出边） | CJS 惯用法改写点 | 特有动作 |
|---|---|---|---|---|---|
| `common/js/resolver` | @dominion/common-js-resolver | lib | — | 无（仅补导入扩展名） | — |
| `common/js/config` | @dominion/common-js-config | lib | — | 无 | js-yaml 走 ESM 构建（R5） |
| `common/js/otel` | @dominion/common-js-otel | lib | — | 无 | **init() 注册 IITM hook（R3 主决策）** |
| `common/js/logs` | @dominion/common-js-logs | lib | — | 无 | — |
| `common/js/grpc/otel` | @dominion/common-js-grpc-otel | lib | — | 测试 `require`→`createRequire`（index.test.ts:32-33,38） | 插桩装配文档注释终态化 |
| `common/js/grpc/resolver` | @dominion/common-js-grpc-resolver | lib | logs, resolver | 无 | tsconfig paths → `src/index.js`（R1） |
| `projects/game/pkg/saolei-board` | @dominion/game-saolei-board | lib+cli | — | golden.test.ts:22 `__dirname` | bin ESM 化（js_binary runfiles package.json 已具备） |
| `third_party/dsh/core` | @dominion/dsh-core | pin | — | 无 | **新增 .swcrc**（固化既有 ESM 产物，R2） |

### 波 2 — 服务

| 包 | name | 入口 | grpc 插桩 | 工作区内依赖（出边） | CJS 惯用法改写点 | 特有动作 |
|---|---|---|---|---|---|---|
| `experimental/ts/hello_world` | @dominion/hello_world | js_binary `src/main.js` | 否 | — | 无 | **BUILD data 补 package.json**（runfiles 模块判定，R6） |
| `experimental/ts/grpc_hello_world` | @dominion/grpc_hello_world | bootstrap | 是 | config, logs, grpc/otel, otel | server.ts:29-30 `__dirname` | **smoke_test.sh ESM 化**（含服务根 package.json 断言） |
| `experimental/grpc_chain/mid` | @dominion/grpc_chain_mid | bootstrap | 是 | logs, grpc/otel, grpc/resolver, otel | server.ts:20 `__dirname` | — |
| `experimental/openai_llm/client` | @dominion/openai-llm-client | bootstrap | 否 | logs, otel, resolver | 无 | — |
| `experimental/ts/team_graph_spike` | @dominion/team-graph-spike | bootstrap | 否 | logs, otel, resolver | 无（导入已带 .js） | — |
| `experimental/dsh/demo/agent` | @dominion/dsh-demo-agent | bootstrap | 是 | grpc/otel, grpc/resolver, logs, otel, resolver, dsh-core(运行时闭包) | dsh.ts:49,88、server.ts:24 `__filename` | **BUILD:57-62 与 dsh.ts:81 注释终态化**（原则 VII） |
| `projects/game/agent` | @dominion/game-agent | bootstrap + bootstrap-test | 是 | config, logs, otel, grpc/otel, grpc/resolver, resolver, saolei-board | server.ts:64、prompt-client.ts:29-33,44、skill-loader.ts:39（+注释）、skill-loader.test.ts:40-67 | —（`server_pkg`/`server_pkg_test` 经宏默认自动打包 package.json） |

### 波 3 — 横切（非包实体）

| 对象 | 动作 |
|---|---|
| `tools/dev/js/ts_proto_library.bzl` | 经 `--importFileExtension` 生成带 `.js` 相对导入（R4） |
| `tools/release/defs.bzl` | `artifact_pkg_js` 新增 `package_json` 属性（label，默认 `"package.json"`、不可关闭）+ 存在性门禁（Phase 2）与内容门禁 `"type": "module"`（Phase 5 启用）（R6） |
| `experimental/ts/grpc_hello_world/smoke_test.sh` | ESM 化 + 服务根 package.json 断言（R6） |
| `style/javascript.md` | 重写模块规范（R10，FR-009） |
| `tools/dev/js/vitest_test.bzl`、`tools/dev/js/run_vitest.mjs` | **零改动**（R7） |
| `projects/game/desktop/frontend` | **零改动**（已 ESM，仅回归验证 FR-002） |

## 3. 状态迁移（格式翻转的原子变更集）

每包单次原子变更（波 1/2 各包同构）：

```text
package.json   : + "type": "module"                    （格式声明）
tsconfig.json  : module commonjs → nodenext             （+ paths 指向 src/index.js，仅消费方包）
.swcrc         : commonjs → es6 + preserveImportMeta    （发射格式）
src/**/*.ts    : 相对导入补 .js；__dirname/__filename → import.meta.dirname/url
BUILD.bazel    : 服务 target 零改动（宏默认打包 package.json）；js_binary 面 data 补 package.json（hello_world）
测试文件        : __dirname → import.meta.dirname；require → createRequire（唯一豁免点）
注释           : 引用 CJS 前提的注释改为终态表述（原则 VII）
```

不变量（每包变更后的门禁）：`bazel build <pkg>/...` + `bazel test <pkg>/...` 全绿；对消费方包追加其下游 `*_typecheck` target 验证。

## 4. 验收态数据（Success Criteria 对应）

- SC-001/SC-004 静态审计命令集以 [contracts/esm-package-conventions.md §6](contracts/esm-package-conventions.md#6) 为唯一权威版本（终态零命中，豁免除外）；执行任务为 tasks.md T021。
- SC-002：`bazel test //...` 全绿，用例总数不少于 tasks.md Phase 2 Checkpoint 记录的基线（T021 对比）。
- SC-003/SC-005：`guitar run projects/game/testplan/system_test.yaml` 与 `guitar run experimental/dsh/demo/testplan/interface_test.yaml` 全用例通过（trace 断言覆盖 SC-005）；grpc_hello_world 与 grpc_chain/mid 的既有 testplan 亦执行通过（部署级验收闭环达成）；openai_llm/client 与 team_graph_spike 的既有 testplan 因既有缺陷无法通过、已完全移除（处置记录见 [tasks.md](tasks.md) T024）；hello_world 为 js_binary 非部署型服务无需豁免；README 不登记豁免。
