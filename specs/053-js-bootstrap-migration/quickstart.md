# Quickstart: JS bootstrap 组件与 experimental 目录统一为 js — 验证指南

**Feature**: [spec.md](spec.md) | **Date**: 2026-09-01

本文件为端到端**验证指南**（非实现步骤；实现任务见 tasks.md）。场景按依赖顺序排列，§5 为最终验收（宪法原则 VI）。契约引用：[contracts/bootstrap-js-api.md](contracts/bootstrap-js-api.md)、[contracts/migration-rename-map.md](contracts/migration-rename-map.md)。

## 0. 前置条件

- 仓库根执行；bazel 可用（`bazel version`）。
- 已按 tasks.md 完成实现（本指南验证终态，不用于驱动开发）。

## 1. 静态审计：目录与标识符统一（spec SC-003）

执行 [contracts/migration-rename-map.md](contracts/migration-rename-map.md) §3 的四条审计命令。

**预期**：全部命令零输出/退出码符合断言（无 `experimental/ts`、`experimental.ts.`、`grpc-hello-world-ts` 残留；`experimental/ts` 目录不存在）。

## 2. 构建与单元测试门禁（原则 IV）

```bash
bazel build //...
bazel test //common/js/bootstrap/... //experimental/js/... //tools/dev/js/...
```

**预期**：
- 全部通过；
- `//common/js/bootstrap:lib_test` 覆盖 [bootstrap-js-api.md](contracts/bootstrap-js-api.md) §3.1/§5/§6 的核心语义（排序、回滚、健康 FIFO、关停预算、daemon 分类与退避、适配器两阶段停止）；
- `//experimental/js/grpc_hello_world:smoke_test` 通过（打包 tar 无 MODULE_NOT_FOUND）。

## 3. 本地运行验证：grpc_hello_world 接入共享 bootstrap（spec FR-013）

```bash
# 经 bazel 直接运行服务入口（或按 js_binary target 名称调整）
bazel run //experimental/js/grpc_hello_world:server &
sleep 3
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:38080/healthz   # 探活
curl -s http://127.0.0.1:38080/healthz                                  # 响应体
kill -TERM %1                                                           # 触发优雅退出
wait %1; echo "exit=$?"
```

**预期**（对齐 052 契约与 [bootstrap-js-api.md](contracts/bootstrap-js-api.md) §3.1/§4）：
- `/healthz` 返回 `200` 与 `ok\n`；其余路径 404；
- 进程日志按序出现：组件启动 → health 启动（端口字段）→（SIGTERM）→ health 先停 → 组件逆序停止 → 进程退出；
- 退出码 `0`（干净信号退出 resolve → 服务入口 exit(0)）。

## 4. 本地运行验证：team_graph_spike 接入（spec FR-013，含新增健康端点）

构建并运行 `experimental/js/team_graph_spike` 服务（本地无需 fake-llm，仅验证生命周期）：

**预期**：
- 启动后 `curl http://127.0.0.1:38080/healthz` 返回 `200 ok\n`（接入新增的预期行为，spec Clarifications 第 4 条）；
- 业务端口 `:8080` 的既有 `/health`、`/invoke` 行为不变（前者 200）；
- SIGTERM 后进程按 bootstrap 语义优雅退出（health 先停）；`bazel test //experimental/js/team_graph_spike/...` 单测通过。

## 5. 大型测试验收（原则 VI：实际执行，全部用例通过）

```bash
# testplan skill：guitar run <plan.yaml>，完成部署→测试→清理闭环
guitar run experimental/js/grpc_hello_world/testplan/interface_test.yaml
```

**预期**（单计划双 suite，见 `style/large_test.md`）：
- **default suite**：部署 `experimental/js/grpc_hello_world`（app `grpc-hello-world-js`）+ gateway，经公共端点 `https://apitest.liukexin.com/experimental/js/grpc-hello-world/say-hello` 验证配置驱动问候语与 `GREETING_SUFFIX` 叠加（**新路径**生效，映射 M5）；部署 READY 即证明 startupProbe 通过（health 端点由共享 bootstrap 提供）；
- **selfheal suite**：`HEALTH_STOP_AFTER_MS=60000` 注入后，观测"成功 → 判死失败窗口（≥1 次失败）→ 容器自动重启恢复"全周期（`specs/052-deploy-health-probe/contracts/verification-testplan.md` §2 时序约束）；
- 两个 suite 的全部 case 通过、清理完成，方为验收通过（任何 failed/flaky = 未通过）。

## 6. 行为不变性核对（spec FR-011/FR-013）

对照迁移前基线（`git show <pre-migration-commit>` 可取旧版 interface_test 结果）：

- interface_test 断言的问候语内容**不变**（config 值 `"hello from ts config"` 与 `ts-suffix` 均为保留的测试数据，[migration-rename-map.md](contracts/migration-rename-map.md) §2）；
- 变化的只有：路径前缀（`ts`→`js`）、app 名、目录、importpath——即映射表 M1-M15 项，无表外行为差异。
