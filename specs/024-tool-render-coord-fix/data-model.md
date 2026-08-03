# Data Model: Tool Bubble Rendering & Saolei Coordinate Accuracy

**Feature**: [024-tool-render-coord-fix](./spec.md) | **Date**: 2026-07-26

This feature introduces **no proto change and no new entity**. It fixes two existing surfaces: the tool-bubble status interpretation (frontend) and the saolei grid→pixel geometry calibration (agent). This document specifies the corrected behavior of those two surfaces so implementation and tests have an exact reference. Decisions are grounded in [research.md](research.md) D1..D8.

## 1. Tool-result status — the three render states

A tool-result bubble resolves to exactly one of three render states, derived from the `ToolResultPart.status` field (`projects/game/desktop/frontend/src/api.ts`):

| Render state | Condition on `status` | Icon | Label | Colour |
|---|---|---|---|---|
| **succeeded** | `status` ∈ {`"TOOL_RESULT_STATUS_SUCCEEDED"`, `ToolResultStatus.SUCCEEDED` (1)} | `✓` | `succeeded` | success (green) |
| **failed** | `status` ∈ {`"TOOL_RESULT_STATUS_FAILED"`, `ToolResultStatus.FAILED` (2)} | `✗` | `failed` | failure (red) |
| **neutral** | `status` is **absent/undefined** OR `status` ∈ {`"TOOL_RESULT_STATUS_UNSPECIFIED"`, `ToolResultStatus.UNSPECIFIED` (0)} | neutral glyph (e.g. `›`) | `done` | neutral/muted |

The unresolved state (no `tool_result` MessagePart yet) keeps the existing **"running…"** treatment — it is distinct from the three resolved states above.

