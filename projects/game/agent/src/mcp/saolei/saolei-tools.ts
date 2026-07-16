/**
 * @fileoverview The five saolei LangChain tool factories (plan D-1, T013).
 *
 * Each tool is an ordinary LangChain tool built with `tool()` + `zod` (identical
 * pattern to `mouse-tool.ts`), bound at creation to the session-scoped
 * `SaoleiMcp` instance (board state) and the `OperationBridge` (window-message
 * dispatch). They are selected when a profile declares `mcp_names: ["saolei"]`.
 *
 * Rejection semantics (D-5, FR-024a): a game-rule rejection returns a
 * `SaoleiToolResult { status: "rejected", reason }` WITHOUT dispatching any
 * window input; thrown errors are reserved for infrastructure failure (a
 * dispatch that resolves FAILED — desktop unreachable / no window bound).
 *
 * Tool → PartBlock mapping (data-model.md §5d):
 *   saolei_init        → [KeyPart{F2}]
 *   saolei_click       → [MouseMovePart{centre,WINDOW_MESSAGE}, MouseClickPart{LEFT_CLICK,WINDOW_MESSAGE}]
 *   saolei_flag        → [MouseMovePart{centre,WINDOW_MESSAGE}, MouseClickPart{RIGHT_CLICK,WINDOW_MESSAGE}]
 *   saolei_double_click→ [MouseMovePart{centre,WINDOW_MESSAGE}, MouseClickPart{LEFT_RIGHT_PRESS,WINDOW_MESSAGE}]
 *   saolei_update      → (no dispatch; pure state update)
 *
 * See contracts/saolei-mcp-tools.md for the canonical schemas and descriptions.
 */

import type { StructuredToolInterface } from "@langchain/core/tools";
import { tool } from "langchain";
import { z } from "zod";

import type { OperationBridge, OperationResult } from "../../operation-bridge";
import type { SaoleiMcp } from "./saolei-mcp";
import { CELL_STATES } from "./board";
import type { BoardLifecycle, BoardState, CellUpdate } from "./board";
import { cellCentre } from "./geometry";
import {
  validateChordTarget,
  validateClickTarget,
  validateFlagTarget,
  validatePositionalOperation,
  validateUpdateBatch,
} from "./validation";

/** Proto enum string literals the saolei tools dispatch. */
const WINDOW_MESSAGE = "INPUT_DELIVERY_WINDOW_MESSAGE";
const LEFT_CLICK = "MOUSE_CLICK_ACTION_LEFT_CLICK";
const RIGHT_CLICK = "MOUSE_CLICK_ACTION_RIGHT_CLICK";
const LEFT_RIGHT_PRESS = "MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS";
const KEY_F2 = "KEY_ACTION_F2";

/** Bridge SUCCEEDED status string. */
const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";

/**
 * Structured result returned by all five tools (data-model.md §7). Rejections
 * carry `status: "rejected"` + `reason`; success carries `status: "ok"` and a
 * board summary so the agent can verify state.
 */
export interface SaoleiToolResult {
  status: "ok" | "rejected";
  reason?: string;
  board?: { width: number; height: number; lifecycle: BoardLifecycle };
}

/**
 * A content block returned to LangChain. Mirrors the mouse tool shape so a
 * post-operation screenshot renders as a real image to the model (it must read
 * the board from the screenshot before calling `saolei_update`).
 */
type SaoleiContentBlock =
  | { type: "text"; text: string }
  | { type: "image_url"; image_url: { url: string } };

/**
 * Create the five saolei LangChain tools bound to a session's MCP instance and
 * OperationBridge. Selected when a profile declares `mcp_names: ["saolei"]`.
 */
export function createSaoleiTools(
  mcp: SaoleiMcp,
  bridge: OperationBridge,
): StructuredToolInterface[] {
  const board = mcp.getBoard();

  return [
    createSaoleiInit(board, bridge),
    createSaoleiClick(board, bridge),
    createSaoleiFlag(board, bridge),
    createSaoleiDoubleClick(board, bridge),
    createSaoleiUpdate(board),
  ];
}

// ─── shared helpers ───────────────────────────────────────────────────────

