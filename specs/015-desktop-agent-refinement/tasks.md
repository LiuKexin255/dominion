# Tasks: Desktop Agent Interaction Refinement

**Input**: Design documents from `/specs/015-desktop-agent-refinement/`

**Prerequisites**: [plan.md](plan.md) (required), [spec.md](spec.md) (required), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Existing test files (mouse-tool.test.ts, execute_v2_test.go, marker_test.go) MUST be updated when their corresponding source files change. No new test suites are created unless the implementation introduces a new testable unit.

**Organization**: Tasks grouped by user story (US1 P1 → US4 P4) to enable independent implementation and testing.

## Format: `[ID] [P?] [Story] [新增|修改|删除] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story (US1–US4)
- **[新增|修改|删除]**: Change classification per Constitution §IV
- File paths are project-relative under repository root

## Constitution Check

*GATE: Must pass before implementation begins.*

- **Citation Provenance (§I)**: every task referencing an external library, API, or inherited design decision MUST include an inline citation or explicitly reference the parent `plan.md`/`research.md` source. Win32 API references inherit citations from [research.md](research.md) R-001. LangChain tool format inherits from feature 014 research. ✅
- **Code Style Precedence (§II)**: T001 reads `style/golang.md` and `style/README.md` before any code change. Every subsequent code task inherits this prerequisite. ✅
- **External Dependency Research (§III)**: No new dependencies. Win32 cursor APIs researched in [research.md](research.md) R-001 with Microsoft Learn citations. `syscall.NewLazyDLL` pattern inherited from existing `execute_windows.go`. ✅
- **Refactoring-Oriented Changes (§IV)**: every task carries 新增 / 修改 / 删除 classification from [plan.md](plan.md). 修改 and 删除 tasks include design-applicability review notes. ✅

---

## Phase 1: Setup

**Purpose**: Constitutional prerequisite — read style guidelines before any code change.

- [ ] T001 Read `style/golang.md` (Go style) and `style/README.md` (TypeScript/Svelte style) and confirm understanding of formatting, naming, and comment conventions before any implementation task begins

---

## Phase 2: User Story 1 — Real-Time Desktop Dialog Update (Priority: P1) 🎯 MVP

**Goal**: The desktop conversation dialog renders streaming text, tool results, and screenshot consumption incrementally during continuous agent operations — not deferred until the agent run completes.

**Independent Test**: Start a continuous agent operation with multiple tool calls. Observe the dialog: text, tool results, and screenshot removal should all appear in real time, not in a batch after the run.

**Root cause** (from [research.md](research.md) R-002): `SendUserTurn` in `app.go` is a blocking Wails bound method — its synchronous recv loop holds the IPC call open until the agent sends a `wait` frame, preventing the frontend from clearing state.

- [ ] T002 [US1] [修改] Refactor `SendUserTurn` to non-blocking in `projects/game/desktop/app.go` — send the user-turn frame and return immediately; extract the `for { RecvFrame }` recv loop into a new `recvLoop` goroutine that emits frames via `runtime.EventsEmit` and auto-executes inbound operations via `handleInboundOperation`; add `recvDone chan struct{}` field to `App` struct for goroutine lifecycle; `CloseAgent` waits for goroutine exit. **Design review**: the existing function conflates "send" and "receive-entire-response"; separating them is the intended refactor. `handleInboundOperation` flow is preserved, called from the goroutine.

- [ ] T003 [US1] [修改] Move `pendingScreenshot = null` before the `await sendUserTurn(...)` call in `projects/game/desktop/frontend/src/App.svelte` `handleSendChatText` so the screenshot preview clears immediately on send. Also move `queueCount` decrement before the await. **Design review**: the await was blocking the state clear; with the non-blocking SendUserTurn (T002) the await resolves immediately, but the early clear is still correct.

**Checkpoint**: Dialog updates in real time during continuous agent operations. Pending screenshot clears on send.

---

## Phase 3: User Story 2 — Mouse Tool Split (Priority: P2)

**Goal**: The single `mouse` tool is replaced by `mouse_move` (coordinates only, MOVE action) and `mouse_click` (click-type only, at current cursor position, no coordinates). Each tool has a narrower parameter space to reduce coordinate/action errors.

