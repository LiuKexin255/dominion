# Contract: Input Delivery (Mouse Parts + KeyPart + PartBlock Dispatch)

**Feature**: 018-saolei-mcp | **Decision**: D-3 (revised per user direction)

**Principle**: a `Part` declares the **operation** (mouse move, mouse click, key press) and is **tool-agnostic**; the desktop owns the **implementation**. There are no tool-specific parts. The same mouse parts serve every tool and both delivery paths via an `InputDelivery` enum. See [data-model.md §5](../data-model.md#5-proto-changes-input-delivery-on-mouse-parts--generic-keypart--partblock-dispatch) for the proto definitions.

> This contract supersedes the earlier `window-message-parts.md` (removed), which incorrectly introduced tool-specific `WindowMousePart`/`WindowKeyPart`.

## 1. `InputDelivery` enum + mouse part semantics

`MouseMovePart` and `MouseClickPart` each gain `InputDelivery delivery` (`SIMULATE` | `WINDOW_MESSAGE`, default `SIMULATE`).

| Part | `SIMULATE` (default, existing behavior) | `WINDOW_MESSAGE` (occlusion-free) |
|---|---|---|
| `MouseMovePart{x,y}` | move the physical OS cursor to (x,y) | **no cursor move**; supplies the target coordinate for a message-based click in the same `PartBlock` |
| `MouseClickPart{action}` | click at the *current* physical cursor position | PostMessage `WM_*BUTTON*` for `action` at the coordinate from the companion `MouseMovePart` in the same block |

**Why this shape**: a click is "at current position" in simulate mode (move-then-click is the existing 2-step physical model) and "at the block's declared coordinate" in window-message mode. Unifying on the existing parts + a delivery flag avoids proliferating part types and keeps the desktop logic decoupled from tools.

**Constraint**: a `WINDOW_MESSAGE` click **must** be accompanied by a `MouseMovePart` (coordinate source) in the same `PartBlock`; the desktop rejects a message-mode click with no coordinate source. Parts within one block SHOULD share the same `delivery`.

**Backward compatibility**: the existing mouse tool leaves `delivery` unset → `SIMULATE` → unchanged physical behavior. No existing consumer is affected.

## 2. `KeyPart` (generic key-press operation)

```proto
enum KeyAction { KEY_ACTION_UNSPECIFIED = 0; KEY_ACTION_F2 = 1; /* extensible */ }
message KeyPart { string tool_id = 1; KeyAction key = 2; }
```

`KeyPart` declares a key-press operation only. Per user direction, **the desktop owns the implementation** (PostMessage `WM_KEYDOWN`/`WM_KEYUP` to the bound window). No `delivery` field — if a future key needs physical simulation, it is added as an additive field later.

## 3. `PartBlock` multi-part dispatch (bridge)

`OperationBridge.dispatch` is generalized to dispatch a **`PartBlock`** (one or more parts) as a single operation and await one `ToolResultPart`. A move+click combo is one block → one atomic desktop operation → one result (so the LLM/tool sends one dispatch, not two calls). The single-part path is the one-element block.

The existing dispatch was already PartBlock-shaped (`envelope.content = { parts: [part] }`); the refactor accepts multiple parts and correlates by a single `tool_id` + 5s timeout (existing-design verdict in [plan.md](../plan.md#change-classification)).

## 4. Desktop execution by delivery (Go / Wails, Win32)

The desktop receives a `PartBlock` over the bidi stream and realizes each part per its declared delivery:

| PartBlock | `SIMULATE` realization | `WINDOW_MESSAGE` realization |
|---|---|---|
| `[MouseMovePart{x,y}, MouseClickPart{action}]` | `SetCursorPos`/`SendInput` move to (x,y); then click `action` at current pos (existing physical path) | PostMessage `WM_*BUTTON*`(action) at (x,y) — **no `SetCursorPos`/`SendInput`** (cursor never moves over the target) |
| `[KeyPart{F2}]` | (n/a — KeyPart has no delivery) | PostMessage `WM_KEYDOWN`(VK_F2) then `WM_KEYUP`(VK_F2) |

For `WINDOW_MESSAGE` mouse, the desktop reads the coordinate from the `MouseMovePart` in the block and the action from the `MouseClickPart`, then PostMessages the corresponding `WM_LBUTTONDOWN/UP`, `WM_RBUTTONDOWN/UP`, or L+R (chord for `LEFT_RIGHT_PRESS`) to the bound window's HWND at client-relative (x,y). `LEFT_RIGHT_PRESS` = both buttons down then both up in one operation.

After delivery, the desktop captures a screenshot of the bound window (same path as the mouse tool, spec 014) and returns one `ToolResultPart{ tool_id, status (SUCCEEDED/FAILED), message, screenshot? }`.

**Reuse**: this **extends** the existing input executor (honors `InputDelivery`) — it is not a separate parallel executor, and it does not modify the `SIMULATE` path the existing mouse tool relies on (existing-design verdict, [plan.md](../plan.md#change-classification)).

## 5. Bound-window precondition

Like the existing mouse tool, mouse/key parts **require a bound window**. With no window bound, the dispatch resolves `FAILED` ("desktop disconnected" / "no window bound") — surfaced by the saolei tool as an infrastructure failure (may throw, since it is not a game-rule rejection).

## 6. Backward compatibility summary

- `InputDelivery` field on `MouseMovePart`/`MouseClickPart`: additive (new field numbers), defaults to `SIMULATE`.
- `KeyPart` in `Part.kind`: additive (field 7).
- `OperationBridge.dispatch`: API widened from single-Part to PartBlock; single-part callers pass a one-element block (or the helper keeps the single-Part overload delegating to the block).
- Existing mouse tool: unchanged (leaves `delivery` unset → `SIMULATE`).
