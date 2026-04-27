本目录包含部署相关定义与工具

## 部署定义

1. 使用 `service.yaml` 定义需要被部署的服务单元。
2. 使用 `deploy.yaml` 定义部署环境，以及该环境需要部署哪些服务单元。

`deploy.yaml` 示例：

```yaml
name: alice.dev
desc: "开发环境"
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
```

带环境变量的 `deploy.yaml` 示例：

```yaml
name: alice.dev
desc: "开发环境"
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
      env:
        LOG_LEVEL: debug
        TIMEOUT_MS: "1500"
```

### `deploy.yaml` 中的环境类型

`type` 是 `deploy.yaml` 的必填顶层字段，用于声明环境用途。可选值：

- `prod`：生产环境。
- `test`：测试环境。
- `dev`：开发环境。

示例：

```yaml
name: alice.dev
desc: "开发环境"
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
```

说明：

- `type` 必须显式填写，不能省略。
- `prod` 类型环境按域名和 path 访问即可。
- `test` 和 `dev` 类型环境如果配置了 `http`，访问对应 HTTPRoute 时需要在请求中携带 `env` header，值为完整环境名。

例如，下面的 `test` 环境：

```yaml
name: alice.test
desc: "测试环境"
type: test
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
    http:
      hostnames:
        - hello.example.com
      matches:
        - backend: grpc
          path:
            type: PathPrefix
            value: /v1
```

访问该 HTTPRoute 时，请求需要携带：

```http
env: alice.test
```

因此访问 `test` 或 `dev` 类型环境的 HTTPRoute 时，客户端需要在请求中带上 `env` header，且值等于完整环境名（如 `alice.test`、`alice.dev`）。

### 动态测试环境名

`test` 类型 deploy 的 `name` 支持占位符 `{{run}}`，用于在部署时动态生成环境名。

示例：

```yaml
name: game.{{run}}
desc: "动态测试环境"
type: test
services:
  - artifact:
      path: //projects/game/service.yaml
      name: gateway
```

使用 `deploy apply --run` 部署时，`{{run}}` 会被替换为传入的 run 标识：

```bash
deploy apply --run lt3x8q2 //projects/game/testplan/test_deploy.yaml
```

部署后的完整环境名为 `game.lt3x8q2`。

约束：

- 只有 `type: test` 允许使用 `{{run}}`，`prod` 和 `dev` 类型禁止使用。
- `--run` 值须满足命名规则 `^[a-z][a-z0-9]{0,7}$`。
- `deploy.yaml` 中不含 `{{run}}` 时传 `--run` 会报错。
- 不支持多个 `{{run}}`，不支持其他占位符。

### `deploy.yaml` 中的服务类型

`services` 中的每一项只能二选一：

- `artifact`：引用 `service.yaml` 中定义的服务产物进行部署。
- `infra`：声明一个基础设施实例并由 deploy 工具直接生成对应资源。

同一个 `services[]` 项中，`artifact` 和 `infra` **不能同时出现**。

#### 1. `artifact`：部署服务产物

`artifact` 用于引用一个 `service.yaml` 中定义的产物。通过 `service.yaml` 顶层 `kind` 字段定义运行时工作负载类型。

`deploy.yaml` 示例：

```yaml
name: alice.dev
desc: "开发环境"
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
    http:
      hostnames:
        - hello.example.com
      matches:
        - backend: grpc
          path:
            type: PathPrefix
            value: /v1
```

对应的 `service.yaml` 示例：

```yaml
name: service
app: grpc-hello-world
desc: grpc hello world service
kind: stateless
artifacts:
  - name: service
    target: :service_image
    tls: true
    oss: false
    ports:
      - name: grpc
        port: 50051
```

字段说明：

- `artifact.path`：`service.yaml` 路径。
- `artifact.name`：引用 `service.yaml` 中 `artifacts[].name` 的名称。
- `artifact.env`：可选，为产物配置环境变量，key 为变量名，value 为明文值。
- `artifacts[].tls`：可选，为产物启用 TLS，注入证书相关环境变量。
- `artifacts[].oss`：可选，为产物启用 OSS（对象存储服务），注入 S3 凭证环境变量。
- `http`：可选，为该服务额外生成 HTTPRoute；`backend` 需要填写产物里声明的端口名。

说明：

- `kind` 配置在 `service.yaml` 中，可选值为 `stateless` (默认) 或 `stateful`。
- `artifacts[].type` 字段已被移除，请使用 `kind` 代替。
- deploy 工具会先读取 `service.yaml`，找到对应 `artifact.name`，再根据其 `target` 解析镜像并根据 `kind` 生成对应的 Kubernetes 资源。

#### 2. `stateful`：部署有状态服务

当 `service.yaml` 中配置 `kind: stateful` 时，deploy 工具会生成 Kubernetes StatefulSet 及其配套资源。

