# Contract: grpc-runtime 基建插件（`ctx.grpc`）

**Feature**: [spec.md](../spec.md) | **契约层级**: 组合级基建（组合清单一件，领域无关）

## 1. 插件标识与挂载

| 项 | 值 |
|---|---|
| 包名 | `@dominion/dsh-grpc-runtime`（`common/js/dsh-plugins/grpc-runtime/`） |
| cordis 行 id | `grpc-runtime` |
| 组合位置 | `projects/game/agent_v2/cordis.yml`，与 `@dominion/dsh-core` 基线行同级；无 Config |
| inject / provide | inject 无；provide `grpc`（`ctx.grpc`） |
| 内部依赖 | `@dominion/common-js-grpc-resolver`、`@dominion/common-js-grpc-otel`、`@grpc/grpc-js`、`@grpc/proto-loader`（不拆独立 common lib） |

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

- grpc-otel 须**先于** grpc-js 模块加载（`common/js/grpc/otel` 既有约束）：apply 内先注册 otel 与 resolver（均幂等），再允许任何 channel 创建。
- 基建 MUST NOT 模块顶层副作用初始化（import 顺序不可控）。

## 5. 非目标与扩展位

- 仅 gRPC **client** 侧；server 配置、HTTP/Mongo 等协议不入本插件。
- 同构扩展位：未来其他协议基建化遵循同模式——组合级基建行 + `ctx.<proto>` 服务面 + 消费边缘持连接参数；命名于各自特性定稿。
- 领域语义零承载：不校验业务请求、不做工具面、不感知场景。
