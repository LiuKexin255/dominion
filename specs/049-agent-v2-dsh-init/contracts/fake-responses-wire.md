# Contract: fake-llm Responses 端点 wire（`POST /v1/responses`）

**Feature**: [spec.md](spec.md) FR-007/FR-011 | **决策**: [research.md](../research.md) D7

**载体**: `projects/game/fake-llm`（既有 Go 服务）**增量**新增端点；既有 `/v1/chat/completions` 端点与全部存量用例零改动。

## 1. 请求接受（fake 侧容忍义务）

```jsonc
POST /v1/responses
Authorization: Bearer <任意值，忽略>       // header 容忍（对齐 demo fake-llm §2）
Content-Type: application/json
{
  "model": "<忽略>",                       // 模型目录由 agent_v2 侧 cordis.yml 对齐
  "instructions": "<可选，忽略>",
  "input": [ ...message items... ],        // 仅消费 message items 的文本
  "stream": true                           // 必须支持；stream:false 返回等价非流式 JSON
}
```

- **input 解析**：提取 `role`（user/assistant）与 content 内 `input_text`/`output_text` 文本，重建消息序列（供模板匹配与多轮条件）；未知 item 类型（reasoning 等）忽略。
- **非法请求**（缺 model/input、JSON 语法错误）→ 400；不崩溃。

## 2. SSE 事件发射（OpenAI Responses 词汇）

`stream:true` 时以 `text/event-stream` 发射（事件名 + JSON data，逐帧 flush；与 [glm-llm-plugin.md](glm-llm-plugin.md) §5 的消费词汇一致）：

```text
event: response.created
data: {"type":"response.created","response":{"id":"resp_fake_1","status":"in_progress"}}

event: response.output_item.added                    ← 模板含 think 时
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_fake_1"}}

event: response.reasoning_summary_text.delta
data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_fake_1","output_index":0,"delta":"<思考增量>"}
                                                     ← 可多帧（模板分片 + 可控延迟）

event: response.output_item.added
data: {"type":"response.output_item.added","output_index":1,"item":{"type":"message","role":"assistant"}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_fake_1","output_index":1,"delta":"<正文增量>"}
                                                     ← 可多帧

event: response.output_item.done
data: {"type":"response.output_item.done","output_index":1,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"<完整正文>"}]}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_fake_1","status":"completed","usage":{"input_tokens":N,"output_tokens":M,"output_tokens_details":{"reasoning_tokens":K}}}}
```

**不变式**：

1. 无 think 模板**不发射**任何 reasoning 事件（US2 场景 2：无思考内容零 THINK 块）。
2. 正文与思考分属不同 output item/output_index，不混排。
3. 终局 `response.completed` 必发且最后；usage 数值为确定性常量（由模板长度推导，非随机）。
4. 错误注入模板（可选）：发射 `response.failed`（code/message 确定）用于 Edge-模型故障 用例。

## 3. 模板设施（复用既有 + Responses 投影）

| 设施 | 来源 | Responses 投影 |
|---|---|---|
| 关键词模板匹配 | `projects/game/fake-llm/service/matcher.go` 既有 | 命中模板的 `think`/`text` 字段 → reasoning/text 事件序列 |
| 多轮条件（`history_keywords`/`min_turn`） | demo fake-llm 模式（047 D7），game fake-llm 对齐实现 | 条件满足选模板 → 多轮连续性用例（US1-2） |
| 可控延迟 | game fake-llm 既有（043/044 stall 依赖） | delta 帧间延迟 → "回合进行中"窗口（FR-012 队列用例） |
| 确定性兜底 | 既有 | 无命中时输出确定内容 |

**testdata 新增**（`projects/game/fake-llm/service/testdata/`）：

- think+text 模板（含 history_keywords 变体）——US1/US2 主路径。
- 纯 text 模板——US2 场景 2。
- 长延迟模板——FR-012 排队窗口。
- 失败模板（可选）——Edge-模型故障。

## 4. 部署与替换机制

- 测试部署（`projects/game/testplan/deploy_agent_v2.yaml`）包含 fake-llm 服务；agent_v2 环境变量 `GLM_LLM_TARGET=dominion:///game/fake-llm:8080` → bootstrap 解析 → `GLM_BASE_URL=http://{endpoint}/v1`（[research.md](../research.md) D9）+ `GLM_API_KEY=dummy-key`。
- 生产部署不包含 fake-llm；`GLM_BASE_URL` 默认 `https://open.bigmodel.cn/api/v1`（真实端点手工冒烟，SC-003）。
- **零外部网络**：大型测试期间 agent_v2 仅访问 fake（SC-001）。

## 5. 验收锚点

| 场景 | 断言 |
|---|---|
| 流式词汇 | 事件序符合 §2 不变式；插件（[glm-llm-plugin.md](glm-llm-plugin.md) §6 fixture 共享）与 fake 双向对齐 |
| 确定性 | 同输入同输出（含 usage 常量） |
| think 区分 | think 模板 → agent_v2 流中出现 THINK 块；纯 text 模板 → 零 THINK 块 |
| 多轮 | 第二轮消息命中 history_keywords 模板（回复内容依赖首轮） |
| 延迟窗口 | 长延迟模板下 Send(2nd) 在回合结束前到达 → queued 路径可测 |
