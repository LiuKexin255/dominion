# Implementation Plan: Tool Bubble Rendering & Saolei Coordinate Accuracy

**Branch**: `024-tool-render-coord-fix` | **Date**: 2026-07-26 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/024-tool-render-coord-fix/spec.md` (two post-023 defects: the conversation tool bubble renders as unstyled text with a spurious "failed" status, and saolei cell clicks land on the wrong cell).

## Summary

Two targeted fixes for defects found after [023 — Conversation Content-Model Refactor & Saolei MCP Simplification](../023-saolei-mcp-refine/spec.md), both confirmed by reading the code (see [spec.md §Motivation & Root Cause](spec.md)):

1. **Tool bubble rendering (US1).** The 023 refactor renamed the `ChatView.svelte` tool-bubble template to `.tool-bubble`/`.tool-head`/`.tool-name`/`.tool-args`/`.tool-result`/`.tool-pending`/`.tool-resolved-*` classes but never updated the `<style>` block (it still holds the pre-refactor `.op-card`/`.op-result-card`/… rules the template no longer references) → the tool call renders as unstyled text. Independently, saolei (MCP) tool results carry the neutral status `TOOL_RESULT_STATUS_UNSPECIFIED` (proto enum 0); **protojson omits default-value fields** ([ProtoJSON spec — Presence and default-values](https://protobuf.dev/programming-guides/json/#presence)), so `status` is dropped on the wire and the renderer's success check + status-text ternary fall through to `"failed"`. Fix: add the missing `.tool-*` styles, and make the status judgment treat an absent/`UNSPECIFIED` status as **neutral** (never "failed"), reading from the status carried from the LLM message stream (neutral for saolei per 023 C15/D12; `SUCCEEDED`/`FAILED` for native mouse tools).

2. **Saolei coordinate accuracy (US2).** A `saolei_click(4,4)` dispatches pixel `(168,344)` but lands several rows too low (the operator reported ~cell `(4,8)`). Verified: the desktop's `WINDOW_MESSAGE` path posts the click at **exactly** the agent-supplied client coordinate — `app_operation.go` `runMouseMoveAndClick` → `ExecuteWindowMessageClick` → `makeLPARAM` apply **no** `ScreenshotToScreenCoords`, **no** `SetCursorPos`/`MoveCursor`, **no** foreground move (the user's "compounded displacement" hypothesis is refuted by the code). The real cause is a **coordinate-space mismatch**: the grid→pixel geometry `BOARD_ORIGIN_Y_PX=200` (`projects/game/agent/src/mcp/saolei/geometry.ts`, from [018 research.md D6](../018-saolei-mcp/research.md)) was calibrated to the **screenshot / full-window** space — `CaptureWindow` (`projects/game/desktop/internal/capture/capture.go`) captures the entire window via DWM extended-frame bounds (non-client chrome included). `WINDOW_MESSAGE` clicks operate in **client** coordinates (chrome excluded). The 96 px of non-client chrome (operator-measured) produces the systematic downward drift. Fix: compensate the screenshot→client chrome **in the agent** (`geometry.ts`) — keep the screenshot-space layout constants and subtract an explicit `CHROME_OFFSET_Y_PX` so `center(x,y)` yields a client coordinate the desktop posts verbatim (a refactor — reconcile the space at the source, do not layer an opposing offset in the desktop).

Technical approach and decisions are grounded in [research.md](research.md) (D1..D8); the geometry module contract in [contracts/coordinate-space-contract.md](contracts/coordinate-space-contract.md); data structures (tool-bubble status model + geometry coordinate spaces) in [data-model.md](data-model.md).

## Technical Context

**Language/Version**: TypeScript (agent, Node.js; `@langchain/core`, `@modelcontextprotocol/sdk` per `pnpm-workspace.yaml` catalog) + Go (desktop backend, Wails v2) + Svelte 5 (desktop frontend, Vite). proto3 (`projects/game/game.proto`).

**Primary Dependencies**:
- *Existing (agent TS)*: `@langchain/langgraph` (`MemorySaver`), `@langchain/core`, `zod`, `@modelcontextprotocol/sdk`.
- *Existing (desktop)*: Wails v2 runtime, raw Win32 (`user32.dll`, `dwmapi.dll`), `github.com/kbinani/screenshot`.
- *Existing (frontend)*: Svelte 5, `marked`, `dompurify`, Vite.
- *No new external dependency is introduced* by this feature. **No proto change.**

**Storage**: In-memory only. LangChain `MemorySaver` checkpoint is the sole persistence. Unchanged from 023.

**Testing**: `vitest` (agent TS unit/integration via the `vitest_test` macro — `style/javascript.md`), `go test` (desktop Go unit), large tests via the `testplan` skill (`style/large_test.md`). **The frontend package (`projects/game/desktop/frontend`) has no unit-test infrastructure** — its `BUILD.bazel` declares only `vite_build` (no `vitest_test`, no `*.test.ts`); Svelte changes are verified by `bazel build` + manual (consistent with the 023 assumption that the desktop client is build+unit(manual)+manual verified). The agent is a service → large tests REQUIRED for acceptance (Constitution §VI).

**Target Platform**: Agent = Linux container (deployed via `oci_image`). Desktop = Windows (Win32 input, Wails v2). The saolei click-landing acceptance (US2) requires a Windows host with Microsoft Minesweeper bound (a manual integration gate, as in 023 T020).

**Project Type**: web-service (agent) + desktop-app (desktop) — a multi-project change confined to the existing `projects/game/{agent/, desktop/}` tree. No new project.

**Performance Goals**: None. The status classification is a constant-time frontend check; the geometry change is arithmetic. No new latency.

**Constraints**:
- No proto change, no API change, no new external interface (the fixes are internal: a frontend renderer + an agent geometry constant calibration). The geometry module signature `center(x,y)` is unchanged — only the constants.
- The grid→pixel geometry MUST be calibrated in the **client** coordinate space (the space of the `WM_*` `lParam` the desktop posts). The desktop-facing `WINDOW_MESSAGE` operation Part is unchanged.
- protojson omits default-value fields; the frontend status handling MUST be robust to an absent `status` field, not assume presence (the requirement holds whether or not `EmitDefaults` is ever enabled).
- The chrome height is operator-measured at 96 px ([research.md](research.md) D2), so the client-space board top `BOARD_ORIGIN_Y_PX = 200 − 96 = 104` (`center(4,4) → (168, 248)`); `BOARD_ORIGIN_X_PX` stays 24 (left chrome is sub-cell). The compensation is applied in the agent (`geometry.ts`) — the operation originator — so the desktop receives a correct coordinate and posts it verbatim; validated by the click-landing test.

**Scale/Scope**: Single supervised session per operator. The change touches one Svelte component (+ optionally `api.ts`), one agent geometry module, and two test files (agent unit + large). No new project or external dependency.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Checked against `.specify/memory/constitution.md` (v1.3.0). All principles satisfied; no unjustified complexity. Quality gates (in execution order):

| # | Principle / Gate | Status | Evidence |
|---|---|---|---|
| 1 | **V — Read Before Code** (doc reading gate) | PASS (planned) | `tasks.md` (next command) MUST declare per-phase docs under the three mandatory categories (代码规范文档 / 官方文档 / 技术文章). Required reading includes `style/javascript.md`, `style/golang.md`, `style/large_test.md`, the ProtoJSON spec (default-value omission), the Win32 `WM_LBUTTONDOWN` client-coord doc, and this feature's contract/data-model. |
| 2 | **III — Interface-First Design** | PASS | Interface design precedes implementation: [contracts/coordinate-space-contract.md](contracts/coordinate-space-contract.md) fixes the geometry module contract (grid → **client** pixel; the coordinate-space reconciliation rule + the screenshot relationship), and [data-model.md](data-model.md) fixes the tool-bubble status model (succeeded/failed/neutral; absent → neutral). No new external/RPC/HTTP interface is introduced (internal module + renderer fixes only). |
| 3 | **II — Refactoring Over Patching** | PASS | Both fixes ARE the refactor, not patches: (US1) the tool-bubble styling + status logic are the incomplete artifacts of the 023 content-model refactor — completed, not special-cased; (US2) the coordinate-space mismatch is reconciled at the geometry layer (calibrate to client space), not patched with an opposing offset to "cancel" the drift. No new complexity is added; the changes simplify/complete existing surfaces. |
| 4 | **I — Citation & Provenance** | PASS | All design docs cite sources: repo-relative paths for internal refs, full URLs for external (ProtoJSON spec, Win32 `WM_LBUTTONDOWN` / `DwmGetWindowAttribute` docs). See References in [spec.md](spec.md) and each artifact. |
| 5 | **IV — Test Granularity & Cadence** | PASS (planned) | Compile (`bazel build //projects/game/...`) + unit (`bazel test //projects/game/...`) per code-change task, as part of the task — not separately tasked. The agent geometry change updates `saolei-mcp.test.ts`; the frontend change is build-verified (no frontend unit-test infra — see Testing). |
| 6 | **VI — Large Test Acceptance for Services** | PASS (planned) | The agent is a service; large tests REQUIRED and MUST be executed via the `testplan` skill (`guitar run projects/game/testplan/system_test.yaml`), not merely built. The `agent-saolei` suite (`projects/game/testplan/agent_saolei_test.go`) is UPDATED to assert the new client-space dispatch coordinates; all cases MUST pass. The US2 click-landing (does the click hit the intended cell?) is a manual Windows integration gate (as in 023 T020) — the large test verifies the **geometry** (dispatched coords), the manual test verifies the **landing**. |

