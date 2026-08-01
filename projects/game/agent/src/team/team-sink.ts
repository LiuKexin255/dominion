/**
 * team/team-sink.ts — the saolei template's `SaoleiEventSink` consumer +
 * per-session ephemeral game-state buffer (D7).
 *
 * The saolei MCP server (Phase 4) emits structured game events via
 * `SaoleiEventSink`; this module consumes them into a per-session, in-process
 * **ephemeral buffer** — a plain object, NOT LangGraph store / mongo /
 * checkpointer (D7). The buffer drives the planner trigger and supplies the
 * planner's review input (D6 steps 3-6):
 *
 * - `onMove` / `onGameStart` update `gameState`;
 * - `onGameEnd` writes `gameEvent = {state, status, endedAt, consumed:false}`
 *   (+ updates `gameState`);
 * - the player node post-process consumes the event once (player.ts) and
 *   writes `TeamState.gameEnded = status`;
 * - the planner reads `gameState` for the review (planner.ts).
 *
 * Contract: `specs/031-team-template-mode/contracts/saolei-sink-contract.md`
 * §4 (consumer) + `contracts/team-graph-contract.md` §3/§4. The buffer is NOT
 * cleared by `RefreshTeam` (D7 — it is overwritten by the next move; it is
 * not "memory").
 */

import type { GameState } from "@dominion/game-saolei-board";

import type { CellTool, SaoleiEventSink } from "../mcp/saolei/saolei-mcp";

/** A structured game-end event written by the sink (D6 step 3). */
export interface GameEventRecord {
	state: GameState;
	status: "won" | "lost";
	endedAt: number;
	/** Consumed by the player node post-process (read once, D6 step 4). */
	consumed: boolean;
}

/**
 * The per-session ephemeral game-state buffer (D7): the latest recognized
 * `gameState` and the latest (unconsumed) `gameEvent`. A plain in-process
 * object, one instance per session (created by `SessionTeam`, Batch 2).
 */
export interface EphemeralGameBuffer {
	gameState: GameState | null;
	gameEvent: GameEventRecord | null;
}

/** Create an empty per-session ephemeral buffer. */
export function createEphemeralGameBuffer(): EphemeralGameBuffer {
	return { gameState: null, gameEvent: null };
}

/**
 * Build the team-side `SaoleiEventSink` consumer bound to a buffer
 * (contract saolei-sink-contract.md §4).
 *
 * - `onGameStart`: a new game began (`saolei_init`) — the recognized initial
 *   state becomes the current `gameState`. It does NOT clear `gameEvent`: the
 *   buffer records the LATEST end event (D6 遗留假设 — "buffer 仅记最新结束
 *   事件"), so an unconsumed event from a prior game still triggers the
 *   planner once when the player run returns, and `onGameEnd` overwrites it
 *   with the newest event.
 * - `onMove`: the recognized state after a legal cell operation.
 * - `onGameEnd`: write the structured end event (status is the MCP's
 *   first-hand `won|lost` computation, FR-017) + update `gameState`.
 */
export function createTeamSink(buffer: EphemeralGameBuffer): SaoleiEventSink {
	return {
		onGameStart: (state: GameState) => {
			buffer.gameState = state;
		},
		onMove: (
			_tool: CellTool,
			_x: number,
			_y: number,
			state: GameState,
		) => {
			buffer.gameState = state;
		},
		onGameEnd: (state: GameState, status: "won" | "lost") => {
			buffer.gameEvent = {
				state,
				status,
				endedAt: Date.now(),
				consumed: false,
			};
			buffer.gameState = state;
		},
	};
}

/**
 * Read the latest game-end event AND mark it consumed (D6 step 4 — the
 * player node post-process runs this exactly once after `createAgent`
 * returns). Returns `null` when no unconsumed event exists, so a second call
 * is a no-op (planner fires at most once per game end).
 */
export function consumeGameEvent(
	buffer: EphemeralGameBuffer,
): GameEventRecord | null {
	const event = buffer.gameEvent;
	if (!event || event.consumed) return null;
	event.consumed = true;
	return event;
}

/**
 * Peek the latest recognized game state without consuming (the planner node
 * reads this as its review input — D6 step 6). `null` when no state has been
 * recognized yet in this session.
 */
export function peekGameState(buffer: EphemeralGameBuffer): GameState | null {
	return buffer.gameState;
}
