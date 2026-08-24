/**
 * team-sink.test.ts — team-side SaoleiEventSink consumer + ephemeral buffer
 * semantics (`specs/031-team-template-mode/contracts/saolei-sink-contract.md`
 * §4 / §7, `contracts/team-graph-contract.md` §4, research.md D7). The sink
 * consumes `onOperate` — ONE callback per `saolei_operate` call (single or
 * batch) carrying the full operations list, and gameLog records one entry per
 * call (FR-004; `specs/039-planner-memory-calibration/contracts/
 * saolei-operate-contract.md` §4).
 */

import { describe, expect, it } from "vitest";
import type { GameState } from "@dominion/game-saolei-board";

import type { CellOperation, GameStats } from "../mcp/saolei/saolei-mcp.js";
import {
	consumeGameEvent,
	createEphemeralGameBuffer,
	createTeamSink,
	peekGameState,
} from "./team-sink.js";

/** A minimal recognizable GameState (3x3, all empty cells). */
function makeState(marker?: "won" | "lost"): GameState {
	const grid = Array.from({ length: 3 }, () =>
		Array.from({ length: 3 }, () => "0" as const),
	);
	return {
		width: 3,
		height: 3,
		grid,
		...(marker ? { mineCounter: { decoded: true, value: 3 } } : {}),
	};
}

