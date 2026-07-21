# javascript/typescript 规范

## 引用

* 引用 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html) 作为本仓库 js/ts 规范基准。

> 如果本文档内容与上述引用冲突，则以本文档为优先。

## 测试 (Testing)

本节规定本仓库 TypeScript 测试的执行模型与 mock 约定。完整背景（执行模型、mock 根因、
require-in-the-middle）见 `specs/019-js-test-reliability/`。

### js_test 执行模型

所有 `*.test.ts` 通过 Bazel `js_test` target 执行。无论 CI（`bazel test`，经 `vitest_test`
宏 → `tools/dev/js/run_vitest.mjs`）还是本地 vitest CLI（`vitest run <file>`），被测包的
`.ts` 源码都经 vitest 的**单一 Vite pipeline** 现场转译。两种模式按构造一致：同一份源码、
同一条 pipeline，故 `instanceof` 检查与模块级单例在两种模式下行为相同。

### 声明测试 target：`vitest_test` 宏

每个包的 `:lib_test` 必须通过 `tools/dev/js/vitest_test.bzl` 的 `vitest_test` 宏声明：

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

`data` 规则：

- **`data` 必须是包的 `.ts` 源码 glob**（生产 + 测试），**不能**包含 `:lib`。若编译产物
  `errors.js` 与源码 `errors.ts` 并存，模块解析歧义（Node/vitest 可能优先 `.js`）会把同一
  源文件解析成两个模块实例，导致 `instanceof` 失败与模块级单例分裂。
- **`data` 必须镜像 `:lib` 的 `node_modules` deps**：丢弃 `:lib` 会同时移除其 `deps` 传递
  带入 runfiles 的外部 npm 包，故源码里的 `import "@opentelemetry/api"` 等会因 runfiles
  中无该包而 `Cannot find package` 崩溃——必须把 `:lib` 的 `deps`（所有 `:node_modules/*`
  项）显式列入 `data`。外部 npm 包在 runfiles 中无 `.ts` 源，永远以 node_modules 发布形态
  加载，不重新制造 dual-instance。
- 生成的 proto 类型若仅被 `import type` 引用（运行时擦除），无需列入 `data`。

宏内部细节：`genrule` 把 canonical shim `tools/dev/js/run_vitest.mjs` 拷贝进当前包，并自动
注入 `:node_modules/vitest` 与本地 `entry_point`——**禁止在包级 `BUILD.bazel` 中手写裸
`genrule` / `entry_point` / `:node_modules/vitest`**。shim 的退出码契约（失败 fail-closed、
空套件 vacuous pass）见 `specs/019-js-test-reliability/contracts/run-vitest-shim.md`。

### Mock 约定

**可靠模式（必须采用）**：依赖注入 / `vi.fn()` test-double seam。生产代码以参数接收协作者
（构造器参数或工厂入参），测试传入一个 `vi.fn()`。完全不涉及模块拦截，故 CLI 与 Bazel
结果恒等。

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

**脆弱模式（禁止新增）**：模块级 `vi.mock("external-dep", …)` 拦截跨包外部依赖。vitest
官方明确"使应用可测试是应用架构的职责，应使用依赖注入"（[vitest Mocking Modules —
Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)），且模块级
`vi.mock` 对跨包外部依赖（仍以 node_modules 编译形态加载）的拦截比 DI 更易随 vitest 升级
而漂移。若确属无法转为 DI 的残留 mock，必须断言该 mock 被实际调用（见下），并在该 mock
处内联注明保留理由（引用本节）。

### 规则：验证 mock 确实生效

凡使用 `vi.mock` / `vi.fn` 拦截的测试，**必须**对被拦截的调用做正向断言
（`expect(fn).toHaveBeenCalled()` 或等价），以证明 mock 真的被 exercise——否则一个
"静默未拦截"的 mock 会制造假绿。

### 规则：`require()` for require-in-the-middle instrumented packages

某些 npm 包（如 `@opentelemetry/instrumentation-grpc`）通过 **`require-in-the-middle`**
在 Node 的 `Module._load` 上安装 hook 来运行时 monkey-patch 目标模块（如 `@grpc/grpc-js`）。
vitest 的 `import()` 走 Vite SSR 加载器，**绕过 `Module._load`**，导致 hook 不触发、模块
加载未被 patch。

测试中使用此类 instrumentation 时，必须用 `require()`（而非 `import()`）加载被 instrument
的包，且 `require()` 必须在 `registerInstrumentations()` 之后调用：

```typescript
provider.register();
registerInstrumentations({ instrumentations: [createGrpcInstrumentation()] });
// require() goes through Module._load → hook fires → module patched
const grpc: typeof import("@grpc/grpc-js") = require("@grpc/grpc-js");
```

这是仅测试环境的问题：生产代码由 swc 编译为 CJS（`"module": "commonjs"`），`import()` 被
转译为 `require()`，hook 正常触发。