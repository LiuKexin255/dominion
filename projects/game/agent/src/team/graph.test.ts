/**
 * team/graph.test.ts — end-to-end tests of the saolei team graph
 * (`specs/031-team-template-mode/contracts/team-graph-contract.md` §7,
 * research.md D6/D14).
 *
 * Deterministic fake-model + fake-tool driven (same pattern as
 * `experimental/ts/team_graph_spike/src/spike.test.ts`): the player's fake
 * tool triggers the real team sink (writing the ephemeral buffer), so the
 * full sink → buffer → node post-process → conditional-edge → planner path is
 * exercised without any LLM or MCP server (DI seams, style/javascript.md
 * §测试).
 *
 * Assertion notes (D14 注意事项 3): `gameEnded`'s FINAL value is `null` (the
 * planner clears it, D6 step 6) — "a game ended" is asserted via the planner
 * having RUN (`plannerMessages` non-empty / strategy written).
 */

import { describe, expect, it, vi } from "vitest";
import { AIMessage, HumanMessage, SystemMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import { fakeModel } from "@langchain/core/testing";
import { tool } from "langchain";
import { z } from "zod";
import type { GameState } from "@dominion/game-saolei-board";

import { FakeStrategyStore } from "../strategy-store";
import { appendSkillBodyToPrompt, SKILL_PROMPT_SEPARATOR } from "../skill-loader";
import {
	createEphemeralGameBuffer,
	createTeamSink,
	type EphemeralGameBuffer,
} from "./team-sink";
import { buildTeamGraph, SAOLEI_TEAM_AGENTS } from "./graph";
import { DEFAULT_PLAYER_BASE } from "./player";
import type { CreateAgentFn } from "./player";
import { DEFAULT_PLANNER_BASE } from "./planner";
import type { TeamStateValue } from "./state";

/** A minimal recognizable GameState (3x3, all empty cells). */
function makeState(): GameState {
	return {
		width: 3,
		height: 3,
		grid: Array.from({ length: 3 }, () =>
			Array.from({ length: 3 }, () => "0" as const),
		),
	};
}

/** The fake player tool: ends the game (structured sink event) on every call. */
function buildGameEndingPlayerTool(buffer: EphemeralGameBuffer) {
	const sink = createTeamSink(buffer);
	return tool(
		async ({ x, y }: { x: number; y: number }) => {
			// D6 step 2: structured signal via the sink, no text parsing.
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

/** The player's fake model for a "play one game then idle" flow. */
function playOneGamePlayerModel() {
	// call 1: make a move (fires the fake tool → sink.onGameEnd("won"));
	// call 2: stop (game over); call 3: the planner→player return — idle.
	return fakeModel()
		.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
		.respond(new AIMessage("game won, stopping"))
		.respond(new AIMessage("idle, no new game"));
}

/** The planner's fake model for a "review + update strategy" flow. */
function updateStrategyPlannerModel(content: string) {
	return fakeModel()
		.respondWithTools([{ name: "update_strategy", args: { content } }])
		.respond(new AIMessage("strategy updated"));
}

/** Common graph wiring shared by most tests. */
function buildTestGraph(
	overrides: {
		store?: FakeStrategyStore;
		playerModel?: ReturnType<typeof playOneGamePlayerModel>;
		plannerModel?: ReturnType<typeof updateStrategyPlannerModel>;
		sessionId?: string;
		playerBasePrompt?: string;
		plannerBasePrompt?: string;
		createAgentFn?: CreateAgentFn;
	} = {},
) {
	const store = overrides.store ?? new FakeStrategyStore();
	const buffer = createEphemeralGameBuffer();
	const sessionId = overrides.sessionId ?? "graph-test";
	const { graph, checkpointer } = buildTeamGraph({
		playerModel: overrides.playerModel ?? playOneGamePlayerModel(),
		plannerModel:
			overrides.plannerModel ?? updateStrategyPlannerModel("corner-first"),
		strategyStore: store,
		buffer,
		sessionId,
		playerTools: [buildGameEndingPlayerTool(buffer)],
		playerBasePrompt: overrides.playerBasePrompt ?? "",
		plannerBasePrompt: overrides.plannerBasePrompt ?? "",
		createAgentFn: overrides.createAgentFn,
	});
	return { graph, checkpointer, store, buffer, sessionId };
}

function contentType(m: BaseMessage): string {
	const c = m.content;
	return typeof c === "string" ? c : JSON.stringify(c);
}

describe("SAOLEI_TEAM_AGENTS template description (FR-031/D3)", () => {
	it("declares player as accepting user input and planner as not", () => {
		expect(SAOLEI_TEAM_AGENTS).toEqual([
			{ name: "player", accepts_user_input: true },
			{ name: "planner", accepts_user_input: false },
		]);
	});
});

describe("team graph — game-end flow (player → planner → player)", () => {
	it("routes player→planner on game end, planner writes strategy and clears gameEnded, then returns to player (D6)", async () => {
		const playerModel = playOneGamePlayerModel();
		const plannerModel = updateStrategyPlannerModel("corner-first");
		const { graph, store } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-flow" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// D14 注意事项 3: the FINAL gameEnded is null (cleared by the planner).
		expect(result.gameEnded).toBeNull();

		// The planner RAN (game ended ⇒ routed to planner exactly once):
		// its channel carries the review request + AI + tool messages.
		expect(result.plannerMessages.length).toBeGreaterThan(0);

		// The planner wrote the strategy to the long-term store (FR-013).
		expect(await store.get("graph-test")).toBe("corner-first");

		// planner→player edge: the player ran AGAIN after the planner
		// (idle — no new game ⇒ gameEnded stays null ⇒ END). The player
		// model was called 3 times: move / stop / idle.
		expect(playerModel.calls).toHaveLength(3);
		expect(result.playerMessages.length).toBeGreaterThan(0);

		// The strategy message never entered the channel (D4 — strategy not
		// in short-term state): the channel holds NO system message.
		// (Note: fakeModel derives tool-call message content from the model
		// input, so strategy text may appear inside AI content — the channel
		// shape, not the text, is the contract.)
		for (const m of result.playerMessages) {
			expect(m._getType()).not.toBe("system");
		}
	});

	it("routes player→END when no game ended (planner never runs)", async () => {
		const playerModel = fakeModel().respond(new AIMessage("just chatting"));
		const plannerModel = updateStrategyPlannerModel("should-not-run");
		const { graph, store } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("你好")] },
			{ configurable: { thread_id: "t-noend" }, recursionLimit: 50 },
		)) as TeamStateValue;

		expect(result.gameEnded).toBeNull();
		// Planner not triggered: no review messages, no strategy write.
		expect(result.plannerMessages).toEqual([]);
		expect(await store.get("graph-test")).toBe("");
		// Only ONE player run (conditional edge → END, not a loop).
		expect(playerModel.calls).toHaveLength(1);
	});

	it("keeps per-agent channels independent — player and planner histories do not mix (D5/FR-005)", async () => {
		const { graph } = buildTestGraph();

		await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-partition" }, recursionLimit: 50 },
		);

		const snapshot = (await graph.getState({
			configurable: { thread_id: "t-partition" },
		})) as unknown as { values: TeamStateValue };

		// Both channels reconstructable from the single MemorySaver (A3).
		expect(snapshot.values.playerMessages.length).toBeGreaterThan(0);
		expect(snapshot.values.plannerMessages.length).toBeGreaterThan(0);

		// Player's channel holds the player's conversation only.
		const playerTexts = snapshot.values.playerMessages.map(contentType);
		expect(playerTexts.some((t) => t.includes("开始游戏"))).toBe(true);
		expect(playerTexts.some((t) => t.includes("本局已结束"))).toBe(false);

		// Planner's channel holds the review conversation only.
		const plannerTexts = snapshot.values.plannerMessages.map(contentType);
		expect(plannerTexts.some((t) => t.includes("本局已结束"))).toBe(true);
		expect(plannerTexts.some((t) => t.includes("开始游戏"))).toBe(false);
	});

	it("injects the current strategy into the player prompt as 当前态势 (FR-015) without a read tool", async () => {
		const store = new FakeStrategyStore();
		await store.put("graph-test", "flags-then-numbers");
		const playerModel = playOneGamePlayerModel();
		const { graph, buffer } = buildTestGraph({ store, playerModel });

		await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-strategy" }, recursionLimit: 50 },
		);

		// The player model's FIRST call received the strategy SystemMessage.
		const firstCall = playerModel.calls[0]?.messages as BaseMessage[];
		expect(firstCall).toBeDefined();
		const strategyMsg = firstCall.find(
			(m) =>
				m._getType() === "system" &&
				contentType(m).includes("flags-then-numbers"),
		);
		expect(strategyMsg).toBeInstanceOf(SystemMessage);
		// The player's tool set is the injected fake — no read tool exists.
		expect(buffer.gameEvent?.consumed).toBe(true);
	});

	it("injects the current strategy into the planner's system context (FR-014; initial \"\")", async () => {
		const store = new FakeStrategyStore();
		await store.put("graph-test", "corner-first");
		const plannerModel = updateStrategyPlannerModel("corner-first");
		const { graph } = buildTestGraph({ store, plannerModel });

		await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-planner-strategy" }, recursionLimit: 50 },
		);

		const firstCall = plannerModel.calls[0]?.messages as BaseMessage[];
		expect(firstCall).toBeDefined();
		// Strategy injected as a system message (FR-014).
		const strategyMsg = firstCall.find(
			(m) => m._getType() === "system" && contentType(m).includes("corner-first"),
		);
		expect(strategyMsg).toBeDefined();
		// The review input (gameState) is present as the planner's prompt.
		expect(
			firstCall.some((m) => contentType(m).includes("本局已结束")),
		).toBe(true);
	});

	it("planner system context starts with the EMPTY strategy on a fresh session (FR-014 初始 \"\")", async () => {
		const plannerModel = updateStrategyPlannerModel("first-strategy");
		const { graph } = buildTestGraph({ plannerModel });

		await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-empty-strategy" }, recursionLimit: 50 },
		);

		const firstCall = plannerModel.calls[0]?.messages as BaseMessage[];
		const strategyMsg = firstCall.find(
			(m) => m._getType() === "system" && contentType(m).includes("当前策略"),
		);
		expect(strategyMsg).toBeDefined();
		expect(contentType(strategyMsg as BaseMessage)).toContain("无");
	});
});