**The fix (D3)**: the **neutral** row is the corrected behavior. Today the renderer treats an absent/`UNSPECIFIED` status as **failed** (its `isToolResultSucceeded` returns false and the status-text ternary falls through to `"failed"`). Because protojson omits the zero-value `status` ([ProtoJSON — Presence and default-values](https://protobuf.dev/programming-guides/json/#presence)), a neutral (saolei/MCP) result arrives with `status` absent, so it MUST map to **neutral**, not failed. A pure classifier — e.g. `classifyToolResultStatus(status): "succeeded" | "failed" | "neutral"` in `projects/game/desktop/frontend/src/api.ts` — is the single source of truth the renderer reads (optional extraction; D3/D6). The classifier MUST accept the protojson enum-name string and the numeric enum form for each value, and MUST treat `undefined`/`null`/`""` as neutral.

**Status source (unchanged)**: the status is whatever the LLM message stream carries — `SUCCEEDED`/`FAILED` for native mouse tools (`buildToolResultMessage`, 023 Phase 3); neutral (`UNSPECIFIED`) for MCP (saolei) tools (023 C15/D12). No agent-side status change is part of this feature.

## 2. Tool bubble — render structure (unchanged shape, completed styling)

The bubble structure is unchanged from 023 ([spec 023 data-model.md §5](../023-saolei-mcp-refine/data-model.md)): one bubble per conversation-channel `tool_call.id`, showing the `tool_call` (name + args) first and updating in place with the `tool_result` (state from §1 + message + screenshot) when it arrives. **Defect 1a** is purely that the template classes (`.tool-bubble`, `.tool-head`, `.tool-name`, `.tool-args`, `.tool-result`, `.tool-pending`, `.tool-resolved-success`, `.tool-resolved-failure`) have **no CSS definitions** — the 023 refactor renamed the template but left the `<style>` block on the pre-refactor `.op-card`/`.op-result-*` names. The fix (D5) adds those rules, reusing the `.op-*` visual language (bordered box; monospace args; coloured outcome by §1 state). Live and history render identically (single source of truth — 023 FR-009).

## 3. Geometry — two coordinate spaces and the reconciliation

There are two pixel coordinate spaces in play for the bound Minesweeper window:

| Space | Origin | Includes non-client chrome? | Used by |
|---|---|---|---|
| **screenshot / full-window** | the window's outer top-left (`DwmGetWindowAttribute` `DWMWA_EXTENDED_FRAME_BOUNDS` / `GetWindowRect`) | **yes** (title bar, menu bar, borders) | `CaptureWindow` (`projects/game/desktop/internal/capture/capture.go`) — what the model sees |
| **client** | the client-area top-left (the `WM_*` `lParam` origin; excludes non-client chrome) | **no** | `WINDOW_MESSAGE` clicks (`runMouseMoveAndClick` → `ExecuteWindowMessageClick` → `makeLPARAM`) |

The board occupies the same physical cells in both spaces, but its top-edge offset differs by the non-client chrome height (96 px for the target Microsoft Minesweeper — operator-measured).

**The defect**: `BOARD_ORIGIN_Y_PX = 200` (`projects/game/agent/src/mcp/saolei/geometry.ts`, from [018 research.md D6](../018-saolei-mcp/research.md)) is the board top in the **screenshot** space; the `WINDOW_MESSAGE` click consumes it as a **client** coordinate, so every click drifts down by the chrome height (96 px ≈ 3 rows): grid `(4,4)` → client-y `344` → row 7 (the operator reported ~row 8).

**The fix (D1/D2)**: compensate the screenshot→client chrome **in the agent** (`geometry.ts`), keeping the screenshot-space layout constants and adding an explicit non-client-chrome offset, so `center(x,y)` yields the client-space cell centre the click path expects:

```
// board layout — X has no chrome compensation (left border is sub-cell);
// Y is screenshot-space, compensated to client space below.
BOARD_ORIGIN_X_PX            = 24
BOARD_ORIGIN_Y_PX_SCREENSHOT = 200              // matches the screenshot (018 D6)
CELL_SIZE_PX                 = 32
// non-client chrome height (screenshot↔client Y difference), operator-measured 96 px
CHROME_OFFSET_Y_PX           = 96
// client-space board origin (WM_* lParam space, chrome-excluded)
BOARD_ORIGIN_Y_PX            = BOARD_ORIGIN_Y_PX_SCREENSHOT − CHROME_OFFSET_Y_PX  // = 104

centerX(x) = BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2
centerY(y) = BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2
```

Worked example: `center(4,4) = (24 + 4·32 + 16, 104 + 4·32 + 16) = (168, 248)` — client-y `248` is exactly row 4's centre, so the click lands on `(4,4)`. The `center(x,y)` **signature and `CELL_SIZE_PX` are unchanged**; the change is the chrome compensation (one explicit constant). The screenshot capture is unchanged (the model uses grid coordinates, not screenshot pixels, to act — D1).

The compensation is **WINDOW_MESSAGE-only**: it lives in `center()`, which only the three saolei cell tools (all `method: WINDOW_MESSAGE`) call; `SIMULATED` operations consume screenshot-space coordinates and are NOT compensated (the desktop's `SIMULATED` path expects screenshot-relative input) — see [contracts/coordinate-space-contract.md](./contracts/coordinate-space-contract.md) §Compensation scope.

The exact origin values are window-layout-specific (018 D6 posture retained) and are finalized by the measurement step ([research.md](research.md) D2) and validated by the click-landing test ([quickstart.md](quickstart.md) Scenario 4).

## 4. State / lifecycle — unchanged

No state machine changes. The tool bubble lifecycle (call → resolve) and the saolei tool dispatch lifecycle (dispatch → bridge result → MCP content) are unchanged from 023. This feature only corrects (a) how a resolved status is *classified* for display and (b) the *constant* the geometry uses; neither alters a state transition.

## 5. Validation rules

- **Status classifier**: `classifyToolResultStatus(undefined)` → `"neutral"`; `("TOOL_RESULT_STATUS_UNSPECIFIED")` → `"neutral"`; `(0)` → `"neutral"`; `("")` → `"neutral"`; `("TOOL_RESULT_STATUS_SUCCEEDED")` / `(1)` → `"succeeded"`; `("TOOL_RESULT_STATUS_FAILED")` / `(2)` → `"failed"`. (Numeric and enum-name forms both accepted — protojson emits the name; defensive against the integer form.)
- **Geometry**: for the calibrated constants, every in-bounds `(x,y)` MUST yield a client coordinate inside cell `(x,y)`; a click at `center(x,y)` MUST land on cell `(x,y)` (manual Windows verification). Out-of-bounds coordinates still dispatch to whatever pixel the formula yields (023 accepted tradeoff — no validation re-added).
