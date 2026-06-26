# Data Model: Desktop Agent Interaction Refinement

**Feature**: 015-desktop-agent-refinement
**Date**: 2026-06-26

## Proto Changes

### Message — content oneof extended (修改)

The `operation` and `operation_result` fields are moved INTO the existing
`content` oneof (alongside `text` and `image_data`), so a single Message
carries exactly one body — text, image, a tool invocation, or a tool result.
The `type` field comment is updated to list all six values.

Field numbers are unchanged (`operation = 8`, `operation_result = 9`), so the
change is wire-compatible with existing serialized history.

```
message Message {
  // ... name, message_id, sender, type, create_time unchanged ...

  string type = 4;   // "text" | "thinking" | "warn" | "image"
                      //   | "operation" | "operation_result"

  oneof content {
    string text = 5;
    bytes image_data = 7;
    AgentOperationFrame operation = 8;
    AgentOperationResultFrame operation_result = 9;
  }
}
```

**Classification (§IV)**: 修改 — refactor of an existing message. **Design
verdict**: previously `operation` and `operation_result` were independent
optional fields, yet a single history Message is semantically either a tool
call OR a tool result (the `type` field already encoded this exclusivity, and
both are exclusive with text/image too). Modeling them as separate optionals
permitted the invalid state where multiple bodies are set simultaneously.
Promoting them into the `content` oneof makes the mutual-exclusivity invariant
structural and unifies the body discriminator. The prior design did not serve
the goal; the oneof corrects it. Getters (`GetOperation()` /
`GetOperationResult()`) remain valid and return `nil` when their case is
inactive, so all existing read sites are unaffected.

### AgentMouseAction (unchanged)

```
enum AgentMouseAction {
  AGENT_MOUSE_ACTION_UNSPECIFIED = 0;
  AGENT_MOUSE_ACTION_LEFT_CLICK = 1;
  AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK = 2;
  AGENT_MOUSE_ACTION_RIGHT_CLICK = 3;
  AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK = 4;
  AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS = 5;
  AGENT_MOUSE_ACTION_MOVE = 6;
}
```

No proto change. The enum and all values are unchanged from feature 014. The tool split is entirely on the agent side — both tools dispatch `AgentMouseOperation` frames with the same proto.

### AgentMouseOperation (unchanged)

```
message AgentMouseOperation {
  AgentMouseAction action = 1;
  int32 x_px = 2;
  int32 y_px = 3;
}
```

No proto change. For `mouse_click`, `x_px` and `y_px` are sent as `0` and ignored by the desktop. For `mouse_move`, `x_px` and `y_px` carry the screenshot-relative coordinates.

### AgentOperationResultFrame (unchanged)

```
message AgentOperationResultFrame {
  string operation_id = 1;
  AgentOperationResultStatus status = 2;
  string message = 3;
  AgentImageFrame screenshot = 4;
}
```

No proto change. The `screenshot` field now carries an image that includes the real OS cursor (rendered by `DrawCursor`), without the self-drawn marker ring (removed).

### AgentImageFrame (unchanged)

No change. The screenshot data is still PNG-encoded; the only difference is that the PNG now contains the cursor at its real position.

## TypeScript Type Changes (Agent Side)

### mouse-tool.ts — Tool Split (修改)

