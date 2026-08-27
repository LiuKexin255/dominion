# Contract: fake-llm Wire（chat-completions SSE）

**Feature**: [spec.md](spec.md) FR-006 | **Date**: 2026-08-22

fake-llm（Go，`experimental/dsh/demo/fake-llm`）对 dsh `dsh-llm-deepseek` 适配器暴露的 wire 契约。上游义务来源：[research.md](../research.md) D1（适配器恒 `stream:true`、SSE 解析、header 集）。

## 1. 端点

| 端点 | 方法 | 说明 |
|---|---|---|
| `/v1/chat/completions` | POST | OpenAI Chat Completions 兼容（`stream` true/false 均支持） |
| `/health` | GET | 返回 200 "ok" |

baseURL 消费方式：dsh 侧 `FAKE_LLM_BASE_URL = http://<endpoint>/v1`，适配器拼接 `/chat/completions`（D2）。

## 2. 请求

OpenAI Chat Completions 形状；fake-llm 消费的字段：

| 字段 | 消费方式 |
|---|---|
| `messages[]` | 匹配输入：最后一条 `role=user` 消息做关键词匹配；其余消息做历史关键词匹配与轮次计数（[fake-llm-templates.md](fake-llm-templates.md)） |
| `stream` | true → §3 SSE；false → §4 单 JSON |
| `model` | **忽略**（fake 目录由 dsh 侧 `models[]` 配置对齐，均为 `fake-chat-v1`） |
| 其余字段（tools/temperature/...） | 忽略 |

**必须容忍（忽略、不校验）的 header**：`authorization: Bearer <dummy>`、`accept: text/event-stream`、`x-deepseek-harness-user-id`、可选 `x-deepseek-harness-session-id` / `x-deepseek-harness-compact`、attribution User-Agent（`x-deepseek-ai-app/*`）。

## 3. SSE 流式响应（dsh 路径，`stream: true`）

`Content-Type: text/event-stream`，帧序列（模板 reply 为单个 text）：

```
data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":<ts>,"model":"fake-chat-v1","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":<ts>,"model":"fake-chat-v1","choices":[{"index":0,"delta":{"content":"<模板 text 第一段>"},"finish_reason":null}]}

data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":<ts>,"model":"fake-chat-v1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}

data: [DONE]
```

**不变量**（适配器解析义务的镜像）:

1. 首帧 delta 携带 `role: "assistant"`（空 content 或首段均可）；
2. content deltas 按序拼接 = 模板 `text` 全文（无丢失/乱序）；
3. finish 帧携带 `finish_reason: "stop"` 与 `usage`（finish chunk 内或独立 usage-only chunk 均被适配器接受）；
4. **终帧必须 `data: [DONE]`**——缺失触发适配器 `STREAM_CLOSED` 错误；
5. 帧间无注入 delay（demo 模板不模拟思考中断；该能力属 `projects/game/fake-llm`，本 fake 不实现）。

## 4. 非流式响应（`stream: false`，测试便利）

标准单 JSON（`choices[0].message.content` = 模板 text，`usage` 同上）。dsh 路径不经过此模式。

## 5. 错误语义

fake-llm 自身无业务错误路径（未命中 → 兜底模板，仍 200）。 malformed JSON body → 400（OpenAI 风格 error 对象）。适配器将 401/403→AUTH、429→RATE_LIMIT 等映射——fake-llm 不产生这些状态（除 400 外）。

## 6. 部署形态

- `service.yaml`：`app: dsh-demo`、`name: fake-llm`、`kind: stateless`、port `http: 8080`、无 tls（`projects/game/fake-llm/service.yaml` 同款）。
- 寻址：agent 侧 resolver target `dominion:///dsh-demo/fake-llm:8080`（FR-011）。
- 大型测试豁免：测试基建定位，随 testplan 部署，README 记录 Constitution VI 豁免（`projects/game/fake-llm/README.md` 先例）。
