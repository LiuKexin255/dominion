# Implementation Plan: Conversation Content-Model Refactor & Saolei MCP Simplification

**Branch**: `023-saolei-mcp-refine` | **Date**: 2026-07-25 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/023-saolei-mcp-refine/spec.md` (Clarifications C1..C12 settle the design at the message-structure level).

## Summary

> **Revision (2026-07-25):** US3 implementation proved MCP tools cannot obtain the LangChain `tool_call.id` and the MCP adapter cannot carry `additional_kwargs.toolResultStatus`. The design is revised to **decouple the conversation channel from the operation channel** (research.md D10), **re-anchor the debug Confirm onto a session-top drawer** (D11), and accept **neutral status for saolei (MCP) tools** (D12). This unblocks US3 and removes the now-dead `toolId` coupling. See the "Revision impact on already-implemented phases" section below.

A refactor of the desktop↔agent conversation content model that splits the single `Part` oneof into two disjoint categories — a display-only **`MessagePart`** (text / thinking / image / **`tool_call`** (new) / `tool_result`) and a control-only **`FlowPart`** (mouse/keyboard operations + wait/warn/status signals) — so the conversation renders from one source of truth (the LLM messages) and operation/control signals stop appearing as chat entries. A new **`ToolCallPart`** carries the model's tool invocation (name + args + `tool_id`); a tool call and its result render as **one evolving bubble** keyed by the LangChain `tool_call.id` (conversation channel). The **operation channel** (`FlowPart` dispatch) is **decoupled**: `OperationBridge.dispatch` mints its own operation id (no `toolId` parameter) for dispatch↔result correlation, independent of the conversation id. The real `ToolResultStatus` is carried into the `ToolMessage` (`additional_kwargs`) for **native** tools (mouse) and through the checkpoint, so history reconstruction reads the actual outcome; **MCP** (saolei) tools read neutral (never FAILED) — eliminating the spurious "failed" history bug (the text-heuristic `inferToolResultStatus` is removed). The saolei MCP becomes **stateless**: the per-session grid model, the rule validators, the operate-then-update alternation, and the `saolei_update` tool are removed; the four surviving tools (`saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`) are pure dispatch-and-return over the existing `OperationBridge`, and the grid→pixel geometry is retained. The 022 debug-mode hold is re-anchored onto a **session-top drawer** on the operation channel: the hold stays at the same point (after desktop execute, before return to agent), the Confirm control moves into a drawer showing the operation request content (decoupled from the conversation), and the execution outcome is reachable in the log during the hold.

Technical approach and decisions are grounded in [research.md](research.md) (decisions D1..D12; D10..D12 are the 2026-07-25 decoupling revision); the proto interface contract in [contracts/content-model-contract.md](contracts/content-model-contract.md); the operation-channel + tool contract in [contracts/tool-dispatch-contract.md](contracts/tool-dispatch-contract.md); the debug Confirm drawer in [contracts/debug-drawer-contract.md](contracts/debug-drawer-contract.md); data structures in [data-model.md](data-model.md).

## Technical Context

**Language/Version**: TypeScript (agent, Node.js; `@langchain/core` ^1.2.3, `@langchain/langgraph` ^1.4.8, `langchain` ^1.5.3, `@langchain/mcp-adapters` ^1.1.3, `@modelcontextprotocol/sdk` ^1.29.0 per `pnpm-workspace.yaml` catalog) + Go (desktop, Wails v2) + proto3 (`projects/game/game.proto`).

**Primary Dependencies**:
- *Existing (agent TS)*: `langchain` (`createAgent`, `tool`), `@langchain/langgraph` (`MemorySaver`), `@langchain/core` (`AIMessage`/`ToolMessage`/`HumanMessage`, `streamEvents`), `@modelcontextprotocol/sdk` (`McpServer`), `@langchain/mcp-adapters` (`MultiServerMCPClient`), `zod`, `@grpc/grpc-js`, `@grpc/proto-loader`.
- *Existing (desktop Go)*: Wails v2 runtime, raw Win32 (`user32.dll`) — no new third-party dependency.
- *No new external dependency is introduced* by this feature.

**Storage**: In-memory only. LangChain `MemorySaver` checkpoint of `BaseMessage`s is the sole persistence (history). Proto `Message`/`AgentFrame` are transport/reconstruction types. Per Clarification C2 the proto change is a **clean break** — old checkpoints remain readable (they store `BaseMessage`s, whose shape is unchanged: `AIMessage.tool_calls`, `ToolMessage.content` + `additional_kwargs`); only the proto reconstruction layer changes. No database.

**Testing**: `vitest` (agent TS unit/integration), `go test` (desktop unit), large tests via the `testplan` skill (`style/large_test.md`). Compile + unit tests run per code change as part of each task (not separately tasked). The agent is a service → large tests REQUIRED for acceptance (Constitution §VI).

**Target Platform**: Agent = Linux container (deployed via `oci_image`); MCP server on localhost. Desktop = Windows (Win32 input, Wails v2).

**Project Type**: web-service (agent) + desktop-app (desktop) — a multi-project change, confined to the existing `projects/game/{game.proto, agent/, desktop/}` tree.

**Performance Goals**: No new latency target. Tool-dispatch latency is still dominated by the existing `OperationBridge` timeout (20 min backstop, 022 FR-014) and the LLM turn. The content-model split adds no round-trips.

**Constraints**:
- Clean proto break (C2): old sessions/checkpoints need not render under the new proto reconstruction; no compatibility shim.
- Decoupled ids (D10, revised): the conversation channel groups `tool_call`↔`tool_result` MessageParts by the LangChain `tool_call.id`; the operation channel (`FlowPart` dispatch↔`ToolResultPart`) uses a bridge-minted id. The two are NOT required to match. (`OperationBridge.dispatch` takes no `toolId` parameter.)
- Real tool-result status survives the `MemorySaver` round-trip (FR-012) for **native** tools — carried via `ToolMessage.additional_kwargs.toolResultStatus` (research.md D4). **MCP** (saolei) tools read neutral (UNSPECIFIED), never FAILED (D12).
- The desktop-facing operation contract from 018 is unchanged (`KeyboardPressPart{F2}` for init; `MouseMoveAndClickPart` at fixed board centre with `WINDOW_MESSAGE` for cell ops) (FR-020, Assumption).
- Debug Confirm is a session-top drawer on the operation channel (D11), not on a conversation bubble.

**Scale/Scope**: Single supervised session per operator. The change touches the proto, the agent handler/bridge/llm/tools/saolei-mcp/skill, and the desktop recvLoop/ChatView/api. No new project or external dependency.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Checked against `.specify/memory/constitution.md` (v1.3.0). All principles satisfied; no unjustified complexity. Quality gates (in execution order):

| # | Principle / Gate | Status | Evidence |
|---|---|---|---|
| 1 | **V — Read Before Code** (doc reading gate) | PASS (planned) | `tasks.md` (next command) MUST declare per-phase docs under the three mandatory categories (代码规范文档 / 官方文档 / 技术文章). Required reading includes `style/javascript.md`, `style/golang.md`, `style/api.md`, `style/large_test.md`, the LangChain JS `streamEvents` + tool `config.toolCall` docs, the MCP SDK docs, and this feature's contracts. |
| 2 | **III — Interface-First Design** | PASS | Interface contracts settled BEFORE implementation: [contracts/content-model-contract.md](contracts/content-model-contract.md) (the proto content-model split + `ToolCallPart`), [contracts/tool-dispatch-contract.md](contracts/tool-dispatch-contract.md) (decoupled operation channel — `dispatch` mints its own id; native-tool status carriage; stateless saolei tool set), and [contracts/debug-drawer-contract.md](contracts/debug-drawer-contract.md) (session-top Confirm drawer on the operation channel). |
| 3 | **II — Refactoring Over Patching** | PASS | This IS the refactor (spec §Motivation): the live/history divergence is fixed by unifying on one source of truth (LLM messages) and splitting control out, not by patching `inferToolResultStatus` or special-casing saolei parts. The saolei state/validation layer is removed (simplification, not stacking). `pushResult` — now consumer-less — is removed. The 2026-07-25 revision **decouples** the conversation and operation channels (removing the unimplementable MCP `tool_id`/`additional_kwargs` coupling) and **removes** the Confirm-on-bubble coupling surface — net simplification (Principle II). |
| 4 | **I — Citation & Provenance** | PASS | All design docs cite sources: repo-relative paths for internal refs, full URLs for external (LangChain `config.toolCall` source, streamEvents docs). See References sections of `spec.md`, `research.md`, and each contract. |
| 5 | **IV — Test Granularity & Cadence** | PASS (planned) | Compile (`bazel build //...`) + unit tests (`bazel test //...`) per code-change task; not separately tasked. [quickstart.md](quickstart.md) Scenarios 1–4 are unit/integration; Scenarios 5–7 are large tests. |
| 6 | **VI — Large Test Acceptance for Services** | PASS (planned) | The agent is a service; large tests REQUIRED and MUST be executed via the `testplan` skill (`guitar run <plan.yaml>`), not merely built. Existing `projects/game/testplan/system_test.yaml` suites (`agent-saolei`, `agent-operation`, `checkpoint-resume`, `agent-dialog`) are UPDATED to the new content model + stateless tools + carried status; new cases added per [quickstart.md](quickstart.md) Scenarios 5–7. All cases MUST pass. |

