# Research: Tool Bubble Rendering & Saolei Coordinate Accuracy

**Feature**: [024-tool-render-coord-fix](./spec.md) | **Date**: 2026-07-26

This document records the design decisions for the two post-023 defects. Each was investigated by reading the code (root causes are cited in [spec.md §Motivation & Root Cause](spec.md)); this research settles the fix approach and resolves all plan-time unknowns. No `[NEEDS CLARIFICATION]` items remain.

## D1. Coordinate-space reconciliation — calibrate the geometry to the CLIENT space

**Decision**: Apply the screenshot→client **chrome compensation in the agent**, in `projects/game/agent/src/mcp/saolei/geometry.ts` (the saolei MCP module, used by the agent service — the operation originator). Keep the screenshot-space board-layout constants unchanged (they match the screenshot/visual layout), introduce an explicit **non-client-chrome offset** constant, and have `center(x,y)` yield **client coordinates** directly — so the coordinate dispatched to the desktop is already correct and the desktop applies no conversion (it posts the value verbatim, per D8). `center(x,y)` signature and `CELL_SIZE_PX` are unchanged; the screenshot capture (`projects/game/desktop/internal/capture/capture.go` `CaptureWindow`) remains full-window.

Concretely (final values per D2):

```ts
// board layout — X has no chrome compensation (left border is sub-cell);
// Y is screenshot-space and compensated to client space below.
const BOARD_ORIGIN_X_PX = 24;
const BOARD_ORIGIN_Y_PX_SCREENSHOT = 200;   // matches the screenshot (018 D6)
const CELL_SIZE_PX = 32;
// non-client chrome height: the Y difference between the screenshot (full-window)
// and client (WM_* lParam) coordinate spaces. Measured 96 px on the target
// Minesweeper (title bar + menu bar + borders). Window-layout-specific.
const CHROME_OFFSET_Y_PX = 96;
// client-space board origin (WM_* lParam space, chrome-excluded) = screenshot − chrome
const BOARD_ORIGIN_Y_PX = BOARD_ORIGIN_Y_PX_SCREENSHOT - CHROME_OFFSET_Y_PX; // = 104
```

**Rationale**: The only consumer of `center(x,y)` is the saolei tool dispatch into a `MouseMoveAndClickPart` whose `WINDOW_MESSAGE` path packs the coordinate verbatim into the `WM_*` `lParam` (client space) — see D8. The model acts via grid coordinates `(x,y)` and does **not** need pixel-accurate screenshot↔grid mapping to play, so making the geometry client-accurate is sufficient and minimal. This is a **refactor over a patch** (Constitution §II): reconcile the coordinate space at the source, do not layer an opposing offset to "cancel" the drift.

**Scope (invariant — the compensation is WINDOW_MESSAGE-only)**: the chrome compensation applies **only to `WINDOW_MESSAGE` operations**. `SIMULATED` operations (the generic `mouse_move` / `mouse_click` / `mouse_move_and_click` tools, whose `method` defaults to `SIMULATED`) consume **screenshot-space** coordinates — the desktop's `SIMULATED` branch adds the window origin via `ScreenshotToScreenCoords` (`screenX = screenshotX + windowLeft`) to reach screen-absolute coords, so compensating them would shift the click *up* by the chrome height and miss. The current design satisfies this **structurally**: `center()` lives in `mcp/saolei/geometry.ts` and is called **only** by the three saolei cell tools (`saolei_click` / `saolei_flag` / `saolei_chord_click`), all of which dispatch `method: WINDOW_MESSAGE`; the generic mouse tools do **not** call `center()` (verified — `projects/game/agent/src/tools/mouse_click/mouse-click.ts` and `tools/mouse_move/mouse-move.ts` import neither `geometry` nor `center`; their coordinates come straight from the model's tool input, in screenshot space). **Keep it that way**: do not generalize the compensation into the desktop's shared `runMouseMoveAndClick` (both branches would break `SIMULATED`); do not route the generic mouse tools through `center()`; and keep the saolei cell tools on `WINDOW_MESSAGE` (`center()` yields client coords, which would be wrong for the `SIMULATED` path).

