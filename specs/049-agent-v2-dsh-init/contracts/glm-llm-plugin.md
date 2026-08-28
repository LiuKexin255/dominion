# Contract: `@dominion/dsh-llm-glm` 插件（GLM codingplan Responses 适配器）

**Feature**: [spec.md](spec.md) FR-007/FR-008 | **决策**: [research.md](../research.md) D1/D9

**位置**: `common/js/dsh-plugins/llm-glm/`（workspace 包，`common/js/**` glob 已覆盖）| **dsh 版本线**: 0.1.1-rc.2 精确 pin

## 1. 包契约

```jsonc
// package.json（@dominion/dsh-llm-glm）
{
  "name": "@dominion/dsh-llm-glm",
  "private": true,
  "type": "module",                    // ESM（specs/048 契约）
  "main": "./src/index.js",            // 编译产物（workspace 包运行期经 npm 链接解析；先例 common/js/otel/package.json，specs/048-js-esm-migration/contracts/esm-package-conventions.md）
  "types": "./src/index.d.ts",
  "exports": {
    ".": {
      "types": "./src/index.d.ts",
      "default": "./src/index.js"
    }
  },
  "dependencies": {
    "eventsource-parser": "catalog:",  // SSE 解析（对齐官方 dsh-llm-deepseek deps）
    "@deepseek-ai/schemastery": "^3.18.1"
  },
  "peerDependencies": {
    "@deepseek-ai/dsh-llm": "0.1.1-rc.2",
    "@deepseek-ai/cordis": "^4.0.1"
  }
}
```

依赖形态对照官方 `dsh-llm-deepseek`（deps: eventsource-parser + schemastery；peers 裁剪至最小集，[research.md](../research.md) D1-6）。**插件不得读取文件系统 secret**（cookbook 约定；token 由宿主 bootstrap 注入 env，D9）。

## 2. cordis 插件导出契约

```ts
// src/index.ts
import type { Context } from "@deepseek-ai/cordis";
import { z } from "@deepseek-ai/schemastery";
import { GlmResponsesAdapter } from "./adapter.js";

export const name = "llm-glm";
export const inject = ["llm"];

export interface GlmConfig {
  /** GLM API token 的环境变量名；值非空（assertUsableApiKey 校验）。 */
  apiKeyEnv: string;
  /** OpenAI Responses 端点，含版本路径（如 https://open.bigmodel.cn/api/v1）。 */
  baseURL: string;
  /** 显式模型目录（resolveModel 依据；catalog advisory 不做请求校验）。 */
  models: ReadonlyArray<{ id: string; contextWindow: number }>;
}

export const Config: z<z.infer<typeof configSchema>> = /* schemastery object */;
export function apply(ctx: Context, config: GlmConfig): void {
  ctx.llm.registerAdapter(["glm-responses"], new GlmResponsesAdapter(config));
}
```

- **provider route 名**: `glm-responses`（agent_v2 侧 `agentOptions: {provider: 'glm-responses', model: <GLM_MODEL>}`）。
- **注册语义**: effect-based、重复路由抛 `DUPLICATE_ADAPTER`、随 fiber dispose（cookbook；`LlmRuntime.registerAdapter` API）。
- **agent_v2 组合行**: 见 [data-model.md](../data-model.md) §2.6。

## 3. 适配器实现契约（`src/adapter.ts`）

```ts
export class GlmResponsesAdapter extends LlmAdapter {
  constructor(config: GlmConfig);
  override providerInfo(provider: string): LlmProviderInfo;   // id=provider, name="GLM (OpenAI Responses)"
  override async resolveModel(provider, model, signal?): Promise<LlmResolvedModelInfo>;
  // contextWindow 取自 config.models 命中项；未命中返回最小元数据（advisory）
  override async *stream(options: GenerateOptions): AsyncIterable<StreamChunk>;
}
```

**协议义务**（[docs/cookbook/adding-an-llm-adapter.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md)，逐条落实）：

1. `usage` 先于 `finish`、`finish` 后零输出——实现方式：缓冲至 `response.completed`/`response.incomplete`/`response.failed` 终局事件再 flush。
2. block `index` 按流中首现顺序分配、同块增量复用同 index。
3. 错误两条路径：传输/协议失败 → 从 `stream()` **throw** `LlmError`（稳定 code，如 `GLM_HTTP_500`/`GLM_PROTOCOL`）；provider 带内失败（`response.failed`/`error` 事件）→ 终局 `finish{kind:'error', failure}`。
4. 尊重 `options.signal`（传入 fetch；abort 时终局 `finish{kind:'aborted'}` 或 throw）。
5. 无法满足的 `GenerateOptions` 字段（如 `stop` 序列、`reasoningEffort` 未支持值）→ throw `LlmError(..., 'UNSUPPORTED')`，不静默丢弃。
6. 请求头必含 `Authorization: Bearer <env(apiKeyEnv)>` 与 attribution headers（`attributionHeaders()`，dsh-llm 导出）。
7. **不**发送 `replayState`（Responses 端点按 input 全量重建；无服务端 response id 复用需求——`store:false` 语义）。

