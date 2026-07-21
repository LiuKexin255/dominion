# Contract: Saolei MCP Tools

**Feature**: 018-saolei-mcp
**Date**: 2026-07-20
**Status**: Phase 1 contract — MUST be satisfied by implementation. Settled BEFORE any tool code (Constitution Principle III).

This contract pins the five saolei MCP tools exposed by the session-bound `McpServer` (research.md D3) and consumed by the agent's loopback `MultiServerMCPClient` (research.md D2). It is the MCP server↔client interface.

## Authority

- Spec: `spec.md` FR-005..FR-019, Clarifications (Round 1 + Round 2).
- Data model: `data-model.md` (game state, status enum, alternation, validation rules §8).
- Research: `research.md` D2 (adapter), D8 (result surfacing).
- MCP tool model: `@modelcontextprotocol/sdk` `McpServer.registerTool(name, { description, inputSchema }, handler)`; tool results are `{ content: ContentBlock[], isError?: boolean }`.

## Common conventions

- **Coordinates**: `(x, y)` grid coordinates, top-left origin `(0, 0)`, `x` = column, `y` = row. The MCP server translates to window-client pixels via the fixed formula (`data-model.md` §5) before dispatching.
- **Result surfacing** (research.md D8): tools return **normal MCP results** (`isError: false`) for BOTH accepted and validation-rejected outcomes, with a text content block describing the outcome. The `@langchain/mcp-adapters` TS adapter raises `ToolException` on `isError: true` and does NOT return the error to the model — so `isError` is reserved for unexpected/internal failures only, never for rule rejections.
- **Alternation** (FR-011): only `saolei_init` is exempt; the three cell operations set `pendingUpdate=true` on dispatch and require an accepted `saolei_update` before the next operation. Validation rejections do not set the flag (Clarification Q3 → A).
- **Dispatch**: accepted cell operations go through `OperationBridge.dispatch(part)` and await the desktop `ToolResultPart` (5 s timeout, existing behavior, `projects/game/agent/src/operation-bridge.ts:132-201`).
- **Schema notation**: JSON Schema (the MCP `inputSchema` format). `status` is a string union for wire simplicity.

## Tool: `saolei_init`

Initialize / restart the minesweeper game for this session.

- **inputSchema**:
  ```json
  { "type": "object",
    "properties": {
      "width":  { "type": "integer", "minimum": 1, "description": "column count (x in 0..width-1)" },
      "height": { "type": "integer", "minimum": 1, "description": "row count (y in 0..height-1)" }
    },
    "required": ["width", "height"] }
  ```
  (Cell counts, NOT pixels — board pixel geometry is fixed per `data-model.md` §5.)
- **Behavior**:
  1. Dispatch a `KeyboardPressPart{ key: KEYBOARD_KEY_F2 }` to the desktop (new game) via `OperationBridge`.
  2. Reset the per-session `GameState`: size `grid` to `height` rows × `width` cols, all `INITIAL`; store `width`/`height`; set `initialized=true`, `pendingUpdate=false`, `lastOp=null`.
- **Exempt from alternation**: no `saolei_update` required after it (FR-006). Re-calling `saolei_init` re-dispatches F2 and resets state to the new dimensions (FR-027).
- **Result**: `{ content: [{ type: "text", text: "game initialized (F2 dispatched); grid <width>x<height>, all cells INITIAL" }] }` plus, if the desktop returned a screenshot, an image content block.

## Tool: `saolei_click`

Left-click (reveal) an unopened, unflagged cell.

- **inputSchema**:
  ```json
  { "type": "object",
    "properties": { "x": { "type": "integer", "minimum": 0 }, "y": { "type": "integer", "minimum": 0 } },
    "required": ["x", "y"] }
  ```
- **Pre-dispatch validation (FR-013)**: require `grid[y][x] == INITIAL`. Reject (no dispatch, no `pendingUpdate` set) if the cell is not `INITIAL`.
- **Behavior on accept**: dispatch `MouseMoveAndClickPart{ x: centerX(x), y: centerY(y), click: LEFT_CLICK, method: WINDOW_MESSAGE }`; await desktop result; set `pendingUpdate=true`, `lastOp={kind:"click", target:{x,y}}`.
- **Result (accept)**: text status + optional screenshot image block; instructs the model to call `saolei_update` next.
- **Result (reject)**: `{ content: [{ type: "text", text: "rejected: cell (x,y) is not INITIAL (current=<status>)" }] }`. Model may retry immediately.

