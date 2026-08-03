# Quickstart: Deploy Scope Removal

**Feature**: 033-deploy-scope-cleanup
**Date**: 2026-08-03
**Spec**: [spec.md](spec.md)

## 前提条件

1. 仓库已 clone，Bazel 工作区可用。
2. deploy service 可达（默认 `http://infra.liukexin.com`），或启动本地 mock server。
3. Go 工具链通过 `bazel run //:go` 可用。

## 验证步骤

### 1. 编译验证

```bash
# CLI 工具编译
bazel build //tools/release/deploy/v3:deploy_v3

# 后端 service 编译
bazel build //projects/infra/deploy:go_default_library
```

**预期**：编译成功，无错误。

### 2. 单元测试验证

```bash
# CLI 单测
bazel test //tools/release/deploy/v3:deploy_test

# 后端 service 单测
bazel test //projects/infra/deploy:go_default_test
```

**预期**：所有测试通过。

### 3. CLI 行为验证（scope 命令移除）

```bash
# scope 命令应返回 unknown command 错误
bazel run //tools/release/deploy/v3:deploy_v3 -- scope
```

**预期**：stderr 输出包含 `unknown command: scope`，退出码 1。

### 4. CLI 行为验证（--scope flag 从 apply/del/describe 移除）

```bash
# apply 传 --scope 应返回 flag 解析错误
bazel run //tools/release/deploy/v3:deploy_v3 -- apply --scope=team deploy.yaml
```

**预期**：stderr 输出 flag 解析错误（`unknown flag: --scope`），退出码 1。

### 5. CLI 行为验证（短名拒绝）

```bash
# del 传短名应返回完整格式错误
bazel run //tools/release/deploy/v3:deploy_v3 -- del dev
```

**预期**：stderr 输出错误信息说明需要完整 `{scope}.{env_name}` 格式，退出码 1。

### 6. CLI 行为验证（list --scope 可选）

```bash
# list 不指定 --scope，验证请求发送到 deploy/scopes/-/environments
# 需要可达的 deploy service 或 mock server
bazel run //tools/release/deploy/v3:deploy_v3 -- list --endpoint=http://localhost:PORT
```

**预期**：列出所有 scope 的所有环境，每行使用实际完整环境名（如 `alice.dev`），而非 `-`。

```bash
# list 指定 --scope，验证只列出该 scope 的环境
bazel run //tools/release/deploy/v3:deploy_v3 -- list --scope=alice --endpoint=http://localhost:PORT
```

**预期**：仅列出 scope 为 `alice` 的环境。

### 7. 后端通配符验证（handler 单测）

验证 `ListEnvironments` 接受 `deploy/scopes/-` parent 时返回所有 scope 的环境：

```bash
bazel test //projects/infra/deploy:go_default_test -- --run='.*ListEnvironments.*'
```

**预期**：测试通过，包含 `deploy/scopes/-` parent 的测试用例返回所有 scope 的环境，且每个环境名使用 canonical resource name。

### 8. help 输出验证

```bash
bazel run //tools/release/deploy/v3:deploy_v3 -- --help
```

**预期**：
- 命令列表中无 `scope` 行。
- apply/del/describe 命令行中无 `[--scope=name]`。
- list 命令行中保留 `[--scope=name]`。

### 9. README 文档验证

检查 `tools/release/deploy/README.md`：
- 无 `deploy scope` 命令文档。
- 无默认 scope 配置说明。
- 无短名展开说明。
- 环境名格式部分说明完整名始终为必需。
- list 命令部分说明 `--scope` 为可选过滤参数。

## 验证总结

| 步骤 | 验证内容 | 对应 FR |
|------|----------|---------|
| 1 | 编译通过 | — |
| 2 | 单测通过 | SC-005 |
| 3 | scope 命令移除 | FR-001 |
| 4 | --scope flag 移除（apply/del/describe） | FR-002 |
| 5 | 短名拒绝 | FR-004, FR-005, FR-010 |
| 6 | list 跨 scope 与可选 --scope | FR-007, FR-008 |
| 7 | 后端通配符支持 | FR-013, FR-014, FR-016, FR-017 |
| 8 | help 输出更新 | FR-011 |
| 9 | README 文档更新 | FR-012 |