/** Build a rejection result (no window input was dispatched). */
function reject(reason: string, board: BoardState): SaoleiToolResult {
  return { status: "rejected", reason, board: board.toSummary() };
}

/** Build an ok result echoing the board summary. */
function ok(board: BoardState): SaoleiToolResult {
  return { status: "ok", board: board.toSummary() };
}

/**
 * Render a SaoleiToolResult as LangChain content blocks. Always emits the
 * structured result as a JSON text block (the model's primary signal); when a
 * screenshot accompanied the dispatch, appends the image and a pixel-dimension
 * annotation so the model reads the board at the correct resolution.
 */
function buildSaoleiBlocks(
  result: SaoleiToolResult,
  dispatch?: OperationResult,
): SaoleiContentBlock[] {
  const blocks: SaoleiContentBlock[] = [
    { type: "text", text: JSON.stringify(result) },
  ];
  if (dispatch?.screenshot) {
    blocks.push({
      type: "image_url",
      image_url: { url: `data:image/png;base64,${dispatch.screenshot.data}` },
    });
    blocks.push({
      type: "text",
      text: `[图片像素尺寸：${dispatch.screenshot.widthPx}×${dispatch.screenshot.heightPx}（宽×高，单位：像素）。扫雷坐标基于此像素空间。]`,
    });
  }
  return blocks;
}

/**
 * Dispatch a PartBlock and treat a FAILED resolution as infrastructure failure
 * (D-5): game-rule rejections are returned by the caller BEFORE calling this,
 * so a FAILED dispatch here means the desktop was unreachable / no window
 * bound — thrown, not structured.
 */
async function dispatchOrThrow(
  bridge: OperationBridge,
  parts: Parameters<OperationBridge["dispatch"]>[0],
  signal?: AbortSignal,
): Promise<OperationResult> {
  const result = await bridge.dispatch(parts, signal);
  if (result.status !== STATUS_SUCCEEDED) {
    throw new Error(`saolei dispatch failed: ${result.message}`);
  }
  return result;
}

// ─── saolei_init ──────────────────────────────────────────────────────────

function createSaoleiInit(
  board: BoardState,
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x, y }, config): Promise<SaoleiContentBlock[]> => {
      const signal = (config as { signal?: AbortSignal } | undefined)?.signal;
      // init is never game-rule-rejected (FR-009): it dispatches F2 then
      // resets the board. A FAILED dispatch is infrastructure failure.
      const dispatch = await dispatchOrThrow(
        bridge,
        [{ keyPress: { key: KEY_F2 } }],
        signal,
      );
      board.init(x, y);
      return buildSaoleiBlocks(ok(board), dispatch);
    },
    {
      name: "saolei_init",
      description:
        "Initialize (or reset) a Minesweeper game. Sends F2 to start a new game " +
        "and defines the board as x columns by y rows of covered cells. Must be " +
        "called before any other saolei operation. Resets all board state.",
      schema: z.object({
        x: z.number().int().positive().describe("Board width in cells (columns)"),
        y: z.number().int().positive().describe("Board height in cells (rows)"),
      }),
    },
  );
}

// ─── saolei_click ─────────────────────────────────────────────────────────

function createSaoleiClick(
  board: BoardState,
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x, y }, config): Promise<SaoleiContentBlock[]> => {
      const signal = (config as { signal?: AbortSignal } | undefined)?.signal;
      const rejection =
        validatePositionalOperation(board, x, y) ??
        validateClickTarget(board, x, y);
      if (rejection) {
        return buildSaoleiBlocks(reject(rejection.reason, board));
      }

      const centre = cellCentre(x, y);
      const dispatch = await dispatchOrThrow(
        bridge,
        [
          { mouseMove: { xPx: centre.x, yPx: centre.y, delivery: WINDOW_MESSAGE } },
          { mouseClick: { click: LEFT_CLICK, delivery: WINDOW_MESSAGE } },
        ],
        signal,
      );
      board.enterAwaitingUpdate();
      return buildSaoleiBlocks(ok(board), dispatch);
    },
    {
      name: "saolei_click",
      description:
        "Reveal (left-click) the cell at grid coordinate (x, y). Only valid on a " +
        "covered (block) cell. After calling, you MUST observe the board and call " +
        "saolei_update before any further saolei operation.",
      schema: positionalSchema(),
    },
  );
}

