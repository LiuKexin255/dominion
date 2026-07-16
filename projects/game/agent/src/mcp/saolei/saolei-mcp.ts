/**
 * @fileoverview SaoleiMcp — the per-session saolei MCP instance. Owns the
 * BoardState (FR-025b: state is confined to one session; FR-008: never
 * persisted) and exposes the board for the saolei tool factories to validate
 * against and transition. Created lazily when a session's profile declares the
 * `saolei` mcp; dropped on adapter rebuild so a new instance starts at
 * `uninitialized` with no carry-over board (FR-025a/b/c, data-model.md §3).
 *
 * This is the in-process "MCP server" facade (plan D-1): the agent service is
 * the server, and the saolei tools are ordinary LangChain tools bound to this
 * instance + the OperationBridge. No wire protocol, no new dependency.
 */

import { BoardState } from "./board";

/**
 * SaoleiMcp holds the board state machine for one session. The instance is
 * owned by SessionAgent (mirroring OperationBridge); each session has at most
 * one, created lazily at adapter bind time. Disposal is implicit — when the
 * adapter is invalidated/rebuilt, the old instance is dropped and a fresh one
 * begins at `uninitialized` (FR-025c).
 */
export class SaoleiMcp {
  private readonly board = new BoardState();

  /** The board state machine (cell grid + lifecycle). */
  getBoard(): BoardState {
    return this.board;
  }
}
