# game session testplan HTTP 测试壳子方案

## 背景

`design/game_gateway_runtime_migration.md` 已明确迁移后 `session` 服务不再提供对外 HTTP 入口：

* 生产公网 `/v1/sessions/**` 由 `gateway` 统一暴露。
* `session` 自身只保留内部 gRPC 服务。
* `projects/game/session/testplan` 不再作为公网 HTTP 测试入口。

但 `session` 服务仍需要保留服务级接口测试，用于验证 `SessionService` 的 contract，包括 create、get、list、reconnect、delete 以及错误场景。由于测试代码目前按 grpc-gateway HTTP contract 编写，且 session 目标态无公网 HTTP 入口，因此需要在 session testplan 中增加一个仅测试使用的 HTTP 路由壳子。

## 目标

本方案要达成：

* `session` 生产服务继续保持 gRPC-only，不恢复 HTTP server。
* `session` 服务级 testplan 可以通过 HTTP 请求验证 session proto 的 grpc-gateway contract。
* 所有类似的 HTTP 测试壳子统一使用域名：

  ```text
  apitest.liukexin.com
  ```

* 不同测试壳子通过 path prefix 区分，prefix 不重复。
* session testplan 使用 prefix：

  ```text
  /game/session
  ```

* session testplan 的最终测试入口为：

  ```text
  https://apitest.liukexin.com/game/session/v1/sessions
  ```

## 非目标

本方案不做：

* 不把 HTTP server 加回 `projects/game/session/app/cmd/main.go`。
* 不修改 `projects/game/session/service.yaml` 的生产暴露模型。
* 不把测试壳子作为生产服务发布。
* 不替代 gateway 系统级大型测试；生产公网路径仍由 `projects/game/gateway/testplan` 验证。
* 不通过壳子绕过 session 的 gRPC 服务实现，壳子只能通过 gRPC client 调用 session backend。

## 路由模型

### 统一测试域名

所有 HTTP 测试壳子统一挂载在：

```text
apitest.liukexin.com
```

每个壳子必须使用唯一 prefix。session 使用：

```text
/game/session
```

### session testplan HTTP 路由

外部测试请求：

```text
POST   https://apitest.liukexin.com/game/session/v1/sessions
GET    https://apitest.liukexin.com/game/session/v1/sessions/{id}
GET    https://apitest.liukexin.com/game/session/v1/sessions
POST   https://apitest.liukexin.com/game/session/v1/sessions/{id}:reconnect
DELETE https://apitest.liukexin.com/game/session/v1/sessions/{id}
```

测试壳子内部处理：

```text
/game/session/v1/**
  -> strip prefix /game/session
  -> /v1/**
  -> grpc-gateway mux
  -> session gRPC backend
```

## 目录设计

新增目录：

```text
projects/game/session/testplan/apitest/
  BUILD.bazel
  main.go
  service.yaml
```

说明：

* `main.go` 直接放在 `apitest` 目录下，不再创建 `app/cmd` 多层目录。
* 该目录只服务于 session testplan。
* `service.yaml` 描述测试壳子的 artifact，不参与生产服务部署。

## 测试壳子实现设计

### main.go 职责

`projects/game/session/testplan/apitest/main.go` 负责：

1. 启动 HTTP server。
2. 创建到 session gRPC backend 的 client connection。
3. 注册 session proto grpc-gateway handler。
4. 将 `/game/session` prefix 下的请求 strip 后交给 grpc-gateway mux。

建议常量：

```go
const (
    envHTTPPort = "HTTP_PORT"

    defaultHTTPListenAddr = ":8080"
    routePrefix           = "/game/session"
    shutdownTimeout       = 5 * time.Second
)
```

后端 target 使用公共常量：

```go
gameconst.TargetSessionGRPC
```

注册方式使用 gRPC client，而不是直接构造 session handler：

```go
sessionpb.RegisterSessionServiceHandler(ctx, mux, sessionConn)
```

这样测试链路仍然经过真实 gRPC 边界：

```text
HTTP test client -> apitest shell -> grpc-gateway -> session gRPC -> session service/domain/storage/runtime client
```

