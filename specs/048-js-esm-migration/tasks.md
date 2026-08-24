# Tasks: JS 项目全量切换 ESM

**Input**: Design documents from `/specs/048-js-esm-migration/`（plan.md、spec.md、research.md、data-model.md、contracts/、quickstart.md）

**Tests**: 本特性不新增单测任务——既有 vitest 套件即回归防线（spec FR-003 禁止删测/跳测）；编译+单测按宪章原则 IV 内嵌于各任务；大型测试作为验收任务单列（Phase 6，宪章原则 VI）。

**Organization**: 按用户故事组织。注意**实施顺序与优先级倒置**：spec 中 US1（服务，P1）价值最高，但 US2（公共库，P2）是其技术前置——翻转后的 nodenext 服务消费库源码要求库已按 ESM 判定（[research.md](research.md) R9 库先行），故实施序为 基建 → US2 → US1 → US3 → 验收。

**标准翻转清单**（Phase 3/4 各包任务共用的原子变更集，出自 [data-model.md](data-model.md) §3；各任务在其基础上列包特有点）：

```text
package.json   : + "type": "module"
tsconfig.json  : "module": "commonjs" → "nodenext"（消费工作区依赖的包：paths 值改为 ".../src/index.js"）
.swcrc         : {"module": {"type": "es6", "preserveImportMeta": true}}（jsc 不变：typescript/es2020）
src/**/*.ts    : 相对导入（含测试内）补 .js 扩展名；__dirname/__filename → import.meta.dirname/url
验证           : bazel build <pkg>/... && bazel test <pkg>/...
```

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 项目初始化

本特性为仓库原地重构，无项目初始化任务。文档阅读门禁见各 phase 文档清单（宪章原则 V）。

**本 phase 文档清单**（宪章原则 V，编码前必读）：
- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 构建基建先行改动——全部行为中性（迁移前 CJS 状态下构建/测试保持全绿），为波 1/2 依赖

**⚠️ CRITICAL**: 本 phase 完成前不得开始任何用户故事

**本 phase 文档清单**（宪章原则 V，编码前必读）：

- **代码规范文档**：
  - `style/javascript.md`（注意：其中"生产代码由 swc 编译为 CJS"与"require() for RITM"两节描述迁移前状态，Phase 5 将重写；本 phase 以 [specs/048-js-esm-migration/contracts/esm-package-conventions.md](../contracts/esm-package-conventions.md) 为准）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的基准规范）
