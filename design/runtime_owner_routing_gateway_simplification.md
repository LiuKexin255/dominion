# runtime owner 路由下沉与 gateway 简化方案

## 背景

当前 game 链路中，`gateway` 已逐步退化为边缘聚合服务，但仍承担了一部分 runtime owner 路由职责：

* `gateway` 解析 session token 中的 `owner_runtime_id`。
* `gateway` 通过 owner resolver 定位 owner runtime 实例。
* WebSocket upgrade 请求由 `gateway` 直接反向代理到 owner runtime。

这使 `gateway` 仍理解 runtime ownership 语义。目标态应进一步收敛边界：

* `gateway` 只做公网入口和 grpc-gateway 聚合。
* runtime ownership、token、WebSocket owner 路由都归属 `runtime` 服务。
* `gateway -> runtime` 退化为普通服务调用，不再有特殊 owner 路由分支。

同时，`design/game_session_apitest_shell.md` 已为 session 服务级大型测试定义了 test-only HTTP apitest shell 模式。runtime 服务级大型测试也应采用同一模式，避免测试直接依赖生产 gateway 的 owner 路由语义。

## 目标

本方案要达成：

1. `gateway` 不再解析、校验、理解 runtime token。
2. `gateway` 不再持有 owner resolver，不再按 `owner_runtime_id` 选择 runtime 实例。
3. runtime owner 路由下沉到 `runtime` 服务：非 owner runtime 收到 WebSocket connect 后，转发到 owner runtime。
4. `gateway -> runtime` 统一为普通服务调用：
   * runtime read 通过 gateway 的 grpc-gateway 聚合转发到 runtime gRPC backend。
   * runtime WebSocket 通过 gateway 普通反向代理/服务转发到 runtime HTTP backend。
5. `token` 包不再作为 game 共享包，移动到 `runtime/domain` 语义边界内。
6. runtime read 不再由 runtime HTTP server 单独处理；统一采用 grpc-gateway 聚合。
7. runtime 服务级大型测试参考 session apitest shell，使用 `apitest.liukexin.com` 下的 test-only HTTP 入口。
8. runtime 的两个 gRPC 服务都纳入 owner route 逻辑范围：
   * `GameRuntimeReader` 的 read RPC 根据 token 中的 owner runtime 路由到 owner 实例。
   * `GameRuntimeManager` 的 owner-bound RPC 根据 token 或目标 ownership 路由到 owner 实例。

## 非目标

本方案不做：

* 不改变 session 对外 HTTP contract。
* 不把 runtime owner 路由放回 gateway。
* 不让 apitest shell 承担业务逻辑、鉴权、token 签发或 owner 决策。
* 不把 runtime apitest shell 作为生产服务发布。
* 不改变 WebSocket 协议消息结构。

## 目标架构

### gateway 职责

`gateway` 是纯边缘聚合层：

```text
public client
  -> gateway
     -> session gRPC backend via grpc-gateway
     -> runtime read gRPC backend via grpc-gateway
     -> runtime HTTP backend for WebSocket connect
```

`gateway` 不再做：

* token parse / verify。
* owner runtime 决策。
* owner runtime endpoint resolve。
* runtime token 签发或续签。
* runtime domain 状态管理。

### runtime 职责

`runtime` 持有 runtime domain 边界内的能力：

```text
runtime
  -> token issue / verify / routing-claim parse
  -> runtime ownership metadata
  -> WebSocket connect validation
  -> non-owner -> owner runtime proxy
  -> local runtime session handling
```

WebSocket 路由模型：

```text
GET /v1/sessions/{id}/game/connect?token=...
  -> runtime HTTP server
  -> parse token routing claims
  -> if owner_runtime_id == local runtime id:
       verify token and handle locally
     else:
       resolve owner runtime HTTP endpoint
       reverse proxy original request to owner runtime
```

owner runtime 端仍执行完整 token 校验；非 owner runtime 只使用未验证 claims 做转发决策，不建立信任边界。

## runtime gRPC owner route 范围

