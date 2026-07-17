# javascript/typescript 规范

## 基准

* 以 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html) 作为本仓库 js/ts 规范基准。

> 如果本文档内容与上述引用冲突，则以本文档为优先。

## 测试 (Testing)

本节规定本仓库 TypeScript 测试的执行模型与 mock 约定（FR-009）。完整背景见
`specs/019-js-test-reliability/research.md`（§2 为 mock 根因，§3 为 runner 机制，
**§6 为 module-identity 根因与 Fix B 决策**）。

### js_test 执行模型

本仓库所有 `*.test.ts` 通过 Bazel `js_test` target 执行。**两种执行模式现在都把包的
`.ts` 源码经 vitest 的单一 Vite pipeline 现场转译**——历史上"Bazel 预编译 `:lib` vs CLI
转译"的对比已退役（Fix B，详见 `specs/019-js-test-reliability/research.md` §6 与
`specs/019-js-test-reliability/plan.md` Module-Identity Revision）：

| 模式 | 入口 | 被测代码形态 | `vi.mock()` 是否拦截 |
|------|------|-------------|---------------------|
| **Bazel `js_test`**（CI/`bazel test`） | `vitest_test` 宏 → `tools/dev/js/run_vitest.mjs` | **包 `.ts` 源码**（`data = glob(["src/**/*.ts"])`，由 vitest 的 Vite pipeline 现场转译） | **是**——源码经 vitest 转译，静态 `import` 被改写为 `__handle_mock__` 包装，模块加载前注册 mock |
| **vitest CLI**（`transpile-on-the-fly`） | 直接 `vitest run <file>` | **包 `.ts` 源码**（同上，同一 Vite pipeline） | **是**——同上 |

两种模式现在**按构造一致**（SC-003）：同一份源码、同一条转译 pipeline，故 `instanceof`
检查与模块级单例在 Bazel 与 CLI 下行为相同。

**退役的旧模型（`:lib` 在 test `data` 中）为何被废弃**：每个包的 `tsconfig` 固定
`"module": "commonjs"`，故 `:lib`（`ts_project` + `swc`）产出 CommonJS。当 `:lib` 在
test `data` 中时，runfiles 的 `src/` 同时存在编译 `*.js` 与源 `*.ts`——测试经 vitest
转译（实例 A），而预编译 CJS 内部的 `require()` **不被 vitest 拦截**（Vite SSR 不改写
已编译 CJS 的 `require`），故生产代码以原生 Node 加载共享模块（实例 B）。生产代码与
测试把**同一源文件解析成两个模块实例**，导致 `expect(err).toBeInstanceOf(X)` 失败
（即便 `name`/`message` 正确）、模块级单例（reporter/default-logger/OTel-tracer）分裂。
vitest 维护者确认这是 expected CJS 行为（[vitest#7591](https://github.com/vitest-dev/vitest/issues/7591)：
"`require(...)` is not intercepted by Vitest, so the module is different … current behavior
is expected"；[vitest#5601](https://github.com/vitest-dev/vitest/issues/5601)：`server.deps.inline`
是标准手段）。**Fix B 把 test `data` 改为包源码（丢弃 `:lib`）**，使整包经单一 Vite pipeline
转译 → 单一模块实例，dual-instance 根因在**基础设施层移除**。

**§2 `vi.mock` 拦截缺口的现状**：`specs/019-js-test-reliability/research.md` §2 记录的
"模块级 `vi.mock("external-dep")` 在 Bazel 下不拦截预编译 `:lib`"缺口，**在 `:lib` 不再
进入 test `data` 后不再出现**（包自身代码现已全部经 vitest 转译，`vi.mock` 对包内 import
生效）。该缺口的根因——"vitest 仅对经自身 pipeline 转译的文件做 hoisting"——依然成立
（[vitest Mocking guide — How it works](https://vitest.dev/guide/mocking/modules#how-it-works)），
故下方"可靠 vs 脆弱"约定与"验证 mock 生效"规则仍须遵守；只是触发条件从"任何包内 mock"
收窄为"对**跨包外部依赖**的模块级 mock"。

**取舍声明（Fix B）**：测试执行的是**源码**（vitest 现场转译），而非 swc 编译产物。
编译/类型正确性**未丢失**——`:lib`（`ts_project`）仍作为 `server_pkg` 等生产 target 的
依赖被构建，故 `bazel build //...:server_pkg` 仍做类型检查并产出编译产物；改变的只是
*测试*看到的内容。SC-001（诚实信号）/SC-003（CLI==Bazel）由单 pipeline 源码转译更好地满足。

### 受支持的使用面：`vitest_test` 宏 + 共享 shim

- **每个包的 `:lib_test` 必须通过 `tools/dev/js/vitest_test.bzl` 的 `vitest_test` 宏声明**，
  调用方传入**包的原始 `.ts` 源码 glob**（生产 + 测试）+ 必要的 `:node_modules/*` 依赖
  （**不是** `:lib`）：
  ```starlark
  load("//tools/dev/js:vitest_test.bzl", "vitest_test")
  vitest_test(
      name = "lib_test",
      data = glob(["src/**/*.ts"], allow_empty = True) + [
          ":node_modules/@types/node",          # 镜像该包 :lib 的 node_modules deps
          # ... 其余 :lib deps 中出现的 :node_modules/* 条目
      ],
  )
  ```
  宏内部 `genrule` 把唯一的 canonical shim `tools/dev/js/run_vitest.mjs` 拷贝进当前包，
  并自动注入 `:node_modules/vitest` 与本地 `entry_point`——这些是**宏内部细节，禁止在
  包级 `BUILD.bazel` 中手写裸 `genrule` / `entry_point` / `:node_modules/vitest`**。
- **`data` 必须为源码、且必须镜像 `:lib` 的 `node_modules` deps**：丢弃 `:lib` 会同时移除
  `:lib` 的 `deps` 传递带入 runfiles 的外部 npm 包，故源码里的 `import "@opentelemetry/api"`
  等会因 runfiles 中无该包而 `Cannot find package` 崩溃——必须把 `:lib` 的 `deps`（所有
  `:node_modules/*` 项）显式列入 `data`。**外部 npm 包在 runfiles 无 `.ts` 源，永远以
  node_modules 发布形态加载，加入它们不重新制造 dual-instance**（dual-instance 只针对包
  自身源码的 `.ts` vs `.js` 歧义）。生成的 proto 类型若仅被 `import type` 引用（运行时
  擦除），无需列入 `data`。详见 `tools/dev/js/vitest_test.bzl` docstring 与
  `specs/019-js-test-reliability/research.md` §6。
- **关键不变量**：`data` 中**绝不能同时**有 `:lib` 和源码——若编译 `errors.js` 与源码
  `errors.ts` 并存，模块解析歧义（Node/vitest 可能优先 `.js`）会重新制造 dual instance。
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

- **模块级 `vi.mock("external-dep", …)` 拦截跨包外部依赖**。Fix B 之前（`:lib` 在 test
  `data` 中），预编译 `:lib` 的 import 绕过 vitest → CLI 绿、Bazel 红；**Fix B 之后该拦截
  缺口已在基础设施层移除**（包源码现经单一 Vite pipeline 转译，`vi.mock` 对源码 import
  生效）。但模块级 `vi.mock` 仍**不推荐**：vitest 官方明确"使应用可测试是应用架构的职责，
  应使用依赖注入"（[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)），
  且 `vi.mock` 对**跨包外部依赖**（仍以 node_modules 编译形态加载）的拦截比 DI 更易随
  vitest 升级而漂移。故**优先 DI**；新模块级 `vi.mock` 禁止新增。
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