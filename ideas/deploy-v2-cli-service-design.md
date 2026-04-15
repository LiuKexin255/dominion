# Deploy v2 CLI + Service 方案

## 目标

将 `tools/deploy` 调整为 **CLI + service** 工作模式：

1. `tools/deploy/v2` 只负责本地配置解析、镜像推送与部署前置准备。
2. `projects/infra/deploy` 负责环境权威状态、部署执行、资源删除与状态推进。
3. `deploy` 命令名保持不变，待 v2 完成后再将 `//:deploy_install` 切换到 v2 安装入口。

本方案只覆盖本次 v2 迁移本身；与本次目标无关的未来规划单独放到文末。

## 引用方案

以下内容已在现有方案中定义，本方案直接引用，不重复展开：

1. `ideas/deploy-service-async-k8s-redesign.md`
   - deploy service 的异步状态推进、worker、删除语义、runtime 边界。
2. `ideas/deploy-env-identity-redesign.md`
   - 环境标识收敛为完整环境名 `{scope}.{env_name}`。
3. `ideas/deploy 内置 service.md`
   - `deploy.yaml` 中 `infra` 的基本语义，以及“删除环境只删除运行资源，不删除数据”的方向。

本方案只补充：CLI 与 service 的职责切分、调用契约、命令语义、切换策略与实现锚点。

## 本次实现范围

### 目标边界

本次只覆盖以下内容：

1. 在 `tools/deploy` 下新增 `v2/` 目录，承载新的 CLI 实现。
2. `deploy` CLI 保留 `apply`、`del`、`list`、`scope` 语义，移除 `use`、`cur` 与“激活环境”语义。
3. CLI 本地解析 `deploy.yaml` / `service.yaml`，并完成镜像推送与镜像地址解析。
4. CLI 将整理后的完整 desired state 发送给 `projects/infra/deploy`。
5. `apply` / `del` 由 CLI 调用 service API 后轮询环境状态，直到成功、失败或超时。
6. `projects/infra/deploy` 负责资源部署、删除、removed resources 的 prune 与状态写回。
7. README、testplan、脚本与安装入口在 v2 完成后切换到新语义。

### 非目标

本次不解决以下问题：

1. 旧 v1 CLI 本地缓存与旧命令语义兼容。
2. 旧资源的自动接管、自动清理。
3. deploy service 的自举问题；过渡期手动处理。
4. PVC 的最终生命周期优化；CLI 不参与该问题，后续在 service 侧单独优化。
5. 多集群、多 namespace、多控制面调度能力。

## 已确认决策

### 1. 职责切分

#### CLI 负责

`tools/deploy/v2` 只负责：

1. 解析命令参数。
2. 读取并校验 `deploy.yaml` / `service.yaml`。
3. 解析 artifact target。
4. 执行本地镜像推送。
5. 将产物解析为可用的 `image url + digest`。
6. 将配置编译为 service 可接受的完整 desired state。
7. 调用 deploy service 的 Get / Create / Update / Delete / List。
8. 轮询并展示最终结果。

#### Service 负责

`projects/infra/deploy` 负责：

1. 环境权威状态持久化。
2. desired state 接受与校验。
3. Kubernetes apply / delete。
4. full desired-state replacement 下 removed resources 的删除/prune。
5. 环境状态推进与错误写回。

CLI 不再负责：

1. 直接连接 Kubernetes。
2. 生成或 apply K8s runtime objects。
3. 管理本地“当前激活环境”。

### 2. 命令语义

#### 保留命令

1. `deploy apply`
2. `deploy del`
3. `deploy list`
4. `deploy scope`

#### 删除命令

1. `deploy use`
2. `deploy cur`

#### 语义变化

1. `apply` 不再激活环境。
2. `scope` 仅表示本地默认 scope 配置，用于 CLI 输入补全。
3. `list` 改为从 service 拉取环境列表，而非读取本地 `.env/profile`。
4. `del` 改为调用 service 的 `DeleteEnvironment`，不再直接删 K8s 资源。

### 3. `apply` 的 create / update 契约

`deploy apply` 固定采用以下流程：

1. 解析 `deploy.yaml` 中的完整环境名。
2. CLI 先调用 `GetEnvironment`。
3. 环境不存在：调用 `CreateEnvironment`。
4. 环境存在：调用 `UpdateEnvironment`。

本次不新增单独的 `ApplyEnvironment` / upsert API。

### 4. 镜像解析责任

镜像推送与镜像解析固定由 CLI 负责。

也就是说：

1. `service.yaml` 中的 artifact target 由 CLI 本地解析。
2. CLI 执行 `oci_push`。
3. 发送给 service 的是可直接部署的镜像地址，而不是 Bazel target。

因此 service 不依赖本地 workspace、Bazel target 或 push runner。

### 5. 轮询与终态语义

#### apply

1. 成功终态：`READY`
2. 失败终态：`FAILED`

#### delete

1. 成功终态：环境不存在（`GetEnvironment` 返回 not found）

#### timeout

如果在等待时间内未到达操作成功的状态，则算作超时。

CLI 至少需要输出：

1. 当前环境名
2. 当前状态
3. 最后的 `status.message`

### 6. desired state 语义

