# SigNoz Collector 前置依赖

## 目标

本文档描述 Golang OpenTelemetry 公共库上线前需要满足的 SigNoz Collector 与 Kubernetes 前置依赖。

这些内容属于部署前置条件，不纳入 `common/gopkg` / `common/gopkg/grpc` 的代码实现范围。应用公共库只负责把 logs、traces、metrics 通过 OTLP/gRPC 发送到 Collector；Collector 负责补齐 Kubernetes metadata、映射业务维度并转发到 SigNoz。

## 范围

本文档覆盖：

* SigNoz Collector 服务地址和端口要求。
* OTLP receiver 要求。
* `k8sattributes` processor 要求。
* Kubernetes RBAC 要求。
* 资源字段映射要求。
* logs、traces、metrics pipeline 的必要处理顺序。

本文档不覆盖：

* SigNoz 的安装方式。
* Collector Helm chart 或 manifest 的完整实现。
* 生产集群容量规划。
* 告警、dashboard、retention 策略。

## Collector 服务要求

应用公共库默认使用以下 OTLP/gRPC endpoint：

```text
dominion-opentelemetry-collector.kube-public.svc.cluster.local:4317
```

要求：

* Kubernetes Service 名称为 `dominion-opentelemetry-collector`。
* Service namespace 为 `kube-public`。
* Service 暴露 OTLP/gRPC 端口 `4317`。
* 集群内服务可通过完整 DNS `dominion-opentelemetry-collector.kube-public.svc.cluster.local` 访问。
* 自建集群内默认使用 insecure gRPC；如启用 TLS，需要同步调整应用公共库配置。

## OTLP Receiver 要求

Collector 必须启用 OTLP receiver 的 gRPC 协议：

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
```

logs、traces、metrics 共用该 receiver。

## Kubernetes Metadata 要求

Collector 必须启用 `k8sattributes` processor。

该 processor 负责将来自应用的 telemetry 关联到 Pod，并补齐 Kubernetes resource attributes。没有该 processor，本方案无法保证 `service`、`environment`、`container` 等维度完整存在。

必须提取的 Kubernetes metadata：

```yaml
processors:
  k8sattributes:
    auth_type: serviceAccount
    extract:
      metadata:
        - k8s.namespace.name
        - k8s.pod.name
        - k8s.pod.uid
        - k8s.container.name
        - k8s.deployment.name
        - k8s.statefulset.name
      labels:
        - tag_name: app
          key: app.kubernetes.io/name
          from: pod
        - tag_name: service
          key: app.kubernetes.io/component
          from: pod
        - tag_name: environment
          key: dominion.io/environment
          from: pod
```

如当前 Collector 版本不支持直接提取 `k8s.container.name`，需要通过该版本支持的 container metadata 配置实现同等效果，并在验收中确认 `container` 维度存在。

## Pod 关联要求

Collector 需要能将 telemetry 关联到来源 Pod。

推荐配置：

```yaml
processors:
  k8sattributes:
    pod_association:
      - sources:
          - from: resource_attribute
            name: k8s.pod.ip
      - sources:
          - from: resource_attribute
            name: k8s.pod.uid
      - sources:
          - from: connection
```

在应用直接通过 OTLP/gRPC 连接 Collector 的模式下，`from: connection` 通常可以完成 Pod 关联。若中间经过代理、sidecar 或 gateway，需要根据实际链路增加 `k8s.pod.ip` 或 `k8s.pod.uid` 等 resource attribute。

## RBAC 要求

运行 Collector 的 ServiceAccount 必须具备读取 Kubernetes metadata 的权限。

最低要求：

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: dominion-opentelemetry-collector-k8sattributes
rules:
  - apiGroups: [""]
    resources:
      - pods
      - namespaces
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - apps
    resources:
      - replicasets
    verbs:
      - get
      - list
      - watch
```

