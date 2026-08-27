# Quickstart: JS 项目全量切换 ESM 验证指南

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-24

端到端验证场景，证明重构后模块形态正确、行为等价。命令均从仓库根执行；构建入口一律 bazel（`AGENTS.md`）。

## 前置

- 环境：Node 工具链经 bazel（24.14.0，`MODULE.bazel:125`）；无需本地 node。
- 阅读契约：[contracts/esm-package-conventions.md](contracts/esm-package-conventions.md)、[contracts/otel-instrumentation-esm-contract.md](contracts/otel-instrumentation-esm-contract.md)。

## 场景 1 — 库包 ESM 产物与单测（波 1 后）

```bash
bazel build //common/js/... //projects/game/pkg/saolei-board:core //third_party/dsh/core:version_lib
bazel test //common/js/... //projects/game/pkg/saolei-board:lib_test
```

**预期**：构建/测试全绿。抽查产物为 ESM（无 `Object.defineProperty(exports,...)`，`export` 字面量）：

```bash
cat bazel-bin/common/js/config/src/index.js | head -5
```

**预期**：输出含 `export`/`import` 语法；若源码使用 `import.meta`，产物中保留（验证 swc `preserveImportMeta`）。

## 场景 2 — CLI 与 js_binary 入口（R6）

```bash
bazel run //projects/game/pkg/saolei-board:cli -- --help        # bin 入口 ESM 可执行
bazel run //experimental/ts/hello_world:run                    # js_binary ESM 入口可运行
```

**预期**：两者正常运行退出；若报 `require is not defined` 即 runfiles 缺 `package.json`（R6 判定失败）。

## 场景 3 — 服务 tar 模块判定与烟测（grpc_hello_world）

```bash
bazel build //experimental/ts/grpc_hello_world:server_pkg
bazel test //experimental/ts/grpc_hello_world:smoke_test
```

**预期**：烟测通过（tar 内服务根 `package.json` 存在且 `"type":"module"`；依赖经动态 `import` 加载成功）。手工抽查：

```bash
tar -tf bazel-bin/experimental/ts/grpc_hello_world/server_pkg.tar | grep -E 'package.json$'
tar -xf bazel-bin/experimental/ts/grpc_hello_world/server_pkg.tar -O dominion/grpc-hello-world-ts/service/package.json
```

**终态门禁验证**（Phase 5 内容门禁启用后）：临时移除该包 package.json 的 `"type": "module"` 再 `bazel build //experimental/ts/grpc_hello_world:server_pkg`，构建 MUST 失败（ESM-only，CJS 服务产物不可构建）；验证后还原。

## 场景 4 — 全仓构建与单测（SC-002）

```bash
bazel build //...
bazel test //common/... //projects/... //experimental/...
```

**预期**：全绿；用例数不少于重构前基线（tasks.md Phase 2 Checkpoint 记录，无 skip/删除，FR-003）。前端包回归包含在内（FR-002：`//projects/game/desktop/frontend:lib_test` 与 vite_build）。

## 场景 5 — 静态审计零 CJS 残留（SC-001/SC-004）

按 [contracts/esm-package-conventions.md §6](contracts/esm-package-conventions.md) 的权威命令集执行静态审计，全部零命中：

- CJS 编译配置残留（tsconfig `"module": "commonjs"`、`.swcrc` `"type": "commonjs"`）
- 源码 CJS 惯用法（`__dirname`/`__filename`/`module.exports`）
- `require(` 直用（生产源码；测试文件唯一豁免 `common/js/grpc/otel/src/index.test.ts` 的 createRequire 场景，见 [otel 契约 §3](contracts/otel-instrumentation-esm-contract.md)）
- workspace 包 `package.json` 缺 `"type": "module"`（唯一例外：仓库根 package.json，非 workspace 包且无 JS 源）

## 场景 6 — 大型测试（SC-003/SC-005，验收闭环）

按 `style/large_test.md` 与 testplan skill 执行（**必须完整部署→测试→清理闭环，全部用例通过**，原则 VI）：

```bash
guitar validate projects/game/testplan/system_test.yaml
guitar run projects/game/testplan/system_test.yaml

guitar validate experimental/dsh/demo/testplan/interface_test.yaml
guitar run experimental/dsh/demo/testplan/interface_test.yaml
```

**预期**：两计划全部用例通过；trace 断言证明 gRPC server span 正常产出（插桩等价，SC-005）。闭包审计（closure_audit_test）随 dsh 计划执行或单跑 `bazel test //experimental/dsh/demo/testplan:closure_audit_test`。

## 场景 7 — 插桩时序单元验证（局部）

```bash
bazel test //common/js/grpc/otel:lib_test
```

**预期**：`index.test.ts`（createRequire 形式）通过——RITM 装配 wiring 在 ESM 包下保持等价（生产 IITM 路径由场景 6 覆盖）。

## 排查指引

- 服务启动即 `SyntaxError: Cannot use import statement outside a module` → 服务根/tar 缺 `"type":"module"` 的 package.json（R6）。
- `ERR_MODULE_NOT_FOUND`（相对路径）→ 源码存在未补 `.js` 的相对导入（R1）。
- gRPC 调用无 trace → 违反 [otel 契约 §2](contracts/otel-instrumentation-esm-contract.md) 时序（hook 注册晚于 grpc-js 加载），对照契约 §5 排查。