```typescript
// ─── mouse_move tool ───────────────────────────────────────────────────────

const mouseMoveSchema = z.object({
  x_px: z.number().describe("X coordinate in pixels, image-relative"),
  y_px: z.number().describe("Y coordinate in pixels, image-relative"),
});

export function createMouseMoveTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x_px, y_px }): Promise<MouseContentBlock[]> => {
      const frame: AgentOperationFrame = {
        mouse: {
          action: "AGENT_MOUSE_ACTION_MOVE",
          xPx: x_px,
          yPx: y_px,
        },
      };
      const result = await bridge.dispatch(frame);
      return buildResultBlocks(result);
    },
    {
      name: "mouse_move",
      description:
        "Move the mouse cursor to the given image-relative pixel coordinates " +
        "without clicking. Use this to position the cursor before a click. " +
        "When a window is bound, the result includes a post-action screenshot " +
        "showing the cursor at its new position.",
      schema: mouseMoveSchema,
    },
  );
}

// ─── mouse_click tool ──────────────────────────────────────────────────────

const CLICK_TYPES = [
  "LEFT_CLICK",
  "LEFT_DOUBLE_CLICK",
  "RIGHT_CLICK",
  "RIGHT_DOUBLE_CLICK",
  "LEFT_RIGHT_PRESS",
] as const;

const CLICK_TYPE_TO_PROTO = {
  LEFT_CLICK: "AGENT_MOUSE_ACTION_LEFT_CLICK",
  LEFT_DOUBLE_CLICK: "AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK",
  RIGHT_CLICK: "AGENT_MOUSE_ACTION_RIGHT_CLICK",
  RIGHT_DOUBLE_CLICK: "AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK",
  LEFT_RIGHT_PRESS: "AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS",
} as const;

const mouseClickSchema = z.object({
  click_type: z
    .enum(CLICK_TYPES)
    .describe("Click type to perform at the current cursor position"),
});

export function createMouseClickTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ click_type }): Promise<MouseContentBlock[]> => {
      const frame: AgentOperationFrame = {
        mouse: {
          action: CLICK_TYPE_TO_PROTO[click_type],
          xPx: 0,
          yPx: 0,
        },
      };
      const result = await bridge.dispatch(frame);
      return buildResultBlocks(result);
    },
    {
      name: "mouse_click",
      description:
        "Perform a mouse click at the current cursor position. Use mouse_move " +
        "first to position the cursor. Click types: left click, left " +
        "double-click, right click, right double-click, simultaneous " +
        "left+right press. When a window is bound, the result includes a " +
        "post-action screenshot showing the cursor at the click position.",
      schema: mouseClickSchema,
    },
  );
}

// ─── shared result-block builder ───────────────────────────────────────────

function buildResultBlocks(result: OperationResult): MouseContentBlock[] {
  const blocks: MouseContentBlock[] = [
    { type: "text", text: result.message },
  ];
  if (result.screenshot) {
    blocks.push({
      type: "image_url",
      image_url: {
        url: `data:image/png;base64,${result.screenshot.data}`,
      },
    });
    blocks.push({
      type: "text",
      text: `[图片像素尺寸：${result.screenshot.widthPx}×${result.screenshot.heightPx}（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]`,
    });
  }
  return blocks;
}
```

### session-agent.ts — Tool Registration (修改)

```typescript
// Before (feature 014):
const tools = [createMouseTool(bridge), ...otherTools];

// After (feature 015):
const tools = [createMouseMoveTool(bridge), createMouseClickTool(bridge), ...otherTools];
```

## Go Type Changes (Desktop Side)

### operation/execute_v2.go — Function Split (修改)

```go
// MoveCursor moves the cursor to the given absolute screen coordinates.
// Validates coordinates against the virtual desktop bounds.
func MoveCursor(screenX, screenY int32) error {
    rect, err := virtualScreenRect()
    if err != nil {
        return err
    }
    if err := validateScreenCoords(screenX, screenY, rect); err != nil {
        return err
    }
    return setCursorPos(screenX, screenY)
}

// ExecuteClickAtCurrentPos dispatches button events for the given click
// action at the cursor's current position. Does NOT move the cursor.
func ExecuteClickAtCurrentPos(action game.AgentMouseAction) error {
    if err := validateClickAction(action); err != nil {
        return err
    }
    events, err := actionEventSequence(action)
    if err != nil {
        return err
    }
    for _, flag := range events {
        sendInput(mouseInput{
            Type: inputMouse,
            Mi:   mouseEvent{DwFlags: flag},
        })
    }
    return nil
}
```

### operation/execute_v2_logic.go — Click Validator (新增 helper)

```go
// validateClickAction rejects MOVE and UNSPECIFIED — only button-press
// actions are valid for ExecuteClickAtCurrentPos.
func validateClickAction(action game.AgentMouseAction) error {
    switch action {
    case game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_CLICK,
        game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK,
        game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_CLICK,
        game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK,
        game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS:
        return nil
    default:
        return fmt.Errorf("not a click action: %v", action)
    }
}
```

### operation/cursor.go — Win32 Cursor Drawing (新增)

