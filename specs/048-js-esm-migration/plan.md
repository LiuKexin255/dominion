# Implementation Plan: JS 项目全量切换 ESM

**Branch**: `048-js-esm-migration` | **Date**: 2026-08-24 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/048-js-esm-migration/spec.md`

## Summary

将 pnpm workspace 内 15 个 CJS 包全量切换为原生 ESM（`"type": "module"` + NodeNext 书写约定 + swc ESM 发射），前端包（已 ESM）保持不变；约束为服务对外行为零变化（接口契约/日志/trace 等价）、依赖版本集合不变。技术路线（详见 [research.md](research.md)）：

- **R1/R2**: tsconfig `module: nodenext`（相对导入补 `.js`）+ `.swcrc` `es6`+`preserveImportMeta`，两文件锁步。
- **R3**: OTel gRPC 插桩经 `common/js/otel` 的 `init()` 注册 IITM loader hook（`module.register("@opentelemetry/instrumentation/hook.mjs")`，幂等）——RITM 对 ESM→CJS 导入不可见，此为插桩等价的关键收敛点；测试侧保留 createRequire 验证路径。
- **R4**: `ts_proto_library` 生成物相对导入补 `.js` 后处理（生成代码为纯 `import type`，运行时无影响）。
- **R6**: `artifact_pkg_js` 新增 `package_json` 属性（label 类型，默认 `"package.json"`、不可关闭，7 个服务 target 零改动自动生效）+ 两道构建期门禁：存在性（缺文件分析期失败，Phase 2 起生效）与内容（断言 `"type": "module"`，Phase 5 启用——此后 CJS 服务 tar 无法构建）；js_binary 面 runfiles 内保证最近 package.json 判定；smoke 测试 ESM 化并断言服务根 package.json。
- **R9**: 库先行（拓扑序）、每包原子翻转，中间态由 Node 24 `require(esm)` 兜底，终态该桥接退役。

契约（原则 III）：[contracts/esm-package-conventions.md](contracts/esm-package-conventions.md)（包级 ESM 规范与审计判据）、[contracts/otel-instrumentation-esm-contract.md](contracts/otel-instrumentation-esm-contract.md)（插桩时序与遥测等价验收）。全景清单（[data-model.md](data-model.md) §2）；验证步骤（[quickstart.md](quickstart.md)）。

## Technical Context

**Language/Version**: TypeScript 6.0.2（源码），Node 24.14.0（运行时，distroless nodejs24-debian12 镜像；`MODULE.bazel:117-126`）

**Primary Dependencies**: aspect_rules_ts 3.8.8、aspect_rules_swc 2.7.1、aspect_rules_js 3.0.3、rules_nodejs 6.7.4（Bazel 构建链）；vitest 3.2.7（单测）；@grpc/grpc-js 1.14.4、@grpc/proto-loader 0.8.1、@opentelemetry/* 1.9/0.218/2.7、langchain 家族、@modelcontextprotocol/sdk、@deepseek-ai/dsh-*（0.1.1-rc.2 钉扎）——**版本集合冻结，不随本特性变更**

**Storage**: N/A（无存储变更）

**Testing**: vitest（`tools/dev/js/vitest_test.bzl` → js_test，直跑 raw `.ts` 源）；sh_test 烟测（grpc_hello_world）；go_largetest 大型测试经 guitar（`projects/game/testplan/system_test.yaml`、`experimental/dsh/demo/testplan/interface_test.yaml`）

**Target Platform**: linux/amd64 容器（Node ESM 运行时）；前端为 Wails webview（不变）

**Project Type**: monorepo——8 库/CLI/钉扎包 + 7 服务包 + 1 前端包（pnpm workspace）

**Performance Goals**: N/A（重构以行为等价为约束；不引入可感知的启动/运行时退化）

**Constraints**: 不改变服务对外行为（FR-011）；不改依赖版本（FR-006）；Go/Python 项目零影响（FR-012）；bazel 为唯一构建入口；非 JS 项目不受影响。

**Scale/Scope**: 15 包翻转；约 11 处 `__dirname`/`__filename` 改写 + 1 处 `require()` 测试改写 + 全量相对导入补扩展名；2 个 Bazel 宏内点状调整；1 份规范文档重写。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.（注：此处 Phase 0/1 为 plan 工作流的调研/设计阶段，与 tasks.md 的实施 Phase 1-6 编号无关）*

| 原则 | 状态 | 说明 |
|---|---|---|
| I. 引用溯源 | ✅ | [research.md](research.md) 全部决策附仓库内路径与外部 URL；契约/数据模型同 |
| II. 重构式变更 | ✅ | 本特性即设计层重构：统一两套模块机制（消除 require(esm) 桥接与 CJS/ESM 并存），插桩注册收敛 `init()` 单点而非散布各 bootstrap；无补丁式叠加 |
| III. 接口优先 | ✅ | 两份契约先于实现：包级 ESM 规范（审计判据）+ 插桩时序契约（含验收与回退条款） |
| IV. 测试颗粒度 | ✅ | 每包翻转内嵌 `bazel build`+`bazel test`（不单列 task）；大型测试单列验收 task（guitar 完整闭环） |
| V. 编码前阅读文档 | ✅ | 本 plan 声明必读集（见下）；tasks.md 将按 phase 显式列出三分类文档清单 |
| VI. 服务型应用大型测试验收 | ✅ | game agent 与 dsh demo 两个 guitar 计划实际执行（部署→测试→清理，全用例通过）为验收 task；grpc_hello_world 以 sh_test 烟测 + tar 断言覆盖；其余 4 个 experimental 服务（hello_world、grpc_chain/mid、openai_llm/client、team_graph_spike）无既有 testplan，按原则 VI 豁免条款在 README 登记豁免理由（tasks T024） |
| VII. 终态表述 | ✅ | 迁移即清除 CJS 前提的注释/文档（`experimental/dsh/demo/agent/BUILD.bazel:57-62`、`src/dsh.ts:81`、`skill-loader.ts` 注释、`style/javascript.md`），一律改写为 ESM 终态理由；不留"迁移中"痕迹 |

**设计阶段复查（plan 工作流 Phase 1）**: 设计产物（research/data-model/contracts/quickstart）与上表逐项复核无新违规。原则 VI 补充说明：game agent 与 dsh demo 两个 testplan 执行为强制验收门；其余 4 个 experimental 服务无既有 testplan，按原则 VI 豁免条款在 README 登记豁免理由（tasks T024）。

**下游必读文档集**（设计期汇总；编码时以 tasks.md 各 phase 三分类清单为准，不一致时以 tasks.md 为准）：
- 契约：`specs/048-js-esm-migration/contracts/esm-package-conventions.md`、`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`
- 依据：`specs/048-js-esm-migration/research.md`、`specs/048-js-esm-migration/data-model.md`
- 上游背景：`specs/019-js-test-reliability/research.md`（双实例问题与 Fix C 否决）、`specs/047-dsh-chat-demo/research.md` D8/D10（require(esm) 与 __filename 锚点——本次终态化对象）
- 规范：`style/javascript.md`（重写对象）、`style/large_test.md`（验收 task 必读）
- 外部关键参考（正文已引）：TypeScript Handbook modules/reference（https://www.typescriptlang.org/docs/handbook/modules/reference.html ）、OTel esm-support（https://github.com/open-telemetry/opentelemetry-js/blob/main/doc/esm-support.md ）、swc modules 配置（https://swc.rs/docs/configuration/modules ）、Node packages#type（https://nodejs.org/api/packages.html#type ）

## Project Structure

### Documentation (this feature)

```text
specs/048-js-esm-migration/
├── plan.md              # 本文件
├── research.md          # 技术决策 R1-R10
├── data-model.md        # 包依赖图与迁移状态模型
├── quickstart.md        # 端到端验证场景
├── contracts/
│   ├── esm-package-conventions.md          # 包级 ESM 契约（审计判据）
│   └── otel-instrumentation-esm-contract.md # 插桩时序与遥测等价契约
└── tasks.md             # /speckit.tasks 产出（待生成）
```

### Source Code (repository root)

结构不变（原地重构，原则 II）；触及面：

```text
common/js/{config,otel,resolver,logs}/      # 波1 库包：package.json/tsconfig/.swcrc/源码扩展名
common/js/grpc/{otel,resolver}/             # 波1：+ createRequire 测试改写、paths 适配、init() hook 注册
projects/game/pkg/saolei-board/             # 波1：lib+cli（golden 测试 __dirname、bin ESM）
third_party/dsh/core/                       # 波1：新增 .swcrc（固化既有 ESM 产物）
experimental/ts/{hello_world,grpc_hello_world,team_graph_spike}/   # 波2 服务
experimental/{grpc_chain/mid,openai_llm/client}/                   # 波2 服务
experimental/dsh/demo/agent/                # 波2：__filename 锚点简化、注释终态化
projects/game/agent/                        # 波2：__dirname×6、双入口 bootstrap
tools/dev/js/ts_proto_library.bzl           # 生成物 .js 扩展名后处理（R4）
experimental/ts/grpc_hello_world/smoke_test.sh  # ESM 化（R6）
tools/release/defs.bzl                       # artifact_pkg_js 新增 package_json 属性（R6）
tools/dev/js/{vitest_test.bzl,run_vitest.mjs}   # 零改动（R7）
projects/game/desktop/frontend/             # 零改动（已 ESM，回归验证）
style/javascript.md                         # 波3：模块规范重写
```

**Structure Decision**: 保持既有 monorepo 布局与构建 target 结构，不新增目录/包；变更全部落在包内配置、源码惯用法与两个宏的点状调整。`data-model.md` §2 的包清单即完整触及面清单。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规需豁免。（说明：`.swcrc`×15 与 tsconfig×15 的"重复"是 aspect_rules_swc 每包发现机制（`tools/dev/js` 各 BUILD `load @aspect_rules_swc` + 包根 `.swcrc`）的既定形态，非本特性引入；两文件锁步约束已写入契约 §2 作为审计项。）

## 实施波次（编号与 tasks.md 对齐；供 /speckit.tasks 参考）

1. **Phase 2 — 基建**: `artifact_pkg_js` 新增 `package_json` 属性与存在性门禁（`tools/release/defs.bzl`；落地时服务尚为 CJS、type-less package.json 行为中性，存在性门禁立即生效且全绿）、`ts_proto_library.bzl` 后处理、smoke_test.sh ESM 化、dsh-core `.swcrc`。（先行动基面，波 1/2 依赖）
2. **Phase 3 — 基础库波**: 按 R9 拓扑序 8 包翻转，含 `common/js/otel` init() hook（R3）与 grpc/otel 测试 createRequire 改写；每包门禁 = 该包 build+test（+下游消费方 typecheck）。
3. **Phase 4 — 服务波**: 7 服务翻转（js_binary runfiles/双入口/注释终态化；服务 tar 的 package.json 经宏默认自动生效）；每包门禁 = build + test + （grpc_hello_world）smoke_test。
4. **Phase 5/6 — 收尾与验收**: 启用 `package_json` 内容门禁（打包 action 断言 `"type": "module"`，CJS 服务 tar 自此无法构建）、`style/javascript.md` 重写、注释终态化、全仓 build/test、静态审计（quickstart 场景 5）、前端回归、两个 guitar 大型测试（场景 6，全部用例通过 = 验收）、README 大型测试豁免登记（原则 VI，tasks T024）。

各波验证步骤见 [quickstart.md](quickstart.md)；回退与风险条款见 [contracts/otel-instrumentation-esm-contract.md](contracts/otel-instrumentation-esm-contract.md) §4。