**Independent Test**: Instruct the agent to move the cursor, then click. The agent invokes `mouse_move` then `mouse_click` as separate tools. The click fires at the position set by the move.

**Clarification** (2026-06-26): `mouse_click` clicks at the current cursor position; coordinates belong only to `mouse_move`.

### Desktop Go Layer

- [ ] T004 [P] [US2] [修改] Refactor `ExecuteMouseAction` into `MoveCursor(screenX, screenY int32) error` (validate bounds + `SetCursorPos`) and `ExecuteClickAtCurrentPos(action AgentMouseAction) error` (validate click type + dispatch `actionEventSequence` events without `SetCursorPos`) in `projects/game/desktop/internal/operation/execute_v2.go`. Remove the original combined function. **Design review**: the existing two-phase design (SetCursorPos → events) splits along its natural seam. Both new functions share existing `validateScreenCoords` / `actionEventSequence` helpers.

- [ ] T005 [P] [US2] [修改] Add `validateClickAction(action AgentMouseAction) error` helper that rejects MOVE/UNSPECIFIED in `projects/game/desktop/internal/operation/execute_v2_logic.go`. Existing `validateMouseAction` stays unchanged for MOVE validation.

- [ ] T006 [P] [US2] [修改] Update non-Windows stub in `projects/game/desktop/internal/operation/execute_v2_other.go` — replace `ExecuteMouseAction` with `MoveCursor` + `ExecuteClickAtCurrentPos` stubs returning "not supported".

- [ ] T007 [US2] [修改] Update `executeAgentOperation` routing in `projects/game/desktop/app.go` — for MOVE: `ScreenshotToScreenCoords` + `MoveCursor`; for clicks: `ExecuteClickAtCurrentPos(action)` (no coordinate conversion). Depends on T004. **Design review**: the function currently always converts coordinates and calls the combined executor; the new version branches by action type. Screenshot capture flow unchanged.

### Agent TypeScript Layer

- [ ] T008 [P] [US2] [修改] Split `createMouseTool(bridge)` into `createMouseMoveTool(bridge)` (schema `{x_px, y_px}`, dispatches `AGENT_MOUSE_ACTION_MOVE`) and `createMouseClickTool(bridge)` (schema `{click_type}`, dispatches click action with `xPx:0, yPx:0`) in `projects/game/agent/src/mouse-tool.ts`. Extract shared `buildResultBlocks(result)` helper for the content-block return format. See [data-model.md](data-model.md) for full code. **Design review**: the single tool's combined schema is split along the action/coordinate boundary. Dispatch through `OperationBridge` is shared.

- [ ] T009 [US2] [修改] Register `mouse_move` and `mouse_click` tools instead of single `mouse` tool in `projects/game/agent/src/session-agent.ts`. Depends on T008.

- [ ] T010 [US2] [修改] Update tests in `projects/game/agent/src/mouse-tool.test.ts` — split into `createMouseMoveTool` and `createMouseClickTool` test cases; verify MOVE dispatches coordinates, click dispatches at position 0,0.

**Checkpoint**: Agent uses two separate tools. `mouse_move` positions cursor; `mouse_click` fires at current position.

---

## Phase 4: User Story 3 — Screenshot Cursor, Keyboard Shortcut, Click-to-Zoom (Priority: P3)

**Goal**: Screenshots include the real OS cursor (via win32 API). A keyboard shortcut enables cursor-preserving capture. User-attached screenshots support click-to-zoom.

**Independent Test**: Position cursor at a known location, press `Ctrl+Shift+S` to capture, verify cursor appears in screenshot at correct position. Click screenshot thumbnail to open zoom modal.

### Desktop Go Layer — Cursor Rendering

- [ ] T011 [P] [US3] [新增] Create `projects/game/desktop/internal/operation/cursor.go` (build-tagged `//go:build windows`) with `DrawCursor(img *image.RGBA, winLeft, winTop int32) error` — calls `GetCursorInfo` → `GetIconInfo` → `GetObject` → `DrawIconEx` via `syscall.NewLazyDLL("user32.dll")` per [research.md](research.md) R-001. Skips silently when cursor hidden/suppressed. Uses explicit pixel dimensions (not `DI_DEFAULTSIZE`). Cleans up `hbmMask`/`hbmColor` via `DeleteObject`.

