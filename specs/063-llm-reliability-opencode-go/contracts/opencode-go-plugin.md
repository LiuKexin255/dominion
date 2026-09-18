# Contract: `@dominion/dsh-llm-opencode-go` 插件（opencode-go 网关 Chat Completions 适配器）

**Feature**: [spec.md](../spec.md) FR-013..017 | **决策**: [research.md](../research.md) D12

**位置**: `common/js/dsh-plugins/llm-opencode-go/`（workspace 包，`common/js/**` glob 已覆盖）| **dsh 版本线**: 0.1.1-rc.2 精确 pin

**结构先例**: llm-glm 插件契约 `specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md`；Chat Completions 线协议先例 = 官方 deepseek 适配器（本地物化 `node_modules/.pnpm/@deepseek-ai+dsh-llm-deepseek@0.1.1-rc.2_*/lib/index.js` serialize/translate 段）

**端点**: `POST {baseURL}/chat/completions`，默认 baseURL `https://opencode.ai/zen/go/v1`（https://opencode.ai/docs/zh-cn/go/ ）

## 1. 包契约

```jsonc
// package.json（@dominion/dsh-llm-opencode-go）
{
  "name": "@dominion/dsh-llm-opencode-go",
  "private": true,
  "type": "module",                    // ESM（specs/048 契约）
  "main": "./src/index.js",
  "types": "./src/index.d.ts",
  "exports": { ".": { "types": "./src/index.d.ts", "default": "./src/index.js" } },
  "dependencies": {
    "eventsource-parser": "catalog:",
    "@deepseek-ai/schemastery": "catalog:",
    "@deepseek-ai/dsh-timeout": "catalog:"   // idleWatchdog（与 llm-glm 新增依赖同源）
  },
  "peerDependencies": {
    "@deepseek-ai/dsh-llm": "catalog:",       // 0.1.1-rc.2 线（pnpm catalog 精确 pin）
    "@deepseek-ai/cordis": "catalog:"
  },
  "devDependencies": {
    "@types/node": "catalog:",
    "typescript": "catalog:",
    "vitest": "catalog:"
  }
}
```

**构建与装配**：`BUILD.bazel` 手工镜像 llm-glm（`npm_link_all_packages`、`ts_config`、`ts_project(:lib)`、`js_library(:pkg)`、`js_runtime_library(:runtime_pkg)`、`vitest_test(:lib_test)`）——根 `gazelle_binary` 仅注册 proto/go/python 语言，不生成 JS target。作为 cordis 行的运行时装载经 `projects/game/agent_v2/package.json` workspace 依赖 + `BUILD.bazel` `artifact_pkg_js` `runtime_deps` 的 `:runtime_pkg` 入闭包（npm_deps 对 workspace 包为 no-op）。

依赖对齐 llm-glm 的 pin 决策（`common/js/dsh-plugins/llm-glm/README.md` 依赖 pin 节）；不读取文件系统 secret（token 由宿主 bootstrap 注入 env）。

## 2. cordis 插件导出契约

```ts
export const name = "llm-opencode-go";
export const inject = ["llm"];

export interface OpencodeGoConfig {
  apiKeyEnv: string;              // 默认 OPENCODE_API_KEY；空值 → 不携带 Authorization
  baseURL: string;                // 默认 https://opencode.ai/zen/go/v1
  models: ReadonlyArray<{ id: string; contextWindow: number }>;
  retryPolicy?: RetryPolicy;      // 见 contracts/llm-failure-taxonomy.md §2
  streamIdleTimeoutMs?: number;   // 默认 300000
}

export function apply(ctx: Context, config: OpencodeGoConfig): void {
  ctx.llm.registerAdapter(["opencode-go"], new OpencodeGoChatAdapter(config));
}
```

**provider route 名**: `opencode-go`（选择面复合标识前缀，见 [model-selection.md](model-selection.md)）。注册语义：effect-based、重复路由 `DUPLICATE_ADAPTER`、随 fiber dispose（dsh `LlmRuntime.registerAdapter` 既有）。

**默认模型目录**（静态快照，仅 chat 路由模型——FR-015 裁定；Responses/Anthropic 路由模型排除）：

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

目录覆盖官方文档 API 端点表中 **Chat Completions 路由的全部 16 个模型**（https://opencode.ai/docs/zh-cn/go/ ，2026-09-12）——因 UpdateTeam 按部署目录强校验，此表即用户可选模型范围。id 与 contextWindow 取 OpenCode 模型数据库 `models.dev` 的 `opencode-go` provider 快照（https://models.dev/api.json ，2026-09-12 抓取）。仅存在于网关 `/models`、官方文档未收录的 id（`glm-5`、`kimi-k2.5`、`mimo-v2-pro`/`mimo-v2-omni`、`qwen3.5-plus`、`grok-4.5`、`omen-alpha`、`ox-alpha-free` 等）无 wire 路由声明，不收录；Responses 路由（grok/gpt/muse）与 Anthropic Messages 路由（qwen/minimax）模型按 FR-015 排除。`contextWindow` 为部署可调 advisory 快照（动态模型发现不在 v1 范围）。目录为 advisory：`resolveModel` 未命中返回最小元数据（与 llm-glm 同语义）。模型模态不改变 v1 文本范围：`glm-5.3-flash`/`deepseek-v4.1-flash` 等声明 image 输入的模型同样按 §3 对 image 块抛 `UNSUPPORTED_CONTENT`。

