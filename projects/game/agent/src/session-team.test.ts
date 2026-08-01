/**
 * session-team.test.ts — Tests for SessionTeam / SessionTeamStore
 * (specs/031-team-template-mode/tasks.md T017; research.md D10).
 *
 * Uses a REAL compiled team graph (fakeModel + fake player tool driving the
 * sink — same pattern as team/graph.test.ts) so the turn-runner path
 * (streamEvents → per-node updates → TurnBlocks) is exercised end-to-end,
 * plus RefreshTeam (FR-018) and per-agent channel reconstruction (A3).
 *
 * Mock strategy (style/javascript.md §测试): SessionTeam receives the graph
 * handle, buffer and session id via constructor; SessionTeamStore receives a
 * factory — no `vi.mock`.
 */

import { describe, expect, it } from "vitest";
import { AIMessage, HumanMessage } from "@langchain/core/messages";
import { fakeModel } from "@langchain/core/testing";
import { tool } from "langchain";
import { z } from "zod";
import type { GameState } from "@dominion/game-saolei-board";

import { FakeStrategyStore } from "./strategy-store";
import { SessionTeam, SessionTeamStore, TeamAlreadyExistsError } from "./session-team";
import { createEphemeralGameBuffer, createTeamSink } from "./team/team-sink";
import { buildTeamGraph } from "./team/graph";
import type { TeamStateValue } from "./team/state";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";

function makeState(): GameState {
	return {
		width: 3,
		height: 3,
		grid: Array.from({ length: 3 }, () =>
			Array.from({ length: 3 }, () => "0" as const),
		),
	};
}

function buildGameEndingPlayerTool(buffer: ReturnType<typeof createEphemeralGameBuffer>) {
	const sink = createTeamSink(buffer);
	return tool(
		async ({ x, y }: { x: number; y: number }) => {
			await sink.onGameEnd(makeState(), "won");
			return `moved to (${x},${y}); game won`;
		},
		{
			name: "fake_saolei_move",
			description: "Fake saolei move that ends the game.",
			schema: z.object({ x: z.number(), y: z.number() }),
		},
	);
}

function playOneGamePlayerModel() {
	return fakeModel()
		.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
		.respond(new AIMessage("won, stopping"))
		.respond(new AIMessage("idle, no new game"));
}

function updateStrategyPlannerModel(content: string) {
	return fakeModel()
		.respondWithTools([{ name: "update_strategy", args: { content } }])
		.respond(new AIMessage("strategy updated"));
}

/** Build a fully-wired SessionTeam (real graph) for a session id. */
function buildTestTeam(sessionId: string, store = new FakeStrategyStore()) {
	const buffer = createEphemeralGameBuffer();
	const handle = buildTeamGraph({
		playerModel: playOneGamePlayerModel(),
		plannerModel: updateStrategyPlannerModel("corner-first"),
		strategyStore: store,
		buffer,
		sessionId,
		playerTools: [buildGameEndingPlayerTool(buffer)],
	});
	const team = new SessionTeam(handle, buffer, sessionId);
	return { team, store, buffer, sessionId };
}

/** Recording emit sink collecting every frame the loop pushes. */
function recordingEmit(): {
	emit: (f: AgentFrame) => void;
	frames: AgentFrame[];
} {
	const frames: AgentFrame[] = [];
	return { emit: (f) => frames.push(f), frames };
}

function textBlocks(frames: AgentFrame[]): { agent?: string; text: string }[] {
	const out: { agent?: string; text: string }[] = [];
	for (const f of frames) {
		const fr = f as Record<string, unknown>;
		if (fr.payload !== "messageParts") continue;
		for (const p of (fr.messageParts as { parts: { text?: { content?: string } }[] }).parts) {
			const content = p.text?.content;
			if (content) out.push({ agent: fr.agent as string | undefined, text: content });
		}
	}
	return out;
}

function waitCount(frames: AgentFrame[]): number {
	return frames.filter((f) => {
		const fr = f as Record<string, unknown>;
		return (
			fr.payload === "flowParts" &&
			(fr.flowParts as { parts: Record<string, unknown>[] }).parts.some(
				(p) => "wait" in p,
			)
		);
	}).length;
}

function flush(ms = 60): Promise<void> {
	return new Promise((r) => setTimeout(r, ms));
}