runtime 当前有两个 gRPC service：

```text
GameRuntimeReader
  GetGameRuntime
  GetGameSnapshot

GameRuntimeManager
  CreateGameRuntime
  RefreshGameRuntime
```

目标态中，owner route 不只覆盖 WebSocket，也覆盖 runtime gRPC 调用。原因是 gateway 和 session 都会通过普通服务发现调用 runtime aggregate target，请求可能落到非 owner runtime 实例；非 owner 实例不能直接返回 not found 或触发不必要 rebuild，而应把 owner-bound 请求转发到 owner runtime。

### 范围选项评估

| 选项 | 覆盖范围 | 优点 | 问题 | 结论 |
| --- | --- | --- | --- | --- |
| A. 只覆盖 WebSocket | 仅 `GET /game/connect` owner mismatch 时 proxy | 改动最小 | runtime read 经 grpc-gateway 落到非 owner 会误报 not found；`RefreshGameRuntime` 落到非 owner 会触发 session rebuild | 不采用 |
| B. 覆盖 WebSocket + `GameRuntimeReader` | WebSocket、`GetGameRuntime`、`GetGameSnapshot` | read API 可以正确归位 | `RefreshGameRuntime` 仍可能落到非 owner，导致不必要 rebuild 和 owner epoch 语义混乱 | 不采用 |
| C. 覆盖 WebSocket + `GameRuntimeReader` + `RefreshGameRuntime` | 所有 owner-bound runtime 入口 | gateway/session 都可退化为普通服务调用；owner 归位统一在 runtime 内完成 | 需要新增 runtime gRPC forwarding 组件 | 采用 |
| D. 覆盖所有 `GameRuntimeManager` RPC | `CreateGameRuntime` 也尝试 owner route | 表面上最统一 | `CreateGameRuntime` 没有既有 owner，不存在可路由目标；会引入伪 owner 分配语义 | 不采用 |

### 范围决策

采用选项 C：

```text
owner route scope = WebSocket + GameRuntimeReader + RefreshGameRuntime
```

具体决策：

1. `GameRuntimeReader.GetGameRuntime`：owner-bound，必须根据 request metadata token 路由到 owner runtime。
2. `GameRuntimeReader.GetGameSnapshot`：owner-bound，必须根据 request metadata token 路由到 owner runtime。
3. `GameRuntimeManager.RefreshGameRuntime`：owner-bound，必须根据 `old_token` 路由到 owner runtime。
4. `GameRuntimeManager.CreateGameRuntime`：owner 建立入口，不做 owner route；由接收请求的 runtime 本地创建并成为 owner。
5. WebSocket connect：owner-bound，继续根据 query token 路由到 owner runtime。

这个决策保证：所有已经持有 token 的 runtime 入口都能从 token 中找到 owner 并归位；没有 token 的 owner 建立入口不伪造路由语义。

### GameRuntimeReader

`GameRuntimeReader` 的两个 RPC 都是 owner-bound read：

* `GetGameRuntime`
* `GetGameSnapshot`

处理模型：

```text
incoming gRPC metadata token
  -> parse routing claims
  -> if owner_runtime_id == local runtime id:
       verify token and handle locally
     else:
       resolve owner runtime gRPC endpoint
       call same RPC on owner runtime
       return owner runtime response/error unchanged
```

这样经 gateway grpc-gateway 聚合的 read 请求即使落到非 owner runtime，也能返回 owner runtime 上的真实状态。

### GameRuntimeManager

`GameRuntimeManager` 中两个 RPC 的 owner route 语义不同：

* `CreateGameRuntime` 是 ownership 建立入口。请求没有 old token，也没有既有 owner 可路由；它在接收请求的 runtime 实例本地创建 runtime，并返回该实例作为 owner。
* `RefreshGameRuntime` 是 owner-bound 续期入口。请求携带 `old_token`，应根据 old token 中的 `owner_runtime_id` 路由到 owner runtime；如果请求先落到非 owner runtime，非 owner runtime 应通过 gRPC 转发到 owner runtime，而不是直接失败导致 session 误判为需要 rebuild。

