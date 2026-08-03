本目录包含部署相关定义与工具。

## 配置文件

### deploy.yaml

定义部署环境及其中包含的服务。

```yaml
version: "3.0"
name: alice.dev
desc: "开发环境"
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
      env:                        # 可选：环境变量
        LOG_LEVEL: debug
```

顶层字段：

- `version`：必填，固定为 `"3.0"`。
- `name`：环境名（完整环境名格式见[环境名](#环境名格式)）。
- `desc`：环境描述。
- `type`：必填，环境类型。

#### 环境类型

| type | 说明 |
|------|------|
| `prod` | 生产环境，按域名和 path 直接访问。 |
| `test` | 测试环境，访问 HTTPRoute 时需携带 `env` header（值为完整环境名）。 |
| `dev` | 开发环境，访问规则同 `test`。 |

`test` 类型支持 `name` 中使用 `{{run}}` 占位符，部署时由 `--run` 参数替换：

```yaml
version: "3.0"
name: game.{{run}}
type: test
services:
  - artifact:
      path: //projects/game/service.yaml
      name: gateway
```

```bash
deploy apply --run lt3x8q2 //path/to/deploy.yaml   # 部署后环境名：game.lt3x8q2
```

约束：`--run` 值须匹配 `^[a-z][a-z0-9]{0,7}$`；`deploy.yaml` 中不含 `{{run}}` 时传 `--run` 会报错。

#### 服务类型

`services` 中每项为 `artifact`（部署服务产物）或 `infra`（部署基础设施），二者不可同时出现。

##### artifact

引用 `service.yaml` 定义的服务产物：

```yaml
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
      replicas: 3              # 可选，默认 1
      secrets:                 # 可选：逻辑名到 K8s Secret 的绑定
        database-url:
          secret: prod-db
          key: username
    http:                      # 可选：生成 HTTPRoute
      hostnames:
        - hello.example.com
      matches:
        - backend: grpc
          path:
            type: PathPrefix
            value: /v1
```

对应的 `service.yaml`：

```yaml
version: "3.0"
name: service
app: grpc-hello-world
desc: grpc hello world service
kind: stateless          # stateless（默认）或 stateful
artifacts:
  - name: service
    target: :cmd_image    # 指向 artifact_image target
    tls: true             # 可选：启用 TLS
    oss: false            # 可选：启用 OSS
    secrets:              # 可选：声明所需 secret 逻辑名
      - database-url
      - stripe-api-key
    ports:
      - name: grpc
        port: 50051
```

字段说明：

| 字段 | 说明 |
|------|------|
| `artifact.path` | `service.yaml` 路径。 |
| `artifact.name` | 引用 `artifacts[].name` 的名称。 |
| `artifact.env` | 可选，环境变量 key-value。 |
| `artifact.secrets` | 可选，对象。键为 service.yaml 中声明的逻辑名，值为 `{secret, key}` 映射到 Kubernetes Secret（见[Secret 配置](#secret-配置)）。 |
| `artifacts[].target` | 指向 `artifact_image` target（见[服务镜像构建](#服务镜像构建)）。 |
| `artifacts[].tls` | 可选，启用 TLS（见[TLS](#tls-配置)）。 |
| `artifacts[].oss` | 可选，启用 OSS（见[OSS](#oss-配置)）。 |
| `artifacts[].secrets` | 可选，字符串数组，声明该产物运行时所需的 Kubernetes Secret 逻辑名。每个名字须匹配 `^[a-z][a-z0-9_-]{0,63}$`（见[Secret 配置](#secret-配置)）。 |
| `kind` | `stateless`（默认）或 `stateful`。 |
| `http` | 可选，生成 HTTPRoute；`backend` 填写端口名。 |

当 `kind: stateful` 时，deploy 生成 StatefulSet 及配套资源。配置的 `hostnames` 会根据副本数展开为实例域名：`{service}-{N}.{hostname}`（如 `game-gateway-0.gateway.example.com`）。

##### infra

声明基础设施实例，当前支持 `resource: mongodb`：

```yaml
services:
  - infra:
      resource: mongodb
      profile: dev-single
      name: mongo
      app: hello-world
      persistence:
        enabled: true
```

`infra` 不依赖 `service.yaml`，deploy 工具直接生成对应资源。

## 服务镜像构建

业务服务在 `BUILD.bazel` 中使用 `artifact_pkg_go`/`artifact_pkg_js` 打包，再用 `artifact_image` 构建镜像。打包规则生成常规 Bazel target，`artifact_image` 通过 `pkg` label 引用该 target，并根据其 provider 自动选择基础镜像和启动参数。

### 打包规范

#### Go 规范（`type = "go"`）

- 二进制放置于 `/dominion/{app}/{service}/bin/{binary_name}`。
- 基础镜像：`@distroless_base`。
- 容器启动：`ENTRYPOINT ["/dominion/{app}/{service}/bin/{binary_name}"]`。

#### JS 规范（`type = "js"`）

- `ts_project` 编译产出的 JS 文件按相对包目录的路径放置于 `/dominion/{app}/{service}/` 下。
- `runtime_protos` 中的 proto 文件按标准导入路径放置于同一目录下。
- `npm_deps` 中的 Node 模块放于 `/dominion/{app}/{service}/node_modules/` 下。
- 基础镜像：`@distroless_nodejs`。
- 容器启动：`ENTRYPOINT ["node"]`，`CMD ["/dominion/{app}/{service}/{entrypoint}"]`。

### Go 服务

```python
load("//tools/release:defs.bzl", "artifact_image", "artifact_pkg_go")

# 将 Go 二进制打包为 tar
artifact_pkg_go(
    name = "service_pkg",
    app = "game",
    binary = ":cmd",       # go_binary 标签
    service = "session",
)

# 从打包信息构建 OCI 镜像
artifact_image(
    name = "cmd_image",
    app = "game",
    pkg = ":service_pkg",
    service = "session",
)
```

`service.yaml` 中 `artifacts[].target` 指向 `artifact_image` target：

```yaml
version: "3.0"
name: session
app: game
kind: stateless
artifacts:
  - name: cmd
    target: app/cmd:cmd_image
    ports:
      - name: http
        port: 8081
```

deploy CLI 会校验 `service.yaml` 中的 `app`/`name` 与 `artifact_image` 声明一致。

### Node.js / grpc-js 服务

Node/grpc-js 服务使用 `artifact_pkg_js` 打包文件，需通过 `entrypoint` 参数指定入口脚本：

```python
load("//tools/release:defs.bzl", "artifact_image", "artifact_pkg_js")

# 将 JS 服务文件打包为 tar
artifact_pkg_js(
    name = "server_pkg",
    app = "my-app",
    entrypoint = "src/server.js",  # 入口脚本（相对于 ts_project 包目录）
    ts_project = ":server",         # ts_project target，自动收集编译后的 JS 文件
    runtime_protos = [              # proto_library targets，自动收集传递依赖的 proto 文件
        ":greeter_proto",
    ],
    npm_deps = [                    # 运行时 npm 依赖
        ":node_modules/@grpc/grpc-js",
        ":node_modules/@grpc/proto-loader",
    ],
    service = "service",
)

# 从打包信息构建 OCI 镜像
artifact_image(
    name = "cmd_image",
    app = "my-app",
    pkg = ":server_pkg",
    service = "service",
)
```

### 规则说明

| 规则 | 说明 |
|------|------|
| `artifact_pkg_go(name, app, service, binary)` | 将 Go 二进制打包为 tar，放置于 `/dominion/{app}/{service}/bin/{binary_name}`。生成带 `ArtifactPkgInfo` 的常规 target。 |
| `artifact_pkg_js(name, app, service, ts_project, entrypoint, runtime_protos, npm_deps)` | 将 JS 文件打包为 tar。`ts_project` 指向 ts_project target，自动收集编译后的 JS 文件。`runtime_protos` 为 proto_library target 列表，自动收集传递依赖的 proto 文件（按标准导入路径放置）。`npm_deps` 为运行时 npm 依赖列表。`entrypoint` 指定入口脚本（相对于 ts_project 包目录）。生成带 `ArtifactPkgInfo` 的常规 target。 |
| `artifact_image(name, app, service, pkg)` | 从打包 target 构建 OCI 镜像。根据打包类型自动选择基础镜像、entrypoint 和 cmd。 |

## 部署工具

### 安装

```bash
bazel run //:deploy_install
```

- 默认安装路径：`$HOME/.local/bin`。
- 可通过 `--prefix` 指定路径：`bazel run //tools/release/deploy/v3:install -- --prefix=/path/to/bin`。

### 全局参数

- `--endpoint`：deploy service 地址，默认 `http://infra.liukexin.com`。
- `--timeout`：操作超时，默认 `5m`。
- `-v, --verbose`：打印 trace ID 等隐藏信息。

### 命令

**部署/更新**：

```bash
deploy apply [--run <id>] {path-of-deploy.yaml}
```

自动从 `deploy.yaml` 读取环境名，推送镜像后将 desired state 提交给 deploy service。`apply` 采用全量替换语义：配置中移除的服务会被自动清理。

**删除环境**：

```bash
deploy del {env-name}
```

`env-name` 须为完整环境名（`{scope}.{env_name}` 格式，如 `alice.dev`），不支持简版名。

**列出环境**：

```bash
deploy list [--scope=name]
```

`--scope` 可选：指定时只列出该 scope 下的环境；不指定时列出所有 scope 的环境。

**查看环境详情**：

```bash
deploy describe {env-name}
```

打印单个部署环境的详细状态：环境名、状态（中文描述）、服务列表（应用服务与基础设施）、最近调和与最近成功时间。服务列表每项内联 **per-service rollout 状态**（来自 deploy service 的 `Environment.status.services`）：`就绪`（READY）、`等待发布: {原因}`（WAITING）、`失败: {原因}`（FAILED）、`已提交，等待观测`（PENDING，资源已提交、首次 rollout 观测尚未完成）；`status.services` 无该服务匹配项时不追加状态文本（兼容旧版服务端，回退纯服务列表）。`说明:` 行仅在无 per-service 数据（`status.services` 为空）且环境级 message 非空时输出，用于表达 apply 失败、retry-exhausted 等非 rollout 原因。数据来自 deploy service 的环境状态，单次查询无轮询。环境不存在时输出 `环境 {env-name} 不存在` 提示并以非零退出码返回。

输出示例（滚动发布进行中，per-service 状态主线，`说明:` 不输出）：

```
环境 game.lt3x8q2
状态: 等待滚动发布
服务:
  - service (app=game) [artifact] 就绪
  - gateway (app=game) [artifact] 等待发布: 可用副本不足（available: 0/1）
最近调和: 2026-08-03T10:30:05Z
最近成功: -
```

输出字段顺序与格式见 `../../../specs/032-guitar-deploy-failure-state/contracts/deploy-describe.md`。

**路径规则**：

- `//` 开头：按 Bazel 工作区根目录解析。
- 相对路径：按当前 shell 工作目录解析。
- `/` 开头：按系统绝对路径解析。

### 环境名格式

环境唯一标识：`{scope}.{env_name}`（如 `alice.dev`）。

- `scope` 和 `env_name` 须匹配 `^[a-z][a-z0-9]{0,7}$`。
- 环境名始终使用完整 `{scope}.{env_name}` 格式（如 `alice.dev`），不支持简版名。
- `test` 类型支持 `{{run}}` 占位符，最终名为 `{scope}.{run}`。

## TLS 配置

在 `service.yaml` 中设置 `artifacts[].tls: true` 启用 TLS。

## OSS 配置

在 `service.yaml` 中设置 `artifacts[].oss: true` 启用对象存储（S3 兼容）。凭证由 deploy 工具自动注入（环境变量 `S3_ACCESS_KEY`、`S3_SECRET_KEY`）。

## Secret 配置

在 `service.yaml` 中声明 secret 逻辑名，在 `deploy.yaml` 中将逻辑名绑定到 Kubernetes Secret。

```yaml
# service.yaml
version: "3.0"
name: orders-api
app: orders
kind: stateless
artifacts:
  - name: api
    target: :api_image
    secrets:
      - database-url
      - stripe-api-key
```

```yaml
# deploy.yaml
version: "3.0"
name: orders.prod
desc: "orders production environment"
type: prod
services:
  - artifact:
      path: //projects/orders/service.yaml
      name: api
      secrets:
        database-url:
          secret: orders-prod-secrets
          key: DATABASE_URL
        stripe-api-key:
          secret: orders-prod-secrets
          key: STRIPE_API_KEY
```

Secret 名称规则：
- 必须以小写字母开头，只允许小写字母、数字、下划线、连字符。
- 最长 63 字符：`^[a-z][a-z0-9_-]{0,63}$`。
- 同一 artifact 内不可重复。

部署时，deploy 工具会校验：
- 每个声明的 secret 必须有绑定（否则拒绝部署）。
- 不允许绑定未声明的 secret（防止拼写错误或过期配置）。

运行时，secret 文件通过 projected volume 挂载到容器：
- 环境变量 `DOMINION_SECRET_DIR` 指向 `/mnt/dominion/secret`（平台保留，自动注入）。
- 文件路径为 `/mnt/dominion/secret/{logical_name}`。

```bash
# 读取 secret 文件示例
cat /mnt/dominion/secret/database-url
cat /mnt/dominion/secret/stripe-api-key
```

## 环境变量配置

在 `deploy.yaml` 中通过 `artifact.env` 配置：

```yaml
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
      env:
        LOG_LEVEL: debug
        DATABASE_URL: "postgres://localhost:5432/mydb"
```

- key 须匹配 `^[a-zA-Z_][a-zA-Z0-9_]*$`。
- 以下为平台保留变量名，不可使用：`SERVICE_APP`、`DOMINION_ENVIRONMENT`、`POD_NAMESPACE`、`TLS_CERT_FILE`、`TLS_KEY_FILE`、`TLS_CA_FILE`、`TLS_SERVER_NAME`、`S3_ACCESS_KEY`、`S3_SECRET_KEY`、`DOMINION_SECRET_DIR`。