**Post-Phase-1 re-check**: see the "Post-Phase-1 Constitution Re-check" section at the end of this file.

## Revision impact on already-implemented phases (2026-07-25)

Phases 1–3 were implemented against the ORIGINAL (coupled) design before this revision. The revision requires the following adjustments (to be sequenced in `tasks.md`):

| Implemented phase | Revision impact |
|---|---|
| Phase 1 (spikes D2/D4) | **None** — both hypotheses still hold (D2: `config.toolCall.id` reachable for native tools; D4: `additional_kwargs` round-trips). |
| Phase 2 (US1 content model + rendering) | **Small rework**: `OperationBridge.dispatch` drops the `toolId` parameter (revert the Phase-2 addition); mouse tools stop passing `toolCallId` to `dispatch` (keep reading `config.toolCall.id` only for the `ToolMessage.tool_call_id`). `ChatView`/`App.svelte` remove the `heldToolIds`/`onConfirm` props (Confirm leaves the bubble — the drawer lands in US4). The proto split, tool_call/tool_result bubbles, and live/history rendering are RETAINED. |
| Phase 3 (US2 real status) | **None for native tools** — `buildToolResultMessage` + `consumeToolResults` (live path) are retained. Saolei neutral status (D12) is a US3 concern. |
| Phase 4 (US3 saolei) | **Unblocked** — saolei tools dispatch via `bridge.dispatch(part, signal)` (no toolId) and return MCP content blocks; status neutral (D12). No MCP→native conversion; no `additional_kwargs` injection. |
| Phase 5 (US4 debug) | **Redesigned** — Confirm moves from the tool bubble to a session-top drawer (D11, `contracts/debug-drawer-contract.md`). |
| Phase 6 (large tests) | Update assertions: decoupled ids, saolei neutral status, drawer (manual/desktop). |

