本目录包含部署相关定义与工具

## 部署定义

1. 使用 `service.yaml` 定义需要被部署的服务单元。
2. 使用 `deploy.yaml` 定义部署环境，以及该环境需要部署哪些服务单元。

## 部署工具

通过部署工具 `env_deploy` 将 `deploy.yaml` 定义的环境连同其中包含的服务部署到 k8s 当中。部署工具通过 `bazel rules` 包装，类似 `gazelle`。

### 相关命令

1. 创建/切换环境

```bash
bazel run //:deploy -- use [--app={app-name}] {env-name}
```

如果环境不存在，则创建环境；如存在则切换环境

2. 部署/更新服务

```bash 
bazel run //:deploy -- deploy {path-of-deploy.yaml}
```

3. 删除环境

```bash
bazel run //:deploy -- del [--app={app-name}] {env-name}
```

4. 列出环境

```bash
bazel run //:deploy -- list
```

5. 查看当前激活环境

```bash
bazel run //:deploy -- cur
```

6. `app` 参数规则

- `--app` 可省略；省略时默认使用当前激活环境相同的 `app` 名称。
- 使用标准参数前置风格，flag 放在位置参数前。

7. `deploy` 文件路径规则

- 以 `//` 开头：按项目根目录（`BUILD_WORKSPACE_DIRECTORY`）解析。
- 不以 `/` 开头的相对路径：按当前 shell 工作目录（`BUILD_WORKING_DIRECTORY`）解析。
- 以 `/` 开头：按系统绝对路径解析。

示例：

```bash
bazel run //:deploy -- deploy //experimental/grpc_hello_world/deploy.yaml
bazel run //:deploy -- deploy experimental/grpc_hello_world/deploy.yaml
```

### 本地缓存

环境包含的信息缓存在当前仓库 .env 目录下，例如环境名为 `dev`，则保存相关信息存储在 .env/dev.env.json。