如果需要补齐 deployment、statefulset 或 node 相关属性，需要按 Collector 版本和配置补充对应权限。

## 字段映射要求

Collector 需要将 Kubernetes label 和 metadata 映射为最终查询维度。

目标字段：

```text
app = <app.kubernetes.io/name>
service = <app.kubernetes.io/component>
environment = <dominion.io/environment>
container = <k8s.container.name>
service.name = app/service
deployment.environment.name = environment
```

示例 transform 配置：

```yaml
processors:
  transform/dominion_resource:
    error_mode: ignore
    trace_statements:
      - context: resource
        statements:
          - set(attributes["service.name"], Concat([attributes["app"], attributes["service"]], "/")) where attributes["app"] != nil and attributes["service"] != nil
          - set(attributes["deployment.environment.name"], attributes["environment"]) where attributes["environment"] != nil
          - set(attributes["container"], attributes["k8s.container.name"]) where attributes["k8s.container.name"] != nil
    metric_statements:
      - context: resource
        statements:
          - set(attributes["service.name"], Concat([attributes["app"], attributes["service"]], "/")) where attributes["app"] != nil and attributes["service"] != nil
          - set(attributes["deployment.environment.name"], attributes["environment"]) where attributes["environment"] != nil
          - set(attributes["container"], attributes["k8s.container.name"]) where attributes["k8s.container.name"] != nil
    log_statements:
      - context: resource
        statements:
          - set(attributes["service.name"], Concat([attributes["app"], attributes["service"]], "/")) where attributes["app"] != nil and attributes["service"] != nil
          - set(attributes["deployment.environment.name"], attributes["environment"]) where attributes["environment"] != nil
          - set(attributes["container"], attributes["k8s.container.name"]) where attributes["k8s.container.name"] != nil
```

不同 Collector 版本的 transform processor 表达式函数可能存在差异。实际部署时可以使用等价语法实现 `service.name = app + "/" + service`，并以验收结果为准。

## Pipeline 要求

logs、traces、metrics pipeline 都必须包含以下处理顺序：

```text
otlp receiver
  -> k8sattributes
  -> transform/resource mapping
  -> batch
  -> SigNoz exporter
```

示例结构：

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [k8sattributes, transform/dominion_resource, batch]
      exporters: [<signoz-exporter>]
    metrics:
      receivers: [otlp]
      processors: [k8sattributes, transform/dominion_resource, batch]
      exporters: [<signoz-exporter>]
    logs:
      receivers: [otlp]
      processors: [k8sattributes, transform/dominion_resource, batch]
      exporters: [<signoz-exporter>]
```

## 验收标准

部署公共库实践服务后，需要在 SigNoz 中验证：

* traces 能看到 `experimental/golang/grpc_hello_world` 的 gateway 到 service 调用链路。
* logs 能通过 trace id 与 traces 关联。
* metrics 能按 gRPC method 和 status 聚合。
* logs、traces、metrics 的 resource attributes 中均存在：
  * `app`
  * `service`
  * `environment`
  * `container`
  * `service.name`
  * `k8s.namespace.name`
  * `k8s.pod.name`
* `service.name` 格式为 `app/service`。
* `environment` 格式为 `scope.env`。

## 与应用代码的边界

应用公共库不负责：

* 安装或升级 SigNoz。
* 创建 Collector Service。
* 创建 Collector RBAC。
* 修改 Collector pipeline。
* 保证 Collector 的 processor 版本兼容性。

应用公共库负责：

* 将 deploy 环境中的 logs、traces、metrics 通过 OTLP/gRPC 发送到默认 endpoint。
* 非 deploy 环境不远程上报 trace / metric，也不创建 OTLP exporters。
* 非 deploy 环境日志输出到控制台。
* 非 deploy 环境不解析、不访问 `dominion-opentelemetry-collector.kube-public.svc.cluster.local`。
* 保证 trace id 在 deploy 与 non-deploy 环境中都可通过公共 API 获取。
