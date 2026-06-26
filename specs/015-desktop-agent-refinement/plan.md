# Implementation Plan: Desktop Agent Interaction Refinement

**Branch**: `015-desktop-agent-refinement` | **Date**: 2026-06-26 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/015-desktop-agent-refinement/spec.md`

## Summary

Four refinements to the desktop agent interaction, addressing issues discovered during real use of feature 014: (1) make `SendUserTurn` non-blocking so the conversation dialog updates in real time during continuous agent operations; (2) split the `mouse` tool into `mouse_move` (coordinates only) and `mouse_click` (click-type only, at current cursor position) to reduce coordinate/action confusion; (3) draw the real OS cursor onto screenshots using win32 `GetCursorInfo` + `DrawIconEx`, add a keyboard shortcut for cursor-preserving screenshot capture, and add click-to-zoom for screenshot previews; (4) display collapsed tool-result content in operation bubbles and render history identically to live view.

## Technical Context

**Language/Version**: Go 1.23 (desktop backend), TypeScript 5.x / Svelte 5 (frontend), TypeScript 5.x (agent service)

**Primary Dependencies**: Go stdlib `syscall`/`image`/`image/png` (win32 cursor drawing, no new deps), `github.com/kbinani/screenshot` (existing BitBlt capture), LangChain `langchain@1.5.0` / `@langchain/core@1.2.0` (tool definitions), Wails v2 (desktop framework, IPC + events), Protocol Buffers (proto model)

**Storage**: N/A

**Testing**: `bazel test` (Go unit tests via `go_test`, TypeScript unit tests via vitest, Svelte component tests via vitest + @testing-library/svelte). Large-test testplan at `projects/game/testplan/` covers the agent service end-to-end via the fake-llm harness; US2 tool-name changes (mouse→mouse_move/mouse_click) require syncing `sample_tools.yaml` + `agent_operation_test.go` profiles, and US4 operation-history reconstruction is unit-tested in `handler.test.ts` (fake-llm cannot initiate tool_calls, so operation history is not exercised at the large-test layer — see `projects/game/testplan/README.md` §7).

**Target Platform**: Windows (desktop backend — win32 API), Linux (agent service), cross-platform (frontend dev)

**Project Type**: desktop-app (Wails Go + Svelte) + agent gRPC service

**Performance Goals**: Screenshot capture + cursor overlay + delivery < 500 ms (inherited from 014 SC-004); dialog event rendering < 1 second per event (SC-001)

**Constraints**: Screenshot ≤ 5 MiB (inherited); win32 cursor drawing limited to no cursor shadow, no animated cursor frames (R-001); `mouse_click` has no coordinate parameters (clarification 2026-06-26)

**Scale/Scope**: Single-session, single-window desktop automation; 4 distinct change areas spanning Go, TypeScript agent, and Svelte frontend

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Citation Provenance (§I)**: All external API references (GetCursorInfo, DrawIconEx, GetIconInfo, CURSORINFO, ICONINFO, DeleteObject) carry inline citations in [research.md](research.md) with Microsoft Learn URLs. LangChain tool format inherited from feature 014 with existing citations. ✅
- **Code Style Precedence (§II)**: Implementation tasks MUST read `style/golang.md` (Go), `style/README.md` (TS: Google TypeScript Style), and `style/` Svelte conventions before code changes. ✅ — inherited by tasks.md.
- **External Dependency Research (§III)**: No new external dependencies. Win32 cursor APIs researched against official Microsoft Learn documentation (R-001). `syscall.NewLazyDLL` pattern already used in `execute_windows.go`. LangChain tool format researched in feature 014 (R-001 in 014 research). All documented in [research.md](research.md) with citations. ✅
- **Refactoring-Oriented Changes (§IV)**: Every change classified below as 新增 / 修改 / 删除. 修改 changes implemented as refactors of existing units. Design verdicts recorded for every 修改 and 删除. ✅

*Post-Phase-1 re-check*: All research findings (R-001 through R-005) consolidated. No new dependencies discovered. No gates violated. ✅

## Project Structure

### Documentation (this feature)

```text
specs/015-desktop-agent-refinement/
├── plan.md              # This file
├── research.md          # Phase 0 — R-001 through R-005
├── data-model.md        # Phase 1 — proto + TS schema + frontend types
├── quickstart.md        # Phase 1 — validation scenarios
├── contracts/
│   ├── mouse-tools.md   # Phase 1 — mouse_move + mouse_click tool contracts
│   └── screenshot.md    # Phase 1 — cursor rendering + keyboard shortcut + zoom
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
projects/game/desktop/
├── app.go                              # [修改] SendUserTurn non-blocking + recv goroutine
├── internal/operation/
│   ├── execute_v2.go                   # [修改] ExecuteMouseAction → split into MoveCursor + ExecuteClickAtCurrentPos
│   ├── execute_v2_logic.go             # [修改] validateMouseAction stays; actionEventSequence stays
│   ├── cursor.go                       # [新增] win32 cursor drawing: GetCursorInfo + DrawIconEx
│   └── cursor_test.go                  # [新增] cursor drawing unit tests (build-tagged windows)
├── internal/capture/
│   └── capture.go                      # [修改] CaptureWindow → optional cursor overlay step
└── frontend/src/
    ├── App.svelte                      # [修改] keyboard shortcut, history mapping, pendingScreenshot clear, zoom modal
    ├── api.ts                          # [修改] MessageEntry type extension for operations
    └── components/
        ├── ChatView.svelte             # [修改] tool-result bubble collapsed content, screenshot zoom, operation result display
        └── ScreenshotModal.svelte      # [新增] click-to-zoom modal/lightbox component