- **官方文档**：
  - [SWC Module 编译配置](https://swc.rs/docs/configuration/modules)（`preserveImportMeta` 语义）
  - [rules_swc: tsconfig/.swcrc 同步指引](https://github.com/aspect-build/rules_swc/blob/main/docs/tsconfig.md)（swc 不读 tsconfig，两文件锁步）
  - [rules_ts: Transpiler 拆分机制](https://github.com/aspect-build/rules_ts/blob/main/docs/transpiler.md)（tsc 类型检查与 swc 发射分离）
  - [@grpc/proto-loader（proto-loader-gen-types 生成器）](https://github.com/grpc/grpc-node/tree/master/packages/proto-loader)
- **技术文章/技术参考文档**：
  - [specs/048-js-esm-migration/research.md](../research.md)（R2/R4/R6）
  - [specs/048-js-esm-migration/contracts/esm-package-conventions.md](../contracts/esm-package-conventions.md)（§2/§4）
  - [specs/048-js-esm-migration/data-model.md](../data-model.md)（§2 波 3 表）

- [ ] T001 为 `artifact_pkg_js` 新增 `package_json` 属性并落地存在性门禁：修改 `tools/release/defs.bzl`——新增 `package_json = attr.label(default = "package.json", allow_single_file = True)`，打包 action 将该文件复制至 tar 服务根 `dominion/{app}/{service}/package.json`（复用既有 data_files 同级拷贝路径）；属性不可关闭（无 None 合法值 = 缺文件时构建分析期失败）。验证：`bazel build //experimental/ts/grpc_hello_world:server_pkg //experimental/grpc_chain/mid:server_pkg //experimental/openai_llm/client:server_pkg //experimental/ts/team_graph_spike:server_pkg //experimental/dsh/demo/agent:server_pkg //projects/game/agent:server_pkg //projects/game/agent:server_pkg_test` 全绿，且解包任一 tar 确认服务根 package.json 存在（彼时无 `"type"` 字段，行为中性）。依据 research.md R6（含 `js_runtime_library` 的 `package_json` 命名先例 `tools/release/js_runtime_library.bzl`）
- [ ] T002 `ts_proto_library` 生成物补 `.js` 扩展名：修改 `tools/dev/js/ts_proto_library.bzl`——为生成器传 `--importFileExtension=.js`（原生选项），生成 `.ts` 相对 import 说明符带 `.js`（包说明符不动），action 恢复 `ctx.actions.run`。验证：`bazel build //experimental/ts/grpc_hello_world:greeter_types //experimental/dsh/demo/agent:chat_types //projects/game/agent:game_types //experimental/grpc_chain/mid:echo_types`，抽查生成物相对导入已带 `.js`；4 个消费方 typecheck 仍绿（node10 解析对 `X.js`→`X.ts` 替换兼容）：`bazel test //experimental/ts/grpc_hello_world:server_typecheck_test //experimental/dsh/demo/agent:server_lib_typecheck_test //projects/game/agent:lib_typecheck_test //experimental/grpc_chain/mid:server_typecheck_test`。依据 research.md R4
- [ ] T003 [P] `third_party/dsh/core` 显式化 swc 配置：新建 `third_party/dsh/core/.swcrc`，内容 `{"jsc": {"parser": {"syntax": "typescript"}, "target": "es2020"}, "module": {"type": "es6", "preserveImportMeta": true}}`（该包当前无 `.swcrc`、swc 默认 `es6`——本任务把意外的 ESM 输出固化为显式契约，产物应无变化）。验证：`bazel build //third_party/dsh/core:version_lib` 并 diff `bazel-bin/third_party/dsh/core/version.js` 与改动前一致（仍为 `export var ...`）。依据 research.md R2

**Checkpoint**: 基建就绪（宏改动全绿、生成物带扩展名、dsh-core 配置显式化），库/服务翻转可开始。**SC-002 基线**：本 phase 末执行 `bazel test //common/... //projects/... //experimental/...` 并记录 vitest 用例总数，作为"重构前基线"（T021 用例数对比依据）。

---

## Phase 3: User Story 2 - 公共库以 ESM 交付并被工作区消费 (Priority: P2)

**Goal**: 8 个库/CLI/钉扎包翻转为 ESM，自身单测全绿、API 集合不变，为服务翻转铺平消费链路

**Independent Test**: 各包 `bazel build`+`bazel test` 全绿；产物为 ESM 语法（`export`/`import` 字面量，无 `Object.defineProperty(exports,...)`）；未翻转的 CJS 服务消费方经 require(esm) 仍可加载（Node 24 兜底）——即 Phase 3 结束时全仓 `bazel test //common/... //experimental/... //projects/...` 仍全绿

**本 phase 文档清单**（编码前必读）：

- **代码规范文档**：
  - `style/javascript.md`（caveat 同 Phase 2；测试执行模型/mock 约定/`vitest_test` 宏 data 规则全部继续有效）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：
  - [TypeScript Handbook: Module Resolution & NodeNext](https://www.typescriptlang.org/docs/handbook/modules/reference.html)（相对导入扩展名、`paths`、最近 package.json 判定）
  - [Node: package.json `type` 字段](https://nodejs.org/api/packages.html#type)
  - [Node: import.meta.dirname](https://nodejs.org/api/esm.html#importmetadirname)
  - [SWC Module 编译配置](https://swc.rs/docs/configuration/modules)
  - （T006/T008）[OpenTelemetry JS ESM 支持](https://github.com/open-telemetry/opentelemetry-js/blob/main/doc/esm-support.md)
  - （T006）[Node: module.register()](https://nodejs.org/api/module.html#moduleregisterspecifier-parenturl)
  - （T008）[Node: module.createRequire()](https://nodejs.org/api/module.html#modulecreaterequirefilename)
  - [vitest#5999（NodeNext 风格 `.js` 导入在 vitest 的解析）](https://github.com/vitest-dev/vitest/issues/5999)（标准翻转清单中测试内相对导入补 `.js` 的运行时解析，research R7 版本敏感性点）
- **技术文章/技术参考文档**：
  - [specs/048-js-esm-migration/research.md](../research.md)（R1/R2/R3/R5/R7/R8/R9）
  - [specs/048-js-esm-migration/contracts/esm-package-conventions.md](../contracts/esm-package-conventions.md)
  - [specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md](../contracts/otel-instrumentation-esm-contract.md)（T006/T008 必读：hook 注册与 createRequire 豁免条款）
  - [specs/048-js-esm-migration/data-model.md](../data-model.md)（§2 波 1 表：各包改写点）
  - [specs/048-js-esm-migration/quickstart.md](../quickstart.md)（场景 1/2/7——场景 2 仅 saolei-board CLI 部分适用，hello_world 属 Phase 4）
  - （T008）`specs/019-js-test-reliability/research.md`（RITM/dual-instance 背景）
  - （T008）`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（shim 契约不变项）

- [ ] T004 [P] [US2] 翻转 `common/js/resolver`（标准翻转清单；包内 8 模块+测试相对导入补 `.js`；无工作区依赖）。验证：`bazel build //common/js/resolver/... && bazel test //common/js/resolver:lib_test`，抽查 `bazel-bin/common/js/resolver/src/index.js` 为 ESM
- [ ] T005 [P] [US2] 翻转 `common/js/config`（标准翻转清单；测试在 `test/` 目录，`test/config.test.ts` 对 `../src/*` 的相对导入补 `.js`；js-yaml 经其 `exports.import` 自动切 ESM 构建）。验证：`bazel test //common/js/config:lib_test`
- [ ] T006 [P] [US2] 翻转 `common/js/otel` 并在 `init()` 注册 IITM hook：`common/js/otel/src/index.ts` 增加 `import { register } from "node:module"`，`init()` 首次调用时（模块级 flag 幂等）执行 `register("@opentelemetry/instrumentation/hook.mjs", import.meta.url)`——`@opentelemetry/instrumentation` 为本包直接依赖（package.json），解析可靠；时序语义与禁止事项见 otel 契约 §2/§5。其余按标准翻转清单。验证：`bazel test //common/js/otel:lib_test`；**冒烟检查** `bazel-bin/common/js/otel/src/index.js` 中 `import.meta` 原样保留（swc `preserveImportMeta` 版本敏感性点，research.md 版本敏感性清单）
- [ ] T007 [P] [US2] 翻转 `common/js/logs`（标准翻转清单；`src/context.ts`/`logger.ts`/`reporter.ts` 及测试相对导入补 `.js`）。验证：`bazel test //common/js/logs:lib_test`
- [ ] T008 [US2] 翻转 `common/js/grpc/otel` 并改写 RITM 验证测试：`common/js/grpc/otel/src/index.test.ts` 的 `require("@grpc/grpc-js")`/`require("@grpc/proto-loader")`（:32-33）改为 `import { createRequire } from "node:module"; const require = createRequire(import.meta.url)` 形式（仍在 `registerInstrumentations()` 之后调用，验证语义不变）；`:38` 的 `__dirname` → `import.meta.dirname`；其余按标准翻转清单（本包 tsconfig 无 paths）。验证：`bazel test //common/js/grpc/otel:lib_test`（RITM patch 生效断言通过）
- [ ] T009 [US2] 翻转 `common/js/grpc/resolver`（标准翻转清单 + tsconfig `paths` 的 `@dominion/common-js-resolver` 值 `../../resolver/src` → `../../resolver/src/index.js`；依赖 T004/T007 已翻转）。验证：`bazel test //common/js/grpc/resolver:lib_test`
- [ ] T010 [P] [US2] 翻转 `projects/game/pkg/saolei-board`（标准翻转清单；`src/core/golden.test.ts:22` 的 `__dirname` → `import.meta.dirname`；bin `saolei-recognize` 经包 `"type": "module"` 自动 ESM 化——`js_library pkg` 已含 package.json（`BUILD.bazel:43`），runfiles 判定已具备）。验证：`bazel test //projects/game/pkg/saolei-board:lib_test && bazel run //projects/game/pkg/saolei-board:cli -- --help`（CLI ESM 入口可执行）
- [ ] T011 [P] [US2] 翻转 `third_party/dsh/core`（package.json + `"type": "module"`；tsconfig `module` → `nodenext`；`.swcrc` 已于 T003 落地；单文件 `version.ts` 无相对导入）。验证：`bazel build //third_party/dsh/core/...`；`bazel test //experimental/dsh/demo/testplan:closure_audit_test` 仍绿（闭包审计不受模块格式影响）

**Checkpoint**: 全部库/CLI/钉扎包为 ESM；全仓 `bazel test //common/... //experimental/... //projects/game/pkg/...` 全绿（CJS 服务消费方经 require(esm) 兜底）

---

## Phase 4: User Story 1 - 服务以 ESM 形态构建与运行 (Priority: P1) 🎯

**Goal**: 7 个服务包翻转为 ESM 并以 ESM 产物构建/打包/运行，服务对外行为不变

**Independent Test**: 各服务 `bazel build`+`bazel test` 全绿；服务 tar 含 `"type": "module"` 的服务根 package.json（T001 宏默认）；grpc_hello_world 烟测（ESM 化后）通过；hello_world/saolei-board CLI 可 `bazel run`。（大型部署级验收在 Phase 6）

**本 phase 文档清单**（编码前必读）：

- **代码规范文档**：
  - `style/javascript.md`（caveat 同 Phase 2）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：
  - [TypeScript Handbook: Module Resolution & NodeNext](https://www.typescriptlang.org/docs/handbook/modules/reference.html)
  - [Node: package.json `type` 字段](https://nodejs.org/api/packages.html#type)
  - [Node: import.meta.dirname](https://nodejs.org/api/esm.html#importmetadirname)
  - [vitest#5999（NodeNext 风格 `.js` 导入在 vitest 的解析）](https://github.com/vitest-dev/vitest/issues/5999)
- **技术文章/技术参考文档**：
  - [specs/048-js-esm-migration/research.md](../research.md)（R1/R5/R6/R8/R9）
  - [specs/048-js-esm-migration/contracts/esm-package-conventions.md](../contracts/esm-package-conventions.md)（§3 CJS 依赖导入约定、§4 打包）
  - [specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md](../contracts/otel-instrumentation-esm-contract.md)（§2 bootstrap 两段式必须保持）
  - [specs/048-js-esm-migration/data-model.md](../data-model.md)（§2 波 2 表：各服务改写点）
  - [specs/048-js-esm-migration/quickstart.md](../quickstart.md)（场景 2/3）
  - （T017）`specs/047-dsh-chat-demo/research.md`（D8/D10：require(esm) 与 `__filename` 锚点决策——本任务将其终态化）

- [ ] T012 [US1] 翻转 `experimental/ts/hello_world`（标准翻转清单；`experimental/ts/hello_world/BUILD.bazel` 的 `js_binary` `data = [":lib"]` → `data = [":lib", "package.json"]`，使 runfiles 内最近 package.json 判定 ESM——rules_js 机制见 research.md R6）。验证：`bazel build //experimental/ts/hello_world:... && bazel run //experimental/ts/hello_world:run`（ESM 入口运行成功）
- [ ] T013 [US1] 翻转 `experimental/ts/grpc_hello_world` 并 ESM 化烟测：标准翻转清单（`src/server.ts:29-30` `__dirname` → `import.meta.dirname`；tsconfig paths 4 项 → `src/index.js`）；`experimental/ts/grpc_hello_world/smoke_test.sh` 适配——`node -e` 内 `require(...)` 改为动态 `import(...)`（或 `--input-type=module`），"Test 3 bootstrap 可解析"断言 `includes('require')` → `includes('import')`，**新增断言**：tar 内 `dominion/grpc-hello-world-ts/service/package.json` 存在且含 `"type": "module"`。验证：`bazel test //experimental/ts/grpc_hello_world:smoke_test //experimental/ts/grpc_hello_world:server_typecheck_test`
- [ ] T014 [P] [US1] 翻转 `experimental/grpc_chain/mid`（标准翻转清单；`src/server.ts:20` `__dirname` → `import.meta.dirname`；tsconfig paths 4 项 → `src/index.js`）。验证：`bazel build //experimental/grpc_chain/mid/... && bazel test //experimental/grpc_chain/mid:server_typecheck_test`；解包 `server_pkg.tar` 确认服务根 package.json 含 `"type": "module"`
- [ ] T015 [P] [US1] 翻转 `experimental/openai_llm/client`（标准翻转清单；无 `__dirname`；tsconfig paths 3 项 → `src/index.js`；langchain ESM 依赖自此走原生 import）。验证：`bazel build //experimental/openai_llm/client/... && bazel test //experimental/openai_llm/client:server_typecheck_test`
- [ ] T016 [P] [US1] 翻转 `experimental/ts/team_graph_spike`（标准翻转清单；相对导入已带 `.js` 仅需查漏；tsconfig paths 3 项 → `src/index.js`）。验证：`bazel test //experimental/ts/team_graph_spike:lib_test //experimental/ts/team_graph_spike:lib_typecheck_test`
- [ ] T017 [P] [US1] 翻转 `experimental/dsh/demo/agent`：标准翻转清单（`src/dsh.ts:49` `path.dirname(__filename)` → `import.meta.dirname`、`:88` `pathToFileURL(__filename).href` → `import.meta.url` 并移除 `:14` 的 `pathToFileURL` 导入、`src/server.ts:24` 同理；tsconfig paths 5 项 → `src/index.js`）；**注释终态化**（宪章原则 VII）：`src/dsh.ts:81` 的"CJS 等价物"注释改写（`experimental/dsh/demo/agent/BUILD.bazel` 的模块格式注释已随 Phase 2 返工终态化，本任务验证步骤中顺带目检其仍为终态即可）。验证：`bazel test //experimental/dsh/demo/agent:lib_test //experimental/dsh/demo/agent:server_lib_typecheck_test && bazel build //experimental/dsh/demo/agent:server_pkg`
- [ ] T018 [P] [US1] 翻转 `projects/game/agent`：标准翻转清单（`src/server.ts:64`、`src/prompt-client.ts:29-33,44`、`src/skill-loader.ts:39` 的 `__dirname` → `import.meta.dirname`，`skill-loader.ts:12-13,29-32` 提及 commonjs 的注释改写为终态；`src/skill-loader.test.ts:40,48,59,67` 测试内 `__dirname` 同步改写；tsconfig paths 5 项 → `src/index.js`；`@modelcontextprotocol/sdk` 子路径导入已带 `.js` 无需改；express 按 R5 约定 default import）。验证：`bazel test //projects/game/agent:lib_test //projects/game/agent:lib_typecheck_test && bazel build //projects/game/agent:server_pkg //projects/game/agent:server_pkg_test`，解包确认双入口 tar 服务根均含 `"type": "module"`

**Checkpoint**: 全部服务为 ESM 产物；bootstrap 两段式（OTel 装配 → 动态 import server）形态未变，otel 契约 §2 时序满足

---

## Phase 5: User Story 3 - 统一的模块规范与开发体验 (Priority: P3)

**Goal**: 规范文档与构建门禁固化 ESM 为仓库标准：`style/javascript.md` 描述终态、内容门禁使 CJS 服务产物不可构建、静态审计零 CJS 残留

**Independent Test**: quickstart.md 场景 4/5 全部通过（全仓 build/test 全绿 + 审计 rg 零命中）；内容门禁负向验证（移除 `"type"` → 构建失败 → 还原）

**本 phase 文档清单**（编码前必读）：

- **代码规范文档**：
  - `style/javascript.md`（T019 的重写对象——先读现文，保留测试执行模型/mock 约定/`vitest_test` 宏规则等仍有效章节）
  - `style/large_test.md`（本 phase 不执行测试计划，但 T021 审计范围与验收口径与 Phase 6 衔接）
- **官方文档**：
  - [Vitest: Mocking modules pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（`style/javascript.md` mock 约定章节引用的官方文档；T019 保留该章节时必读）
- **技术文章/技术参考文档**：
  - [specs/048-js-esm-migration/research.md](../research.md)（R10 重写范围、R5 导入约定、版本敏感性清单）
  - [specs/048-js-esm-migration/contracts/esm-package-conventions.md](../contracts/esm-package-conventions.md)（§3/§6：规范文档的目标内容与审计判据）
  - [specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md](../contracts/otel-instrumentation-esm-contract.md)（§3：createRequire 豁免登记）
  - [specs/048-js-esm-migration/quickstart.md](../quickstart.md)（场景 4/5、场景 3 的终态门禁验证步骤）
  - [specs/048-js-esm-migration/data-model.md](../data-model.md)（§4 验收态审计查询）

- [ ] T019 [US3] 重写 `style/javascript.md` 模块规范（research.md R10）："生产代码由 swc 编译为 CJS"表述 → ESM 终态（swc `es6`+`preserveImportMeta`、NodeNext 扩展名规则、`import.meta.dirname/url` 资源定位约定）；"require() for RITM"节收窄为唯一豁免（`createRequire(import.meta.url)`，引用 otel 契约 §3）；新增：CJS 依赖 default-import 约定（R5 表）、新增插桩 CJS 包时经 `common/js/otel` 扩展的处理路径；测试执行模型/mock 约定/`vitest_test` 宏规则章节保留。验证：文档描述与 Phase 3/4 实际构建行为逐条一致（人工比对）
- [ ] T020 [US3] 启用 `artifact_pkg_js` 内容门禁：修改 `tools/release/defs.bzl`——打包 action 内断言 manifest 含 `"type": "module"`（如 `grep -q '"type": "module"'`，失败即 action 失败，报错信息注明"ESM-only 构建，CJS 服务产物不再支持"，research.md R6 内容门禁条款）。验证：7 个服务 target 构建全绿；**负向验证**（quickstart 场景 3 终态门禁步骤）：临时移除 `experimental/ts/grpc_hello_world/package.json` 的 `"type": "module"` → `bazel build //experimental/ts/grpc_hello_world:server_pkg` MUST 失败 → 还原后重新构建通过
- [ ] T021 [US3] 静态审计与全仓验证（quickstart 场景 4/5）：`bazel build //...`、`bazel test //common/... //projects/... //experimental/...` 全绿（含前端包回归，FR-002）；**用例数对比（SC-002）**：vitest 用例总数不少于 Phase 2 Checkpoint 记录的重构前基线；**依赖版本冻结（FR-006）**：`git diff pnpm-workspace.yaml pnpm-lock.yaml` 零变更；静态审计按 [contracts/esm-package-conventions.md](../contracts/esm-package-conventions.md) §6 权威命令集执行，全部零命中——CJS 编译配置残留（tsconfig `"module": "commonjs"`、`.swcrc` `"type": "commonjs"`）、`__dirname`/`__filename`/`module.exports`、`require(` 直用（唯一豁免 `common/js/grpc/otel/src/index.test.ts` 的 createRequire 场景）、workspace 包 `package.json` 缺 `"type": "module"`（唯一例外：仓库根 package.json）

**Checkpoint**: ESM 成为仓库规范并被构建系统强制；SC-001/SC-002/SC-004 达成

---

## Phase 6: Polish & Cross-Cutting Concerns（大型测试验收）

**Purpose**: 服务型应用的部署级验收（宪章原则 VI：MUST 经 testplan skill 实际执行完整部署→测试→清理闭环，全部用例通过；仅构建检查不构成验收）

**本 phase 文档清单**（执行前必读）：

- **代码规范文档**：
  - `style/large_test.md`（测试计划结构与 guitar 执行规范；其引用的 `style/golang.md` 仅在编写测试代码时需要——本 phase 只执行既有计划，不新增用例）
- **官方文档**：无
- **技术文章/技术参考文档**：
  - `.opencode/skills/testplan/SKILL.md`（guitar 执行入口与操作步骤）
  - [specs/048-js-esm-migration/quickstart.md](../quickstart.md)（场景 6）
  - [specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md](../contracts/otel-instrumentation-esm-contract.md)（§4 遥测等价验收判据）
  - `projects/game/testplan/README.md`（system_test.yaml 计划说明）
  - `README.md`（T024 豁免登记对象）

- [ ] T022 [P] 通过 testplan skill 执行 game agent 大型测试：`guitar validate projects/game/testplan/system_test.yaml && guitar run projects/game/testplan/system_test.yaml`（完整部署→测试→清理闭环）。验收：全部用例通过（失败/flaky 即验收未过，修复后重跑）；gRPC 调用 trace 断言确认 server span 正常产出（IITM hook 生产路径等价，SC-005；若 IITM 路径被证伪，按 otel 契约 §4 回退方案处置并记录）
- [ ] T023 [P] 通过 testplan skill 执行 dsh demo 大型测试与闭包审计：`guitar validate experimental/dsh/demo/testplan/interface_test.yaml && guitar run experimental/dsh/demo/testplan/interface_test.yaml`（全用例通过），并重跑 `bazel test //experimental/dsh/demo/testplan:closure_audit_test //experimental/dsh/demo/testplan:multiturn_test`（部署形态变化后的闭包完整性）
- [ ] T024 [P] 按宪章原则 VI 豁免条款在仓库 README 登记 JS 服务大型测试豁免：`experimental/ts/hello_world`、`experimental/grpc_chain/mid`、`experimental/openai_llm/client`、`experimental/ts/team_graph_spike` 无既有 testplan，且 FR-011 约束本特性不新增部署面；这些服务的验证以构建+产物断言（tar 内服务根 package.json）+既有单测覆盖（hello_world 另经 `bazel run` 冒烟）；豁免理由与覆盖说明写入 README。验证：README 增补内容与本任务描述一致（人工比对）

**Checkpoint**: SC-003/SC-005 达成、README 豁免登记完成（原则 VI）——特性验收完成

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: 无任务
- **Phase 2 (Foundational)**: T001/T002 串行无依赖可并行推进（T003 [P]）——**阻塞所有用户故事**
- **Phase 3 (US2)**: 依赖 Phase 2；T004–T007 互不依赖（零工作区依赖包，[P] 并行）；T008 在 T006 后（R9 保守排序）；T009 依赖 T004+T007；T010/T011 [P]
- **Phase 4 (US1)**: 依赖 Phase 3 **全部完成**（R9 库先行：nodenext 服务消费库源码要求库已 ESM 判定）；T012 → T013（先用最简服务验证翻转模式，再验证 tar+烟测链路）；T014–T018 [P] 并行（各自独立包，库依赖已就绪）
- **Phase 5 (US3)**: 依赖 Phase 4 全部完成（T020 内容门禁在最后一个服务翻转后启用，否则打断中间态构建；T021 审计要求全部翻转完成）
- **Phase 6 (验收)**: 依赖 Phase 5（T020 内容门禁先行验证）；T022/T023/T024 可并行

### User Story Dependencies

- **US2 (P2)**: 实施上最先——它是 US1 的技术前置（不是价值优先级低）；独立可交付（CJS 服务消费方经 require(esm) 兜底）
- **US1 (P1)**: 价值最高、验收核心；依赖 US2 完成
- **US3 (P3)**: 依赖 US1/US2 全部完成（规范描述终态、门禁强制终态）
- 优先级与实施顺序的倒置是本特性依赖结构所致（同一重构的三个层次切片），非优先级重排

### Parallel Opportunities

- Phase 2: T003 与 T001/T002 并行
- Phase 3: T004/T005/T006/T007 四包并行；T010/T011 并行
- Phase 4: T014–T018 五个服务并行（不同包目录，无文件冲突）
- Phase 6: T022、T023 与 T024 并行

---

## Implementation Strategy

### MVP First

1. Phase 2 基建（3 任务）
2. Phase 3 US2 全部（8 包）
3. Phase 4 的 T012+T013（最简服务 + tar/烟测链路验证翻转模式）
4. **STOP and VALIDATE**: 全仓测试 + grpc_hello_world 烟测绿 → 翻转模式已被证明，剩余服务为模式复制

### Incremental Delivery

1. 基建 → 库波（US2 checkpoint：全仓仍绿）→ 服务波（US1 checkpoint）→ 规范与门禁（US3 checkpoint）→ 大型测试验收
2. 每个 checkpoint 均可中断/恢复：每包翻转是原子变更（标准翻转清单），包间通过 require(esm) 兜底保持中间态可用

---

## Notes

- 每个翻转任务的 `bazel build`+`bazel test` 为任务内嵌验证（宪章原则 IV），不单列任务
- 禁止以删除/跳过测试达成切换（spec FR-003）；测试用例数量只增不减
- `tools/dev/js/vitest_test.bzl`、`tools/dev/js/run_vitest.mjs`、`projects/game/desktop/frontend`、Go/Python 项目零改动（FR-002/FR-012；前端由 T021 全仓测试回归覆盖）
- 所有新增/改写注释只表述 ESM 终态，不保留迁移过程表述（宪章原则 VII）
- 静态审计命令集唯一权威版本为 `contracts/esm-package-conventions.md` §6；quickstart/data-model/T021 均引用之而不复制命令，避免多处拷贝漂移
