# pkg/grpc

`pkg/grpc` 是仓库内 grpc 相关扩展或插件的统一目录。

凡是对 grpc client/server 的公共增强能力，统一放在该目录下维护，避免分散在业务代码或其他工具包中重复实现。

`pkg/grpc` 根目录本身只承担两类职责：

1. 聚合入口。
2. 目录说明。

当前阶段的聚合入口由 `default.go` 提供，具体 resolver / dial / target / env / k8s 实现统一放在 `pkg/grpc/solver` 子目录中。

## 目标

该目录负责承载仓库内共享的 grpc 基础能力，包括但不限于：

1. 服务发现与 resolver。
2. TLS 配置与 credentials 封装。
3. 其他后续需要统一治理的 grpc 扩展能力。

## 边界

`pkg/grpc` 负责 grpc 扩展能力本身，不负责具体业务协议实现。

例如：

- 负责：共享的 grpc 连接、发现、安全、装配能力。
- 不负责：具体 proto 生成代码、业务方法封装、业务请求编排。

## 目录结构

- `default.go`：包级默认装配入口。
- `solver/`：resolver、target 解析、环境读取和 k8s 查询的实现目录。
- `tls/`：标准 TLS credentials 封装。

## TLS 支持

`pkg/grpc/tls` 包为 gRPC 提供标准的 TLS 凭据。

### 使用示例

```go
import grpctls "dominion/pkg/grpc/tls"

serverConfig := &grpctls.ServerConfig{
    CertFile: "/etc/tls/tls.crt",
    KeyFile:  "/etc/tls/tls.key",
    MinVersion: tls.VersionTLS12,
}
creds, err := grpctls.NewServerTransportCredentials(serverConfig)
```

### 默认入口

使用 `Default()` 进行标准配置装配：

```go
import "dominion/pkg/grpc"

opts, err := grpc.Default(grpc.DefaultConfig{
    ServerTLS: serverConfig,
})
server := grpc.NewServer(opts.Server...)
```

### 设计约束与特性

- **固定路径**：默认使用 `/etc/tls/tls.crt`、`/etc/tls/tls.key` 和 `/etc/tls/ca.crt`。
- **安全边界**：`ServerName` 必须来自受信任的部署配置，不接受解析器或用户输入。
- **Fail-Closed**：TLS 启用但配置错误时直接报错，不回退到非安全连接。
- **Phase-1 限制**：当前版本不支持 mTLS、动态证书轮转或自定义验证回调。
- **部署中立**：部署层注入的环境变量采用中立命名（如 `TLS_CERT_FILE`），不带有 gRPC 前缀。