**Alternatives considered**:
- *Compensate in the desktop (`runMouseMoveAndClick` subtract the chrome height)*: rejected — the user decision is that the compensation belongs in the agent/saolei-mcp (the operation originator that dispatches the mouse op), so the desktop receives a correct coordinate and applies no conversion. Patching the desktop would burden every mouse consumer with a saolei-specific hack and leave the geometry wrong for any future caller.
- *Capture only the client area in `CaptureWindow` (so screenshot == client)*: rejected as the primary fix — it does not avoid the chrome compensation (the board top in a client-only screenshot is still 104 px, not 200), and it changes what the model sees (shifting the screenshot the saolei skill/prompts were tuned against) for no play benefit. Left as a future consideration if pixel-accurate screenshot↔grid mapping is ever needed.
- *Dynamically derive the board origin from the window structure*: rejected — Minesweeper's internal layout (smiley/timer panel height) is not exposed by Win32; it is application-drawn. 018 D6 already accepted hardcoded, window-layout-specific constants; this feature keeps that posture and corrects the value.

## D2. Deriving the client-space board-origin constants

**Decision**: The non-client-chrome height is **96 px** (operator-measured on the target Microsoft Minesweeper: title bar + menu bar + borders). The compensation is therefore:

- `CHROME_OFFSET_Y_PX = 96` (measured; window-layout-specific).
- Client-space board top `BOARD_ORIGIN_Y_PX = BOARD_ORIGIN_Y_PX_SCREENSHOT − CHROME_OFFSET_Y_PX = 200 − 96 = 104`.
- Worked check: `center(4,4) = (24 + 4·32 + 16, 104 + 4·32 + 16) = (168, 248)` — client-y `248` is exactly the centre of row 4 (`104 + 4·32 + 16`), so the click lands on `(4,4)`. (Pre-fix, `center(4,4)` emitted `344`; in the client space `344 = 104 + 7·32 + 16`, i.e. row 7's centre — consistent with the operator's report of the click landing several rows too low.)
- `BOARD_ORIGIN_X_PX = 24` (screenshot-space) is retained as-is for now: the left non-client chrome is only the window border (~3 px), a sub-cell error that does not change the column. If a left-border compensation is later wanted, add a `CHROME_OFFSET_X_PX` symmetrically; it is a measurement-time confirmation, not a blocking unknown.
- `CELL_SIZE_PX = 32` is unchanged (cells are 32×32 px in both spaces).

**Why keep the chrome as a separate constant** (not bake 104 directly into `BOARD_ORIGIN_Y_PX`): the operator noted different windows may have different chrome heights. A standalone `CHROME_OFFSET_Y_PX` makes the screenshot→client conversion an explicit, individually-tunable step (one number to change per window layout), while the screenshot-space layout constants stay aligned with what the model/operator sees. `geometry.ts` remains the single coordinate source — `saolei-mcp.ts` calls `center()` unchanged.

**Measurement method** (confirms 96 / 104 on the target window): with the Minesweeper bound, either measure the non-client height directly (window-rect top − client-rect top) or probe a known cell. The click-landing test (quickstart Scenario 4) is the validation: `saolei_click(4,4)` must land on `(4,4)`.

**Rationale**: 96 px is the operator's measured value (ground truth), superseding the earlier ~128 px estimate inferred from the reported row drift. Keeping all constants in one place (`geometry.ts`) means a re-tune never touches the tool contracts.

**Note (DPI)**: if the system runs at non-100% DPI scaling, the WM `lParam` coordinates and the screenshot pixels may be in different DPI spaces. The measurement step MUST confirm the WM client coords match the geometry's pixel space (i.e. the click lands correctly under the operator's actual DPI); if a DPI factor is present it is folded into the calibrated constant. This is a measurement-time check, not a separate code path.

## D3. Tool-result status fix — treat absent / UNSPECIFIED as neutral (frontend)

**Decision**: The tool-result status judgment is fixed in the **frontend renderer** (`projects/game/desktop/frontend/src/components/ChatView.svelte`, optionally with a pure helper extracted to `projects/game/desktop/frontend/src/api.ts`). A result whose `status` is **absent** (protojson default-omission of enum-zero) OR explicitly `TOOL_RESULT_STATUS_UNSPECIFIED` MUST classify as **neutral** — never as "failed". `SUCCEEDED` → succeeded; `FAILED` → failed (no regression for native mouse tools, which always carry a non-zero status).

**Rationale**: The agent already carries the correct status — neutral for saolei (MCP) tools (023 C15/D12; `consumeToolResults` in `projects/game/agent/src/llm.ts` and `readToolResultStatus` in `projects/game/agent/src/handler.ts` both default to `STATUS_UNSPECIFIED`) and `SUCCEEDED`/`FAILED` for native tools. The defect is purely that protojson omits the zero-value `status` on the wire and the renderer's success check (`isToolResultSucceeded`) + status-text ternary treat an absent field as failure. Per the user's note ("failed/success 判断逻辑是否也应该同步修改"), the judgment is aligned to the status actually carried from the LLM message stream.

