# @dominion/dsh-llm-opencode-go (`common/js/dsh-plugins/llm-opencode-go`)

opencode-go 订阅网关的 OpenAI **Chat Completions** 协议 dsh LLM 适配插件：以
cordis 插件形态（`name = 'llm-opencode-go'`、`inject = ['llm']`）向 dsh LLM
适配缝注册 provider 路由 `opencode-go`，对接 opencode-go 网关
（Base URL `https://opencode.ai/zen/go/v1`，
https://opencode.ai/docs/zh-cn/go/ ）。插件契约（包声明、cordis 导出、请求
序列化、SSE 事件映射、适配器义务）见
`specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md`。

v1 仅覆盖 Chat Completions 线协议
（`specs/063-llm-reliability-opencode-go/spec.md` FR-015 裁定）：默认目录为官方
文档该路由的全部 16 个模型；Responses / Anthropic Messages 路由模型排除。

## 依赖 pin 决策

- dsh 家族 peer `@deepseek-ai/dsh-llm` 按精确版本 `0.1.1-rc.2` pin（无前缀）：
  dsh 家族按 0.1.1-rc.2 线锁定是仓库既定决策（`third_party/dsh/core` 同线），
  依据 `specs/049-agent-v2-dsh-init/research.md` D1。
- deps 取 llm-glm 的最小集（`common/js/dsh-plugins/llm-glm/README.md` 依赖 pin
  节；本包依赖清单的契约见
  `specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §1）：
  `eventsource-parser`（SSE 解析）+ `@deepseek-ai/schemastery`（Config
  schema）+ `@deepseek-ai/dsh-timeout`（`idleWatchdog` 流停滞看护）；peer 裁剪至
  `@deepseek-ai/dsh-llm` + `@deepseek-ai/cordis`（不消费 credentials/settings
  等宿主设施）。
- token 不读取文件系统 secret：宿主 bootstrap 三级解析后注入 env
  （`specs/063-llm-reliability-opencode-go/research.md` D13）。

## 配置（cordis 行）

| 配置项 | 默认 | 说明 |
|---|---|---|
| `apiKeyEnv` | `OPENCODE_API_KEY` | 空值 → 请求不携带 Authorization（条件携带） |
| `baseURL` | `https://opencode.ai/zen/go/v1` | endpoint 基址（含版本路径） |
| `models` | 下表 16 项 | 静态目录（advisory `resolveModel`） |
| `retryPolicy?` | dsh 默认 | dsh `RetryPolicySchema` 透传 |
| `streamIdleTimeoutMs?` | 300000 | 流停滞看护窗口（上限 `MAX_TIMER_DELAY_MS`） |

## 默认模型目录

官方 Chat Completions 路由的 16 个文档化模型
（`specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §2
唯一来源；id/`contextWindow` 取 models.dev `opencode-go` 快照，advisory 且部署
可调）：

| id | contextWindow |
|---|---|
| `OPENCODE_MODEL` env ‖ `glm-5.3` | 1000000 |
| `glm-5.3-flash` | 1000000 |
| `glm-5.2` | 1000000 |
| `glm-5.1` | 202752 |
| `kimi-k3` | 1048576 |
| `kimi-k2.7-code` | 262144 |
| `kimi-k2.6` | 262144 |
| `longcat-2.0` | 1000000 |
| `deepseek-v4.1-flash` | 1000000 |
| `deepseek-v4-pro` | 1000000 |
| `deepseek-v4-flash` | 1000000 |
| `deepseek-v4-flash-vision-exp` | 1000000 |
| `mimo-v2.5` | 1000000 |
| `mimo-v2.5-pro` | 1048576 |
| `hy4-preview` | 1024000 |
| `hy3` | 256000 |

目录外 id 经 `resolveModel` 以最小元数据 advisory 解析、不拒绝；选择面写入
校验仍按部署目录强校验
（`specs/063-llm-reliability-opencode-go/spec.md` FR-014）。

## 失败分类与看护

失败分类、重试声明、Retry-After、空补全分类、idle 看护与传输清理义务与
`llm-glm` 共用同一契约（
`specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md`
§1-§2，`specs/063-llm-reliability-opencode-go/spec.md` FR-016）。本插件差异项：

- 请求头：条件 `Authorization: Bearer` + `attributionHeaders()`（含产品自定义
  User-Agent，不含凭据）+ `x-opencode-session`（取 `options.sessionId`，未提供
  则省略）。
- provider 展示元数据：`{id: provider, name: "OpenCode Go (Chat Completions)"}`。

## 测试

```bash
bazel test //common/js/dsh-plugins/llm-opencode-go:lib_test
```

serialize / wire / adapter 三组单测覆盖
`specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §6 的
全部分支（序列化全表、SSE 帧序与空补全、失败分类/看护/清理、条件 Authorization
与会话头、默认目录完整性）。

## 参照源码

- Chat Completions 序列化/翻译先例：官方适配插件
  [packages/llm/llm-deepseek](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/llm/llm-deepseek)
  （本地物化：`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm-deepseek/`）
- 适配器协议义务：dsh cookbook
  [adding-an-llm-adapter](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md)
- `LlmAdapter`/`StreamChunk` 契约：
  [packages/llm/llm/src/index.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm/src/index.ts)
  （本地物化：`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm/lib/types/index.d.ts`）
- Responses wire 同族对照：`common/js/dsh-plugins/llm-glm/README.md`