describe("createTeamSink", () => {
	it("onGameStart records the initial state as gameState", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const state = makeState();
		await sink.onGameStart(state);
		expect(peekGameState(buffer)).toBe(state);
	});

	it("onOperate updates gameState (and does not touch gameEvent)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const state1 = makeState();
		const state2 = makeState();
		await sink.onOperate([{ type: "click", x: 1, y: 1 }], state1);
		expect(peekGameState(buffer)).toBe(state1);
		expect(buffer.gameEvent).toBeNull();

		await sink.onOperate(
			[
				{ type: "flag", x: 2, y: 2 },
				{ type: "chord", x: 0, y: 0 },
			],
			state2,
		);
		expect(peekGameState(buffer)).toBe(state2);
	});

	it("onGameEnd writes an unconsumed structured end event + updates gameState", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const state = makeState("won");
		await sink.onGameEnd(state, "won");
		expect(buffer.gameEvent).toMatchObject({
			state,
			status: "won",
			consumed: false,
		});
		expect(typeof buffer.gameEvent?.endedAt).toBe("number");
		expect(peekGameState(buffer)).toBe(state);
	});

	it("onGameEnd overwrites the previous end event (latest event wins — D6 遗留假设)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		await sink.onGameEnd(makeState("lost"), "lost");
		await sink.onGameEnd(makeState("won"), "won");
		expect(buffer.gameEvent?.status).toBe("won");
	});

	it("onGameEnd stores the per-game stats into gameEvent.stats (037 US5 FR-031)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const state = makeState("lost");
		const stats: GameStats = {
			operationCount: 7,
			correctFlags: 3,
			avgOpsPerMine: 2.33,
		};
		await sink.onGameEnd(state, "lost", stats);
		expect(buffer.gameEvent?.stats).toEqual(stats);
		// The event itself is still the structured record (stats ride along,
		// contracts/game-stats-contract.md §4).
		expect(buffer.gameEvent).toMatchObject({
			state,
			status: "lost",
			consumed: false,
		});
	});

	it("onGameEnd without stats leaves gameEvent.stats undefined (backward compatible)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		await sink.onGameEnd(makeState("won"), "won");
		expect(buffer.gameEvent?.stats).toBeUndefined();
	});

	it("onGameStart resets gameLog and writes the initial saolei_init entry", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		// A stale operate from a prior game must be wiped by onGameStart
		// (specs/036-team-mode-bugfix/data-model.md §2 — the planner reviews only the current game).
		await sink.onOperate([{ type: "click", x: 1, y: 1 }], makeState());
		expect(buffer.gameLog.length).toBeGreaterThan(0);

		await sink.onGameStart(makeState());
		expect(buffer.gameLog).toHaveLength(1);
		expect(buffer.gameLog[0]).toMatchObject({
			tool: "saolei_init",
			status: "playing",
		});
	});

	it("onOperate records ONE gameLog entry per call with the full operations list (FR-004 — not one entry per op)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const state1 = makeState();
		const state2 = makeState();
		const batch: CellOperation[] = [
			{ type: "click", x: 1, y: 1 },
			{ type: "flag", x: 2, y: 2 },
		];
		await sink.onOperate([{ type: "click", x: 0, y: 0 }], state1);
		await sink.onOperate(batch, state2);

		// Two calls ⇒ exactly two entries — the 3-op batch is ONE entry
		// carrying its full operations (FR-004).
		expect(buffer.gameLog).toHaveLength(2);
		expect(buffer.gameLog[0]).toMatchObject({
			tool: "saolei_operate",
			operations: [{ type: "click", x: 0, y: 0 }],
			status: "playing",
		});
		expect(buffer.gameLog[0].state).toBe(state1);
		expect(buffer.gameLog[1]).toMatchObject({
			tool: "saolei_operate",
			operations: batch,
			status: "playing",
		});
		expect(buffer.gameLog[1].state).toBe(state2);
	});

	it("onOperate computes status via isTerminalState/isWin (loss-first)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const lostState: GameState = {
			width: 3,
			height: 3,
			grid: [
				["0", "0", "0"],
				["0", "HIT_MINE", "0"],
				["0", "0", "0"],
			],
		};
		const wonState: GameState = {
			width: 3,
			height: 3,
			grid: [
				["0", "0", "0"],
				["0", "0", "0"],
				["0", "0", "0"],
			],
			mineCounter: { decoded: true, value: 0 },
		};

		await sink.onOperate([{ type: "click", x: 0, y: 0 }], lostState);
		expect(buffer.gameLog[0].status).toBe("lost");

		await sink.onOperate([{ type: "click", x: 0, y: 0 }], wonState);
		expect(buffer.gameLog[1].status).toBe("won");
	});

	it("onGameEnd appends a (game-end) entry with the passed status", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const state = makeState("lost");
		await sink.onGameStart(makeState());
		await sink.onGameEnd(state, "lost");

		expect(buffer.gameLog).toHaveLength(2);
		expect(buffer.gameLog[1]).toMatchObject({
			tool: "(game-end)",
			status: "lost",
		});
		expect(buffer.gameLog[1].state).toBe(state);
	});

	it("onGameStart clears cross-game log accumulation (FR-007)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		// First game: init → operate → end accumulates several entries.
		await sink.onGameStart(makeState());
		await sink.onOperate([{ type: "click", x: 1, y: 1 }], makeState());
		await sink.onGameEnd(makeState("lost"), "lost");
		expect(buffer.gameLog.length).toBeGreaterThan(1);

		// Second game: onGameStart must reset to only the new initial entry.
		await sink.onGameStart(makeState());
		expect(buffer.gameLog).toHaveLength(1);
		expect(buffer.gameLog[0]).toMatchObject({
			tool: "saolei_init",
			status: "playing",
		});
	});
});

describe("consumeGameEvent / peekGameState", () => {
	it("consumeGameEvent returns the event once and marks it consumed", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		await sink.onGameEnd(makeState("lost"), "lost");

		const first = consumeGameEvent(buffer);
		expect(first?.status).toBe("lost");
		expect(buffer.gameEvent?.consumed).toBe(true);
		// D6 step 4: read exactly once — the second consume is a no-op, so
		// the planner cannot re-trigger on the same game end.
		expect(consumeGameEvent(buffer)).toBeNull();
	});

	it("consumeGameEvent returns null when no event was written", () => {
		const buffer = createEphemeralGameBuffer();
		expect(consumeGameEvent(buffer)).toBeNull();
	});

	it("peekGameState returns null before any event", () => {
		const buffer = createEphemeralGameBuffer();
		expect(peekGameState(buffer)).toBeNull();
	});
});
