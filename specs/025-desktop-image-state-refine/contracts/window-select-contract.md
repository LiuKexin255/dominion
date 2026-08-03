# Contract: Selected window — single source of truth

**Feature**: [spec.md](../spec.md) (FR-001..FR-006) | **Research**: [research.md](../research.md) D3 | **Data model**: [data-model.md](../data-model.md) §3

This contract specifies the window-select flow: the window chosen in the desktop session chat page is the target for every screenshot and every operation, with no separate "binding" step.

## 1. Problem (why)

There are two notions of "the active window" today:

- Frontend: `selectedWindowHandle` (`projects/game/desktop/frontend/src/App.svelte:125`), set by the window dropdown (`App.svelte:880-885`).
- Backend: `App.boundWin` (`projects/game/desktop/app.go:200`), set **only** by `BindWindow` (`app.go:1249-1277`), which is called **only** inside `handleCaptureScreenshot` (`App.svelte:770-788`).

Selecting a window without clicking Capture leaves `boundWin` zero-valued, so every operation fails at `app.go:1074` (`"no window bound"`) and the post-action screenshot is skipped (`app.go:1129`). This is the reported defect.

## 2. Single source of truth

The `boundWin` field and the `BindWindow`/`CaptureScreenshot` two-step are **removed** (Constitution §II — collapse the redundant layer). The selected window handle is passed to each operation and each screenshot capture; the backend resolves the `capture.WindowRef` (`projects/game/desktop/internal/capture/window.go:14-21`) from the selected handle at use time.

### 2.1 Frontend (`App.svelte`)

- The dropdown continues to bind `selectedWindowHandle`. Selecting a window is sufficient — no follow-up action is required to "activate" it.
- The manual "Capture Screenshot" affordance (button `App.svelte:886-888`, `Ctrl+Shift+S` `App.svelte:186-194`) is retained as a **user-initiated "attach a screenshot to my next message"** action; it captures from the selected window (no `bindWindow` call). It is no longer a prerequisite for operations.
- `handleCaptureScreenshot` drops the `bindWindow(selectedWindowHandle)` call and captures directly from the selected window.

### 2.2 Backend (`app.go`, `app_operation.go`)

- `App.boundWin` field removed. `BindWindow` removed. `CaptureScreenshot` (`app.go:1279-1309`) takes the selected handle (resolves `WindowRef`) instead of reading `a.boundWin`.
- `executeAgentOperation` (`app.go:1032`) resolves the selected `WindowRef` (or returns a graceful "no window selected" failure, FR-005 — replacing the `app.go:1074` guard) and passes the handle into the executors.
- Mouse executors `runMouseMoveAndClick` / `runMouseMove` / `runMouseClick` (`projects/game/desktop/app_operation.go:28,94,156`) take the resolved window handle as a parameter instead of reading `a.boundWin.Handle`.
- `ScaleFactor`/`WindowTitle` for `ImagePart`/`FlowResultPart.screenshot` are read from the resolved selected-window `WindowRef` at capture time (replacing `app.go:719-720,1146-1147`).
- `SendUserTurn` (`app.go:693-758`) reads the selected window's `ScaleFactor`/`WindowTitle` for the inbound `ImagePart`.

### 2.3 Resolution

The backend resolves the `WindowRef` from the selected handle via a `capture.ListWindows` lookup (the same lookup `BindWindow` performs today, `app.go:1249-1277`). If the handle is not found (window closed between selection and use), the operation/screenshot fails gracefully (FR-005 / §3).

## 3. Validity & failure handling

| Condition | Behavior |
|---|---|
| A window is selected (no Capture pressed) | operation executes against it; post-action screenshot captured and returned (FR-002/FR-003) |
| No window selected | operation/screenshot fails gracefully with a clear user-facing message — no crash, no silent no-op (FR-005) |
| Selected window closed/minimized/hidden between selection and use | `capture.CaptureWindow` rejects it (existing validation, `projects/game/desktop/internal/capture/capture.go`); surfaced as a clear failure; the user selects another window |
| A different window selected mid-session | the next operation/screenshot targets the new selection (FR-004) |

## 4. Out of scope

- The window-listing API (`ListWindows`, `app.go:1233-1247`) and the `WindowRef`/`CapturedImage` types are unchanged.
- Multi-window or multi-target operations are not introduced; there is exactly one selected window per session.

## 5. Test anchors

- Frontend: selecting a window and sending a message (without pressing Capture) yields an operation that executes against the selected window and returns a post-action screenshot — no "no window bound" failure.
- Backend unit: `executeAgentOperation` with a selected handle resolves the window and runs; with no handle, returns the graceful "no window selected" failure; `BindWindow` no longer exists.
- Large test: select-then-chat end-to-end ([quickstart.md](../quickstart.md)).