projects/game/agent/src/
├── mouse-tool.ts                       # [修改] split into createMouseMoveTool + createMouseClickTool
├── mouse-tool.test.ts                  # [修改] split tests for two tools
├── session-agent.ts                    # [修改] register two tools instead of one
```

**Structure Decision**: Existing monorepo layout — no new directories or projects. New files: `cursor.go` (+ test) in the already-existing `internal/operation/` package, `ScreenshotModal.svelte` in the already-existing `components/` directory.

## Change Classification (§IV)

### Proto Layer — Message content oneof (US4)

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 0 | `projects/game/game.proto` — `Message` | 修改 | Move `operation` (8) and `operation_result` (9) into the existing `content` oneof (alongside `text` and `image_data`); update the `type` field comment to list all six values. Field numbers unchanged → wire-compatible. | **Refactor**: previously `operation` and `operation_result` were independent optionals, but a single history Message is semantically either a tool call OR a tool result (and exclusive with text/image) — the `type` field already encoded this. Separate optionals permitted the invalid state of multiple bodies set at once. Promoting them into `content` makes the mutual-exclusivity invariant structural. Getters stay valid, so Go read sites (`view_model.go`) are unaffected; only the TS agent `handler.ts` construction sites must set the proto-loader oneof discriminator. |

### Desktop Go Layer — Real-Time Dialog Update (US1)

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 1 | `app.go` — `SendUserTurn` | 修改 | Refactor: send user-turn frame and return immediately; move the recv loop into a new `recvLoop` goroutine that emits frames via `runtime.EventsEmit` and auto-executes inbound operations via `handleInboundOperation` | **Refactor**: the current function conflates "send" and "receive-entire-response". The recv loop is extracted into a goroutine started after the send. `SendUserTurn` returns `nil` immediately after `SendFrame` succeeds. The goroutine handles all subsequent frames until `wait`. The existing `handleInboundOperation` flow is preserved but called from the goroutine. |
| 2 | `app.go` — recv goroutine lifecycle | 新增 | New field `recvDone chan struct{}` on `App` to signal goroutine completion; goroutine started in `SendUserTurn`, stopped on `wait` frame or `RecvFrame` error; `CloseAgent` waits for goroutine to exit | New lifecycle management for the background receiver. The goroutine is scoped per-turn: one goroutine per `SendUserTurn` call, exits on `wait`/error. |

### Desktop Go Layer — Mouse Tool Split (US2)

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 3 | `execute_v2.go` — `ExecuteMouseAction` | 修改 | Refactor into two functions: `MoveCursor(screenX, screenY int32) error` (validate bounds + SetCursorPos) and `ExecuteClickAtCurrentPos(action AgentMouseAction) error` (validate action is a click type + dispatch events without SetCursorPos) | **Refactor**: the existing function's two-phase design (SetCursorPos → events) is split along its natural seam. `MoveCursor` is Phase 1 alone; `ExecuteClickAtCurrentPos` is Phase 2 alone. The original combined function is removed. Both new functions share the existing `validateScreenCoords` / `actionEventSequence` helpers. |
| 4 | `app.go` — `executeAgentOperation` | 修改 | Route by action type: MOVE → `ScreenshotToScreenCoords` + `MoveCursor`; clicks → `ExecuteClickAtCurrentPos(action)` (no coordinate conversion, no SetCursorPos) | **Refactor**: the existing function always converts coordinates and calls the combined executor. The new version branches: MOVE uses coordinates (screenshot-relative → screen-absolute); clicks skip coordinates and fire at the current cursor position set by the preceding mouse_move. The screenshot capture + delivery flow is unchanged. |
| 5 | `execute_v2_logic.go` — `validateMouseAction` | 修改 (minor) | Add `validateClickAction` helper that rejects MOVE/UNSPECIFIED for the click-only path. Existing `validateMouseAction` stays for MOVE. | Existing validation design still serves. A click-specific validator is a thin wrapper. |

### Desktop Go Layer — Screenshot Cursor (US3)

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 6 | `cursor.go` | 新增 | New file: `DrawCursor(img *image.RGBA, windowBounds capture.WindowBounds) error` — get cursor info via GetCursorInfo, get hotspot + size via GetIconInfo + GetObject, draw onto a GDI memory DC created from the image, read back. Skip silently if cursor is hidden/suppressed. | New module in existing package. Pure win32 syscall, no external deps. Build-tagged `//go:build windows`. |
| 7 | `capture.go` — `CaptureWindow` | 修改 | After `screenshot.CaptureRect` produces the `*image.RGBA` and before `EncodePNG`, call `DrawCursor` to overlay the real OS cursor at its current position relative to the captured window bounds | **Refactor**: the capture pipeline gains one step. The function currently validates window → bounds → capture → encode. The cursor overlay is inserted between capture and encode. |
| 8 | `marker.go` — `ApplyMarker` | 删除 | Remove the self-drawn red-ring marker overlay, since screenshots now include the real cursor. The marker code (`drawRing`, `setPixel`) is removed. | **Design verdict**: the marker was a workaround for the missing cursor (feature 014). With real cursor rendering (US3), the workaround is obsolete. Removing it eliminates a self-drawn approximation that the spec calls out as wrong. |
| 9 | `app.go` — `executeAgentOperation` | 修改 (minor) | Remove `ApplyMarker` call; the screenshot already includes the cursor from step 7 | Trivial: delete the marker application lines, pass raw screenshot bytes to the result frame. |

