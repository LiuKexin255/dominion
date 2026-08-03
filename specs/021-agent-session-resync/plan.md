# Implementation Plan: Agent Session Resync & Adapter Simplification

**Branch**: `021-agent-session-resync` | **Date**: 2026-07-24 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/021-agent-session-resync/spec.md` (incl. clarifications C1–C4) plus the plan-input decisions recorded in [research.md](research.md) (D1–D8).

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

## Summary

Four optimizations to the `018-saolei-mcp` implementation that make the desktop↔agent session resilient on reconnect and simplify the agent's adapter lifecycle:

1. **Status ping-pong** — on session (re-)entry the desktop's existing connect-time status probe is reused; the agent now reports its real working state (`ACTIVE` if a turn is in-flight, else `IDLE`/`UNSPECIFIED`), and the desktop reconciles its "Agent is typing…" indicator against the response. Fixes the stuck-typing-after-reconnect defect.
2. **Stream-scoped sink lifecycle** — `OperationBridge` sink registration/unregistration become compare-and-delete (stream-owned), so a stale stream close can no longer clobber a fresh reconnect's sink. Fixes the "all MCP tools fail after reconnect" defect.
3. **Display-only tool results** — `saolei_update` (agent-internal) forwards a display-only `ToolResultPart` via a new `OperationBridge.pushResult`; the desktop renders it unchanged and performs no input action.
4. **Adapter simplification + profile guard** — `SessionAgent.getOrCreateAdapter` no longer rebuilds on a profile-name mismatch; a turn whose resolved profile differs from the bound adapter is rejected (warn+wait, non-fatal) before it runs; Refresh is the sole rebuild path.

Technical approach and root-cause analysis are grounded in [research.md](research.md); interface contracts in [contracts/](contracts/); state shapes in [data-model.md](data-model.md).

## Technical Context

**Language/Version**: TypeScript (agent, Node.js) + Go (desktop) + Svelte (desktop frontend); proto3 (`projects/game/game.proto`).

**Primary Dependencies**:
- *Existing (agent TS)*: `langchain`, `@langchain/langgraph`, `@grpc/grpc-js`, `@grpc/proto-loader`, `zod`. **No new dependency** is introduced.
- *Desktop (Go)*: existing Wails bridge + WebSocket client. **No new dependency.**

**Storage**: In-memory only (session-scoped adapter/bridge state). N/A for databases.

**Testing**: `vitest` (agent TS unit/integration), `go test` (desktop unit), large tests via the `testplan` skill (`style/large_test.md`). Compile + unit tests run per code change as part of each task (not separately tasked).

**Target Platform**: Agent = Linux container; Desktop = Windows (the reconnect/resync behaviors are platform-agnostic and apply to the WS/gRPC channel regardless of OS).

**Project Type**: web-service (agent) + desktop-app (desktop) — a multi-project, behavioral change.

**Performance Goals**: The status ping-pong adds one localhost round trip at connect time (already present as the connect probe; only its response is now consumed). No new latency target. The supervised single-session loop imposes no throughput target.

**Constraints**: No proto schema change (research.md D8 — all reuse `StatusSignal`/`ToolResultPart`/`WarnSignal`/`WaitSignal`). Backward compatible — behaviors are additive over the existing channel; the one removal (implicit profile-switch) is replaced by an explicit guard + Refresh, which the desktop already calls after profile changes.

**Scale/Scope**: Single supervised session per human operator. Not a multi-tenant/high-concurrency service.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Checked against `.specify/memory/constitution.md` (v1.2.0). All principles satisfied; no unjustified complexity. Quality gates (in execution order):

| # | Principle / Gate | Status | Evidence |
|---|---|---|---|
| 1 | **V — Read Before Code** (doc reading gate) | PASS (planned) | `tasks.md` (next command) MUST declare per-phase docs under the three mandatory categories (代码规范文档 / 官方文档 / 技术文章). Required reading includes `style/javascript.md`, `style/golang.md`, `style/large_test.md`, and this feature's contracts. |
| 2 | **III — Interface-First Design** | PASS | Behavioral contracts settled BEFORE implementation: [contracts/agent-desktop-channel-contract.md](contracts/agent-desktop-channel-contract.md) (status ping-pong + display tool result) and [contracts/agent-session-lifecycle-contract.md](contracts/agent-session-lifecycle-contract.md) (sink ownership, adapter lifecycle, profile guard, status derivation). |
| 3 | **II — Refactoring Over Patching** | PASS | The reconnect defect is fixed by correcting the sink ownership model (compare-and-delete), not by patching around the race. The adapter simplification *removes* the implicit-switch branch (simplification when over-designed for the new guard-based model). No new proto; no parallel mechanism. |
| 4 | **I — Citation & Provenance** | PASS | All design docs cite sources: repo-relative paths for internal refs (`projects/game/agent/src/...`, `projects/game/desktop/app.go`, `projects/game/game.proto`), and the root-cause findings reference exact code sites. See References sections of `spec.md`, `research.md`, and each contract. |
| 5 | **IV — Test Granularity & Cadence** | PASS (planned) | Compile (`bazel build`) + unit tests (`bazel test`) per code-change task; not separately tasked. [quickstart.md](quickstart.md) Scenarios 1–5 are unit/integration; Scenarios 6–7 are large tests. |
| 6 | **VI — Large Test Acceptance for Services** | PASS (planned) | The agent is a service; large tests REQUIRED for acceptance. [quickstart.md](quickstart.md) Scenario 6 (reconnect resilience) and Scenario 7 (Refresh-only profile switch) are large-test plans under `testplan/`, executed via the `testplan` skill. |

**Post-Phase-1 re-check**: Phase 1 produced `data-model.md`, both contracts, and `quickstart.md`. No design change introduced a constitution violation. Interface-first (Principle III) holds — contracts precede the future `tasks.md`. The proto-no-change finding (D8) keeps the change purely behavioral and backward compatible. No complexity-tracking entries needed.

## Project Structure

### Documentation (this feature)

```text
specs/021-agent-session-resync/
├── plan.md                                   # This file
├── research.md                               # Phase 0 — decisions D1..D8 (root-cause + design)
├── data-model.md                             # Phase 1 — status derivation, sink lifecycle, adapter lifecycle, guard FSM
├── quickstart.md                             # Phase 1 — validation scenarios 1..7
├── contracts/
│   ├── agent-desktop-channel-contract.md     # Phase 1 — status ping-pong + display tool result
│   └── agent-session-lifecycle-contract.md   # Phase 1 — sink ownership, adapter, profile guard, status derivation
└── tasks.md                                  # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                                # UNCHANGED (no schema change — research.md D8)
│
├── agent/                                     # TypeScript agent
│   ├── BUILD.bazel                           # gazelle regen if new src files
│   └── src/
│       ├── operation-bridge.ts               # EXTEND: stream-scoped sink handle (registerSink→handle,
│       │                                     #         unregisterSink compare-and-delete); NEW pushResult(part)
│       ├── session-agent.ts                  # SIMPLIFY: getOrCreateAdapter removes rebuild-on-mismatch
│       ├── handler.ts                        # EXTEND: status derives ACTIVE/IDLE; profile guard before mutex;
│       │                                     #         cleanupSinks passes per-stream sink handle
│       └── mcp/saolei/
│           └── saolei-mcp.ts                 # EXTEND: saolei_update forwards display ToolResultPart (pushResult)
│
├── desktop/                                   # Go + Svelte desktop app
│   ├── app.go                                # EXTEND: ConnectAgent returns probe response status
│   └── frontend/src/
│       ├── api.ts                            # EXTEND: connectAgent returns StatusSignalStatus
│       └── App.svelte                        # EXTEND: resetPlayPageState resets processing; reconcile
│                                             #         processing/playState from returned status on entry
│
└── (testplan/)                               # NEW large-test plans (Phase 2 / implementation): Scenarios 6 & 7
```

**Structure Decision**: A behavioral change across the existing `projects/game/{game.proto (unchanged), agent/, desktop/}` tree. No new project, no new dependency, no proto change. All edits are extensions/simplifications of existing components per the contracts.

## Complexity Tracking

> Not applicable — no Constitution Check violations to justify. The change is purely behavioral over the existing message set (no proto change); the sink fix corrects an ownership model rather than adding parallel state; the adapter change *removes* a branch (simplification). No entries.

## Phase 0 / Phase 1 Outputs (complete)

- **Phase 0 — Research**: [research.md](research.md) (decisions D1–D8, root-cause analysis for both reconnect defects, all spec unknowns resolved).
- **Phase 1 — Data model**: [data-model.md](data-model.md).
- **Phase 1 — Contracts**: [contracts/agent-desktop-channel-contract.md](contracts/agent-desktop-channel-contract.md), [contracts/agent-session-lifecycle-contract.md](contracts/agent-session-lifecycle-contract.md).
- **Phase 1 — Quickstart**: [quickstart.md](quickstart.md) (Scenarios 1–7).

## Next step

Run `/speckit.tasks` to generate `tasks.md`, phasing the implementation against the contracts above. Each phase's document list MUST follow the constitution's three mandatory categories (代码规范文档 / 官方文档 / 技术文章), and each code phase MUST include compile (`bazel build //...`) + unit (`bazel test //...`) as part of the task, with the large-test scenarios (6, 7) as the acceptance gate.