`apply` 固定为 **full desired-state replacement**。

也就是说：

1. CLI 每次都发送完整 desired state。
2. service 必须将其视为新的完整权威期望。
3. 如果某个 service / infra / route 从配置中被移除，下次 `apply` 后对应运行资源也应被删除。

具体 prune 逻辑由 service 实现，CLI 不感知环境部署细节。

### 7. 删除 ownership 边界

删除逻辑完全由 service 负责。

CLI 只负责发起删除请求与轮询结果，不参与：

1. 删除顺序
2. label selector
3. 资源识别
4. 资源保留/复用策略

关于 PVC 的更细策略，本次不在 CLI 方案中展开，后续由 service 单独优化。

### 8. 过渡与切换策略

1. 先完成 `tools/deploy/v2`。
2. 旧 CLI 代码可暂时保留，但不再保留旧安装入口。
3. `//:deploy_install` 在 v2 完成前不切换。
4. 待 v2 验证完成后，再将安装入口切换到 v2。
5. 旧 v1 资源与旧配置不做自动接管或自动清理。

## 关键细节

### 1. 配置模型保持不变

本次不调整用户输入模型：

1. `deploy.yaml` 继续作为环境级配置。
2. `service.yaml` 继续描述服务产物。
3. CLI 继续负责把两类配置编译成 service 的 desired state。

### 2. 环境名仍以完整环境名为唯一标识

沿用 `ideas/deploy-env-identity-redesign.md`：

1. 环境唯一标识为 `{scope}.{env_name}`。
2. 简版环境名只允许出现在 CLI 输入层。
3. 默认 `scope` 仅用于 CLI 输入补全。

service API 使用资源名形式：

```text
deploy/scopes/{scope}/environments/{env_name}
```

CLI 负责在本地完成：

1. 完整环境名与资源名之间的映射
2. 简版名补全为完整环境名

### 3. 本地状态最小化

v2 CLI 不再维护 `.env/` 下的环境权威缓存。

本地仅保留与 CLI 自身输入体验直接相关的配置，例如：

1. 默认 `scope`
2. 未来如需增加 endpoint / auth 配置，也应仅作为 CLI 本地配置存在

本地不再保存：

1. 当前激活环境
2. deploy/service/profile 缓存
3. 远端环境状态镜像

### 4. 轮询只基于 service 状态

轮询结果只以 service 返回的环境状态为准，不尝试从 Kubernetes 侧额外推断状态。

CLI 的职责是：

1. 发起请求
2. 周期性 `GetEnvironment`
3. 根据终态语义结束
4. 输出 service 返回的失败信息

### 5. 文档与测试计划同步切换

以下文档与测试需要按新语义更新：

1. `tools/deploy/README.md`
2. `projects/infra/deploy/testplan/interface_test.md`
3. 依赖 `deploy use` / `deploy cur` / `--kubeconfig` 的脚本与示例

## 实现锚点

### CLI 侧锚点

1. `tools/deploy/main.go`
   - 当前命令分发与 v1 语义入口。
2. `tools/deploy/BUILD.bazel`
   - 新增 v2 binary / install target。
3. `tools/deploy/install.sh`
   - 安装入口切换时需要改向 v2 binary。
4. `tools/deploy/pkg/config/config.go`
   - `deploy.yaml` / `service.yaml` 解析入口。
5. `tools/deploy/pkg/imagepush/`
   - 本地镜像推送与解析逻辑，v2 继续复用其职责。
6. `tools/deploy/pkg/env/`
   - v1 本地 `.env/` 缓存与激活语义来源；v2 需避开此模型。
7. `tools/deploy/README.md`
   - v2 命令语义落地文档。

### Service 侧锚点

1. `projects/infra/deploy/deploy.proto`
   - CLI 与 service 的 API 契约。
2. `projects/infra/deploy/handler.go`
   - Create / Update / Delete 行为入口。
3. `projects/infra/deploy/domain/environment.go`
   - desired state 更新与状态机。
4. `projects/infra/deploy/domain/worker.go`
   - 异步 apply / delete 推进。
5. `projects/infra/deploy/runtime/k8s/converter.go`
   - desired state 到 runtime workload 的映射。
6. `projects/infra/deploy/runtime/k8s/executor.go`
   - 资源 apply / delete；需补足 full replacement 下的 prune 语义。
7. `projects/infra/deploy/testplan/interface_test.md`
   - 接口测试命令流需切到 v2 语义。

## 验收标准

完成后应满足：

1. `deploy apply` 不再直接连接 Kubernetes。
2. `deploy del` 不再直接连接 Kubernetes。
3. CLI 仅在本地解析配置、推送镜像、调用 service、轮询结果。
4. `deploy use` / `deploy cur` 从 v2 语义中消失。
5. `deploy scope` 仍可作为本地默认 scope 配置使用。
6. service 能依据完整 desired state 完成 create / update / delete / prune。
7. 文档与接口测试计划按新语义更新。

## 未来规划

以下内容不纳入本次方案目标：

1. deploy service 的自举自动化。
2. PVC 等持久化资源的更细回收策略。
3. 旧资源自动迁移或自动接管。
4. 多集群、多 namespace、复杂控制面能力。
