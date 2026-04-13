# gRPC TLS 标准化方案

## 目标

为仓库内 gRPC 服务提供统一的安全传输能力，避免继续运行在明文模式。

当前阶段不引入 mesh，由应用自身完成标准 TLS。

## 已确认决策

### 1. 当前阶段不引入 service mesh

暂不引入 Istio 等 mesh。

原因：

1. 当前目标是先摆脱明文传输。
2. 直接使用 gRPC 应用层 TLS 改动更小、路径更短。
3. 当前阶段先聚焦传输安全本身，避免同时引入额外基础设施复杂度。

### 2. TLS 能力做成共享工具库的一部分

TLS 不应分散在各个业务服务中各自实现。

统一放入 `pkg/grpc/xxx` 子目录中，形成标准化 gRPC client/server 接入方式。

建议：

- grpc 相关扩展或插件统一放在 `pkg/grpc/xxx`
- TLS 能力放入 `pkg/grpc/tls`

其中 `pkg/grpc` 根目录只保留聚合入口和 README，不直接承载具体插件实现。

核心目标是：

1. 统一读取证书配置。
2. 统一构造 gRPC credentials。
3. 统一错误处理与默认行为。

同时，`pkg/grpc` 下提供 `Default` 方法，作为包内标准扩展装配入口。

当前阶段，`Default` 至少应统一装配：

1. 标准 TLS 能力。
2. 标准化 gRPC option 聚合能力。

这样业务侧只依赖统一默认入口，而不需要分别理解 resolver 与 TLS 的装配细节。

### 3. 第一阶段优先使用标准 TLS credentials

第一阶段优先使用 grpc-go 标准 TLS credentials 封装，先把“退出明文”作为第一目标。

当前方案不考虑 advanced TLS、回调式校验或动态证书轮转。

### 4. 证书通过 deploy 配置注入

证书已经保存在 Kubernetes Secret 中。

当前方案要求在 deploy 配置中增加证书配置，由 deploy 在生成 Kubernetes 资源时完成证书挂载与注入。

这样做的边界是：

1. deploy 负责证书资源引用与容器挂载。
2. `pkg/grpc/tls` 负责按约定路径读取证书并构造 TLS credentials。

### 5. 证书路径固定为 `/etc/tls`

当前方案固定证书目录为 `/etc/tls`。

推荐约定：

1. 服务端证书：`/etc/tls/tls.crt`
2. 服务端私钥：`/etc/tls/tls.key`
3. client 使用的 CA 证书也放在 `/etc/tls` 下

## 当前仓库现状

仓库已经出现 TLS 的雏形，但尚未启用：

1. `experimental/grpc_hello_world/service/main.go` 中已存在：
   - `tls_cert_file`
   - `tls_key_file`
2. 但服务端 `credentials.NewServerTLSFromFile(...)` 代码仍被注释。
3. client 端 `experimental/grpc_hello_world/gateway/main.go` 仍使用 `insecure.NewCredentials()`。

这说明：

1. 应用层 TLS 路线与仓库方向一致。
2. 目前缺的是标准化封装，而不是全新技术方向。

## 方案范围

### client 侧

共享库需要统一提供：

1. 服务端证书校验。
2. CA 证书加载。
3. 构造标准 `DialOption`。

### server 侧

共享库需要统一提供：

1. 从文件或挂载路径加载服务端证书与私钥。
2. 构造 `grpc.Creds(...)`。

## 推荐模式

### 第一阶段：直接落标准 TLS

第一阶段至少保证：

1. client 校验 server 身份。
2. 所有 gRPC 连接退出明文模式。

第一阶段推荐优先落地：

1. server 使用受控证书和私钥启动。
2. client 使用受控 CA 校验 server 身份。
3. 不再允许 `insecure.NewCredentials()` 成为默认路径。

## 与 deploy 的衔接

证书路径、环境信息和相关参数都应通过构建出的 workload 注入到容器中。

建议由 deploy 侧统一将以下信息注入 Pod：

1. 当前环境信息。
2. Secret 对应的证书挂载配置。
3. `/etc/tls` 下的证书文件路径约定。

当前仓库还没有通用 env 注入能力，因此后续实现需要补齐：

1. schema/config 对 env 的表达能力。
2. workload 对 env 的承载能力。
3. builder 对 env 的实际写入能力。
4. deploy 配置对证书 Secret 的表达能力。

## Default 装配入口

推荐业务通过 `pkg/grpc.Default(...)` 获取标准 TLS 相关默认能力。

该入口在当前阶段至少统一装配：

1. 标准 TLS credentials。
2. 标准化 gRPC option。

## grpc-go 约束

实现时需要明确以下约束：

1. 当前方案只使用标准 TLS credentials，不引入高级回调扩展。
2. TLS 校验使用的 authority / server name 不能依赖不可信的服务发现结果。
3. TLS 版本与校验策略建议显式配置，不依赖未来可能变化的默认值。

## 当前无阻塞项

当前方案没有需要继续确认的架构级阻塞，可以进入实现设计。

## 实现阶段仍需确定的细节

以下属于实现层选择，不影响方案成立：

1. deploy 配置中证书字段的具体命名。
2. Secret 到 `/etc/tls` 的具体挂载形式。
3. client 默认使用的 server name / authority 来源。

## 未来计划

以下内容属于后续演进方向，不属于当前落地范围：

1. 如果未来引入 mesh，TLS 职责下沉到 mesh，业务调用入口保持不变。
2. 评估是否纳入更丰富的 grpc 默认扩展能力。