### Desktop Go Layer — Non-Windows Stubs

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 10 | `internal/operation/execute_v2_other.go` | 修改 | Update non-Windows stub to expose `MoveCursor` + `ExecuteClickAtCurrentPos` (both return "not supported"), replacing the old combined `ExecuteMouseAction` stub | Mirror the Windows-side split. Non-Windows is build-only; no behavioral change. |

### Agent TypeScript Layer — Mouse Tool Split (US2)

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 11 | `mouse-tool.ts` | 修改 | Replace `createMouseTool(bridge)` with `createMouseMoveTool(bridge)` (schema: `{ x_px, y_px }`, dispatches MOVE) and `createMouseClickTool(bridge)` (schema: `{ click_type }`, dispatches click action at current position). Both return content-block arrays inheriting 014's format. | **Refactor**: the single tool with combined schema is split along the action/coordinate boundary. `mouse_move` owns coordinates; `mouse_click` owns click types. The dispatch path through `OperationBridge` is shared. |
| 12 | `session-agent.ts` | 修改 | Register `mouse_move` and `mouse_click` tools instead of the single `mouse` tool | Direct consequence of the tool split. The tool registration array gains one entry and the old entry is replaced. |

### Frontend Svelte Layer — Real-Time Update + Tool Bubbles + History + Zoom (US1, US3, US4)

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 13 | `App.svelte` — `handleSendChatText` | 修改 | Move `pendingScreenshot = null` before the `await sendUserTurn(...)` call so the preview clears immediately on send | **Refactor**: the await was blocking the screenshot clear. Moving the clear before the await is the minimal correct fix. |
| 14 | `App.svelte` — `handleLoadMessages` | 修改 | Extend `typeFromString` and the message mapping to handle `'operation'` and `'operation_result'` types; include `operation` and `operationResult` data in mapped chat entries | **Refactor**: the history loader currently drops operations. Extending it to map all live-view message types makes history rendering identical to live view (FR-014). |
| 15 | `App.svelte` — keyboard shortcut | 新增 | `window.addEventListener('keydown', ...)` handler for `Ctrl+Shift+S` that calls `handleCaptureScreenshot` | New event listener for cursor-preserving screenshot capture. |
| 16 | `App.svelte` — screenshot zoom modal | 新增 | State `zoomedImageUrl` + render `<ScreenshotModal>` overlay when any screenshot image is clicked | New modal state and component wiring for click-to-zoom. |
| 17 | `ChatView.svelte` — operation-result bubble | 修改 | Add `<details>` collapsed section to `operation_result` entries showing the result message and screenshot (when present in `operationResult.screenshot`). Add click-to-zoom on screenshots. | **Refactor**: the current bubble shows only status + message text. The `<details>` pattern is already used for image entries — reused for consistency. |
| 18 | `ChatView.svelte` — pending screenshot zoom | 修改 | Add click handler on `attachment-thumb` to open the zoom modal | Minor: wire the existing thumbnail to the new modal. |
| 19 | `ScreenshotModal.svelte` | 新增 | New component: full-screen overlay showing the clicked screenshot at maximum fit size, dismissible by click/Escape | New component for click-to-zoom. |

