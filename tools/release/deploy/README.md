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
| `artifacts[].target` | 指向 `artifact_image` target（见[服务镜像构建](#服务镜像构建)）。 |
| `artifacts[].tls` | 可选，启用 TLS（见[TLS](#tls-配置)）。 |
| `artifacts[].oss` | 可选，启用 OSS（见[OSS](#oss-配置)）。 |
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

业务服务在 `BUILD.bazel` 中使用 `artifact_image` 宏声明镜像：

```python
load("//tools/release:defs.bzl", "artifact_image")

artifact_image(
    name = "cmd_image",
    app = "game",
    service = "session",
    binary = ":cmd",
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

## 部署工具

### 安装

```bash
bazel run //:deploy_install
```

- 默认安装路径：`$HOME/.local/bin`。
- 可通过 `--prefix` 指定路径：`bazel run //tools/release/deploy/v3:install -- --prefix=/path/to/bin`。

### 全局参数

- `--endpoint`：deploy service 地址，默认 `http://infra.liukexin.com:8081`。
- `--timeout`：操作超时，默认 `5m`。

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

支持完整环境名（`alice.dev`）和简版名（`dev`，需配置默认 scope）。

**列出环境**：

```bash
deploy list
```

**配置默认 scope**：

```bash
deploy scope                # 查看
deploy scope {scope-name}   # 设置
```

默认 scope 用于简版环境名补全，为本地仓库级配置。

**路径规则**：

- `//` 开头：按 Bazel 工作区根目录解析。
- 相对路径：按当前 shell 工作目录解析。
- `/` 开头：按系统绝对路径解析。

### 环境名格式

环境唯一标识：`{scope}.{env_name}`（如 `alice.dev`）。

- `scope` 和 `env_name` 须匹配 `^[a-z][a-z0-9]{0,7}$`。
- 输入含 `.` 视为完整环境名，否则视为简版名（需默认 scope）。
- `test` 类型支持 `{{run}}` 占位符，最终名为 `{scope}.{run}`。

## TLS 配置

在 `service.yaml` 中设置 `artifacts[].tls: true` 启用 TLS。

## OSS 配置

在 `service.yaml` 中设置 `artifacts[].oss: true` 启用对象存储（S3 兼容）。凭证由 deploy 工具自动注入（环境变量 `S3_ACCESS_KEY`、`S3_SECRET_KEY`）。

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
- 以下为平台保留变量名，不可使用：`SERVICE_APP`、`DOMINION_ENVIRONMENT`、`POD_NAMESPACE`、`TLS_CERT_FILE`、`TLS_KEY_FILE`、`TLS_CA_FILE`、`TLS_SERVER_NAME`、`S3_ACCESS_KEY`、`S3_SECRET_KEY`。