```go
//go:build windows

package operation

// Win32 constants for cursor drawing.
const (
    cursorShowing    uint32 = 0x00000001 // CURSOR_SHOWING
    cursorSuppressed uint32 = 0x00000002 // CURSOR_SUPPRESSED
    diNormal         uint32 = 0x00000003 // DI_NORMAL
)

// CURSORINFO matches the Win32 CURSORINFO structure.
type cursorInfo struct {
    CbSize      uint32
    Flags       uint32
    HCursor     uintptr
    PtScreenPos point
}

// ICONINFO matches the Win32 ICONINFO structure.
type iconInfo struct {
    FIcon    uint32 // BOOL: 0 = cursor, 1 = icon
    XHotspot uint32
    YHotspot uint32
    HbmMask  uintptr
    HbmColor uintptr
}

// DrawCursor overlays the real OS cursor onto the provided image at the
// cursor's current screen position relative to the captured window bounds.
// If the cursor is hidden or suppressed, the image is returned unchanged.
func DrawCursor(img *image.RGBA, winLeft, winTop int32) error { ... }
```

### operation/marker.go — Removed (削除)

The entire file (`ApplyMarker`, `drawRing`, `setPixel`) is deleted. The marker constants (`markerRadius`, `markerColor`) are deleted.

## Frontend Type Changes (Svelte)

### ChatEntry — Extended for History (修改)

```typescript
// App.svelte / ChatView.svelte — ChatEntry already supports operation types.
// No type change needed. The change is in handleLoadMessages mapping logic.

// handleLoadMessages (App.svelte) — extended typeFromString:
function typeFromString(raw: string): 'thinking' | 'text' | 'warn' | 'operation' | 'operation_result' | 'image' {
  if (['thinking', 'text', 'warn', 'operation', 'operation_result', 'image'].includes(raw)) return raw as any
  return 'text'
}
```

### ChatView.svelte — operation_result Bubble Extension (修改)

The `operation_result` rendering gains a `<details>` section:

```svelte
{:else if msg.type === 'operation_result' && msg.operationResult}
  {@const result = msg.operationResult}
  {@const succeeded = isOperationSucceeded(result.status)}
  <div class="op-result-card" ...>
    <span class="op-result-icon">{succeeded ? '✓' : '✗'}</span>
    <span class="op-result-status">{succeeded ? 'succeeded' : 'failed'}</span>
    {#if result.message}
      <span class="op-result-message">{result.message}</span>
    {/if}
    <!-- NEW: collapsed screenshot when present -->
    {#if result.screenshot}
      <details class="op-result-details">
        <summary>Result screenshot</summary>
        <img
          class="screenshot-img clickable"
          src="data:image/png;base64,{result.screenshot.data}"
          alt="Operation result screenshot"
          onclick={() => onZoom("data:image/png;base64," + result.screenshot.data)}
        />
      </details>
    {/if}
  </div>
```

### ScreenshotModal.svelte — New Component (新增)

```svelte
<script lang="ts">
  let { imageUrl, onClose }: { imageUrl: string; onClose: () => void } = $props()

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose()
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="modal-overlay" onclick={onClose} role="button" tabindex={0}>
  <img class="modal-image" src={imageUrl} alt="Screenshot" />
</div>
```

## Validation Rules

| Rule | Scope | Source |
|------|-------|--------|
| mouse_click does not accept coordinates | Agent `mouse-tool.ts` schema | FR-003 |
| mouse_move dispatches only MOVE action | Agent `mouse-tool.ts` | FR-002 |
| Click actions do NOT call SetCursorPos | Desktop `ExecuteClickAtCurrentPos` | FR-003 |
| MOVE action calls SetCursorPos only | Desktop `MoveCursor` | FR-002 |
| Screenshot includes real OS cursor when visible | Desktop `DrawCursor` in `CaptureWindow` | FR-009 |
| Cursor hidden/suppressed → no cursor drawn | Desktop `DrawCursor` flag check | FR-009 edge case |
| Self-drawn marker removed | Desktop `marker.go` deleted | FR-009 (real cursor replaces marker) |
| SendUserTurn returns after send (non-blocking) | Desktop `app.go` | FR-007 |
| pendingScreenshot cleared before await | Frontend `App.svelte` | FR-008 |
| History maps operation/operation_result | Frontend `App.svelte handleLoadMessages` | FR-013/FR-014 |
| Tool-result bubble shows collapsed result | Frontend `ChatView.svelte` | FR-012 |
| Keyboard shortcut triggers capture without mouse | Frontend `App.svelte` | FR-010 |
| Click-to-zoom on any screenshot image | Frontend `ScreenshotModal` + ChatView | FR-011 |
