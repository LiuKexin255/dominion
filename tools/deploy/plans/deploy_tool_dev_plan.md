# 部署工具（env_deploy）开发与变更计划

## 背景与目标

基于 `tools/deploy/README.md` 的约定，开发部署工具 `env_deploy`，并通过 Bazel 暴露统一入口 `//:deploy`，支持以下能力：

1. 创建/切换环境：`use [--app={app-name}] {env-name}`
2. 部署/更新服务：`deploy {path-of-deploy.yaml}`
3. 删除环境：`del [--app={app-name}] {env-name}`
4. 列出全部环境：`list`
5. 查看当前激活环境：`cur`
6. `app` 参数如果省略，则使用当前激活环境相同 `app` 名称。

同时实现本地环境缓存到 `.env` 目录，包含当前激活环境指针与环境/部署配置文件。

## 现状分析

1. 当前 `tools/deploy` 下已有文档与 Schema：
   - `tools/deploy/README.md`
   - `tools/deploy/service.schema.yaml`
   - `tools/deploy/deploy.schema.yaml`
2. 已有可用示例：
   - `experimental/grpc_hello_world/deploy.yaml`
   - `experimental/grpc_hello_world/service/service.yaml`
   - `experimental/grpc_hello_world/gateway/service.yaml`
3. 当前仓库中尚无 deploy 工具实现代码与 Bazel rule 包装。

## 关键设计决策（MVP）

1. 语言与形态：使用 Golang 实现 CLI（`env_deploy`）。
2. 配置校验：解析 YAML 后使用 JSON Schema 做强校验（复用现有 schema）。
3. 执行方式：以 K8s HTTP API 为执行后端（基于 `client-go`），不依赖本机 `kubectl`。
4. 产物解析：从 `service.yaml` 的 `artifacts[].target` 推导并使用对应 push 目标信息生成镜像引用。
5. 资源删除策略：仅删除由部署工具管理标签命中的资源，不执行命名空间级删除。
6. 缓存策略：在 `.env` 下维护环境资料与部署主配置引用。环境 profile 文件名为 `{app}__{env}.json`；当前激活环境写入 `.env/current.json`；部署主配置写入 `.env/deploy/{app}__{env}__{templateApp}__{template}.yaml`。
7. 路径解析策略：
   - `//` 前缀表示项目根目录（`BUILD_WORKSPACE_DIRECTORY`）下的路径；
   - 非 `/` 开头路径表示基于 shell 当前目录（`BUILD_WORKING_DIRECTORY`）的相对路径；
   - `/` 开头路径保持系统绝对路径语义。

## 迭代计划

### epoch 1：基础骨架与配置契约

step 1.1: 新增 CLI 入口与参数解析

- 新增 `tools/deploy/cmd/env_deploy/main.go`
- 支持并校验参数组合：`use [--app] {env-name}` / `deploy {path-of-deploy.yaml}` / `del [--app] {env-name}` / `list` / `cur`
- 参数解析采用每个 command 一个 `FlagSet`，通过工厂函数生成，并按参数表绑定与执行命令级校验。

step 1.2: 新增配置加载与 Schema 校验模块

- 读取并解析 `deploy.yaml` 与 `service.yaml`
- 使用 `service.schema.yaml`、`deploy.schema.yaml` 完成校验并输出可读错误

step 1.3: 新增环境缓存模块

- 读写 `.env/current.json`、`.env/{app}__{env}.json` 与 `.env/deploy/*.yaml`
- 实现 `use {env-name}` 创建/切换（不存在则创建，存在则切换）

交付结果：

- CLI 可执行
- 配置可校验
- 环境缓存可创建与切换

### epoch 2：部署主流程（优化后）

step 2.0: 新增 k8s 抽象层

- 在 `tools/deploy/pkg` 新增 `k8s` 包，承载部署对象抽象与资源操作入口。
- 统一管理部署对象模型、资源组织方式与部署流程编排边界。
- 保持该层与 CLI、环境缓存层解耦，便于后续测试与扩展。

