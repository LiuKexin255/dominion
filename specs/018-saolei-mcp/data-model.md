# Data Model: Saolei MCP

**Feature**: 018-saolei-mcp | **Date**: 2026-07-14 | **Source**: [spec.md](./spec.md), [plan.md](./plan.md), [research.md](./research.md)

This document defines the domain entities, state machines, coordinate model, proto Part additions, and profile-data extensions for the saolei MCP. Tool input/output schemas (Zod) live in [contracts/](./contracts/); this document covers the data shapes and transitions.

## 1. Cell State

The state of a single board cell. A fixed enumeration (spec FR-006).

| Value | Meaning | Category |
|---|---|---|
| `block` | initial unrevealed cell | coverable |
| `zero` .. `eight` | revealed number cell (adjacent-mine count) | revealed (terminal for that cell) |
| `flag` | mine marker placed by the agent | flagged |
| `boom` | detonated mine | terminal (game over) |

**Invariants**:
- Number states (`zero`..`eight`) and `boom` are **terminal** for that cell — they never transition back to `block` or `flag` (FR-022 rejects such transitions).
- Only `saolei_flag` (right-button) may produce/clear a `flag` state (FR-020). No other tool transitions a cell to/from `flag`.
- Coordinates are zero-indexed from the top-left: `(0,0)` is the top-left cell (FR-007).

## 2. Board State (per-session, in-memory)

Owned by the saolei MCP instance; **never persisted**; scoped to one session (FR-008, FR-025b).

```
BoardState {
  width:  number            // x cells, set by saolei_init
  height: number            // y cells, set by saolei_init
  grid:    CellState[height][width]   // row-major; grid[y][x]
  lifecycle: "uninitialized" | "ready" | "awaiting-update" | "terminal"
}
```

**Lifecycle marker values**:

| Marker | Meaning | Entry | Exit |
|---|---|---|---|
| `uninitialized` | no board yet (before first `saolei_init`) | MCP instance creation | `saolei_init` → `ready` |
| `ready` | board initialized; an operation is permitted | `saolei_init`; or `saolei_update` accepted | any of click/flag/double_click → `awaiting-update` |
| `awaiting-update` | an operation was dispatched; **no further operation permitted** until `saolei_update` | `saolei_click`/`saolei_flag`/`saolei_double_click` dispatched | `saolei_update` accepted → `ready` |
| `terminal` | a `boom` is present on the board | `saolei_update` reports a `boom` | `saolei_init` only → `ready` |

**State machine** (spec FR-009..012, FR-011a, FR-023):

```
                 saolei_init (F2, reset to all 'block')
  uninitialized ──────────────────────────────────────► ready
                                                            │
                                       click/flag/double_click (dispatched)
                                                            ▼
                                                      awaiting-update
                                                            │
                                            saolei_update accepted
                                                            ▼
              ┌─────────────────────────────────────────── ready ◄─────────────┐
              │ saolei_update reports 'boom'                                      │
              ▼                                                                  │
           terminal                                                             │
              │                                                                  │
              │ saolei_init only (no other op accepted while terminal)           │
              └──────────────────────────────────────────────────────────────────┘
```

**Rules**:
- `saolei_init` may be called from **any** state and always resets to all-`block` + `ready` (FR-009, edge case).
- There is **no automatic timeout** out of `awaiting-update`; recovery is `saolei_init` only (FR-011a, clarification Q1).
- `awaiting-update` blocks **all** positional operations, not just the one used (FR-010, FR-017).
- A new MCP instance (adapter rebuild) starts at `uninitialized` and discards all prior state (FR-025c).

## 3. MCP Instance Lifecycle (per-session)

The saolei MCP instance is owned by the `SessionAgent`, mirroring the existing per-session `OperationBridge` (spec FR-025a, plan D-4).

```
SessionAgent (per session)
 ├── OperationBridge         (existing; agent → desktop operation channel)
 ├── checkpointer            (existing)
 └── saoleiMcp?: SaoleiMcp   (新增; created lazily when profile declares mcp_names=["saolei"])
```

- **Creation**: lazily, when the session's profile declares the `saolei` mcp, at adapter bind time. Each `SessionAgent` has at most one saolei MCP instance.
- **Isolation**: state is confined to the owning session; no cross-session visibility (FR-025b).
- **Disposal**: when the adapter is invalidated/rebuilt (profile refresh, `invalidateAdapter`), the old MCP instance is dropped; a new instance starts at `uninitialized` with no carry-over board (FR-025c).

## 4. Coordinate Model (constants pinned by user)

Module constants in `mcp/saolei/geometry.ts` (spec FR-013, FR-015; plan D-6):

| Constant | Value | Unit |
|---|---|---|
| `TOP_OFFSET` | `200` | px (board top edge from window top) |
| `LEFT_OFFSET` | `24` | px (board left edge from window left) |
| `BLOCK_LENGTH` | `32` | px (cell width = cell height) |

**Cell-centre → window-client-relative pixel** for grid coordinate `(x, y)`:

