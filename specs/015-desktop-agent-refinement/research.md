# Research: Desktop Agent Interaction Refinement

**Feature**: 015-desktop-agent-refinement
**Date**: 2026-06-26

## R-001: Win32 API Cursor Drawing on Screenshots (FEASIBLE)

**Decision**: Draw the real OS cursor onto captured screenshots using the win32 API sequence `GetCursorInfo → GetIconInfo → DrawIconEx`, called from Go via `syscall.NewLazyDLL("user32.dll")` (no cgo).

**Rationale**: The current screenshot pipeline uses `github.com/kbinani/screenshot` which captures via GDI BitBlt — BitBlt does NOT include the cursor. The standard, production-proven method to add the cursor is the 4-call GDI sequence documented on Microsoft Learn and used by multiple Go screenshot libraries (`ghp3000/screenshot`, `getlantern/systray`).

**API Sequence**:

1. `GetCursorInfo(&CURSORINFO)` — retrieve cursor handle, screen position, visibility flags. ([GetCursorInfo — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getcursorinfo); [CURSORINFO struct](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-cursorinfo))
2. Skip if `flags != CURSOR_SHOWING` (0x01) or `flags & CURSOR_SUPPRESSED` (0x02) — cursor is hidden or suppressed by touch/pen.
3. `GetIconInfo(hCursor, &ICONINFO)` — get hotspot (`xHotspot`, `yHotspot`) and bitmap handles (`hbmMask`, `hbmColor`). ([GetIconInfo — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-geticoninfo))
4. `GetObject(hbmColor, &BITMAP)` — get actual pixel dimensions (`bmWidth`, `bmHeight`). Do NOT use `DI_DEFAULTSIZE` — it defaults to 32×32 system icon size.
5. Compute hotspot-adjusted position: `drawX = ptScreenPos.x - xHotspot`, `drawY = ptScreenPos.y - yHotspot`.
6. `DrawIconEx(hdc, drawX, drawY, hCursor, bmWidth, bmHeight, 0, NULL, DI_NORMAL)` — render cursor onto the target device context. ([DrawIconEx — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-drawiconex))
7. `DeleteObject(hbmMask)`, `DeleteObject(hbmColor)` — free bitmaps created by GetIconInfo. ([DeleteObject — Microsoft Learn](https://learn.microsoft.com/en-us/wingdi/winringdi/nf-winringdi-deleteobject))

**Go binding**: `golang.org/x/sys/windows` does NOT wrap these functions. Use direct `syscall.NewLazyDLL("user32.dll")` + `NewProc`. This is the same pattern already used in `projects/game/desktop/internal/operation/execute_windows.go` (`user32DLL`). No new external dependency.

**Integration approach**: The current `CaptureWindow` flow uses `kbinani/screenshot.CaptureRect` which returns an `*image.RGBA`. DrawIconEx needs an HDC. The cleanest integration is to perform the cursor drawing within a GDI memory-DC round-trip: create a compatible DC + DIB section from the captured image, draw the cursor via DrawIconEx, read back via GetDIBits. This keeps `kbinani` for the BitBlt capture and adds cursor as a post-processing step.

**Known limitations** (acceptable for this feature):
- **Cursor shadows are NOT captured** — DWM renders shadows separately from the cursor bitmap. The cursor appears without its drop shadow. Acceptable: the purpose is showing cursor position, not pixel-perfect aesthetics.
- **Animated cursors** — DrawIconEx draws one frame (`istepIfAniCur = 0`). Acceptable: the agent needs to see position, not animation.
- **DPI scaling** — DrawIconEx does NOT participate in DPI virtualization ([GetIconInfo DPI note](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-geticoninfo)). Must use explicit `bmWidth`/`bmHeight` from `GetObject(hbmColor)`, NOT `DI_DEFAULTSIZE`. The app should already be per-monitor DPI-aware (Wails v2 sets this).

**Alternative considered**: DXGI Desktop Duplication API ([Desktop Duplication API — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/direct3ddxgi/desktop-dup-api)) natively includes the cursor via `GetFramePointerShape` and handles shadows. Go library `shinkar94/godesktopdup` wraps this. Rejected because: (1) requires DirectX 11.1+ and Windows 8+, (2) significantly higher complexity (COM, device management), (3) the GDI approach is sufficient for the stated goal.

**Evidence**:
- [Capture screen shot with mouse cursor (Stack Overflow)](https://stackoverflow.com/questions/1628919/capture-screen-shot-with-mouse-cursor) — canonical C++ reference for the exact API sequence
- [ghp3000/screenshot — GitHub](https://github.com/ghp3000/screenshot) — Go library implementing BitBlt + DrawIconEx with `DrawCursor()` option
- [DrawIconEx — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-drawiconex) — `DI_NORMAL = 0x0003`, explicit size params

---

## R-002: Blocking SendUserTurn — Root Cause of Real-Time Dialog Freeze

**Decision**: Refactor `SendUserTurn` in `app.go` to be non-blocking: send the user-turn frame and return immediately, then run the WebSocket recv loop in a separate goroutine that emits frames via `runtime.EventsEmit` as they arrive.

**Rationale**: The current `SendUserTurn` (app.go:441–536) is a Wails bound method that enters a synchronous `for { resp := a.ws.RecvFrame(a.ctx) }` loop. This loop blocks until the agent sends a `wait` frame (signaling the turn is complete). While blocked:

1. The Wails IPC call for `SendUserTurn` stays pending — the frontend's `await sendUserTurn(...)` does not resolve.
2. The frontend's `pendingScreenshot = null` (App.svelte:433) cannot execute until the IPC resolves, so the screenshot preview stays in the input area for the entire agent run.
3. The `processing = false` and `playState` transitions (App.svelte:434, 376) are deferred until the `wait` frame.

Although `runtime.EventsEmit` IS called inside the loop (app.go:517) and events DO reach the frontend via the Wails event channel, the practical effect observed by the user is that the dialog appears frozen because the Wails IPC layer serializes event delivery with the pending bound-method response — events emitted during a pending IPC call may be buffered until the call completes, depending on the Wails WebView2 IPC implementation.

**Design verdict (§IV)**: The current `SendUserTurn` design conflates two responsibilities: (1) sending the user turn and (2) receiving the entire agent response. This was acceptable when operations were simple and fast, but with continuous multi-operation agent runs, the blocking behavior blocks the UI. The refactor separates these concerns: `SendUserTurn` sends and returns; a background goroutine handles reception and event emission. The existing `handleInboundOperation` flow (auto-executing mouse operations and sending results back) moves into the goroutine.

**Concurrency safety**: The recv goroutine shares `a.ws` (WebSocket client) and `a.boundWin` (bound window) with the main goroutine. After the refactor, `SendUserTurn` no longer touches `a.ws` after the initial `SendFrame` — the goroutine owns the recv side. `handleInboundOperation` and `executeAgentOperation` already operate on `a.boundWin` which is only set by `BindWindow` (called from the frontend, serialized by Wails). No new mutex is needed for the initial implementation, but a `sync.Mutex` on the WebSocket send path should be considered if concurrent sends become possible.

**Alternatives considered**:
- Moving only `pendingScreenshot = null` before the `await` on the frontend: fixes the screenshot lingering but NOT the broader dialog freeze (events still buffered behind pending IPC).
- Adding a Wails frontend event for "screenshot consumed": adds complexity without fixing the root cause.
- Replacing Wails IPC with a direct WebSocket from the frontend: rejected — massive architectural change, Wails bound methods are the standard pattern.

---

## R-003: Mouse Tool Split — Agent and Desktop Impact

**Decision**: Split the single `mouse` LangChain tool into `mouse_move` and `mouse_click`. Per the clarification (2026-06-26), `mouse_click` clicks at the current cursor position and does NOT accept coordinates; `mouse_move` accepts coordinates and repositions the cursor.

**Agent-side impact** (`mouse-tool.ts`):
- Replace `createMouseTool(bridge)` with `createMouseMoveTool(bridge)` and `createMouseClickTool(bridge)`.
- `mouse_move` schema: `{ x_px, y_px }` — dispatches `AgentMouseOperation { action: MOVE, xPx, yPx }`.
- `mouse_click` schema: `{ click_type: LEFT_CLICK | LEFT_DOUBLE_CLICK | RIGHT_CLICK | RIGHT_DOUBLE_CLICK | LEFT_RIGHT_PRESS }` — dispatches `AgentMouseOperation { action: <click_type>, xPx: 0, yPx: 0 }`. Coordinates are not sent; the desktop ignores them for click actions.
- Both return the content-block array (text + optional screenshot image + size annotation), inheriting feature 014's return format.

**Desktop-side impact** (`execute_v2_logic.go`, `app.go`):
- `executeAgentOperation` currently always converts screenshot coordinates to screen coordinates and calls `ExecuteMouseAction(screenX, screenY, action)`. For click actions from `mouse_click`, the desktop should NOT call `SetCursorPos` — it should dispatch button events at the current cursor position.
- Refactor `ExecuteMouseAction` into two functions:
  - `MoveCursor(screenX, screenY int32) error` — validates bounds, calls `SetCursorPos`.
  - `ExecuteClickAtCurrentPos(action AgentMouseAction) error` — validates action is a click type, dispatches `actionEventSequence` events (no SetCursorPos).
- `executeAgentOperation` routes: MOVE → `ScreenshotToScreenCoords` + `MoveCursor`; clicks → `ExecuteClickAtCurrentPos(action)`.

**Proto impact**: No change. `AgentMouseOperation` retains `action`, `x_px`, `y_px`. For click operations, `x_px`/`y_px` are zero (sent as 0 by the agent, ignored by the desktop). The proto enum `AgentMouseAction` is unchanged — MOVE remains value 6.

**Historical compatibility**: Conversations that recorded old-format `mouse` tool calls (with `action + coordinates`) must still render in history. The old `operation` frame format is unchanged — the proto is the same, only the agent-side tool boundary is restructured.

---

## R-004: Tool Result Bubble Display and History Consistency

**Decision**: Extend `ChatView.svelte` to display collapsed result content (including screenshots) in operation-result bubbles, and extend `handleLoadMessages` in `App.svelte` to map `operation` and `operation_result` message types from history.

**Current state**:
- `ChatView.svelte` renders `operation_result` entries with only status icon + message text (line 172–183). The `operationResult` object includes a `screenshot` field (base64 PNG) but it is not displayed.
- `handleLoadMessages` (App.svelte:261–295) maps only `text`, `image`, `thinking`, `warn` types via `typeFromString`. Operations and results are not mapped from history.
- The `MessageEntry` interface (api.ts:35) has a `type` field but the server's stored message types need to be checked to confirm operations are persisted.

**Frontend changes**:
- Operation-result bubble: add a `<details>` collapsed section showing the result message and screenshot (when present), mirroring the existing `image-details` pattern used for image entries.
- History mapping: extend `typeFromString` to handle `'operation'` and `'operation_result'`; include `operation` and `operationResult` data in the mapped chat entries.
- Click-to-zoom for screenshots: add a modal/lightbox overlay triggered by clicking any screenshot image (pending attachments, operation-result screenshots, and image entries).

**Server-side dependency**: The `ListMessages` API must return operations and operation results if they are to appear in history. If the agent service does not persist these, a server-side change is needed. This will be confirmed during implementation.

---

## R-005: Keyboard Shortcut for Screenshot Capture

**Decision**: Add a keyboard shortcut in the Wails frontend (Svelte) to trigger `handleCaptureScreenshot` without a mouse click.

**Rationale**: The current screenshot capture is triggered by clicking the "Capture Screenshot" button (App.svelte:669). Clicking the button moves the cursor to the button, displacing it from the position the user wants to capture. A keyboard shortcut lets the user position the cursor at a target and capture without displacement.

**Implementation**: Wails v2 supports global/window keyboard shortcuts via the `runtime.Environment` or frontend keydown listeners. Since the shortcut only needs to work while the desktop app is focused, a Svelte `window.addEventListener('keydown', ...)` handler is sufficient. The shortcut should be a non-conflicting combination (e.g., `Ctrl+Shift+S`).

---

## References

### Official Documentation

- [GetCursorInfo — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getcursorinfo) — retrieves cursor handle, position, visibility flags
- [CURSORINFO structure — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-cursorinfo) — flags: CURSOR_SHOWING (0x01), CURSOR_SUPPRESSED (0x02)
- [GetIconInfo — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-geticoninfo) — retrieves hotspot + bitmap handles; note DPI virtualization exclusion
- [ICONINFO structure — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-iconinfo) — fIcon, xHotspot, yHotspot, hbmMask, hbmColor
- [DrawIconEx — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-drawiconex) — draws cursor onto HDC; DI_NORMAL = 0x0003; explicit cxWidth/cyWidth required
- [DeleteObject — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/wingdi/nf-wingdi-deleteobject) — free GDI bitmap handles from GetIconInfo

### Repositories

- [ghp3000/screenshot — GitHub](https://github.com/ghp3000/screenshot) — Go screenshot library with BitBlt + DrawIconEx cursor support (`DrawCursor()` method)
- [langchain-ai/langchainjs — _formatToolOutput](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/langchain-core/src/tools/index.ts#L785-L811) — tool content-block array passthrough (inherited from feature 014, v1.5.0)

### Articles & RFCs

- [Capture screen shot with mouse cursor — Stack Overflow](https://stackoverflow.com/questions/1628919/capture-screen-shot-with-mouse-cursor) — canonical C++ reference for GetCursorInfo + DrawIconEx cursor overlay