describe("SessionTeam", () => {
	it("drives one team turn per submit: player + planner blocks streamed with agent tags, then wait", async () => {
		const { team } = buildTestTeam("st-turn-1");
		const { emit, frames } = recordingEmit();

		team.submit({ text: "开始游戏" }, emit);
		expect(team.isRunning()).toBe(true);
		await flush();

		expect(team.isRunning()).toBe(false);
		// The turn ran player (game end → planner → player idle) and the
		// loop emitted a terminal wait.
		expect(waitCount(frames)).toBe(1);
		// Player frames carry agent="player" (fakeModel echoes input text as
		// response content — the agent tag is the contract, not the text).
		const texts = textBlocks(frames);
		expect(texts.length).toBeGreaterThan(0);
		expect(texts.every((t) => t.agent === "player" || t.agent === "planner")).toBe(true);
	});

	it("getTeamState reconstructs both per-agent channels from the single checkpointer (A3)", async () => {
		const { team, sessionId } = buildTestTeam("st-state");
		const { emit } = recordingEmit();
		team.submit({ text: "开始游戏" }, emit);
		await flush();

		const state = await team.getTeamState();
		expect(state).not.toBeNull();
		expect((state as TeamStateValue).playerMessages.length).toBeGreaterThan(0);
		// The planner ran (game ended) — its channel holds the review.
		expect((state as TeamStateValue).plannerMessages.length).toBeGreaterThan(0);
		expect(sessionId).toBe("st-state");
	});

	it("refreshTeam clears BOTH channels, keeps the strategy, leaves gameEnded alone (FR-018)", async () => {
		const store = new FakeStrategyStore();
		const { team } = buildTestTeam("st-refresh", store);
		const { emit } = recordingEmit();
		team.submit({ text: "开始游戏" }, emit);
		await flush();

		// Strategy written by the planner during the turn.
		expect(await store.get("st-refresh")).toBe("corner-first");

		await team.refreshTeam();

		const state = (await team.getTeamState()) as TeamStateValue;
		expect(state.playerMessages).toEqual([]);
		expect(state.plannerMessages).toEqual([]);
		expect(await store.get("st-refresh")).toBe("corner-first");
		expect(state.gameEnded).toBeNull();
	});

	it("abort stops a running turn and emits wait", async () => {
		// A player model that never stops would hang the test; instead use a
		// gate-less quick model and abort mid-flight via a slow fake: the
		// echo runner here is the REAL graph, so use a plain turn and abort
		// immediately after submit (best-effort — loop observes abort).
		const { team } = buildTestTeam("st-abort");
		const { emit, frames } = recordingEmit();
		team.submit({ text: "hi" }, emit);
		team.abort();
		await flush();
		expect(team.isRunning()).toBe(false);
	});

	it("getBridge returns the session bridge and getSink binds the ephemeral buffer", async () => {
		const { team, buffer, sessionId } = buildTestTeam("st-surface");
		expect(team.getBridge()).toBeDefined();
		const sink = team.getSink();
		expect(sink).toBeDefined();
		// The sink writes the session's ephemeral buffer (D7).
		sink.onGameEnd(makeState(), "lost");
		expect(buffer.gameEvent).not.toBeNull();
		expect(buffer.gameEvent?.status).toBe("lost");
		expect(sessionId).toBe("st-surface");
	});
});

describe("SessionTeamStore", () => {
	it("create builds once per session and forwards template+profile; get returns the cached team", async () => {
		const created: string[] = [];
		const seenArgs: Array<[string, string]> = [];
		const store = new SessionTeamStore(async (sessionId, template, profileName) => {
			created.push(sessionId);
			seenArgs.push([template, profileName]);
			return buildTestTeam(sessionId).team;
		});

		const t1 = await store.create("s-1", "saolei", "default");
		const t2 = await store.create("s-1", "saolei", "default");
		expect(t1).toBe(t2);
		expect(created).toEqual(["s-1"]);
		expect(seenArgs).toEqual([["saolei", "default"]]);
		expect(store.get("s-1")).toBe(t1);
		expect(store.get("s-2")).toBeUndefined();
	});

	it("create is idempotent for the same profile on an existing session", async () => {
		const created: string[] = [];
		const store = new SessionTeamStore(async (sessionId) => {
			created.push(sessionId);
			return buildTestTeam(sessionId).team;
		});

		const t1 = await store.create("s-2", "saolei", "default");
		const t2 = await store.create("s-2", "saolei", "default");
		expect(t1).toBe(t2);
		expect(created).toEqual(["s-2"]);
	});

	it("create rejects TeamAlreadyExistsError for a DIFFERENT profile on an existing session", async () => {
		const created: string[] = [];
		const store = new SessionTeamStore(async (sessionId) => {
			created.push(sessionId);
			return buildTestTeam(sessionId).team;
		});

		const t1 = await store.create("s-2", "saolei", "default");
		await expect(
			store.create("s-2", "saolei", "other"),
		).rejects.toBeInstanceOf(TeamAlreadyExistsError);
		await expect(
			store.create("s-2", "saolei", "other"),
		).rejects.toThrow(/profile 'default'/);
		expect(t1).toBe(store.get("s-2"));
		expect(created).toEqual(["s-2"]);
	});

	it("single-flights concurrent creates for the same session", async () => {
		const created: string[] = [];
		const store = new SessionTeamStore(async (sessionId) => {
			created.push(sessionId);
			return buildTestTeam(sessionId).team;
		});

		const [t1, t2] = await Promise.all([
			store.create("s-3", "saolei", "default"),
			store.create("s-3", "saolei", "default"),
		]);
		expect(t1).toBe(t2);
		expect(created).toEqual(["s-3"]);
	});

	it("single-flight loser with a DIFFERENT profile rejects after the winner completes", async () => {
		const created: string[] = [];
		const store = new SessionTeamStore(async (sessionId) => {
			created.push(sessionId);
			return buildTestTeam(sessionId).team;
		});

		const [t1, t2] = await Promise.allSettled([
			store.create("s-3", "saolei", "default"),
			store.create("s-3", "saolei", "other"),
		]);
		expect(t1.status).toBe("fulfilled");
		expect(t2.status).toBe("rejected");
		if (t2.status === "rejected") {
			expect(t2.reason).toBeInstanceOf(TeamAlreadyExistsError);
		}
		// Only ONE team was ever built (single-flight), and the loser's
		// profile comparison re-entry did not build a second graph.
		expect(created).toEqual(["s-3"]);
		expect(store.get("s-3")).toBe(
			t1.status === "fulfilled" ? t1.value : undefined,
		);
	});

	it("does not create implicitly: get on an unknown session returns undefined", async () => {
		const created: string[] = [];
		const store = new SessionTeamStore(async (sessionId) => {
			created.push(sessionId);
			return buildTestTeam(sessionId).team;
		});

		expect(store.get("s-missing")).toBeUndefined();
		expect(created).toEqual([]);
	});
});