## 4. 请求序列化（`src/serialize.ts`：`GenerateOptions` → Responses 请求体）

```jsonc
POST {baseURL}/responses
{
  "model": "<options.model>",
  "instructions": "<options.system>",          // system slot（spine persona 渲染产物）
  "input": [ /* 见下表映射 */ ],
  "stream": true,
  // temperature/maxTokens（如提供）: "temperature"/"max_output_tokens"
}
```

| harness 侧 | Responses input item | 说明 |
|---|---|---|
| `Message{role:'user', content:[text]}` | `{type:'message', role:'user', content:[{type:'input_text', text}]}` | |
| `Message{role:'assistant', content:[text]}` | `{type:'message', role:'assistant', content:[{type:'output_text', text}]}` | |
| assistant `reasoning` 块 | **不回传**（GLM 逐轮重生成推理；简化历史、上下文随轮次单调增长对齐 spec Edge 超长会话行为） | |
| `tool-result` 消息 / `ToolCallBlock` | 后续工具 step 扩展点（本阶段零工具，遇到即 throw `UNSUPPORTED_CONTENT`） | fail-loud |
| user `image` 块 | throw `UNSUPPORTED_CONTENT`（text-only，FR-006 范围） | |

input item 格式依据 OpenAI Responses 官方规范（[openai-openapi responses](https://github.com/openai/openai-openapi)：`input_text`/`output_text` content 类型与 message item 形状）。

## 5. SSE 事件 → StreamChunk 映射（`src/wire.ts`）

消费 `eventsource-parser` 解析的 SSE 流（`event:`/`data:` 帧），按事件 `type` 分派：

| Responses 事件 | StreamChunk | 说明 |
|---|---|---|
| `response.created` / `response.in_progress` | （忽略） | 生命周期壳 |
| `response.output_item.added`（item.type=`reasoning`） | `block-start{index, 'reasoning'}` | 新 index 分配 |
| `response.reasoning_summary_part.added`（part.type=`summary_text`） | `block-start{index, 'reasoning'}` | 若未见 item.added（GLM 变体容错） |
| `response.reasoning_summary_text.delta` / `response.reasoning_text.delta` | `reasoning-delta{index, text}` | 两词汇均接受（OpenAI 标准 + 直出 reasoning 变体） |
| `response.output_item.added`（item.type=`message`） | `block-start{index, 'text'}` | |
| `response.content_part.added`（part.type=`output_text`） | `block-start{index, 'text'}` | item.added 的补充形态（容错去重） |
| `response.output_text.delta` | `text-delta{index, text}` | |
| `response.output_item.added`（item.type=`function_call`） | `block-start{index, 'tool-call', id:call_id, name}` | 后续工具 step 启用路径 |
| `response.function_call_arguments.delta` | `tool-call-delta{index, id, argumentsDelta}` | 同上 |
| `response.output_item.done` / `response.content_part.done` | `block-end{index, block}` | 终态块（text 拼接/reasoning 拼接/完整 args） |
| `response.completed` | `usage{...}` → `finish{kind:'stop'\|'tool-calls'}` | 有 function_call → `tool-calls`；usage 映射：input_tokens→inputTokens、output_tokens→outputTokens、output_tokens_details.reasoning_tokens→reasoningTokens |
| `response.incomplete` | `usage` → `finish{kind:'max-tokens'}` | |
| `response.failed` / `error` | `finish{kind:'error', failure{message, code}}` | 带内失败路径 |
| 其他未知 `type` | 忽略 | forward-compat |

**映射依据**: OpenAI Responses streaming events 官方定义（[openai-openapi](https://github.com/openai/openai-openapi) streaming events 节：事件序 `output_item.added → content_part.added → output_text.delta → …done → response.completed` 与 usage 载荷）；GLM 端点为该协议的 Codex 兼容实现（[GLM codingplan 文档](https://docs.bigmodel.cn/cn/coding-plan/tool/others) §编程端点）。真实端点的词汇偏差（如 reasoning 事件名）在手工冒烟（SC-003）中确认；适配器对未知事件忽略、对两种 reasoning delta 词汇均接受，偏差吸收面已预留。

## 6. 测试义务（vitest，随包交付）

1. **序列化单测**：user/assistant 历史往返（多轮）、system→instructions、reasoning 不回传、image/tool 内容 UNSUPPORTED_CONTENT。
2. **wire 单测**：构造 SSE 帧序列（含 reasoning→message 交错、多块多 delta、completed 带 usage）断言 StreamChunk 序（index 分配、usage-先-finish、终态一致）；`response.failed` → error finish；HTTP 非 200 → throw LlmError。
3. **协议义务回归**：finish 后零输出、delta index 复用。
4. fixture SSE 与 [fake-responses-wire.md](fake-responses-wire.md) 共享词汇（同一事件构造器输出）。

## 7. 交付边界

- 本插件**不**实现：credentials/settings 集成、模型发现（`LlmModelDiscoveryRequest`）、图片/文件上传、Preserved Thinking（reasoning 回传）——均为后续 step 扩展点。
- 失败信息不得包含 token 内容（SC-004；对齐官方 `assertUsableApiKey` 的 "key never enters the message" 诊断原则）。
