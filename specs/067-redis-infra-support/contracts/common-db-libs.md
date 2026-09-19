# Contract: 数据库公共库 API（Go redis / JS redis / JS mongo）

**Feature**: [../spec.md](../spec.md) | **Research**: [../research.md](../research.md)（D1/D2/D4/D7/D10）

本契约定义三个公共库的对外接口。**JS 侧 API 遵循 JS 惯例（async 工厂 + options 对象 + 依赖注入），不照搬 Go 风格**（spec 用户输入约束；`style/javascript.md`）。

## 1. Go：`common/gopkg/redis`（新包，对齐 `common/gopkg/mongo` 模式）

```go
// NewClient 按 dominion target（"app/name"，默认端口 6379）建立 redis 客户端。
// 解析端点 → 派生密码 → 构造客户端（惰性连接，不 Ping——与 mongo 库行为一致）。
func NewClient(rawTarget string, opts ...ClientOption) (*redis.Client, error)

// WithK8sResolver 使用 legacy Kubernetes resolver（测试/特殊环境注入点）。
func WithK8sResolver() ClientOption
```

行为契约：

* target 解析失败 → `error`（含原文与期望格式）。
* 端点解析失败或空端点 → `error`（含 target 上下文）。
* 凭证：内部按 [deploy-redis-infra.md §3.3](deploy-redis-infra.md) 派生，仅密码（无用户名）。
* 底层客户端：`github.com/redis/go-redis/v9` `redis.NewClient(&redis.Options{Addr, Password})`；未命中返回 `redis.Nil`（驱动哨兵错误原样透出）。
* 关闭由调用方负责（`client.Close()`）。

## 2. JS：`common/js/redis`（新包 `@dominion/common-js-redis`，node-redis v5）

```ts
export interface RedisClientOptions {
  /** 端点解析器注入点；缺省使用 @dominion/common-js-resolver 的 createResolver() */
  resolver?: EndpointResolver;
  /** 环境变量读取注入点；缺省 process.env（读取 DOMINION_ENVIRONMENT） */
  env?: Record<string, string | undefined>;
}

/**
 * 按 dominion target（"app/name"，默认端口 6379）建立已连接的 redis 客户端。
 * 内部：resolve 端点 → 派生密码 → createClient({ socket: { host, port }, password })
 *       → await connect()。
 * 返回 node-redis 客户端（已连接）；断连由调用方负责（client.disconnect()）。
 */
export function createRedisClient(
  target: string,
  options?: RedisClientOptions,
): Promise<RedisClient>;
```

行为契约：

* target 非法（缺 `/`、空段）→ 抛 `Error`（消息含期望格式 `"app/name"`）。
* 端点解析失败/空 → 抛 `Error`（含 target 上下文）。
* 连接失败 → 抛出 node-redis 原始错误（不吞掉、不重试）。
* 不读取、不支持任何连接串覆盖环境变量（直连覆盖属应用层策略，见 §4 agent_v2 模式）。
* 错误体系：普通 `Error`，不引入 sentinel error（JS 惯例；调用方按消息与驱动错误类型判断）。

包约定（`style/javascript.md`）：原生 ESM（`"type": "module"`）、相对导入带 `.js`、`vitest_test` 宏单测、依赖注入 mock（resolver/env 传 test-double）、`redis: "catalog:"` 依赖、提供 `runtime_pkg` target 供 `artifact_pkg_js` 打包。

## 3. JS：`common/js/mongo`（新包 `@dominion/common-js-mongo`）

```ts
export interface MongoClientOptions {
  resolver?: EndpointResolver;
  env?: Record<string, string | undefined>;
}

/** 按 dominion target 建立已连接的 mongodb 客户端（mongodb 驱动 v7）。 */
export function createMongoClient(
  target: string,
  options?: MongoClientOptions,
): Promise<MongoClient>;

/**
 * 仅产出凭据 URI（mongodb://admin:{password}@{host}:{port}/admin?authSource=admin），
 * 不建连。供"宿主注入连接串"模式（插件经环境变量接收连接，见 §4）。
 */
export function resolveMongoUri(
  target: string,
  options?: MongoClientOptions,
): Promise<string>;
```

行为契约：

* **target 语法**：接受 `app/name`（默认端口 27017）与 `dominion:///app/service:port`（显式端口）两种形式；统一归一化为 `app/service:port` 后交由 `@dominion/common-js-resolver`（JS resolver 要求 target 带端口；`dominion:///` 是既有 JS 调用方惯例，见 `projects/game/agent_v2/src/presets.ts`）。`dominion:///` 前缀与显式端口为两个独立可选维度，其任意组合（另含 `dominion:///app/service` 与 `app/service:port`）均为合法输入，且按同一规则归一化（去前缀、缺端口补 27017），归一化结果无歧义。归一化后的 app/service/端口/default-env 语义与 Go `common/gopkg/mongo` 一致——Go 端只接受 `app/name` 并在 solver 内补默认端口，故此处为**输入语法的超集**，端点解析与凭证结果无差异。
* 与 Go `common/gopkg/mongo` 行为对齐：默认端口 27017、用户名 `admin`、`authSource=admin`、环境名取 `DOMINION_ENVIRONMENT`（缺失回退 `default`）、端点取解析结果首个。
* 密码派生算法与 `common/gopkg/mongo/credentials.go` 逐字节一致（key `dominion-mongo-stable-password`）。
* `createMongoClient` 成功返回前已完成驱动 `connect()`；关闭由调用方负责（`client.close()`）。
* `mongodb` 为已核实 lexer 可检测的 CJS 具名导入白名单成员（`style/javascript.md` §CJS 第三方依赖导入约定），具名导入 `MongoClient` 类型允许。

## 4. 使用模式（应用层）

* **直连覆盖优先**（agent_v2 既有模式，保持不变）：应用先检查自有环境变量（如 `MONGO_URI`），存在则直连；否则调用公共库。该优先级逻辑属应用代码，公共库不感知。

```ts
// projects/game/agent_v2/src/presets.ts 替换后形态（示意）
// 对外签名保持 `resolveMongoUri(deps?)` 不变（bootstrap.ts 零改动，见 specs/067-redis-infra-support/tasks.md T013）。
import { resolveMongoUri as resolveTargetMongoUri } from "@dominion/common-js-mongo";
import type { EndpointResolver } from "@dominion/common-js-resolver";

export async function resolveMongoUri(
  deps: { env?: Record<string, string | undefined>; resolver?: EndpointResolver } = {},
): Promise<string> {
  const env = deps.env ?? process.env;
  const direct = env.MONGO_URI;                 // 直连优先（测试/本地）
  if (direct) return direct;
  return resolveTargetMongoUri(MONGO_TARGET, deps);  // 公共库派生+解析
}
```

* **Go 服务**（demo go_service 示意）：

```go
client, err := redis.NewClient("redis-demo/redis")   // common/gopkg/redis
defer client.Close()
```

## 5. 测试要求

* 三个库均须覆盖：target 解析（合法/非法表驱动）、端点选择（多端点取首、空端点报错）、密码派生（与已知向量比对——向量取自 Go/JS/实例侧共享测试数据，保证三实现一致）、resolver/env 注入的 mock 断言（`style/javascript.md` Mock 约定：对注入的 test-double 做正向断言）。
* 跨实现一致性测试向量（同一 inputs → 同一密码）作为单测固定数据写入各包测试，不以跨包运行时依赖方式共享。
