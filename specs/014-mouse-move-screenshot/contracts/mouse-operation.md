# Contract: Mouse Tool & Operation Result Wire Protocol

**Feature**: 014-mouse-move-screenshot
**Date**: 2026-06-25

## 1. Mouse Tool (Agent-Facing)

### Schema

| Field | Type | Description |
|-------|------|-------------|
| `x_px` | number | X coordinate in pixels, image-relative |
| `y_px` | number | Y coordinate in pixels, image-relative |
| `action` | enum | `LEFT_CLICK` \| `LEFT_DOUBLE_CLICK` \| `RIGHT_CLICK` \| `RIGHT_DOUBLE_CLICK` \| `LEFT_RIGHT_PRESS` \| `MOVE` |

### Tool Description (updated)

> Perform a mouse operation at the given image-relative pixel coordinates. Actions: LEFT_CLICK, LEFT_DOUBLE_CLICK, RIGHT_CLICK, RIGHT_DOUBLE_CLICK, LEFT_RIGHT_PRESS, MOVE (reposition cursor without clicking). Results include a post-action screenshot with a marker at the executed position when a window is bound.

### Return Format

The tool callback returns a content-block array:

1. **Text block**: action result status message (e.g., `"ok"` or error description).
2. **Image block** (conditional — only when screenshot is available): `image_url` with `data:image/png;base64,...` URL.
3. **Text block** (conditional — follows image): pixel-dimension annotation `[图片像素尺寸：W×H（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]`

When no screenshot is available (no window bound, capture failed, or size exceeded), only block 1 is returned.

## 2. Operation Result Wire Protocol (Desktop ↔ Agent)

### AgentOperationResultFrame

| Field | Tag | Type | Description |
|-------|-----|------|-------------|
| `operation_id` | 1 | string | Matches the dispatch operation_id |
| `status` | 2 | AgentOperationResultStatus | SUCCEEDED / FAILED / UNSPECIFIED |
| `message` | 3 | string | Human-readable result or error |
| `screenshot` | 4 | AgentImageFrame (optional) | Post-action screenshot with marker |

### Screenshot Presence Rules

| Condition | `screenshot` field | `status` field | `message` field |
|-----------|-------------------|----------------|-----------------|
| Action succeeded, capture succeeded, ≤ 5 MiB | Populated (with marker) | SUCCEEDED | `"ok"` |
| Action succeeded, capture failed | Absent | SUCCEEDED | `"action ok; screenshot failed: <reason>"` |
| Action succeeded, capture > 5 MiB | Absent | SUCCEEDED | `"action ok; screenshot exceeds 5 MiB limit"` |
| Action failed, capture succeeded | Populated (with marker) | FAILED | Action error message |
| Action failed, capture failed | Absent | FAILED | Action error + screenshot failure |
| No window bound | Absent | FAILED | `"no window bound"` |

### AgentImageFrame (screenshot payload)

| Field | Tag | Type | Description |
|-------|-----|------|-------------|
| `encoding` | 2 | ImageEncoding | `IMAGE_ENCODING_PNG` |
| `data` | 3 | bytes | PNG-encoded image data (with overlay marker) |
| `width_px` | 4 | int32 | Image width in pixels |
| `height_px` | 5 | int32 | Image height in pixels |
| `scale_factor` | 6 | double | DPI scale factor of captured display |
| `window_title` | 7 | string | Title of captured window |

## 3. AgentMouseAction Enum (Wire Protocol)

| Value | Name | Behavior |
|-------|------|----------|
| 0 | UNSPECIFIED | Invalid — rejected by validator |
| 1 | LEFT_CLICK | SetCursorPos → left-down → left-up |
| 2 | LEFT_DOUBLE_CLICK | SetCursorPos → (left-down → left-up) ×2 |
| 3 | RIGHT_CLICK | SetCursorPos → right-down → right-up |
| 4 | RIGHT_DOUBLE_CLICK | SetCursorPos → (right-down → right-up) ×2 |
| 5 | LEFT_RIGHT_PRESS | SetCursorPos → left-down → right-down → right-up → left-up |
| 6 | MOVE | SetCursorPos only (no button events) |
