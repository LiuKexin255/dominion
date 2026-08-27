# Research: JS 项目全量切换 ESM

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-24 | **Status**: Complete

本文件记录 `048-js-esm-migration` 方案设计阶段的全部技术决策。每个决策按 **Decision / Rationale / Alternatives** 组织，引用仓库内路径（相对根目录）与仓库外 URL（原则 I 引用溯源）。

## 现状基线（调研结论）

- pnpm workspace 共 16 包：15 包 CJS（TS 源码以 ESM 语法书写，`ts_project(transpiler=swc)` 按 `.swcrc` 编译为 CJS），1 包（`projects/game/desktop/frontend`）已是 ESM（Vite，浏览器侧，无互操作面）。
- 14 份 `.swcrc` 字节级相同（`module.type: commonjs`）；`third_party/dsh/core` **没有** `.swcrc`，swc 默认 `es6`，其产物实际上已是 ESM（意外状态，见 [R2](#r2)）。
- 服务镜像 tar（`artifact_pkg_js`，`tools/release/defs.bzl:578`）**不含服务根 package.json**，故 `.js` 默认按 CJS 解析；ESM-only 依赖（langchain、MCP SDK、dsh 系列）经 Node 24 `require(esm)` 桥接消费。
- `require(esm)` 依赖链路已在 `specs/047-dsh-chat-demo/research.md` D8 验证（无顶层 await）。
- proto 生成代码（`tools/dev/js/ts_proto_library.bzl`，proto-loader-gen-types）**全部为 `import type`**，编译期擦除、不进 tar —— 生成代码运行时格式是**非问题**，仅类型检查受 NodeNext 影响（见 [R4](#r4)）。
- `style/javascript.md` 以 CJS 编译目标为前提书写（"生产代码由 swc 编译为 CJS"、"测试必须用 require() 触发 RITM"），须随重构更新（FR-009）。

---

## R1: 模块解析与书写策略 — NodeNext + 相对导入带 `.js` 扩展名

**Decision**: 全部 15 包 tsconfig 改为 `"module": "nodenext"`（隐含 `moduleResolution: nodenext`、`esModuleInterop` 默认开启）；源码相对导入统一补 `.js` 扩展名（`./merge` → `./merge.js`）；每包 `package.json` 增加 `"type": "module"`。

**Rationale**:
- 产物由 Node 直接运行（容器 `ENTRYPOINT /nodejs/bin/node` + `CMD .../src/bootstrap.js`，`tools/release/defs.bzl:641/665`），swc 不改写说明符（specifier passthrough），Node ESM 运行时**只接受带扩展名的相对导入**——NodeNext 是 TS 官方对"编译为 Node 原生 ESM"的唯一严格对应模式，并强制扩展名书写，消除运行时/类型检查歧义（TypeScript Handbook: https://www.typescriptlang.org/docs/handbook/modules/reference.html ）。
- `module: nodenext` 下 `.ts` 文件按**最近 package.json 的 `"type"` 判定格式**——per-package `"type": "module"` 即整包翻转，单一开关点。
- `import.meta.dirname`（Node ≥20.11 运行时、24.0 起非实验；`@types/node ≥20.11` 提供类型，本仓库 22.20 ✓）可直接替换 `__dirname`，见 [R8](#r8)。Node 运行时文档：https://nodejs.org/api/esm.html#importmetadirname ；类型来源 DefinitelyTyped PR [#68157](https://github.com/DefinitelyTyped/DefinitelyTyped/pull/68157)/[#68192](https://github.com/DefinitelyTyped/DefinitelyTyped/pull/68192)。

**Alternatives**:
- `module: esnext` + `moduleResolution: bundler`：bundler 解析允许无扩展名导入，但产物由 Node 而非 bundler 运行，无扩展名导入将在运行时 `ERR_MODULE_NOT_FOUND`——仅适合前端包（frontend 正是此配置，保持不变）。
- 保持 CJS：与本特性目标冲突；且 `specs/019-js-test-reliability/research.md` Fix C 记录的模块双实例问题根源即 CJS 编译层，本次为主动偿还。

**路径映射适配**: 消费方 tsconfig 的 `paths` 当前映射到目录（如 `"@dominion/common-js-logs": ["../../../common/js/logs/src"]`）。NodeNext 不支持目录导入，改为映射到 `.../src/index.js`——TS 扩展名替换规则（`X.js` → `X.ts`）会定位到源码 `.ts` 进行类型检查（Handbook "paths" 与相对导入扩展节：https://www.typescriptlang.org/docs/handbook/modules/reference.html ）。运行时解析不受 `paths` 影响（走 pnpm workspace `node_modules` 链接 + 包 `exports`）。注意 saolei-board 的入口是 `./src/core/index.js`（非 src/index）。

## R2: swc 编译配置 — `module.type: "es6"` + `preserveImportMeta: true`

**Decision**: 全部 15 包（含 `third_party/dsh/core` 新增）`.swcrc` 统一为：

```json
{
  "jsc": { "parser": { "syntax": "typescript" }, "target": "es2020" },
  "module": { "type": "es6", "preserveImportMeta": true }
}
```

**Rationale**:
- swc 无 `nodenext` 模块类型，ESM 输出即 `"es6"`（swc 官方配置：https://swc.rs/docs/configuration/modules ）。
- **`preserveImportMeta` 默认为 `false`**，会把 `import.meta` 转译掉（面向 CJS/downlevel 目标的设计）；ESM 产物需要 `import.meta.url/dirname` 原样保留，MUST 显式置 `true`（同上官方文档）。
- swc 不读取 tsconfig（https://github.com/swc/swc/issues/1348 ），tsconfig（类型检查，`tsc` 走 `*_typecheck` target）与 `.swcrc`（发射）必须人工保持一致；aspect_rules_swc 文档建议维持两文件同步：https://github.com/aspect-build/rules_swc/blob/main/docs/tsconfig.md 。
- `third_party/dsh/core` 现状无 `.swcrc` → swc 默认已是 ESM 输出（`bazel-out/.../third_party/dsh/core/version.js` 为 `export var ...`），新增 `.swcrc` 是把意外状态固化为显式契约（原则 VII 终态）。
- 实现时对 `import.meta` 输出做一次冒烟验证（swc 二进制随 rules_swc 2.7.1 工具链固定，行为以实际产物为准）。
- 已知 swc/tsc 发射差异需注意：类型导入擦除歧义（`export {A} from "./x"` 类写法必须显式 `import type`/`export type`）、不能消费 npm 包的 `const enum`（https://github.com/aspect-build/rules_ts/discussions/398 ）。仓库源码现状无此类写法，规范文档将禁止引入。

## R3: OTel gRPC 插桩在 ESM 下的等价机制 — `init()` 内注册 IITM loader hook（主）+ 测试侧 createRequire（保留）

**背景（最关键发现）**: RITM（require-in-the-middle ^8.0.1，经 `@opentelemetry/instrumentation` 0.218 传入）patch 的是 **`Module.prototype.require`**（https://github.com/nodejs/require-in-the-middle ）。而 Node 的 ESM→CJS 加载路径是 ESM translator 直接调 `Module._load`（Node v24.14.0 源码 `lib/internal/modules/esm/translators.js` 注释明言 "This goes through Module._load to accommodate monkey-patchers"：https://github.com/nodejs/node/blob/v24.14.0/lib/internal/modules/esm/translators.js ）——`Module._load` 位于 `Module.prototype.require` **之下**，RITM 的 patch 对 ESM `import` 的 CJS 包**不可见**。社区佐证：opentelemetry-js [#2779](https://github.com/open-telemetry/opentelemetry-js/issues/2779)。

同时：静态 import 声明提升，导入模块体（含 `registerInstrumentations()`/`init()` 调用）总在其全部静态导入求值**之后**执行（ECMA-262 module evaluation：https://tc39.es/ecma262/multipage/ecmascript-modules.html#sec-innermoduleevaluation ）——本仓库现有的"bootstrap 两段式"（静态只导 OTel 装配 → `await init()` → `await import("./server.js")`，见 `projects/game/agent/src/bootstrap.ts`）在 ESM 下继续保持正确的注册/加载顺序。

**Decision（生产路径）**: 在 `common/js/otel` 的 `init()` 中注册 OTel 官方 ESM loader hook（幂等，仅一次）：

```ts
import { register } from "node:module";
// init() 首次调用时：
register("@opentelemetry/instrumentation/hook.mjs", import.meta.url);
```

- IITM（import-in-the-middle，`@opentelemetry/instrumentation` 0.218 已依赖 `^3.0.0` 并发布 `hook.mjs`）可以 hook **从 ESM 图导入的一切模块（含 CJS 包内部）**，是 OTel 官方 ESM 支持路径：https://github.com/open-telemetry/opentelemetry-js/blob/main/doc/esm-support.md 、https://github.com/nodejs/import-in-the-middle 。
- 程序化 `module.register()`（而非启动参数 `--experimental-loader`，后者在 Node 处于弃用路径）是 OTel 官方声明的演进方向：https://github.com/open-telemetry/opentelemetry-js/issues/4933 。`parentURL` 用 `common/js/otel` 自身的 `import.meta.url`，`@opentelemetry/instrumentation` 是其直接运行时依赖（`common/js/otel/src/index.ts:7`），解析可靠；tar 内经 npm 闭包扁平化后同样可解析（RITM 今天能在生产生效即证明 `@opentelemetry/instrumentation` 在 tar 中）。
- 注册时机在 `init()` 内：先于任何 `await import("./server.js")`（IITM hook 对注册之后的 ESM 加载生效）；`common/js/otel` 及 bootstrap 的静态图中**不含** `@grpc/grpc-js`（装配只导 OTel SDK/装配器），无提前加载风险。所有服务 bootstrap 的形态不变（原则 II：一处收敛，服务零改动获得正确插桩）。
- Node 24.14 ≥ 24.11.1：IITM 同步 `registerHooks` 相关的 CJS-in-ESM-graph 缺陷已修复（nodejs/node#59929，见 https://github.com/nodejs/import-in-the-middle README 版本表）；本方案用异步 `module.register` 路径，不受该修复窗口约束，但版本满足度已确认。

**Decision（测试路径，保留既有验证语义）**: `common/js/grpc/otel/src/index.test.ts` 的 `require("@grpc/grpc-js")`（注册后触发 RITM 验证）改写为 `createRequire(import.meta.url)` 形式：

```ts
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const grpc: typeof import("@grpc/grpc-js") = require("@grpc/grpc-js");
```

- vitest 的 Vite 流水线不经过 Node loader（这正是 `specs/019-js-test-reliability/research.md` 记录的双实例根源）；`"type": "module"` 后测试文件按 ESM 处理，不应再依赖环境 `require`（vitest #846、#5522：https://github.com/vitest-dev/vitest/issues/846 、https://github.com/vitest-dev/vitest/issues/5522 ）。`createRequire` 返回的 require 走真实 `Module.prototype.require` → RITM 可见，验证语义与现状等价（Node 文档：https://nodejs.org/api/module.html#modulecreaterequirefilename ）。
- 该例外将记入 `style/javascript.md`（FR-008 的"测试豁免"）。

**Telemetry 等价验收**: 单测只覆盖 RITM 装配路径；生产 ESM 路径（IITM）的等价性由**大型测试验证**——game agent 与 dsh demo 的 testplan 断言 trace 产出（SC-005），作为 [contracts/otel-instrumentation-esm-contract.md](contracts/otel-instrumentation-esm-contract.md) 的验收条款。

**Alternatives**:
- **bootstrap 内 `createRequire` 预加载 grpc-js**（注册插桩后先 `require("@grpc/grpc-js")` 入 CJS 缓存，此后 ESM import 命中同一缓存）：无需 loader 机制，但依赖"ESM 对已缓存 CJS 模块的命名导出取值时点"这一未验证语义（Node CJS namespace 的命名导出为静态分析快照，live-binding 语义微妙：https://nodejs.org/api/esm.html#commonjs-namespaces ），且 OTel 维护者对 ESM 应用的官方指引是 loader hook 路径。列为**回退方案**：若实现期验证 IITM 在 Node 24.14 + grpc-js 1.14.4 组合上有缺陷，切换至此路径并补缓存语义验证。
- `NODE_OPTIONS=--experimental-loader=...` / 镜像 entrypoint 追加参数：改动部署面（entrypoint/NODE_OPTIONS 侵入 `tools/release/defs.bzl` 通用机制），且 `--experimental-loader` 处于弃用路径。不采用。

## R4: proto 生成代码 — 生成器原生 `--importFileExtension` 发射 `.js` 扩展名

**Decision**: `tools/dev/js/ts_proto_library.bzl` 向 proto-loader-gen-types 传递 `--importFileExtension=.js`（生成器原生选项，0.7.14 起提供，https://github.com/grpc/grpc-node/pull/2912 ；本仓库 catalog 钉扎 `@grpc/proto-loader` ^0.8.1，`pnpm-workspace.yaml:14`），生成器原生发射 NodeNext 兼容的相对导入；包说明符（`@grpc/grpc-js`、`@grpc/proto-loader`）由生成器原样发射、不受该选项影响。

**Rationale**:
- 生成代码全部是 `import type`（如 `bazel-out/.../greeter_types/experimental/ts/grpc_hello_world/greeter.ts` 的 `import type ... from './experimental/ts/grpc_hello_world/Greeter'`），运行时被擦除、不进 tar——模块格式本身无运行时影响。
- 但消费方 tsconfig `include` 覆盖 `generated/**/*.ts`（`projects/game/agent/tsconfig.json`、`experimental/ts/grpc_hello_world/tsconfig.json`），NodeNext 类型检查**对 `import type` 同样要求相对路径带扩展名**（Handbook 相对导入扩展节），否则 TS 报错阻断 `*_typecheck`。
- 版本前提：`--importFileExtension` 由 0.7.14 引入（https://github.com/grpc/grpc-node/pull/2912 ），更旧版本静默忽略该 flag、生成无扩展名导入导致 NodeNext 消费方 typecheck 失败；规则的 `tool` 属性覆盖约束为版本 MUST ≥0.7.14（`tools/dev/js/ts_proto_library.bzl` tool 属性 doc）。

**Alternatives**: 切换 ESM 友好的代码生成器（ts-proto 等）——超出"仅模块系统切换、不改依赖集合"边界（spec Assumptions/FR-006），否决。

## R5: CJS 第三方依赖互操作普查（按包逐一验证）

命名导出从 CJS 侧可用的前提是 cjs-module-lexer 静态检测（Node 文档：https://nodejs.org/api/esm.html#commonjs-namespaces ；检测模式冻结：https://github.com/nodejs/cjs-module-lexer ）。已逐一核实 node_modules 内实际产物：

| 依赖 | 版本 | 分发形态 | ESM 侧导入结论 |
|---|---|---|---|
| `@grpc/grpc-js` | 1.14.4 | 纯 CJS，无 exports map | `Object.defineProperty(exports, "X", {get})` 模式——lexer 可检测，**现有具名导入继续可用**；具名导出快照时点已由 R3 主方案规避 |
| `@grpc/proto-loader` | 0.8.1 | 纯 CJS | `exports.load/loadSync/... =` 模式，具名导入可用；不转发 protobufjs |
| `protobufjs`（proto-loader 传递依赖） | 7.6.5 | 纯 CJS | `module.exports = require(...)` 链 + 事后属性追加——**命名导出不可完整检测**；但仓库中仅被 proto-loader 内部 CJS→CJS 引用、生成代码仅 `import type`，**不产生 ESM 侧直接导入**，无影响（佐证 ts-proto#181 类错误场景不会触发） |
| `js-yaml` | 5.2.3 | 双分发（`exports.import → js-yaml.mjs`） | ESM 侧获得原生 ESM 构建，干净 |
| `express` | 5.2.1 | 纯 CJS，无 exports | 规范约定 **default import**（`import express from "express"`）——具名导入依赖 lexer 对 `lib/express.js` 的检测，不做硬依赖 |
| `mongodb` | 7.5.0 | 纯 CJS | `Object.defineProperty(exports, ...)` 模式，具名导入可用 |
| `pngjs` | 7.0.0 | 纯 CJS | `exports.PNG =` 模式，具名导入可用 |
| `@opentelemetry/*`（api/api-logs/instrumentation*/exporter*/sdk*） | 1.9/0.218/2.7 | CJS main（无 Node 侧 `import` condition） | ESM `import` 命中 `default` → CJS 构建；vitest 同样不认 bundler `module` 字段（vitest#4007）→ 测试与生产消费同一构建，RITM/IITM 语义一致 |
| langchain 家族、`@modelcontextprotocol/sdk`、`@deepseek-ai/dsh-*` | — | ESM-only（`type: module`） | 由 `require(esm)` 桥接改为**原生 ESM import**，桥接机制整体退役 |
| `zod` | 3.x | 双分发 | 无影响 |

**规范约定**（写入 `style/javascript.md`）：对 CJS 依赖优先 default import + 属性访问；具名导入仅用于已核实 lexer 可检测的包（上表"可用"行）。TS nodenext 对 CJS 具名导入持"乐观假设"（microsoft/TypeScript#54018：https://github.com/microsoft/TypeScript/issues/54018 ），静态检查不兜底运行时。

另：`require(esm)` 在 Node 24 无 flag 无警告（v22.12 起 unflagged；https://nodejs.org/docs/latest-v24.x/api/modules.html#loading-ecmascript-modules-using-require ）——迁移期间任何残余 CJS 入口（如旧 smoke 脚本过渡态）仍可消费 ESM 包，为分阶段迁移兜底。

## R6: 打包与运行入口 — tar/镜像附带服务根 package.json；js_binary 依赖 runfiles 内最近 package.json

**Decision**:
1. `artifact_pkg_js`（`tools/release/defs.bzl`）新增属性 `package_json = attr.label(default = "package.json", allow_single_file = True)`，所指文件打包至服务根 `dominion/{app}/{service}/package.json`。直接以标签声明被打包的文件（而非布尔开关间接表达）；默认值即正确行为——全部 7 个 `artifact_pkg_js` target（grpc_hello_world、grpc_chain/mid、openai_llm/client、team_graph_spike、dsh demo agent、game agent `server_pkg` + `server_pkg_test`）**零 BUILD 改动**自动获得该文件，未来新增服务同样默认正确。命名沿用本仓库既有约定：`js_runtime_library`（`tools/release/js_runtime_library.bzl`）的 `package_json = "package.json"` 属性（使用例 `projects/game/pkg/saolei-board/BUILD.bazel:66`）；不采用 `package`（与 Bazel 术语 BUILD package / `native.package()` 冲突）。
   **两道构建期门禁（终态执行语义，不可豁免）**：
   - **存在性门禁（随属性即生效，Phase 2 起）**：label 类型 + 非 None 默认值使属性**不可关闭**——目标所在 BUILD 包缺 `package.json` 文件时 label 解析失败，`bazel build` 分析期即报错。不存在"不打包 service-root package.json"的合法配置；CJS 时代的"缺省即 CJS"路径被结构性移除。
   - **内容门禁（Phase 5 收尾启用）**：打包 action 内断言 manifest 含 `"type": "module"`（`grep` 失败即 action 失败，报错信息指明"ESM-only 构建，CJS 服务产物不再支持"）。文件内容在 Bazel 分析期不可读，故为执行期断言。
   **时序**：Phase 2 落地属性时各服务尚为 CJS（package.json 无 `"type"`，最近 package.json 存在但无 `"type"` 仍按 CJS 解析，打包行为中性）——存在性门禁立即生效（7 个服务均有 package.json，全绿），内容门禁若同时启用会打断 Phase 2→4 窗口的 main 分支构建，故安排在 Phase 5（最后一个服务翻转完成后）启用，此后任何 CJS 服务 tar 均无法通过构建。
2. `js_binary` 运行面（`experimental/ts/hello_world:run`、`projects/game/pkg/saolei-board:cli`）：确保包自身 `package.json` 进入 runfiles 并位于产物同根（rules_js 机制：runfiles 中最近 package.json 决定 `.js` 模块类型，https://github.com/aspect-build/rules_js/issues/446 ）。saolei-board 的 `js_library pkg` 已含 `package.json`（`projects/game/pkg/saolei-board/BUILD.bazel:43`）；`hello_world` 现仅 `data = [":lib"]`，需补 `package.json` 进 data。rules_js 3.0.3 已含 `type: module` 场景的 launcher `.cjs` 修复（https://github.com/aspect-build/rules_js/pull/1818 ，原 issue #1756）。rules_js 上游 target 无自定义参数面，此路径保持 data 声明（与 tar 路径的宏参数机制不同属正常——前者是第三方规则的黑盒复用，后者是本仓库自有宏的 API 设计）。
3. `experimental/ts/grpc_hello_world/smoke_test.sh` 适配 ESM：`require('./node_modules/...')` 改为 `await import(...)`（`node -e` 内动态 import，或 `--input-type=module`）；"Test 3 bootstrap 可解析"断言从 `includes('require')` 改为 ESM 标记（如 `includes('import')`）；**新增断言**：tar 内 `dominion/{app}/{service}/package.json` 存在且 `"type": "module"`（`package_json` 属性打包结果的产物级门禁）。
4. `experimental/dsh/demo/agent/BUILD.bazel:57-62` 的"无服务根 package.json → CJS 默认解析"注释整体改写为 ESM 终态表述（原则 VII，改写后描述 `package_json` 属性默认打包的语义）；闭包审计（`experimental/dsh/demo/testplan/closure_audit_test.go`）审计的是依赖闭包集合而非模块格式，不受影响，但需全量重跑。

**Rationale**: Node 以**最近的 package.json `"type"`** 判定 `.js` 模块格式（https://nodejs.org/api/packages.html#type ）；tar 内 node_modules 各包自带 package.json 已满足依赖侧，唯服务根缺位。ESM-only dsh 依赖自此走原生 import（`experimental/dsh/demo/agent/src/dsh.ts:88` 的 `pathToFileURL(__filename)` 锚点简化为 `import.meta.url`，:81 的"CJS 等价物"注释随之改写）。

**Alternatives**:
- `include_package_json = attr.bool(mandatory = True)`（布尔开关）——开关到实际文件之间隔一层间接（True → 隐含"包内 package.json"），且需逐 target 显式声明 7 处；直接标签赋值 + 正确默认值更清晰且零 BUILD 改动。否决。
- 可选属性（`package_json = None` 表示不打包，靠调用方自觉）——保留了"无 service-root package.json"的合法配置空间，与"ESM 成为规范、不再支持 CJS 构建"的终态冲突：新增服务遗漏 manifest 时会静默回退 CJS 默认解析。否决；属性不可关闭。
- 各服务 target 经既有 `data_files` 增加 `"package.json"`（宏零改动）——把"模块格式判定的必要文件"降格为普通资源文件，语义隐式：BUILD 读者无从区分 package.json 是资源还是格式声明；新增服务时遗漏仅在运行时（启动 `SyntaxError` 或大型测试）暴露。否决。
- 入口改 `.mjs` 后缀（`src/bootstrap.mjs`）——ts_project/swc 输出扩展名联动、BUILD/gazelle/entrypoint 字符串全链改动，爆炸半径大于宏属性方案，否决。

## R7: vitest 测试基建 — 不变 + 两处点状适配

**Decision**:
- `vitest_test` 宏（`tools/dev/js/vitest_test.bzl`）与 `tools/dev/js/run_vitest.mjs` **零改动**：入口本就是 `.mjs`；`data` 装 raw `.ts` 源码（而非编译 `:lib`）的契约在 ESM 下继续成立且更重要（避免双实例，`specs/019-js-test-reliability` 的既有结论对 ESM 同样适用）。
- NodeNext 风格带 `.js` 扩展名的相对导入在 vitest 3.2.7 下可解析：vitest 3.x 基于 Vite ≥6.1，TS importer 的 `./x.js` → `./x.ts` 映射已修复（https://github.com/vitejs/vite/pull/18889 、https://github.com/vitest-dev/vitest/issues/5999 ）。
- 唯一使用 `require()` 的测试（`common/js/grpc/otel/src/index.test.ts:32-33`）改 `createRequire`（见 R3 测试路径）。

**Rationale**: vitest 经 Vite 流水线变换执行 TS，与包 `"type"` 无强耦合；宏的"禁止 :lib 与 .ts 并存"不变量（`tools/dev/js/vitest_test.bzl:8-34`）与本迁移正交。

## R8: CJS 惯用法改写清单（file:line 基线）

| 位置 | 现状 | ESM 等价 |
|---|---|---|
| `experimental/ts/grpc_hello_world/src/server.ts:29-30` | `path.join(__dirname, "..")` | `path.join(import.meta.dirname, "..")` |
| `experimental/grpc_chain/mid/src/server.ts:20` | 同上 | 同上 |
| `experimental/dsh/demo/agent/src/dsh.ts:49,88` | `path.dirname(__filename)` / `pathToFileURL(__filename).href` | `import.meta.dirname` / `import.meta.url`（`:14` pathToFileURL 导入一并移除） |
| `experimental/dsh/demo/agent/src/server.ts:24` | `path.dirname(__filename)` | `import.meta.dirname` |
| `projects/game/agent/src/server.ts:64` | `__dirname` | `import.meta.dirname` |
| `projects/game/agent/src/prompt-client.ts:29-33,44` | `__dirname` | `import.meta.dirname` |
| `projects/game/agent/src/skill-loader.ts:39`（注释 :12-13,:29-32 提及 commonjs） | `join(__dirname, ...)` | `join(import.meta.dirname, ...)`；注释改写为终态 |
| 测试: `projects/game/agent/src/skill-loader.test.ts:40,48,59,67`、`projects/game/pkg/saolei-board/src/core/golden.test.ts:22`、`common/js/grpc/otel/src/index.test.ts:38` | `__dirname` | `import.meta.dirname` |
| `common/js/grpc/otel/src/index.test.ts:32-33` | `require(...)` | `createRequire(import.meta.url)`（R3） |
| `experimental/ts/grpc_hello_world/smoke_test.sh:97-117` | `node -e "require(...)"` | 动态 `import(...)`（R6） |
| 全部 15 包源码相对导入 | 大多无扩展名（dsh demo agent 与 team_graph_spike 已带） | 统一补 `.js` |

`import.meta.dirname` 在 Node 24 为非实验特性（v24.0.0 起，https://nodejs.org/api/esm.html#importmetadirname ）；`@types/node` 22.20 ≥ 20.11 提供类型（R1）。

## R9: 迁移顺序与原子性 — 依赖拓扑（库先行），每包一次原子翻转

**Decision**: 按依赖拓扑分三波，每包的 `package.json`/`tsconfig`/`.swcrc`/源码/构建文件**同批原子变更**，波内按序：

1. **基础库**（无工作区内依赖方未翻转者先动）：`common/js/resolver`（零依赖）→ `common/js/config`、`common/js/otel`（含 R3 hook 注册）、`common/js/logs` → `common/js/grpc/otel`（dev 依赖 grpc-js；tsconfig 无 paths）→ `common/js/grpc/resolver`（tsconfig paths 改 index.js）→ `projects/game/pkg/saolei-board`、`third_party/dsh/core`（新增 .swcrc）。
2. **服务**：`experimental/ts/hello_world`（+BUILD data 补 package.json）→ `experimental/ts/grpc_hello_world`（+smoke 断言）→ `experimental/grpc_chain/mid` → `experimental/openai_llm/client` → `experimental/ts/team_graph_spike` → `experimental/dsh/demo/agent` → `projects/game/agent`（两个入口 bootstrap/bootstrap-test）。
3. **横切收尾**：`style/javascript.md` 重写、注释终态化（原则 VII）、静态审计、全量 build/test、大型测试。

**Rationale**:
- 消费方（node10 旧解析）类型检查对依赖包格式不敏感；反之翻转后的 nodenext 消费方要求其 paths 指向的依赖源码按 ESM 判定（最近 package.json）——库先行保证任意中间提交点全仓类型检查可通过。
- pnpm workspace 链接与 `require(esm)`（Node 24 兜底，R5）使未翻转的 CJS 消费方仍可加载已翻转 ESM 库（无顶层 await，`specs/047-dsh-chat-demo/research.md` D8 已验证依赖集合无 TLA）——波内顺序不必强拓扑，但拓扑序最小化中间态依赖桥接。
- 前端包不参与（已 ESM）；`tools/dev/js/run_vitest.mjs` 已 `.mjs`。

## R10: 规范文档更新范围（FR-009）

`style/javascript.md` 需重写的模块相关表述：
- "生产代码由 swc 编译为 CJS、`import()` 被转译为 `require()`"（:104 附近）→ ESM 终态表述（swc es6 + preserveImportMeta、NodeNext 扩展名规则）。
- 测试 `require()` 强制令（:87-105）→ 收窄为唯一豁免：RITM 验证场景使用 `createRequire(import.meta.url)`；生产插桩等价由 IITM hook（`common/js/otel` init 内注册）承载。
- 新增：相对导入必须带 `.js`；CJS 依赖 default-import 约定（R5）；`import.meta.dirname/url` 资源定位约定；新增插桩目标 CJS 包时的处理路径（经 `common/js/otel` 扩展，而非散落 bootstrap）。

## 版本敏感性观察清单（实现期验证点）

- swc（rules_swc 2.7.1 工具链二进制）对 `preserveImportMeta: true` 的实际输出——首个包翻转时冒烟检查产物含 `import.meta`。
- IITM（`@opentelemetry/instrumentation` 0.218 内 `import-in-the-middle` ^3）hook 在 Node 24.14.0 的 `module.register` 路径——由 game agent / dsh demo 大型测试的 trace 断言覆盖（R3 验收）。
- vitest 3.2.7 对带扩展名导入的解析（Vite ≥6.1 已修复，首个库包测试运行时确认）。
- `@types/node` 22.20 的 `import.meta.dirname` 类型可用性（编译门禁自然覆盖）。

## 参考文档总索引

仓库内：
- `specs/019-js-test-reliability/research.md`（CJS 双实例问题、Fix C 否决记录——本特性承接）
- `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest shim 契约，保持不变）
- `specs/047-dsh-chat-demo/research.md` D8/D10（require(esm) 决策与 `__filename` 锚点——终态化对象）
- `tools/release/defs.bzl`、`tools/dev/js/ts_proto_library.bzl`、`tools/dev/js/vitest_test.bzl`、`tools/dev/js/run_vitest.mjs`
- `style/javascript.md`

仓库外（正文已随决策引用）：TypeScript Handbook（modules/reference、ESM/CJS interop 附录）、Node 文档（packages#type、esm#commonjs-namespaces、esm#importmetadirname、modules#require(esm)、module#createrequire）、swc.rs/docs/configuration/modules、aspect rules_ts/rules_swc/rules_js 文档与 issue（#446/#1818/#362、transpiler.md、tsconfig.md）、opentelemetry-js（esm-support.md、#2779、#4933）、nodejs/require-in-the-middle、nodejs/import-in-the-middle、nodejs/cjs-module-lexer、vitest（#846/#5522/#5999/#4007）、vite（#18889）、microsoft/TypeScript#54018、DefinitelyTyped（#68157/#68192）。
