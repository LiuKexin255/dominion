# Implementation Plan: Saolei MCP for Grid-Based Minesweeper Operation

**Branch**: `018-saolei-mcp` | **Date**: 2026-07-20 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/018-saolei-mcp/spec.md` plus plan-input decisions recorded in `spec.md` → Clarifications (Round 2).

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

## Summary

Add a localhost **MCP server** to the game agent that exposes five **saolei** tools (`saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`, `saolei_update`) replacing raw mouse tools as the model's operation channel for minesweeper. The MCP server is path-bound per session (`/internal/mcp/{session_id}`), reusing the existing session-scoped `OperationBridge`; the agent's own LangGraph turn loop is the loopback MCP client via the official `@langchain/mcp-adapters` `MultiServerMCPClient`. The MCP maintains per-session game state with rule-based validation and an operate-then-update alternation. Saolei operations translate to generic proto operation Parts — a new `KeyboardPressPart` (F2 new game) and a new `MouseMoveAndClickPart` (window-message mouse at grid-cell centers) — dispatched to the desktop, which gains generic keyboard + window-message-mouse execution. The desktop profile editor gains an "MCP: saolei" option that also auto-injects a built-in `saolei` skill.

Technical approach and decisions are grounded in [research.md](research.md); interface contracts in [contracts/](contracts/); data structures in [data-model.md](data-model.md).

## Technical Context

**Language/Version**: TypeScript (agent, Node.js) + Go (desktop) + proto3 (`projects/game/game.proto`).

**Primary Dependencies**:
- *New (agent TS)*: `@langchain/mcp-adapters` (loopback MCP client — `MultiServerMCPClient`), `@modelcontextprotocol/sdk` / `@modelcontextprotocol/node` (MCP server runtime — `NodeStreamableHTTPServerTransport`). HTTP server: Node `http` or `express` (chosen at first tasks phase; Express matches the SDK's examples). All pinned in root `pnpm-workspace.yaml` catalog per `AGENTS.md`.
- *Existing (agent TS)*: `langchain`, `@langchain/langgraph`, `@langchain/openai`, `@langchain/anthropic`, `@grpc/grpc-js`, `@grpc/proto-loader`, `zod`.
- *Desktop (Go)*: raw Win32 via `syscall.NewLazyDLL("user32.dll")` — no new third-party dependency (adds `PostMessage`/`SendMessage` procs + keyboard `SendInput`/`WM_KEYDOWN` alongside existing `SetCursorPos`/`SendInput`).

**Storage**: In-memory only — `GameState` per session (`data-model.md` §1), co-located with the session's `OperationBridge`. No persistence (consistent with `SessionAgentStore`). N/A for databases.

**Testing**: `vitest` (agent TS unit/integration), `go test` (desktop unit), large tests via the `testplan` skill (`style/large_test.md`). Compile + unit tests run per code change as part of each task (not separately tasked).

**Target Platform**: Agent = Linux container (deployed via `oci_image`); the MCP server listens on localhost. Desktop = Windows (Win32 input).

**Project Type**: web-service (agent) + desktop-app (desktop) — a multi-project change.

**Performance Goals**: Tool-dispatch latency is dominated by the existing `OperationBridge` 5 s timeout and the LLM turn; no new hard latency target. The MCP loopback adds only localhost HTTP overhead per tool call (negligible). The supervised single-session loop imposes no throughput target.

**Constraints**: MCP server on a localhost port distinct from gRPC 50051 (env-configurable). Per-session isolation (FR-026). Proto changes MUST be backward compatible (new enum defaults to `SIMULATED`; additive oneof). Fixed board geometry targets standard Microsoft Minesweeper (24/200/32).

**Scale/Scope**: Single supervised session per human operator; one MCP server instance per session (lazily created). Not a multi-tenant/high-concurrency service.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Checked against `.specify/memory/constitution.md` (v1.2.0). All principles satisfied; no unjustified complexity. Quality gates (in execution order):

| # | Principle / Gate | Status | Evidence |
|---|---|---|---|
| 1 | **V — Read Before Code** (doc reading gate) | PASS (planned) | `tasks.md` (next command) MUST declare per-phase docs under the three mandatory categories (代码规范文档 / 官方文档 / 技术文章). Required reading includes `style/javascript.md`, `style/golang.md`, `style/api.md`, `style/large_test.md`, the LangChain MCP + MCP SDK docs, and this feature's contracts. |
| 2 | **III — Interface-First Design** | PASS | Interface contracts settled BEFORE implementation: [contracts/proto-operation-contract.md](contracts/proto-operation-contract.md) (agent↔desktop proto) and [contracts/mcp-tool-contract.md](contracts/mcp-tool-contract.md) (MCP server↔client). The adapter factory signature change is part of the design (`data-model.md` §9). |
| 3 | **II — Refactoring Over Patching** | PASS | The change extends the proto additively (new Parts + enum) and adds a new MCP server component rather than patching `buildTools`. `PromptClient`/`ProfileData`/`AdapterFactory` are extended (not bypassed) to carry `mcpNames`. The desktop gains a parallel `WINDOW_MESSAGE` execution path alongside `SIMULATED` (not a hack on the existing path). |
| 4 | **I — Citation & Provenance** | PASS | All design docs cite sources: repo-relative paths for internal refs, full URLs for external (LangChain/MCP/minesweeper). See References sections of `spec.md`, `research.md`, and each contract. |
| 5 | **IV — Test Granularity & Cadence** | PASS (planned) | Compile (`bazel build`) + unit tests (`bazel test`) per code-change task; not separately tasked. [quickstart.md](quickstart.md) Scenarios 1–5 are unit/integration; Scenarios 6–7 are large tests. |
| 6 | **VI — Large Test Acceptance for Services** | PASS (planned) | The agent is a service; large tests REQUIRED for acceptance. [quickstart.md](quickstart.md) Scenarios 6 (desktop executes new Parts) and 7 (end-to-end model reveal sequence) are large-test plans under `testplan/`, executed via the `testplan` skill. |

**Post-Phase-1 re-check**: Phase 1 produced `data-model.md`, `contracts/proto-operation-contract.md`, `contracts/mcp-tool-contract.md`, `quickstart.md`. No design change introduced a constitution violation. Interface-first (Principle III) holds — contracts precede the future `tasks.md`. No complexity-tracking entries needed.

## Project Structure

### Documentation (this feature)

```text
specs/018-saolei-mcp/
├── plan.md              # This file
├── research.md          # Phase 0 — decisions D1..D10
├── data-model.md        # Phase 1 — game state, enums, geometry, validation
├── quickstart.md        # Phase 1 — validation scenarios 1..7
├── contracts/
│   ├── proto-operation-contract.md   # Phase 1 — Part/enum extensions (agent↔desktop)
│   └── mcp-tool-contract.md          # Phase 1 — 5 saolei MCP tools (server↔client)
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                              # EXTEND: Part.kind += keyboard_press(7), mouse_move_and_click(8);
│                                           #         new KeyboardPressPart, KeyboardKey, MouseMoveAndClickPart,
│                                           #         MouseInputMethod enum; method field on MouseMovePart/MouseClickPart
│
├── agent/                                  # TypeScript agent
│   ├── BUILD.bazel                         # gazelle regen after proto + new src files
│   ├── package.json                        # += @langchain/mcp-adapters, @modelcontextprotocol/sdk (+ express?)
│   └── src/
│       ├── server.ts                       # EXTEND: start the localhost MCP HTTP server alongside gRPC
│       ├── prompt-client.ts                # EXTEND: ProfileResult += mcpNames
│       ├── session-agent.ts                # EXTEND: ProfileData += mcpNames; pass to adapter factory
│       ├── llm.ts                          # EXTEND: AdapterFactory += mcpNames; saolei profile builds MCP-client tools + skill (not mouse tools)
│       ├── mcp-host.ts                     # NEW: per-session MCP HTTP host + route /internal/mcp/{session_id}
│       ├── skill-loader.ts                 # NEW: load built-in src/skill/{name}/SKILL.md; mcp_name→skill registry
│       ├── mcp/
│       │   └── saolei/                     # NEW: saolei MCP integration (per src/mcp/README.md)
│       │       ├── saolei-mcp.ts           #   builds the session-bound McpServer (5 tool handlers)
│       │       ├── game-state.ts           #   GameState + CellStatus + alternation (data-model.md §1-4)
│       │       ├── geometry.ts             #   fixed board-layout constants + grid→client formula (§5)
│       │       ├── validation.ts           #   rule validators keyed by lastOp.kind (§8)
│       │       └── *.test.ts               #   side-by-side unit tests (repo convention)
│       └── skill/
│           └── saolei/
│               └── SKILL.md                # NEW: built-in saolei skill (per skill-md-format.md)
│
├── desktop/                                # Go + Svelte desktop app
│   ├── app.go                              # EXTEND: executeAgentOperation handles KeyboardPressPart + MouseMoveAndClickPart; honor MouseInputMethod
│   ├── view_model.go                       # (mcp_names already present in AgentProfileView)
│   ├── internal/operation/
│   │   ├── execute_windows.go              # EXTEND: real ExecuteKeyPress (WM_KEYDOWN/UP or SendInput); PostMessage mouse path
│   │   ├── window_message_windows.go       # NEW: PostMessage WM_* with client-coord lParam (WINDOW_MESSAGE method)
│   │   └── convert.go                      # (ScreenshotToScreenCoords unchanged; WINDOW_MESSAGE skips it)
│   └── frontend/src/
│       ├── components/ProfileManagement.svelte  # EXTEND: MCP chip (saolei); include mcp_names in create + update-mask
│       └── api.ts                          # (AgentProfile.mcpNames already typed)
│
└── (testplan/)                             # NEW large-test plans (Phase 2 / implementation): Scenarios 6 & 7
```

**Structure Decision**: Multi-project change across the existing `projects/game/{game.proto, agent/, desktop/}` tree. No new top-level project is introduced; new code lives under the already-contracted `src/mcp/saolei/` and `src/skill/saolei/` directories (`specs/020-agent-resources-layout/contracts/directory-layout.md`). The proto is the single shared interface regenerated into both TS (`ts_proto_library`) and Go.

## Complexity Tracking

> Not applicable — no Constitution Check violations to justify. The proto extension is additive and backward compatible; the MCP server is a new component squarely required by the feature (not gold-plating); the desktop change is a parallel execution path behind a default-preserving enum. No entries.

## Phase 0 / Phase 1 Outputs (complete)

- **Phase 0 — Research**: [research.md](research.md) (decisions D1..D10, all spec unknowns resolved).
- **Phase 1 — Data model**: [data-model.md](data-model.md).
- **Phase 1 — Contracts**: [contracts/proto-operation-contract.md](contracts/proto-operation-contract.md), [contracts/mcp-tool-contract.md](contracts/mcp-tool-contract.md).
- **Phase 1 — Quickstart**: [quickstart.md](quickstart.md) (Scenarios 1–7).

## Next step

Run `/speckit.tasks` to generate `tasks.md`, phasing the implementation against the contracts above. Each phase's document list MUST follow the constitution's three mandatory categories (代码规范文档 / 官方文档 / 技术文章), and each code phase MUST include compile (`bazel build //...`) + unit (`bazel test //...`) as part of the task, with the large-test scenarios (6, 7) as the acceptance gate.