The proto (`contracts/content-model-contract.md`) is **unchanged** by this revision — only the SEMANTICS of `tool_id` (two independent ids) and the debug/drawer surface change. `tasks.md` (regenerated by `/speckit.tasks`) sequences the rework + remaining phases.

## Project Structure

### Documentation (this feature)

```text
specs/023-saolei-mcp-refine/
├── plan.md              # This file
├── research.md          # Phase 0 — decisions D1..D9
├── data-model.md        # Phase 1 — content categories, ToolCallPart, status, bubble model
├── quickstart.md        # Phase 1 — validation scenarios 1..7
├── contracts/
│   ├── content-model-contract.md   # Phase 1 — proto split (MessagePart/FlowPart) + ToolCallPart
│   ├── tool-dispatch-contract.md   # Phase 1 — decoupled operation channel (dispatch mints id), native-tool status carriage, stateless saolei tools
│   └── debug-drawer-contract.md    # Phase 1 — session-top Confirm drawer (operation channel, D11)
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                              # REFACTOR: split Part oneof → MessagePart + FlowPart;
│                                           #   new ToolCallPart, MessageParts, FlowParts;
│                                           #   AgentFrame.payload oneof { message_parts; flow_parts };
│                                           #   Message.content → MessageParts; remove PartBlock/Part;
│                                           #   WaitSignal/WarnSignal/StatusSignal become FlowPart kinds.
│
├── agent/                                  # TypeScript agent
│   ├── BUILD.bazel                         # gazelle regen after proto + src changes
│   └── src/
│       ├── handler.ts                      # Connect: emit MessageParts (text/thinking/image/tool_call/
│       │                                   #   tool_result) + FlowParts (operations/wait/warn/status);
│       │                                   #   ListMessages: reconstruct MessageParts from BaseMessages
│       │                                   #   (AIMessage.tool_calls → ToolCallPart; ToolMessage +
│       │                                   #   additional_kwargs.toolResultStatus → ToolResultPart);
│       │                                   #   remove inferToolResultStatus / toolCallToPart /
│       │                                   #   reconstructToolResult (replaced by real-status path).
│       ├── llm.ts                          # generateTurn: yield tool_call blocks (from AIMessage.tool_calls)
│       │                                   #   and tool_result blocks (from ToolMessage); new ContentBlock
│       │                                   #   variants { type: "tool_call" | "tool_result" }.
│       ├── operation-bridge.ts             # dispatch(part, signal): bridge mints its own operation id
│       │                                   #   (decoupled — D10; NO toolId param); REMOVE pushResult
│       │                                   #   (consumer-less after saolei_update removal).
│       ├── tools/shared/result-blocks.ts   # buildToolResultMessage(result, toolCallId, name): return a
│       │                                   #   ToolMessage carrying content blocks + additional_kwargs.
│       │                                   #   toolResultStatus (real status) — replaces buildResultBlocks.
│       ├── tools/mouse_click/mouse-click.ts# read config.toolCall.id → dispatch(part, signal) →
│       │                                   #   return ToolMessage via buildToolResultMessage (tool_call.id
│       │                                   #   → ToolMessage.tool_call_id, NOT to dispatch — D10).
│       ├── tools/mouse_move/mouse-move.ts  # (same change as mouse-click)
│       ├── mcp-host.ts                     # createSaoleiMcpServer(bridge) — drop the GameState param.
│       ├── mcp/saolei/
│       │   ├── saolei-mcp.ts               # REWRITE: 4 stateless tools (init/click/flag/chord_click),
│       │   │                               #   pure dispatch-and-return; drop saolei_update + alternation.
│       │   ├── geometry.ts                 # KEPT (unchanged) — grid→pixel formula.
│       │   ├── game-state.ts               # REMOVE (no per-session grid model).
│       │   ├── validation.ts               # REMOVE (no rule validators).
│       │   ├── saolei-mcp.test.ts          # UPDATE: assert 4 tools, stateless, back-to-back dispatch.
│       │   └── validation.test.ts          # REMOVE.
│       └── skill/saolei/SKILL.md           # REWRITE: 4 tools, top-left (x,y), read screenshot;
│                                           #   no saolei_update, no alternation, no validation.
│
├── desktop/                                # Go + Svelte desktop app
│   ├── app.go                              # recvLoop: branch on payload (message_parts → append to
│   │                                       #   chatstream + render; flow_parts → execute operations,
│   │                                       #   handle wait/warn/status, do NOT append operations);
│   │                                       #   handleInboundOperation: drop the result mirror to
│   │                                       #   chatStreams (FR-010 — screenshot comes from the LLM
│   │                                       #   tool result); debug hold Confirm associates with the
│   │                                       #   tool_call bubble via tool_id (FR-023).
│   ├── view_model.go                       # MessageViewModel.content is now MessageParts (protojson).
│   └── frontend/src/
│       ├── api.ts                          # SPLIT: MessagePart/FlowPart types; new ToolCallPart;
│       │                                   #   messagePartKind(); AgentFrame.payload { message_parts,
│       │                                   #   flow_parts }; Message.content → MessageParts.
│   │       ├── App.svelte                      # handleAgentFrame: react to wait/warn/status (now in
│       │                                       #   flow_parts); drives the session-top Confirm drawer
│       │                                       #   (heldOperations, D11) — NO heldToolIds/onConfirm on ChatView.
│       │                                       #   handleMessageParts replaces handleContentPayload.
│       └── components/ChatView.svelte      # Render ONLY MessageParts; group tool_call + tool_result
│                                           #   into ONE evolving bubble per conversation tool_call.id
│                                           #   (NO Confirm control — that is the drawer).
│
└── testplan/                               # UPDATE existing suites + add cases (style/large_test.md:
│                                           #   organized by module, not by spec-id)
    ├── system_test.yaml                    # agent-saolei / agent-operation / checkpoint-resume /
│                                           #   agent-dialog suites updated to the new model.
    ├── agent_saolei_test.go                # stateless 4-tool flow; back-to-back click (no update).
    ├── agent_operation_test.go             # tool_call/tool_result bubble status carried through checkpoint.
    ├── agent_checkpoint_test.go            # leave/re-enter preserves real status (no spurious failed).
    └── agent_dialog_test.go                # tool_call renders name+args; operation parts not rendered.
```

