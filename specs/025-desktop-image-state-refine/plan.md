# Implementation Plan: Desktop Window-Select Flow, Image-Transfer Hardening & Saolei Text-State Recognition

**Branch**: `025-desktop-image-state-refine` | **Date**: 2026-07-26 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/025-desktop-image-state-refine/spec.md`, plus plan-phase directive: introduce a `FlowResultPart` to separate the operation-execution result (control channel) from the display `ToolResultPart` (FR-023..FR-026).

## Summary

Three independent fixes to the desktop↔agent↔LLM game path, plus one cross-cutting proto refinement that enables them cleanly:

1. **Window-select (Problem 1)** — collapse the redundant "bound window" layer (`App.boundWin`) so the selected window is the single source of truth for screenshots and operations (Constitution §II). Fixes the "no window bound" failure.
2. **Image-transfer hardening (Problem 2)** — switch the desktop↔gateway WebSocket leg from protojson text frames (base64 images, ~33% inflation) to **binary protobuf frames**, and fix the desktop `WSClient` read limit (currently the `coder/websocket` default of 32 KiB, which tears down the session on any image-bearing frame). Resolved in [research.md](./research.md) D1.
3. **Saolei text-state + validation (Problem 3)** — route screenshot recognition through `@dominion/game-saolei-board` (deterministic, agent-side); the saolei MCP returns a **text** board and validates each move **strictly** against the recognized state before dispatch.
4. **FlowResultPart (cross-cutting)** — introduce a control-channel `FlowResultPart` (a new `FlowPart` kind) for operation-execution results, so the screenshot travels control-channel and the model-facing display stays text-only for saolei (FR-023..FR-026). Completes 023's conversation/control decoupling.

## Technical Context

**Language/Version**: Go 1.x (desktop `projects/game/desktop`, gateway `projects/game/gateway`); TypeScript (agent `projects/game/agent`, library `projects/game/pkg/saolei-board`); Svelte + TypeScript (desktop frontend `projects/game/desktop/frontend`).

**Primary Dependencies**:
- Go: `github.com/coder/websocket` (WS transport), `google.golang.org/protobuf` (proto), Wails v2 (desktop shell), grpc-gateway v2 (gateway).
- TS: `@modelcontextprotocol/sdk` (MCP server), `@langchain/*` (agent loop), `pngjs` (PNG decode in saolei-board).
- **New agent dependency**: `@dominion/game-saolei-board` (workspace package, per `projects/game/pkg/saolei-board/README.md` → "External packages").

**Storage**: In-memory per-session state only — the recognized saolei board (`SaoleiBoard` instance in the per-session MCP server) and the LangChain checkpoint (`MemorySaver`) are co-located on the agent and lost together on restart (spec Assumption). No persistence layer.

**Testing**: `bazel test` (Go `*_test.go`, TS `vitest`, saolei-board golden tests); large tests via the testplan skill (`tools/test/guitar`) per `style/large_test.md`. The agent service is the large-test SUT.

**Target Platform**: Desktop client = Windows (Wails, Win32 capture/PostMessage); services (gateway/proxy/agent) = Linux containers.

**Project Type**: desktop-app (Wails) + web-services (gateway, proxy, agent) + shared library (saolei-board) + shared proto (`projects/game/game.proto`).

**Performance Goals**: Every image round-trip (user-turn screenshot + each post-action screenshot) must complete without frame-size failure, regardless of window size/DPI; recognition (saolei-board) is deterministic and fast (fixed-geometry color analysis, no OCR/CV).

**Constraints**:
- No oversized-frame failures on the desktop↔agent path (FR-007); no "no window bound" failure when a window is selected (FR-002); illegal saolei moves rejected before dispatch (FR-014/FR-015).
- Desktop-facing operation contract unchanged (`KeyboardPressPart{F2}` for `saolei_init`; `MouseMoveAndClickPart` at fixed geometry with `WINDOW_MESSAGE` for cell ops — FR-019/FR-020).

**Scale/Scope**: Single-user game sessions; one WS connection per session; screenshots up to ~10 MiB (the gateway's existing read limit).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution: `.specify/memory/constitution.md` v1.3.0.

| # | Principle | Status | Evidence |
|---|---|---|---|
| I | Citation & Provenance | PASS | All artifacts cite repo-relative paths (e.g. `projects/game/game.proto:403`) and external URLs (`coder/websocket` docs). No bare references. |
| II | Refactoring Over Patching | PASS | Problem 1 collapses the redundant `boundWin` layer (simplification). The `FlowResultPart` (FR-023..026) refactors the operation-result out of the display `tool_result` (architectural correction, not a patch). Problem 3 re-introduces recognized state by design (deterministic), reversing 023's stateless premise for the recognition dimension — a documented, scoped reversal. |
| III | Interface-First Design | PASS | Interfaces designed in Phase 1 before implementation: `FlowResultPart` proto + `FlowPart` oneop change ([contracts/flow-result-contract.md](./contracts/flow-result-contract.md)); saolei tool return + validation contract ([contracts/saolei-mcp-contract.md](./contracts/saolei-mcp-contract.md)); WS transport contract ([contracts/image-transport-contract.md](./contracts/image-transport-contract.md)); window-select contract ([contracts/window-select-contract.md](./contracts/window-select-contract.md)). |
| IV | Test Granularity & Cadence | PASS | Compile + unit (`bazel build`/`bazel test`) are part of each code task, not separate tasks. Large test (testplan skill) is the acceptance gate for the agent SUT. |
| V | Read Before Code | PASS (planned) | `tasks.md` (Phase 2) will declare per-phase doc lists in the three-category format, including `style/golang.md`, `style/javascript.md`, `style/api.md`, `style/large_test.md`, the saolei-board README, and `coder/websocket` docs. |
| VI | Large Test Acceptance for Services | PASS (planned) | The agent is a service → large test via testplan skill (`guitar run <plan.yaml>`, full deploy→test→cleanup); all cases must pass. Build-only checks do not constitute acceptance. |

No violations. No Complexity Tracking entries required.

## Project Structure

### Documentation (this feature)

```text
specs/025-desktop-image-state-refine/
├── plan.md                       # This file
├── research.md                   # Phase 0: transport decision, FlowResultPart, saolei integration
├── data-model.md                 # Phase 1: FlowResultPart proto, recognized-state, selected-window
├── quickstart.md                 # Phase 1: validation guide
├── contracts/
│   ├── flow-result-contract.md       # FlowResultPart proto + control/display channel separation
│   ├── saolei-mcp-contract.md        # text-board return + strict validation rules
│   ├── image-transport-contract.md   # binary-proto WS frames + read limits
│   └── window-select-contract.md     # selected window = single source of truth
└── tasks.md                      # Phase 2 (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                       # + FlowResultPart message; + flow_result kind in FlowPart oneof
├── desktop/
│   ├── app.go                       # remove boundWin; selected-window flows into ops+screenshot; executeAgentOperation returns FlowResultPart; handleInboundOperation sends flow_parts frame
│   ├── app_operation.go             # mouse executors take the selected window handle (not a.boundWin)
│   ├── internal/api/websocket.go    # WSClient: proto.Marshal/Unmarshal binary frames + SetReadLimit
│   └── frontend/src/App.svelte      # selectedWindowHandle drives screenshots/ops directly (no bind step)
├── gateway/cmd/main.go              # wsStream: proto.Marshal/Unmarshal binary frames (drop protojson on WS leg)
├── agent/
│   ├── src/operation-bridge.ts      # handleResult consumes FlowResultPart (not ToolResultPart)
│   ├── src/handler.ts               # route flow_parts frames; flow_result → bridge.handleResult
│   ├── src/mcp/saolei/saolei-mcp.ts # per-session SaoleiBoard; strict validation; return text board
│   └── package.json                 # + @dominion/game-saolei-board (workspace:*)
└── pkg/saolei-board/                # consumed (unchanged) — recognition engine
```

**Structure Decision**: No new project/directory. This feature edits the existing game projects (`desktop`, `gateway`, `agent`) and the shared proto, and consumes the existing `pkg/saolei-board` library. The four problem areas map to distinct files (see tree above), enabling independent phases.

## Complexity Tracking

> Not applicable — Constitution Check passes with no violations to justify.
