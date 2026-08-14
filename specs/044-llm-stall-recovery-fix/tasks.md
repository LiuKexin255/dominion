# Tasks: LLM Stream Stall Recovery — Timeout Tuning & Partial Output Persistence

**Input**: Design documents from `/specs/044-llm-stall-recovery-fix/`

**Prerequisites**: [plan.md](plan.md) (required), [spec.md](spec.md) (required), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md), [large-test-status.md](large-test-status.md)

**Tests**: Per Constitution IV, unit tests are part of each implementation task (not separate tasks). Large-test cases (Go, by module) + `guitar run` execution are separate acceptance tasks (Constitution VI).

**Organization**: Tasks are grouped by user story (spec.md US1/US2/US3, all P1). FR-012 (WarnSignal standardization + proto reconciliation) is cross-cutting and lives in Phase 5.

**2026-08-14 amendment**: implementation Phases 1–4 + T010 are **DONE** (marked `[X]` with commits; record in [large-test-status.md](large-test-status.md) §1). The resume scope ([plan.md](plan.md) "Update 2026-08-14 — Resume Scope") adds **Phase 5A** (service-config channel, goal 2) as new tasks **T014–T017**, and rewrites the uncompleted Phase 5 tasks: **T011's 60s re-baseline is SUPERSEDED by config-driven 5s/2s timings** (the fd81521 code exists and is rescaled, not redone); T012 gains the R9 contingency; T013 gains the config-channel housekeeping; T018/T019 are split-out [P] metadata/testdata edits (T019 additionally syncs the 046 store-level pin assertion — see its 2026-08-14 amendment note); T020 is the OPTIONAL γ case gated on the SC-005 ruling. **Task IDs T011–T013 are preserved for cross-reference stability; execution order is T014–T017 → T011/T018/T019 → T012 → T013 (T020 optional)** — see Dependencies.

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

- [x] T001 Verify baseline: run `bazel build //projects/game/agent/...` and `bazel test //projects/game/agent/src:all` (or the package test targets); confirm green. This is the regression baseline — all subsequent changes must keep it green. **(done — verification only; baseline held green through all phases)**

---

## Phase 2: User Story 1 — Idle Detection Aligns With Industry Norms (Priority: P1) 🎯 MVP

**Goal**: Raise the chunk-idle default from the industry-outlier 30s to the industry-median 120s; enforce a 60s minimum; correct the inaccurate "15–30s consensus" references. This is the stop-the-bleeding fix — it removes the bulk of false stalls across ALL models. (spec FR-001.)

**Independent Test**: `STREAM_IDLE_TIMEOUT_MS === 120_000` with the env var unset; values `< 60_000` clamped; explicit env override honored. The existing `graph.test.ts:2684-2689` (`nodes[name].timeout?.idleTimeout === STREAM_IDLE_TIMEOUT_MS`) continues to pass unchanged.

### 文档清单（编码前必读 — Constitution V）

