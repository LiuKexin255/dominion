/**
 * team-sink.test.ts — team-side SaoleiEventSink consumer + ephemeral buffer
 * semantics (`specs/031-team-template-mode/contracts/saolei-sink-contract.md`
 * §4 / §7, `contracts/team-graph-contract.md` §4, research.md D7).
 */

import { describe, expect, it } from "vitest";
import type { GameState } from "@dominion/game-saolei-board";

import {
	consumeGameEvent,
	createEphemeralGameBuffer,
	createTeamSink,
	peekGameState,
} from "./team-sink";

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

	it("onMove updates gameState (and does not touch gameEvent)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const state1 = makeState();
		const state2 = makeState();
		await sink.onMove("saolei_click", 1, 1, state1);
		expect(peekGameState(buffer)).toBe(state1);
		expect(buffer.gameEvent).toBeNull();

		await sink.onMove("saolei_flag", 2, 2, state2);
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

	it("onGameStart resets gameLog and writes the initial saolei_init entry", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		// A stale move from a prior game must be wiped by onGameStart
		// (specs/036-team-mode-bugfix/data-model.md §2 — the planner reviews only the current game).
		await sink.onMove("saolei_click", 1, 1, makeState());
		expect(buffer.gameLog.length).toBeGreaterThan(0);

		await sink.onGameStart(makeState());
		expect(buffer.gameLog).toHaveLength(1);
		expect(buffer.gameLog[0]).toMatchObject({
			tool: "saolei_init",
			status: "playing",
		});
	});

	it("onMove accumulates operation entries with tool/x/y/state/status", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const state1 = makeState();
		const state2 = makeState();
		await sink.onMove("saolei_click", 1, 1, state1);
		await sink.onMove("saolei_flag", 2, 2, state2);

		expect(buffer.gameLog).toHaveLength(2);
		expect(buffer.gameLog[0]).toMatchObject({
			tool: "saolei_click",
			x: 1,
			y: 1,
			status: "playing",
		});
		expect(buffer.gameLog[0].state).toBe(state1);
		expect(buffer.gameLog[1]).toMatchObject({
			tool: "saolei_flag",
			x: 2,
			y: 2,
			status: "playing",
		});
		expect(buffer.gameLog[1].state).toBe(state2);
	});

	it("onMove computes status via isTerminalState/isWin (loss-first)", async () => {
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

		await sink.onMove("saolei_click", 0, 0, lostState);
		expect(buffer.gameLog[0].status).toBe("lost");

		await sink.onMove("saolei_click", 0, 0, wonState);
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
		// First game: init → move → end accumulates several entries.
		await sink.onGameStart(makeState());
		await sink.onMove("saolei_click", 1, 1, makeState());
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
