# javascript/typescript 规范

## 引用

* 引用 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html) 作为本仓库 js/ts 规范基准。

> 如果本文档内容与上述引用冲突，则以本文档为优先。

## 模块系统 (ESM)

本仓库全部 JS/TS 包为**原生 ESM**：每包 `package.json` 声明 `"type": "module"`，swc 发射
ESM 产物，由 Node ESM 运行时直接执行。包级契约（声明、编译配置、书写规则、审计判据）见
`specs/048-js-esm-migration/contracts/esm-package-conventions.md`。

### 编译配置：tsconfig 与 `.swcrc` 锁步

- `tsconfig.json`：`"module": "nodenext"`（隐含 `moduleResolution: nodenext` 与
  `esModuleInterop`）、`target: ES2020`。消费工作区依赖时 `paths` 指向
  `"<dep>/src/index.js"`——TS 扩展名替换规则（`X.js` → `X.ts`）定位到 `.ts` 源码做类型
  检查（https://www.typescriptlang.org/docs/handbook/modules/reference.html ）。
- `.swcrc`：`{"module": {"type": "es6", "preserveImportMeta": true}}`、`jsc.target:
  es2020`。swc 无 `nodenext` 模块类型，ESM 输出即 `"es6"`
  （https://swc.rs/docs/configuration/modules ）。
- 两文件 MUST 同步变更：swc 不读取 tsconfig（https://github.com/swc/swc/issues/1348 ），
  类型检查（`tsc`，`*_typecheck` target）与发射（swc）的一致性由人工保持
  （https://github.com/aspect-build/rules_swc/blob/main/docs/tsconfig.md ）。
- `preserveImportMeta` MUST 显式 `true`：swc 默认 `false` 会把 `import.meta` 转译掉，ESM
  产物的 `import.meta.url/dirname` 依赖它原样保留。

### 源码书写规则

- 相对导入 MUST 带 `.js` 扩展名（`./merge.js`）；禁止无扩展名与目录导入。Node ESM 运行时
  只接受带扩展名的相对导入，NodeNext 在类型层强制同一书写，消除运行时/类型检查歧义
  （https://www.typescriptlang.org/docs/handbook/modules/reference.html ）。
- 资源定位：目录用 `import.meta.dirname`，URL 锚点用 `import.meta.url`
  （https://nodejs.org/api/esm.html#importmetadirname ）。禁止 `__dirname`/`__filename`/
  `require()` 直用/`module.exports`。
- 类型再导出 MUST 显式 `export type`，且不得消费 npm 包的 `const enum`——规避 swc 类型
  擦除歧义（https://github.com/aspect-build/rules_ts/discussions/398 ）。

### CJS 第三方依赖导入约定

仍为 CJS 的第三方依赖从 ESM 侧导入时，具名导出依赖 cjs-module-lexer 静态检测
（https://nodejs.org/api/esm.html#commonjs-namespaces ）；TS nodenext 对 CJS 具名导入持
"乐观假设"，静态检查不兜底运行时（https://github.com/microsoft/TypeScript/issues/54018 ）。

- **default import 为默认约定**（`import express from "express"`），具名成员经属性访问。
- 具名导入仅允许用于已核实 lexer 可检测的包：`@grpc/grpc-js`、`@grpc/proto-loader`、
  `mongodb`、`pngjs`（依据 `specs/048-js-esm-migration/research.md` R5 逐包核实表）。
- 新增 CJS 依赖未经产物导出模式核实前，一律 default import。

### 打包与运行

- 服务 tar（`artifact_pkg_js`，`tools/release/defs.bzl`）将服务 manifest 打包至
  `dominion/{app}/{service}/package.json`：Node 以最近 package.json `"type"` 判定 `.js`
  模块格式（https://nodejs.org/api/packages.html#type ），服务根必须携带 manifest。
- 构建期门禁为终态执行语义，不可豁免：目标所在包缺 `package.json` 时分析期失败；
  manifest 缺 `"type": "module"` 时打包 action 失败——ESM-only 构建，CJS 服务产物不再
  支持。
- `js_binary` target 的 data/runfiles MUST 使包自身 `package.json` 与编译产物同根可见
  （runfiles 内最近 package.json 判定模块格式）。

### OTel 插桩与 loader hook

生产插桩由 `common/js/otel` 的 `init()` 统一承载：首次调用时注册 OTel ESM loader hook
（IITM，`register("@opentelemetry/instrumentation/hook.mjs", import.meta.url)`，幂等）。
RITM（require-in-the-middle）patch 的 `Module.prototype.require` 对 ESM import 的 CJS 包
不可见，被插桩包（当前集合 `{"@grpc/grpc-js"}`）在 ESM 服务里 MUST 经 IITM hook 加载。
时序契约与禁止事项见 `specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`：

- 服务 bootstrap 保持两段式：静态只导 OTel 装配 → `await init()` → 动态
  `import("./server.js")`；禁止 bootstrap 静态导入被插桩包（会使包在 hook 生效前加载，
  插桩静默丢失）。
- 新增插桩目标 CJS 包时：instrumentation 装配经 `common/js/otel` 体系扩展（构造器置于
  公共模块、由服务传入 `init({ instrumentations: [...] })`），不在各 bootstrap 散落注册
  loader hook；并同步 otel 契约的被插桩集合登记。

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

### 规则：`createRequire()` —— RITM 插桩包的测试加载（唯一 require 豁免）

某些 npm 包（如 `@opentelemetry/instrumentation-grpc`）通过 **`require-in-the-middle`**
在 Node 的 `Module._load` 上安装 hook 来运行时 monkey-patch 目标模块（如 `@grpc/grpc-js`）。
vitest 的 `import()` 走 Vite SSR 加载器，**绕过 `Module._load`**，导致 hook 不触发、模块
加载未被 patch。

测试中验证 RITM 插桩 wiring 时，必须用 `createRequire` 构造的 `require()`（而非 `import()`）
加载被 instrument 的包，且该 `require()` 必须在 `registerInstrumentations()` 之后调用：

```typescript
import { createRequire } from "node:module";

// ESM 测试中唯一的 require 豁免：走真实 CJS loader（Module.prototype.require），
// RITM hook 可见（https://nodejs.org/api/module.html#modulecreaterequirefilename ）
const require = createRequire(import.meta.url);

provider.register();
registerInstrumentations({ instrumentations: [createGrpcInstrumentation()] });
// require() goes through Module._load → hook fires → module patched
const grpc: typeof import("@grpc/grpc-js") = require("@grpc/grpc-js");
```

这是 ESM 测试代码中**唯一**允许的 require 形式，豁免登记于
`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md` §3（当前唯一
使用点：`common/js/grpc/otel/src/index.test.ts`）。生产代码无此豁免：生产插桩由 IITM
hook 承载（见上文"OTel 插桩与 loader hook"），服务代码统一 `import`。