```
X_px = LEFT_OFFSET + x * BLOCK_LENGTH + BLOCK_LENGTH / 2
     = 24 + x*32 + 16     (e.g. x=0 → 40, x=1 → 72)
Y_px = TOP_OFFSET  + y * BLOCK_LENGTH + BLOCK_LENGTH / 2
     = 200 + y*32 + 16    (e.g. y=0 → 216, y=1 → 248)
```

All coordinates are **window-client-relative pixels** (the same space the existing mouse tool uses). The desktop translates these to the bound window's client area for `PostMessage` delivery. **Bounds check**: a coordinate is in-bounds iff `0 <= x < width && 0 <= y < height` (FR-021).

## 5. Proto Changes: Input Delivery on Mouse Parts + Generic KeyPart + PartBlock Dispatch

**Principle** (user direction): a `Part` declares the **operation** (mouse move, mouse click, key press) and is **tool-agnostic**; the desktop owns the **implementation**. We do NOT add tool-specific parts. Instead: (a) the existing mouse parts gain an `InputDelivery` enum so the same part serves every tool and both delivery paths; (b) a new generic `KeyPart` declares key-press operations; (c) the bridge dispatches a `PartBlock` (one or more parts) so a move+click combo is one atomic operation. Occlusion-free input (FR-014) is achieved via `WINDOW_MESSAGE` delivery — the desktop PostMessages the bound window without moving the OS cursor.

### 5a. New `InputDelivery` enum + delivery field on mouse parts (修改)

```proto
// InputDelivery tells the desktop HOW to realize a mouse operation.
// Parts declare the operation; the desktop implements per the declared delivery.
enum InputDelivery {
  INPUT_DELIVERY_UNSPECIFIED     = 0;
  INPUT_DELIVERY_SIMULATE        = 1;  // physical cursor (existing behavior) — default
  INPUT_DELIVERY_WINDOW_MESSAGE  = 2;  // PostMessage to the bound window; no cursor move
}

// MouseMovePart — extended with delivery. (field 4 is new; 1-3 unchanged)
message MouseMovePart {
  string tool_id   = 1;
  int32 x_px       = 2;
  int32 y_px       = 3;
  InputDelivery delivery = 4;   // default SIMULATE when unset
}

// MouseClickPart — extended with delivery. (field 3 is new; 1-2 unchanged)
message MouseClickPart {
  string tool_id   = 1;
  MouseClickAction click = 2;
  InputDelivery delivery = 3;   // default SIMULATE when unset
}
```

**Semantics by delivery**:

| Part | `SIMULATE` (default) | `WINDOW_MESSAGE` |
|---|---|---|
| `MouseMovePart{x,y}` | move the physical cursor to (x,y) | **no cursor move**; supplies the target coordinate for a message-based click in the same `PartBlock` |
| `MouseClickPart{action}` | click at the *current* physical cursor position | PostMessage `WM_*BUTTON*` for `action` at the coordinate supplied by the companion `MouseMovePart` in the same block |

**Constraint**: a `WINDOW_MESSAGE` click **must** be accompanied by a `MouseMovePart` (its coordinate source) in the same `PartBlock`; the desktop rejects a message-mode click with no coordinate source. All parts in one block SHOULD share the same `delivery` (a block is one operation realized one way).

**Backward compatibility**: the existing mouse tool leaves `delivery` unset → `SIMULATE` → current physical behavior. No change to existing consumers.

### 5b. New generic `KeyPart` + `KeyAction` enum (新增 — additive to `Part.kind`)

```proto
// KeyAction enumerates key-press operations the desktop can realize.
// Extensible; the desktop implements each (PostMessage WM_KEYDOWN/UP by default).
enum KeyAction {
  KEY_ACTION_UNSPECIFIED = 0;
  KEY_ACTION_F2          = 1;  // VK_F2 (0x71) — Minesweeper "new game"
}

// KeyPart declares a key-press operation. tool_id links it to its ToolResultPart.
// The desktop owns the implementation (PostMessage WM_KEYDOWN/WM_KEYUP to the bound window).
message KeyPart {
  string tool_id = 1;
  KeyAction key   = 2;
}
```

Add `KeyPart` to the `Part.kind` oneof (additive, field 7):

```proto
message Part {
  oneof kind {
    TextPart        text          = 1;
    ThinkingPart    thinking      = 2;
    ImagePart       image         = 3;
    MouseMovePart   mouse_move    = 4;
    MouseClickPart  mouse_click   = 5;
    ToolResultPart  tool_result   = 6;
    KeyPart         key_press     = 7;   // 新增 (generic key-press operation)
  }
}
```

> `KeyPart` declares the operation only — per user direction, the desktop decides the implementation (PostMessage to the bound window). A `delivery` field is intentionally omitted from `KeyPart`; if a future key needs physical simulation, it can be added as an additive field.

### 5c. `PartBlock` multi-part dispatch (bridge 修改)

The `OperationBridge.dispatch` is generalized to accept a **`PartBlock`** (one or more parts) and return one `ToolResultPart`. A move+click combo is dispatched as one block (e.g. `[MouseMovePart{x,y,WINDOW_MESSAGE}, MouseClickPart{LEFT_CLICK,WINDOW_MESSAGE}]`) → one atomic desktop operation → one result. The single-part path is the one-element block.