- [ ] T012 [P] [US3] [新增] Create `projects/game/desktop/internal/operation/cursor_test.go` (build-tagged `//go:build windows`) with unit coverage for `DrawCursor` hidden/suppressed cursor handling and GDI resource cleanup paths where practical. Depends on T011.

- [ ] T013 [US3] [修改] Integrate `DrawCursor` call into `CaptureWindow` in `projects/game/desktop/internal/capture/capture.go` — after `screenshot.CaptureRect` and before `EncodePNG`, call `DrawCursor(img, bounds.Left, bounds.Top)`. Depends on T011. **Design review**: the capture pipeline gains one step (cursor overlay) between capture and encode, using bounds already computed.

- [ ] T014 [US3] [删除] Delete `projects/game/desktop/internal/operation/marker.go` and `projects/game/desktop/internal/operation/marker_test.go` if present, and remove the `ApplyMarker` call in `projects/game/desktop/app.go` `executeAgentOperation` — pass raw screenshot bytes (with cursor from T013) directly to the result frame. **Design review**: the marker was a workaround for the missing cursor (feature 014); with real cursor rendering it is obsolete. Removing it eliminates the self-drawn approximation.

### Frontend Svelte Layer — Shortcut + Zoom

- [ ] T015 [P] [US3] [新增] Create `projects/game/desktop/frontend/src/components/ScreenshotModal.svelte` — full-screen dark overlay showing clicked screenshot at maximum fit size, dismissible by click or Escape. Props: `imageUrl`, `onClose`.

- [ ] T016 [US3] [修改] Add keyboard shortcut (`Ctrl+Shift+S` → `handleCaptureScreenshot`) via `window.addEventListener('keydown', ...)` in `projects/game/desktop/frontend/src/App.svelte` `onMount`. Add `zoomedImageUrl` state and render `<ScreenshotModal>` when set. Depends on T015.

- [ ] T017 [US3] [修改] Add click-to-zoom handler on screenshot images (pending attachment thumbnail, image entries, operation-result screenshots) in `projects/game/desktop/frontend/src/components/ChatView.svelte` — accept `onZoom` callback prop, wire image click handlers. Depends on T015.

**Checkpoint**: Screenshots show real cursor. `Ctrl+Shift+S` captures without cursor displacement. Click any screenshot to zoom.

---

## Phase 5: User Story 4 — Tool Result Bubble + History Consistency (Priority: P4)

**Goal**: Tool operation bubbles display collapsed result content. Historical conversations include tool operations and results, rendered identically to live view.

**Independent Test**: Run a conversation with tool operations. Verify collapsed results expand on click. Reload from history — operations, results, and layout match live view.

- [ ] T018 [P] [US4] [修改] Add `<details>` collapsed section to `operation_result` bubble in `projects/game/desktop/frontend/src/components/ChatView.svelte` — show result message and screenshot (when present in `operationResult.screenshot`) collapsed by default with expand affordance. See [data-model.md](data-model.md) for markup. **Design review**: the existing `<details>` pattern for image entries is reused for consistency.

- [ ] T019 [US4] [修改] Extend `MessageEntry` in `projects/game/desktop/frontend/src/api.ts` to include optional `operation?: AgentOperationFrame` and `operationResult?: AgentOperationResultFrame` fields returned by history. **Design review**: frontend API typing must match the live `ChatEntry` operation/result data shape so history can render identically to live view.

- [ ] T020 [US4] [修改] Extend `handleLoadMessages` and `typeFromString` in `projects/game/desktop/frontend/src/App.svelte` to map `'operation'` and `'operation_result'` message types from history — include `operation` and `operationResult` data in mapped `ChatEntry` objects. Depends on T019. **Design review**: the history loader currently drops operations; extending it makes history rendering identical to live view (FR-013/FR-014).

- [ ] T021 [US4] [修改] Inspect `ListMessages` behavior in `projects/game/agent/src/handler.ts` and document whether operation and operation_result frames are already persisted in the session message store. If they are already persisted, mark this task complete with the evidence in implementation notes.

- [ ] T022 [US4] [修改] If T021 finds operation frames are not persisted, extend `projects/game/agent/src/handler.ts` to persist operation and operation_result entries with the same fields needed by `MessageEntry.operation` and `MessageEntry.operationResult`; if T021 confirms they are persisted, leave this task as no-op with evidence. **Design review**: history completeness is required by FR-013/FR-014; this task converts the prior conditional investigation into a concrete implementation gate.

