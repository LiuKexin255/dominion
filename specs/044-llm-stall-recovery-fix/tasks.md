# Tasks: LLM Stream Stall Recovery — Timeout Tuning & Partial Output Persistence

**Input**: Design documents from `/specs/044-llm-stall-recovery-fix/`

**Prerequisites**: [plan.md](plan.md) (required), [spec.md](spec.md) (required), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Per Constitution IV, unit tests are part of each implementation task (not separate tasks). Large-test cases (Go, by module) + `guitar run` execution are separate acceptance tasks (Constitution VI).

**Organization**: Tasks are grouped by user story (spec.md US1/US2/US3, all P1) to enable independent implementation and testing. FR-012 (WarnSignal standardization + proto reconciliation) is cross-cutting and lives in the final phase.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1/US2/US3)
- Exact file paths included in every description

## Path Conventions

Two change surfaces: the agent service (`projects/game/agent/src/`) and the desktop frontend (`projects/game/desktop/frontend/src/`); large tests in Go (`projects/game/testplan/`); proto at `projects/game/game.proto`. Built with Bazel.

---

## Phase 1: Setup (Baseline)

**Purpose**: Confirm the existing tree builds and tests pass before changes; orient on the design docs.

### 文档清单（编码前必读 — Constitution V）

- **代码规范文档**: `style/javascript.md`（js_test 执行模型、DI/`vi.fn()` 可靠 mock 约定）及其引用的 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**: 无
- **技术文章**: [`survey/llm-stream-stall-recovery-revision.md`](../../survey/llm-stream-stall-recovery-revision.md) §1–§3（背景与生产证据）；[research.md](research.md)（R1–R8 决策摘要）

- [ ] T001 Verify baseline: run `bazel build //projects/game/agent/...` and `bazel test //projects/game/agent/src:all` (or the package test targets); confirm green. This is the regression baseline — all subsequent changes must keep it green.

---

## Phase 2: User Story 1 — Idle Detection Aligns With Industry Norms (Priority: P1) 🎯 MVP

**Goal**: Raise the chunk-idle default from the industry-outlier 30s to the industry-median 120s; enforce a 60s minimum; correct the inaccurate "15–30s consensus" references. This is the stop-the-bleeding fix — it removes the bulk of false stalls across ALL models. (spec FR-001.)

**Independent Test**: `STREAM_IDLE_TIMEOUT_MS === 120_000` with the env var unset; values `< 60_000` clamped; explicit env override honored. The existing `graph.test.ts:2684-2689` (`nodes[name].timeout?.idleTimeout === STREAM_IDLE_TIMEOUT_MS`) continues to pass unchanged.

### 文档清单（编码前必读 — Constitution V）

