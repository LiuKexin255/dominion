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
 * - `onOperate` / `onGameStart` update `gameState`;
 * - `onGameEnd` writes `gameEvent = {state, status, endedAt, consumed:false}`
 *   (+ updates `gameState`); the per-game statistics carried by `onGameEnd`
 *   (US5 — `specs/037-saolei-team-optimize/spec.md` FR-031) are stored into
 *   `gameEvent.stats` alongside the event;
 * - the player node post-process consumes the event once (player.ts) and
 *   writes `TeamState.gameEnded = status`;
 * - the planner reads `gameState` for the review (planner.ts);
 * - the callbacks also accumulate `gameLog` — the full operation sequence of
 *   the CURRENT game (reset on `onGameStart`), which the planner renders as
 *   its review input (`specs/036-team-mode-bugfix/data-model.md` §2). One
 *   `saolei_operate` call — single or batch — is recorded as ONE entry
 *   carrying its full operations list (FR-004;
 *   `specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`
 *   §4), never one entry per operation.
 *
 * Contract: `specs/031-team-template-mode/contracts/saolei-sink-contract.md`
 * §4 (consumer) + `contracts/team-graph-contract.md` §3/§4. The buffer is NOT
 * cleared by `RefreshTeam` (D7 — it is overwritten by the next move; it is
 * not "memory").
 */

import { isWin } from "@dominion/game-saolei-board";
import type { GameState } from "@dominion/game-saolei-board";

import { isTerminalState } from "../mcp/saolei/saolei-mcp.js";
import type {
	CellOperation,
	GameStats,
	SaoleiEventSink,
} from "../mcp/saolei/saolei-mcp.js";

/** A structured game-end event written by the sink (D6 step 3). */
export interface GameEventRecord {
	state: GameState;
	status: "won" | "lost";
	endedAt: number;
	/** Consumed by the player node post-process (read once, D6 step 4). */
	consumed: boolean;
	/**
	 * Per-game statistics carried by `onGameEnd`
	 * (`specs/037-saolei-team-optimize/spec.md` FR-031; contracts/
	 * game-stats-contract.md §4), read by the planner's review input
	 * (FR-032). Optional — an unupgraded sender omits it (backward
	 * compatible).
	 */
	stats?: GameStats;
}

/**
 * A single game-log entry: one step of the game — the tool that triggered it
 * (`"saolei_init"` | `"saolei_operate"` | `"(game-end)"`), the operation list
 * for a `saolei_operate` step (a batch is ONE entry carrying its full
 * operations, FR-004 — `specs/039-planner-memory-calibration/contracts/
 * saolei-operate-contract.md` §4), the board state after the step, and the
 * resulting game status. The sink accumulates one entry per step into
 * `EphemeralGameBuffer.gameLog` (reset on `onGameStart`), and the planner
 * renders the full sequence as its review input
 * (`specs/036-team-mode-bugfix/data-model.md` §2).
 */
export interface GameLogEntry {
	/** Tool that triggered this step ("saolei_init", "saolei_operate", ...).
	 *  Game-end events use the literal "(game-end)". */
	tool: string;
	/**
	 * The full operation list of one `saolei_operate` call (single or batch).
	 * Absent for `saolei_init` / `(game-end)` steps.
	 */
	operations?: CellOperation[];
	/** Board state after the operation (text-rendered for the planner). */
	state: GameState;
	/** Game status after the operation. */
	status: "won" | "lost" | "playing";
}

/**
 * The per-session ephemeral game-state buffer (D7): the latest recognized
 * `gameState`, the latest (unconsumed) `gameEvent`, and the `gameLog` of the
 * current game. A plain in-process object, one instance per session (created
 * by `SessionTeam`, Batch 2).
 */
export interface EphemeralGameBuffer {
	gameState: GameState | null;
	gameEvent: GameEventRecord | null;
	/** Full operation sequence of the current game (reset on `onGameStart`,
	 *  `specs/036-team-mode-bugfix/data-model.md` §1). */
	gameLog: GameLogEntry[];
}

/** Create an empty per-session ephemeral buffer. */
export function createEphemeralGameBuffer(): EphemeralGameBuffer {
	return { gameState: null, gameEvent: null, gameLog: [] };
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
 *   with the newest event. It DOES reset `gameLog` — the planner reviews only
 *   the CURRENT game, never an accumulation across games
 *   (`specs/036-team-mode-bugfix/data-model.md` §2, `specs/036-team-mode-bugfix/spec.md` FR-007).
 * - `onOperate`: one callback per `saolei_operate` call (single or batch) —
 *   the recognized state after processing + the call's FULL operations list
 *   (FR-004; `specs/039-planner-memory-calibration/contracts/
 *   saolei-operate-contract.md` §4). `stats` is ignored here — the end-event
 *   path (`onGameEnd`) stores the per-game statistics.
 * - `onGameEnd`: write the structured end event (status is the MCP's
 *   first-hand `won|lost` computation, FR-017) + update `gameState`; the
 *   optional `stats` (US5, FR-031) is stored into `gameEvent.stats` for the
 *   planner's review input (FR-032).
 */
export function createTeamSink(buffer: EphemeralGameBuffer): SaoleiEventSink {
	return {
		onGameStart: (state: GameState) => {
			buffer.gameState = state;
			buffer.gameLog = [];
			buffer.gameLog.push({ tool: "saolei_init", state, status: "playing" });
		},
		onOperate: (operations: CellOperation[], finalState: GameState) => {
			buffer.gameState = finalState;
			// FR-004: one gameLog entry per saolei_operate call, carrying the
			// full operations list (never one entry per operation). Same
			// loss-first decision order as the MCP's private `gameStatus`
			// (`specs/036-team-mode-bugfix/data-model.md` §3).
			buffer.gameLog.push({
				tool: "saolei_operate",
				operations,
				state: finalState,
				status: isTerminalState(finalState)
					? "lost"
					: isWin(finalState)
						? "won"
						: "playing",
			});
		},
		onGameEnd: (
			state: GameState,
			status: "won" | "lost",
			stats?: GameStats,
		) => {
			buffer.gameEvent = {
				state,
				status,
				endedAt: Date.now(),
				consumed: false,
				// US5 (specs/037-saolei-team-optimize/spec.md FR-031): the
				// MCP-computed statistics ride along with the end event.
				stats,
			};
			buffer.gameState = state;
			buffer.gameLog.push({ tool: "(game-end)", state, status });
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