**Post-Phase-1 re-check**: see the "Post-Phase-1 Constitution Re-check" section at the end of this file.

## Project Structure

### Documentation (this feature)

```text
specs/024-tool-render-coord-fix/
├── plan.md                          # This file
├── research.md                      # Phase 0 — decisions D1..D8
├── data-model.md                    # Phase 1 — tool-bubble status model + geometry coordinate spaces
├── quickstart.md                    # Phase 1 — validation scenarios 1..4
├── contracts/
│   └── coordinate-space-contract.md # Phase 1 — geometry module contract (grid → client pixel)
└── tasks.md                         # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── agent/                                  # TypeScript agent
│   ├── BUILD.bazel                         # gazelle regen if src layout changes (none expected)
│   └── src/
│       └── mcp/saolei/
│           ├── geometry.ts                 # FIX (US2): add CHROME_OFFSET_Y_PX (96, measured) and
│           │                               #   BOARD_ORIGIN_Y_PX = SCREENSHOT(200) − chrome(96) = 104 (client).
│           │                               #   center(x,y) now yields WM_* client coords; signature unchanged.
│           └── saolei-mcp.test.ts          # UPDATE: assert center() yields the new client-space coords.
│
├── desktop/                                # Go + Svelte desktop app
│   └── frontend/src/
│       ├── api.ts                          # (US1, optional) extract classifyToolResultStatus(status) pure fn
│       │                                   #   so the renderer + (future) tests share one definition.
│       └── components/
│           └── ChatView.svelte             # FIX (US1): add the missing .tool-bubble/.tool-head/.tool-name/
│                                              .tool-args/.tool-result/.tool-pending/.tool-resolved-* CSS
│                                              rules; status judgment treats absent/UNSPECIFIED as neutral.
│
└── testplan/                               # Large-test suites (Constitution §VI)
    └── agent_saolei_test.go                # UPDATE: assert the dispatched MouseMoveAndClickPart uses the new
                                                client-space coords (e.g. center(4,4) → (168, 248) with CHROME_OFFSET_Y_PX = 96).
```

