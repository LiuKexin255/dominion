# Quickstart: Guitar Deploy Failure Environment Diagnostics

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-02

本文件为验证指南：如何端到端证明本特性工作正常。不含完整实现代码（实现见后续 `tasks.md`）。

## 前置条件

1. 仓库可编译：`bazel build //tools/release/deploy/v3:deploy_v3 //tools/test/guitar/cmd:guitar`。
2. 已安装 `deploy` 与 `guitar` 到 `$PATH`：
   - `bazel run //:deploy_install`
   - `bazel run //:guitar_install`
3. 可访问 deploy service（默认 `http://infra.liukexin.com`；可用 `--endpoint` 覆盖）。
4. 具备一个可部署的环境配置（deploy.yaml），并能构造"部署不成功"场景（见下）。

## 单测验证（快速反馈，PR 必过）

```bash
bazel test //tools/release/deploy/v3:deploy_test     # 含新增 describe_test.go
bazel test //tools/test/guitar/pkg/run:run_test  # 含扩展的 deploy-failure 用例
```

**期望**：全绿。关键断言：
- `describe_test.go`：`describeCommand` 对 FAILED/READY/PENDING/WAITING_ROLLOUT/不存在 等场景输出正确字段（状态中文、message、服务列表、时间戳），格式见 [data-model.md](./data-model.md) 输出模型。
- `del_list_test.go`（`TestListCommand`）：增补 WAITING_ROLLOUT 环境断言（`formatState` 扩展波及 list）。
- guitar `run_test.go` 的 "deploy failure" 用例：调用顺序为 `apply → describe → del`；`describe` 实参含 `--timeout=10s` 与 `fullEnvName`；`describe` 收到的 ctx `Err()==nil`（非取消上下文，外层 ctx 已取消）；`SuiteResult.Err` 为原始 apply 错误（未被诊断掩盖）。

## 端到端验证 1：`deploy describe` 单命令（人工可跑）

**目的**：验证新 `describe` 命令对真实环境的输出。

1. 部署一个可就绪的测试环境（或复用已存在的 test 环境）：
   ```bash
   deploy apply --run ltabcd01 //path/to/some/deploy.yaml
   ```
2. 待其就绪后查询：
   ```bash
   deploy describe game.ltabcd01
   ```
3. **期望输出**（字段顺序见 [contracts/deploy-describe.md](./contracts/deploy-describe.md)）：
   ```
   环境 game.ltabcd01
   状态: 就绪
   服务:
      - <service> (app=<app>) [artifact]
   最近调和: <RFC3339>
   最近成功: <RFC3339>
   ```
4. 查询不存在的环境：
   ```bash
   deploy describe game.nonexist
   ```
   **期望**：输出 `环境 game.nonexist 不存在`，退出码非零。
5. 清理：`deploy del game.ltabcd01`。

## 端到端验证 2：guitar 部署失败时打印诊断（核心场景）

**目的**：验证 spec FR-001~FR-004、SC-001/SC-002/SC-003。

构造部署失败场景的二选一：

- **A. 超时场景**：准备一个 deploy.yaml，其 suite `timeout` 或全局 `--timeout` 设得很短（如 `--timeout=20s`），使环境未及就绪即超时。
- **B. 失败终态场景**：准备一个 deploy.yaml 引用一个会拉镜像失败/无法就绪的 service（例如不存在的 image target），使环境达到 `FAILED`。

执行：

```bash
guitar run <plan-with-failing-deploy.yaml>
```

**期望**（控制台）：
- 出现 `--- Suite: <name> ---` 头与 `  Deploy`。
- 部署失败后出现醒目诊断头部行 `  --- 环境状态 (env=<scope>.<runID>) ---`，紧随其后是顶格的 `deploy describe` 详情文本：
  - 场景 A（超时）：`状态:` 显示 `部署中` 或 `等待滚动发布`（区分超时未完成，满足 SC-002）。
  - 场景 B（失败）：`状态: 失败`，并出现 `说明: <失败原因，含哪个 service>`（满足 SC-001），`服务:` 列出本次部署的 service（满足 SC-003）。
- 随后 `  Cleanup` 正常执行，环境被删除（满足 FR-007/SC-004）。
- Summary 中该 suite 为 `failure, error: deploy apply ...`（原始错误未被改写，满足 FR-006）。

## 端到端验证 3：成功路径无影响（回归）

**目的**：验证 FR-008/SC-005。

```bash
guitar run <plan-with-healthy-deploy.yaml>
```

**期望**：输出与未引入本特性时完全一致——无诊断头部行、无 describe 文本；suite 成功；环境正常清理。

## 边界验证

- **环境创建前即失败**（如镜像推送失败 / deploy.yaml 配置错误）：deploy apply 在环境创建前报错，`describe` 会返回"环境不存在"。**期望**：guitar 向 stderr 打 warning，suite 仍以原始 apply 错误失败（满足 FR-005）。
- **非 TTY 输出**：`guitar run <plan> 2>&1 | tee out.log`，**期望**：日志中无 ANSI 颜色码（满足 FR-009）。
- **describe 自身失败**（deploy service 临时不可达）：**期望**：guitar 打 warning，不崩溃，cleanup 仍执行（满足 FR-006）。

## 通过判据

- 单测全绿。
- 端到端验证 1/2/3 与边界验证的"期望"全部符合。
- 失败场景下，用户能仅凭控制台诊断信息读出失败原因与涉及的 service（无需手动二次查询 deploy service）。