处理模型：

```text
CreateGameRuntime
  -> local create
  -> return local runtime id as owner_runtime_id

RefreshGameRuntime(old_token)
  -> parse old_token routing claims
  -> if owner_runtime_id == local runtime id:
       verify old_token and refresh locally
     else:
       resolve owner runtime gRPC endpoint
       call RefreshGameRuntime on owner runtime
       return owner runtime response/error unchanged
```

### gRPC owner router 实现位置

建议新增 runtime 内部 owner route 组件：

```text
projects/game/runtime/owner/
  resolver.go      # owner_runtime_id -> runtime endpoint
  grpc_router.go   # owner-bound gRPC forwarding
  http_router.go   # WebSocket HTTP forwarding
```

gRPC forwarding 不应放在 gateway，也不应放在 session runtime client。session 仍只调用 `game/runtime:grpc`，runtime 自己负责 owner-bound 请求归位。

### endpoint target

owner resolver 需要同时支持两类 endpoint：

```text
game/runtime:http  # WebSocket proxy
game/runtime:grpc  # GameRuntimeReader / RefreshGameRuntime proxy
```

两个 target 都按 deploy stateful instance 的 hostname 匹配 `owner_runtime_id`。

## token 包边界调整

### 当前问题

当前 token 位于：

```text
projects/game/pkg/token
```

但 token 的字段和语义已经是 runtime 专属：

* `owner_runtime_id`
* `owner_epoch`
* `reconnect_generation`
* runtime internal audience

`gateway` 不再解析 token 后，token 不需要继续作为 `projects/game/pkg` 共享能力。

### 目标位置

将 token 移入 runtime domain 边界，例如：

```text
projects/game/runtime/domain/token
```

迁移后依赖方向：

```text
runtime/domain/token
  <- runtime/service
  <- runtime/handler
  <- runtime/ws
  <- runtime/owner routing
```

`session` 不应直接解析 token。session 只保存 runtime 返回的 token 字符串和 owner runtime id。

`gateway` 不应导入 runtime token 包。

### 代码调整原则

* 保持 token wire format 不变，避免现有 session 持久化 token 失效。
* 包路径迁移不改变 `Claims` 字段名和 JSON tag。
* 原 `projects/game/pkg/token` 删除或只在迁移期短暂保留；最终目标是不再共享。

## pkg/testutil 评估

当前 `projects/game/pkg/testutil` 只被一个包引用：

```text
projects/game/gateway/testplan
```

当前 helper 包含：

* `NewTestSessionID`
* `CreateSession`
* `ReconnectSession`
* `DeleteSession`
* `DialWebSocket`
* `ParseSessionToken`

其中 `ParseSessionToken` 依赖 `projects/game/pkg/token` 的 `Parser`。token 迁入 runtime domain 后，如果继续保留 `pkg/testutil.ParseSessionToken`，会产生两个问题：

1. `projects/game/pkg/testutil` 需要反向依赖 runtime domain token，破坏 shared pkg 的边界。
2. gateway testplan 会间接保留“解析 runtime token”的断言倾向，而目标态 gateway 不再理解 token。

因此本方案建议删除 `projects/game/pkg/testutil`，将仍有价值的 HTTP/session 测试 helper 内联或移动到唯一消费者：

```text
projects/game/gateway/testplan
```

调整原则：

* `NewTestSessionID`、`CreateSession`、`ReconnectSession`、`DeleteSession` 可以作为 gateway testplan 私有 helper 保留。
* `DialWebSocket` 可以直接使用 `websocket.Dial` 或作为 gateway testplan 私有 helper。
* `ParseSessionToken` 不应继续作为 gateway testplan 公共断言；如果测试确实需要 owner id，应从 session response 的 `owner_runtime_id` 字段或 runtime 行为结果断言，而不是解析 token。

验收：

* 删除 `projects/game/pkg/testutil` 后无其他包引用。
* `projects/game/gateway/testplan` 不再导入 runtime token 包。
* gateway testplan 不再断言 gateway 能解析 token claims。