**Structure Decision**: Multi-project change across the existing `projects/game/{game.proto, agent/, desktop/}` tree. No new top-level project or external dependency. The proto is the single shared interface regenerated into both TS (`ts_proto_library` `game_types`) and Go. Removed files (`game-state.ts`, `validation.ts`, `validation.test.ts`) are deleted; the saolei MCP keeps its directory with the surviving `saolei-mcp.ts` + `geometry.ts`.

## Complexity Tracking

> Not applicable — no Constitution Check violations to justify. The content-model split is the refactor the spec mandates (Principle II: the existing unified `Part` cannot satisfy "render only LLM messages"; simplifying it required, not gold-plating). Removing the saolei state/validation layer and `pushResult` is net simplification. No entries.

## Phase 0 / Phase 1 Outputs

- **Phase 0 — Research**: [research.md](research.md) (decisions D1..D9, all spec plan-time unknowns resolved).
- **Phase 1 — Data model**: [data-model.md](data-model.md).
- **Phase 1 — Contracts**: [contracts/content-model-contract.md](contracts/content-model-contract.md), [contracts/tool-dispatch-contract.md](contracts/tool-dispatch-contract.md), [contracts/debug-drawer-contract.md](contracts/debug-drawer-contract.md).
- **Phase 1 — Quickstart**: [quickstart.md](quickstart.md) (Scenarios 1–7).