**Structure Decision**: Multi-project change across the existing `projects/game/{agent/, desktop/}` tree. No new top-level project, no new external dependency, **no proto change**. The geometry module keeps its single file (`geometry.ts`) and its `center(x,y)` signature; only the origin constants change. The frontend fix is confined to `ChatView.svelte` (+ optionally `api.ts` for an extracted pure status classifier). Removed/added files: none mandatory (the optional `api.ts` helper is an in-place addition to an existing file).

## Complexity Tracking

> Not applicable — no Constitution Check violations to justify. US1 completes the incomplete artifacts of the 023 refactor (net simplification of the rendering surface). US2 reconciles a coordinate-space mismatch at the geometry layer rather than layering an offset (net simplification). No entries.

## Phase 0 / Phase 1 Outputs

- **Phase 0 — Research**: [research.md](research.md) (decisions D1..D8; all plan-time unknowns resolved — coordinate-space reconciliation, constant derivation, status-fix location, neutral visual, bubble-CSS approach, frontend testability, large-test impact, and the refuted "compounded displacement" hypothesis).
- **Phase 1 — Data model**: [data-model.md](data-model.md).
- **Phase 1 — Contract**: [contracts/coordinate-space-contract.md](contracts/coordinate-space-contract.md).
- **Phase 1 — Quickstart**: [quickstart.md](quickstart.md) (Scenarios 1..4).

