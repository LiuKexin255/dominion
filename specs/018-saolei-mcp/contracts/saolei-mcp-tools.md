# Contract: Saolei MCP Tools

**Feature**: 018-saolei-mcp | **Decision**: D-1 (plain LangChain tools, see [research.md §R-1](../research.md))

The saolei MCP server exposes five tools. Per D-1, these are **ordinary LangChain tools** built with the `tool()` factory + `zod` schemas (identical pattern to `mouse-tool.ts`), bound at creation to the session-scoped `SaoleiMcp` instance (board state) and the `OperationBridge` (window-message dispatch). They are selected when a profile declares `mcp_names: ["saolei"]`.

Tool names follow the existing codebase convention (snake_case, e.g. `mouse_move`). All return a structured `SaoleiToolResult` (see [data-model.md §7](../data-model.md#7-tool-result--rejection-shape)) — rejections are `status: "rejected"`, never thrown errors.

## Common result type

```typescript
// Returned by all five tools (structured success; rejections are NOT thrown).
interface SaoleiToolResult {
  status: "ok" | "rejected";
  reason?: string;                       // machine-readable reason when rejected
  board?: { width: number; height: number; lifecycle: BoardLifecycle };
}
```

## Tool: `saolei_init`

Initialize / reset the board. Sends F2 to the bound window (new game) and resets the MCP board to all `block`.

```typescript
const saolei_init = tool(
  async ({ x, y }) => { /* dispatch PartBlock[KeyPart{F2}]; reset board to x×y block; lifecycle→ready */ },
  {
    name: "saolei_init",
    description:
      "Initialize (or reset) a Minesweeper game. Sends F2 to start a new game and " +
      "defines the board as x columns by y rows of covered cells. Must be called " +
      "before any other saolei operation. Resets all board state.",
    schema: z.object({
      x: z.number().int().positive().describe("Board width in cells (columns)"),
      y: z.number().int().positive().describe("Board height in cells (rows)"),
    }),
  },
);
```

- **Dispatches**: `PartBlock [KeyPart{F2}]` (desktop PostMessages `WM_KEYDOWN/UP`).
- **State effect**: `grid = block[x][y]`; `lifecycle: * → ready` (FR-009).
- **Rejects**: never on game-rule grounds (init is always allowed). May throw only on infrastructure failure (desktop unreachable).

## Tool: `saolei_click`

Reveal a cell (left button) at grid coordinate `(x, y)`.

```typescript
const saolei_click = tool(
  async ({ x, y }) => { /* validate; compute centre px; dispatch PartBlock[Move{x,y,WINDOW_MESSAGE}, Click{LEFT_CLICK,WINDOW_MESSAGE}]; lifecycle→awaiting-update */ },
  {
    name: "saolei_click",
    description:
      "Reveal (left-click) the cell at grid coordinate (x, y). Only valid on a covered " +
      "(block) cell. After calling, you MUST observe the board and call saolei_update " +
      "before any further saolei operation.",
    schema: z.object({
      x: z.number().int().min(0).describe("Cell column (0 = leftmost)"),
      y: z.number().int().min(0).describe("Cell row (0 = topmost)"),
    }),
  },
);
```

- **Dispatches**: `PartBlock [MouseMovePart{x_px,y_px,WINDOW_MESSAGE}, MouseClickPart{LEFT_CLICK,WINDOW_MESSAGE}]` — the move part supplies the cell-centre coordinate (from [data-model.md §4](../data-model.md#4-coordinate-model-constants-pinned-by-user)); the desktop PostMessages the click at that coordinate without moving the OS cursor.
- **Rejects** (`status: rejected`): not-initialized, awaiting-update, cell-not-block, out-of-bounds, terminal.
- **State effect on dispatch**: `lifecycle: ready → awaiting-update`.

## Tool: `saolei_flag`

Toggle a mine flag (right button) at grid coordinate `(x, y)`.

```typescript
const saolei_flag = tool(
  async ({ x, y }) => { /* validate; dispatch PartBlock[Move{x,y,WINDOW_MESSAGE}, Click{RIGHT_CLICK,WINDOW_MESSAGE}]; lifecycle→awaiting-update */ },
  {
    name: "saolei_flag",
    description:
      "Toggle a mine flag (right-click) on the cell at (x, y). Places a flag on a covered " +
      "(block) cell, or clears a flag on a flagged cell. Only right-button actions produce " +
      "or clear flags. You MUST call saolei_update afterwards.",
    schema: z.object({
      x: z.number().int().min(0).describe("Cell column (0 = leftmost)"),
      y: z.number().int().min(0).describe("Cell row (0 = topmost)"),
    }),
  },
);
```

- **Dispatches**: `PartBlock [MouseMovePart{x_px,y_px,WINDOW_MESSAGE}, MouseClickPart{RIGHT_CLICK,WINDOW_MESSAGE}]`.
- **Rejects**: not-initialized, awaiting-update, out-of-bounds, terminal, cell-not-block-and-not-flag (flag targets only `block` or `flag` cells).
- **State effect on dispatch**: `lifecycle: ready → awaiting-update`.

## Tool: `saolei_double_click`

Chord a numbered cell (left+right together) to reveal its non-mine neighbours.

```typescript
const saolei_double_click = tool(
  async ({ x, y }) => { /* validate cell is a number; dispatch PartBlock[Move{x,y,WINDOW_MESSAGE}, Click{LEFT_RIGHT_PRESS,WINDOW_MESSAGE}]; lifecycle→awaiting-update */ },
  {
    name: "saolei_double_click",
    description:
      "Chord (left+right click together) the numbered cell at (x, y). Valid only on a " +
      "revealed number cell. Reveals all non-flagged neighbours. You MUST call " +
      "saolei_update afterwards.",
    schema: z.object({
      x: z.number().int().min(0).describe("Cell column (0 = leftmost)"),
      y: z.number().int().min(0).describe("Cell row (0 = topmost)"),
    }),
  },
);
```

- **Dispatches**: `PartBlock [MouseMovePart{x_px,y_px,WINDOW_MESSAGE}, MouseClickPart{LEFT_RIGHT_PRESS,WINDOW_MESSAGE}]` (chord).
- **Rejects**: not-initialized, awaiting-update, cell-not-number, out-of-bounds, terminal.
- **State effect on dispatch**: `lifecycle: ready → awaiting-update`.
- **Note**: flag-count-match precondition is intentionally NOT validated this version (FR-024 iterative; the game is ground truth).

## Tool: `saolei_update`

Report observed cell states after an operation (batch, applied atomically).

```typescript
const SAOLEI_CELL_STATES = [
  "block","zero","one","two","three","four","five","six","seven","eight","flag","boom",
] as const;

const saolei_update = tool(
  async ({ cells }) => { /* validate each transition; apply atomically; lifecycle awaiting-update→ready (or terminal on boom) */ },
  {
    name: "saolei_update",
    description:
      "Report the observed board state after an operation, as a batch of (x, y, state) " +
      "cell updates. Applied atomically: if ANY transition is illegal the whole batch is " +
      "rejected and no state changes. Reporting a 'boom' cell makes the board terminal " +
      "until the next saolei_init.",
    schema: z.object({
      cells: z.array(z.object({
        x: z.number().int().min(0).describe("Cell column"),
        y: z.number().int().min(0).describe("Cell row"),
        state: z.enum(SAOLEI_CELL_STATES).describe("Observed cell state"),
      })).min(1).describe("Changed cells to apply"),
    }),
  },
);
```

- **Dispatches**: nothing (pure state update — no window input).
- **Rejects** (`status: rejected`, batch left unapplied): not-initialized, any illegal transition (e.g. number→block, flag a revealed number), coordinate out of board bounds. `reason` lists the offending cells (FR-022, SC-007).
- **State effect on success**: `lifecycle: awaiting-update → ready`; if any cell becomes `boom`, `lifecycle → terminal`.

## Selection rule

These five tools are added to the agent's tool set **iff** the active profile's `mcp_names` contains `"saolei"` (resolved by the refactored `buildTools` registry, [plan.md](../plan.md#changes)). Unknown `mcp_names` are ignored with a warning (FR-035), consistent with existing unknown-`tool_names` handling.
