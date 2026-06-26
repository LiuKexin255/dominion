# Quickstart: Desktop Agent Interaction Refinement

**Feature**: 015-desktop-agent-refinement
**Date**: 2026-06-26

## Prerequisites

- Windows machine with the Dominion desktop app built via `bazel build //projects/game/desktop`
- Agent service running (or gateway reachable)
- A target window to bind (e.g., a browser, notepad, or game)

## Validation Scenarios

### Scenario 1: Real-Time Dialog Update (US1, P1)

**Goal**: Verify the conversation dialog updates incrementally during continuous agent operations.

1. Launch the desktop app, select a session, connect, and bind a window.
2. Send a message that triggers a multi-step agent operation (e.g., "click the top-left corner, then click the center").
3. **Expected**: Streaming text, tool results, and screenshot consumption appear incrementally in the dialog as they occur — NOT deferred until the agent completes.
4. **Expected**: The pending screenshot preview clears immediately when the message is sent (not after the agent finishes).

### Scenario 2: Mouse Tool Split (US2, P2)

**Goal**: Verify `mouse_move` and `mouse_click` are separate tools with correct behavior.

1. Instruct the agent: "Move the cursor to the center of the window."
2. **Expected**: The agent invokes `mouse_move` (not `mouse` or `mouse_click`).
3. **Expected**: The cursor moves to the specified position; no click events are dispatched.
4. **Expected**: A post-action screenshot is returned showing the cursor at the new position.
5. Instruct the agent: "Now left-click."
6. **Expected**: The agent invokes `mouse_click` with `click_type: LEFT_CLICK`.
7. **Expected**: The click fires at the current cursor position (where `mouse_move` left it); no cursor movement occurs.
8. **Expected**: A post-action screenshot is returned.

### Scenario 3: Screenshot Cursor Rendering (US3, P3)

**Goal**: Verify screenshots include the real OS cursor.

1. Bind a window and position the cursor at a visible location within the window.
2. Press `Ctrl+Shift+S` to capture a screenshot (without moving the cursor).
3. **Expected**: The captured screenshot shows the real OS cursor (with its actual cursor icon, e.g., arrow pointer) at the position where it was.
4. **Expected**: No red-ring marker overlay is present (the old marker is removed).
5. Attach the screenshot to a message and send it.
6. **Expected**: The screenshot thumbnail appears in the pending attachment area.

### Scenario 4: Click-to-Zoom (US3, P3)

**Goal**: Verify screenshots can be zoomed.

1. Capture a screenshot (via button or `Ctrl+Shift+S`).
2. Click the pending attachment thumbnail.
3. **Expected**: A full-screen modal opens showing the screenshot at a larger size.
4. Press `Escape` or click the overlay.
5. **Expected**: The modal closes, returning to the composition view.
6. Send the message. In the message thread, click the image entry.
7. **Expected**: The zoom modal opens again for the sent screenshot.

### Scenario 5: Tool Result Bubble Display (US4, P4)

**Goal**: Verify tool results are displayed collapsed in operation bubbles.

1. Trigger an agent operation (e.g., ask the agent to move the cursor).
2. **Expected**: The operation-result bubble shows status (✓/✗) and message.
3. **Expected**: If the result includes a screenshot, a collapsed `<details>` section labeled "Result screenshot" is visible.
4. Click to expand.
5. **Expected**: The screenshot is shown inline.
6. Click the screenshot.
7. **Expected**: The zoom modal opens.

### Scenario 6: History Consistency (US4, P4)

**Goal**: Verify historical conversations render identically to live view.

1. Have a conversation with multiple tool operations and results.
2. Navigate away (back to sessions) and re-enter the same session.
3. **Expected**: The conversation history loads with all messages, including tool operations and results, rendered identically to how they appeared during the live conversation.
4. **Expected**: Operation bubbles, result bubbles (with collapsed screenshots), text messages, and image messages all render with the same layout as live view.

## Build & Test Commands

```bash
# Build the desktop app
bazel build //projects/game/desktop

# Run Go unit tests (operation package)
bazel test //projects/game/desktop/internal/operation:all

# Run agent TypeScript tests
bazel test //projects/game/agent/src:mouse-tool_test

# Run frontend component tests
bazel test //projects/game/desktop/frontend:all

# Format and update after code changes
bazel run //:go -- fmt [changed files]
bazel run //:gazelle projects/game/desktop
bazel mod tidy
```

## Style Documents to Read Before Implementation

- `style/golang.md` — Go style guide (for `cursor.go`, `execute_v2.go` changes)
- `style/README.md` — TypeScript/Svelte style guide (for `mouse-tool.ts`, `ChatView.svelte` changes)