step 2.1: 增加配置对象到部署对象转换

- 将 `deploy/service` 配置对象转换为 k8s 部署对象 DTO。
- 支持 Deployment、Service 以及存在 `http` 配置时的路由对象生成。
- 将镜像引用解析与端口、路由等部署输入纳入同一转换流程。

step 2.2: 新增 k8s client 初始化能力

- 在 `k8s` 包内实现 API 客户端初始化能力，覆盖 typed 与 dynamic 两类客户端。
- 将运行时依赖（集群访问配置、命名空间上下文等）统一收敛到该层处理。
- 对初始化失败场景提供一致的失败返回路径。

step 2.3: 与 env 包整合部署/更新流程

- 将 `k8s` 包能力接入 `env` 包，由环境对象触发资源部署与更新。
- 调整 `deploy` 命令执行链路，使部署流程由“配置更新”升级为“远端资源操作 + 本地状态同步”。
- 保持现有命令参数契约不变（`deploy/list/cur/use/del`）。

step 2.4: 与 env 包整合删除流程

- 在删除环境时通过 `k8s` 包执行资源删除，并采用“管理标签命中资源删除”策略。
- 删除完成后清理本地环境缓存与关联配置；若删除目标为当前激活环境，清理 `.env/current.json`。
- 保障重复执行删除时行为稳定，不引入额外副作用。

交付结果：

- `pkg/k8s` 抽象层与 client 初始化能力可用
- 示例环境可完成部署、更新、删除全流程
- 部署工具运行不依赖本机 `kubectl` 二进制

### epoch 3：Bazel 包装与质量收敛

step 3.1: Bazel 规则包装

- 新增 `tools/deploy` 下 Bazel 规则/宏定义
- 在根 `BUILD.bazel` 暴露 `//:deploy` 运行入口

step 3.2: 测试补齐

- 单元测试：参数解析、Schema 校验、缓存、资源渲染、API 执行器（部署/更新/删除与异常分支）
- 集成测试：基于 `experimental/grpc_hello_world` 做最小链路验证
- 为单测 target 设置 `size = "small"`

step 3.3: 文档与契约统一

- 修正 `README` 命令拼写（`//:deply` -> `//:deploy`）
- 对齐 `deploy.schema.yaml` 与示例字段定义（如 `name` 字段是否 required）
- 明确 `deploy` 文件路径规则（`//`/相对路径/绝对路径）并与实现保持一致

交付结果：

- `bazel run //:deploy -- ...` 可用
- 文档、示例、Schema、实现一致
- 构建与测试通过

## 变更清单（预计）

1. 新增 `tools/deploy/cmd/main.go`
2. 新增 `tools/deploy/pkg/*`（config、schema、cache、render、executor）
3. 新增/修改 `tools/deploy/BUILD.bazel` 与 Bazel rule/macro 文件 `defs.bzl`
4. 修改根 `BUILD.bazel` 暴露 `//:deploy`
5. 修改 `tools/deploy/README.md` 与必要 schema 文件
6. 新增部署工具相关测试文件
7. 按需补充依赖（`k8s.io/client-go`、`k8s.io/apimachinery`、`sigs.k8s.io/gateway-api`）

## 验证计划

1. 构建：`bazel build //:deploy`
2. 环境切换：`bazel run //:deploy -- use --app=grpc-hello-world grpc-dev`
3. 部署更新：`bazel run //:deploy -- deploy experimental/grpc_hello_world/deploy.yaml`
4. 删除环境：`bazel run //:deploy -- del --app=grpc-hello-world grpc-dev`
5. 列出环境：`bazel run //:deploy -- list`
6. 查看当前环境：`bazel run //:deploy -- cur`
7. 测试：`bazel test //...`

## 执行约束（遵循仓库规范）

Golang 与 Bazel 相关操作按以下顺序执行：

1. `bazel run @rules_go//go -- fmt [变更文件]`
2. `bazel run @rules_go//go -- mod tidy -v`
3. `bazel run //:gazelle`
4. `bazel mod tidy`