- **代码规范文档**: `style/javascript.md` + [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（`style/javascript.md` Mock 约定引用的外部规范：DI 优先、禁止模块级 `vi.mock` 的依据）
- **官方文档**: [LangChain PR #36949 — stream_chunk_timeout 120s default](https://github.com/langchain-ai/langchain/pull/36949)；[OpenClaw PR #93965 — 120s idle + 300s first-event](https://github.com/openclaw/openclaw/pull/93965)；[opencode PR #18264 — chunk timeout disabled by default（043 "15–30s" 引用不准确的反向证据）](https://github.com/anomalyco/opencode/pull/18264)；[openai/codex issue #23807 — stream_idle_timeout 默认 300s（行业宽松端，佐证 30s 是离群值）](https://github.com/openai/codex/issues/23807)；[langchainjs issue #9088 — JS 侧无客户端 chunk-idle 防护](https://github.com/langchain-ai/langchainjs/issues/9088)（继续依赖 LangGraph `idleTimeout` 的论据）
- **技术文章**: [survey](../../survey/llm-stream-stall-recovery-revision.md) §4.1/§4.5/§5.1/§5.2/§6.1；[research.md](research.md) R1；[contracts/idle-timeout-contract.md](contracts/idle-timeout-contract.md) §1；[043 spec](../043-llm-stream-stall-recovery/spec.md) FR-001（被修订项）

### Implementation for User Story 1

- [x] T002 [US1] In `projects/game/agent/src/llm.ts:43-44`: change `STREAM_IDLE_TIMEOUT_MS` default `30_000` → `120_000`; add a 60s-minimum clamp (values `< 60_000` resolve to `120_000`) at the env-var read site; export `STREAM_IDLE_TIMEOUT_EXPLICIT: boolean`; rewrite the doc-comment block to drop the "community 15–30s consensus" line and cite the accurate anchors. Unit tests in `projects/game/agent/src/llm.test.ts` (default/clamp/explicit/flag). `bazel test //projects/game/agent/src:llm_test`. (FR-001, FR-008.) **(done — commit `f953e9a`)**

**Checkpoint**: US1 complete — the default is corrected; unit tests green. Shippable MVP.

---

## Phase 3: User Story 2 — Reasoning Models Get Extended Thinking Tolerance (Priority: P1)

**Goal**: Add a per-reasoning-model idle-timeout floor so reasoning models (e.g., `deepseek-v4-flash`, ~65s to first content token) are no longer false-stalled during legitimate deep thinking. (spec FR-002/FR-003.)

**Independent Test**: `getReasoningIdleTimeoutFloor("openai/deepseek-v4-flash") === 600_000`; `getReasoningIdleTimeoutFloor("gpt-4") === null`; longest-first matching; env-explicit-below-floor as-is.

### 文档清单（编码前必读 — Constitution V）

- **代码规范文档**: `style/javascript.md` + [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（`style/javascript.md` Mock 约定引用的外部规范）
- **官方文档**: [Hermes `reasoning_timeouts.py` — per-reasoning-model stale-timeout floor（commit 27c486e）](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)；[Hermes issue #61461 — deepseek-v4-flash 首 content token ~65s 实测](https://github.com/NousResearch/hermes-agent/issues/61461)；[LangGraph `TimeoutPolicy`（node-level idleTimeout）](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/pregel/utils/timeout.ts)
- **技术文章**: [survey](../../survey/llm-stream-stall-recovery-revision.md) §4.3/§5.3/§6.2；[research.md](research.md) R2；[contracts/idle-timeout-contract.md](contracts/idle-timeout-contract.md) §1–§3；[data-model.md](data-model.md) §2
- **仓库内必读间接引用**: `projects/game/agent/src/model-provider.ts:26-32`（`parseModelSpec`）；`projects/game/agent/src/team/graph.ts:177-189` 与 `:383-389`（应用点）

### Implementation for User Story 2

- [x] T003 [P] [US2] Create `projects/game/agent/src/reasoning-timeouts.ts`: `REASONING_IDLE_TIMEOUT_FLOOR` allowlist + `getReasoningIdleTimeoutFloor()` (longest-substring-first) + `resolveStreamIdleTimeout()` per [idle-timeout-contract.md §1](contracts/idle-timeout-contract.md#1-resolution-rule) 规则序（显式 as-is 分支在前，禁止裸 `max(env_or_default, floor)`）。Unit tests in `projects/game/agent/src/reasoning-timeouts.test.ts`. `bazel test //projects/game/agent/src:reasoning_timeouts_test`. (FR-002, FR-003.) **(done — commit `8cf212e`)**
- [x] T004 [US2] Wire the floor into `projects/game/agent/src/team/graph.ts`: optional `playerModelSpec`/`plannerModelSpec` on `TeamGraphDeps`; `resolveStreamIdleTimeout` at the player/planner `addNode` sites; extend `graph.test.ts` (floor applied / spec omitted / env-explicit-below-floor as-is); wire production call sites `projects/game/agent/src/server.ts:260,335`. `bazel test //projects/game/agent/src:graph_test`. (FR-002/FR-003.) **(done — commit `8cf212e`)**

**Checkpoint**: US1 + US2 complete at the unit layer.

---

## Phase 4: User Story 3 — Streamed Output Survives a Stall and Reconnection (Priority: P1)

**Goal**: Persist the stalled node's already-streamed partial output to the checkpoint (with a per-block "interrupted" flag) so it survives reconnection and `ListMessages` returns it; render the interrupted indicator on the desktop after reconnect. (spec FR-004–FR-007/FR-013.)

### 文档清单（编码前必读 — Constitution V）

- **代码规范文档**: `style/javascript.md` + [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（`style/javascript.md` Mock 约定引用的外部规范）；`style/api.md` 及其引用的 [AIP-192 Documentation](https://google.aip.dev/192)、[AIP-126 Enumerations](https://google.aip.dev/126)
- **官方文档**: [LangGraph `NodeTimeoutError`](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/errors.ts)；已安装 `node_modules/@langchain/langgraph/dist/pregel/timeout.js:200-211`；[LangGraph `messagesStateReducer`](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/channels/messages.ts)
- **技术文章**: [survey](../../survey/llm-stream-stall-recovery-revision.md) §5.5/§6.4；[research.md](research.md) R3–R7；[contracts/partial-output-contract.md](contracts/partial-output-contract.md)；[contracts/desktop-rendering-contract.md](contracts/desktop-rendering-contract.md) §3；[data-model.md](data-model.md) §3–§5
- **仓库内必读间接引用**: `projects/game/agent/src/session-team.ts:725-912` 与 `:1170`；`projects/game/agent/src/turn-loop.ts`；`projects/game/agent/src/handler.ts`（ListMessages 读路径与 helpers）；`projects/game/agent/src/llm.ts:428-435`；[043 contracts/stall-recovery-contract.md](../043-llm-stream-stall-recovery/contracts/stall-recovery-contract.md)；`projects/game/game.proto:478-540`；`projects/game/proto_test.go:370-399`

### Implementation for User Story 3

- [x] T005 [US3] **Gating spike** (research.md R4): `graph.updateState` succeeds after the stall's AbortSignal fired — validated in `projects/game/agent/src/session-team.test.ts`. **(done — spike passed as expected; updateState is an independent checkpointer mutation)** **(commit `1c13b4e`)**
- [x] T006 [P] [US3] Add `mergePartialBlocks` in `projects/game/agent/src/session-team.ts` per [partial-output-contract.md §3](contracts/partial-output-contract.md)（text/reasoning 合并、tail interrupted 标记、tool_call+result 保留、无 result 的 tool_call 丢弃）。Unit tests all branches. (FR-005, FR-006.) **(done — commit `1c13b4e`)**
- [x] T007 [US3] In `runTeamTurn` (`projects/game/agent/src/session-team.ts:725-912`): accumulate `partialBlocks`, catch idle `NodeTimeoutError`, `persistPartialOutput`（按 `err.node` 分区、写对应 channel、merge 后 `updateState`、re-throw）。Unit tests: stalled-node channel write / multi-node partition / `finishError` unchanged. `bazel test //projects/game/agent/src:session_team_test //projects/game/agent/src:turn_loop_test`. (FR-004–FR-007, FR-011.) **(done — commit `1c13b4e`)**
- [x] T008a [P] [US3] **Proto field** (FR-010 controlled exception — see plan.md): `PartCompletion` enum + `completion = 2` on `TextPart`/`ThinkingPart` in `projects/game/game.proto`, AIP-192 comments; regenerate Go + TS protos; `bazel build //projects/game/...`. (FR-005 wire carrier, FR-013.) **(done — commit `3531228`)**
- [x] T008b [P] [US3] Translate checkpoint→wire in `projects/game/agent/src/handler.ts` ListMessages helpers: typed `completion` field instead of the loose-JSON cast; `handler.test.ts` asserts `PART_COMPLETION_INTERRUPTED` on the interrupted tail and absence on normal parts. `bazel test //projects/game/agent/src:handler_test`. (FR-005, SC-003.) **(done — commit `3531228`)**
- [x] T009 [P] [US3] Desktop: `PartCompletion` TS enum + `completion?` on `TextPart`/`ThinkingPart` + pure `partInterrupted()` in `projects/game/desktop/frontend/src/api.ts`; interrupted indicator render in `components/ChatView.svelte`（agent text）与 `components/ChatMessage.svelte`（thinking）; unit tests in `api.test.ts`. `bazel test //projects/game/desktop/frontend:lib_test` + `bazel build //projects/game/desktop/frontend:dist`. (FR-013, SC-003.) **(done — commit `3531228`)**

**Checkpoint**: US3 complete — partial output survives reconnect end-to-end (checkpoint → ListMessages → desktop).

---

## Phase 5A (2026-08-14 resume): Service-Config Channel & Heartbeat Observability (goal 2)

**Purpose**: Move the agent's timeout parameters onto the 045 service-config channel so a deploy can select a fast, controlled configuration ([plan.md](plan.md) "Update 2026-08-14" item 1; [contracts/idle-timeout-contract.md §5](contracts/idle-timeout-contract.md#5-service-config-channel-2026-08-14-amendment); [data-model.md §7](data-model.md#7-agent-timeout-config-entry-2026-08-14-amendment--service-config-channel); research.md R10). Also adds the per-tick heartbeat logging that discriminates the T012 false-stall root cause (research.md R9).

**Independent Test**: `resolveAgentTimeouts` matrix unit-green (quickstart D1/D2); agent builds and all existing unit tests stay green with `DOMINION_CONFIG_DIR` unset (defaults path unchanged); `deploy_agent_stall.yaml` validates with `configs: [agent_timeouts]`.

### 文档清单（编码前必读 — Constitution V）

- **代码规范文档**: `style/javascript.md`（DI/`vi.fn()` 注入 reader、`vi.resetModules` 动态 import 模式——`projects/game/agent/src/llm.test.ts:423-485` 是既有先例）+ [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（Mock 约定引用的外部规范）
- **官方文档**: 无
- **技术文章**: [045 contracts/sdk-js.md](../045-deploy-config/contracts/sdk-js.md)（`readConfig<T>` API、错误语义、深合并）；[045 contracts/runtime-contract.md](../045-deploy-config/contracts/runtime-contract.md) §1/§3（`DOMINION_CONFIG_DIR` 发现、`{block}/{key}` 布局、错误表）；[045 contracts/yaml-schema.md](../045-deploy-config/contracts/yaml-schema.md) §1/§2（service.yaml `configs` 声明 schema、deploy.yaml 按名选择）；[contracts/idle-timeout-contract.md](contracts/idle-timeout-contract.md) §1（修订后解析规则）/§5（config 通道全量契约）；[data-model.md](data-model.md) §7（字段/矩阵/absence 语义）；[research.md](research.md) R9（心跳可观测性）/R10（通道设计与被拒备选）；[quickstart.md](quickstart.md) Phase D（D1/D2/D4）
- **仓库内必读间接引用**: `common/js/config/src/index.ts`（SDK 实现——错误类型不区分 absent/malformed 的依据）；`experimental/ts/grpc_hello_world/package.json` + `experimental/ts/grpc_hello_world/BUILD.bazel`（**wiring 参照**：package.json `"@dominion/common-js-config": "workspace:*"`、ts_project deps `:node_modules/@dominion/common-js-config`、`artifact_pkg_js` `runtime_deps` 加 `//common/js/config:runtime_pkg`、**不加** npm_deps）；`projects/game/agent/BUILD.bazel`（`lib` ts_project、`server_pkg` 与 `server_pkg_test`——stall 部署用的是 **`cmd_image_test`/`server_pkg_test`**，两个 pkg 都要加 runtime_pkg）；`projects/game/agent/src/server.ts:37`（`@dominion/common-js-logs` `info` 导入先例）；`projects/game/agent/src/llm.ts`（常量定义处、`withIdleHeartbeat`）；`projects/game/agent/service.yaml`

### Implementation

- [ ] T014 Create `projects/game/agent/src/agent-timeouts.ts` + `projects/game/agent/src/agent-timeouts.test.ts` per [idle-timeout-contract.md §5](contracts/idle-timeout-contract.md#5-service-config-channel-2026-08-14-amendment) 与 [data-model.md §7](data-model.md#7-agent-timeout-config-entry-2026-08-14-amendment--service-config-channel): export `AgentTimeouts { streamIdleTimeoutMs; toolHeartbeatIntervalMs; initTurnTimeoutMs }`, `DEFAULT_AGENT_TIMEOUTS = { 120_000, 10_000, 120_000 }`, `AGENT_TIMEOUTS_CONFIG_BLOCK = "agent_timeouts"`, `AGENT_TIMEOUTS_CONFIG_KEY = "timeouts"`; `loadAgentTimeoutOverrides(reader = readConfig): Partial<AgentTimeouts> | undefined`（`try { reader(block, key, {}) } catch { return undefined }` —— SDK 对 unset-dir/未选中/不可解析抛同一 Error，统一按 absent 处理，契约 §5 记录了该取舍）；**纯函数** `resolveAgentTimeouts(env: { streamIdleTimeoutMs?: string; initTurnTimeoutMs?: string }, overrides?: Partial<AgentTimeouts>)` 按矩阵实现——idle: env set→`Number>=60_000 ? value : 120_000`（clamp 仅 env 层）> overrides.streamIdleTimeoutMs（finite && >0，**as-is 无 clamp**）> 120_000；heartbeat: overrides.toolHeartbeatIntervalMs（finite && >0）> 10_000（**无 env 通道**）；init: env `GAME_INIT_TURN_TIMEOUT_MS`（`Number(...) || 120_000`）> overrides.initTurnTimeoutMs > 120_000；校验：resolved heartbeat ≥ resolved idle → `throw`（错误信息含两者与来源提示，043 FR-003 不变量）；返回值附 `streamIdleExplicit: boolean`（env set OR overrides 提供 idle）。Unit tests（quickstart D1/D2 全矩阵）: env unset+无 overrides→默认; env idle "45000"→clamp 120s; env idle "90000"→as-is; overrides `{streamIdleTimeoutMs:5000}`→5000 as-is; env+overrides 同设→env 胜; overrides `{streamIdleTimeoutMs:5000}`（默认 heartbeat 10s）→throw; overrides 部分字段→其余默认; `streamIdleExplicit` 真值表; 注入 `vi.fn()` reader 抛错→`undefined`、返回条目→解析出的 Partial。Reader 一律参数注入（禁 `vi.mock`，style/javascript.md）。`bazel run //:gazelle projects/game/agent/src` 生成 target 后 `bazel test //projects/game/agent/src:agent_timeouts_test`。(FR-008 修订, research.md R10.)
- [ ] T015 Re-source `projects/game/agent/src/llm.ts` constants + heartbeat per-tick logs: import `resolveAgentTimeouts`/`loadAgentTimeoutOverrides` from `./agent-timeouts`; at module scope `const resolved = resolveAgentTimeouts({ streamIdleTimeoutMs: process.env.GAME_STREAM_IDLE_TIMEOUT_MS, initTurnTimeoutMs: process.env.GAME_INIT_TURN_TIMEOUT_MS }, loadAgentTimeoutOverrides())`; **保留全部导出名**（下游 `reasoning-timeouts.ts`/`team/graph.ts`/`session-team.ts` 不改）：`STREAM_IDLE_TIMEOUT_MS = resolved.streamIdleTimeoutMs`、`STREAM_IDLE_TIMEOUT_EXPLICIT = resolved.streamIdleExplicit`（现为 env **或** config 显式）、`INIT_TURN_TIMEOUT_MS = resolved.initTurnTimeoutMs`、`TOOL_HEARTBEAT_INTERVAL_MS = resolved.toolHeartbeatIntervalMs`；更新这四处的 doc-comment（引用 idle-timeout-contract §1/§5 的优先级与 env-scoped clamp）。`withIdleHeartbeat`（`llm.ts:328-348`）加可观测性（research.md R9 / quickstart D4）：`import { info } from "@dominion/common-js-logs"`；wrapper invoke 时 `info("tool heartbeat wrapper started", { tool: tool.name, intervalMs: TOOL_HEARTBEAT_INTERVAL_MS })`；每个 tick `info("tool heartbeat tick", { tool: tool.name, intervalMs: TOOL_HEARTBEAT_INTERVAL_MS, tick })`（tick 为递增序号）。验证 `projects/game/agent/src/llm.test.ts` 既有 `vi.resetModules` + 动态 import env 用例**不改即过**（单测环境 `DOMINION_CONFIG_DIR` 未设 → overrides undefined → 纯 env 路径），并补一条断言：env+DOMINION_CONFIG_DIR 均未设时四常量等于默认值。`bazel test //projects/game/agent/src:llm_test //projects/game/agent/src:reasoning_timeouts_test //projects/game/agent/src:graph_test`。(R9/R10.)
- [ ] T016 Declare + wire the config dependency: (1) `projects/game/agent/service.yaml` 顶层（`artifacts:` 之前）加 `configs:` 块——name `agent_timeouts`、data 条目 name `timeouts`、type `yaml`、value 为 `streamIdleTimeoutMs: 5000\ntoolHeartbeatIntervalMs: 2000`（测试档；注释说明仅 stall 部署选择、生产不选择即默认，schema 见 [045 yaml-schema.md §1](../045-deploy-config/contracts/yaml-schema.md#1-serviceyaml--顶层配置块声明)）。(2) `projects/game/agent/package.json` dependencies 加 `"@dominion/common-js-config": "workspace:*"`；`bazel run @pnpm -- --dir <仓库根绝对路径>/projects/game/agent -- up`（`--dir` 必须为绝对路径；本机检出于 `/mnt/code/dominion.worktrees/dominion1`，其他检出按其实际根路径替换）。(3) `projects/game/agent/BUILD.bazel`（gazelle 生成 node_modules target 后手工补 target 级改动）：`lib` ts_project deps 加 `:node_modules/@dominion/common-js-config`；`server_pkg` **与** `server_pkg_test`（stall 部署用 `cmd_image_test`）的 `runtime_deps` 各加 `"//common/js/config:runtime_pkg"`——完整对照 `experimental/ts/grpc_hello_world/BUILD.bazel`（其 `artifact_pkg_js` **不加** npm_deps，runtime_pkg 已覆盖运行时打包）。(4) `bazel run //:gazelle projects/game/agent` + `bazel mod tidy` + `bazel build //projects/game/agent/...` + `bazel test //projects/game/agent/...` 全绿。(FR-008 修订.)
- [ ] T017 Switch the stall deploy to config: in `projects/game/testplan/deploy_agent_stall.yaml` — 删除 `agent_test` artifact 的 `env: GAME_STREAM_IDLE_TIMEOUT_MS: "60000"` 块，改为 `configs: [- agent_timeouts]`（deploy.yaml 仅按名选择，schema 见 [045 yaml-schema.md §2](../045-deploy-config/contracts/yaml-schema.md#2-deployyaml--artifact-配置块选择)）；更新文件 `desc`（"idle timeout 60s" → "config-driven idle timeout 5s / heartbeat 2s"）。生产 `projects/game/deploy.yaml` 与标准套件 `deploy_agent.yaml` **不动**（不选择块 → 默认）。验证：`guitar validate projects/game/testplan/system_test.yaml`（config 选择在编译期校验——[045 spec FR-007](../045-deploy-config/spec.md)：拒绝引用未定义配置块名的部署，拒绝发生在提交环境变更之前）。

**Checkpoint**: config 通道端到端就绪（声明 → 选择 → SDK 读取 → 常量解析）；`DOMINION_CONFIG_DIR` 未设时行为与修订前逐字节一致（单测回归证明）。

---

## Phase 5: Cross-Cutting (WarnSignal Standardization + Proto) & Large-Test Acceptance

**Purpose**: FR-012（done, T010）与 Constitution-VI 大型测试验收。**T011 的 60s 重基线已被 config 驱动的 5s/2s 时序取代**（[plan.md](plan.md) "Update 2026-08-14" item 3 显式 supersedes fd81521 中的时序常量；`agent_stall_test.go` 已有代码是改造基础，不是从零重写）。

### 文档清单（编码前必读 — Constitution V）

- **代码规范文档**: `style/large_test.md`（按模块组织、每 SUT 一份测试计划、`guitar run` 闭环、`pkg/testtool`、禁止按需求编号建文件/计划）；`style/golang.md`（大型测试用 Go：命名、表驱动、given/when/then、helper 复用）及其引用的 [Google Go Style](https://google.github.io/styleguide/go/)（入口索引）、[Style Guide](https://google.github.io/styleguide/go/guide)（规范，所有作者与评审者必读）、[Style Decisions](https://google.github.io/styleguide/go/decisions)（规范，面向 readability mentor）、[Best Practices](https://google.github.io/styleguide/go/best-practices)（鼓励遵循）
- **官方文档**: 无
- **技术文章**: [research.md](research.md) R9（心跳根因与应急分支）/R11（SC-005 重估）；[contracts/desktop-rendering-contract.md](contracts/desktop-rendering-contract.md) §1/§2（T010 依据，已完成）；[quickstart.md](quickstart.md) Phase D（D3/D4 验收场景）/A4（US2 大型用例取消 + SC-005 注）；[046 contracts/template-config.md](../046-fake-llm-think-chunking/contracts/template-config.md) §3.3（`think-interrupt-gap` 有限 gap 模板语义：`chunk_delays[i]` 作用于发出 `reasoning_chunks[i+1]` 之前）
- **仓库内必读间接引用**: `.opencode/skills/testplan/SKILL.md`（`guitar validate` / `guitar run <plan.yaml>` 部署→测试→清理闭环）；`projects/game/testplan/system_test.yaml`（agent-stall suite 接线与 §11 注释块，T018 改）；`projects/game/testplan/README.md:205-228`（§6 执行说明与 agent-stall 部署段——T018 改 `:221-228`，2026-08-14 amendment 扩展）；`projects/game/testplan/BUILD.bazel:216-252`（`agent_stall_test` target 注释与 `size` 属性——T018 改，2026-08-14 amendment 扩展）；`projects/game/testplan/agent_stall_test.go`（fd81521 版为改造基础：常量在 `:63-82`、四个用例）；`projects/game/testplan/deploy_agent_stall.yaml`（T017 改后形态）；`projects/game/testplan/helpers_test.go:35-42`（`wsReadTimeout=75s` 是**读超时上限非 sleep**，5s 窗口下无需改——但需阅读确认）；`projects/game/fake-llm/service/testdata/stall_recovery.yaml`（`think-interrupt-gap`：chunks/keywords/delays，T019 改 delays）；`projects/game/fake-llm/service/message_store_test.go:789-845`（046 store-level pin 断言块——`:819-821` pin `think-interrupt-gap` 的 `["1s","90s"]`，T019 同步改 `["1s","15s"]`，2026-08-14 amendment）；`projects/game/game.proto:445-465`（T010 已完成的注释调和，勿重复改）

### Implementation

- [x] T010 [P] FR-012: reconcile the proto comment at `projects/game/game.proto:451-453` (and the `FlowPart` comment) to document `WarnSignal` as the **rendered exception**; comment-only; desktop rendering verified already present. (FR-012.) **(done — commit `166683e`)**
- [ ] T011 Rescale + extend the stall suite to the config-driven timings (supersedes the fd81521 60s baseline; [plan.md](plan.md) item 3): in `projects/game/testplan/agent_stall_test.go` — (1) 常量重标定：`stallWindow` 60s→`5 * time.Second`（= `agent_timeouts` 的 `streamIdleTimeoutMs`）、`stallDetectMin`/`stallDetectMax` 45s/115s→`3s`/`10s`（下界防回归到 120s 默认需 >5s 余量：120s 默认在 ~120s 触发，超上界 10s；旧 15s/30s/60s 窗口均超上界；配置未生效（默认 120s）亦超上界——config 通道回归即被抓）、`stallToolReplyDelay` 65s→`12 * time.Second`（≈2.4× 窗口，2s 心跳下 ≈6 个 tick——**精确复现 R9 失败模式**：需要“重复 tick 刷新”而非单 tick）；(2) 文件头与常量注释 env→config 全面更新（原 "GAME_STREAM_IDLE_TIMEOUT_MS=60000" 表述改为 "agent_timeouts config block: streamIdleTimeoutMs=5000, toolHeartbeatIntervalMs=2000 via deploy_agent_stall.yaml configs selection"）；(3) **新增 case (d)** `TestAgentStallThinkInterruptGapDetected`（046→044 打通验证，quickstart D3）：`setupTeamSession(..., "gpt-4", "gpt-4")`（非 reasoning 模型，config 显式即有效超时）；`sendText(t, conn, sessionID, "think interrupt gap")`（命中 `think-interrupt-gap` 模板）；依次收到两个 thinking frame（chunk0 "Analyzing the board state."、chunk1 "Evaluating candidate moves."，间隔 ~1s）后记录 `stallStart := time.Now()`；`drainUntilWait` 收 warn+wait（恰 1 个 wait、有 warn）；elapsed ∈ [stallDetectMin, stallDetectMax]（有限 15s gap 在 ~5s 处触发检测）；`listMessages(t, ..., "player")` 断言 partial 两个 reasoning chunk 已持久化（`messagesContainThinking` 命中 chunk0 文本即可——merge 后为拼接 thinking part）且存在 `thinking part completion == game.PartCompletion_PART_COMPLETION_INTERRUPTED`（tail 标记；模板的 chunk2/text 永不到达）；(4) 既有四用例的时序注释同步（60s→5s、65s→12s、心跳 10s→2s）。`bazel run //:go -- fmt projects/game/testplan/agent_stall_test.go` + `bazel test //projects/game/testplan:agent_stall_test` 的编译验证（实际执行在 T012）。(FR-001/FR-003/FR-004/FR-005 大型验收准备; SC-002/SC-003/SC-004.)
- [ ] T018 [P] Update `projects/game/testplan/system_test.yaml`: §11 注释块（`:76-80` 附近）与 `agent-stall` suite description（`:194-195`）中 "GAME_STREAM_IDLE_TIMEOUT_MS=60000 / deploy_agent_stall.yaml env" 表述全部改为 config 表述（"deploy_agent_stall.yaml selects the agent_timeouts config block (streamIdleTimeoutMs=5000, toolHeartbeatIntervalMs=2000) via 045 deploy-config; the agent resolves env > config > default"）；若 description 提及心跳 10s 处改 2s。同步更新 `projects/game/testplan/helpers_test.go:35-42` 的 `wsReadTimeout` 注释（"deploy_agent_stall.yaml GAME_STREAM_IDLE_TIMEOUT_MS=60000" → config 驱动表述）；`wsReadTimeout = 75s` 常量值不改（读超时上限，仍远超 5s 窗口）。仅注释/描述，不改 suites/cases 结构与常量。**2026-08-14 amendment（executor 执行前校验发现规划遗漏：fd81521 的 60s 重基线共写入四处的 env/60s 表述——system_test.yaml、helpers_test.go、README.md、BUILD.bazel，原 T018 只列前两处）——文件范围扩展为四处，新增下述 (3)/(4)；表述基准不变：T011 已落地的 `projects/game/testplan/agent_stall_test.go:50-57` 头注释与 `system_test.yaml:76-82` §11 注释，不引入第三种措辞**：
  (3) `projects/game/testplan/README.md:221-228`（§6 "How to run the testplan" 末段，agent-stall 套件部署描述）env→config 表述更新：a) "the same topology with `GAME_STREAM_IDLE_TIMEOUT_MS: "60000"` injected on the `agent_test` artifact (spec 044 FR-001's 60s minimum — the pre-044 15s value would be clamped to 120s ...)" → "the same topology with the `agent_timeouts` config block (streamIdleTimeoutMs=5000, toolHeartbeatIntervalMs=2000) selected on the `agent_test` artifact via the 045 deploy-config channel"；b) clamp 论述方向反转——原括号解释 env 通道下 sub-60s 值被 clamp 到 120s（为何 env 必须 60s），新文改为 config 值 as-is 生效、按设计绕过 env 通道的 60s clamp（[contracts/idle-timeout-contract.md §5](contracts/idle-timeout-contract.md#5-service-config-channel-2026-08-14-amendment)——这是套件拿到 sub-60s 窗口的机制，也是 T017 env→config 切换的动机）；c) 末句 "`STREAM_IDLE_TIMEOUT_MS` is evaluated at agent module load, hence the env must be set at deploy time" → "the config overrides are resolved once at agent startup (module load), hence the block must be selected at deploy time"（机制不变——加载期一次性解析；语义 env→config）。
  (4) `projects/game/testplan/BUILD.bazel:216-252`（`agent_stall_test` go_largetest target 的注释与属性）：a) 头注释（`:216-220`）"T011 cases b/c" 补为 "b/c/d"；"deploy_agent_stall.yaml — GAME_STREAM_IDLE_TIMEOUT_MS=60000, spec 044 FR-001's 60s minimum" → "deploy_agent_stall.yaml — selects the agent_timeouts config block (streamIdleTimeoutMs=5000, toolHeartbeatIntervalMs=2000) via the 045 deploy-config channel (config values bypass the env channel's 60s clamp by design)"；b) 043 US3 bullet（`:227-230`）"a saolei_operate dispatch delayed > 65s (> idleTimeout)" → "a saolei_operate dispatch delayed 12s (> idleTimeout — ~6 heartbeat ticks at the 2s cadence)"；c) 044 US1 bullet（`:231-234`）"detected within the configured 60s window — elapsed detection bracketed against the old 15s/30s windows and the 120s default" → "detected within the config-driven 5s window — elapsed detection bracketed against the older windows (15s/30s/60s) and the 120s default"；d) **补 case (d) bullet**（044 US3 bullet 之后；T011 已实现 `TestAgentStallThinkInterruptGapDetected`）："TestAgentStallThinkInterruptGapDetected (046→044 enabler pair, quickstart D3): a think-interrupt-gap turn streams two reasoning chunks then a finite 15s gap; the gap exceeds the config-driven 5s window so the detector fires mid-thinking at ~5s; the pre-gap chunks persist as the partial output with completion = PART_COMPLETION_INTERRUPTED on the tail part ([046 contracts/template-config.md §3.3](../046-fake-llm-think-chunking/contracts/template-config.md)）"；e) **size 裁决：`size = "large"` 降为 `size = "medium"`，并同步重写 size 依据注释（`:240-244`）**。裁决依据：i) 原 large 评级的事实基础（三用例各等满 60s idle 窗口 + 65s tool delay ≈ 270s > 300s medium 余量）已随 T011 时序重标定全部消失（60s→5s、65s→12s）：五用例健康路径合计 ≈60–85s（T012 "<2min" 规划上界），medium 的 300s 预算留 ≥3.5× CI 余量（small 的 60s 不足——套件本身超 60s），满足 [style/large_test.md](../../style/large_test.md) §测试用例 "size 按实际情况设置"；size→timeout 映射（small=60s / medium=300s / large=900s）见 [bazel common-definitions — test.size](https://bazel.build/reference/be/common-definitions#test.size)，`guitar run` 经 `bazel test --config=largetest` 执行用例（`.opencode/skills/testplan/SKILL.md` 执行流程），`.bazelrc` 的 `test:largetest` 仅设 `--cache_test_results=no` 未覆写 timeout——size 即实际生效的硬超时。ii) 失败路径核查（medium 不吞诊断）：设计内最坏失败 = config 通道回归（agent 回退 120s 默认）——T1 ~130s 失败、T2 ~25s 通过、T3 ~125s 失败，累计 ~280s 后 T4/T5 在 300s 处被 kill；但 T1/T3 各自的 bracket 断言信息（"detected after ≈120s, want [3s,10s]"）已精确指认默认回退，诊断价值完整保留，且该场景本为硬失败、修复后重跑与档位无关；R9 心跳复发路径 ~5s 快速失败；病态 hang 受 `wsReadTimeout=75s` 首个阻塞读上界约束，300s kill 相对 900s 反而更快暴露问题。iii) **实施注意：size 是 target 属性变更（非注释）**——`agent_stall_test` 是手工维护的自定义命名 `go_largetest` target，gazelle 仅管理默认命名 target（`style/large_test.md` §测试用例的默认名机制即为此设计），本次属性编辑属 gazelle 之外的手工 target 级改动（AGENTS.md "BUILD.bazel 通常只由 gazelle 生成/更新" 的记录在案的例外）；T013 的 `bazel run //:gazelle projects/game/testplan` 不会覆写本 target（与 T013 无顺序约束，T011 未新增源文件、gazelle 对本 target 无增量）。iv) 本任务原文 "仅注释/描述，不改 suites/cases 结构与常量" 就扩展范围修正为：除本修订明确裁决的 `size` 属性外，仍仅注释/描述，不改 suites/cases 结构与任何常量。
  验证（新增范围）：`bazel build //projects/game/testplan:agent_stall_test`（BUILD 文件与 size 属性合法性）；无 yaml/常量改动，`guitar validate` 与单测不受影响；档位的实际印证在 T012——全绿运行观测时长应 ≈60–90s，远低于 medium 的 300s。
- [ ] T019 [P] fake-llm hygiene — **2026-08-14 amendment（executor 前置校验中止后的事实更正）**: 原文声称 "pin 断言已核实无需同步"，与代码不符——046 store-level pin 测试**确实 pin 了** `chunk_delays`。变更共两个文件：(1) `projects/game/fake-llm/service/testdata/stall_recovery.yaml` — `think-interrupt-gap` 的 `chunk_delays: ["1s", "90s"]`（`:43`）→ `["1s", "15s"]`（5s 窗口下检测在 gap 起始 ~5s 触发；15s 保证 gap 覆盖检测点，且用例失败路径（如检测未触发）最多多等 15s 而非 90s）；同步更新模板注释两处 "90s" 表述——`:29` "90s gap (above the agent's idle timeout …)" 改为 15s，并将 "above the agent's idle timeout" 措辞改为指向 stall 部署 config 驱动的 5s idle 窗口（15s 仅在该窗口下超过 idle；默认拓扑 120s 下不触发——模板由显式关键字命中（T011 case (d)）且被 FR-011 排除出随机 fallback），`:33` "The 90s chunk_delays entry" → "The 15s …"（仍为非零 delay，FR-011 isHangCapable 排除语义不变）；契约引用行（`:26-27` → [046 template-config.md §3.3](../046-fake-llm-think-chunking/contracts/template-config.md)）**不改**——契约示例保留 90s 原形状，其 "044 cross-feature note (2026-08-14)" 已记载 rescale。(2) `projects/game/fake-llm/service/message_store_test.go:819-821` — `TestNewMessageStore_LoadsEmbeddedSamples`（`:507`）的 pin 断言 `slices.Equal(thinkGap.ChunkDelays, []string{"1s", "90s"})` 同步改为 `["1s", "15s"]`（`:820` 错误信息中的 want 字面量 `[1s 90s]` 一并更新）；同函数其余 delay pin 不受影响（`think-healthy-cadence` `["200ms","200ms"]` `:802-804`、`think-interrupt-stall` `["1s"]` `:836-838`）；该测试注释块（`:789-794`）与 `projects/game/fake-llm/service/handler_test.go` 均无 "90s" 字面量（grep 核实），无需改动。验证：`bazel run //:go -- fmt projects/game/fake-llm/service/message_store_test.go`（本任务新增 Go 变更）+ `bazel test //projects/game/fake-llm/...` 全绿（含 pin 测试——原文 "本任务仅改 testdata" 与回归绿要求内部矛盾，本修订消除）。**"90s→15s" 跨文档记录经复核仍然如实、无需改动**：[quickstart.md](quickstart.md) D3、[research.md](research.md) R11、[large-test-status.md](large-test-status.md) §6 046 条目、[046 template-config.md §3.3](../046-fake-llm-think-chunking/contracts/template-config.md) cross-feature note 均只记载 "T019 将 90s 缩至 15s"（rescale 事实），不含 "pin 无需同步" 的错误声明；046 template-config.md 自身契约语义（字段规则/示例形状/校验规则）不受本修订影响，不作改动。
- [ ] T012 Large-test acceptance (Constitution VI): execute the testplan via the testplan skill — full deploy→test→cleanup loop: `guitar validate projects/game/testplan/system_test.yaml && guitar run projects/game/testplan/system_test.yaml`（agent-stall suite：043 二用例 + 044 (b)(c)(d) 三用例 = 五用例，config 驱动 5s/2s 时序下全套预计 <2min）。**All cases MUST pass**（任何 failed/flaky = 验收未过，修复后重跑直至全绿；build-only 不构成验收）。**SC-005 裁决收口（spec owner，research.md R11）**：验收记录 MUST 同时载明 α/β/γ 裁决结果——α 或 β 时 SC-001/SC-005 以单测替代并记录在案，γ 时按 T020 补 floor 大型用例。**R9 应急分支（仅当心跳用例 `TestAgentStallToolExecutionNotFalselyDetected` 再失败时执行）**：用 signoz skill 查询本次运行 trace/logs（service `game/agent`）——per-tick 日志在且 false stall → LangGraph `touch()`→`checkIdle` 路径问题（升级调查至 LangGraph 层，考虑 refreshOn/配置绕行）；tick 日志缺失/停止 → wrapper 定时器生命周期 bug（`llm.ts` `withIdleHeartbeat`，修 `setInterval` 路径）；定位后修复、重跑直至全绿。
- [ ] T013 Housekeeping: `bazel run //:gazelle projects/game/agent/src`（T014 新文件 target）与 `bazel run //:gazelle projects/game/testplan`；`bazel run //:go -- fmt`（T011/T019 的 Go 改动已在各自任务内 fmt——T011 `agent_stall_test.go`、T019 `message_store_test.go`（2026-08-14 amendment 新增）；yaml 无需 fmt，仅当另有 Go 改动）；`bazel mod tidy`；最终 `bazel build //...` + `bazel test //projects/game/agent/...`（+ `bazel build //projects/game/desktop/frontend:dist`）全树绿；将 T012 全绿结果与根因裁定结论（心跳用例是否复发、tick 日志证据）以及 SC-005 α/β/γ 裁决结果（spec owner）追加到 [large-test-status.md](large-test-status.md)（§1 进度表与 §4 裁决栏）；勾选本文件全部任务。
- [ ] T020 **[OPTIONAL — gated on SC-005 ruling (research.md R11; [large-test-status.md](large-test-status.md) §4)]** γ floor large-test case（spec owner 裁决为 γ 才执行，默认 α 不做）：在标准套件既有模块文件（`projects/game/testplan/agent_saolei_test.go`，deploy `deploy_agent.yaml`——默认拓扑 env+config 均未设 → 有效超时 `max(120s, deepseek floor 600s)`）新增 ~3min 用例：fake-llm 新增 resumable 模板 `think-floor-tolerance`（`reasoning_chunks` 3 块、`chunk_delays: ["1s", "150s"]`，>120s 默认且 <600s floor）；deepseek profile（`setupTeamSession(..., "deepseek-v4-flash", "deepseek-v4-flash")`——profile 模型名即 bare model spec，经 `team/graph.ts` 传入 `resolveStreamIdleTimeout`；`deepseek-v4-` 子串命中 allowlist（`projects/game/agent/src/reasoning-timeouts.ts:36-38`），与 T003 单测的完整 spec 形式 `openai/deepseek-v4-flash` 等价命中）；断言 150s 静默 gap 内**无** warn/wait frame、gap 后 chunk2+text 到达、turn 正常终止（恰 1 个 terminal wait）——证明 floor 抬升生效（回归时 120s 默认会在 gap 中触发 false stall）。执行 `guitar run projects/game/testplan/system_test.yaml`（saolei suite）全绿。

---

## Dependencies & Execution Order

> **编号与执行顺序说明**：T001–T013 编号因被 plan.md / large-test-status.md / quickstart.md 引用而保持稳定；**实际执行顺序为 T014–T017（Phase 5A）→ T011 ∥ T018 ∥ T019（Phase 5）→ T012 → T013（→ T020 可选）**。已完成任务（`[x]` T001–T010）不重做。

### Phase Dependencies

- **Phase 1–4（T001–T010）**: ✅ 全部完成（commits `f953e9a`/`8cf212e`/`1c13b4e`/`3531228`/`166683e`，见 [large-test-status.md](large-test-status.md) §1）——**勿重做**。
- **Phase 5A（T014–T017）**: resume 的阻塞前置——config 通道不存在则 T011 的 5s/2s 时序无法生效。内部顺序：T014 → T015（llm.ts 消费 resolver）→ T016（声明+打包 wiring，依赖 T014 模块与 T015 import 存在后 gazelle 才能解析 node_modules target）→ T017（deploy 切换，依赖 T016 的 service.yaml 声明，否则 config 选择校验失败）。
- **Phase 5 剩余**: T011/T018/T019 依赖 Phase 5A 完成（时序由 config 决定）；三者不同文件可并行。T012 依赖 T011+T018+T019 全部落地。T013 收尾最后。T020 仅在 SC-005 裁决为 γ 后执行（依赖 Phase 5A 的反向验证：γ 需默认路径，与 T011 互不依赖）。

### User Story Dependencies

- US1/US2/US3（T002–T009）✅ 已完成并单测绿——resume 阶段不涉及新 US 实现，只有验收收口。
- Resume goal 2（config 通道）= Phase 5A；goal 1（大型测试完成）= Phase 5 剩余任务。

### Within Each User Story

- 已完成阶段：不重做（[large-test-status.md](large-test-status.md) "Unchanged on resume"）。
- Phase 5A 内：T014（纯函数+加载器）先于 T015（消费方）；T016 依赖 T014+T015；T017 依赖 T016。
- Phase 5 内：T011/T018/T019 并行（不同文件）；T012 是唯一验收门禁（Constitution VI）；T013 收尾。
- 单测随各实现任务内联（Constitution IV），不单列 task。

### Parallel Opportunities

- T011 ∥ T018 ∥ T019 —— 不同文件（`agent_stall_test.go` / `system_test.yaml` + `README.md` + `BUILD.bazel`（2026-08-14 amendment 扩展）/ fake-llm `stall_recovery.yaml` + `message_store_test.go` pin 断言（2026-08-14 amendment）），Phase 5A 完成后可并行。
- T020（若裁决 γ）与 T011–T019 无文件冲突，可与 Phase 5 并行启动（但其 `guitar run` 与 T012 分开执行，避免环境竞争）。

---

## Parallel Example

```text
# Phase 5A（顺序执行，单一依赖链）:
Task: "T014 agent-timeouts.ts 纯函数解析 + 单测"
# after T014:
Task: "T015 llm.ts 常量重源 + withIdleHeartbeat per-tick 日志"
# after T014+T015:
Task: "T016 service.yaml agent_timeouts 声明 + package.json/BUILD wiring + pnpm/gazelle/mod tidy"
# after T016:
Task: "T017 deploy_agent_stall.yaml env→configs 切换 + guitar validate"

# Phase 5A 完成后（不同文件，并行）:
Task: "T011 agent_stall_test.go 时序重标定 5s/2s/12s + 新增 think-gap case (d)"
Task: "T018 system_test.yaml/README.md/BUILD.bazel 注释 env→config 更新 + agent_stall_test size 降档 medium（2026-08-14 amendment 扩展）"
Task: "T019 fake-llm think-interrupt-gap 90s→15s（stall_recovery.yaml + message_store_test.go pin 断言同步）"

# 全部落地后:
Task: "T012 guitar run 大型测试验收（全绿闭环，含 R9 应急分支预案）"
Task: "T013 housekeeping + 全树构建/测试 + 状态文档收口"
```

---

## Implementation Strategy

### 已交付（历史记录，勿重做）

1. Phase 1–4（US1 120s 默认、US2 reasoning floor、US3 部分输出持久化 + proto 标记 + 桌面渲染）+ T010（proto 注释调和）——全部提交且单测绿。
2. T011 的 fd81521 版本（60s 重基线）代码存在但时序被本修订取代——T011 在其基础上重标定，不从零重写。

### Resume 增量交付（2026-08-14 起）

1. Phase 5A → config 通道端到端就绪（`DOMINION_CONFIG_DIR` 未设 = 行为不变，单测回归证明）。**STOP and VALIDATE**: `bazel test //projects/game/agent/...` 全绿 + `guitar validate` 通过。
2. + T011/T018/T019 → 套件在 5s/2s 时序下就绪（编译级验证）。
3. + T012 → **Constitution VI 验收**：`guitar run` 全绿闭环；心跳用例若复发走 R9 应急分支（tick 日志定界 → 修复 → 重跑至全绿）。
4. + T013 → 全树绿 + 文档收口（large-test-status 追加结论、任务全部勾选）。
5. （可选）T020 → 仅当 spec owner 裁决 SC-005 为 γ。

### Notes

- **No retry/fallback**（FR-009）——不添加任何重试逻辑。
- **不重做已完成阶段**（[large-test-status.md](large-test-status.md) "Unchanged on resume"：Phases 1–4、PartCompletion 设计、US2 floor 大型用例取消、FR-005 标记规则、T010）。
- **Large tests by module**: 用例都在既有 `agent_stall_test.go`（stall 模块）+ 既有 `system_test.yaml` agent-stall suite（deploy: `deploy_agent_stall.yaml`）——不新建 per-feature YAML（`style/large_test.md` §反模式）；T020（γ）落在既有 `agent_saolei_test.go` + 既有 saolei suite，同样不新建。
- **心跳无 env 通道**（仅 config/默认）——Q1 决策；如未来需要 ops 临时调心跳，走 config 或另立特性。
- **SC-005 裁决未决**期间按 α 工作（quickstart A4 注明）；T020 是预析的 γ，勿自行启动。
- **Commit** after each task or logical group; keep the baseline green at every commit.