### Agent Service — History Persistence (US4, conditional)

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 20 | `handler.ts` / agent message storage | 修改 (conditional) | If `ListMessages` does not currently return operations and operation results, extend the agent service to persist them so history is complete. Details depend on server-side investigation during implementation. | **Conditional**: if operations are already persisted as part of the frame stream, only the frontend mapping (item 14) is needed. If not, the agent service must store operation/operation_result frames as retrievable messages. |

## Complexity Tracking

No Constitution Check violations. No complexity justifications needed.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [GetCursorInfo — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getcursorinfo) — retrieves cursor handle, position, visibility flags
- [CURSORINFO structure — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-cursorinfo) — CURSOR_SHOWING / CURSOR_SUPPRESSED flag semantics
- [GetIconInfo — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-geticoninfo) — retrieves hotspot + bitmap handles; DPI virtualization note
- [DrawIconEx — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-drawiconex) — draws cursor onto HDC with explicit size; DI_NORMAL = 0x0003
- [DeleteObject — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/wingdi/nf-wingdi-deleteobject) — free GDI bitmap handles

### Repositories

- [ghp3000/screenshot — GitHub](https://github.com/ghp3000/screenshot) — Go screenshot library with BitBlt + DrawIconEx cursor support (`DrawCursor()`)
- [langchain-ai/langchainjs — _formatToolOutput](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/langchain-core/src/tools/index.ts#L785-L811) — tool content-block array passthrough (inherited from feature 014)

### Articles & RFCs

- [Capture screen shot with mouse cursor — Stack Overflow](https://stackoverflow.com/questions/1628919/capture-screen-shot-with-mouse-cursor) — canonical GetCursorInfo + DrawIconEx reference
