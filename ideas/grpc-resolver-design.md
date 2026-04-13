# gRPC Resolver 标准化方案

## 目标

为仓库内的 gRPC client 提供统一的服务发现能力，使业务代码可以使用 `app/service` 形式访问同环境下的目标服务，而不依赖固定的 Kubernetes `Service` 名称。

本方案只解决两件事：

1. 将业务侧使用方式标准化为 `app/service`。
2. 将 `app/service` 解析为同环境下可直连的 gRPC endpoint 列表。

## 已确认决策

### 1. resolver 返回 `ip:port` 列表

resolver 的职责是把逻辑名解析为可连接的具体 endpoint。

在当前阶段，不引入 mesh，因此 resolver 应直接返回目标 Pod 的 `ip:port` 列表，而不是返回 Kubernetes `Service` 名称。

这样做的原因：

1. 避免把 resolver 退化成一次“名字翻译”。
2. 让 gRPC 真正拿到后端实例列表，后续可以直接使用 gRPC 自身的 balancer 能力。
3. 不依赖 deploy 生成的动态 `svr` 名称。

### 2. 业务侧通过 wrapper 使用 `app/service`

业务代码不直接写 `dominion:///app/service`。

统一将 grpc 相关扩展或插件放到：`pkg/grpc/xxx` 这样的子目录。

`pkg/grpc` 根目录只承担聚合入口与说明职责，不直接承载具体插件实现。

resolver 能力位于：`pkg/grpc/solver`。

该库对外暴露 wrapper 接口，例如：

```go
conn, err := solver.Dial(ctx, "grpc-hello-world/service:50051")
```

wrapper 内部负责：

1. 补全 resolver scheme。
2. 注册自定义 resolver。
3. 构造 gRPC DialOption。

同时，`pkg/grpc` 目录下提供一个 `Default` 方法，用于聚合包内默认启用的 grpc 扩展能力。

当前阶段，`Default` 至少应包含：

1. resolver 注册。
2. 标准化的 gRPC DialOption 聚合。

业务侧可以通过统一入口获取默认扩展能力，而不是逐个手动拼装。

#### grpc-go 约束

实现时需要遵守 grpc-go 的两个约束：

1. resolver scheme 注册是全局的，且要求使用小写名称。
2. 不要根据服务发现结果去填充 `resolver.Address.ServerName`，避免把不可信发现数据带入 TLS authority 校验路径。

因此建议：

1. 使用固定且唯一的小写 scheme，例如 `dominion`。
2. `resolver.Address.ServerName` 不从服务发现结果反推。

### 3. scheme 注册已固定

当前方案已经固定使用小写 scheme：`dominion`。

scheme 的注册与默认装配由 `pkg/grpc` 统一负责，不再作为待确认项。

### 4. 环境信息通过 workload 注入到容器环境变量

当前仓库 `tools/deploy/pkg/k8s/builder.go` 构造容器时只写入 `Name/Image/Ports`，还没有 `Env`。

后续实现时，应沿着以下链路补充：

1. `tools/deploy/pkg/config` 增加环境变量配置能力。
2. `tools/deploy/pkg/k8s/workload.go` 在 workload 结构体中增加需要注入的信息。
3. `tools/deploy/pkg/k8s/builder.go` 在生成 Pod/Container 时写入 env。

因为 builder 是消费 workload 生成 K8s 资源，所以“需要注入的信息先进入 workload，再由 builder 落到 PodSpec”是正确边界。

## 方案结构

### 包结构

建议新增：

```text
pkg/grpc/
  README.md
  default.go
  solver/
    dial.go
    resolver.go
    target.go
    env.go
    k8s.go
```

建议内部职责拆分：

- `default.go`：提供 `Default` 方法，统一返回或装配包内默认 grpc 扩展。
- `solver/dial.go`：对外暴露 `Dial` 等统一入口。
- `solver/resolver.go`：实现 `resolver.Builder` 与 `resolver.Resolver`。
- `solver/target.go`：解析 `app/service`。
- `solver/env.go`：读取当前运行环境注入的信息。
- `solver/k8s.go`：基于 label 查询 EndpointSlice/Endpoints。

其中 `pkg/grpc/README.md` 用于说明该目录是 grpc 扩展的统一入口，以及 `Default` 的职责边界。

需要注意：具体插件实现放在 `pkg/grpc/solver` 这样的子目录中，不直接放在 `pkg/grpc` 根目录。

### target 形式

当前阶段使用显式端口形式：

```text
dominion:///app/service:50051
```

或由 wrapper 对外暴露为：

```text
app/service:50051
```

当前阶段端口不做名字注册与自动推导，resolver 直接从 target 中解析端口。

### 运行流程

1. 业务代码调用 `solver.Dial(ctx, "app/service:50051")`。
2. `solver.Dial` 内部调用 `pkg/grpc.Default(...)` 获取默认扩展能力。
3. wrapper 将 target 规范化为内部 target，例如 `dominion:///app/service:50051`。
4. resolver 从 target 中解析出：
   - `app`
   - `service`
5. resolver 从运行环境读取：
   - 当前环境名
   - 当前 dominion app
   - 当前 namespace（如需要）
6. resolver 使用稳定 label 查询同环境下的目标 EndpointSlice。
7. resolver 使用 target 中显式给出的端口，构造 `[]resolver.Address`。
8. resolver 调用 `cc.UpdateState(...)` 回填地址列表。

## Kubernetes 查询原则

不要依赖 deploy 生成的动态资源名。

优先使用稳定 label 反查目标对象，至少包括：

- `app.kubernetes.io/name`
- `app.kubernetes.io/component`
- `dominion.io/environment`
- `dominion.io/app`

这样可以绕过 `svc-{env}-{service}-{hash}` 这类动态命名，并保持与当前 deploy 生成逻辑解耦。

### EndpointSlice 作为当前实现基线

当前方案直接使用 `EndpointSlice`，不再把 `Endpoints` 作为第一实现。

原因：

1. `EndpointSlice` 是 Kubernetes 当前推荐的服务后端发现方式。
2. 能力和扩展性比 `Endpoints` 更合适作为新实现基线。
3. 避免第一版就做两套后端来源逻辑。

## 刷新策略

当前阶段使用：**初始化全量查询 + 定时刷新**。

具体含义：

1. resolver 在初始化时先执行一次全量查询。
2. 后续通过固定周期轮询 `EndpointSlice` 更新地址列表。
3. 每次查询结果变化后，通过 `UpdateState(...)` 推送给 gRPC。

这样做的原因：

1. 第一版实现更简单。
2. 行为稳定，便于排障。
3. 可以先完成标准化接入。

## 当前无阻塞项

当前方案没有架构级待确认项，可以进入实现设计。

## 实现阶段仍需确定的细节

以下属于实现细节，不影响方案本身成立：

1. 环境变量的准确命名。
2. 定时刷新周期。

## 未来计划

以下内容不属于当前方案范围，统一放在未来计划中管理：

1. 端口名字注册能力。
2. 从显式端口升级为逻辑端口名解析。
3. 从定时刷新升级为 watch 模式。
4. 评估是否接入更丰富的 gRPC balancer / service config 能力。