### 5d. Tool → PartBlock mapping (saolei)

| saolei tool | dispatched `PartBlock` | desktop action |
|---|---|---|
| `saolei_init` | `[KeyPart{F2}]` | PostMessage `WM_KEYDOWN/UP` VK_F2 |
| `saolei_click` | `[MouseMovePart{x,y,WINDOW_MESSAGE}, MouseClickPart{LEFT_CLICK,WINDOW_MESSAGE}]` | PostMessage `WM_LBUTTONDOWN/UP` at (x,y) — no cursor move |
| `saolei_flag` | `[MouseMovePart{x,y,WINDOW_MESSAGE}, MouseClickPart{RIGHT_CLICK,WINDOW_MESSAGE}]` | PostMessage `WM_RBUTTONDOWN/UP` at (x,y) |
| `saolei_double_click` | `[MouseMovePart{x,y,WINDOW_MESSAGE}, MouseClickPart{LEFT_RIGHT_PRESS,WINDOW_MESSAGE}]` | PostMessage L+R button down/up at (x,y) (chord) |

**Result**: the existing generic `ToolResultPart { tool_id, status, message, screenshot }` is reused unchanged — the bridge correlates by `tool_id` regardless of part types in the block.

## 6. Profile Data Extensions (修改)

`ProfileData` (`session-agent.ts`) gains the skill/mcp names + resolved skill contents, alongside the existing `toolNames`:

```
ProfileData {
  model:        string           // existing
  systemPrompt: string           // existing (base)
  toolNames:    string[]         // existing
  skillNames:   string[]         // 新增 (e.g. ["saolei"])
  mcpNames:     string[]         // 新增 (e.g. ["saolei"])
  skillContents: string[]        // 新增 (fetched Skill.body for each skillNames entry)
}
```

`AdapterFactory` signature extends to accept the new fields:

```
AdapterFactory(
  getProvider,
  systemPrompt,
  toolNames,
  skillNames,        // 新增
  mcpNames,          // 新增
  skillContents,     // 新增
  bridge,
  checkpointer,
) => AgentAdapter
```

**Skill assembly** (plan D-2): in `AgentAdapterImpl`, the effective system prompt is:

```
effectiveSystemPrompt = systemPrompt
  + SEPARATOR
  + skillContents.join(SEPARATOR)
```

where `SEPARATOR` is a stable delimiter (e.g. `"\n\n---\n\n"`). This is the single place prompt composition happens (existing design verdict in plan.md Change Classification).

**Tool resolution** (plan D-1, existing `buildTools` refactor): `buildTools` becomes a registry that resolves both `toolNames` (existing tools) and `mcpNames` (mcp-bundled tool sets) into a flat `StructuredToolInterface[]`. For `"saolei"` mcp, it calls the saolei tool factory, binding the session-scoped `SaoleiMcp` instance + `OperationBridge`.

## 7. Tool Result / Rejection Shape

Per clarification Q2 + FR-024a, saolei tools **return success** with a structured result; thrown errors are reserved for infrastructure failure.

```
SaoleiToolResult {
  status: "ok" | "rejected"          // "rejected" = game-rule rejection (not an error)
  reason?: string                    // machine-readable reason code/message when rejected
  board?: { width, height, lifecycle } // summary echoed so the agent can verify state
}
```

- `status: "rejected"` is returned for: pre-init operation, awaiting-update block, illegal cell (click non-`block`, chord non-number), out-of-bounds, terminal-after-boom, illegal `saolei_update` transition (FR-016..023).
- A thrown error occurs only for infrastructure failure (e.g. the `OperationBridge` reports desktop-unreachable on the window-message dispatch) — never for a game-rule rejection.
- `saolei_update` returns `status: "rejected"` with the list of illegal transitions when a batch fails **atomically** (FR-022, SC-007 — no partial application).

## 8. Validation Rules (foundational set — FR-024 scopes exhaustive rules as iterative)

| Rule | FR | Effect |
|---|---|---|
| Operation before `saolei_init` | FR-016 | reject (`status: rejected`, reason `not-initialized`) |
| Operation while `awaiting-update` | FR-017 | reject (reason `awaiting-update`) |
| `saolei_click` on non-`block` cell | FR-018 | reject (reason `cell-not-block`) |
| `saolei_double_click` on non-number cell | FR-019 | reject (reason `cell-not-number`) |
| Only `saolei_flag` produces/clears `flag` | FR-020 | enforced on `saolei_update` (reason `illegal-flag-transition`) |
| Coordinate out of bounds | FR-021 | reject (reason `out-of-bounds`) |
| `saolei_update` illegal transition (e.g. number→block, flag a number) | FR-022 | reject batch atomically (reason lists offending cells) |
| Operation after `boom` (terminal) | FR-023 | reject (reason `terminal`) |

The rule set is intentionally non-exhaustive (FR-024); additional rules (e.g. chord flag-count match) are deferred to follow-up work.