## Tool: `saolei_flag`

Right-click to toggle a flag on an unopened cell.

- **inputSchema**: same `{x, y}` shape as `saolei_click`.
- **Pre-dispatch validation (FR-014)**: require `grid[y][x] == INITIAL`. Reject otherwise.
- **Behavior on accept**: dispatch `MouseMoveAndClickPart{ ..., click: RIGHT_CLICK, method: WINDOW_MESSAGE }`; set `pendingUpdate=true`, `lastOp={kind:"flag", target:{x,y}}`.
- **Result**: same shape as `saolei_click` (accept/reject).

## Tool: `saolei_chord_click`

Chord — a **single simultaneous left+right button press** on a satisfied number cell (one atomic operation, NOT two separate clicks and NOT a left double-click). Reveals all unflagged neighbors of the target.

- **inputSchema**: same `{x, y}` shape.
- **Pre-dispatch validation (FR-015)**: require `grid[y][x]` is a number `1..8` AND the count of adjacent `FLAG` cells equals that number. Reject otherwise.
- **Behavior on accept**: dispatch `MouseMoveAndClickPart{ x: centerX(x), y: centerY(y), click: LEFT_RIGHT_PRESS, method: WINDOW_MESSAGE }`; set `pendingUpdate=true`, `lastOp={kind:"chord_click", target:{x,y}}`.
- **Result**: same shape as `saolei_click` (accept/reject).

## Tool: `saolei_update`

Batch-update cell statuses observed after the most recent operation.

- **inputSchema**:
  ```json
  { "type": "object",
    "properties": {
      "cells": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "x": { "type": "integer", "minimum": 0 },
            "y": { "type": "integer", "minimum": 0 },
            "status": { "type": "string",
              "enum": ["INITIAL","0","1","2","3","4","5","6","7","8","FLAG","HIT_MINE","MINE"] }
          },
          "required": ["x", "y", "status"] }
      } },
    "required": ["cells"] }
  ```
- **Pre-conditions**: `pendingUpdate == true` AND `lastOp != null`. If `pendingUpdate == false` (no operation awaiting), reject: "no operation awaiting update".
- **Validation (FR-013/014/015/016)**: see `data-model.md` §8 for the full formalization. Key points:
  - All coordinates in range `[0,width)×[0,height)`.
  - The batch must be consistent with `lastOp.kind` (click connectivity incl. target; flag single-cell INITIAL↔FLAG; chord adjacency/flag-preservation/connectivity rules).
  - On reject: state unchanged, `pendingUpdate` stays `true`.
- **Behavior on accept**: apply all cell transitions to `grid`; set `pendingUpdate=false`, `lastOp=null`.
- **Result (accept)**: `{ content: [{ type: "text", text: "state updated; <n> cells changed; ready for next operation" }] }`.
- **Result (reject)**: `{ content: [{ type: "text", text: "rejected: <violated rule detail>" }] }`. State unchanged; model may send a corrected `saolei_update`.

## Alternation summary (model-facing contract)

```text
saolei_init                                   (no update required; may repeat)
[ saolei_click | saolei_flag | saolei_chord_click ]   -- dispatches, then:
saolei_update                                          -- validates + applies; then:
[ next operation ]                                     -- alternates
```

- A second operation before an accepted `saolei_update` is rejected ("must update first").
- A validation-rejected operation does NOT enter the pending state — the model may retry immediately.
- A validation-rejected `saolei_update` leaves the session pending — the model must send a corrected `saolei_update` (it cannot start a new operation).

## Skill coupling

When the saolei MCP is selected on a profile, the built-in `src/skill/saolei/SKILL.md` is auto-injected into the system prompt (FR-023/024). The skill MUST document: the five tools, the `(x,y)` coordinate convention, the operation→update alternation, the status enum, and the validation expectations — so the model uses the tools correctly without rediscovering the contract.

## Out of scope for this contract

- Proto Part field numbers → `contracts/proto-operation-contract.md`.
- Fixed board geometry → `data-model.md` §5.
- MCP server transport/routing → `research.md` D3.
