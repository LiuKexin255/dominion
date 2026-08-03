# Implementation Plan: Desktop Conversation Debug Mode

**Branch**: `022-desktop-debug-mode` | **Date**: 2026-07-24 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/022-desktop-debug-mode/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

## Summary

Add a developer-facing **debug mode** to the desktop conversation page (Svelte 5 + Wails v2), toggled by a single UI switch in the chat toolbar. Debug mode (1) emits a gated DEBUG log level from both the frontend logger and the desktop Go backend logger into the existing log panel, and (2) **holds each tool result on the desktop side** — after `handleInboundOperation` computes it but before it is sent to the agent — until the developer clicks a "Confirm" button on the result bubble (or the 15-minute auto-continue fires). The agent only waits normally; the pause is transparent (identical result/state as no pause). The agent service's dispatch timeout is raised from 5 s to 20 min as a backstop (20 > 15, so the desktop auto-continue always fires first). No state is persisted; all debug control-plane state is transient in-memory.

Technical approach (full detail in [research.md](./research.md), interface in [contracts/debug-control-plane.md](./contracts/debug-control-plane.md), entities in [data-model.md](./data-model.md), validation in [quickstart.md](./quickstart.md)):
- Frontend owns the toggle (`$state`); propagates to Go via a new bound method `SetDebugMode`.
- Confirm is a new bound method `ConfirmToolResult(toolID)`, keyed by the existing `tool_id`.
- The held indicator travels two new additive Wails events (`game:debug:result-held` / `result-released`); the result data itself still flows the unchanged, append-only SSE chat stream.
- Go holds via a `select` on confirm-channel / `time.After(15min)` / `ctx.Done()`; DEBUG logging is a zero-cost no-op when off (atomic flag in `applog` + frontend `logger`).
- Agent: one-constant change (`DISPATCH_TIMEOUT_MS` 5 s → 20 min).

## Technical Context

**Language/Version**: Go (desktop backend, `projects/game/desktop`) + TypeScript / Svelte 5 runes (frontend, `projects/game/desktop/frontend`).

**Primary Dependencies**: Wails v2 (`github.com/wailsapp/wails/v2`) — method binding + runtime events; Svelte 5 (`$state`/`$props`/`onclick`); protobuf-generated game types (`projects/game`). No new dependencies.

**Storage**: N/A — the feature is stateless by design (spec FR-010); all state is transient in-memory (debug flag, holds map, held-toolID set). No DB, no files, no persistence.

**Testing**: Go `testing` via `bazel test //projects/game/desktop/...` (unit tests for hold/debug logic; existing `app_test.go`, `view_model_test.go`); frontend `svelte-check` typecheck + `vite build` (no JS test runner exists in the repo — `projects/game/desktop/frontend/package.json`). Agent timeout validated by existing large tests at `projects/game/testplan/`.

**Target Platform**: Windows desktop (Wails v2 WebView2); single-user, single active session.

**Project Type**: desktop-app (Wails v2: Go backend + web frontend).

**Performance Goals**: Debug OFF = zero added overhead (DEBUG logging is a gated no-op; hold path is not entered). Debug ON = added latency up to the confirmation time (≤15 min) on the tool-result return only; all other paths unchanged.

**Constraints**: Hold must be transparent (identical result/state as no pause — FR-007); agent backstop 20 min must exceed desktop 15-min auto-continue (FR-013/FR-014); no persisted state (FR-010); SSE channel and `game:log` mechanism unchanged (FR-015).

**Scale/Scope**: 1 developer user, 1 active session, at most a few concurrently-held results (in practice one at a time — the blocking `recvLoop` serializes tool operations). Touches: 1 Go file cluster (`app.go`, `internal/applog/logger.go`), the frontend chat page (`App.svelte`, `ChatView.svelte`, `logger.ts`, `api.ts`, `main.ts`), and 1 agent constant (`operation-bridge.ts`).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Loaded from `.specify/memory/constitution.md` (v1.3.0). All gates pass; no violations to justify (Complexity Tracking left empty).