## runtime read 聚合模型

### 当前问题

runtime HTTP server 当前主要处理 WebSocket，runtime read API 已有 proto HTTP annotation，但不应由 runtime HTTP server 单独暴露服务级 REST 入口。

### 目标模型

runtime read 统一由 grpc-gateway 聚合：

```text
HTTP GET /v1/sessions/{id}/game/runtime
  -> grpc-gateway mux
  -> GameRuntimeReader.GetGameRuntime gRPC

HTTP GET /v1/sessions/{id}/game/snapshot
  -> grpc-gateway mux
  -> GameRuntimeReader.GetGameSnapshot gRPC
```

生产公网入口：

```text
game.liukexin.com
  -> gateway grpc-gateway mux
  -> runtime gRPC backend
```

runtime 服务级测试入口：

```text
apitest.liukexin.com/game/runtime
  -> runtime apitest shell
  -> runtime gRPC backend
```

runtime 自身 HTTP server 只保留 WebSocket connect 入口，不再承载 read REST 路由。

## runtime apitest shell 设计

参考 `design/game_session_apitest_shell.md`，新增 runtime test-only HTTP 壳子。

### 目录

新增：

```text
projects/game/runtime/testplan/apitest/
  BUILD.bazel
  main.go
  service.yaml
```

### 路由

统一测试域名：

```text
apitest.liukexin.com
```

runtime prefix：

```text
/game/runtime
```

read API 测试入口：

```text
GET https://apitest.liukexin.com/game/runtime/v1/sessions/{id}/game/runtime
GET https://apitest.liukexin.com/game/runtime/v1/sessions/{id}/game/snapshot
```

内部处理：

```text
/game/runtime/v1/**
  -> strip prefix /game/runtime
  -> /v1/**
  -> grpc-gateway mux
  -> runtime gRPC backend
```

WebSocket 测试入口：

```text
wss://apitest.liukexin.com/game/runtime/v1/sessions/{id}/game/connect?token=...
```

内部处理：

```text
/game/runtime/v1/sessions/{id}/game/connect
  -> strip prefix /game/runtime
  -> /v1/sessions/{id}/game/connect
  -> runtime HTTP backend
  -> runtime owner routing
```

### 壳子职责

`runtime/testplan/apitest/main.go` 负责：

1. 启动 HTTP server。
2. 创建 runtime gRPC backend client。
3. 注册 `GameRuntimeReader` grpc-gateway handler。
4. 创建到 runtime HTTP backend 的普通 reverse proxy，用于 WebSocket connect。
5. 将 `/game/runtime` prefix 下的请求 strip 后分发：
   * read API -> grpc-gateway mux。
   * WebSocket connect -> runtime HTTP backend。

壳子不做：

* token parse / verify。
* owner runtime resolve。
* runtime session 创建。
* WebSocket 业务处理。

## testplan 修改

### runtime test deploy

修改：

```text
projects/game/runtime/testplan/test_deploy.yaml
```

部署对象：

1. mongo。
2. runtime。
3. runtime-apitest。

runtime 服务暴露：

* gRPC route 保留，用于 apitest shell 调 runtime read gRPC。
* HTTP route 可作为内部服务 endpoint 供 apitest shell proxy WebSocket，不作为服务级测试公共入口。

runtime-apitest 暴露：

```yaml
http:
  hostnames:
    - apitest.liukexin.com
  matches:
    - backend: http
      path:
        type: PathPrefix
        value: /game/runtime
```

### runtime interface testplan

修改：

```text
projects/game/runtime/testplan/interface_test.yaml
```

endpoint 改为：

```yaml
endpoint:
  http:
    public: https://apitest.liukexin.com/game/runtime
```

测试代码中基于 `testtool.MustEndpoint("http", "public")` 构造：

```text
/v1/sessions/{id}/game/connect
/v1/sessions/{id}/game/runtime
/v1/sessions/{id}/game/snapshot
```

最终请求会落到：

```text
https://apitest.liukexin.com/game/runtime/v1/...
```

