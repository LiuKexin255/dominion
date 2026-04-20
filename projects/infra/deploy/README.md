# deploy service

本目录用于承载 deploy 控制面的协议与实现。当前模型以 `Environment` 作为唯一对外资源，通过 gRPC 暴露环境的增删改查接口；环境内部的 Kubernetes 资源不会直接对外暴露。

## 目标

`deploy service` 的职责是：

1. 维护部署环境的**权威状态**。
2. 接收客户端提交的环境**期望状态**。
3. 异步将期望状态 reconcile 到 Kubernetes 集群。
4. 对外提供环境当前的**观察状态**与错误信息。

本地 CLI 是薄客户端，只负责：

- 参数解析与用户交互。
- 组织 gRPC 请求并调用远端服务。
- 展示环境状态、错误信息和执行结果。

## 部署信息

* Host: infra.liukexin.com
* Path: /v1/deploy
* 因服务无法自举，本服务禁止使用 `deploy` 工具部署，也禁止生成除 `k8s` 以外的部署配置。

## 其他

* 因无法使用 deploy 部署工具，本服务**不进行**大型测试。