**Alternatives considered**:
- *Enable protojson `EmitDefaults` so `UNSPECIFIED` is always present*: rejected as the fix — it touches serialization globally, and the renderer must still handle a neutral value correctly. The renderer-side fix is robust whether or not defaults are emitted (and is the smaller, targeted change).
- *Change the agent to emit a non-zero "neutral" status*: rejected — `UNSPECIFIED` IS the correct neutral status (023 FR-014: a result whose real status is unavailable MUST be neutral); inventing a new status would re-open the proto and the 023 status contract.

**Protojson evidence**: the [ProtoJSON spec — Presence and default-values](https://protobuf.dev/programming-guides/json/#presence) states: *"If the field doesn't support field presence and has the default value … serializers should omit it from the output."* `ToolResultPart.status` is a plain proto3 enum (no `optional`, no field presence), so `status = UNSPECIFIED (0)` is omitted → the frontend observes `undefined`.

## D4. Neutral-status visual — a neutral marker, distinct from success and failure

**Decision**: A resolved tool result with neutral status renders with a **neutral** indicator and label — visually distinct from both success (✓ green) and failure (✗ red), e.g. a neutral glyph (such as `›` or `·`) + the label `"done"` (or equivalent) in a muted/neutral colour. The result message + screenshot are still shown (the actual outcome is conveyed by the message text + screenshot, per 023 C7/C8). The existing "running…" state for an unresolved call (no result yet) is unchanged.

**Rationale**: "pending" (the current label for `UNSPECIFIED`) is misleading for a *resolved* neutral result — the result has arrived, the operation ran; only the structured status is unclassified (the real outcome is in the message/screenshot). A neutral "done" state conveys "completed; status not classified as success/failure" without implying failure. The exact glyph/label is a small presentation detail finalized in [data-model.md](data-model.md); the requirement is "neutral, not failed".

**Alternatives considered**:
- *Show no status badge at all for neutral (message + screenshot only)*: acceptable but gives no at-a-glance outcome cue; a small neutral marker is clearer.
- *Keep "pending"*: rejected — implies the result has not arrived, which is wrong once the tool_result MessagePart is present.

## D5. Tool-bubble styling — add the missing `.tool-*` CSS rules

**Decision**: Add the missing CSS rules for the tool-bubble classes the 023 template already uses — `.tool-bubble`, `.tool-head`, `.tool-name`, `.tool-args`, `.tool-result`, `.tool-pending`, `.tool-resolved-success`, `.tool-resolved-failure` — in `ChatView.svelte`'s `<style>` block, reusing the visual language of the pre-refactor `.op-card` / `.op-result-card` / `.op-result-success` / `.op-result-failure` rules (bordered box, monospace coords/args, coloured outcome). The template's class names are kept (they are semantically correct for the tool bubble); only the stylesheet is completed.

**Rationale**: The defect is a stylesheet-completion gap from the 023 refactor (the template was renamed but the `<style>` was not). Reusing the existing `.op-*` visual language keeps the bubble consistent with the prior look (a bordered operation card) with the least churn. A repo-wide search (`rg "\.tool-bubble|\.tool-head|\.tool-name|\.tool-args|\.tool-result|\.tool-pending|\.tool-resolved"` in `ChatView.svelte`) confirms **zero** matching CSS rules today — the template references classes that have no definitions.

**Alternatives considered**:
- *Rename the template classes back to `.op-*` to match the existing CSS*: rejected — `.tool-*` is the more correct name (these are tool-call bubbles, not generic operation cards), and adding CSS is lower-risk than re-renaming the template (which could miss a reference). The old `.op-*` rules that are now unreferenced MAY be removed as cleanup, but that is optional and separate.

## D6. Frontend testability — build + manual (no frontend unit-test infra)

**Decision**: The frontend change (`ChatView.svelte` / `api.ts`) is verified by `bazel build //projects/game/desktop/...` (the `vite_build` compiles the Svelte) + manual visual verification. No frontend unit test is added because the frontend package has **no unit-test infrastructure**: its `BUILD.bazel` declares only `vite_build` (no `vitest_test`, no `*.test.ts`); `projects/game/desktop/frontend/**/*.test.ts` finds none. The **status correctness** (the agent carries neutral for saolei) is covered by agent unit tests + the large test; the **rendering** (bubble styling, neutral visual) is manual.

**Rationale**: Setting up a `vitest_test` target for the frontend is a separate infrastructure effort out of scope for a bug-fix. This matches the 023 assumption ("the desktop client is verified by build + unit + manual per `style/large_test.md`") — there, "unit" is the Go side (`*_test.go`); the Svelte rendering is manual. The optional `api.ts` `classifyToolResultStatus(status)` pure helper (D3) makes the logic single-sourced and unit-testable *if* frontend testing is added later, but adding the test target itself is not part of this feature.

**Alternatives considered**:
- *Add a `vitest_test` target to the frontend package and unit-test `classifyToolResultStatus`*: valuable but scope creep (new tooling for a bug-fix). Deferred.

## D7. Large-test impact — update saolei coord assertions + re-run

**Decision**: Changing the geometry constants changes the `MouseMoveAndClickPart` coords the agent dispatches for a given grid `(x,y)`, so two test files MUST be updated and the large test re-executed:
- `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts` (unit) — the dispatch-coordinate assertions move to the new client-space values (e.g. `center(4,4)` → `(168, 248)` with `CHROME_OFFSET_Y_PX = 96` ⇒ `BOARD_ORIGIN_Y_PX = 104`).
- `projects/game/testplan/agent_saolei_test.go` (large, `agent-saolei` suite) — same coordinate assertions, run via `guitar run projects/game/testplan/system_test.yaml` (Constitution §VI; all cases MUST pass).

The large test verifies the **geometry** (the agent dispatches the agreed client-space coords). The **click-landing** (does that coord hit cell `(4,4)` on a real Minesweeper?) is a manual Windows integration gate (as in 023 T020), because no CI environment has a Minesweeper window. Both are required for US2 acceptance.

**Rationale**: The agent is the service SUT; its large test MUST be executed (not merely built) and all cases MUST pass (Constitution §VI v1.3.0). The desktop click-landing is inherently a Windows-host verification.

## D8. Refuted hypothesis — there is NO compounded mouse-displacement logic

**Decision (finding)**: The user's hypothesis ("检查是否叠加了其他鼠标位移的逻辑，应当去掉") was investigated and **refuted** — the `WINDOW_MESSAGE` execution path posts the click at exactly the agent-supplied client coordinate, with no compounding. The fix is therefore at the geometry layer (D1/D2), not by removing desktop-side displacement logic.

**Evidence** (read from the code):
- `projects/game/desktop/app_operation.go` `runMouseMoveAndClick` — the `MOUSE_INPUT_METHOD_WINDOW_MESSAGE` branch calls only `operation.ExecuteWindowMessageClick(a.boundWin.Handle, part.GetClick(), part.GetXPx(), part.GetYPx())`. It does **not** call `ScreenshotToScreenCoords`, `MoveCursor`/`SetCursorPos`, or `logSetForeground` (those are the `SIMULATED`/default branch only).
- `projects/game/desktop/internal/operation/window_message_windows.go` `ExecuteWindowMessageClick` → `makeLPARAM(clientX, clientY)` packs the coordinate into the `WM_*` `lParam` via `PostMessageW`. No cursor movement.
- `projects/game/desktop/internal/operation/window_message_logic.go` `makeLPARAM` — `uintptr(uint16(x)) | (uintptr(uint16(y)) << 16)` (the MAKELPARAM macro); no offset.

So client-y `344` is posted verbatim; in the **client** coordinate space (board top = 104 once the 96 px chrome is excluded) `344 = 104 + 7·32 + 16` — the centre of row 7, several rows below the intended row 4 (the operator reported ~row 8; the precise row depends on the exact 200/96 values, both of which carry measurement error — the fix is the chrome compensation, validated by the click-landing test). The missing chrome compensation in the geometry is the sole source of the drift. This finding is recorded so the implementation does not hunt for desktop displacement logic to remove.

**External references consulted**:
- ProtoJSON default-value omission — https://protobuf.dev/programming-guides/json/#presence (and JSON Options: "Always emit fields without presence").
- Win32 `WM_LBUTTONDOWN` `lParam` = client coordinates — https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-lbuttondown
- `DwmGetWindowAttribute` (`DWMWA_EXTENDED_FRAME_BOUNDS`) returns full-window bounds incl. non-client chrome vs `GetClientRect` — https://learn.microsoft.com/en-us/windows/win32/api/dwmapi/nf-dwmapi-dwmgetwindowattribute