- **§I Citation & Provenance** — PASS. Every design artifact cites repository-relative paths or full URLs (spec References; research.md Provenance + each Decision; data-model.md; contracts §1–§6; quickstart.md). The plan itself links spec/research/data-model/contracts/quickstart.
- **§II Refactoring Over Patching** — PASS. The change extends the existing result-return path at its natural seam (`handleInboundOperation`) by reordering compute→append→hold→send when debug is ON, leaving the OFF path identical (FR-011). The DEBUG level is added to the existing loggers (`applog`, frontend `logger`) as a first-class gated level, not a scattered ad-hoc patch. The scope is the minimum that satisfies the requirements; no over-engineering.
- **§III Interface-First Design** — PASS. The frontend↔backend contract is designed before implementation in [contracts/debug-control-plane.md](./contracts/debug-control-plane.md): bound methods (`SetDebugMode`, `ConfirmToolResult`), event names + payload schemas (`game:debug:result-held`/`result-released`), the rendering rule, error/no-op semantics, and compatibility. The agent change is a constant (no new interface).
- **§IV Test Granularity & Cadence** — PASS (planned). Compile + Go unit tests run per change as part of development (`bazel build`/`bazel test` on affected targets; frontend `svelte-check` + `vite build`) — not separate tasks. Large tests are addressed under Gate 5 below.
- **Gate 5 / §VI Large-Test Acceptance** — PASS (scoped). Per `style/large_test.md`, large tests target gRPC/HTTP **services**. The desktop app is a client (out of mandate); it is validated by Go unit tests + frontend typecheck/build + the manual [quickstart.md](./quickstart.md). The only service touched is the agent (gRPC), whose dispatch behavior is already covered by existing large tests at `projects/game/testplan/` (`agent_operation_test.go`, `system_test.yaml`); the `DISPATCH_TIMEOUT_MS` constant change must keep those green. No new large-test plan is created (would violate `style/large_test.md` anti-pattern #1 "parallel test plans"); the agent timeout is validated by running the existing agent plan via the `testplan` skill.

## Project Structure

### Documentation (this feature)

```text
specs/022-desktop-debug-mode/
├── plan.md                        # This file (/speckit.plan output)
├── spec.md                        # (/speckit.specify output)
├── checklists/requirements.md     # (/speckit.specify output)
├── research.md                    # Phase 0 output (/speckit.plan)
├── data-model.md                  # Phase 1 output (/speckit.plan)
├── quickstart.md                  # Phase 1 output (/speckit.plan)
├── contracts/
│   └── debug-control-plane.md     # Phase 1 output (/speckit.plan) — frontend↔Go interface
└── tasks.md                       # Phase 2 output (/speckit.tasks — NOT created here)
```

### Source Code (repository root)

The feature modifies files in the existing desktop app + one agent constant. No new top-level directories.

```text
projects/game/desktop/
├── app.go                         # *App: add holds map + atomic debug flag; SetDebugMode/ConfirmToolResult bound methods; hold in handleInboundOperation
├── app_test.go                    # Go unit tests for hold/debug logic (extend)
├── internal/applog/logger.go      # add Debug() + atomic debug flag + SetDebug()
├── internal/applog/logger_test.go # (extend if exists)
└── frontend/src/
    ├── api.ts                     # WailsApp: add SetDebugMode/ConfirmToolResult; wrappers
    ├── App.svelte                 # debug switch ($state), game:debug:* listeners, heldToolIds, wire ChatView
    ├── main.ts                    # (optionally) register game:debug:* listeners, or keep in App.svelte onMount
    ├── logger.ts                  # add gated debug level (setDebugEnabled / logDebug)
    └── components/
        ├── ChatView.svelte        # render "Confirm" control on held toolResult parts (heldToolIds prop + onConfirm)
        └── LogPanel.svelte        # add log-debug style

projects/game/agent/
└── src/operation-bridge.ts        # DISPATCH_TIMEOUT_MS: 5_000 → 1_200_000 (20 min)
```

**Structure Decision**: Extend the existing single Wails desktop project (`projects/game/desktop`) — no new apps/packages. The frontend and Go backend already coexist there and communicate via Wails bindings + runtime events, which is exactly the transport this feature reuses (research.md "Provenance"). The one agent-side constant lives where it already does (`projects/game/agent/src/operation-bridge.ts`). This is the minimal structure; introducing new modules would violate Constitution §II (over-engineering).

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No Constitution violations. (Empty.)
