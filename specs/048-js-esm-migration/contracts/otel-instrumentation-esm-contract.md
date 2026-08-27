# Contract: OTel 插桩 ESM 等价契约（otel-instrumentation-esm）

**Feature**: [spec.md](../spec.md) | **Date**: 2026-08-24

定义模块系统切换后 **OpenTelemetry gRPC 插桩的加载时序契约**与**遥测等价验收标准**（spec FR-007 / SC-005 / Edge Case 1）。依据：[research.md R3](../research.md#r3)。

## 1. 机制与约束（为什么是现在这样）

- 插桩生效依赖模块加载钩子：RITM patch `Module.prototype.require`（仅覆盖 CJS require 链），IITM 覆盖 ESM 图内模块（含 ESM import 的 CJS 包）。Node 的 ESM→CJS 路径直接调 `Module._load`，**RITM 对其不可见**——ESM 服务里 `import "@grpc/grpc-js"` 必须经 IITM hook（https://github.com/open-telemetry/opentelemetry-js/blob/main/doc/esm-support.md ）。
- 静态 import 提升：模块体（`init()` 调用处）必然晚于其全部静态导入求值——bootstrap 静态图 MUST NOT 含 `@grpc/grpc-js`（现状满足：bootstrap 仅静态导入 OTel 装配模块）。
- 被插桩 CJS 目标集合：`{"@grpc/grpc-js"}`（唯一 instrumentation 为 `@opentelemetry/instrumentation-grpc`）。集合变更必须同步本契约 §4。

## 2. 生产路径契约（bootstrap 两段式 + init 注册 hook）

```text
bootstrap.js（ESM）
├── 静态导入：仅 OTel 装配（common-js-otel / common-js-grpc-otel / common-js-logs）
└── main()
    ├── await init({ instrumentations: [...] })
    │     └── init 首次调用时（幂等）：
    │          register("@opentelemetry/instrumentation/hook.mjs", import.meta.url)
    │          （node:module；parentURL 为 common/js/otel 自身，解析其直接依赖）
    ├── installReporter(...)
    └── const { startServer } = await import("./server.js")
          └── server 的 import "@grpc/grpc-js" 经 IITM hook → 已注册的插桩生效
```

- hook 注册收敛在 `common/js/otel` 的 `init()` 内（幂等：模块级 flag，防止测试多次 init 叠加 hook 层）；**服务 bootstrap 不感知 hook 存在**（单一职责收敛，服务零样板）。
- 时序不变量：`register()` → `init()` 完成插桩注册 → 动态 `import("./server.js")`。三步顺序不可倒置；server 永远经动态导入进入（禁止改为静态导入）。
- 服务入口集合：`projects/game/agent`（bootstrap + bootstrap-test）、`experimental/dsh/demo/agent`、`experimental/ts/grpc_hello_world`、`experimental/grpc_chain/mid`。无 grpc 插桩的服务（openai_llm/client、team_graph_spike、hello_world）经同一 `init()` 顺带注册 hook，属无害空转（无 instrumentation 注册则无 patch）。

## 3. 测试路径契约（RITM 验证保留）

`common/js/grpc/otel/src/index.test.ts` 保持"注册后 require 触发 RITM"的验证语义，唯一合法写法：

```ts
import { createRequire } from "node:module";
const require = createRequire(import.meta.url); // ESM 下唯一的 require 豁免（经真实 CJS loader，RITM 可见）
const grpc: typeof import("@grpc/grpc-js") = require("@grpc/grpc-js"); // 必须在 registerInstrumentations 之后
```

- vitest 的 Vite 流水线不走 Node loader，`import()` 不触发 RITM/IITM——该测试仅验证装配 wiring；**生产 ESM 路径的等价性由 §4 大型测试验收**。
- 该豁免在 `style/javascript.md` 中登记（FR-008）。

## 4. 遥测等价验收（SC-005）

- **判据**：大型测试环境部署重构后服务，gRPC 调用产生的 **server span 必须出现在采集侧**（trace 断言）；日志/指标上报不回归。
- **执行**：`guitar run projects/game/testplan/system_test.yaml` 与 `guitar run experimental/dsh/demo/testplan/interface_test.yaml`（完整部署→测试→清理闭环，原则 VI）；全部用例通过为验收通过。
- **回退方案**（仅当 IITM 路径在 Node 24.14 + grpc-js 1.14.4 上被证伪时启用，并须在本契约追加记录）：`init()` 内改用 createRequire 预加载 `@grpc/grpc-js`（注册插桩后先经 CJS loader 加载入缓存，后续 ESM import 命中同一缓存），启用前必须验证 ESM 对已缓存 CJS 模块命名导出的取值时点（https://nodejs.org/api/esm.html#commonjs-namespaces ）。

## 5. 禁止事项

- 禁止在任何服务 bootstrap 中静态导入 `@grpc/grpc-js`（或未来新增的被插桩 CJS 包）——会使该包在 hook 生效前加载，插桩静默丢失。
- 禁止在 bootstrap/init 之外的模块注册 loader hook。
- 禁止以 `NODE_OPTIONS` / `--experimental-loader` 参数方式承载 hook（侵入部署面，且该 flag 处于弃用路径）。