## gateway testplan 调整

`projects/game/gateway/testplan` 继续作为系统级大型测试，验证生产公网入口：

```text
game.liukexin.com -> gateway -> session/runtime
```

需要调整断言重点：

* gateway WebSocket 可连接成功。
* gateway 不负责 owner route，owner mismatch 场景应由 runtime 处理。
* forged token / invalid token 最终仍由 runtime 拒绝。
* gateway 测试不再断言 gateway 的 `ParseRoutingClaims` 行为。

## 开发步骤

### Step 1：迁移 token 包

1. 新增 `projects/game/runtime/domain/token`。
2. 移动原 `projects/game/pkg/token` 代码和测试。
3. 更新 runtime 内部 import。
4. 移除 gateway 对 token 包的依赖。
5. session 只保留 token 字符串存储，不导入 runtime token。
6. 删除或内联 `projects/game/pkg/testutil`，避免 test helper 继续依赖 token。

验收：

```bash
bazel test //projects/game/runtime/domain/token/...
```

### Step 2：runtime 新增 owner resolver / proxy

1. 将 gateway owner resolver 语义迁移到 runtime 边界。
2. owner resolver 同时支持 `game/runtime:http` 和 `game/runtime:grpc`。
3. WebSocket handler 在本地处理前先做 owner route 判断。
4. 非 owner WebSocket 请求 reverse proxy 到 owner runtime HTTP endpoint。
5. `GameRuntimeReader` 非 owner gRPC 请求转发到 owner runtime gRPC endpoint。
6. `RefreshGameRuntime` 非 owner gRPC 请求转发到 owner runtime gRPC endpoint。

验收：

* owner=self：进入现有本地 WebSocket 流程。
* owner=remote：当前 runtime 不创建本地 session runtime，转发到 owner runtime。
* owner missing / invalid token：返回 400/401。
* owner resolver 找不到实例：返回 503。
* `GetGameRuntime` / `GetGameSnapshot` 落到非 owner runtime 时返回 owner runtime 结果。
* `RefreshGameRuntime` 落到非 owner runtime 时不会触发 session rebuild，而是转发到 owner runtime。
* `CreateGameRuntime` 仍在接收请求的 runtime 本地建立 ownership。

### Step 3：简化 gateway

1. 删除 gateway owner resolver 初始化。
2. 删除 gateway token parser 依赖。
3. gateway WebSocket connect 改为普通服务转发到 runtime HTTP backend。
4. gateway read API 继续通过 grpc-gateway 转发到 runtime gRPC backend。

验收：

* gateway 不导入 runtime token 包。
* gateway 不导入 owner resolver 包。
* gateway router 不出现 `owner_runtime_id` 分支。

### Step 4：runtime read 收敛为 grpc-gateway 聚合

1. 确认 `GameRuntimeReader` proto HTTP annotation 保持完整。
2. runtime 生产 HTTP server 不增加 read REST handler。
3. gateway 注册 `GameRuntimeReader` grpc-gateway handler。
4. runtime apitest shell 注册 `GameRuntimeReader` grpc-gateway handler。

验收：

* `GET /v1/sessions/{id}/game/runtime` 经 gateway 可用。
* `GET /v1/sessions/{id}/game/snapshot` 经 gateway 可用。
* runtime 自身 HTTP server 除 WebSocket connect 外不直接服务 read REST。

### Step 5：新增 runtime apitest shell

1. 新增 `projects/game/runtime/testplan/apitest/main.go`。
2. 新增 `service.yaml`。
3. 运行 gazelle 生成/更新 BUILD。
4. 如需要镜像 target，按现有 `artifact_image` 规则补充。

验收：

```bash
bazel build //projects/game/runtime/testplan/apitest/...
```

### Step 6：调整大型测试

1. 修改 runtime test deploy，加入 runtime-apitest。
2. 修改 runtime interface endpoint 到 `apitest.liukexin.com/game/runtime`。
3. 修改 runtime test code 中需要依赖 read API 的路径，保持相对 `/v1` 不变。
4. 修改 gateway testplan 中 owner routing 相关断言，转为验证 runtime 处理结果。