### 路由处理

测试壳子使用标准 HTTP prefix strip 逻辑：

```go
handler := http.StripPrefix(routePrefix, mux)
router.Handle(routePrefix+"/", handler)
```

需要注意：

* `/game/session/v1/sessions` strip 后必须是 `/v1/sessions`。
* `/game/session` 本身可返回 404，不需要额外健康检查。
* 壳子不实现业务路由，不解析 session id，不处理业务错误。

### 错误边界

测试壳子只负责协议转换与路由：

* gRPC 业务错误由 grpc-gateway 按 session proto contract 映射到 HTTP。
* 壳子不包装 session 返回的错误语义。
* 壳子不做鉴权、token 签发、owner 决策或 runtime 路由。

## service.yaml 设计

新增：

```text
projects/game/session/testplan/apitest/service.yaml
```

建议内容：

```yaml
version: "3.0"
name: session-apitest
app: game
desc: game session test-only HTTP api shell
kind: stateless
artifacts:
  - name: cmd
    target: :cmd_image
    tls: true
    ports:
      - name: http
        port: 8080
```

约束：

* `session-apitest` 只是测试部署名。
* 不写入 `projects/game/session/service.yaml`。
* 不作为生产网关或生产 session 入口。

## BUILD 设计

新增：

```text
projects/game/session/testplan/apitest/BUILD.bazel
```

目标包括：

* `go_binary`：编译 `main.go`。
* `oci_image` 或仓库现有镜像规则：提供 `service.yaml` 的 `cmd_image` target。

依赖包括：

* `//common/gopkg/bootstrap`
* `//common/gopkg/grpc`
* `//common/gopkg/grpc/solver`
* `//common/gopkg/http`（如需要沿用仓库 HTTP wrapper）
* `//common/gopkg/otel`
* `//projects/game/pkg/const`
* `//projects/game/session`
* `@grpc_ecosystem_grpc_gateway//runtime`
* `@org_golang_google_grpc//:grpc`

实际 BUILD 文件应由 gazelle 生成/更新；如镜像 target 需要手工补充，应在 gazelle 后追加，不改动 gazelle 生成内容。

## session testplan 部署修改

修改：

```text
projects/game/session/testplan/test_deploy.yaml
```

目标部署对象：

1. mongo
2. runtime
3. session
4. session-apitest

建议结构：

```yaml
services:
  - infra:
      app: game
      resource: mongodb
      profile: dev-single
      name: mongo

  - artifact:
      path: //projects/game/runtime/service.yaml
      name: cmd
      env:
        SESSION_TOKEN_SECRET: "dev-session-token-secret"
        SESSION_TOKEN_TTL: "5m"
        SESSION_IDLE_TTL: "90s"
        GRPC_PORT: "8082"
    http:
      hostnames:
        - runtime.game.liukexin.com
      matches:
        - backend: http
          path:
            type: PathPrefix
            value: /v1
    grpc:
      hostnames:
        - runtime.game.liukexin.com
      matches:
        - backend: grpc
          path:
            type: PathPrefix
            value: /

  - artifact:
      path: //projects/game/session/service.yaml
      name: cmd
      env:
        SESSION_TOKEN_SECRET: "dev-session-token-secret"
    grpc:
      hostnames:
        - session.game.liukexin.com
      matches:
        - backend: grpc
          path:
            type: PathPrefix
            value: /

  - artifact:
      path: //projects/game/session/testplan/apitest/service.yaml
      name: cmd
    http:
      hostnames:
        - apitest.liukexin.com
      matches:
        - backend: http
          path:
            type: PathPrefix
            value: /game/session
```

说明：

* session 不再配置 HTTP route。
* session-apitest 是唯一暴露到 `apitest.liukexin.com/game/session` 的 HTTP 入口。
* runtime 是否需要 HTTP route 取决于 session 测试是否覆盖 runtime HTTP/WS 交互；如果 session 接口测试只依赖 session 调用 runtime gRPC lifecycle，则 runtime 的 gRPC route 是必要项，HTTP route 可按实际测试依赖保留或删除。

## interface_test.yaml 修改

