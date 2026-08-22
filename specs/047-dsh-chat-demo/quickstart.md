# Quickstart: dsh Chat Demo 验证指南

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-22

端到端验证 demo：三个服务（gateway / dsh agent / fake-llm）+ `third_party/dsh/core` 底座按 FR-008/SC-001~005 完成验收。契约细节见 [contracts/](contracts/)，不在此重复。

## 0. 前置条件

- 仓库构建环境（bazel、pnpm workspace、testplan skill 可用——`AGENTS.md`）。
- dsh 依赖已 pin 入根 `pnpm-lock.yaml`（`third_party/dsh/core/package.json` + `experimental/dsh/demo/agent/package.json` 声明后按 AGENTS.md 流程 `pnpm up` → gazelle → `bazel mod tidy`）。

## 1. 构建 + 单测（Constitution IV：每次变更必跑）

```bash
bazel build //experimental/dsh/demo/... //third_party/dsh/core/...
bazel test  //experimental/dsh/demo/... //third_party/dsh/core/...
```

**预期**: 全绿。fake-llm 单测覆盖模板匹配（含多轮条件与优先级，[contracts/fake-llm-templates.md](contracts/fake-llm-templates.md) §3）与 SSE 帧序列（[contracts/fake-llm-wire.md](contracts/fake-llm-wire.md) §3 不变量）；agent 服务单测覆盖 handler 会话映射逻辑（mock dsh Context）。

## 2. 闭包审计（SC-004）

```bash
bazel test //experimental/dsh/demo/testplan:closure_audit_test
```

**预期**: 通过——底座零插件包、tar 内 dsh-* 包全部可溯源到服务声明、同名包版本唯一（[contracts/dsh-agent-service.md](contracts/dsh-agent-service.md) §4 审计断言）。

## 3. 大型测试（Constitution VI 验收，FR-008）

按 `style/large_test.md` 与 testplan skill 执行：

```bash
# 经 testplan skill（guitar CLI）：
guitar run experimental/dsh/demo/testplan/interface_test.yaml
```

**部署面**: `deploy.yaml` 三服务 + ingress（PathPrefix `/experimental/dsh-demo`，hostname `apitest.liukexin.com`）；agent 侧 env 注入 `FAKE_LLM_API_KEY`（dummy）与 resolver 所需环境（部署系统注入）。

**预期（全部用例通过，零 failed/flaky）**:

| 用例组 | 覆盖 | 断言锚点 |
|---|---|---|
| ChatRoundTrip | US1-1/2/3：命中模板逐字一致、重复确定性、兜底回复 | [contracts/chat-api.md](contracts/chat-api.md) §4 |
| MultiTurn | US2-1/2/3：多轮分支、会话隔离、并发交错 | 同上 |
| Robustness | Edge：空字段 400 | 同上 |

用例经 `testtool.MustEndpoint("http","public")` 寻址网关，`POST /experimental/dsh-demo/conversations/{id}:sendMessage`（[contracts/chat-api.md](contracts/chat-api.md)）。

## 4. 手动冒烟（可选，部署存续期间）

```bash
curl -X POST https://apitest.liukexin.com/experimental/dsh-demo/conversations/smoke-1:sendMessage \
  -H 'content-type: application/json' -H "x-dominion-env: <env>" \
  -d '{"message":"hello"}'
# 预期: {"name":"conversations/smoke-1","reply":"Hello! How can I help you today?"}
# 同会话再发 {"message":"hello"} → 多轮分支 reply（US2-1 行为）
```

## 5. 排障指引

- **agent 启动即退**（fail-loud，FR-009）: 查进程日志的组合诊断（行解析/peer/激活错误）——`signoz` skill 查 `dsh-demo/agent` 日志。
- **回复不符/超时**: 依次核对 ① `FAKE_LLM_BASE_URL` 是否注入（bootstrap 日志有 resolved endpoint 记录）② fake-llm 是否命中预期模板（其结构化日志）③ 回合事件序列（`session/event` 相关日志）。
- **native addon 加载失败**: `node-addon-require-builtin` 与镜像 base（distroless nodejs24-debian12 / glibc 2.36）兼容性问题——见 `survey/deepseek-harness-b1-bazel-packaging.md` §7 风险 2。

## 6. 验收对照（Done 定义）

| SC | 验证方式 |
|---|---|
| SC-001/002/005 | §3 大型测试全绿 |
| SC-003 | §3 guitar run 完整闭环（部署→用例→清理）执行记录 |
| SC-004 | §2 闭包审计通过 |