验收：

```bash
guitar validate projects/game/runtime/testplan/interface_test.yaml
guitar run projects/game/runtime/testplan/interface_test.yaml
guitar validate projects/game/gateway/testplan/interface_test.yaml
guitar run projects/game/gateway/testplan/interface_test.yaml
```

## 测试计划

### 单元测试

执行：

```bash
bazel test //projects/game/runtime/... //projects/game/gateway/... //projects/game/session/...
```

重点覆盖：

* token 包迁移后 claims、签发、校验、routing parse wire format 不变。
* runtime WebSocket owner route self / remote / missing owner / resolver failure。
* runtime gRPC owner route：reader self/remote、refresh self/remote、create local ownership。
* gateway router 不再解析 token。
* runtime read handler 仍通过 gRPC contract 返回正确结果。

### 构建验证

执行：

```bash
bazel build //projects/game/runtime/... //projects/game/gateway/... //projects/game/session/...
```

### 大型测试

执行：

```bash
guitar run projects/game/runtime/testplan/interface_test.yaml
guitar run projects/game/gateway/testplan/interface_test.yaml
guitar run projects/game/session/testplan/interface_test.yaml
```

说明：

* runtime testplan 验证 runtime 服务 contract 和 runtime 内部 owner route。
* gateway testplan 验证生产公网入口聚合链路。
* session testplan 继续验证 session apitest shell 到 session gRPC contract。

## 验收标准

完成后应满足：

* `gateway` 不再导入 token 包。
* `gateway` 不再包含 owner resolver / owner proxy 代码。
* `gateway -> runtime` 是普通服务调用，不按 owner 选择实例。
* token 包位于 runtime domain 边界内。
* session 不直接依赖 runtime token 包。
* `projects/game/pkg/testutil` 删除或不再依赖 runtime token；gateway testplan 不再导入 token。
* runtime WebSocket owner mismatch 可由 runtime 代理到 owner runtime。
* runtime gRPC owner-bound 请求可由 runtime 代理到 owner runtime。
* runtime read API 经 grpc-gateway 聚合可用。
* runtime apitest shell 使用 `/game/runtime` prefix。
* `projects/game/runtime/testplan/interface_test.yaml` endpoint 使用 `https://apitest.liukexin.com/game/runtime`。
* runtime、gateway、session 三个 testplan 均可通过。

## 增量修改点评估

在原方案基础上，新增修改点如下：

1. **gRPC owner route 组件**：runtime 不只需要 HTTP/WebSocket owner proxy，还需要 gRPC owner forwarding。
2. **owner resolver target 扩展**：从只解析 `game/runtime:http` 扩展为同时解析 `game/runtime:http` 和 `game/runtime:grpc`。
3. **Reader RPC 转发**：`GetGameRuntime`、`GetGameSnapshot` 在非 owner runtime 上需要转发到 owner runtime。
4. **Manager Refresh 转发**：`RefreshGameRuntime` 在非 owner runtime 上需要根据 `old_token` 转发，避免误触发 session rebuild。
5. **CreateGameRuntime 明确定义为 owner 建立入口**：不做 pre-owner route，由接收实例创建并成为 owner。
6. **testutil 删除/内联**：当前只有 gateway testplan 使用，且包含 token parse helper；迁移 token 后应删除或内联，避免共享包继续依赖 runtime domain。
7. **大型测试覆盖增加**：runtime testplan 需要覆盖 gRPC read/refresh 落到非 owner 实例的行为，gateway testplan 需要移除 token parse 断言。

## 待定项评估

当前方案仍有以下待定项，需要实现前确认或在实现中按代码约束收敛：

1. **gRPC 转发实现方式**：
   * 方案倾向在 runtime handler 层显式转发同名 RPC。
   * 可选方案是 gRPC interceptor，但当前 handler 已有 per-RPC token/session 解析逻辑，显式转发更直接。

