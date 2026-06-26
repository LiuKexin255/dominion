# Contract: Screenshot Cursor Rendering, Keyboard Shortcut, and Click-to-Zoom

**Feature**: 015-desktop-agent-refinement
**Date**: 2026-06-26

## 1. Screenshot Cursor Rendering (Desktop Capture Pipeline)

### Contract

Screenshots captured by `CaptureWindow` MUST include the real OS-rendered cursor at its actual on-screen position when the cursor is visible. The cursor is drawn using the win32 API sequence:

1. `GetCursorInfo` — retrieve cursor handle, screen position, visibility flags
2. If `flags == CURSOR_SHOWING` and `flags & CURSOR_SUPPRESSED == 0`:
   - `GetIconInfo` — get hotspot and bitmap handles
   - `GetObject(hbmColor)` — get actual pixel dimensions
   - Compute position: `drawX = ptScreenPos.x - xHotspot - windowLeft`, `drawY = ptScreenPos.y - yHotspot - windowTop`
   - Draw onto the captured image via GDI memory-DC round-trip with `DrawIconEx(hdc, drawX, drawY, hCursor, width, height, 0, NULL, DI_NORMAL)`
   - `DeleteObject(hbmMask)`, `DeleteObject(hbmColor)`
3. If cursor is hidden/suppressed: screenshot is returned without cursor (no error)

### Marker Removal

The self-drawn red-ring marker (`ApplyMarker`) from feature 014 is **removed**. Screenshots no longer have a marker overlay — the real cursor replaces it.

### Cursor Position Semantics

The cursor's screen position (`ptScreenPos`) is relative to the virtual desktop origin (may be negative on multi-monitor). The draw position on the screenshot image is:

```
imageX = ptScreenPos.x - iconInfo.xHotspot - windowBounds.Left
imageY = ptScreenPos.y - iconInfo.yHotspot - windowBounds.Top
```

Where `windowBounds.Left/Top` are the screen coordinates of the captured window's top-left corner (from `CaptureWindowBounds`). If the resulting `(imageX, imageY)` falls outside the image bounds, the cursor is clipped (DrawIconEx handles this natively).

### Limitations

- **No cursor shadow**: DWM renders shadows separately; they are not captured.
- **Animated cursors**: only the first frame is drawn (`istepIfAniCur = 0`).
- **DPI**: explicit pixel dimensions from `GetObject` are used (not `DI_DEFAULTSIZE`) to ensure correct rendering at any DPI.

## 2. Keyboard Shortcut for Screenshot Capture (Frontend)

### Contract

The desktop frontend provides a keyboard shortcut (`Ctrl+Shift+S`) that triggers screenshot capture of the bound window without mouse interaction.

### Behavior

| Aspect | Specification |
|--------|--------------|
| Shortcut | `Ctrl+Shift+S` |
| Scope | Active only when the chat page is visible and a window is selected |
| Action | Calls `handleCaptureScreenshot()` (same as the "Capture Screenshot" button) |
| Cursor displacement | None — the shortcut does not move the cursor |
| Conflict handling | If the shortcut conflicts with an existing binding, it can be changed without code changes (configurable constant) |

### Rationale

Clicking the "Capture Screenshot" button moves the cursor to the button, making it impossible to test "screenshot includes cursor at position X" scenarios. The keyboard shortcut allows the user to position the cursor at a target and capture without displacement.

## 3. Click-to-Zoom for Screenshots (Frontend)

### Contract

Any screenshot image displayed in the desktop frontend supports click-to-open, showing the screenshot at maximum fit size in a modal overlay.

### Trigger Points

| Location | Current Behavior | New Behavior |
|----------|-----------------|--------------|
| Pending attachment thumbnail (input area) | Static thumbnail (48px height) | Click opens zoom modal |
| Image entry in message thread | `<details>` with inline image | Click on image opens zoom modal |
| Operation-result screenshot (collapsed) | Not displayed | Click on image opens zoom modal |

### Modal Behavior

| Aspect | Specification |
|--------|--------------|
| Open | Click on any screenshot image |
| Display | Full-screen dark overlay, image centered, scaled to fit viewport while preserving aspect ratio |
| Close | Click anywhere on overlay, or press `Escape` |
| Background scroll | Disabled while modal is open |

## 4. Screenshot Size Limit (unchanged)

Screenshots remain subject to the 5 MiB maximum (inherited from feature 014). The cursor overlay adds negligible size (cursor bitmap is tiny relative to the window image). No size-limit change needed.