`deploy.yaml` 示例：

```yaml
name: game.prod
desc: "生产环境"
type: prod
services:
  - artifact:
      path: //projects/game/service.yaml
      name: gateway
      replicas: 3
    http:
      hostnames:
        - gateway.example.com
      matches:
        - backend: tcp
          path:
            type: PathPrefix
            value: /v1
```

对应的 `service.yaml` 示例：

```yaml
name: gateway
app: game-gateway
kind: stateful
artifacts:
  - name: gateway
    target: :gateway_image
    ports:
      - name: tcp
        port: 8080
```

**生成的资源：**

- **StatefulSet**：管理有状态 Pod 实例。
- **Governing Service**：Headless Service，用于维护 Pod 的稳定网络身份。
- **Per-instance Service**：为每个 Pod 实例生成一个独立的 Service（如 `game-gateway-0`），通过标签精确选中对应 Pod。
- **Per-instance HTTPRoute**：为每个实例生成独立的路由规则。

**域名展开规则：**

对于 `stateful` 服务，`http.hostnames` 中配置的是**基础域名**。deploy service 会根据副本数自动展开为实例域名：
`{service_name}-{N}.{hostname}` (N 为从 0 开始的实例索引)

例如上述配置（3 副本）将生成以下域名：
- `game-gateway-0.gateway.example.com`
- `game-gateway-1.gateway.example.com`
- `game-gateway-2.gateway.example.com`

若配置了多个基础域名，则每个实例都会对应多个展开后的域名。

**限制与说明：**

- `stateful` 的 `http` 配置格式与 `stateless` 完全一致，均需提供 `hostnames` 和完整的 `matches` 规则。
- **缩容行为**：当 `replicas` 减小时，被移除实例对应的 Service 和 HTTPRoute 会立即被删除，相关域名立即失效。
- **域名展开时机**：域名展开由 deploy service 端在生成资源时完成，CLI 编译器仅负责校验和传输基础域名。

#### 3. `infra`：部署基础设施实例

`infra` 用于在环境中声明基础设施资源。当前仅支持 `resource: mongodb`。

示例：

```yaml
name: alice.dev
desc: "开发环境"
type: dev
services:
  - infra:
      resource: mongodb
      profile: dev-single
      name: mongo
      app: hello-world
      persistence:
        enabled: true
```

字段说明：

- `infra.resource`：基础设施类型，当前只支持 `mongodb`。
- `infra.profile`：基础设施 profile 名称，从 `tools/release/deploy/pkg/k8s/static_config.yaml` 中读取对应配置。
- `infra.name`：该基础设施实例名称。
- `infra.app`：该基础设施归属的应用名。
- `infra.persistence.enabled`：是否启用持久化存储。

说明：

- `infra` 不依赖 `service.yaml`。
- 对于 MongoDB，deploy 工具会基于 profile 直接生成所需资源；当 `persistence.enabled: true` 时会额外创建持久化存储相关资源。

#### 4. `artifact` 与 `infra` 组合示例

同一个环境中可以同时包含基础设施和业务服务，例如先声明 MongoDB，再部署依赖它的服务：

```yaml
name: liukexin.demo
desc: "Mongo Demo CRUD 服务"
type: dev
services:
  - infra:
      app: mongo-demo
      resource: mongodb
      profile: dev-single
      name: mongo
      persistence:
        enabled: true
  - artifact:
      path: //experimental/mongo_demo/cmd/service.yaml
      name: cmd
    http:
      hostnames:
        - mongo-demo.liukexin.com
      matches:
        - backend: http
          path:
            type: PathPrefix
            value: /v1
```

## 部署工具

通过部署工具 `deploy` 将 `deploy.yaml` 定义的环境连同其中包含的服务部署到集群中。

> **注意**：当前工具已切换到 v2 语义，通过调用 `deploy service` 实现部署，不再直接连接 Kubernetes 集群。

### 安装

使用以下命令安装 `deploy` 工具：

```bash
bazel run //:deploy_install
```

- 默认安装路径为 `$HOME/.local/bin`。
- 可以通过 `--prefix` 参数指定安装路径，例如：`bazel run //tools/release/deploy/v2:install -- --prefix=/usr/local/bin`。
- 安装完成后，请确保安装路径已添加到系统的 `PATH` 环境变量中。

### 全局参数

- `--endpoint`：deploy service 地址，默认为 `http://infra.liukexin.com:8081`。
- `--timeout`：操作超时时间，默认为 `5m`。

### 相关命令

1. 部署/更新服务

```bash
deploy apply [--run <id>] {path-of-deploy.yaml}
```

- 自动从 `deploy.yaml` 中读取完整环境名。
- `--run`：当 `deploy.yaml` 的 `name` 包含 `{{run}}` 时，指定动态 run 标识。
- CLI 解析配置并推送镜像后，将 desired state 发送给 deploy service。
- CLI 会轮询环境状态，直到达到 `READY` 或 `FAILED` 终态，或操作超时。
- `apply` 采用 **full desired-state replacement** 语义：配置中移除的服务或资源会被自动清理。