2. **非 owner gRPC 转发的 metadata 传递**：
   * Reader RPC 需要透传 `token` metadata。
   * Refresh RPC 的 `old_token` 在 request body 中，metadata 需求较少。
   * 实现时需明确 context deadline、trace metadata 是否透传。

3. **runtime apitest shell 是否代理 WebSocket**：
   * 如果 runtime testplan 继续使用 WebSocket，则 shell 必须代理 WebSocket 到 runtime HTTP backend。
   * 如果 runtime testplan 改成只测 gRPC/read contract，则可不代理 WebSocket；但当前 runtime testplan 覆盖 WS，因此方案按“需要代理”处理。

4. **`projects/game/pkg/testutil` 删除时机**：
   * 当前实际只有 gateway testplan 使用，技术上可直接删除并内联。
   * 若后续其他 testplan 想复用 session HTTP helper，应新建不依赖 token 的 test helper，而不是保留当前 `pkg/testutil`。

5. **CreateGameRuntime 的负载选择策略**：
   * 当前方案定义为普通服务调用，落到哪个 runtime 就由哪个 runtime 建立 owner。
   * 是否需要更强的分配策略不属于本次迁移目标。

## Open Questions / TBD

以下问题不是方案方向阻塞项，但实现前需要明确最终落点或在实现 PR 中关闭：

| TBD | 问题 | 当前倾向 | 关闭条件 |
| --- | --- | --- | --- |
| TBD-1 | gRPC owner forwarding 放在 handler 显式实现还是 interceptor？ | handler 显式实现 | `GetGameRuntime`、`GetGameSnapshot`、`RefreshGameRuntime` 的 self/remote 单测覆盖后关闭 |
| TBD-2 | 非 owner gRPC forwarding 是否透传全部 incoming metadata？ | 透传 token、deadline、trace metadata；避免透传 hop-by-hop/无关 metadata | forwarding helper 明确 metadata 策略并有单测覆盖后关闭 |
| TBD-3 | runtime apitest shell 是否必须代理 WebSocket？ | 必须代理，因为当前 runtime testplan 覆盖 WebSocket 协议 | runtime testplan 仍包含 WS 用例且 apitest shell 代理 WS 通过大型测试后关闭 |
| TBD-4 | `projects/game/pkg/testutil` 是删除还是迁到 gateway testplan 私有 helper？ | 删除共享包，将仍需 helper 内联到 `projects/game/gateway/testplan` | `rg "projects/game/pkg/testutil|pkg/testutil|testutil\."` 无跨包引用后关闭 |
| TBD-5 | `CreateGameRuntime` 是否需要负载分配策略？ | 不需要，本次按普通服务调用落点建立 owner | 大型测试证明 create/reconnect/read/ws 在多 runtime 副本下可用后关闭 |
| TBD-6 | owner resolver 是否复用同一个 resolver 类型支持 HTTP/GRPC target？ | 复用 resolver 核心逻辑，按 target 参数解析不同 endpoint | runtime owner 包提供 HTTP/GRPC 两类解析并通过 resolver 单测后关闭 |

如果实现过程中发现 TBD 影响外部 contract，应先更新本设计文档，再继续编码。

## 决策记录

| 决策 | 结论 |
| --- | --- |
| owner 路由归属 | runtime |
| gateway token 行为 | 不解析、不校验、不签发 |
| token 包位置 | `projects/game/runtime/domain/token` |
| gateway 到 runtime | 普通服务调用 |
| runtime read 暴露 | grpc-gateway 聚合 |
| runtime WebSocket owner mismatch | runtime 内部 proxy 到 owner runtime |
| runtime gRPC owner route | Reader RPC 和 RefreshGameRuntime 纳入 owner route；CreateGameRuntime 本地建立 owner |
| pkg/testutil | 删除或内联到 gateway testplan，不再作为共享包 |
| runtime apitest 域名 | `apitest.liukexin.com` |
| runtime apitest prefix | `/game/runtime` |
| runtime 服务级测试入口 | `https://apitest.liukexin.com/game/runtime` |