describe("team graph — multi-game loop (FR-009)", () => {
	it("plays two games in one turn: planner fires once per game end and the strategy accumulates", async () => {
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respond(new AIMessage("game 1 won"))
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 2, y: 2 } }])
			.respond(new AIMessage("game 2 won"))
			.respond(new AIMessage("idle, stopping"));
		const plannerModel = fakeModel()
			.respondWithTools([
				{ name: "update_strategy", args: { content: "v1" } },
			])
			.respond(new AIMessage("v1 written"))
			.respondWithTools([
				{ name: "update_strategy", args: { content: "v2" } },
			])
			.respond(new AIMessage("v2 written"));

		const { graph, store } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-multi" }, recursionLimit: 100 },
		)) as TeamStateValue;

		// Planner ran twice (once per game end) and the LAST write wins.
		expect(await store.get("graph-test")).toBe("v2");
		expect(result.gameEnded).toBeNull();
		// Two review REQUESTS (human messages) in the planner channel — one
		// per game end. (AI content is fakeModel-derived from the input, so
		// only the human review requests are counted.)
		const reviewRequests = result.plannerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				contentType(m).includes("本局已结束"),
		);
		expect(reviewRequests).toHaveLength(2);
		// Player: 5 model calls (move/stop ×2 + idle).
		expect(playerModel.calls).toHaveLength(5);
	});
});

