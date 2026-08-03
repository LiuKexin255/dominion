# Quickstart: Guitar Deploy Failure Environment Diagnostics

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-02（初版）, 2026-08-03（修订：per-service 状态验证）

本文档为可运行的端到端验证指南，证明本特性（per-service 状态 + describe 主线 + guitar 诊断）工作正常。分两部分：①deploy service / describe 侧单测验证（确定性，不依赖远端部署）；②端到端冒烟（依赖 deploy service 新版上线 infra.liukexin.com）。

## 前置条件

- 仓库已 `bazel build //...` 通过。
- deploy service endpoint 可达：`http://infra.liukexin.com`（`.env/cli.json` 默认 scope `liukexin`）。
- 可用的测试 testplan：`experimental/ts/grpc_hello_world/testplan/interface_test.yaml`（deploy.yaml 含 2 个 artifact：service + gateway，env 名 `liukexin.{{run}}`）。
- **端到端冒烟（第二部分）额外前置**：deploy service **新版本**（含 `EnvironmentStatus.services` 字段）已部署到 infra.liukexin.com。deploy service 因无法自举（`projects/infra/deploy/README.md:24-28`）经其独立 k8s 部署流程上线，**不**经 guitar/testplan——故端到端冒烟须在 deploy service 新版上线后进行；若远端仍为旧版，第二部分会回退到初版行为（无 per-service 文本），此时第一部分单测仍为权威验收。

## 第一部分：单测验证（确定性，主验收门禁）

deploy service 不进行大型测试（`projects/infra/deploy/README.md:28`），故单测是 deploy service 侧变更的权威验收。

```bash
# deploy service 全量单测（proto 映射 / domain / reconcile / storage / runtime / handler）
bazel test //projects/infra/deploy/...

# deploy CLI v3（describe per-service 输出）
bazel test //tools/release/deploy/v3:deploy_test

# guitar（诊断 hook 行为，无改动，回归）
bazel test //tools/test/guitar/pkg/run:run_test
```

**须新增/扩展的断言点**（实现阶段在 tasks.md 细化，此处为验证目标）：

- `domain`：`buildInitialServiceStatuses` 对 artifacts+infras 产出正确 PENDING 列表；`cloneStatus` 深拷贝 Services（修改副本不影响原）。
- `service/reconcile_test.go`：`applyAndWait` 转移写入初始 PENDING Services；`retainWaitingRollout`/`markFailedFromRollout`/`markReadyFromRollout` 持久化 CheckRollout 返回的 Services；`transitionToReconciling`/`transitionToDeleting` 清空 Services。
- `storage/mongo_test.go`：Services 往返（写入→读回一致）；nil Services 清空 stale（先写非空再写 nil → 读回为空）。
- `runtime/k8s/rollout_test.go`：`CheckRollout` 产出 per-service 列表（含 failed 时其余服务状态仍上报，验证不再 early-return）；env-level State/Message 由 per-service 正确派生。
- `handler_test.go`：`GetEnvironment` 返回的 `status.services` 与 domain 一致。
- `v3/describe_test.go`：per-service 状态文本内联（READY/WAITING/FAILED/PENDING）；有 per-service 数据时无 `说明:` 行；无 per-service 且 message 非空时输出 `说明:`。

**通过标准**：上述全部 `bazel test` 用例 PASS。

## 第二部分：端到端冒烟（依赖 deploy service 新版上线）

> 本部分为**冒烟验证**，非强制 testplan 门禁（deploy service 因无法自举不进行大型测试，见 [../plan.md](./plan.md) Constitution Check 原则 VI）。目的是在 deploy service 新版上线后，人工确认 per-service 状态在真实 guitar 失败诊断中可见。

### 场景 A：部署超时（验证 per-service 状态 + 时序空窗消除）

用极短 `--timeout` 触发部署超时，观察诊断输出是否列出每个服务及其状态。

```bash
guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml --timeout=5s
```

**预期**（deploy service 新版上线后）：
- 控制台在 `  --- 环境状态 (env=liukexin.<run>) ---` 头部后，顶格输出 describe：
  - `状态: 等待滚动发布`（或 `部署中`，取决于首个 checkRollout 是否完成）
  - `服务:` 列表，每个 artifact 一行，含 per-service 状态文本：
    - 若在空窗期（applyAndWait 已写、checkRollout 未完成）：`  - service (app=grpc-hello-world-ts) [artifact] 已提交，等待观测` + `  - gateway (...) [artifact] 已提交，等待观测`
    - 若 checkRollout 已完成：失败/等待的服务显示 ` 等待发布: ...` 或 ` 失败: ...`，已就绪的显示 ` 就绪`
  - **无 `说明:` 行**（有 per-service 数据时按契约不输出）
- 原始错误保留：`suite default: failure, error: deploy apply ...: signal: killed`
- cleanup 仍执行：`环境 liukexin.<run> 已删除`

**对比初版**：初版同场景 `说明:` 为空（message 未填充）、服务列表无状态文本；修订后 per-service 状态稳定可见。

### 场景 B：部署成功（验证零影响，FR-008）

```bash
guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml --timeout=60s
```

**预期**：
- apply 成功，**不**打印 `  --- 环境状态 ---` 头部、**不**调用 describe（成功路径不进诊断分支）。
- 测试通过（或按测试本身结果），输出与未引入本特性一致。

### 场景 C：standalone describe（READY 环境）

部署一个环境后，直接调 describe 观察 READY 态 per-service 状态：

```bash
# 先 apply 一个环境（手动或借用 testplan 的 deploy.yaml）
deploy apply --run smoke032 experimental/ts/grpc_hello_world/testplan/deploy.yaml
# 轮询至就绪后
deploy describe liukexin.smoke032
# 清理
deploy del liukexin.smoke032
```

**预期**：
```
环境 liukexin.smoke032
状态: 就绪
服务:
  - service (app=grpc-hello-world-ts) [artifact] 就绪
  - gateway (app=grpc-hello-world-ts) [artifact] 就绪
最近调和: <ts>
最近成功: <ts>
```
- 每个 artifact 显示 ` 就绪`，无 `说明:` 行（READY message 历来为 "ready"，与状态行冗余，契约规定有 per-service 时不输出）。

### 场景 D：环境不存在（降级，FR-005）

```bash
deploy describe liukexin.nonexistent032
```

**预期**：输出 `环境 liukexin.nonexistent032 不存在`，退出码 1。guitar 场景下转为 stderr warning，原始部署错误保留。

## 非终端输出边界（FR-009）

```bash
guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml --timeout=5s 2>&1 | cat
```

**预期**：管道输出无 ANSI 颜色码（describe 输出本就无色；Reporter 头部行遵循 `checkTerminal` 降级）。

## 验收门禁总结

| 层 | 门禁 | 责任 |
|----|------|------|
| deploy service / describe / guitar 单测 | 第一部分 `bazel test` 全 PASS | **主验收**（deploy service 不进行大型测试） |
| 端到端冒烟（per-service 可见、零影响、降级） | 第二部分场景 A~D 人工确认 | 冒烟（依赖 deploy service 新版上线，非阻塞门禁） |

## 参考

- 数据契约：[contracts/environment-status.md](./contracts/environment-status.md)
- describe 契约：[contracts/deploy-describe.md](./contracts/deploy-describe.md)
- guitar 契约：[contracts/guitar-integration.md](./contracts/guitar-integration.md)
- 输出模型：[data-model.md](./data-model.md)
- deploy service 大型测试例外：`projects/infra/deploy/README.md:28`、[plan.md](./plan.md) Constitution Check 原则 VI
