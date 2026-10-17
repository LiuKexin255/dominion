# Quickstart: Redis 基础设施支持验证指南

**Feature**: [spec.md](spec.md) | **Contracts**: [contracts/deploy-redis-infra.md](contracts/deploy-redis-infra.md) · [contracts/common-db-libs.md](contracts/common-db-libs.md) · [contracts/redis-demo.md](contracts/redis-demo.md)

本指南给出特性完成后的端到端验证场景。实现细节见 tasks 阶段产物。

## 前置条件

* deploy CLI 已更新安装：`bazel run //:deploy_install`（schema 含 `resource: redis`）。
* deploy service 已更新部署（static_config 含 `redis.dev-single`、converter 支持 redis、凭证注入改造生效）。
* 集群可访问；`guitar` CLI 可用（testplan skill）。

## 场景 1：单元与构建验证（每次变更）

```bash
bazel build //...
bazel test //tools/release/deploy/... //projects/infra/deploy/... \
  //common/gopkg/redis/... //common/js/redis/... //common/js/mongo/... \
  //projects/game/agent_v2/... //experimental/redis_demo/... //experimental/golang/mongo_demo/...
```

预期：全部通过；`tools/release/deploy/pkg/schema` 的 unknown-resource 负例已改为 `mysql` 等非法值并通过。

## 场景 2：redis infra 部署面验证

```bash
# 使用 demo 的部署拓扑（或最小 deploy.yaml：仅 redis infra）
deploy apply //experimental/redis_demo/testplan/deploy.yaml --run <run-id>
deploy describe redis.<run-id>
```

预期：

* describe 显示 `- redis (app=redis-demo) [infra: redis]` 且状态**就绪**。
* 集群内 `redis-<env>-redis-<hash>` Deployment / `svc-...` Service 存在；**无** `secret-...` 实例 Secret。
* mongo 实例（如已部署）同样新增无 Secret；对存量环境重新 apply 后旧 `secret-...` 被自动删除。

## 场景 3：demo 大型测试（端到端闭环）

```bash
guitar run //experimental/redis_demo/testplan/interface_test.yaml
```

预期（对应 [contracts/redis-demo.md §4](contracts/redis-demo.md) 用例）：

1. go 自环 set/get 一致；2. js 自环一致；3. go set → js get 一致；4. js set → go get 一致；5. delete 后 NOT_FOUND。
全部用例通过、环境自动清理（部署→测试→清理闭环）。

## 场景 4：agent_v2 回归（JS mongo 公共库替换）

```bash
bazel test //projects/game/agent_v2/...
```

预期：全部既有测试通过；`projects/game/agent_v2/src/presets.ts` 不再含本地 HMAC/端点解析实现（改用 `@dominion/common-js-mongo`）；`MONGO_URI` 直连优先行为保留（相关既有用例验证）。

## 场景 5：凭证一致性验证（SC-003）

凭证链路按以下三条口径验证：

1. **实例侧认证生效**：`guitar run` 的 demo 用例 1–4（自环与跨语言读写）全部通过，即证明客户端派生密码与实例注入密码一致（连接认证成功）。
2. **集群无实例 Secret**（含存量 mongo 环境）：

    ```bash
    kubectl -n <namespace> get secret \
      -l dominion.io/environment=<env>,app.kubernetes.io/managed-by=dominion.io
    # 预期：无 mongo/redis 实例 Secret；用户业务 Secret 不带该标签，不受影响
    ```

3. **存量兼容**：对既有 mongo 环境重新 `deploy apply` 后旧实例 Secret 被 prune 删除，实例密码不变、连接不中断。
