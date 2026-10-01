# Contract: grpc-runtime 基建插件（`ctx.grpc`）

**Feature**: [spec.md](../spec.md) | **契约层级**: 组合级基建（组合清单一件，领域无关）

## 1. 插件标识与挂载

| 项 | 值 |
|---|---|
| 包名 | `@dominion/dsh-grpc-runtime`（`common/js/dsh-plugins/grpc-runtime/`） |
| cordis 行 id | `grpc-runtime` |
| 组合位置 | `projects/game/agent_v2/cordis.yml`，与 `@dominion/dsh-core` 基线行同级；无 Config |
| inject / provide | inject 无；provide `grpc`（`ctx.grpc`） |
| 内部依赖 | 源码依赖 `@dominion/common-js-grpc-resolver`、`@grpc/grpc-js`、`@grpc/proto-loader`；部署闭包依赖 `@dominion/common-js-grpc-otel`（runtime_pkg 闭包物化插桩装配，供服务 bootstrap 注册，源码不 import——时序见 §4）；不拆独立 common lib |

## 2. 服务面 API

```ts
interface GrpcClientOptions {
  readonly target: string;                 // dominion URI，如 "dominion:///game/memory:50051"
  readonly protoPath: string;              // canonical import path（如 "projects/game/game.proto"）
                                           // 或绝对路径（逃逸出口）
  readonly includeDirs?: readonly string[];// 缺省 [解析出的 service root]
  readonly serviceName: string;            // 完全限定名，如 "projects.game.MemoryService"
  readonly label?: string;                 // 日志标签，缺省 serviceName
}

interface GrpcRuntime {
  client<T>(options: GrpcClientOptions): T;
  callUnary<T>(
    client: grpc.Client,
    label: string,
    invoke: (
      client: grpc.Client,
      metadata: grpc.Metadata,
      options: { deadline: Date },
      callback: (err: grpc.ServiceError | null, response?: T) => void,
    ) => void,
    deadlineMs?: number,
  ): Promise<T>;
}
```

**义务**：

- **O1 client 构造**：protoPath 为 **canonical import path** 时按仓库 runtime_protos 部署标准解析（`tools/release/deploy/README.md` §runtime_protos——proto 物化于 service root 下的标准 import 路径，与 TLS 常量同级的仓库统一约定；service root 经模块相对深溯，memory-service `serviceRoot()` 生产布局同型 walk）；绝对路径直接使用（逃逸出口）。`loadSync(protoPath, protoOptions)`（`longs:String, enums:String, defaults:true, oneofs:true, includeDirs`——与 `ts_proto_library` 生成选项严格一致）→ `loadPackageDefinition` → 按 `serviceName` 逐段解析构造器 → 以共享 channel 构造 service client，返回 `T`。`as T` 强转是全仓**唯一**的 proto 收口点（取代 memory-service 两处 `(proto as any)`，spec FR-012）。
- **O2 缓存共享**：packageDefinition 按 (protoPath + includeDirs) 缓存；channel 按 target 缓存，同 target 的多 service client 经 `channelOverride` 共享同一 channel（grpc-js 维护者模式，https://github.com/grpc/grpc-node/issues/2214）。键为 target 的前提是凭据/channel 策略为基建统一常量；策略分化时必须扩键。
- **O3 unary 策略**（`callUnary`，语义等价迁移自 `memory-service/src/client.ts:217-249`）：`Metadata({waitForReady:true})`；deadline 缺省 10s（`deadlineMs` 可覆盖）；调用前 `probeChannel`（防"Waiting for LB pick"无限排队）；失败后记录 channel 状态日志并 reject。
- **O4 teardown**：组合 dispose 时关闭全部缓存 channel（`ctx.effect`）。

## 3. 连接策略（部署统一常量，why 注释随代码）

| 策略 | 值 | why |
|---|---|---|
| 负载均衡 | `round_robin` 覆盖默认 `pick_first` | 滚动升级期间多实例路由 |
| keepalive | **不做** app 级 keepalive | 对齐 grpc-go `ClientDefault`；unary server 默认 enforcement 会 GOAWAY 多余 ping |
| 重连退避 | initial 1s / max 15s | 既有生产参数 |
| TLS | `/etc/tls/ca.crt` 存在 → `createSsl(rootCert)` + `ssl_target_name_override=TLS_SERVER_NAME`；否则 insecure | 部署统一 mTLS 布局 |

## 4. 初始化顺序约束

- gRPC 插桩注册归**服务 bootstrap 两段式入口**（[048 otel 契约](../../048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md) §2）：bootstrap 静态图仅含 OTel 装配，`await init({ instrumentations: [createGrpcInstrumentation()] })` 先于组合动态 import；本插件经组合 Loader 动态进图，其静态 `@grpc/grpc-js` import 必然落在插桩 hook 生效之后。消费本插件的服务入口 MUST 遵循该两段式形态（`projects/game/agent_v2/src/bootstrap.ts` 为先例）。
- 本插件 MUST NOT 自行注册插桩：`common/js/grpc/otel` 仅导出插桩工厂（注册唯一经 `common/js/otel` 的 `init()`，归 bootstrap）；bootstrap 已完成注册，插件内再注册会双重 patch / 双 span。
- apply 仅幂等注册 dominion resolver（`registerDominionResolver`）→ 建立惰性 runtime（channel 缓存不预建）→ `ctx.provide` → 注册 teardown effect；本插件 MUST NOT 模块顶层副作用初始化、MUST NOT 在 apply 期创建 channel（import 顺序与进图时机不可控）。channel 唯一创建点是 `ctx.grpc.client()`（§2 O1/O2 的消费路径），必然后于 apply、亦必然后于 bootstrap 插桩注册。

## 5. 非目标与扩展位

- 仅 gRPC **client** 侧；server 配置、HTTP/Mongo 等协议不入本插件。
- 同构扩展位：未来其他协议基建化遵循同模式——组合级基建行 + `ctx.<proto>` 服务面 + 消费边缘持连接参数；命名于各自特性定稿。
- 领域语义零承载：不校验业务请求、不做工具面、不感知场景。