**Checkpoint**: Tool results visible collapsed in bubbles. History matches live view.

---

## Phase 6: Polish & Cross-Cutting

- [ ] T023 Run `bazel run //:gazelle projects/game/desktop` to update BUILD.bazel for new Go file `cursor.go`, new `cursor_test.go`, and deletion of `marker.go`

- [ ] T024 Run full build and test verification: `bazel build //projects/game/desktop && bazel build //projects/game/agent && bazel test //projects/game/desktop/internal/operation:all && bazel test //projects/game/agent/src:all`

- [ ] T025 Run [quickstart.md](quickstart.md) validation scenarios 1–6 on a Windows machine to verify all four user stories end-to-end

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **US1 (Phase 2)**: Depends on T001 (style reading)
- **US2 (Phase 3)**: Depends on T001. T007 touches `app.go` after T002 (US1) — sequential
- **US3 (Phase 4)**: Depends on T001. T014 touches `app.go` after T007 (US2) — sequential
- **US4 (Phase 5)**: Depends on T001. T018 touches `ChatView.svelte` after T017 (US3) — sequential
- **Polish (Phase 6)**: Depends on all user stories complete

### `app.go` Serialization Constraint

Three user stories modify `projects/game/desktop/app.go`:
1. T002 (US1): `SendUserTurn` refactor
2. T007 (US2): `executeAgentOperation` routing
3. T014 (US3): remove `ApplyMarker` call in `executeAgentOperation`

These MUST be done sequentially in the order T002 → T007 → T014 (matching the US1 → US2 → US3 priority order).

### Within Each User Story

- Desktop Go changes before frontend changes (frontend depends on backend behavior)
- New files (`cursor.go`, `cursor_test.go`, `ScreenshotModal.svelte`) before files that depend on them
- Tool creation (`mouse-tool.ts`) before tool registration (`session-agent.ts`)

### Parallel Opportunities

- T004, T005, T006 can run in parallel (different files in `internal/operation/`)
- T008 can run in parallel with T004–T006 (different project: agent vs desktop)
- T011 can run in parallel with T015 (Go file vs Svelte component)
- T018 can run in parallel with T021 (frontend vs agent service, different files)

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. T001: Read style guidelines
2. T002–T003: Make dialog update in real time
3. **STOP and VALIDATE**: Start a continuous agent operation, verify dialog updates incrementally

### Incremental Delivery

1. US1 → Dialog real-time update (MVP — highest impact bug fix)
2. US2 → Mouse tool split (safety improvement)
3. US3 → Screenshot cursor + shortcut + zoom (perception accuracy)
4. US4 → Tool bubbles + history (visibility improvement)
5. Polish → BUILD.bazel, full build, quickstart validation

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks
- `app.go` changes MUST be sequential across US1 → US2 → US3
- Proto is unchanged — no proto regeneration needed
- No new external dependencies — win32 APIs via existing `syscall` pattern
- `marker.go` deletion (T014) also removes `marker_test.go` if it exists
- Non-Windows stubs (T006) are build-only; no behavioral testing needed

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [GetCursorInfo — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getcursorinfo) — cursor handle, position, visibility (R-001, T011)
- [DrawIconEx — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-drawiconex) — draw cursor onto HDC (R-001, T011)
- [GetIconInfo — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-geticoninfo) — hotspot + bitmap handles (R-001, T011)
- [CURSORINFO structure — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-cursorinfo) — CURSOR_SHOWING / CURSOR_SUPPRESSED flags (R-001, T011)

### Repositories

- [ghp3000/screenshot — GitHub](https://github.com/ghp3000/screenshot) — Go BitBlt + DrawIconEx cursor pattern reference (R-001, T011)
- [langchain-ai/langchainjs — _formatToolOutput](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/langchain-core/src/tools/index.ts#L785-L811) — tool content-block passthrough (inherited from feature 014, T008)

### Articles & RFCs

- [Capture screen shot with mouse cursor — Stack Overflow](https://stackoverflow.com/questions/1628919/capture-screen-shot-with-mouse-cursor) — canonical GetCursorInfo + DrawIconEx reference (R-001, T011)
