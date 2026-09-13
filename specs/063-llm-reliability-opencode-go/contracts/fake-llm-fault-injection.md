# Contract: fake-llm 故障注入扩展（transient / stall / chat 状态注入）

**Feature**: [spec.md](../spec.md) SC-001..005 验收支撑 | **决策**: [research.md](../research.md) D14, D15

**位置**: `projects/game/fake-llm/`（`service/message_types.go` schema、`service/responses.go`、`service/handler.go`、`service/testdata/*.yaml` fixtures）

**现状基线**: Responses wire 带内 `failure`（恒定）；chat wire `stall`/`stall_after`；无 HTTP 状态/传输级/单次瞬态注入；Responses wire 无 stall（`service/responses.go:576-581` 故意排除）。

## 1. transient 块（per-template 有状态注入）

```yaml
messages:
  - name: agent-v2-transient-503
    keywords: ["trigger transient failure"]
    transient:
      times: 1              # 前 N 次匹配注入；缺省/0 视为 ∞
      http_status: 503      # 与 empty、failure 三选一（可扩展）
      retry_after: 1        # 可选：注入响应的 Retry-After 头（秒；http_status 时）
      error_message: "insufficient quota"  # 可选：注入体 error.message（http_status 时；QUOTA 分类用例）
    # times 耗尽后按本模板正常内容应答（reasoning/text/tool_call 等）
```

- **计数器语义**：每模板独立、进程内、`sync.Mutex` 保护；只对**成功匹配该模板**的请求计数；重启清零（测试拓扑每 plan 重新部署，天然隔离）。
- **注入行为**（两 wire 一致）：
  - `http_status: N` → 直接返回 HTTP N，体为 `{"error":{"message":<error_message 或 "injected http failure">,"type":"injected"}}`；`retry_after` 存在时响应头携带 `Retry-After`。429 + quota 措辞体的用例：`error_message` 指定注入体 `error.message` 文案（如 `"insufficient quota"`），驱动适配器 `QUOTA` 分类。
  - `empty: true` → HTTP 200 + 正常 SSE 生命周期但**零内容块**（Responses：`created → completed`；chat：role chunk + `[DONE]`）。
  - `failure: {code, message}`（复用既有 `ResponseFailure` 形态）→ 既有带内失败语义，受 `times` 约束。
- **向后兼容**：无 `transient` 块的模板行为不变；既有 `failure` 字段语义视为 `transient: {times: ∞, failure}` 的等价快捷形态（实现上可合并，外部行为不变）。

## 2. Responses wire 停滞投影

- `stall` / `stall_after: K` 在 `/v1/responses` 生效：SSE 建立后发送 K 个事件（或 0）后阻塞于 `<-r.Context().Done()`——连接存活、无数据（chat wire `service/handler.go:576-579` 同型）。
- 移除 `responses.go:576-581` 的故意排除及对应注释（宪章 VII 终态化）。
- stall 模板维持 `isHangCapable` 语义：不进随机 fallback 池（`matcher.go:176-195` 既有）。

## 3. chat wire 状态注入

- `transient.http_status` 对 `/v1/chat/completions` 同样生效（opencode-go 插件失败分类测试的注入面）。

## 4. 测试义务（Go 单测，随服务交付）

1. **transient 计数**：`times: 1` → 首请求 503、次请求正常体；并发匹配下恰 N 次注入（mutex）。
2. **retry_after 头 / error_message 体**：注入响应携带 `Retry-After` 头（取值固定为秒数（int））；`error_message` 时注入体 `error.message` 等于配置文案（缺省 `injected http failure`，QUOTA 分类用例）。
3. **empty**：两 wire 零内容块终局（供适配器 EMPTY_RESPONSE 分类断言）。
4. **responses stall**：`stall_after: 1` → 1 事件后无数据（httptest 客户端读超时断言）。
5. **fixtures**：新增 `agent_v2_transient.yaml`（SC-001/SC-002/SC-005 配额与认证触发模板）与 stall 模板（`testdata/`，embed 列表更新）；新增 `opencode_go.yaml`（chat wire，SC-003 的确定性驱动：关键词 `请开始扫雷` → planner 开场文本应答；team fixtures 为 responses_only、chat 端点不可用，工具链复用既有 `saolei.yaml`/`saolei_tools.yaml`）。
6. **回归**：既有 11 个 fixture 加载与匹配用例（`message_store_test.go`）不变。

## 5. 大型测试接线（验收用例，quickstart 场景细化）

| 场景 | fixture/注入 | deploy env | 断言 |
|---|---|---|---|
| SC-001 单次瞬时恢复 | `transient: {times: 1, http_status: 503}`（planner 触发词） | — | turn COMPLETED；无 ERROR 帧；session log `llm/retry` 恰 1 条 |
| SC-002 planner 失败保持 | `transient: {times: 6, http_status: 500}`（1 初始 + 默认 5 重试） | — | turn_end ERROR；GetTeam activation=planner；再 Send 由 planner 应答 |
| SC-004a 停滞看护 | responses `stall: true` 模板 | 独立 `game-stall` suite / `deploy_agent_v2_stall.yaml`（`GLM_STREAM_IDLE_TIMEOUT_MS: "2000"`） | 超时类失败可重试呈现；测试整体超时兜底不挂起 |
| SC-005 配额不重试 | `http_status: 429` + `error_message: "insufficient quota"` | — | 零 `llm/retry` 事件；恰一次 ERROR |
| SC-005 认证不重试 | `http_status: 401` | — | 零 `llm/retry` 事件；恰一次 ERROR |
| SC-003 opencode-go 全流程 | chat wire：`opencode_go.yaml`（planner 开场）+ 既有 `saolei.yaml`/`saolei_tools.yaml`（工具链） | `OPENCODE_LLM_TARGET: dominion:///game/fake-llm:8080` + 合成 `OPENCODE_API_KEY`（非真实 secret） | ListModels 联合目录含两 provider 复合标识；`opencode-go/<model>` 成员完成含工具调用多轮会话；合成 token 值零出现 |

SC-002 的"解除注入后恢复"：`times: 6`（1 次初始 + 默认 5 次重试，恰耗尽单 turn 预算）→ 首个 Send 以失败 turn 收束；下一次 Send 的首次尝试（第 7 次匹配）起模板已耗尽 → 正常应答（无需动态开关）。