## Next step

Run `/speckit.tasks` to generate `tasks.md`, phasing the implementation against the contract + data model above. Each phase's document list MUST follow the constitution's three mandatory categories (代码规范文档 / 官方文档 / 技术文章), and each code phase MUST include compile (`bazel build //projects/game/...`) + unit (`bazel test //projects/game/...`) as part of the task. The large-test suite (`agent-saolei`) is the agent acceptance gate and MUST be executed via the `testplan` skill (`guitar run projects/game/testplan/system_test.yaml`), not merely built — all cases MUST pass. The US2 click-landing is a manual Windows integration gate (recorded in the test record).

## Post-Phase-1 Constitution Re-check

Re-evaluated after producing [research.md](research.md), [data-model.md](data-model.md), [contracts/coordinate-space-contract.md](contracts/coordinate-space-contract.md), [quickstart.md](quickstart.md):

| Principle | Status | Notes |
|---|---|---|
| I — Citation & Provenance | PASS | Every decision in `research.md` and every contract clause cites a repo-relative path or full URL (ProtoJSON spec, Win32 `WM_LBUTTONDOWN` / `DwmGetWindowAttribute` docs, prior spec/contract/code paths). |
| II — Refactoring Over Patching | PASS | US1 completes the 023 refactor's leftover rendering artifacts (bubble CSS + status judgment aligned to the LLM-stream source). US2 reconciles the screenshot-vs-client coordinate space at the geometry layer (calibrate to client space), not by patching an opposing offset. No patch-over; net simplification. |
| III — Interface-First Design | PASS | The geometry module contract (`center(x,y)` → client pixel; coordinate-space reconciliation rule) and the tool-bubble status model are settled before implementation, in [contracts/coordinate-space-contract.md](contracts/coordinate-space-contract.md) and [data-model.md](data-model.md). |
| IV — Test Granularity & Cadence | PASS | Agent geometry change → `saolei-mcp.test.ts` (unit) + `agent_saolei_test.go` (large, via `guitar run`). Frontend change → `bazel build` (no frontend unit-test infra). quickstart Scenarios 1..2 are build/manual; Scenarios 3..4 are large-test + manual-Windows. |
| V — Read Before Code | PASS (planned) | Deferred to `tasks.md` (next command) — each phase MUST declare its three-category doc list. |
| VI — Large Test Acceptance for Services | PASS (planned) | The `agent-saolei` large-test suite is updated (not duplicated) and MUST be executed via `guitar run`; the US2 click-landing is a manual Windows gate (the large test verifies geometry, the manual test verifies landing). |

No design change introduced a constitution violation. No complexity-tracking entries needed.
