# 调研：LLM Stream Stall Recovery 修订（spec 043 后续观察与改进）

> **状态**：调研完成，待立项进入方案设计（建议作为 spec 043 的修订或新 spec 044）
> **日期**：2026-08-12
> **目标服务**：`projects/game/agent/`
> **范围**：spec 043「LLM Stream Stall Recovery」上线后的生产观察、跨框架调研、output1 丢失 bug 分析，以及对应的修订建议
> **说明**：本文为调研材料，供后续制定方案（spec / plan）时使用；非 SDD spec 文档。所有外部引用附完整 URL，仓库内引用附相对路径。

---

## 1. 背景与目标

### 1.1 起因

spec 043「LLM Stream Stall Recovery」（`specs/043-llm-stream-stall-recovery/spec.md`）于 2026-08-11 全部交付，引入 LangGraph `TimeoutPolicy.idleTimeout`（默认 **30000ms**）作为 chunk-idle 检测机制，并显式排除 retry（FR-013）。上线后观察到两个严重问题：

1. **生产环境 saolei 模板的 stall 极其频繁**——session `a7cb3d62f0269fa88410093380f79def`（prod `game.prod`）在 2026-08-12 13:17–13:24 短短 7 分钟内连续两次 stall（player + planner），用户反馈"几乎无法完成一局扫雷就会中断"。同一 provider（opencode-go gateway）和 model（`deepseek-v4-flash`）下，opencode 客户端使用同样模型**没有这么严重的中断**。
2. **stall 后已流式发送给前端的 agent output 在 session 重新进入时消失**——典型场景：`开始游戏 → agent output1 → 中断 → user "继续游戏" → agent output2 → abort`，重新进入后只能看到 `开始游戏 / 继续游戏 / agent output2`，**output1 永久丢失**。

### 1.2 调研问题

| # | 问题 | 结论章节 |
|---|---|---|
| Q1 | spec 043 的 `idleTimeout=30s` 默认值是否符合行业实践？ | §4 + §5.1 |
| Q2 | spec 043 references 中的"15-30s community consensus"是否准确？ | §5.2 |
| Q3 | 其他 agent 框架（LangChain / Codex / Hermes / OpenClaw / opencode client）如何处理同类问题？ | §4 |
| Q4 | reasoning 模型（如 `deepseek-v4-flash`）需要什么特殊处理？ | §4.3 + §5.3 |
| Q5 | stall 后已流式发送的 agent output 为什么会丢失？如何修复？ | §5.5 + §6.4 |
| Q6 | spec 043 FR-013「禁止 retry」是否仍然合理？ | §4.2/4.3/4.5 + §5.4 |

### 1.3 调研主要参考