// ─── saolei_flag ──────────────────────────────────────────────────────────

function createSaoleiFlag(
  board: BoardState,
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x, y }, config): Promise<SaoleiContentBlock[]> => {
      const signal = (config as { signal?: AbortSignal } | undefined)?.signal;
      const rejection =
        validatePositionalOperation(board, x, y) ??
        validateFlagTarget(board, x, y);
      if (rejection) {
        return buildSaoleiBlocks(reject(rejection.reason, board));
      }

      const centre = cellCentre(x, y);
      const dispatch = await dispatchOrThrow(
        bridge,
        [
          { mouseMove: { xPx: centre.x, yPx: centre.y, delivery: WINDOW_MESSAGE } },
          { mouseClick: { click: RIGHT_CLICK, delivery: WINDOW_MESSAGE } },
        ],
        signal,
      );
      board.enterAwaitingUpdate();
      return buildSaoleiBlocks(ok(board), dispatch);
    },
    {
      name: "saolei_flag",
      description:
        "Toggle a mine flag (right-click) on the cell at (x, y). Places a flag on a " +
        "covered (block) cell, or clears a flag on a flagged cell. Only right-button " +
        "actions produce or clear flags. You MUST call saolei_update afterwards.",
      schema: positionalSchema(),
    },
  );
}

// ─── saolei_double_click ──────────────────────────────────────────────────

function createSaoleiDoubleClick(
  board: BoardState,
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x, y }, config): Promise<SaoleiContentBlock[]> => {
      const signal = (config as { signal?: AbortSignal } | undefined)?.signal;
      const rejection =
        validatePositionalOperation(board, x, y) ??
        validateChordTarget(board, x, y);
      if (rejection) {
        return buildSaoleiBlocks(reject(rejection.reason, board));
      }

      const centre = cellCentre(x, y);
      const dispatch = await dispatchOrThrow(
        bridge,
        [
          { mouseMove: { xPx: centre.x, yPx: centre.y, delivery: WINDOW_MESSAGE } },
          { mouseClick: { click: LEFT_RIGHT_PRESS, delivery: WINDOW_MESSAGE } },
        ],
        signal,
      );
      board.enterAwaitingUpdate();
      return buildSaoleiBlocks(ok(board), dispatch);
    },
    {
      name: "saolei_double_click",
      description:
        "Chord (left+right click together) the numbered cell at (x, y). Valid only " +
        "on a revealed number cell. Reveals all non-flagged neighbours. You MUST " +
        "call saolei_update afterwards.",
      schema: positionalSchema(),
    },
  );
}

// ─── saolei_update ────────────────────────────────────────────────────────

function createSaoleiUpdate(
  board: BoardState,
): StructuredToolInterface {
  return tool(
    async ({ cells }): Promise<SaoleiContentBlock[]> => {
      const updates = cells as unknown as CellUpdate[];
      const rejection = validateUpdateBatch(board, updates);
      if (rejection) {
        return buildSaoleiBlocks(reject(rejection.reason, board));
      }
      board.commitUpdate(updates);
      return buildSaoleiBlocks(ok(board));
    },
    {
      name: "saolei_update",
      description:
        "Report the observed board state after an operation, as a batch of " +
        "(x, y, state) cell updates. Applied atomically: if ANY transition is " +
        "illegal the whole batch is rejected and no state changes. Reporting a " +
        "'boom' cell makes the board terminal until the next saolei_init.",
      schema: z.object({
        cells: z
          .array(
            z.object({
              x: z.number().int().min(0).describe("Cell column"),
              y: z.number().int().min(0).describe("Cell row"),
              state: z
                .enum(CELL_STATES as unknown as [string, ...string[]])
                .describe("Observed cell state"),
            }),
          )
          .min(1)
          .describe("Changed cells to apply"),
      }),
    },
  );
}

/** Shared `(x, y)` grid-coordinate schema for the positional tools. */
function positionalSchema(): z.ZodObject<{
  x: z.ZodNumber;
  y: z.ZodNumber;
}> {
  return z.object({
    x: z.number().int().min(0).describe("Cell column (0 = leftmost)"),
    y: z.number().int().min(0).describe("Cell row (0 = topmost)"),
  });
}
