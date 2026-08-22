# Data Model: dsh Chat Demo

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-08-22

本文定义 demo 的实体、数据形态与映射关系。接口行为契约见 [contracts/](contracts/)；此处只关心"数据长什么样、从哪来、到哪去"。

## 1. Conversation（会话）

| 属性 | 形态 | 说明 |
|---|---|---|
| id | string（调用方提供，非空） | 对外会话标识；**直映射**为 dsh `SessionId`（宿主自选，[research.md](research.md) D5）与 fake-llm 匹配上下文的会话边界 |
| turns | 有序 ChatTurn 列表 | 会话内按时间排序的对话轮次 |
| lifecycle | 内存态 | dsh agent 进程内存活（registry + session store），随进程销毁；无持久化（spec Assumptions） |

**唯一性/冲突**: 同一 id 的 live agent 重复 `create` 抛错——服务侧 get-or-create（D5）；同一 id 并发首个请求经创建防抖去重（单一创建 promise）。

**隔离**: 不同 id 的 agent 为独立 fiber，上下文互不可见（US2-场景2/3 的判定依据）。

## 2. ChatTurn（对话轮次）

| 属性 | 形态 | 说明 |
|---|---|---|
| user_message | string（非空） | 调用方消息文本 |
| reply | string | agent 回复 = 该轮**末条** `assistant/message` 事件的 text blocks 拼接（D3）；无 assistant 消息时为空串 |
| state machine | `pending → completed \| failed` | 进入（followup 完成 inbox 入列）→ 终态由 idle 终止信号或错误决定 |

一轮的内部事件序列（dsh 侧，服务不外露）：`followup → agent/status running → turn/start → assistant/chunk* → assistant/message → turn/end → agent/status idle`（[research.md](research.md) D10-4）。

## 3. SendMessageRequest / SendMessageResponse（wire 消息，gRPC ↔ HTTP）

| 消息 | 字段 | 校验 |
|---|---|---|
| SendMessageRequest | `name: string`（资源名 `conversations/{id}`，id 非空——URI 变量 `{name=conversations/*}` 拼装，[contracts/chat-api.md](contracts/chat-api.md)）、`message: string`（必填非空） | 空值/坏资源名 → gRPC INVALID_ARGUMENT / HTTP 400（Edge Cases：畸形请求） |
| SendMessageResponse | `name: string`（回显资源名）、`reply: string` | reply 确定性由模板匹配保证 |

资源名 `name` 中的 id 与 Conversation.id 同一（服务端提取 `conversations/` 后缀即得）。proto 定义位于 `experimental/dsh/demo/chat.proto`（AIP-136 自定义方法模式，契约细节：[contracts/chat-api.md](contracts/chat-api.md)）。

## 4. Model Response Template（模型响应模板）

fake-llm 的脚本化回复定义（testdata 文件内嵌，`go:embed`），字段契约：[contracts/fake-llm-templates.md](contracts/fake-llm-templates.md)。数据形态概览：

| 字段 | 形态 | 默认 |
|---|---|---|
| name | string（唯一标识） | 必填 |
| keywords | string[]（任一命中即匹配最后一条 user 消息，大小写不敏感子串） | 必填（可为空数组=仅多轮条件） |
| history_keywords | string[]（可选；**全部**须出现在除最后一条 user 消息外的历史中） | 无（退化纯关键词匹配） |
| min_turn | int（可选；请求中 user 消息数 ≥ min_turn 才可命中） | 1 |
| text | string（确定性回复内容） | 必填 |
| reasoning | string（可选思考内容；chat-completions 路径下不发送，保留 schema 兼容） | 无 |

**匹配优先级**: 多轮条件模板（条件全满足）> 纯关键词模板 > 兜底模板（确定性随机挑选非挂起模板，seed 稳定）。

## 5. dsh Composition Manifest（组合清单 cordis.yml）

demo 的启用面唯一事实源（数据文件，`artifact_pkg_js.data_files` 携带），两行结构（完整契约：[contracts/dsh-agent-service.md](contracts/dsh-agent-service.md)）：

```yaml
- id: agent-spine
  name: '@deepseek-ai/dsh-agent-spine-demo'
  config: { persona, workspaceContext: false, includeRuntimeContext: false,
             includeHarnessIdentity: false, skills: {enabled: false},
             toolBash: false, toolJobs: false }
- id: llm-deepseek
  name: '@deepseek-ai/dsh-llm-deepseek'
  config: { apiKeyEnv: FAKE_LLM_API_KEY, baseURL: !!js process.env.FAKE_LLM_BASE_URL,
             models: [{id: fake-chat-v1, contextWindow: 100000}] }
```

**不变量**: 启用行 ⊆ 物化 node_modules 集合（闭包校验的职责面，US3/SC-004）。

## 6. dsh Core Baseline（框架核心底座）

`third_party/dsh/core` 的闭包清单（≈11 包，[research.md](research.md) D6）：**数据上就是一个精确 pin 的 package.json + 枚举 link targets 的 `js_runtime_library`**，无代码语义。版本不变量：全部 `@deepseek-ai/*` 同锁 `0.1.1-rc.2` 线；服务闭包内同名包版本唯一（SC-004 审计项）。

## 7. 实体关系总览

```
调用方 ──HTTP──▶ gateway ──gRPC──▶ agent 服务
                                        │ get-or-create(session_id)
                                        ▼
                                   dsh Agent(=Session)  ──followup/事件──▶ ChatTurn
                                        │ LLM 调用（deepseek-official 路由）
                                        ▼
                                   fake-llm ──匹配──▶ Template ──▶ 确定性 SSE 回复
```

- Conversation 1—N ChatTurn；ChatTurn.reply 由 (模板匹配 × 会话历史) 决定。
- Conversation.id 三处一致：gRPC session_id = dsh SessionId = fake-llm 侧 messages 历史的会话边界。
- Template 与 Conversation 无持久关联——多轮感知完全由请求内 messages 历史承载（无服务端会话状态）。

## 8. 数据量假设

PoC 规模：并发会话数个（US2 交错 2 个）、单会话轮次 < 10、模板 < 20 条、dsh 闭包 ≈40±10 包。无容量设计要求。