- **代码规范文档**: `style/javascript.md` + [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**: [LangChain PR #36949 — stream_chunk_timeout 120s default](https://github.com/langchain-ai/langchain/pull/36949)；[OpenClaw PR #93965 — 120s idle + 300s first-event](https://github.com/openclaw/openclaw/pull/93965)；[opencode PR #18264 — chunk timeout disabled by default（043 "15–30s" 引用不准确的反向证据）](https://github.com/anomalyco/opencode/pull/18264)；[openai/codex issue #23807 — stream_idle_timeout 默认 300s（行业宽松端，佐证 30s 是离群值）](https://github.com/openai/codex/issues/23807)；[langchainjs issue #9088 — JS 侧无客户端 chunk-idle 防护](https://github.com/langchain-ai/langchainjs/issues/9088)（继续依赖 LangGraph `idleTimeout` 的论据）
- **技术文章**: [survey](../../survey/llm-stream-stall-recovery-revision.md) §4.1/§4.5/§5.1/§5.2/§6.1；[research.md](research.md) R1；[contracts/idle-timeout-contract.md](contracts/idle-timeout-contract.md) §1；[043 spec](../043-llm-stream-stall-recovery/spec.md) FR-001（被修订项）

### Implementation for User Story 1

- [ ] T002 [US1] In `projects/game/agent/src/llm.ts:43-44`: change `STREAM_IDLE_TIMEOUT_MS` default `30_000` → `120_000`; add a 60s-minimum clamp (values `< 60_000` resolve to `120_000`) at the env-var read site; **export `STREAM_IDLE_TIMEOUT_EXPLICIT: boolean`（env 是否显式设置）——供 `resolveStreamIdleTimeout`（T003）区分"显式配置 as-is"与"默认+floor"（idle-timeout-contract.md §1 规则序 / FR-003）**; rewrite the doc-comment block (`:34-42`) to drop the "community 15–30s consensus" line and cite the accurate anchors above. Update `projects/game/agent/src/llm.test.ts`: assert default `=== 120_000` (env unset), `< 60_000` clamped, explicit env value honored as-is, `STREAM_IDLE_TIMEOUT_EXPLICIT` 标志正确（未设 → false；显式设置 → true）. Run `bazel test //projects/game/agent/src:llm_test`. (FR-001, FR-008 configurability.)

**Checkpoint**: US1 complete — the default is corrected; unit tests green; `graph_test` still green (unchanged assertion). This alone is a shippable MVP (dramatic false-positive reduction).

---

## Phase 3: User Story 2 — Reasoning Models Get Extended Thinking Tolerance (Priority: P1)

**Goal**: Add a per-reasoning-model idle-timeout floor so reasoning models (e.g. `deepseek-v4-flash`, ~65s to first content token) are no longer false-stalled during legitimate deep thinking. The floor follows Hermes's `max(default, floor)` semantics; explicit operator config always wins. (spec FR-002/FR-003.)

**Independent Test**: `getReasoningIdleTimeoutFloor("openai/deepseek-v4-flash") === 600_000`; `getReasoningIdleTimeoutFloor("gpt-4") === null`; longest-first matching. `buildTeamGraph({ playerModelSpec: "openai/deepseek-v4-flash", ... })` → `nodes.player.timeout.idleTimeout === 600_000`; with no spec → `=== STREAM_IDLE_TIMEOUT_MS`; with `GAME_STREAM_IDLE_TIMEOUT_MS=90000` 显式设置 + DeepSeek spec → `=== 90_000`（显式配置 as-is，即使低于 floor — FR-003/US2.3）.

### 文档清单（编码前必读 — Constitution V）

- **代码规范文档**: `style/javascript.md` + [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**: [Hermes `reasoning_timeouts.py` — per-reasoning-model stale-timeout floor（commit 27c486e）](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)；[Hermes issue #61461 — deepseek-v4-flash 首 content token ~65s 实测](https://github.com/NousResearch/hermes-agent/issues/61461)；[LangGraph `TimeoutPolicy`（node-level idleTimeout）](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/pregel/utils/timeout.ts)
- **技术文章**: [survey](../../survey/llm-stream-stall-recovery-revision.md) §4.3/§5.3/§6.2；[research.md](research.md) R2；[contracts/idle-timeout-contract.md](contracts/idle-timeout-contract.md) §1–§3（§1 分辨率规则是 T003 `resolveStreamIdleTimeout` 的实现依据，含显式 env 优先分支）；[data-model.md](data-model.md) §2
- **仓库内必读间接引用**: `projects/game/agent/src/model-provider.ts:26-32`（`parseModelSpec`，floor 匹配复用）；`projects/game/agent/src/team/graph.ts:177-189`（`TeamGraphDeps` 现状）与 `:383-389`（player/planner `addNode` 应用点）

### Implementation for User Story 2

- [ ] T003 [P] [US2] Create `projects/game/agent/src/reasoning-timeouts.ts`: export `REASONING_IDLE_TIMEOUT_FLOOR`（allowlist，值与 [data-model.md §2](data-model.md#2-reasoning-model-floor-allowlist-entity) 一致并在此冻结：`deepseek-r1`/`deepseek-reasoner`/`deepseek-v4-` → 600_000；`o1-`/`o3-` → 600_000；`o3-mini-`/`o4-mini-` → 300_000；`claude-opus-` → 240_000）, `getReasoningIdleTimeoutFloor(modelSpec): number | null` (strip `{provider}/` via `parseModelSpec` from `model-provider.ts`; lowercase; **longest-substring-first** match), and `resolveStreamIdleTimeout(modelSpec?): number` — **按 [idle-timeout-contract.md §1](contracts/idle-timeout-contract.md#1-resolution-rule) 规则序**：env 显式设置（`STREAM_IDLE_TIMEOUT_EXPLICIT`，T002）→ 返回 `STREAM_IDLE_TIMEOUT_MS` as-is（即使低于 floor — FR-003/US2.3）；否则匹配 floor → `max(STREAM_IDLE_TIMEOUT_MS, floor)`；否则 `STREAM_IDLE_TIMEOUT_MS`。**禁止写成裸 `max(env_or_default, floor)`** —— 那会把显式设置的低值（如 env=90s + DeepSeek 600s floor）抬到 600s，违反 FR-003。Add `projects/game/agent/src/reasoning-timeouts.test.ts`: matching/longest-first/null/**env-below-floor as-is** cases. Run `bazel test //projects/game/agent/src:reasoning_timeouts_test`. (FR-002, FR-003, FR-008 auditable table.)
- [ ] T004 [US2] Wire the floor into `projects/game/agent/src/team/graph.ts`: add **optional** `playerModelSpec?: string` / `plannerModelSpec?: string` to `TeamGraphDeps` (`:177-181`); in the builder compute `playerIdle = resolveStreamIdleTimeout(deps.playerModelSpec)` / `plannerIdle = resolveStreamIdleTimeout(deps.plannerModelSpec)` and pass them to `addNode("player", …, { timeout: { idleTimeout: playerIdle, refreshOn: "auto" } })` / `addNode("planner", …)` at `:383-389` (replacing the raw `STREAM_IDLE_TIMEOUT_MS`). Extend `projects/game/agent/src/team/graph.test.ts:2684-2689`: assert `nodes.player.timeout.idleTimeout === 600_000` when `playerModelSpec` is a DeepSeek model; `=== STREAM_IDLE_TIMEOUT_MS` when spec omitted (backward compat for the ~30 existing call sites); with `GAME_STREAM_IDLE_TIMEOUT_MS` 显式设置为 floor 以下（如 90000）+ DeepSeek spec → `=== 90_000`（as-is — FR-003/US2.3）。 Wire production call sites `projects/game/agent/src/server.ts:260,335` to pass the profile's model specs (available via `prompt-client.ts` `playerModel`/`plannerModel`). Run `bazel test //projects/game/agent/src:graph_test`. (FR-002/FR-003 application point.)

**Checkpoint**: US1 + US2 complete — reasoning models no longer false-stall on deep thinking; non-reasoning models use the 120s default. Independently testable via the floor unit tests + the graph-node timeout assertion.

---

## Phase 4: User Story 3 — Streamed Output Survives a Stall and Reconnection (Priority: P1)

**Goal**: Persist the stalled node's already-streamed partial output to the checkpoint (with a per-block "interrupted" flag) so it survives reconnection and `ListMessages` returns it; render the interrupted indicator on the desktop after reconnect. (spec FR-004/FR-005/FR-006/FR-007/FR-013.)

**Independent Test**: mock stream yields blocks then rejects an idle `NodeTimeoutError{ node:"player" }` → `updateState` called with the merged AIMessage in `playerMessages`, error re-thrown; multi-node turn writes only the stalled channel. `ListMessages` returns the partial output with the interrupted marker. Desktop renders the interrupted indicator on a flagged part.

### 文档清单（编码前必读 — Constitution V）

- **代码规范文档**: `style/javascript.md` + [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（DI/`vi.fn()` 注入 fake stream，禁止跨包 `vi.mock`）；`style/api.md` 及其引用的 [AIP-192 Documentation](https://google.aip.dev/192)（proto 注释质量 —— T008b 新增枚举/字段注释）；[AIP-126 Enumerations](https://google.aip.dev/126)（枚举命名/默认值约定）
- **官方文档**: [LangGraph `NodeTimeoutError`（`.node`/`.kind`/`.idleTimeout` 字段，`dist/errors.d.ts:103-125`）](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/errors.ts)；已安装 `node_modules/.pnpm/@langchain+langgraph@1.4.8_*/node_modules/@langchain/langgraph/dist/pregel/timeout.js:200-211`（`task.writes.splice` — partial 丢失根因）；[LangGraph `messagesStateReducer`（channel 追加/去重语义 — `libs/langgraph/src/channels/messages.ts`）](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/channels/messages.ts)
- **技术文章**: [survey](../../survey/llm-stream-stall-recovery-revision.md) §5.5/§6.4（output1 丢失根因与方案 A）；[research.md](research.md) R3/R4/R5/R6/R7；[contracts/partial-output-contract.md](contracts/partial-output-contract.md)（§1–§7 全文）；[contracts/desktop-rendering-contract.md](contracts/desktop-rendering-contract.md) §3；[data-model.md](data-model.md) §3/§4/§5
- **仓库内必读间接引用**: `projects/game/agent/src/session-team.ts:725-912`（`runTeamTurn`，改造点）与 `:1170`（`messageToContentBlocks`，逆函数参考）；`projects/game/agent/src/turn-loop.ts:91`（`TurnBlock` 类型）、`:330-392`（`runLoop` catch）、`:412-428`（`finishError`/`warnFrame`，**不改**）；`projects/game/agent/src/handler.ts:82-83`（`AGENT_CHANNELS`）、`:619-620`（`ListMessages` 读 checkpoint）、`:668-717`（content 重构）与 `:804-847`（`blockInterrupted`/`textPart`/`reasoningPart` helpers，T008b 改造点）；`projects/game/agent/src/llm.ts:428-435`（`additional_kwargs` 承载 `toolResultStatus` 的既有模式，checkpoint 层 interrupted flag 同模式）；[043 contracts/stall-recovery-contract.md](../043-llm-stream-stall-recovery/contracts/stall-recovery-contract.md)（不改的 043 契约）；`projects/game/game.proto:478-540`（`MessagePart`/`TextPart`/`ThinkingPart` —— T008b 在 `TextPart`/`ThinkingPart` 新增 `PartCompletion completion` 字段，标记的 wire 载体；`projects/game/proto_test.go:370-399` protojson 枚举序列化先例）

### Implementation for User Story 3

- [ ] T005 [US3] **Gating spike** (research.md R4 / partial-output-contract.md §5): in `projects/game/agent/src/session-team.test.ts`, build a graph with a `MemorySaver`, start `streamEvents`, trigger an idle `NodeTimeoutError`, call `graph.updateState(...)` in the catch, then `getState()` — assert the values are present (expected: succeeds — `updateState` is an independent checkpointer mutation, not bound to the aborted invocation). If it FAILS, implement the documented contingency (fresh graph interaction or direct checkpointer `.put`) and record the finding. **This MUST pass before T006/T007 proceed.**
- [ ] T006 [P] [US3] Add `mergePartialBlocks(blocks: TurnBlock[]): { aiMessage: AIMessage; toolMessages: ToolMessage[] }` in `projects/game/agent/src/session-team.ts` (next to `messageToContentBlocks` at `:1170`). Rules per [contracts/partial-output-contract.md §3](contracts/partial-output-contract.md#3-merge-rules--turnblock--messages): text blocks → one `{type:"text"}` content block (concatenated); reasoning blocks → one `{type:"reasoning"}`; the **interrupted tail block** (last block overall) carries `additional_kwargs.interrupted = true`; `tool_call` with a retained `tool_result` → AIMessage `tool_calls[]`; `tool_call` without a `tool_result` → dropped (FR-006); `tool_result` blocks → standalone `ToolMessage`s. Unit-test all branches (text+reasoning; reasoning-only; tool_call+result kept; tool_call-without-result dropped; empty → both empty). (FR-005, FR-006.)
- [ ] T007 [US3] In `runTeamTurn` (`projects/game/agent/src/session-team.ts:725-912`): maintain `const partialBlocks: TurnBlock[] = []` and push a shallow clone `{ agent, block: { ...block } }` before each `yield`; wrap the `for await` loop AND the trailing `await stream.output` (`:911`) in `try { … } catch (err) { if (isNodeTimeoutError(err) && err.kind === "idle") await this.persistPartialOutput(err, partialBlocks); throw err; }`. Implement `persistPartialOutput(err, blocks)`: normalize `stalledAgent = agentFromNamespace(err.node)`; filter `blocks` to `b.agent === stalledAgent` (research.md R7 — avoid duplicating prior completed nodes' checkpointed output); `channel = stalledAgent === "player" ? "playerMessages" : "plannerMessages"`; merge via `mergePartialBlocks`; no-op if both empty (US3.4); else `await this.graphHandle.graph.updateState({ configurable: { thread_id: this.sessionId } }, { [channel]: [aiMessage, ...toolMessages] })`. Unit-test in `session-team.test.ts`: mock stream yields N blocks then rejects idle `NodeTimeoutError{node:"player"}` → `updateState` called with merged AIMessage in `playerMessages`, error re-thrown; multi-node turn (player complete → planner stall) → writes only `plannerMessages`; `turn_loop_test` confirms `finishError` still warn+wait + retained buffer (FR-010 unchanged). Run `bazel test //projects/game/agent/src:session_team_test //projects/game/agent/src:turn_loop_test`. (FR-004, FR-005, FR-006, FR-007, FR-011.)
- [ ] T008a [P] [US3] **Proto field** (FR-010 controlled exception — see plan.md "FR-010 controlled exception"): in `projects/game/game.proto`, add the `PartCompletion` enum (values `PART_COMPLETION_UNSPECIFIED = 0`, `PART_COMPLETION_INTERRUPTED = 1`) and a `PartCompletion completion = 2;` field to BOTH `TextPart` (after `string content = 1;`) and `ThinkingPart` (after its `string content = 1;`). Write AIP-192-compliant comments (third-person present tense, explain when `INTERRUPTED` is set — the tail content block of a stall-persisted partial, spec 044 FR-005 — and that the default `UNSPECIFIED` means a complete part, omitted by protojson). **MUST run before T008b/T009** so the generated types carry the new field. Proto definition draft (verbatim from [data-model.md §4.2](data-model.md#42-wire-layer-carrier--partcompletion-proto-enum-field-cross-network) / [desktop-rendering-contract.md §3](contracts/desktop-rendering-contract.md#3-fr-013--interrupted-indicator-render-after-reconnect)). After editing: regenerate — `bazel build //projects/game:game_go_proto` (Go; gazelle picks up no BUILD change — field is within an existing message) and the agent `game_types` regenerates on next build. Run `bazel build //projects/game/...` to confirm both Go and TS proto regenerate cleanly. (FR-005 wire carrier, FR-013.)
- [ ] T008b [P] [US3] **Translate checkpoint→wire** in `projects/game/agent/src/handler.ts:804-847`: rewrite the `textPart`/`reasoningPart` helpers to **set the proto `completion` field** instead of the prior `as unknown as MessagePart` loose-JSON cast (which could not cross the network — every hop strips undeclared fields). Keep `blockInterrupted(block)` (it reads the checkpoint-layer `additional_kwargs.interrupted` — the input). Change: when a contributing block carries `additional_kwargs.interrupted`, emit `{ text: { content, completion: "PART_COMPLETION_INTERRUPTED" } }` (text) / `{ thinking: { content, completion: "PART_COMPLETION_INTERRUPTED" } }` (reasoning) as a **typed** `MessagePart` (the generated `game_types` now carries `completion` on `TextPart`/`ThinkingPart` — remove the `as unknown as MessagePart` cast and the loose-JSON comment at `:823-828`). A normal complete AIMessage emits no `completion` (default UNSPECIFIED). Update `projects/game/agent/src/handler.test.ts` (`Handler.ListMessages` at `:1368`): assert a checkpointed AIMessage with an interrupted tail block → the returned part's `completion === "PART_COMPLETION_INTERRUPTED"` (or enum); a normal AIMessage → `completion` absent/`UNSPECIFIED`. Run `bazel test //projects/game/agent/src:handler_test`. (FR-005 translation, SC-003.)
- [ ] T009 [P] [US3] Desktop: (1) in `projects/game/desktop/frontend/src/api.ts`, add the `PartCompletion` TS enum (`UNSPECIFIED = 0`, `INTERRUPTED = 1` — mirrors `ToolResultStatus` at `:90-95`) and `completion?: PartCompletion | string` on BOTH `TextPart` (`:111`) and `ThinkingPart` (`:115`); add a pure helper `partInterrupted(part: MessagePart): boolean` (alongside `classifyToolResultStatus` at `:256`) that returns `true` iff the active text/thinking variant carries `completion === INTERRUPTED` (accept both the enum-name string `"PART_COMPLETION_INTERRUPTED"` and the numeric form, mirroring `classifyToolResultStatus`'s defensive dual-form — protojson emits the name string by default but may emit the integer). (2) In `projects/game/desktop/frontend/src/components/ChatView.svelte:284-292` (agent text bubble): when `partInterrupted(item.part)` is true, render a "中断" indicator (a small badge/suffix inside the `.agent-bubble`, reusing the `.warn-bubble` amber ⚠ visual language). (3) In `projects/game/desktop/frontend/src/components/ChatMessage.svelte:104-115` (thinking bubble): same indicator on an interrupted thinking part. (4) Unit-test `partInterrupted` in a new `projects/game/desktop/frontend/src/api.test.ts` (or extend an existing `.ts` test — the `lib_test` target globs `src/**/*.ts` only, so this is a pure-function test, NOT a Svelte component mount): interrupted text → true; interrupted thinking → true; normal text/thinking → false; numeric enum form → true; absent field → false. Run `bazel test //projects/game/desktop/frontend:lib_test` + `bazel build //projects/game/desktop/frontend:dist`（构建验证）. (FR-013, SC-003.)

**Checkpoint**: US3 complete — partial output survives reconnect; the interrupted block is marked end-to-end (checkpoint → ListMessages → desktop). 043's `finishError`/abort/buffer-retention behaviors unchanged (regression-verified).

---

## Phase 5: Cross-Cutting (WarnSignal Standardization + Proto) & Large-Test Acceptance

**Purpose**: FR-012 (formalize the existing ⚠ WarnSignal bubble rendering + reconcile the proto comment) and the Constitution-VI large-test acceptance for the feature.

### 文档清单（编码前必读 — Constitution V）

- **代码规范文档**: `style/large_test.md`（按模块组织、每 SUT 一份测试计划、`guitar run` 闭环、`pkg/testtool`、禁止按需求编号建文件/计划）；`style/golang.md`（大型测试用 Go，遵循其单元测试规范：命名、表驱动、given/when/then、helper 复用）；`style/api.md` 及其引用的 [AIP-192 Documentation](https://google.aip.dev/192)（proto 注释质量）
- **官方文档**: 无
- **技术文章**: [survey](../../survey/llm-stream-stall-recovery-revision.md) §7.2（验证门禁）；[research.md](research.md) R4/R7/R8；[contracts/desktop-rendering-contract.md](contracts/desktop-rendering-contract.md) §1/§2（WarnSignal ⚠ 气泡 + proto 调和）；[quickstart.md](quickstart.md)（A4/B6/C3 验收场景）
- **仓库内必读间接引用**: `.opencode/skills/testplan/SKILL.md`（guitar 工具用法：`guitar validate` / `guitar run <plan.yaml>` 部署→测试→清理闭环）；`projects/game/testplan/system_test.yaml`（**agent-stall suite 所在的主测试计划**，T011/T012 的接线与执行目标；`deploy_agent_stall.yaml` 只是其 deploy 工件、无可运行 suites）；`projects/game/testplan/agent_stall_test.go`（既有 stall 大型测试模块，本特性的 case 加在此；FR-001 后需随 60s 新下限重基线时序）；`projects/game/testplan/deploy_agent_stall.yaml`（既有 stall 部署，env `15000` → `60000`，**禁止新建 YAML**）；`projects/game/testplan/helpers_test.go`（`wsReadTimeout` 于 `:35`，随窗口重基线）、`saolei_fixtures_test.go`（共享 helper，复用不复制）；`projects/game/game.proto:445-465`（content-model 注释，T010 调和点 —— **与 T008a 的 `TextPart`/`ThinkingPart` `:532-540` 是不同区域**，不要混淆）；`projects/game/game.proto:532-540` + `projects/game/proto_test.go:370-399`（T008a 的 `PartCompletion` 字段与 protojson 枚举序列化先例，case (c) 断言依据）

- [ ] T010 [P] FR-012: reconcile the proto comment at `projects/game/game.proto:451-453` (and the `FlowPart` comment at `:488-497`) to document `WarnSignal` (`warn`) as the **rendered exception** — a control signal surfaced by the desktop as a distinct ⚠ warning bubble; all other FlowPart kinds remain consumed, not displayed (per [contracts/desktop-rendering-contract.md §2](contracts/desktop-rendering-contract.md#2-fr-012--warnsignal--bubble-standardize--reconcile)). Comment-only change — no wire-format change, **distinct from T008a's `PartCompletion` field addition** (different proto region: `:451-453`/`:488-497` FlowPart comment vs `:532-540` TextPart/ThinkingPart; serialize the two edits to the same file to avoid conflicts). Verify the desktop already renders every `fp.warn` as a ⚠ bubble (`projects/game/desktop/frontend/src/App.svelte:789-802`, `components/ChatView.svelte:271-279`) so no agent emission change is needed.
- [ ] T011 Large-test cases (by module, per `style/large_test.md`): extend `projects/game/testplan/agent_stall_test.go` (the stall module) with focused cases — (a) **US2**: a reasoning model's deep-thinking phase (silent beyond the old 30s but within its floor) does NOT raise a stall; (b) **US1 regression**: a genuine silent dropout is still detected within the configured window (deploy env 60s, FR-001); (c) **US3**: stall mid-reply → re-enter the session → `ListMessages` returns the partial output with the interrupted marker. **Case (c) assertion** (now that interrupted is a formal proto field): assert the partial reply's tail content part carries `completion == game.PartCompletion_PART_COMPLETION_INTERRUPTED` — read via `part.GetText().GetCompletion()` (text) or `part.GetThinking().GetCompletion()` (reasoning) on the `ListMessages` response `Message.content.parts`. A normal (non-interrupted) part has `completion == PART_COMPLETION_UNSPECIFIED` (the protojson default — field absent). **重基线 stall suite 到 FR-001 的 60s 新下限**（现部署 env `15000` 低于新下限，会被 T002 的钳制静默改成 120s，破坏既有用例时序）：`projects/game/testplan/deploy_agent_stall.yaml` env `GAME_STREAM_IDLE_TIMEOUT_MS: "15000"` → `"60000"`（并更新 `desc`）；相应调整 `agent_stall_test.go` 时序假设 —— `stallToolReplyDelay` 20s → >60s（如 65s，保持 heartbeat 验证有效）、`helpers_test.go:35` `wsReadTimeout` 30s 与 `drainUntilWait` 预算 → >60s（如 75s），并刷新 `agent_stall_test.go`/`system_test.yaml` suite 11 描述中的 15s 窗口注释。用例经既有 `system_test.yaml` 的 **agent-stall suite**（其 `deploy` 即 `deploy_agent_stall.yaml`）运行——不要新建 per-feature YAML（`style/large_test.md` §测试计划数量/§反模式），也不要把 suite/case 写进 deploy-only 的 `deploy_agent_stall.yaml`（它没有 `suites` 段）。Reuse `helpers_test.go`/`saolei_fixtures_test.go`; follow `style/golang.md`. Run `bazel build //projects/game/testplan:...` on the test target.
- [ ] T012 Large-test acceptance (Constitution VI): execute the testplan via the testplan skill — full deploy→test→cleanup loop: `guitar validate projects/game/testplan/system_test.yaml && guitar run projects/game/testplan/system_test.yaml`（agent-stall suite 覆盖本特性全部大型用例；`deploy_agent_stall.yaml` 是 suite 的 deploy 工件、无可运行 suites，**不得**直接作为 `guitar run` 目标）。**All cases MUST pass** (any failed/flaky case = acceptance not met — fix and re-run until fully green). Build-only is explicitly NOT acceptance.
- [ ] T013 Housekeeping: regenerate proto code (T008a added a field — the Go `game_go_proto` and agent `game_types` regenerate on `bazel build`, no BUILD.bazel change needed since the field is within an existing message; run `bazel build //projects/game:game_go_proto //projects/game/agent:game_types` to confirm); `bazel run //:gazelle` in `projects/game/agent/src` (new `reasoning-timeouts.ts` → `BUILD.bazel` target); if Go test files changed, `bazel run //:go -- fmt <files>` + `bazel run //:go -- mod tidy -v` + gazelle for the testplan; `bazel mod tidy`; final `bazel build //...` and `bazel test //projects/game/agent/...` (+ desktop build) to confirm the whole tree is green.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: no dependencies — start immediately; establishes the green baseline.
- **Phase 2 (US1)**: depends on T001. Single constant change; shippable as MVP.
- **Phase 3 (US2)**: depends on T001 (T002 NOT required — US2's `resolveStreamIdleTimeout` reads `STREAM_IDLE_TIMEOUT_MS`, works with either 30s or 120s default; but logically sequence after US1 so the floor composes with the corrected default).
- **Phase 4 (US3)**: depends on T001. **T005 (spike) gates T006/T007** within the phase; **T008a (proto field) gates T008b (handler.ts) and T009 (frontend)**. US3 is independent of US1/US2 (different files: `game.proto`/`session-team.ts`/`handler.ts`/desktop vs `llm.ts`/`graph.ts`).
- **Phase 5**: T010 edits `game.proto:451-453` (FlowPart comment) — serialize with T008a (same file, different region); T011 depends on US1+US2+US3 being implemented (it validates all three); T012 depends on T011; T013 is final.

### User Story Dependencies

- **US1 (P1)**: after Setup — no dependencies on other stories. **MVP.**
- **US2 (P1)**: after Setup — independent of US1 at the code level (different files), but sequenced after US1 so the floor composes with the corrected default. Independently testable.
- **US3 (P1)**: after Setup — independent of US1/US2 (different files). T005 spike gates the persistence tasks. Independently testable.

### Within Each User Story

- Read all phase docs first (Constitution V).
- Foundational/helper modules before their consumers (T003 before T004; T006 before T007; **T008a before T008b** — the proto field must be generated before handler.ts can set the typed `completion` field).
- The gating spike (T005) before the persistence implementation that depends on it (T007).
- Unit tests are written inline with each implementation task (Constitution IV) — no separate test tasks.
- Run `bazel build` + `bazel test` on every change (Constitution IV — part of the dev task).

### Parallel Opportunities

- T003 ∥ T006 ∥ T008a ∥ T010 — all different files (T008a edits `game.proto`; T010 also edits `game.proto` but a different region — `game.proto:451-453` FlowPart comment vs `:533-540` TextPart/ThinkingPart fields; serialize these two edits to avoid conflicting writes). **T008b depends on T008a** (needs the regenerated `completion` field on the typed `MessagePart`); **T009 depends on T008a** (needs the proto field to exist before the frontend can read it end-to-end). Note T006 should land before T007 consumes it, but T006 authoring is parallel with T008a/T009/T010.
- US1 (T002) and US3 (T005–T009) touch disjoint files and can proceed in parallel once Setup is green.
- The large-test case authoring (T011) can start once the underlying stories are code-complete.

---

## Parallel Example

```text
# After Phase 1 (Setup) is green, these can proceed in parallel (different files):
Task: "T002 [US1] revise STREAM_IDLE_TIMEOUT_MS in projects/game/agent/src/llm.ts"
Task: "T003 [P] [US2] create projects/game/agent/src/reasoning-timeouts.ts"
Task: "T005 [US3] gating spike in projects/game/agent/src/session-team.test.ts"
Task: "T010 [P] FR-012 proto comment in projects/game/game.proto"

# Within US3, after T005 passes:
Task: "T006 [P] [US3] mergePartialBlocks in session-team.ts"
Task: "T008a [P] [US3] PartCompletion proto field in game.proto"
Task: "T009 [P] [US3] desktop interrupted indicator in api.ts/ChatView/ChatMessage"
# after T008a regenerates the typed MessagePart:
Task: "T008b [P] [US3] translate checkpoint→wire completion field in handler.ts"
# then T007 (consumes T006) sequentially.
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Complete Phase 1 (Setup — baseline green).
2. Complete Phase 2 (US1 — `STREAM_IDLE_TIMEOUT_MS` → 120s).
3. **STOP and VALIDATE**: `bazel test //projects/game/agent/src:llm_test` green; the default is corrected. This alone dramatically reduces false stalls across all models — shippable.

### Incremental Delivery

1. Setup → baseline green.
2. + US1 → 120s default (MVP — stop-the-bleeding).
3. + US2 → reasoning floor (fixes `deepseek-v4-flash` / saolei specifically).
4. + US3 → partial output survives reconnect (data-integrity fix).
5. Phase 5 → WarnSignal standardization + proto + **large-test acceptance** (Constitution VI — feature not accepted until `guitar run` is fully green).

### Notes

- **No retry/fallback** (FR-009) — explicitly out of scope; do not add retry logic anywhere.
- **Do not modify 043 behaviors** (FR-010): `turn-loop.ts` `finishError`/`finishAbort`, `withIdleHeartbeat`, `INIT_TURN_TIMEOUT_MS`, the `NodeTimeoutError` re-throw in `player.ts`/`planner.ts` — all unchanged; regression tests guard them.
- **Large tests by module**: cases go in the existing `agent_stall_test.go` (stall module), run through the existing `system_test.yaml` `agent-stall` suite (deploy: `deploy_agent_stall.yaml`) — never a new per-feature YAML (`style/large_test.md` §反模式).
- **Commit** after each task or logical group; keep the baseline green at every commit.
