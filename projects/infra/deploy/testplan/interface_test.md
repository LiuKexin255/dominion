# 测试计划

* name：deploy 服务接口测试
* deploy：//projects/infra/deploy/test_deploy.yaml

## Preconditions

- 已安装 `deploy` 工具：`bazel run //:deploy_install`。

## Automated procedure

### 1. 部署 Mongo 与 deploy 服务

```bash
deploy apply //projects/infra/deploy/test_deploy.yaml
```

Expected outcome:

- 命令退出码为 0。

### 2. 运行 HTTP CRUD 持久化集成测试

```bash
bazel test //projects/infra/deploy:integration_test --test_output=errors
```

Expected outcome:

- Bazel 成功执行目标。
- 用例覆盖通过 HTTP API 的 create/get/list/update/delete 流程。
- `TestIntegration_InvalidUpdateDoesNotCorruptPersistence` 返回 `400 Bad Request`，并验证持久化数据未被破坏。

### 3. 清理测试环境

```bash
deploy del deploy.iface
```

Expected outcome:

- 命令退出码为 0。
- Mongo infra 与 deploy 服务对应资源被清理。