修改：

```text
projects/game/session/testplan/interface_test.yaml
```

endpoint 从：

```yaml
endpoint:
  http:
    public: https://game.liukexin.com
```

改为：

```yaml
endpoint:
  http:
    public: https://apitest.liukexin.com/game/session
```

这样现有测试代码中：

```go
sutHostURL + "/v1/sessions"
```

最终会请求：

```text
https://apitest.liukexin.com/game/session/v1/sessions
```

原则上 `projects/game/session/testplan/interface_test.go` 不需要为了 prefix 修改路径常量。

## interface_test.go 调整原则

优先不改测试逻辑，只通过 endpoint 调整完成路径迁移。

如果需要更新注释或错误信息，应将描述从“session HTTP public API”改为：

```text
session test-only HTTP apitest shell -> session gRPC contract
```

测试覆盖仍保持：

1. CreateSession 成功。
2. GetSession 成功。
3. ReconnectSession 成功。
4. DeleteSession 成功。
5. GetSession not found 返回 404。
6. CreateSession invalid type 返回 400。
7. ListSessions 返回 active session list。

## 与 gateway 系统级测试的边界

session testplan 验证：

```text
apitest shell -> session gRPC contract
```

gateway testplan 验证：

```text
game.liukexin.com -> gateway -> session/runtime
```

两者职责不同：

* session testplan 关注 session 服务自身 contract。
* gateway testplan 关注统一公网入口、聚合路由、owner proxy、WebSocket 与 runtime 路由。

因此 session testplan 不应继续使用 `game.liukexin.com`，避免与生产 gateway 入口语义混淆。

## 开发步骤

### Step 1：新增 apitest 壳子

新增：

```text
projects/game/session/testplan/apitest/main.go
projects/game/session/testplan/apitest/service.yaml
```

实现 HTTP -> grpc-gateway -> session gRPC 转发，并使用 `/game/session` prefix。

### Step 2：生成/补充 BUILD

执行：

```bash
bazel run //:gazelle projects/game/session/testplan/apitest
```

如镜像 target 未自动生成，按仓库现有 `service.yaml` artifact 规则补充 `cmd_image` target。

### Step 3：修改 session testplan 部署

修改：

```text
projects/game/session/testplan/test_deploy.yaml
```

将 HTTP route 从 session 服务移到 session-apitest 壳子，并使用：

```text
apitest.liukexin.com + /game/session
```

### Step 4：修改 session testplan endpoint

修改：

```text
projects/game/session/testplan/interface_test.yaml
```

设置：

```text
https://apitest.liukexin.com/game/session
```

### Step 5：验证

执行构建验证：

```bash
bazel build //projects/game/session/testplan/...
```

执行单个大型测试 target 构建/测试：

```bash
bazel test //projects/game/session/testplan:testplan_test
```

执行 testplan：

```text
projects/game/session/testplan/interface_test.yaml
```

## 验收标准

完成后应满足：

* `projects/game/session/service.yaml` 仍只声明 gRPC 端口。
* `projects/game/session/app/cmd/main.go` 不启动 HTTP server。
* `projects/game/session/testplan/apitest/main.go` 直接位于 `apitest` 目录。
* `https://apitest.liukexin.com/game/session/v1/sessions` 可以完成 CreateSession。
* session testplan 中所有原 HTTP contract 用例通过。
* `game.liukexin.com` 不再用于 session 服务级 testplan。
* gateway 系统级 testplan 继续负责验证生产公网入口。
* 新增测试壳子 prefix `/game/session` 不与其他 apitest 壳子冲突。

## 决策记录

| 决策 | 结论 |
| --- | --- |
| 测试域名 | `apitest.liukexin.com` |
| session 测试 prefix | `/game/session` |
| main.go 位置 | `projects/game/session/testplan/apitest/main.go` |
| 是否恢复 session HTTP | 不恢复 |
| 壳子调用方式 | grpc-gateway 通过 gRPC client 调用 session backend |
| 生产公网入口测试 | 仍由 gateway testplan 覆盖 |
| session testplan endpoint | `https://apitest.liukexin.com/game/session` |
