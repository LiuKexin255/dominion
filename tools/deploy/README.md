本目录包含部署相关定义与工具

## 部署定义

1. 使用 `service.yaml` 定义需要被部署的服务单元。
2. 使用 `deploy.yaml` 定义部署环境，以及该环境需要部署哪些服务单元。

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
deploy use {env-name} [--app={app-name}]
```

如果环境不存在，则创建环境；如存在则切换环境

2. 部署/更新服务

```bash 
deploy deploy [--kubeconfig={path}] {path-of-deploy.yaml}
```

- `deploy` 需要先有当前激活环境，必须先执行 `use`，否则命令会失败。
- `--kubeconfig` 可选；不传时按 client-go 默认规则加载（例如 `KUBECONFIG` 或 `~/.kube/config`）。

3. 删除环境

```bash
deploy del {env-name} [--app={app-name}] [--kubeconfig={path}]
```

4. 列出环境

```bash
deploy list
```

- 环境展示和引用格式为 `{app}/{env}`，例如 `grpc-hello-world/dev`。

5. 查看当前激活环境

```bash
deploy cur
```

6. `app` 参数规则

- `--app` 可省略；省略时默认使用当前激活环境相同的 `app` 名称。

7. `deploy` 文件路径规则

- 以 `//` 开头：按项目 Bazel 工作区根目录（包含 WORKSPACE.bazel 或 MODULE.bazel 的目录）解析。
- 不以 `/` 开头的相对路径：按当前 shell 工作目录解析。
- 以 `/` 开头：按系统绝对路径解析。

注意：`deploy` 工具必须在 Bazel 工作区内运行，否则会返回错误。

示例：

```bash
deploy deploy //experimental/grpc_hello_world/deploy.yaml
deploy deploy experimental/grpc_hello_world/deploy.yaml
```

### 本地缓存

环境包含的信息缓存在当前仓库 `.env/` 目录下：

- `.env/current.json`：deploy 上下文文件，记录当前激活环境（`ActiveEnv`）与最近一次使用的应用（`LastApp`）。删除当前激活环境后，`cur` 命令将返回未激活错误，但 `use` 和 `del` 命令在缺省 `--app` 时仍可使用 `LastApp` 作为默认值。
- `.env/{app}__{env}.json`：环境 profile 文件，例如 `grpc-hello-world__dev.json`。
- `.env/deploy/`：deploy 配置缓存，文件名格式为 `{app}__{env}__{template_app}__{template}.yaml`。
- `.env/service/`：service 配置缓存，文件名格式为 `{app}__{env}.yaml`。

### 可选集群冒烟测试

下面是一条**可选**的真实集群冒烟路径，仅用于确认 `grpc_hello_world` 示例在可达的 Kubernetes 集群上能正常跑通。它**不是**默认 `bazel test //tools/deploy/...` 的一部分。

前置条件：

- Kubernetes 集群可达。
- `kubectl` 已配置好可用的 context，并且当前用户有创建/删除 namespace 与资源的权限。
- 如果使用 microk8s 且 kubeconfig 不在默认位置，可显式传入 `--kubeconfig`；否则需要先把 microk8s 配置导出到 `KUBECONFIG` 或 `~/.kube/config`。

冒烟步骤：

```bash
deploy use --app=grpc-hello-world grpc-dev
deploy deploy experimental/grpc_hello_world/deploy.yaml
deploy cur
deploy del --app=grpc-hello-world grpc-dev
```

- `use` 仅切换/创建本地激活环境；如果集群不可达，它仍可正常执行。
- `deploy` 需要访问 Kubernetes；如果集群或 `kubectl` 配置不可用，会直接报出清晰的连接/权限错误并退出。
- `cur` 只查看当前激活环境，不依赖集群。
- `del` 在可达集群上清理环境；如果集群不可达，会同样失败并给出明确错误。
