# @dominion/dsh-llm-glm (`common/js/dsh-plugins/llm-glm`)

GLM codingplan 的 OpenAI **Responses** 协议 dsh LLM 适配插件：以 cordis 插件形态
（`name = 'llm-glm'`、`inject = ['llm']`）向 dsh LLM 适配缝注册 provider 路由
`glm-responses`，对接 GLM codingplan 端点
（Base URL `https://open.bigmodel.cn/api/v1`，
https://docs.bigmodel.cn/cn/coding-plan/tool/others ）。插件契约（包声明、cordis 导出、
适配器协议义务、SSE 事件映射）见
`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md`。

自研 Responses 适配器的原因：官方仅有 chat-completions wire 的 `dsh-llm-deepseek`
适配器，无 Responses 适配器（`specs/049-agent-v2-dsh-init/research.md` D1）。

## 依赖 pin 决策

- dsh 家族 peer `@deepseek-ai/dsh-llm` 按精确版本 `0.1.1-rc.2` pin（无前缀）：dsh
  家族按 0.1.1-rc.2 线锁定是仓库既定决策（`third_party/dsh/core` 同线），依据
  `specs/049-agent-v2-dsh-init/research.md` D1。
- deps 对齐官方适配器 `dsh-llm-deepseek` 的最小集：`eventsource-parser`（SSE 解析，
  catalog 统一管理，版本对齐官方声明 `^3.1.0`）+ `@deepseek-ai/schemastery`
  （Config schema，`^3.18.1`）；peer 裁剪至 `@deepseek-ai/dsh-llm` +
  `@deepseek-ai/cordis`（不消费 credentials/settings/timeout 等宿主设施）。

## 参照源码

- 官方适配插件实现先例：
  [packages/llm/llm-deepseek](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/llm/llm-deepseek)
  （本地物化：`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm-deepseek/`）
- 适配器协议义务：dsh cookbook
  [adding-an-llm-adapter](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md)
- `LlmAdapter`/`StreamChunk` 契约：
  [packages/llm/llm/src/index.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm/src/index.ts)
  （本地物化：`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm/lib/types/index.d.ts`）