2. 删除环境

```bash
deploy del {env-name}
```

- 支持完整环境名（如 `alice.dev`）和简版环境名（如 `dev`）。
- 调用 deploy service 发起删除流程并轮询结果。

3. 列出环境

```bash
deploy list
```

- 从 deploy service 获取所有环境列表。
- 输出格式为完整环境名，例如 `alice.dev`。

4. 配置默认 scope

```bash
deploy scope                # 查看当前默认 scope
deploy scope {scope-name}   # 设置默认 scope
```

- 默认 scope 仅用于 CLI 输入时补全简版环境名。
- 默认 scope 是本地仓库级配置。

5. `deploy.yaml` 文件路径规则

- 以 `//` 开头：按项目 Bazel 工作区根目录（包含 WORKSPACE.bazel 或 MODULE.bazel 的目录）解析。
- 不以 `/` 开头的相对路径：按当前 shell 工作目录解析。
- 以 `/` 开头：按系统绝对路径解析。

注意：`deploy` 工具必须在 Bazel 工作区内运行，否则会返回错误。

示例：

```bash
deploy apply //experimental/grpc_hello_world/deploy.yaml
deploy apply --run lt3x8q2 //projects/game/testplan/test_deploy.yaml
deploy apply experimental/grpc_hello_world/deploy.yaml
```

### 环境名格式

环境唯一标识为完整环境名：`{scope}.{env_name}` (如 `alice.dev`)。

- **命名约束**：`scope` 和 `env_name` 均满足 `^[a-z][a-z0-9]{0,7}$`。
- **动态环境名**：`test` 类型 deploy 支持使用 `{{run}}` 占位符，部署时由 `--run` 参数替换，最终完整环境名为 `{scope}.{run}`。
- **简版名**：仅在 CLI 输入时使用，需要配置默认 scope。
- **解析规则**：
  - 输入包含 `.` 时，视为完整环境名。
  - 输入不含 `.` 时，视为简版名，使用默认 scope 拼接。
  - 未配置默认 scope 时，简版输入将报错。

## TLS 配置

服务可以通过在 `service.yaml` 的 `artifacts[].tls` 字段设置为 `true` 来启用 TLS：

运行时 TLS 配置在 `static_config.yaml` 中定义：

```yaml
tls:
  secret_name: "my-service-tls"
  secret_namespace: "default"
  server_name: "my-service.default.svc.cluster.local"
  ca_file: "/etc/tls/ca.crt"
```

当 TLS 启用时，部署工具会注入以下环境变量：
- `TLS_CERT_FILE`
- `TLS_KEY_FILE`
- `TLS_CA_FILE`
- `TLS_SERVER_NAME`

## OSS 配置

服务可以通过在 `service.yaml` 的 `artifacts[].oss` 字段设置为 `true` 来启用 OSS（对象存储服务）：

```yaml
artifacts:
  - name: service
    target: :service_image
    oss: true
    ports:
      - name: http
        port: 8080
```

运行时 OSS 配置在 `static_config.yaml` 中定义：

```yaml
oss:
  secret: "seaweedfs-s3-credentials"
  access_key: "accessKey"
  secret_key: "secretKey"
```

字段说明：

- `oss.secret`：包含 S3 凭证的 Kubernetes Secret 名称。
- `oss.access_key`：Secret 中 access key 的键名。
- `oss.secret_key`：Secret 中 secret key 的键名。

当 OSS 启用时，部署工具会注入以下环境变量（通过 SecretKeyRef）：
- `S3_ACCESS_KEY`
- `S3_SECRET_KEY`

## 环境变量配置

在 `deploy.yaml` 中，可以通过 `artifact.env` 字段为服务产物配置环境变量：

```yaml
name: alice.dev
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
      env:
        LOG_LEVEL: debug
        DATABASE_URL: "postgres://localhost:5432/mydb"
```

**说明：**

- `env` 是一个 key-value 对象，key 为环境变量名，value 为明文值（必须为字符串类型）。
- 环境变量名必须符合 POSIX 标准：`^[a-zA-Z_][a-zA-Z0-9_]*$`。
- 以下环境变量名被平台保留，不可使用：
  - `SERVICE_APP`
  - `DOMINION_ENVIRONMENT`
  - `POD_NAMESPACE`
  - `TLS_CERT_FILE`
  - `TLS_KEY_FILE`
  - `TLS_CA_FILE`
  - `TLS_SERVER_NAME`
  - `S3_ACCESS_KEY`
  - `S3_SECRET_KEY`
- 用户定义的环境变量会按 key 的字典序排列，并在保留变量之前注入容器。
