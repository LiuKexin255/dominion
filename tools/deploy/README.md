本目录包含部署相关定义与工具

## 部署定义

1. 使用 `service.yaml` 定义需要被部署的服务单元。
2. 使用 `deploy.yaml` 定义部署环境，以及该环境需要部署哪些服务单元。

`deploy.yaml` 示例：

```yaml
name: alice.dev
desc: "开发环境"
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
```

### `deploy.yaml` 中的服务类型

`services` 中的每一项只能二选一：

- `artifact`：引用 `service.yaml` 中定义的服务产物进行部署。
- `infra`：声明一个基础设施实例并由 deploy 工具直接生成对应资源。

同一个 `services[]` 项中，`artifact` 和 `infra` **不能同时出现**。

#### 1. `artifact`：部署服务产物

`artifact` 用于引用一个 `service.yaml` 中定义的产物。当前服务产物类型使用 `type: deployment` 表示会生成常规的 Kubernetes Deployment/Service。

`deploy.yaml` 示例：

```yaml
name: alice.dev
desc: "开发环境"
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
artifacts:
  - name: service
    type: deployment
    target: :service_image
    tls: true
    ports:
      - name: grpc
        port: 50051
```

字段说明：

- `artifact.path`：`service.yaml` 路径。
- `artifact.name`：引用 `service.yaml` 中 `artifacts[].name` 的名称。
- `http`：可选，为该服务额外生成 HTTPRoute；`backend` 需要填写产物里声明的端口名。

说明：

- `type: deployment` 配置在 `service.yaml` 中，而不是 `deploy.yaml` 中。
- deploy 工具会先读取 `service.yaml`，找到对应 `artifact.name`，再根据其 `target` 解析镜像并生成 Deployment/Service。

#### 2. `infra`：部署基础设施实例

`infra` 用于在环境中声明基础设施资源。当前仅支持 `resource: mongodb`。

示例：

```yaml
name: alice.dev
desc: "开发环境"
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
- `infra.profile`：基础设施 profile 名称，从 `tools/deploy/pkg/k8s/static_config.yaml` 中读取对应配置。
- `infra.name`：该基础设施实例名称。
- `infra.app`：该基础设施归属的应用名。
- `infra.persistence.enabled`：是否启用持久化存储。

说明：

- `infra` 不依赖 `service.yaml`。
- 对于 MongoDB，deploy 工具会基于 profile 直接生成所需资源；当 `persistence.enabled: true` 时会额外创建持久化存储相关资源。

#### 3. `artifact` 与 `infra` 组合示例

同一个环境中可以同时包含基础设施和业务服务，例如先声明 MongoDB，再部署依赖它的服务：

```yaml
name: liukexin.demo
desc: "Mongo Demo CRUD 服务"
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
- 可以通过 `--prefix` 参数指定安装路径，例如：`bazel run //tools/deploy/v2:install -- --prefix=/usr/local/bin`。
- 安装完成后，请确保安装路径已添加到系统的 `PATH` 环境变量中。

### 全局参数

- `--endpoint`：deploy service 地址，默认为 `http://infra.liukexin.com:8081`。
- `--timeout`：操作超时时间，默认为 `5m`。

### 相关命令

1. 部署/更新服务

```bash 
deploy apply {path-of-deploy.yaml}
```

- 自动从 `deploy.yaml` 中读取完整环境名。
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
deploy apply experimental/grpc_hello_world/deploy.yaml
```

### 环境名格式

环境唯一标识为完整环境名：`{scope}.{env_name}` (如 `alice.dev`)。

- **命名约束**：`scope` 和 `env_name` 均满足 `^[a-z][a-z0-9]{0,7}$`。
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
