# Quickstart: Deploy Secret Configuration

**Branch**: `002-deploy-secret-config` | **Date**: 2026-06-02

## 概览

本功能为 deploy 工具增加 secret 配置支持。服务维护者在 `service.yaml` 中声明 secret 逻辑名，部署者在 `deploy.yaml` 中将逻辑名绑定到 Kubernetes Secret + key。运行时 secret 文件通过 `/mnt/dominion/secret/` 目录暴露给服务。

## 使用步骤

### 1. 在 service.yaml 中声明 secret

在 artifact 中添加 `secrets` 列表：

```yaml
# service.yaml
version: "3.0"
name: orders-api
app: orders
desc: orders API service
kind: stateless
artifacts:
  - name: api
    target: :api_image
    secrets:                    # 新增字段
      - database-url
      - stripe-api-key
    ports:
      - name: http
        port: 8080
```

Secret 名称规则：
- 必须以小写字母开头
- 只允许小写字母、数字、下划线、连字符
- 最长 63 字符
- 同一 artifact 内不可重复

### 2. 在 deploy.yaml 中绑定 secret

在 artifact 条目中添加 `secrets` 映射：

```yaml
# deploy.yaml
version: "3.0"
name: orders.prod
desc: "orders production environment"
type: prod
services:
  - artifact:
      path: //projects/orders/service.yaml
      name: api
      secrets:                               # 新增字段
        database-url:
          secret: orders-prod-secrets               # K8s Secret 名
          key: DATABASE_URL                         # Secret 中的 key
        stripe-api-key:
          secret: orders-prod-secrets
          key: STRIPE_API_KEY
```

绑定规则：
- 每个声明的 secret 必须有且仅有一个绑定
- 不允许绑定未声明的 secret（防止拼写错误或过期配置）
- `secret` 和 `key` 均不可为空

### 3. 部署

```bash
deploy apply //projects/orders/deploy.yaml
```

如果存在未绑定的 secret，部署将被拒绝：

```
artifact api in //projects/orders/service.yaml: secret "stripe-api-key" declared but not bound
```

### 4. 运行时使用

部署成功后，服务容器中可以：

```bash
# 通过环境变量发现 secret 目录
echo $DOMINION_SECRET_DIR    # 输出: /mnt/dominion/secret

# 读取 secret 文件
cat /mnt/dominion/secret/database-url
cat /mnt/dominion/secret/stripe-api-key
```

## 不需要 secret 的服务

如果服务不声明 secrets，行为完全不变：

```yaml
# service.yaml — 无 secrets 声明，行为不变
version: "3.0"
name: health-check
app: infra
desc: health check service
artifacts:
  - name: checker
    target: :checker_image
    ports:
      - name: http
        port: 8080
```

无需修改 `deploy.yaml`，无需添加 `secrets` 字段。

## 保留环境变量

新增 `DOMINION_SECRET_DIR` 到保留变量名列表，不可在 `deploy.yaml` 的 `env` 中使用：

```
SERVICE_APP, DOMINION_ENVIRONMENT, POD_NAMESPACE,
TLS_CERT_FILE, TLS_KEY_FILE, TLS_CA_FILE, TLS_SERVER_NAME,
S3_ACCESS_KEY, S3_SECRET_KEY, DOMINION_SECRET_DIR
```

## 变更范围

| 文件 | 变更类型 |
|------|---------|
| `tools/release/deploy/pkg/schema/service.schema.json` | 新增 `secrets` 字段 |
| `tools/release/deploy/pkg/schema/deploy.schema.json` | 新增 `secrets` 字段 |
| `tools/release/deploy/pkg/config/config.go` | 新增 `Secrets` / `Secrets` 字段和校验 |
| `tools/release/deploy/v2/compiler/compiler.go` | 新增绑定完整性和多余绑定校验 |
| `projects/infra/deploy/deploy.proto` | 新增 `SecretBinding` message |
| `projects/infra/deploy/domain/spec.go` | 新增 `SecretBinding` 类型和校验 |
| `projects/infra/deploy/runtime/k8s/converter.go` | 传递 bindings 到 workload |
| `projects/infra/deploy/runtime/k8s/builder.go` | 构建 projected volume + env |
| `tools/release/deploy/README.md` | 更新文档 |
| 各层 `*_test.go` 和 `testdata/` | 新增测试 |