## 3. 请求序列化（`src/serialize.ts`：GenerateOptions → Chat Completions 请求体）

```jsonc
POST {baseURL}/chat/completions
Authorization: Bearer <env(apiKeyEnv)>        // 条件：key 非空才携带
x-opencode-session: <options.sessionId>       // 稳定会话标识（网关路由/提示缓存建议）
User-Agent: <产品自定义 UA>                     // 客户端可识别（网关官方建议）
attributionHeaders()                          // 必含
{
  "model": "<options.model>",
  "messages": [ /* 见下表 */ ],
  "stream": true,
  "stream_options": { "include_usage": true },
  "tools": [ /* 非空时：{type:'function', function:{name, description, parameters}} */ ],
  // temperature/maxTokens/stop（如提供）: "temperature"/"max_tokens"/"stop"
}
```

| harness 侧 | chat 消息 | 说明 |
|---|---|---|
| `GenerateOptions.system` | 首条 `{role:'system', content}` | |
| `Message{role:'user', content:[text]}` | `{role:'user', content: 拼接文本}` | text-only 内容保持字符串形态 |
| `Message{role:'assistant', content:[text]}` | `{role:'assistant', content}` | |
| assistant `reasoning` 块 | **不回传**（对齐 llm-glm 049 决策：逐轮重生成推理） | |
| assistant `tool-call` 块 | assistant 消息 `tool_calls: [{id, type:'function', function:{name, arguments}}]` | |
| `tool-result` 块 | 独立 `{role:'tool', tool_call_id, content: 拼接文本 ‖ "(no output)"}` | 与官方 deepseek 同型 |
| user `image` 块 | throw `UNSUPPORTED_CONTENT`（text-only v1） | |

## 4. SSE 事件 → StreamChunk 映射（`src/wire.ts`）

| chat 载荷 | StreamChunk | 说明 |
|---|---|---|
| `delta.reasoning_content`（非空串） | `block-start{reasoning}`（首现）/ `reasoning-delta` | GLM/Kimi interleaved 推理 |
| `delta.content`（非空串） | `block-start{text}`（首现）/ `text-delta` | |
| `delta.tool_calls[].{index,id,function}` | `block-start{tool-call, id, name}`（新 index）/ `tool-call-delta{argumentsDelta}` | 按 wire index 复用块 |
| `choices[0].finish_reason` | 缓存至 `[DONE]` | `stop`→stop、`tool_calls`→tool-calls、`length`→max-tokens、其他→error finish（码 = 值大写） |
| `usage`（任意 chunk） | 缓存至 `[DONE]` | `prompt_tokens`→inputTokens、`completion_tokens`→outputTokens、`completion_tokens_details.reasoning_tokens`→reasoningTokens |
| `[DONE]` 哨兵 | 全部 `block-end` → `usage` → `finish` | 零已开块 + stop → `finish{error, EMPTY_RESPONSE}`（空补全分类） |
| 流结束无 `[DONE]` | throw `STREAM_CLOSED` | |
| 载荷非法 JSON | throw `MALFORMED_RESPONSE` | |
| SSE comment 帧 | `onComment` 回调 → 看护 pulse | 不进块流 |

## 5. 适配器协议义务

完整失败分类、重试声明、看护、空补全、传输清理、abort、条件 Authorization 义务**逐条适用** [contracts/llm-failure-taxonomy.md](llm-failure-taxonomy.md)（两插件共用同一义务集，FR-016）。差异项：

- `providerInfo`：`{id: provider, name: "OpenCode Go (Chat Completions)"}`。
- `x-opencode-session` 头取 `options.sessionId`（未提供则省略）；会话内保持稳定（agent 成员会话 id 天然稳定）。
- `User-Agent` 为产品自定义值（含产品名与版本，不含任何凭据）。

## 6. 测试义务（vitest，随包交付）

1. **序列化单测**：system/多轮历史往返、tool-call/tool-result 配对、reasoning 不回传、tools 平铺与非空才携带、image UNSUPPORTED_CONTENT。
2. **wire 单测**：SSE 帧序列（reasoning/text 交错、多 tool_call、usage 尾 chunk、`[DONE]`）断言 StreamChunk 序（index 分配、usage-先-finish、finish 后零输出）；空补全 → EMPTY_RESPONSE；无 `[DONE]` → STREAM_CLOSED。
3. **失败分类**：taxonomy 契约 §4 的共用用例集（http 状态映射、Retry-After、看护、清理）。
4. **条件 Authorization + 会话头**：空 key → 无 Authorization 且有 `x-opencode-session`；非空 → Bearer 存在；UA 恒存在。
5. fixture 与 fake-llm chat 端点词汇共享（`projects/game/fake-llm/service/handler.go` 产出形态）。
6. **advisory resolveModel**：目录外 id → 最小元数据、不拒绝（与 llm-glm 同语义，FR-014）。
7. **默认目录完整性**：Config 默认目录恰为 §2 的 16 项官方 Chat Completions id（唯一且不含 Responses/Anthropic 路由模型）。

## 7. 交付边界

- 不实现：Anthropic Messages / OpenAI Responses wire（FR-015 v1 排除）、模型发现（`LlmModelDiscoveryRequest`）、图片/文件上传、Preserved Thinking（reasoning 回传）、credentials/settings 体系（specs/049 D9 分歧保持）。
- 失败信息不含 token 内容（SC-003）。