describe("team graph — planner retry/degrade (D6 需求方 #6)", () => {
	it("planner degrades on persistent invoke failure: gameEnded cleared, no strategy write, graph completes", async () => {
		const playerModel = playOneGamePlayerModel();
		const plannerModel = fakeModel()
			.respond(new Error("planner llm down"))
			.respond(new Error("planner llm down"))
			.respond(new Error("planner llm down"));
		const { graph, store } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-degrade" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The graph completed; gameEnded cleared despite the failure
		// (unconditional clear, D6 step 6 — no infinite planner re-trigger).
		expect(result.gameEnded).toBeNull();
		expect(await store.get("graph-test")).toBe("");
	});
});

describe("team graph — strategy injection edge cases", () => {
	it("does not write the strategy SystemMessage into the player channel (D4: 策略不在 state)", async () => {
		const store = new FakeStrategyStore();
		await store.put("graph-test", "secret-strategy-text");
		const playerModel = playOneGamePlayerModel();
		const { graph } = buildTestGraph({ store, playerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-no-state-strategy" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The strategy SystemMessage (fixed id) is filtered from the write-
		// back: the channel holds NO system message. (fakeModel-derived AI
		// content may still echo the text — the channel SHAPE is the
		// contract, D4: 策略不在 state.)
		for (const m of result.playerMessages) {
			expect(m._getType()).not.toBe("system");
		}
	});

	it("player tool failures surface to the player's agent loop (tool error, not graph crash)", async () => {
		const buffer = createEphemeralGameBuffer();
		const failingTool = tool(
			async () => {
				throw new Error("desktop disconnected");
			},
			{
				name: "fake_saolei_move",
				description: "Failing fake move.",
				schema: z.object({ x: z.number(), y: z.number() }),
			},
		);
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respond(new AIMessage("tool failed, stopping"))
			.respond(new AIMessage("idle"));
		const plannerModel = updateStrategyPlannerModel("never");
		const store = new FakeStrategyStore();
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			strategyStore: store,
			buffer,
			sessionId: "graph-test",
			playerTools: [failingTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-toolfail" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The tool error did not crash the graph; no game end event was
		// produced (the failing tool never called the sink), so the planner
		// was NOT triggered.
		expect(result.gameEnded).toBeNull();
		expect(result.plannerMessages).toEqual([]);
		expect(await store.get("graph-test")).toBe("");
	});
});

describe("player/planner base prompts from the TeamProfile (FR-034 semantics A)", () => {
	/**
	 * Spy createAgentFn (DI seam) capturing the `systemPrompt` of each node's
	 * agent. The player's prompt always carries the appended saolei skill
	 * body (SKILL_PROMPT_SEPARATOR); the planner's is a bare base — that
	 * distinguishes the two calls without depending on build order.
	 */
	function captureSystemPrompts(): {
		createAgentFn: CreateAgentFn;
		playerSystemPrompt(): string;
		plannerSystemPrompt(): string;
	} {
		const calls: string[] = [];
		const createAgentFn = vi.fn((config: { systemPrompt?: string }) => {
			calls.push(config.systemPrompt ?? "");
			return { invoke: async () => ({ messages: [] as BaseMessage[] }) };
		});
		return {
			createAgentFn,
			playerSystemPrompt: () =>
				calls.find((p) => p.includes(SKILL_PROMPT_SEPARATOR)) ?? "",
			plannerSystemPrompt: () =>
				calls.find((p) => !p.includes(SKILL_PROMPT_SEPARATOR)) ?? "",
		};
	}

	it("player: non-empty player_prompt overrides the base AND the saolei skill body is still appended (FR-034)", () => {
		const profilePrompt = "你是自定义的 player 操作者。";
		const { createAgentFn, playerSystemPrompt } = captureSystemPrompts();
		buildTestGraph({ playerBasePrompt: profilePrompt, createAgentFn });

		// Semantics A: final assembly = appendSkillBodyToPrompt(profile
		// prompt, ["saolei"]) — starts with the profile prompt and still
		// carries the template-appended skill body.
		const prompt = playerSystemPrompt();
		expect(prompt.startsWith(profilePrompt)).toBe(true);
		expect(prompt).toContain(SKILL_PROMPT_SEPARATOR);
		expect(prompt).toBe(appendSkillBodyToPrompt(profilePrompt, ["saolei"]));
		// The spy was actually exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
	});

	it("player: empty player_prompt falls back to DEFAULT_PLAYER_BASE with the skill body appended (FR-034)", () => {
		const { createAgentFn, playerSystemPrompt } = captureSystemPrompts();
		buildTestGraph({ createAgentFn }); // playerBasePrompt defaults to ""

		const prompt = playerSystemPrompt();
		expect(prompt.startsWith(DEFAULT_PLAYER_BASE)).toBe(true);
		expect(prompt).toContain(SKILL_PROMPT_SEPARATOR);
		expect(prompt).toBe(
			appendSkillBodyToPrompt(DEFAULT_PLAYER_BASE, ["saolei"]),
		);
		expect(createAgentFn).toHaveBeenCalled();
	});

	it("planner: non-empty planner_prompt is used as the systemPrompt verbatim (FR-034)", () => {
		const profilePrompt = "你是自定义的 planner 复盘者。";
		const { createAgentFn, plannerSystemPrompt } = captureSystemPrompts();
		buildTestGraph({ plannerBasePrompt: profilePrompt, createAgentFn });

		expect(plannerSystemPrompt()).toBe(profilePrompt);
		// The planner appends NO skill body (FR-012/FR-034).
		expect(plannerSystemPrompt()).not.toContain(SKILL_PROMPT_SEPARATOR);
		expect(createAgentFn).toHaveBeenCalled();
	});

	it("planner: empty planner_prompt falls back to DEFAULT_PLANNER_BASE (FR-034)", () => {
		const { createAgentFn, plannerSystemPrompt } = captureSystemPrompts();
		buildTestGraph({ createAgentFn }); // plannerBasePrompt defaults to ""

		expect(plannerSystemPrompt()).toBe(DEFAULT_PLANNER_BASE);
		expect(createAgentFn).toHaveBeenCalled();
	});
});
