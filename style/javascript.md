# javascript/typescript 规范

## 基准

* 以 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html) 作为本仓库 js/ts 规范基准。

> 如果本文档内容与上述引用冲突，则以本文档为优先。

## 测试 (Testing)

本节规定本仓库 TypeScript 测试的执行模型与 mock 约定（FR-009）。完整背景见
`specs/019-js-test-reliability/research.md`（§2 为 mock 根因，§3 为 runner 机制）。

### js_test 执行模型

本仓库所有 `*.test.ts` 通过 Bazel `js_test` target 执行，存在**两种执行模式**，二者对
模块拦截（module mocking）的行为不同：

| 模式 | 入口 | 被测代码形态 | `vi.mock()` 是否拦截 |
|------|------|-------------|---------------------|
| **Bazel `js_test`**（CI/`bazel test`） | `vitest_test` 宏 → `tools/dev/js/run_vitest.mjs` | **预编译 `:lib`**（`ts_project` + `swc` 产物，已 emit 为 `import`/`require`） | **否**——预编译产物的 import 直接从 runfiles `node_modules` 解析，绕过 vitest 的 mock registry |
| **vitest CLI**（`transpile-on-the-fly`） | 直接 `vitest run <file>` | **源 `.ts`**（由 vitest 的 Vite pipeline 现场转译） | **是**——vitest 将静态 `import` 改写为 `__handle_mock__` 包装，在模块加载前注册 mock |

根因（[vitest Mocking guide](https://vitest.dev/guide/mocking/modules#how-it-works)）：vitest
仅对**经过自身 Vite pipeline 转译的文件**应用 hoisting transform 以注册 mock；Bazel `js_test`
把预编译 `:lib` 当作 `data` 直接消费，其 import 在进入 vitest 之前就已解析完毕，故
`vi.mock` 失效（详见 `specs/019-js-test-reliability/research.md` §2）。vitest 官方亦明确：
`vi.mock` 仅拦截 `import`、不拦截 `require`；且"使应用可测试是应用架构的职责，而非测试
运行器的职责——应使用依赖注入"（[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)）。

### 受支持的使用面：`vitest_test` 宏 + 共享 shim

- **每个包的 `:lib_test` 必须通过 `tools/dev/js/vitest_test.bzl` 的 `vitest_test` 宏声明**，
  调用方只需传入被测 `:lib` 与测试文件 glob（+ 必要的 `node_modules/*` 依赖）：
  ```starlark
  load("//tools/dev/js:vitest_test.bzl", "vitest_test")
  vitest_test(
      name = "lib_test",
      data = [":lib"] + glob(["src/**/*.test.ts"], allow_empty = True),
  )
  ```
  宏内部 `genrule` 把唯一的 canonical shim `tools/dev/js/run_vitest.mjs` 拷贝进当前包，
  并自动注入 `:node_modules/vitest` 与本地 `entry_point`——这些是**宏内部细节，禁止在
  包级 `BUILD.bazel` 中手写裸 `genrule` / `entry_point` / `:node_modules/vitest`**。
- **shim 的退出码契约**（vitest 结果 → Bazel pass/fail）定义在
  `specs/019-js-test-reliability/contracts/run-vitest-shim.md`：失败 fail-closed（读取不到
  失败计数即视为失败，退出 1），空套件退出 0（vacuous pass）。该契约是本特性的核心接口
  （Constitution §III）。

### 可靠模式 vs 脆弱模式（必读）

**可靠（reliable）——两种模式下行为一致，必须采用**：

- **依赖注入 / `vi.fn()` test-double seam**：生产代码以**参数**接收协作者（构造器参数或
  工厂入参），测试传入一个 `vi.fn()`。完全不涉及模块拦截，故 CLI 与 Bazel 结果恒等。
  ```ts
  // 生产代码：logger 作为构造器参数注入
  export class OTelReporter implements Reporter {
    constructor(name: string, logger?: OTelLogger) {
      this.logger = logger ?? logs.getLogger(name);
    }
  }
  // 测试：注入 vi.fn()，断言其被调用
  const emit = vi.fn();
  const reporter = new OTelReporter("t", { emit, enabled: () => true } as any);
  reporter.write("info", "msg", {});
  expect(emit).toHaveBeenCalledOnce();
  ```
- 先例：`projects/game/agent/src/build-tools.test.ts` L4–9 注释阐明的模式——直接调用被测
  纯函数/注入协作者，不依赖模块拦截。

**脆弱（fragile）——禁止新增；存量已由 US2（`specs/019-js-test-reliability/`）重构**：

- **模块级 `vi.mock("external-dep", …)`，且该依赖被预编译 `:lib` 传递消费**。在 vitest CLI
  下 mock 生效、在 Bazel `js_test` 下 mock 不生效，导致**两种模式结果不一致**（典型表现：
  CLI 绿、Bazel 红，或断言读到空数组/undefined）。
- 若确属**无法**转为 DI 的残留 mock，**必须**断言该 mock 被实际调用（见下），并在该 mock
  处内联注明保留理由（引用本节）。

### 规则：验证 mock 确实生效（FR-010）

凡使用 `vi.mock` / `vi.fn` 拦截的测试，**必须**对被拦截的调用做正向断言（`expect(fn).toHaveBeenCalled()`
或等价），以证明 mock 真的被 exercise——否则一个"静默未拦截"的 mock 会制造假绿。这是
跨模式一致性（SC-003）的前置保证。

### Mock 审计（FR-007）

下表复现 `specs/019-js-test-reliability/research.md` §2 的全量审计。Phase 3（US2）已将三处
脆弱文件重构为 DI / factory seam，故当前仓库所有 mock 使用均为 reliable。

| 文件 | 曾 mock 的模块 | 历史判定 | 当前状态 / 处置 |
|------|---------------|---------|----------------|
| `common/js/logs/src/reporter.test.ts` | `@opentelemetry/api-logs`（曾 L112） | Fragile | ✅ 已重构：`OTelReporter` ctor 接受可选 `logger`（DI seam），测试注入 `vi.fn()`（US2 / T011） |
| `projects/game/agent/src/llm-tools.test.ts` | `langchain`（spy `createAgent`） | Fragile | ✅ 已重构：`AgentAdapterImpl` ctor 接受可选 `createAgent` 工厂（DI seam），测试注入 `vi.fn()`（US2 / T012） |
| `projects/game/agent/src/prompt-client.test.ts` | `@grpc/grpc-js`、`node:fs`、`@grpc/proto-loader`、`@dominion/common-js-grpc-resolver` | Fragile | ✅ 已重构：保留 ctor `client` DI seam；`registerDominionResolver` 移入真实构造分支；导出 `buildChannelOptions()` 作为 channel-option 的 factory seam，移除全部模块级 `vi.mock`（US2 / T013） |
| 其余 ~27 个 `*.test.ts` | 仅 `vi.fn()` / `vi.spyOn()`（构造器参数或局部工厂注入） | Reliable | 无需改动（先例：`handler.test.ts`、`grpc-js-resolver.test.ts`、`build-tools.test.ts`） |