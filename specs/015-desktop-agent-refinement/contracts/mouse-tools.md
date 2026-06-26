# Contract: Mouse Tools (mouse_move + mouse_click)

**Feature**: 015-desktop-agent-refinement
**Date**: 2026-06-26
**Supersedes**: [feature 014 mouse-operation.md](../../014-mouse-move-screenshot/contracts/mouse-operation.md) (single `mouse` tool)

## 1. mouse_move Tool (Agent-Facing)

### Schema

| Field | Type | Description |
|-------|------|-------------|
| `x_px` | number | X coordinate in pixels, image-relative |
| `y_px` | number | Y coordinate in pixels, image-relative |

### Tool Description

> Move the mouse cursor to the given image-relative pixel coordinates without clicking. Use this to position the cursor before a click. When a window is bound, the result includes a post-action screenshot showing the cursor at its new position.

### Dispatch

Builds `AgentOperationFrame` with:
```json
{
  "mouse": {
    "action": "AGENT_MOUSE_ACTION_MOVE",
    "xPx": <x_px>,
    "yPx": <y_px>
  }
}
```

### Return Format

Content-block array (identical to feature 014):
1. Text block: status message (e.g., `"ok"` or error)
2. Image block (conditional): `image_url` with `data:image/png;base64,...`
3. Text block (conditional): pixel-dimension annotation

## 2. mouse_click Tool (Agent-Facing)

### Schema

| Field | Type | Description |
|-------|------|-------------|
| `click_type` | enum | `LEFT_CLICK` \| `LEFT_DOUBLE_CLICK` \| `RIGHT_CLICK` \| `RIGHT_DOUBLE_CLICK` \| `LEFT_RIGHT_PRESS` |

**No coordinate parameters.** The click fires at the current cursor position.

### Tool Description

> Perform a mouse click at the current cursor position. Use mouse_move first to position the cursor. Click types: left click, left double-click, right click, right double-click, simultaneous left+right press. When a window is bound, the result includes a post-action screenshot showing the cursor at the click position.

### Dispatch

Builds `AgentOperationFrame` with:
```json
{
  "mouse": {
    "action": "AGENT_MOUSE_ACTION_<CLICK_TYPE>",
    "xPx": 0,
    "yPx": 0
  }
}
```

`xPx` and `yPx` are always `0` — the desktop ignores them for click actions.

### Return Format

Same content-block array as `mouse_move`.

## 3. Desktop Execution Routing (Desktop ↔ Agent Wire Protocol)

The proto `AgentMouseAction` enum and `AgentOperationFrame` message are unchanged from feature 014. The desktop's `executeAgentOperation` routes by action:

| Action | Desktop Behavior | Coordinate Conversion |
|--------|-----------------|----------------------|
| `MOVE` | `ScreenshotToScreenCoords` → `MoveCursor` (SetCursorPos only) | Yes (screenshot-relative → screen-absolute) |
| `LEFT_CLICK` / `LEFT_DOUBLE_CLICK` / `RIGHT_CLICK` / `RIGHT_DOUBLE_CLICK` / `LEFT_RIGHT_PRESS` | `ExecuteClickAtCurrentPos` (button events only, no SetCursorPos) | No (clicks at current position) |

## 4. Post-Action Screenshot

Both tools receive a post-action screenshot when a window is bound (inheriting feature 014). The screenshot now includes the real OS cursor (rendered by `DrawCursor`) instead of a self-drawn marker ring. See [screenshot.md](screenshot.md) for the cursor rendering contract.

## 5. Historical Compatibility

Conversations recorded with the old single `mouse` tool (action + coordinates) must still render correctly in the history view. The old `AgentOperationFrame` format is identical to the new format — only the agent-side tool boundary changed. No data migration required.
