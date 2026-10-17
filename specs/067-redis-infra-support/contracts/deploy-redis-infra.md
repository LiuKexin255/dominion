# Contract: deploy 配置 Redis Infra 与实例凭证注入

**Feature**: [../spec.md](../spec.md) | **Research**: [../research.md](../research.md)（D3/D4/D5/D6/D9）

## 1. deploy.yaml infra 声明（redis）

`services[].infra` 结构与 mongodb 完全一致，`resource` 取值扩展为 `mongodb | redis`：

```yaml
services:
  - infra:
      resource: redis            # 新增枚举值
      profile: dev-single        # 须存在于 static_config 的 redis profile 表
      name: redis                # 实例名（参与资源名生成）
      app: my-app                # 实例所属应用
      persistence:               # 可选；与 mongodb 语义一致
        enabled: true
        capacity: 8Gi            # 可选，默认取 profile 容量；上限 1Ti
```

校验规则（既有语义扩展，`tools/release/deploy/pkg/schema/deploy.schema.json`）：

* `resource` 非 `{mongodb, redis}` → 配置校验拒绝。
* `profile` 在对应资源种类 profile 表中不存在 → 部署期拒绝（deploy service converter）。
* `persistence.capacity` 非法/超上限 → CLI 配置校验拒绝（既有 `maxInfraPersistenceCapacity`）。
* 生成的资源名超 63 字符 → workload 校验拒绝。

## 2. 实例生成资源（redis standalone）

| 对象 | 名称 | 要点 |
|---|---|---|
| Deployment | `redis-{env}-{name}-{hash}` | Recreate、单副本；命令 `["/bin/sh","-ec","exec redis-server --requirepass \"$REDIS_PASSWORD\" --appendonly yes"]`（`--appendonly yes` 仅持久化启用时存在）；labels：`app`/`service`/`environment`/`managed-by`（与 mongo 同键值约定，`projects/infra/deploy/runtime/k8s/labels.go`） |
| Service | `svc-{env}-{name}-{hash}` | 端口名 `redis`，端口取 profile |
| PVC | `pvc-{env}-{name}-{hash}` | 仅持久化启用；storage 参数取 profile；容量覆盖优先 |
| Secret | —（**不生成**） | redis 与 mongo 一致：实例凭证仅以环境变量注入 |

探针：liveness + readiness TCP 探针（端口名 `redis`），与 mongo 实例同模式；不使用用户服务健康探针约定（38080/`/healthz` 不适用于 infra，`tools/release/deploy/README.md` §健康探针约定）。

## 3. 实例凭证注入契约

### 3.1 mongo（行为变更）

* 实例容器环境变量（init 容器与主容器）：
  * `MONGO_INITDB_ROOT_USERNAME` = profile `admin_username`（直接值）
  * `MONGO_INITDB_ROOT_PASSWORD` = `generateStablePassword(app, environmentName, serviceName)`（直接值；算法与输入序不变）
* 不再创建/管理实例 Secret；`MongoDBWorkload` 不再有 Secret 资源名。
* 存量实例 Secret：随该环境下次 apply 被 prune 删除（按 `dominion.io/environment` + `app.kubernetes.io/managed-by` 标签，不在期望集合即删）；环境删除路径不变。

### 3.2 redis（新增）

* 实例容器环境变量：`REDIS_PASSWORD` = `generateStablePassword(app, environmentName, serviceName)`（redis key 材料，见 §3.3）。
* 认证形态：密码认证（redis default 用户，无用户名）；`--requirepass` 由容器命令引用 `$REDIS_PASSWORD` 生效。

### 3.3 派生算法（redis，客户端/实例两侧逐字节一致）

```
inputs = (app.trim(), environmentName.trim(), serviceName.trim())
mac    = HMAC-SHA256(key = "dominion-redis-stable-password", msg = inputs.join("\x00"))
pwd    = mac.digest().map(b => alphabet[b % 62]).join("")   // alphabet = [a-zA-Z0-9]
pwd    长度不足 24 时循环扩展摘要字节直至 ≥24
```

mongo 派生算法与输入序不变（key `dominion-mongo-stable-password`）。

## 4. profile（static_config.yaml）

```yaml
redis:
  dev-single:
    image: "redis"
    version: "8.4"
    port: 6379
    storage:
      storage_class_name: "truenas-iscsi-v2"
      capacity: "4Gi"
      access_modes: ["ReadWriteOncePod"]
      volume_mode: "Filesystem"
```

`static_config.yaml` 经 `go:embed` 内嵌于 deploy service；profile 变更需随 deploy service 发布生效。

## 5. describe/list 观测

infra 服务条目展示格式沿用现状（`[infra: redis]`）；per-service rollout 状态对 redis Deployment 生效（Deployment 观测路径复用）。

## 6. 兼容性

* 既有 mongodb infra 配置与实例行为除 §3.1 凭证注入方式外不变；密码不变（同输入同派生）。
* 用户业务 Secret（`deploy.yaml` secrets 绑定、`DOMINION_SECRET_DIR` 投影机制）不受影响（`tools/release/deploy/README.md` §Secret 配置）。
* deploy CLI 与 deploy service 需同步升级：旧 CLI 可提交 `resource: redis`（proto 为自由 string）但旧服务端 converter 拒绝；新 CLI 对旧服务端提交 redis 同样在 converter 处拒绝——错误信息明确。