- **生产证据**：session `a7cb3d62f0269fa88410093380f79def` 在 `game.prod` 的 signoz 日志与 trace
- **跨框架**：LangChain（Python `langchain-openai` + JS `@langchain/openai`）、Codex CLI、Hermes Agent、OpenClaw、opencode client
- **opencode-go 文档**：[OpenCode Go 文档](https://opencode.ai/docs/zh-cn/go/)（确认 `deepseek-v4-flash` 走 `/chat/completions`，是 reasoning 模型）
- **spec 043**：`specs/043-llm-stream-stall-recovery/`（spec.md / plan.md / research.md / tasks.md）

---

## 2. spec 043 当前现状回顾

### 2.1 核心机制

| 维度 | 当前实现 | 文件 |
|---|---|---|
| **chunk-idle 检测** | LangGraph `TimeoutPolicy.idleTimeout`（per-node `addNode` 选项） | `projects/game/agent/src/team/graph.ts:365`（player/planner 节点）|
| **默认值** | `STREAM_IDLE_TIMEOUT_MS = 30000`（30s），env `GAME_STREAM_IDLE_TIMEOUT_MS` 可覆盖 | `projects/game/agent/src/llm.ts` |
| **触发后行为** | LangGraph `idleTimeout` 抛 `NodeTimeoutError` → player/planner re-throw → `runTeamTurn` 抛出 → `turn-loop.ts:runLoop` catch → `finishError`（emit `warn` + `wait`，retain buffer）| `projects/game/agent/src/turn-loop.ts:352-358, 413-424` |
| **tool 执行期保护** | 客户端 MCP wrapper `withIdleHeartbeat` 每 `TOOL_HEARTBEAT_INTERVAL_MS`（10s）调 `config.heartbeat()` refresh timer | `projects/game/agent/src/llm.ts`（T008b-r）|
| **init turn 总超时** | `AbortSignal.timeout(INIT_TURN_TIMEOUT_MS = 120000)` | `projects/game/agent/src/session-team.ts:runInitTurn` |
| **retry** | **显式禁止**（FR-013 "MUST NOT add automatic retry"）| `specs/043-llm-stream-stall-recovery/spec.md:135` |

### 2.2 LangGraph `idleTimeout` 的关键内部行为

来自 `node_modules/.pnpm/@langchain+langgraph@1.4.8_*/node_modules/@langchain/langgraph/dist/pregel/timeout.js:200-211`：

```javascript
const elapsed = Date.now() - start;
scope.close();
task.writes.splice(0, task.writes.length);  // ← 关键：主动丢弃所有 buffered writes
timeoutController.abort();
dispose?.();
throw new NodeTimeoutError({ node, elapsed, kind, runTimeout, idleTimeout });
```

LangGraph 的 `idleTimeout` 设计假设 node 是**原子的**（要么完整完成 → writes 进 checkpoint，要么完整失败 → writes 全丢弃）。timer 由 `IdleProgressCallbackHandler`（callback 事件 refresh）+ wall-clock `Promise.race` 组成。

### 2.3 spec 043 references 的依据

`spec.md:20` 写道：

> "opencode 通过 `chunkTimeout`（PR #25575）检测 silent stream dropout ... Recommended values: **15–30 seconds**."

引用的是 [opencode PR #25575](https://github.com/anomalyco/opencode/pull/25575)。这一依据**实际上不准确**——详见 §5.2。

---

## 3. 生产环境观察（session `a7cb3d62f0269fa88410093380f79def`）

### 3.1 stall 事件时间线（2026-08-12，game.prod）

通过 signoz 查询 trace `0362ac8cb2a8089011f92dcb539c756a`：

| 时间 | 服务 | 事件 |
|---|---|---|
| 13:17:15 | gateway | `ws connected` (template=saolei) |
| 13:17:43.356 | agent | `operation bridge sink registered` → player turn 启动 |
| 13:17:43 → 13:20:23 | — | **player 跑了 160.232s** |
| **13:20:23.609** | **agent** | **player stall**: `Node "player" exceeded its idle timeout of 30000ms (elapsed: 160232ms)` → `finishError`（retainedDepth=0）|
| 13:20:34.411 | agent | sink re-registered → 新 turn 启动 |
| 13:21:43.939–942 | agent | planner-family invoke failed × 2 (`This operation was aborted`) |
| **13:21:43.953** | **agent** | **planner stall**: `Node "planner" exceeded its idle timeout of 30000ms (elapsed: 30004ms)` → `finishError` |
| 13:24:11 | gateway | connect stream closed |

### 3.2 关键观察

1. **player `elapsed=160232ms`**：LangGraph `timeout.js:200` 显示 `elapsed = Date.now() - start`（整个 node 运行时长，**不是 idle 时长**）。idle 触发条件是 `now - scope.lastProgress >= idleTimeout`。前 130s 模型持续 streaming（每次 callback refresh timer），最后 30s 真正无 chunk → 触发。这正是用户描述的"think 输出卡顿然后中断"。
2. **planner `elapsed=30004ms`**：从 node 启动到触发全程零事件，30s 一次清零。
3. **prod 所有 session 都用 `deepseek-v4-flash`**（11 次 `creating model provider` 日志全部是该模型），但 **stall 全部集中在 saolei 这一个 session**——印证用户反馈"在其他模板没这么严重"。
4. **同样的 model + gateway，opencode 客户端不会中断**——用户实测对比，是诊断的关键转折点（详见 §4.5）。

### 3.3 opencode-go gateway 的社区反馈（佐证）

2026-08 这两周有大量"opencode-go + deepseek-v4-flash 流断/挂起"的反馈（如 [opencode#40465](https://github.com/anomalyco/opencode/issues/40465)、[#40479](https://github.com/anomalyco/opencode/issues/40479)、[openclaw#93610](https://github.com/openclaw/openclaw/issues/93610)），证明 gateway 自身也存在 L1 故障。但**用户的客户端对比说明：在 L1 之外，我们的 idleTimeout=30s 是更主要的触发因素**。

---

## 4. 跨框架调研

### 4.1 LangChain

#### Python 版（`langchain-openai`）— 与我们最相关

**[PR #36949](https://github.com/langchain-ai/langchain/pull/36949) "feat(openai): prevent silent streaming hangs in ChatOpenAI"**（2026-04-22，已合并于 1.2.0）

引入两个开关（**默认开启**）：

| 开关 | 默认 | 作用 |
|---|---|---|
| `stream_chunk_timeout` | **120s** | 用 `asyncio.wait_for` 包住每个 chunk 的 `__anext__`；测量 **parsed chunks 之间间隔**（keepalive 注释不 reset 它），触发时抛 `StreamChunkTimeoutError`（`TimeoutError` 子类）|
| `http_socket_options` | 启用 | `SO_KEEPALIVE` + `TCP_KEEPIDLE/INTVL/CNT` + `TCP_USER_TIMEOUT`，kernel 层兜底 |

[官方 reference](https://reference.langchain.com/python/langchain-openai/chat_models/base/BaseChatOpenAI/stream_chunk_timeout) 明确区分两种失败模式：
- `stream_chunk_timeout`：parsed-chunk 间隔（**keepalive 不算**）
- `request_timeout`：HTTP 总时长（不同失败模式）

[Issue #35597](https://github.com/langchain-ai/langchain/issues/35597) 还发现 `request_timeout=None` 默认值会 silently disable SDK 默认 600s timeout（PR #35745 修复）。

#### JS 版（`@langchain/openai`，dominion 当前用）

**JS 版本没有 `stream_chunk_timeout` 机制**——见 [langchainjs #9088](https://github.com/langchain-ai/langchainjs/issues/9088)，用户在求 `stream_timeout` 选项（chunk-idle），但未合并。这意味着 **dominion 用 LangChain JS 完全没有 client 层的 chunk-idle 保护**，唯一的防线是 LangGraph 的 `idleTimeout`。

### 4.2 OpenAI Codex CLI

多个 issue + PR 形成完整方案：

| Issue/PR | 关键发现 |
|---|---|
| [#3478](https://github.com/openai/codex/issues/3478) | 150s 切断 → 加 keepalive + heartbeats |
| [PR #5106](https://github.com/openai/codex/pull/5106) | **"prevent idle disconnects via HTTP keepalives + heartbeats"** |
| [PR #32763](https://github.com/openai/codex/pull/32763) | **"optional SSE keepalive comments during streaming silence"** |
| [#13666](https://github.com/openai/codex/issues/13666) | "Reconnecting 2/5" — retry 机制 |
| [#16168](https://github.com/openai/codex/issues/16168) | 300s 服务器端 idle timeout（Cloudflare）|
| [#23807](https://github.com/openai/codex/issues/23807) | 默认 `stream_idle_timeout` = **300s** |
| [#30526](https://github.com/openai/codex/issues/30526) | 需要加 stream-idle / turn-level watchdog |
| [#33090](https://github.com/openai/codex/issues/33090) | Windows 配置示例：`stream_idle_timeout_ms=60000`, `stream_max_retries=1` |

**Codex 的设计**：
- **区分 first_event_timeout（60s）vs stream_idle_timeout（300s）**
- **WebSocket ping/pong keepalive**（30–60s 间隔）
- **5 次 retry with exponential backoff**
- **per-turn stream-recovery deadline**（防止 5×300s = 25 分钟 hang）

### 4.3 Hermes Agent（NousResearch）— **方案最完整**

#### 4.3.1 Per-reasoning-model stale-timeout floor（核心方案）

来自 [commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)（基于 [issue #52217](https://github.com/NousResearch/hermes-agent/issues/52217)）。

`agent/reasoning_timeouts.py` 维护 reasoning 模型 allowlist：

```python
_REASONING_STALE_TIMEOUT_FLOORS: tuple[tuple[str, int], ...] = (
    ("nemotron-3-ultra-", 600),
    ("deepseek-r1",       600),    # ← DeepSeek 系列
    ("deepseek-reasoner", 600),
    ("o1-",               600),
    ("o3-",               600),
    ("claude-opus-",      240),
    # ...
)
# 应用方式：max(default, floor)，从不降低 explicit user config
```

应用点同时在 stream 和 non-stream 两条路径（[commit 1e161206](https://github.com/NousResearch/hermes-agent/commit/1e16120603b4bc34114f87c0d278a7805241087c) 给 `deepseek-v4-flash` 加 600s floor）。

#### 4.3.2 实测数据（opencode-go + deepseek-v4-flash）

[Issue #61461](https://github.com/NousResearch/hermes-agent/issues/61461) "opencode-go + deepseek-v4-flash still hangs after #61397 merged"：

| 测试 | 结果 | 时间 |
|---|---|---|
| Raw `httpx` 直接调 `https://opencode.ai/zen/go/v1/chat/completions` | ✅ 200 | **3.0–3.4s** |
| OpenAI SDK + Hermes timeout | ✅ 完成 | **67s** |
| OpenAI SDK + Hermes keepalive http_client | ✅ 完成 | **86s** |

**关键结论**：
> "deepseek-v4 streaming **can complete** end-to-end, but the first `content` token takes **~65s** because the model consumes all tokens on `reasoning_content` first."

#### 4.3.3 完整的三层 timeout 体系

来自 [Hermes 配置文档](https://hermes-agent.nousresearch.com/docs/user-guide/configuration)：

| Timeout | 默认 | Local providers | 配置 |
|---|---|---|---|
| Socket read | 120s | Auto → 1800s | `HERMES_STREAM_READ_TIMEOUT` |
| Stale stream | 180s | → 900s | `HERMES_STREAM_STALE_TIMEOUT` |
| Stale non-stream | 90s | Auto-disable | `stale_timeout_seconds` |
| Reasoning floor | — | — | `reasoning_timeouts.py` allowlist |

#### 4.3.4 Eager fallback + circuit breaker

[PR #22278](https://github.com/NousResearch/hermes-agent/pull/22278) "eager fallback on stream-stall timeouts" + cross-turn circuit breaker（防 retry loop weaponizing timeout，最终落地于 [#53911](https://github.com/NousResearch/hermes-agent/pull/53911)）。

#### 4.3.5 keepalive pooling 与 Cloudflare 的 race

[Issue #67012](https://github.com/NousResearch/hermes-agent/issues/67012) 详细分析了 `httpx.Limits(keepalive_expiry=20.0)` 与 Cloudflare 的 race condition，通过 A/B 隔离测试确认 20s expiry 实际是 FIX（防 dead connection reuse）而非 cause。

[PR #25260 / commit 55d6a16](https://github.com/NousResearch/hermes-agent/commit/55d6a1636bb1f38b01b708582c527b91cc9fe578) 修复 streaming 路径 hardcoded `connect=30.0` / `pool=30.0`——`connect/pool` 应该用 `min(_base_timeout, 60.0)` 上限（TCP handshake 不应该等 inference 时长）。

### 4.4 OpenClaw

#### 4.4.1 opencode-go 专项 stream wrapper

[PR #93965](https://github.com/openclaw/openclaw/pull/93965) "fix(opencode-go): streaming completes when provider ends responses"：

- **first-event window: 300s**（TTFB，应对慢首字节）
- **idle window: 120s**（chunk 间间隔）
- **关键设计**：区分 **synthetic preamble events**（`start` / `text_start` / `thinking_start` / `toolcall_start`）vs **real provider delta**——idle timer 只在 real delta 后才开始

#### 4.4.2 Boundary events 算 liveness

[Issue #96518](https://github.com/openclaw/openclaw/issues/96518) + [PR #96526](https://github.com/openclaw/openclaw/pull/96526) "treat stream block boundaries as liveness"：修复 delta-only liveness bug，把 `text_end` / `thinking_end` / `toolcall_start` / `toolcall_end` 也算 liveness，gated behind `sawProviderDelta`（pre-delta synthetic 不缩短 first-event window）。

#### 4.4.3 reasoning chunks 必须通知 idle watchdog ⭐ 与我们最相关

[PR #114406](https://github.com/openclaw/openclaw/pull/114406) "fix(ai): notify idle watchdog of OpenAI completions stream activity"：

> "The OpenAI completions transport never called `notifyLlmRequestActivity`. Reasoning models served via OpenAI-compatible endpoints (vLLM with `--reasoning-parser`) that emit only `thinking_delta` events before any `content` were misclassified as idle by the watchdog and aborted with `LLM idle timeout (120s)`."

OpenClaw 发现 OpenAI compatible transport 没把 `thinking_delta` 当 liveness，导致 reasoning 模型被误判 stall。**这正是我们的场景**——前面已验证 LangChain JS `completions.js:188-219` 每个 SSE chunk（包括纯 reasoning）都会触发 `handleLLMNewToken`，所以**我们暂时没有这个 bug**，但需持续关注。

#### 4.4.4 区分 model idle vs tool execution（replay-safety）

[PR #93655](https://github.com/openclaw/openclaw/pull/93655) "classify stuck recovery as idle timeout"：区分 model idle timeout（replay-safe，可 retry/fallback）vs tool execution timeout（replay 有副作用风险，不自动 retry）。

### 4.5 opencode client（用户对比的对象）

#### 4.5.1 chunkTimeout 默认 disabled

[PR #18264](https://github.com/anomalyco/opencode/pull/18264) "fix(core): disable chunk timeout by default"（2026-03-19，jlongster）：

> "We are removing the default chunk timeout for now, you can still turn it on in the config if you want this feature. In the future we may re-introduce this for specific models that we know are safe to apply this, but **this is causing too many small issues across the board right now**."

[commit d69962b](https://github.com/anomalyco/opencode/commit/d69962b0f7ca54494452dd902053088f8113809d) 的 diff：

```diff
-const DEFAULT_CHUNK_TIMEOUT = 300_000
 ...
-const chunkTimeout = options["chunkTimeout"] || DEFAULT_CHUNK_TIMEOUT
+const chunkTimeout = options["chunkTimeout"]
```

**opencode 客户端默认 disabled**——必须用户显式配 `chunkTimeout` 才生效。

#### 4.5.2 retry + reconnect 机制

[PR #19116](https://github.com/anomalyco/opencode/pull/19116) "fix(opencode): reconnect on network disruptions"：

> "Network errors (ECONNRESET, ETIMEDOUT, SSE timeout, etc.) now get their own retry, separate from the existing API retry logic. **It's bounded at 5 attempts with exponential backoff capped at 5s.** If the stream already produced chunks before dying, cleans up partial text/reasoning parts and incomplete tool calls before retrying so you don't get duplicate or corrupt output."

### 4.6 跨框架共识对比表

| 维度 | LangChain (Py) | Codex | Hermes | OpenClaw | opencode client | **dominion (spec 043)** |
|---|---|---|---|---|---|---|
| **chunk-idle 默认值** | **120s** | **300s** | **180s** | **120s idle + 300s first-event** | **disabled** | **30s** ⚠️ |
| **reasoning 模型特殊处理** | 无 | 无 | **600s floor (allowlist)** | 无 | 无 | 无 |
| **first-event vs inter-event 区分** | 无 | **有 (60s vs 300s)** | 无 | **有 (300s vs 120s)** | 无 | 无 |
| **retry / fallback** | 无 | **5 次指数退避** | **eager fallback + circuit breaker** | **rotate/fallback** | **5 次重连** | **明确禁止 (FR-013)** ⚠️ |
| **boundary events 算 liveness** | N/A | N/A | N/A | **是 (text_end/thinking_end/toolcall_*)** | N/A | 未知 |
| **reasoning chunks 是否 notify** | 是 | 是 | 是 | **修复中（#114406）** | 是 | 是（LangChain JS 每个 chunk notify）|
| **TCP keepalive / socket options** | **是（#36949）** | **是（#5106）** | 部分（keepalive expiry 调优）| 无 | 无 | 无 |
| **SSE keepalive comments** | N/A | **是（#32763）** | N/A | N/A | N/A | 无 |

**结论**：spec 043 的 `STREAM_IDLE_TIMEOUT_MS = 30000` 是上表所有框架里**最激进**的，比行业最宽松的（Codex 300s）激进 10 倍，比行业中等水平（LangChain 120s、OpenClaw 120s、Hermes 180s）激进 4–6 倍。

---

## 5. 问题分析

### 5.1 P0：idleTimeout 默认值过激进

**事实**：
- spec 043 默认 30s
- 行业最激进的非我们框架是 LangChain Python 的 120s（4 倍）
- Codex 300s、Hermes reasoning floor 600s、opencode client disabled

**根因**：spec 043 references（§5.2）依据错误，把"PR #25575 description 里的 example value"当成了"community consensus"。

**实测影响**：
- session `a7cb3d62` 在 7 分钟内连续两次 stall
- Hermes 实测 `deepseek-v4-flash` 首 content token 耗时 **~65s**（[hermes#61461](https://github.com/NousResearch/hermes-agent/issues/61461)）——**正常 reasoning 思考就超过 30s**
- saolei 是 reasoning + 多 tool-call 循环场景（每次 tool 后又重新 reason），单局内多次可能触发

### 5.2 P0：spec 043 references 基于过时实践

`spec.md:20` 引用 [opencode PR #25575](https://github.com/anomalyco/opencode/pull/25575)：

> "Recommended values: 15–30 seconds."

但实际上：
1. PR #25575 只是把**用户配置**的 chunkTimeout propagate 到 `streamText.timeout.chunkMs`，**不是改默认值**
2. opencode 客户端的实际默认值在更早的 [PR #18264](https://github.com/anomalyco/opencode/pull/18264) 已改为 **disabled**——理由是 "causing too many small issues across the board"
3. opencode 自己否决了这个 default，但 spec 043 把 PR #25575 description 的 example 当成了 consensus

### 5.3 P1：reasoning 模型需要专门 floor

`deepseek-v4-flash` 是 reasoning 模型（[opencode-go 文档](https://opencode.ai/docs/zh-cn/go/) 明确），其行为：
- 先消化 `reasoning_content`（思考阶段，耗时长）
- 然后才发出第一个 `content` token
- 复杂任务（如 saolei 策略分析）思考时间可达 60-180s

LangGraph 的 `idleTimeout` 通过 `IdleProgressCallbackHandler` 在每个 LangChain callback 时 refresh。前面验证 LangChain JS `@langchain/openai` 1.5.5 的 `completions.js:188-219` 每个 SSE chunk（包括纯 reasoning chunk）都会调用 `handleLLMNewToken`，所以**只要 SSE 流持续发 reasoning chunk，timer 就会 refresh**。但**模型进入深度思考阶段时完全不发 chunk**（连 `:keep-alive` ping 都没有），timer 就超时。

Hermes 的 [reasoning_timeouts.py](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa) 给出了**行业验证的解法**：维护 reasoning 模型 allowlist，对这些模型 floor 设为 600s（DeepSeek 系列）。

### 5.4 P1：FR-013「禁止 retry」与行业实践脱节

spec 043 `spec.md:135` FR-013：

> "This feature MUST NOT add automatic retry of the stalled turn. ... Retry logic is explicitly out of scope."

但所有对比框架都有 retry / fallback：
- **Codex**：5 次指数退避（`stream_max_retries`）
- **Hermes**：eager fallback + cross-turn circuit breaker（[PR #22278](https://github.com/NousResearch/hermes-agent/pull/22278) + [#53911](https://github.com/NousResearch/hermes-agent/pull/53911)）
- **OpenClaw**：rotate/fallback（区分 model idle vs tool execution 的 replay-safety，[PR #93655](https://github.com/openclaw/openclaw/pull/93655)）
- **opencode client**：5 次网络错误重连 + partial cleanup（[PR #19116](https://github.com/anomalyco/opencode/pull/19116)）

**FR-013 的理由**（spec.md:161）："the user's queued messages are retained and auto-drained on the next turn, so manual recovery is one message away"——但这忽略了：
1. **stall 是 false positive 的情况占多数**（reasoning 模型思考被误判），重试一次很可能成功
2. **每次 stall 都丢失 partial output**（§5.5），用户体验不可接受
3. saolei 一局游戏含多 tool-call 循环，每个循环都可能 stall，单局失败概率极高

### 5.5 P1（新发现）：partial output 在 checkpoint 永久丢失

#### 5.5.1 现象

用户实测：
```
开始游戏 → agent output1 → 中断（stall）→ user "继续游戏" → agent output2 → abort
```
重新进入 session 后，`ListMessages` 返回：
```
开始游戏 / 继续游戏 / agent output2
```
**output1 永久丢失**。

#### 5.5.2 根因

LangGraph `idleTimeout` 触发时（`timeout.js:200-203`）：

```javascript
const elapsed = Date.now() - start;
scope.close();
task.writes.splice(0, task.writes.length);  // ← 主动丢弃所有 buffered writes
timeoutController.abort();
throw new NodeTimeoutError({...});
```

LangGraph 假设 node 是原子的（要么完整完成 → writes 进 checkpoint，要么完整失败 → writes 全丢弃）。但 streaming 场景下"原子"被破坏：

| 步骤 | 前端 | LangGraph checkpoint |
|---|---|---|
| user "开始游戏" | "开始游戏" | `playerMessages: [HumanMessage]` |
| agent output1 streaming | 流式看到 partial output1 | createAgent 内部 accumulate（**未写入 task.writes**）|
| **idleTimeout 触发** | stream 中断 + warn + wait | **`task.writes.splice` 清空** + NodeTimeoutError |
| user "继续游戏"（buffer） | QueueSignal +1 | buffer retain（FR-006）|
| next turn drain | "继续游戏" 自动作为下个 turn 输入 | `playerMessages: [Human, Human("继续游戏")]`（output1 不在）|
| agent output2 完成 | output2 | `playerMessages: [Human, Human, AIMessage(output2)]` |
| **ListMessages 重读** | "开始游戏" + "继续游戏" + output2 | **output1 永久丢失** |

`ListMessages` 直接读 checkpoint（`projects/game/agent/src/handler.ts:619-620` → `team.getTeamState()` → `graph.getState()` → `snapshot.values.playerMessages`）。前端通过 Connect stream 看到过 output1 的 partial，但 checkpoint 没有。

#### 5.5.3 spec 043 的盲区

spec 043 的 acceptance scenarios（spec.md US1/US2 + Edge Cases）**没有任何一条覆盖**"stall 时已 streaming 的 agent output 是否要保留"。FR-006 只说 queued-message buffer 保留：

> "the queued-message buffer MUST be RETAINED — NOT cleared"

"queued-message buffer" 指的是 TurnLoop 的 user input buffer，**不包括已经流出去的 agent output**。

---

## 6. 修订建议

按优先级排序。每条修订都直接对应 spec 043 的某个文件/FR/默认值变更。

### 6.1 P0：调整 idleTimeout 默认值 + 纠正 references

**目标**：把默认值从行业最激进的 30s 调整到行业中等水平。

**变更点**：

1. `projects/game/agent/src/llm.ts`：`STREAM_IDLE_TIMEOUT_MS` 默认值 `30000` → **`120000`**（120s，对齐 LangChain Python / OpenClaw）
2. `specs/043-llm-stream-stall-recovery/spec.md:111` FR-001：
   - 现状："default: 30 seconds"
   - 改为："default: 120 seconds"
   - 现状："MUST be at least 15 seconds"
   - 改为："MUST be at least 60 seconds（reasoning 模型推荐 300s 以上）"
3. `specs/043-llm-stream-stall-recovery/spec.md:20` 与 `references`：移除 "Recommended values: 15–30 seconds"，改为引用：
   - [LangChain PR #36949](https://github.com/langchain-ai/langchain/pull/36949)（120s default）
   - [OpenClaw PR #93965](https://github.com/openclaw/openclaw/pull/93965)（120s idle + 300s first-event）
   - [Hermes commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)（reasoning floor）
   - [opencode PR #18264](https://github.com/anomalyco/opencode/pull/18264)（disabled by default 的反向证据）

**风险评估**：
- 调大到 120s 意味着真实网络 stall 的检测延迟变长（30s → 120s）
- 但 reasoning 模型 + saolei 场景下，false positive 的减少远比检测延迟重要
- 若需更激进的真 stall 检测，应配合 §6.3 引入 retry 而非缩短 timeout

### 6.2 P1：实现 per-reasoning-model floor

**目标**：参考 Hermes 的 `reasoning_timeouts.py`，给 reasoning 模型一个更大的 idle timeout 下限。

**设计**（伪代码，仅描述）：

```typescript
// 新增 projects/game/agent/src/reasoning-timeouts.ts
const REASONING_IDLE_TIMEOUT_FLOOR: ReadonlyArray<readonly [substring: string, floorMs: number]> = [
  // DeepSeek 系列（hermes 实测 first-token ~65s，floor 给 600s 安全裕度）
  ["deepseek-r1",         600_000],
  ["deepseek-reasoner",   600_000],
  ["deepseek-v4-",        600_000],
  // OpenAI o 系列
  ["o1-",                 600_000],
  ["o3-",                 600_000],
  ["o4-mini-",            300_000],
  // Anthropic Claude 4.x thinking
  ["claude-opus-",        240_000],
  // MiMo / QwQ / Grok reasoning
  ["mimo-v2.5-pro",       300_000],
  ["qwq-",                300_000],
  ["grok-4-fast-reasoning", 300_000],
];

export function getReasoningIdleTimeoutFloor(modelSpec: string): number | null {
  const bare = parseModelSpec(modelSpec);  // 复用 model-provider.ts
  // longest-first 匹配（参考 hermes：'o3-mini-' 必须早于 'o3-'）
  const sorted = [...REASONING_IDLE_TIMEOUT_FLOOR].sort((a, b) => b[0].length - a[0].length);
  for (const [substring, floor] of sorted) {
    if (bare.includes(substring)) return floor;
  }
  return null;
}
```

应用点：`projects/game/agent/src/team/graph.ts` 的 `addNode("player", ...)` / `addNode("planner", ...)`：

```typescript
// 伪代码
const baseTimeout = STREAM_IDLE_TIMEOUT_MS;
const reasoningFloor = getReasoningIdleTimeoutFloor(profile.playerModel);
const playerTimeout = reasoningFloor ? Math.max(baseTimeout, reasoningFloor) : baseTimeout;
graph.addNode("player", playerNode, { timeout: { idleTimeout: playerTimeout, refreshOn: "auto" } });
```

**新增 FR**（加入 spec 043 或后续 spec）：

> **FR-X**：reasoning 模型（substring 匹配 `REASONING_IDLE_TIMEOUT_FLOOR` 表）的 `idleTimeout` MUST 不低于对应的 floor 值；显式 user config（env var）始终优先（参考 [hermes reasoning_timeouts.py](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa) 的 `max(default, floor)` 设计）。

**风险评估**：
- 维护一份 substring 列表有持续成本（新模型上线需更新）
- 但相比"全部用 600s"或"全部用 30s"，分桶更精准
- longest-first 匹配避免 `o1` 误匹配 `olmo-1`（hermes 已踩过坑）

### 6.3 P1：重新评估 FR-013，引入有限 retry/fallback

**目标**：把 FR-013 从"MUST NOT"改为"有限允许"，参考 opencode client / Codex 的做法。

**新设计**：

> **FR-013（修订）**：stall 触发后允许**单次自动 retry**（条件：未发生 side-effecting tool 调用，即 model idle 而非 tool execution timeout），retry 失败后才走 `finishError`。Retry 不重新发起已经执行过的 tool 调用（replay-safety，参考 [openclaw PR #93655](https://github.com/openclaw/openclaw/pull/93655)）。

**实现思路**（仅描述）：
- `turn-loop.ts:runLoop` 的 catch 块增加 retry 计数器
- 区分 `model_idle`（replay-safe）vs `tool_execution_timeout`（不可 retry）
- model idle 时 retry 一次同 turn，retry 内重新 `graph.streamEvents`（LangGraph checkpointer 保证消息历史延续）
- 配置项 `STALL_MAX_RETRIES`（默认 1，参考 Codex 的 5 次 + OpenClaw 的 single-shot）

**配套**：
- 参考 [Hermes #22278](https://github.com/NousResearch/hermes-agent/pull/22278) 的 cross-turn circuit breaker：连续多次 stall 后停止 retry，避免 weaponizing timeout
- 参考 [opencode PR #19116](https://github.com/anomalyco/opencode/pull/19116) 的 partial cleanup：retry 前清理已 streaming 的 partial text/reasoning/tool_call（与 §6.4 配合）

**风险评估**：
- replay-safety 是关键：必须确保 retry 不重复执行 side-effecting tool（如 `saolei_operate` 已经执行过的点击）
- 这需要识别"model idle"和"tool execution"两个阶段，类似 OpenClaw 的 `classifyStuckRecoveryAbort`
- 实现复杂度高于 §6.1 / §6.2，建议作为独立 spec

### 6.4 P1：Partial output 持久化（方案 A，修复 output1 丢失 bug）

**目标**：stall 触发时把已 streaming 的 agent output 写入 checkpoint，让 `ListMessages` 能读到。

**方案 A：在 `runTeamTurn` 层累积 deltas，stall 时手动 `updateState`**

**实现思路**（仅描述，参考 `projects/game/agent/src/session-team.ts:786-843`）：

1. `runTeamTurn` 维护一个 `partialBlocks: TurnBlock[]`，每个 yield 前先 push（包括 text / reasoning / tool_call / tool_result 块）
2. try/catch 包住 `for await` 循环；catch 到 `NodeTimeoutError` 时：
   - 把 `partialBlocks` 合并成一条 AIMessage（text/reasoning 内容拼到 content，tool_calls 拼到 tool_calls）
   - 单独累积的 ToolMessage（tool_result）作为独立消息
   - 调用 `this.graphHandle.graph.updateState({ configurable: { thread_id: this.sessionId } }, { playerMessages: [mergedAIMessage, ...toolMessages] })` 写入 checkpoint
   - 然后 re-throw 让 `turn-loop` 走 `finishError`（保留 user buffer）
3. 加一个 partial 标记（如 `additional_kwargs.partial = true` 或 `response_metadata.stall_recovered = true`），让前端能区分"完整回复"和"stall 截断回复"

**改造范围**：
- `projects/game/agent/src/session-team.ts:runTeamTurn`（核心）
- 可能需要辅助函数 `mergePartialBlocks(blocks): { aiMessage: AIMessage; toolMessages: ToolMessage[] }`
- 新增 FR（详见下方）

**新增 FR**（建议加入 spec 043 修订或新 spec 044）：

> **FR-Y**：When a turn terminates due to a detected stream stall, the agent's already-streamed partial output (text / reasoning / tool_call blocks emitted before the stall) MUST be persisted to the checkpoint so it survives session reconnection (`ListMessages` returns it). The partial output MUST be marked as incomplete (e.g., via metadata flag) so the user can distinguish a stall-truncated reply from a complete one.
>
> Rationale: spec 043 FR-006 only retains the user input buffer; it does not cover agent output already streamed to the frontend. LangGraph's `idleTimeout` calls `task.writes.splice(0, ...)` on abort, dropping buffered writes — without explicit compensation in the runner, partial output is permanently lost from the checkpoint while remaining visible in the frontend's live stream, creating state inconsistency.

**关键决策点**：

| 子问题 | 选项 | 倾向 |
|---|---|---|
| partial 写入位置 | (a) `playerMessages` channel（与正常完成一致）/ (b) 单独 `partialOutputs` channel | (a) — 保持 ListMessages 协议不变 |
| tool_call 部分 | (a) 丢弃 partial tool_call / (b) 保留并标记 incomplete | (a) — partial tool_call 无法 dispatch，保留会污染 tool history |
| tool_result 已完整 | (a) 保留 / (b) 丢弃 | (a) — 已 dispatch 到 desktop 的操作是 side-effect，必须保留 |
| 标记机制 | (a) `additional_kwargs.partial = true` / (b) `response_metadata.stall_recovered = true` / (c) 单独 AIMessage id prefix | (b) — 与 LangChain 既有 metadata 模式一致 |

**风险评估**：
- `graph.updateState` 在 controller abort 后是否能正常调用？需验证（理论上 abort 只影响 in-flight invoke，updateState 是独立调用）
- 累积所有 deltas 的内存开销：单 turn 内一般 << 100KB，可接受
- tool_call/tool_result 合并的 corner case 需要测试覆盖

---

## 7. 实施建议与阶段划分

### 7.1 推荐分阶段实施

| 阶段 | 内容 | 优先级 | 改造范围 |
|---|---|---|---|
| **Phase 1**（止血） | §6.1：调整 `STREAM_IDLE_TIMEOUT_MS` 默认值 30s → 120s + 更新 spec references | **P0** | 单常量 + spec 文档 |
| **Phase 2**（reasoning 适配） | §6.2：实现 per-reasoning-model floor | P1 | 新增 `reasoning-timeouts.ts` + `graph.ts` 应用 |
| **Phase 3**（数据完整性） | §6.4：partial output 持久化（方案 A）| P1 | `session-team.ts:runTeamTurn` 改造 + 新 FR |
| **Phase 4**（容错增强） | §6.3：有限 retry/fallback + replay-safety | P1（独立 spec）| `turn-loop.ts:runLoop` 改造 + 区分 model/tool 阶段 |
| **Phase 5**（可观测性） | LLM HTTP instrumentation（fetch wrapper 记录 chunk 时间序列）| P2 | `model-provider.ts` 加临时/永久 log |

### 7.2 验证门禁

每个 phase 完成后必须通过：

1. **Phase 1**：
   - 单元测试：`STREAM_IDLE_TIMEOUT_MS === 120_000`
   - 大型测试：在 prod-like 环境跑 saolei session 1 局完整游戏，统计 stall 次数（应显著下降）
2. **Phase 2**：
   - 单元测试：`getReasoningIdleTimeoutFloor("deepseek-v4-flash") === 600_000`、`getReasoningIdleTimeoutFloor("gpt-4") === null`
   - 大型测试：deepseek-v4-flash 跑 saolei 不再因 reasoning 思考触发 stall
3. **Phase 3**：
   - 单元测试：mock stall 场景，验证 `updateState` 被调用、AIMessage 包含已 yield 的 partial 内容、metadata 标记正确
   - 大型测试：saolei stall → 重新进入 session → ListMessages 返回包含 partial output1
4. **Phase 4**：
   - 单元测试：replay-safety（model idle 可 retry、tool execution 不可 retry）
   - 大型测试：连续 stall 触发 circuit breaker

### 7.3 文档更新清单

无论 Phase 如何分阶段，建议一次性更新以下文档：

- `specs/043-llm-stream-stall-recovery/spec.md`：修订 FR-001（默认值）、FR-013（retry）、references；新增 FR-X（reasoning floor）、FR-Y（partial output 持久化）
- `specs/043-llm-stream-stall-recovery/plan.md`：更新 Summary 与 Constitution Check
- `specs/043-llm-stream-stall-recovery/research.md`：补充"跨框架调研"章节（引用本文档 §4）
- `specs/043-llm-stream-stall-recovery/quickstart.md`：更新默认值表格

---

## 8. 附录：核心证据索引

### 8.1 生产环境证据（signoz）

- session `a7cb3d62f0269fa88410093380f79def`，env `game.prod`，trace `0362ac8cb2a8089011f92dcb539c756a`
- 关键日志：
  - `2026-08-12T13:20:23.609Z` — `Node "player" exceeded its idle timeout of 30000ms (elapsed: 160232ms)`
  - `2026-08-12T13:21:43.953Z` — `Node "planner" exceeded its idle timeout of 30000ms (elapsed: 30004ms)`
- prod 7 天内所有 11 次 `creating model provider` 都是 `deepseek-v4-flash`
- stall 全部集中在 saolei session（其他 5 个 session 用同模型未触发）

### 8.2 跨框架关键 PR/Issue

#### LangChain
- [PR #36949](https://github.com/langchain-ai/langchain/pull/36949) — feat(openai): prevent silent streaming hangs in ChatOpenAI（stream_chunk_timeout 默认 120s）
- [commit 4000c22](https://github.com/langchain-ai/langchain/commit/4000c223763aa301cafb85d7b11f4f6a1210efde) — 同上的 commit
- [LangChain stream_chunk_timeout reference](https://reference.langchain.com/python/langchain-openai/chat_models/base/BaseChatOpenAI/stream_chunk_timeout)
- [LangChain KB: StreamChunkTimeoutError](https://kb.langchain.com/articles/4695245229-streamchunktimeouterror-during-async-streaming-with-langchain-openai)
- [Issue #35597](https://github.com/langchain-ai/langchain/issues/35597) — request_timeout=None disables SDK timeout
- [PR #35745](https://github.com/langchain-ai/langchain/pull/35745) — fix(openai): prevent request_timeout=None from disabling SDK default timeout
- [langchainjs #9088](https://github.com/langchain-ai/langchainjs/issues/9088) — Streaming inactivity timeout incorrectly aborts after total timeout（JS 版缺失 stream_timeout）

#### Codex CLI
- [Issue #3478](https://github.com/openai/codex/issues/3478) — Codex CLI requests consistently cut off at ~150s
- [PR #5106](https://github.com/openai/codex/pull/5106) — prevent idle disconnects via HTTP keepalives + heartbeats
- [PR #32763](https://github.com/openai/codex/pull/32763) — optional SSE keepalive comments during streaming silence
- [Issue #13666](https://github.com/openai/codex/issues/13666) — Codex stuck in Thinking
- [Issue #16168](https://github.com/openai/codex/issues/16168) — Codex CLI hangs on Ubuntu（300s proxy idle timeout 分析）
- [Issue #23807](https://github.com/openai/codex/issues/23807) — codex-cli stalls for exactly 300s（stream_idle_timeout 默认 300s）
- [Issue #30526](https://github.com/openai/codex/issues/30526) — app-server turn hangs indefinitely after tool result
- [Issue #33090](https://github.com/openai/codex/issues/33090) — Windows: stream_idle_timeout_ms=60000 配置示例

#### Hermes Agent
- [commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa) — apply per-reasoning-model stale-timeout floor
- [Issue #52217](https://github.com/NousResearch/hermes-agent/issues/52217) — Reasoning models inherit chat-model stale-timeout defaults
- [Issue #61461](https://github.com/NousResearch/hermes-agent/issues/61461) — opencode-go + deepseek-v4-flash still hangs（实测 first-token ~65s）
- [Issue #67012](https://github.com/NousResearch/hermes-agent/issues/67012) — keepalive_expiry=20s breaks Cloudflare streaming
- [Issue #29418](https://github.com/NousResearch/hermes-agent/issues/29418) — Nous inference API streaming times out（deepseek-v4-flash 加 600s floor）
- [PR #25260 / commit 55d6a16](https://github.com/NousResearch/hermes-agent/commit/55d6a1636bb1f38b01b708582c527b91cc9fe578) — honor provider timeout config in streaming API calls
- [PR #22278](https://github.com/NousResearch/hermes-agent/pull/22278) — eager fallback on stream-stall timeouts
- [Issue #23286](https://github.com/NousResearch/hermes-agent/issues/23286) — DeepSeek 600s server-side stream limit

#### OpenClaw
- [PR #93965](https://github.com/openclaw/openclaw/pull/93965) — fix(opencode-go): streaming completes when provider ends responses
- [Issue #93610](https://github.com/openclaw/openclaw/issues/93610) — opencode-go provider streaming never receives termination signal
- [Issue #96518](https://github.com/openclaw/openclaw/issues/96518) — opencode-go stalled-stream watchdog aborts live stream（delta-only liveness bug）
- [PR #96526](https://github.com/openclaw/openclaw/pull/96526) — fix(opencode-go): treat stream block boundaries as liveness
- [PR #97128](https://github.com/openclaw/openclaw/pull/97128) — re-arm idle timer on block-boundary events
- [PR #114406](https://github.com/openclaw/openclaw/pull/114406) — notify idle watchdog of OpenAI completions stream activity（reasoning chunks）
- [PR #93655](https://github.com/openclaw/openclaw/pull/93655) — classify stuck recovery as idle timeout（replay-safety）
- [Issue #95530](https://github.com/openclaw/openclaw/issues/95530) — opencode-go streaming hangs in isolated cron sessions

#### opencode client
- [PR #18264](https://github.com/anomalyco/opencode/pull/18264) — disable chunk timeout by default（commit [d69962b](https://github.com/anomalyco/opencode/commit/d69962b0f7ca54494452dd902053088f8113809d)）
- [PR #19116](https://github.com/anomalyco/opencode/pull/19116) — reconnect on network disruptions
- [PR #25575](https://github.com/anomalyco/opencode/pull/25575) — propagate chunkTimeout to streamText timeout.chunkMs
- [commit 8c53b2b](https://github.com/anomalyco/opencode/commit/8c53b2b47033c579b46b02b1ba9638004de0154f) — increase default chunk timeout from 2 min to 5 min

#### opencode-go gateway（社区反馈）
- [opencode#40465](https://github.com/anomalyco/opencode/issues/40465) — deepseek-v4-flash drops connection before response
- [opencode#40479](https://github.com/anomalyco/opencode/issues/40479) — DeepSeek V4 Flash route hangs
- [opencode#40485](https://github.com/anomalyco/opencode/issues/40485) — deepseek-v4-flash returns 403 / hangs
- [opencode#37635](https://github.com/anomalyco/opencode/issues/37635) — gateway returns reasoning_content instead of content
- [opencode#30002](https://github.com/anomalyco/opencode/issues/30002) — opencode-go upstream idle timeout on reasoning-heavy（建议流式 reasoning_content）
- [opencode#40171](https://github.com/anomalyco/opencode/issues/40171) — Go /v1/responses returns incomplete SSE event stream

### 8.3 仓库内文件引用

| 文件 | 用途 |
|---|---|
| `specs/043-llm-stream-stall-recovery/spec.md` | spec 043 FR 与 references（待修订）|
| `specs/043-llm-stream-stall-recovery/plan.md` | spec 043 plan Summary（待修订）|
| `specs/043-llm-stream-stall-recovery/research.md` | spec 043 research（需补充跨框架调研章节）|
| `specs/043-llm-stream-stall-recovery/tasks.md` | spec 043 任务（已全部交付，本文档不重新规划）|
| `projects/game/agent/src/llm.ts` | `STREAM_IDLE_TIMEOUT_MS` 常量定义（待修订默认值）|
| `projects/game/agent/src/team/graph.ts:365` | player/planner 节点 `addNode` 的 timeout 配置（待应用 reasoning floor）|
| `projects/game/agent/src/turn-loop.ts:334-391` | `runLoop`（retry 改造点）|
| `projects/game/agent/src/turn-loop.ts:413-424` | `finishError`（partial output 持久化的协作点）|
| `projects/game/agent/src/session-team.ts:725-870` | `runTeamTurn`（partial output 累积 + updateState 改造点）|
| `projects/game/agent/src/session-team.ts:317-322` | `getTeamState`（ListMessages 读取链）|
| `projects/game/agent/src/handler.ts:581-635` | `ListMessages` 实现（验证用）|
| `projects/game/agent/src/model-provider.ts` | `initChatModel`（无 stream_chunk_timeout，是 LangChain JS 的局限）|
| `node_modules/.pnpm/@langchain+langgraph@1.4.8_*/node_modules/@langchain/langgraph/dist/pregel/timeout.js:200-211` | LangGraph `idleTimeout` 触发时的 writes.splice 行为（output1 丢失根因）|

### 8.4 opencode-go 文档

- [OpenCode Go（中文）](https://opencode.ai/docs/zh-cn/go/)
- [OpenCode Go（英文）](https://opencode.ai/docs/go/)
- [OpenCode Zen](https://opencode.ai/docs/zen/)
- [OpenCode Config](https://opencode.ai/docs/config/)（`chunkTimeout` 配置项说明）

---

## 9. 待决策问题（供方案设计阶段确认）

1. **idleTimeout 新默认值**：120s（对齐 LangChain/OpenClaw）还是 180s（更接近 Hermes）？
2. **reasoning floor 表的维护责任**：硬编码在 `reasoning-timeouts.ts`，还是从外部配置（env / config file）加载？
3. **partial output 标记机制**：`response_metadata.stall_recovered = true` vs `additional_kwargs.partial = true` vs 单独 id prefix？（倾向前者）
4. **retry 的 replay-safety 实现细节**：如何区分"model idle"和"tool execution"？是否引入 OpenClaw 风格的 `activePotentialSideEffectTool` tracker？
5. **是否拆 spec**：本次修订内容较大，是作为 spec 043 的修订（v2），还是开新 spec 044？
6. **langchain JS 是否需要自写 `stream_chunk_timeout`**：LangChain JS 至今没有 client 层 chunk-idle 保护，我们完全依赖 LangGraph 的 idleTimeout；长期是否需要在 model-provider 层加自写 wrapper？

---

> **下一步**：基于本文档启动 spec 修订（spec 043 v2 或 spec 044），明确每个 phase 的 FR / 任务 / 验证门禁，遵循 `.specify/memory/constitution.md` 的 SDD 流程。
