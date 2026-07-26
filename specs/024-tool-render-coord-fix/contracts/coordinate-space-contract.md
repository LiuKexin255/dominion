# Contract: Saolei Grid→Pixel Coordinate Space

**Feature**: [024-tool-render-coord-fix](../spec.md) | **Date**: 2026-07-26

This contract fixes the interface of the saolei grid→pixel geometry module so that saolei cell clicks land on the intended cell. It is the Phase-1 interface design (Constitution §III) for US2. Decisions are grounded in [research.md](../research.md) D1/D2/D8; the corrected behavior is specified in [data-model.md](../data-model.md) §3.

## 1. Module under contract

`projects/game/agent/src/mcp/saolei/geometry.ts` — `center(x, y)` and the constants `BOARD_ORIGIN_X_PX`, `BOARD_ORIGIN_Y_PX`, `CELL_SIZE_PX`.

## 2. The contract

`center(x, y)` MUST return the centre of cell `(x, y)` in **window-client coordinates** — the coordinate space of the `WM_*` `lParam` consumed by the desktop's `MOUSE_INPUT_METHOD_WINDOW_MESSAGE` click path. This is the space whose origin is the bound window's client-area top-left and which **excludes** the non-client chrome (title bar, menu bar, borders).

```
centerX(x) = BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2
centerY(y) = BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2
```

- `BOARD_ORIGIN_X_PX`, `BOARD_ORIGIN_Y_PX` — the board's top-left edge offset **in client coordinates** (calibrated against the target Microsoft Minesweeper; see §4).
- `CELL_SIZE_PX = 32` — unchanged; cells are 32×32 px.
- **Signature unchanged**: `center(x: number, y: number): { xPx: number; yPx: number }`.

## 3. Coordinate-space reconciliation rule (the fix)

The constants MUST be calibrated to the **client** space, NOT the screenshot / full-window space. Concretely:

- The screenshot capture (`projects/game/desktop/internal/capture/capture.go` `CaptureWindow`) captures the **full window** (bounds from `DwmGetWindowAttribute` `DWMWA_EXTENDED_FRAME_BOUNDS` / `GetWindowRect` — non-client chrome included). Its coordinate space's origin is the window's outer top-left.
- The `WINDOW_MESSAGE` click path (`projects/game/desktop/app_operation.go` `runMouseMoveAndClick` → `projects/game/desktop/internal/operation/window_message_windows.go` `ExecuteWindowMessageClick` → `makeLPARAM`) consumes the coordinate as a **client** coordinate in the `WM_*` `lParam` ([WM_LBUTTONDOWN — lParam is the client coords](https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-lbuttondown)), with **no** `ScreenshotToScreenCoords`, **no** `SetCursorPos`/`MoveCursor`, **no** foreground move applied (verified — [research.md](../research.md) D8). The click is posted verbatim.
- Therefore the geometry MUST supply a client coordinate. The prior value `BOARD_ORIGIN_Y_PX = 200` was the board top in the **screenshot** space; consumed as a client coordinate it drifts down by the non-client chrome height (96 px, operator-measured; ~3 rows). The geometry compensates the chrome **in the agent** (§4) so `center(x,y)` yields a client coordinate the desktop posts verbatim.

**Constraint**: the compensation is applied in the agent (`geometry.ts`) — the operation originator that dispatches the mouse op — so the desktop receives a correct client coordinate and posts it verbatim (D8). Do NOT fix this by layering an opposing offset in the desktop click path (rejected — burdens every mouse consumer with a saolei-specific hack).

### Compensation scope (invariant)

The chrome compensation applies **only to `WINDOW_MESSAGE` operations**. `SIMULATED` operations consume **screenshot-space** coordinates: the desktop's `SIMULATED` branch calls `ScreenshotToScreenCoords(xPx, yPx, windowLeft, windowTop)` (`screenX = screenshotX + windowLeft`; `projects/game/desktop/internal/operation/convert.go`) — it assumes the input is screenshot-relative (chrome-inclusive), so compensating it would shift the click *up* by the chrome height and miss.

This contract is satisfied **structurally**: `center()` is defined in `mcp/saolei/geometry.ts` and called **only** by the three saolei cell tools (`saolei_click` / `saolei_flag` / `saolei_chord_click`), all of which dispatch `method: MOUSE_INPUT_METHOD_WINDOW_MESSAGE`. The generic mouse tools (`projects/game/agent/src/tools/mouse_click/`, `tools/mouse_move/`) do **not** use `center()` — their coordinates come from the model's tool input (screenshot space) and default to `SIMULATED`.

**Regression guards** — do NOT: (a) generalize the compensation into the desktop's shared `runMouseMoveAndClick` (it would break `SIMULATED`); (b) route the generic mouse tools through `center()`; (c) switch the saolei cell tools to `SIMULATED` (client coords are wrong for the `SIMULATED` path). Per [018 research.md D5](../../018-saolei-mcp/research.md) the saolei tools are fixed on `WINDOW_MESSAGE` (a real cursor would visually block cells in the screenshot the model reads).

## 4. Calibration (constant values)

The exact origin values are window-layout-specific (the [018 research.md D6](../../018-saolei-mcp/research.md) posture of hardcoded constants is retained) and are finalized by measurement against the target Minesweeper at implementation time:

- `CHROME_OFFSET_Y_PX = 96` (operator-measured non-client height: title bar + menu bar + borders) ⇒ client-space `BOARD_ORIGIN_Y_PX = BOARD_ORIGIN_Y_PX_SCREENSHOT(200) − 96 = 104`. Worked: `center(4,4) = (168, 248)` = row 4's centre.
- `BOARD_ORIGIN_X_PX = 24` (screenshot-space, retained) — left chrome ≈ border only (~3 px, sub-cell); if a left-border compensation is later wanted, add `CHROME_OFFSET_X_PX` symmetrically. Confirmed at measurement time, not assumed.
- The measurement step MUST confirm the WM client coords match the geometry's pixel space under the operator's actual DPI (fold any DPI factor into the calibrated constant — [research.md](../research.md) D2 note).

The click-landing test ([quickstart.md](../quickstart.md) Scenario 4) is the validation: `saolei_click(x,y)` MUST land on cell `(x,y)`.

## 5. What is NOT changed

- The four saolei tools' contracts (023 US3 / [023 contracts/tool-dispatch-contract.md](../../023-saolei-mcp-refine/contracts/tool-dispatch-contract.md) §6) — `saolei_click`/`saolei_flag`/`saolei_chord_click` still dispatch `MouseMoveAndClickPart{ WINDOW_MESSAGE, … }` via `bridge.dispatch(part, signal)`; `saolei_init` still dispatches `KeyboardPressPart{ F2 }`.
- The desktop-facing `WINDOW_MESSAGE` operation Part and its `lParam` packing — the desktop already posts the exact coordinate it receives.
- The screenshot capture — it remains full-window (the model acts via grid coordinates; pixel-accurate screenshot↔grid mapping is not required to play — [research.md](../research.md) D1).
- The proto — no field, enum, or message changes.

## 6. Test impact

Because the dispatched `MouseMoveAndClickPart` coords for a given grid `(x,y)` change with the chrome compensation, the coordinate assertions in `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts` (unit) and `projects/game/testplan/agent_saolei_test.go` (large, `agent-saolei` suite) MUST be updated to the new client-space values (e.g. `center(4,4) → (168, 248)` with `CHROME_OFFSET_Y_PX = 96`). The large test verifies the **geometry** (dispatched coords); the **click-landing** is a manual Windows gate (Constitution §VI — the agent large test MUST be executed via `guitar run`, all cases pass; the desktop landing is manual).