## Next step

Run `/speckit.tasks` to generate `tasks.md`, phasing the implementation against the contracts above. Each phase's document list MUST follow the constitution's three mandatory categories (代码规范文档 / 官方文档 / 技术文章), and each code phase MUST include compile (`bazel build //...`) + unit (`bazel test //...`) as part of the task. The large-test suites (Constitution §VI) are the acceptance gate and MUST be executed via the `testplan` skill (`guitar run projects/game/testplan/system_test.yaml`), not merely built — all cases MUST pass.

## Post-Phase-1 Constitution Re-check

Re-evaluated after producing `research.md`, `data-model.md`, `contracts/`, `quickstart.md` (including the 2026-07-25 decoupling revision — research.md D10/D11/D12, [contracts/debug-drawer-contract.md](contracts/debug-drawer-contract.md)):

| Principle | Status | Notes |
|---|---|---|
| I — Citation & Provenance | PASS | Every decision in `research.md` and every contract clause cites a repo-relative path or full URL (LangChain `config.toolCall` source, MCP SDK + mcp-adapters root-cause sources, prior spec/contract paths). |
| II — Refactoring Over Patching | PASS | The design unifies live+history on the LLM messages (single source of truth) and removes the text-heuristic status guess + the saolei state/validation layer + the consumer-less `pushResult`. The revision FURTHER removes the unimplementable MCP `tool_id`/`additional_kwargs` coupling by decoupling the channels, and removes the Confirm-on-bubble surface. No patch-over; net simplification. |
| III — Interface-First Design | PASS | Three contracts (proto content model; decoupled operation channel + native status + stateless tools; session-top Confirm drawer) are settled before implementation, with explicit field numbers, schemas, and error semantics. |
| IV — Test Granularity & Cadence | PASS | quickstart Scenarios 1–4 are unit/integration; Scenarios 5–7 are large-test cases folded into the existing `system_test.yaml` suites. |
| V — Read Before Code | PASS (planned) | Deferred to `tasks.md` (next command) — each phase MUST declare its three-category doc list. |
| VI — Large Test Acceptance for Services | PASS (planned) | The agent large-test suites are updated (not duplicated per `style/large_test.md`); acceptance requires actual `guitar run` execution with all cases passing. |

No design change introduced a constitution violation. No complexity-tracking entries needed.
