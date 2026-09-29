# @dominion/dsh-llm-glm (`common/js/dsh-plugins/llm-glm`)

GLM codingplan 的 OpenAI **Responses** 协议 dsh LLM 适配插件：以 cordis 插件形态
（`name = 'llm-glm'`、`inject = ['llm']`）向 dsh LLM 适配缝注册 provider 路由
`glm-responses`，对接 GLM codingplan 端点
（Base URL `https://open.bigmodel.cn/api/v1`，
https://docs.bigmodel.cn/cn/coding-plan/tool/others ）。插件契约（包声明、cordis 导出、
适配器协议义务、SSE 事件映射）见
`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md`。

自研 Responses 适配器的原因：官方 llm 家族无 OpenAI **Responses** 协议的适配器行
（`dsh-llm-deepseek` 为 DeepSeek Messages 传输库、Anthropic Messages
wire；`specs/070-dsh-upgrade/research.md` D7「demo LLM 行换线」终态结论）。

## 依赖 pin 决策

- dsh 家族 peer `@deepseek-ai/dsh-llm` 经 catalog 解析到精确版本 `0.2.0-rc.2`：
  dsh 家族按 0.2.0-rc.2 线锁定是仓库既定决策（`third_party/dsh/core` 同线），
  依据 `specs/073-dsh-0.2-upgrade/research.md` D1。
- deps 取官方适配器 `dsh-llm-deepseek` 的最小集：`eventsource-parser`（SSE 解析，
  catalog 统一管理，版本对齐官方声明 `^3.1.0`）、`@deepseek-ai/schemastery`
  （Config schema，catalog `^3.18.4`），并把官方以 peer 声明的
  `@deepseek-ai/dsh-timeout` 列为直接依赖（流停滞看护 `idleWatchdog` 的实现载体）；
  peer 裁剪至 `@deepseek-ai/dsh-llm` + `@deepseek-ai/cordis`（不消费
  credentials/settings 等宿主设施）。

## 失败分类与看护

适配器抛出的 `LlmError` 全部使用 dsh 共享失败码分类学
（`TRANSPORT`/`TIMEOUT`/`RATE_LIMIT`/`SERVER`/`EMPTY_RESPONSE`/`AUTH`/`QUOTA`/
`CONTEXT_WINDOW_EXCEEDED`/`INVALID_REQUEST`/`HTTP_<status>`/`STREAM_CLOSED`/
`MALFORMED_RESPONSE`）；带内失败保持 provider 原码（缺失时 `UNKNOWN` 兜底，非
共享码集合成员）——两者同受 `llm-retry` 按码路由，组合中既有默认策略因此生效
（可重试集合 `[EMPTY_RESPONSE, RATE_LIMIT, SERVER, TIMEOUT, TRANSPORT]`）。
完整判定表与适配器义务（错误体仅分类不回显、Retry-After、空补全、idle 看护、
传输清理、abort 语义）见
`specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md`。

- `retryPolicy`（可选，dsh `RetryPolicySchema` 透传）经 `providerRetryPolicy()`
  声明；缺省回退 dsh 默认（normal、5 次、500ms→10s）。
- `streamIdleTimeoutMs`（可选，默认 300000）为流停滞看护窗口：SSE comment 帧
  作为活动 pulse 重置窗口，超时转为可重试 `TIMEOUT`。

## 参照源码

- 官方适配插件实现先例：
  [packages/llm/llm-deepseek](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/llm/llm-deepseek)
- 适配器协议义务：dsh cookbook
  [adding-an-llm-adapter](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md)
- `LlmAdapter`/`StreamChunk` 契约：
  [packages/llm/llm/src/index.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm/src/index.ts)
  （本地物化：`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm/lib/types/index.d.ts`）
