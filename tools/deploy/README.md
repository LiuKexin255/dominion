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

## 部署工具

通过部署工具 `deploy` 将 `deploy.yaml` 定义的环境连同其中包含的服务部署到 k8s 当中。

### 安装

使用以下命令安装 `deploy` 工具：

```bash
bazel run //:deploy_install
```

- 默认安装路径为 `$HOME/.local/bin`。
- 可以通过 `--prefix` 参数指定安装路径，例如：`bazel run //:deploy_install -- --prefix=/usr/local/bin`。
- 安装完成后，请确保安装路径已添加到系统的 `PATH` 环境变量中。

### 相关命令

1. 创建/切换环境

```bash
deploy use {env-name}
```

- 支持完整环境名（如 `alice.dev`）和简版环境名（如 `dev`）。
- 简版环境名会使用默认 scope 自动补全。
- 如果环境不存在，则创建环境；如存在则切换环境。

2. 部署/更新服务

```bash 
deploy apply [--kubeconfig={path}] {path-of-deploy.yaml}
```

- `apply` 不再要求预先执行 `use`。
- 自动从 `deploy.yaml` 中读取完整环境名。
- 若目标环境不存在，则自动创建环境。
- 部署成功后，该环境将被设置为当前激活环境。
- `--kubeconfig` 可选；不传时按 client-go 默认规则加载（例如 `KUBECONFIG` 或 `~/.kube/config`）。

3. 删除环境

```bash
deploy del {env-name} [--kubeconfig={path}]
```

- 支持完整环境名和简版环境名。

4. 列出环境

```bash
deploy list
```

- 输出格式为完整环境名，例如 `alice.dev`。

5. 查看当前激活环境

```bash
deploy cur
```

- 输出格式为完整环境名。

6. 配置默认 scope

```bash
deploy scope                # 查看当前默认 scope
deploy scope {scope-name}   # 设置默认 scope
```

- 默认 scope 用于补全简版环境名。
- 默认 scope 是仓库级配置，与当前激活环境无关。

7. `deploy.yaml` 文件路径规则

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

### 本地缓存

环境包含的信息缓存在当前仓库 `.env/` 目录下：

- `.env/current.json`：deploy 上下文文件，记录当前激活环境（`ActiveEnv`）与默认 scope（`DefaultScope`）。
- `.env/profile/<safe-full-env>.json`：环境 profile 文件。
- `.env/deploy/<safe-full-env>.yaml`：deploy 配置缓存。
- `.env/service/<safe-full-env>.yaml`：service 配置缓存。

其中 `<safe-full-env>` 为统一安全编码后的完整环境名。

### 可选集群冒烟测试

下面是一条**可选**的真实集群冒烟路径，仅用于确认 `grpc_hello_world` 示例在可达的 Kubernetes 集群上能正常跑通。它**不是**默认 `bazel test //tools/deploy/...` 的一部分。

前置条件：

- Kubernetes 集群可达。
- `kubectl` 已配置好可用的 context，并且当前用户有创建/删除 namespace 与资源的权限。
- 如果使用 microk8s 且 kubeconfig 不在默认位置，可显式传入 `--kubeconfig`；否则需要先把 microk8s 配置导出到 `KUBECONFIG` 或 `~/.kube/config`。

冒烟步骤：

```bash
deploy use alice.dev
deploy apply experimental/grpc_hello_world/deploy.yaml
deploy cur
deploy del alice.dev
```

- `use` 仅切换/创建本地激活环境；如果集群不可达，它仍可正常执行。
- `apply` 需要访问 Kubernetes；如果集群或 `kubectl` 配置不可用，会直接报出清晰的连接/权限错误并退出。
- `cur` 只查看当前激活环境，不依赖集群。
- `del` 在可达集群上清理环境；如果集群不可达，会同样失败并给出明确错误。

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
