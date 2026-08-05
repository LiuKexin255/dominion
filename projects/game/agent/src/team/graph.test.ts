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
import type { StructuredToolInterface } from "@langchain/core/tools";
import { fakeModel } from "@langchain/core/testing";
import { createAgent, tool } from "langchain";
import { z } from "zod";
import type { GameState } from "@dominion/game-saolei-board";

import { FakeStrategyStore } from "../strategy-store";
import { appendSkillBodyToPrompt, SKILL_PROMPT_SEPARATOR } from "../skill-loader";
import { refreshTeamChannels } from "../context-middleware";
import {
	createEphemeralGameBuffer,
	createTeamSink,
	type EphemeralGameBuffer,
} from "./team-sink";
import { buildTeamGraph, SAOLEI_TEAM_AGENTS } from "./graph";
import { createPlayerNode, DEFAULT_PLAYER_BASE, PLAYER_AGENT_NAME } from "./player";
import type { CreateAgentFn } from "./player";
import { DEFAULT_PLANNER_BASE, PLANNER_AGENT_NAME } from "./planner";
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

/**
 * The fake player tool with a MIXED won/lost outcome (FR-006 — both end a
 * game and both count toward the compression trigger): even x ⇒ won, odd x ⇒
 * lost.
 */
function buildMixedOutcomePlayerTool(buffer: EphemeralGameBuffer) {
	const sink = createTeamSink(buffer);
	return tool(
		async ({ x, y }: { x: number; y: number }) => {
			await sink.onGameEnd(makeState(), x % 2 === 0 ? "won" : "lost");
			return `moved to (${x},${y}); ${x % 2 === 0 ? "won" : "lost"}`;
		},
		{
			name: "fake_saolei_move",
			description: "Fake saolei move that ends the game (won/lost mix).",
			schema: z.object({ x: z.number(), y: z.number() }),
		},
	);
}

/** The player's fake model for a "play one game then idle" flow. */
function playOneGamePlayerModel() {
	// call 1: make a move (fires the fake tool → sink.onGameEnd("won"));
	// call 2: the planner→player return — idle. No "stop" call: the
	// gameEndGuard middleware stops the loop right after the game end.
	return fakeModel()
		.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
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
		playerTools?: StructuredToolInterface[];
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
		playerTools:
			overrides.playerTools ?? [buildGameEndingPlayerTool(buffer)],
		playerBasePrompt: overrides.playerBasePrompt ?? "",
		plannerBasePrompt: overrides.plannerBasePrompt ?? "",
		createAgentFn: overrides.createAgentFn,
	});
	return { graph, checkpointer, store, buffer, sessionId };
}

/**
 * The player's fake model for "5 consecutive game endings then compress": one
 * move tool call per game (each move fires the fake tool → sink.onGameEnd,
 * FR-006 — every ended game counts), then the compress node's player-channel
 * summary call returns `summaryContent`.
 */
function fiveGamesPlayerModel(summaryContent: string) {
	const model = fakeModel();
	for (let i = 0; i < 5; i += 1) {
		model.respondWithTools([
			{ name: "fake_saolei_move", args: { x: i + 1, y: i + 1 } },
		]);
	}
	model.respond(new AIMessage(summaryContent));
	return model;
}

/**
 * The planner's fake model for "5 review runs then compress": per game one
 * `update_strategy` tool call + a plain response (2 model calls per planner
 * run — strategy v1..v5 accumulate, last write wins), then the compress
 * node's planner-channel summary call returns `summaryContent`.
 */
function fiveGamesPlannerModel(summaryContent: string) {
	const model = fakeModel();
	for (let i = 0; i < 5; i += 1) {
		model.respondWithTools([
			{ name: "update_strategy", args: { content: `v${i + 1}` } },
		]);
		model.respond(new AIMessage(`v${i + 1} written`));
	}
	model.respond(new AIMessage(summaryContent));
	return model;
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
		// model was called 2 times: move / idle — the gameEndGuard
		// middleware stops the loop right after the game end, so the
		// pre-fix "stop" call no longer happens.
		expect(playerModel.calls).toHaveLength(2);
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
		expect(playerTexts.some((t) => t.includes("本局游戏过程"))).toBe(false);

		// Planner's channel holds the review conversation only.
		const plannerTexts = snapshot.values.plannerMessages.map(contentType);
		expect(plannerTexts.some((t) => t.includes("本局游戏过程"))).toBe(true);
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
		// The review input (full gameLog rendering) is present as the
		// planner's prompt (Issue 2 — `specs/036-team-mode-bugfix/
		// contracts/team-graph-fix-contract.md` §2.2).
		expect(
			firstCall.some((m) => contentType(m).includes("本局游戏过程")),
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

describe("team graph — Issue 1 (036): game-end loop stop & resilient post-process", () => {
	it("routes player→planner on a LOST game end and stops the loop (Issue 1)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const losingMoveTool = tool(
			async ({ x, y }: { x: number; y: number }) => {
				// D6 step 2: structured signal via the sink, no text parsing.
				await sink.onGameEnd(makeState(), "lost");
				return `moved to (${x},${y}); game lost`;
			},
			{
				name: "fake_saolei_move",
				description: "Fake saolei move that loses the game.",
				schema: z.object({ x: z.number(), y: z.number() }),
			},
		);
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respond(new AIMessage("idle, no new game"));
		const plannerModel = updateStrategyPlannerModel("safer-play");
		const store = new FakeStrategyStore();
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			strategyStore: store,
			buffer,
			sessionId: "graph-test",
			playerTools: [losingMoveTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-lost" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The planner RAN (game ended ⇒ routed exactly once): its channel
		// carries the review request and the strategy was written (FR-013).
		expect(result.plannerMessages.length).toBeGreaterThan(0);
		expect(await store.get("graph-test")).toBe("safer-play");
		// The planner cleared gameEnded (D6 step 6) — final value null.
		expect(result.gameEnded).toBeNull();
		// The gameEndGuard middleware stopped the loop right after the game
		// end: the game-end turn used exactly ONE model call (move) — the
		// pre-fix "stop"/restart call no longer happens; the planner→player
		// return adds the final idle call.
		expect(playerModel.calls).toHaveLength(2);
	});

	it("stops the player loop before the model's restart attempt when the game ends (US1 acceptance #4)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const moveExecutor = vi.fn(async ({ x, y }: { x: number; y: number }) => {
			await sink.onGameEnd(makeState(), "lost");
			return `moved to (${x},${y}); game lost`;
		});
		const losingMoveTool = tool(moveExecutor, {
			name: "fake_saolei_move",
			description: "Fake saolei move that loses the game.",
			schema: z.object({ x: z.number(), y: z.number() }),
		});
		const restartExecutor = vi.fn(async () => "new game started");
		const restartTool = tool(restartExecutor, {
			name: "saolei_init",
			description: "Restart a new saolei game.",
			schema: z.object({}),
		});
		// The model first makes the losing move, then ATTEMPTS to restart a
		// new game (saolei_init). With the game-end event still unconsumed,
		// the gameEndGuard middleware stops the loop before the attempt.
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respondWithTools([{ name: "saolei_init", args: {} }])
			.respond(new AIMessage("idle, no new game"));
		const store = new FakeStrategyStore();
		const node = createPlayerNode({
			model: playerModel,
			strategyStore: store,
			buffer,
			sessionId: "graph-test",
			tools: [losingMoveTool, restartTool],
			playerBasePrompt: "",
		});

		const result = await node({
			playerMessages: [new HumanMessage("开始游戏")],
		} as TeamStateValue);

		// The post-process consumed the event and set gameEnded.
		expect(result.gameEnded).toBe("lost");
		expect(buffer.gameEvent?.consumed).toBe(true);
		// The loop stopped after the move: the model was called exactly once,
		// so the saolei_init response was never consumed (tool call count =
		// 0 — the middleware stopped the loop).
		expect(playerModel.calls).toHaveLength(1);
		expect(restartExecutor).not.toHaveBeenCalled();
		// The tool seam was actually exercised (style/javascript.md — verify
		// the fake tool path works).
		expect(moveExecutor).toHaveBeenCalledTimes(1);
	});

	it("consumes the game-end event and routes to the planner even when the player invoke throws (try/finally, Issue 1)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		// Pre-write an unconsumed game-end event (e.g. the agent loop crashed
		// right after the game ended, before the node's post-process ran).
		await sink.onGameEnd(makeState(), "lost");

		// DI spy (style/javascript.md §测试): the PLAYER agent's invoke throws
		// (e.g. GraphRecursionError / model / tool error); the planner agent
		// is the real one, so the routed-to planner completes normally. The
		// player prompt always carries the appended saolei skill body
		// (FR-034), the planner's never does — same dispatch heuristic as
		// captureSystemPrompts.
		const createAgentFn = vi.fn((config: { systemPrompt?: string }) => {
			if (config.systemPrompt?.includes(SKILL_PROMPT_SEPARATOR)) {
				return {
					invoke: async () => {
						throw new Error("player agent loop crashed");
					},
				};
			}
			return createAgent(config as Parameters<typeof createAgent>[0]);
		});
		const plannerModel = updateStrategyPlannerModel("post-crash-strategy");
		const store = new FakeStrategyStore();
		const { graph } = buildTeamGraph({
			playerModel: fakeModel().respond(new AIMessage("unused")),
			plannerModel,
			strategyStore: store,
			buffer,
			sessionId: "graph-test",
			playerTools: [],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			createAgentFn,
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-invoke-crash" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The spy was actually exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
		// try/finally: the pre-written event was consumed despite the throw.
		expect(buffer.gameEvent?.consumed).toBe(true);
		// gameEnded WAS set ("lost") ⇒ the conditional edge routed to the
		// planner, which ran and cleared it (D6 step 6).
		expect(result.plannerMessages.length).toBeGreaterThan(0);
		expect(await store.get("graph-test")).toBe("post-crash-strategy");
		expect(result.gameEnded).toBeNull();
	});
});

describe("team graph — Issue 2 (036): planner review input renders the full gameLog", () => {
	it("renders every gameLog entry — tool, coordinates, status and board — in the review input (US2 acceptance #1-4)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		// Two moves (still playing), then a losing game end — the sink
		// accumulates one gameLog entry per step (Phase 2, T002-T004).
		const moveTool = tool(
			async ({ x, y }: { x: number; y: number }) => {
				if (x === 3 && y === 4) {
					sink.onMove("saolei_click", 3, 4, makeState());
				} else {
					sink.onMove("saolei_click", 5, 2, makeState());
					sink.onGameEnd(makeState(), "lost");
				}
				return `moved to (${x},${y})`;
			},
			{
				name: "fake_saolei_move",
				description: "Fake saolei move that accumulates a gameLog.",
				schema: z.object({ x: z.number(), y: z.number() }),
			},
		);
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 3, y: 4 } }])
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 5, y: 2 } }])
			.respond(new AIMessage("idle, no new game"));
		const plannerModel = updateStrategyPlannerModel("safer-play");
		const store = new FakeStrategyStore();
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			strategyStore: store,
			buffer,
			sessionId: "graph-test",
			playerTools: [moveTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-gamelog" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The review request (human message) renders every gameLog entry in
		// order — tool + coordinates + status + text board.
		const reviewRequests = result.plannerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				contentType(m).includes("本局游戏过程"),
		);
		expect(reviewRequests).toHaveLength(1);
		const text = contentType(reviewRequests[0] as BaseMessage);
		expect(text).toContain("1. saolei_click(3, 4) → playing");
		expect(text).toContain("2. saolei_click(5, 2) → playing");
		expect(text).toContain("3. (game-end) → lost");
		// Each step's board is text-rendered into the review input.
		expect(text).toContain("board size 3*3");
		// The review request ends with the update_strategy instruction.
		expect(text).toContain(
			"请复盘本局游戏表现，判断策略是否有效，若需要更新则调用 update_strategy。",
		);
		// The game really ended (lost) ⇒ the planner ran and wrote strategy.
		expect(await store.get("graph-test")).toBe("safer-play");
	});

	it("sends a notice review request when the gameLog is empty (US2 acceptance #6 / FR-009)", async () => {
		const buffer = createEphemeralGameBuffer();
		// An unconsumed game-end event WITHOUT any gameLog entry (an abnormal
		// session where no sink callback ever wrote a log): the planner still
		// runs, but its review input is the no-record notice — no empty
		// content, no crash.
		buffer.gameEvent = {
			state: makeState(),
			status: "lost",
			endedAt: Date.now(),
			consumed: false,
		};
		const playerModel = fakeModel().respond(new AIMessage("idle"));
		const plannerModel = fakeModel().respond(new AIMessage("no update"));
		const store = new FakeStrategyStore();
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			strategyStore: store,
			buffer,
			sessionId: "graph-test",
			playerTools: [],
			playerBasePrompt: "",
			plannerBasePrompt: "",
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-empty-log" }, recursionLimit: 50 },
		)) as TeamStateValue;

		const reviewRequests = result.plannerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				contentType(m).includes("请复盘本局游戏（无可用游戏记录）。"),
		);
		expect(reviewRequests).toHaveLength(1);
		expect(result.gameEnded).toBeNull();
	});
});

describe("team graph — US1 (037): planner review input real-time frame (FR-001/FR-004)", () => {
	it("emits the review input as a real-time frame with agent=planner when a game ends (FR-001/FR-002)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		// Two moves (still playing), then a losing game end — the sink
		// accumulates one gameLog entry per step, so the emitted frame
		// carries the full process (specs/037-saolei-team-optimize/spec.md
		// FR-002: live content == reloaded ListMessages content).
		const moveTool = tool(
			async ({ x, y }: { x: number; y: number }) => {
				if (x === 3 && y === 4) {
					sink.onMove("saolei_click", 3, 4, makeState());
				} else {
					sink.onMove("saolei_click", 5, 2, makeState());
					sink.onGameEnd(makeState(), "lost");
				}
				return `moved to (${x},${y})`;
			},
			{
				name: "fake_saolei_move",
				description: "Fake saolei move that accumulates a gameLog.",
				schema: z.object({ x: z.number(), y: z.number() }),
			},
		);
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 3, y: 4 } }])
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 5, y: 2 } }])
			.respond(new AIMessage("idle, no new game"));
		const plannerModel = updateStrategyPlannerModel("safer-play");
		const store = new FakeStrategyStore();
		// DI recording callback (style/javascript.md §测试 — vi.fn() seam,
		// no vi.mock): injected via LangGraph `configurable` (tasks.md 决策
		// #1 — specs/037-saolei-team-optimize/plan.md).
		const emitChannelFrame = vi.fn<(agent: string, content: string) => void>();
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			strategyStore: store,
			buffer,
			sessionId: "graph-test",
			playerTools: [moveTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{
				configurable: { thread_id: "t-us1-frame", emitChannelFrame },
				recursionLimit: 50,
			},
		)) as TeamStateValue;

		// The emitter was actually exercised (style/javascript.md §测试 —
		// positive assertion on the DI seam). One game end ⇒ one planner
		// run ⇒ exactly one review-input frame.
		expect(emitChannelFrame).toHaveBeenCalledTimes(1);
		const [emittedAgent, emittedContent] = emitChannelFrame.mock.calls[0];
		// The frame belongs to the planner tab (FR-001 / US1 AS5).
		expect(emittedAgent).toBe(PLANNER_AGENT_NAME);
		// The frame carries the FULL game process — every step's tool,
		// coordinates, status and text-rendered board (US1 AS2).
		expect(emittedContent).toContain("本局游戏过程");
		expect(emittedContent).toContain("1. saolei_click(3, 4) → playing");
		expect(emittedContent).toContain("2. saolei_click(5, 2) → playing");
		expect(emittedContent).toContain("3. (game-end) → lost");
		expect(emittedContent).toContain("board size 3*3");
		// The emitted content equals the review request written to the
		// planner channel (same buildReviewInput output — the live frame
		// and the reloaded history are identical, FR-002/FR-003).
		const reviewRequests = result.plannerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				contentType(m).includes("本局游戏过程"),
		);
		expect(reviewRequests).toHaveLength(1);
		expect(contentType(reviewRequests[0] as BaseMessage)).toBe(emittedContent);
		// The game really ended ⇒ the planner ran and wrote its strategy.
		expect(await store.get("graph-test")).toBe("safer-play");
	});

	it("emits the no-record notice frame when the gameLog is empty (FR-004)", async () => {
		const buffer = createEphemeralGameBuffer();
		// An unconsumed game-end event WITHOUT any gameLog entry: the planner
		// still runs, and its "无可用游戏记录" notice MUST be emitted too
		// (FR-004 — the empty-log notice is a non-empty review content).
		buffer.gameEvent = {
			state: makeState(),
			status: "lost",
			endedAt: Date.now(),
			consumed: false,
		};
		const playerModel = fakeModel().respond(new AIMessage("idle"));
		const plannerModel = fakeModel().respond(new AIMessage("no update"));
		const store = new FakeStrategyStore();
		const emitChannelFrame = vi.fn<(agent: string, content: string) => void>();
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			strategyStore: store,
			buffer,
			sessionId: "graph-test",
			playerTools: [],
			playerBasePrompt: "",
			plannerBasePrompt: "",
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{
				configurable: { thread_id: "t-us1-empty", emitChannelFrame },
				recursionLimit: 50,
			},
		)) as TeamStateValue;

		expect(emitChannelFrame).toHaveBeenCalledTimes(1);
		const [emittedAgent, emittedContent] = emitChannelFrame.mock.calls[0];
		expect(emittedAgent).toBe(PLANNER_AGENT_NAME);
		expect(emittedContent).toContain("请复盘本局游戏（无可用游戏记录）。");
		// Live frame content == the notice in the planner channel (FR-002).
		const notices = result.plannerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				contentType(m).includes("请复盘本局游戏（无可用游戏记录）。"),
		);
		expect(notices).toHaveLength(1);
		expect(contentType(notices[0] as BaseMessage)).toBe(emittedContent);
	});
});

describe("team graph — Issue 4 (036): inner createAgents inherit the outer graph config", () => {
	it("player's createAgent loop runs >25 model calls without GraphRecursionError (US4 acceptance #1 / SC-006)", async () => {
		const buffer = createEphemeralGameBuffer();
		// A move tool that NEVER ends the game (no sink calls): the player's
		// createAgent loop keeps iterating — the gameEndGuard middleware has
		// no end event to stop on.
		const moveTool = tool(
			async ({ x, y }: { x: number; y: number }) => {
				return `moved to (${x},${y}); still playing`;
			},
			{
				name: "fake_saolei_move",
				description: "Fake saolei move that never ends the game.",
				schema: z.object({ x: z.number(), y: z.number() }),
			},
		);
		// 26 tool-call responses + 1 idle = 27 model calls — the loop needs
		// ~53 inner super-steps, far beyond the createAgent default
		// recursionLimit of 25 (research.md D2). With the outer graph's
		// recursionLimit (1000) inherited via config (FR-013), the loop
		// completes; without it, the inner graph throws GraphRecursionError.
		// NOTE: plain `respond(AIMessage)` (fixed content) is used instead of
		// `respondWithTools` — the latter derives each AI message's content
		// from ALL prior messages (fakeModel `deriveContent`), which grows
		// exponentially across 27 calls (content doubles each step) and
		// exhausts the heap; real LLMs do not echo history into every
		// response, so a fixed-content tool-call response is the faithful
		// test double here.
		const playerModel = fakeModel();
		for (let i = 0; i < 26; i += 1) {
			playerModel.respond(
				new AIMessage({
					content: "move",
					tool_calls: [
						{ name: "fake_saolei_move", args: { x: i, y: i } },
					],
				}),
			);
		}
		playerModel.respond(new AIMessage("idle, stopping"));
		const store = new FakeStrategyStore();
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel: updateStrategyPlannerModel("never"),
			strategyStore: store,
			buffer,
			sessionId: "graph-test",
			playerTools: [moveTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{
				configurable: { thread_id: "t-recursion-player" },
				recursionLimit: 1000,
			},
		)) as TeamStateValue;

		// The inner loop inherited the outer recursionLimit: >25 model calls
		// completed (no GraphRecursionError from the inner createAgent).
		expect(playerModel.calls.length).toBeGreaterThan(25);
		// No game ended (the tool never fired the sink): no planner, END.
		expect(result.gameEnded).toBeNull();
		expect(result.plannerMessages).toEqual([]);
	});

	it("planner's createAgent loop runs >25 model calls in a single invoke, no retry (US4 acceptance #2)", async () => {
		// Wrap the real createAgent and count the PLANNER's invokes: with the
		// recursionLimit inherited, the >25-step loop completes on the FIRST
		// invoke; without it, the first invoke throws GraphRecursionError and
		// invokeWithRetry (planner.ts) retries — the count exposes the fix.
		let plannerInvokeCount = 0;
		const createAgentFn = vi.fn((config: { systemPrompt?: string }) => {
			const agent = createAgent(config as Parameters<typeof createAgent>[0]);
			if (config.systemPrompt?.includes(SKILL_PROMPT_SEPARATOR)) {
				return agent;
			}
			return {
				invoke: async (input: unknown, cfg?: unknown) => {
					plannerInvokeCount += 1;
					return agent.invoke(input, cfg);
				},
			};
		});
		// The player plays one game (move → won) so the conditional edge
		// routes to the planner; the planner then runs its own long loop.
		const playerModel = playOneGamePlayerModel();
		const plannerModel = fakeModel();
		// Same fixed-content tool-call responses as the player test above:
		// `respondWithTools` would grow each AI message's content
		// exponentially (fakeModel `deriveContent` echoes ALL prior messages
		// into every response) and blow the heap across 27 calls.
		for (let i = 0; i < 26; i += 1) {
			plannerModel.respond(
				new AIMessage({
					content: "review",
					tool_calls: [
						{
							name: "update_strategy",
							args: { content: `v${i + 1}` },
						},
					],
				}),
			);
		}
		plannerModel.respond(new AIMessage("review done"));
		const { graph, store } = buildTestGraph({
			playerModel,
			plannerModel,
			createAgentFn,
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{
				configurable: { thread_id: "t-recursion-planner" },
				recursionLimit: 1000,
			},
		)) as TeamStateValue;

		// The DI seam was exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
		// One single successful invoke — no GraphRecursionError, no retry.
		expect(plannerInvokeCount).toBe(1);
		expect(plannerModel.calls.length).toBeGreaterThan(25);
		expect(await store.get("graph-test")).toBe("v26");
		expect(result.gameEnded).toBeNull();
	});

	it("propagates the outer graph's abort signal into the inner createAgent invokes (US4 acceptance #3)", async () => {
		const controller = new AbortController();
		let invokesSeen = 0;
		const createAgentFn = vi.fn((config: { systemPrompt?: string }) => {
			return {
				invoke: async (
					_input: unknown,
					cfg?: { signal?: AbortSignal },
				) => {
					invokesSeen += 1;
					// FR-013: the config the node forwards to the inner
					// createAgent invoke MUST carry an abort signal derived
					// from the outer graph's — aborting the outer controller
					// aborts the signal the inner agent received. The abort
					// happens DURING the invoke: LangGraph disposes the
					// composed-signal listeners once a graph run finishes
					// (pregel runner `disposeCombinedSignal`), so propagation
					// is only observable while the node runs.
					expect(cfg?.signal).toBeDefined();
					if (cfg?.signal) {
						controller.abort();
						expect(cfg.signal.aborted).toBe(true);
					}
					return { messages: [] as BaseMessage[] };
				},
			};
		});
		const { graph, buffer } = buildTestGraph({ createAgentFn });
		// Pre-write an unconsumed game-end event so the turn also routes
		// through the planner node — both nodes' config forwarding (FR-013)
		// is exercised.
		const sink = createTeamSink(buffer);
		await sink.onGameEnd(makeState(), "lost");

		// Aborting mid-invoke interrupts the node execution (LangGraph races
		// node calls against the composed signal), which is exactly the
		// propagation the fix must deliver — without config forwarding the
		// graph run would complete normally instead.
		await expect(
			graph.invoke(
				{ playerMessages: [new HumanMessage("开始游戏")] },
				{
					configurable: { thread_id: "t-abort" },
					recursionLimit: 50,
					signal: controller.signal,
				},
			),
		).rejects.toThrow();
		expect(invokesSeen).toBeGreaterThan(0);
		expect(createAgentFn).toHaveBeenCalled();
		expect(controller.signal.aborted).toBe(true);
	});
});

describe("team graph — multi-game loop (FR-009)", () => {
	it("plays two games in one turn: planner fires once per game end and the strategy accumulates", async () => {
		// One move per game end (the gameEndGuard middleware stops the loop
		// right after each game end — the pre-fix "game N won" stop calls are
		// gone); the planner runs between games; the final idle ends the turn.
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 2, y: 2 } }])
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
				contentType(m).includes("本局游戏过程"),
		);
		expect(reviewRequests).toHaveLength(2);
		// Player: 3 model calls — one move per game end (the gameEndGuard
		// middleware stops the loop right after each game end) + a final
		// idle. The pre-fix "game N won" stop calls no longer happen.
		expect(playerModel.calls).toHaveLength(3);
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

describe("team graph — US2 (037): 5-game compression (FR-006..FR-015)", () => {
	it("compresses both channels into one summary after 5 ended games, player stops (FR-006/FR-008/FR-009/FR-010/FR-012)", async () => {
		// Mixed won AND lost outcomes — both count a game (FR-006). The tool
		// closes over the SAME buffer the graph uses (sink → buffer → node).
		const buffer = createEphemeralGameBuffer();
		const playerModel = fiveGamesPlayerModel("player 摘要内容");
		const plannerModel = fiveGamesPlannerModel("planner 摘要内容");
		const store = new FakeStrategyStore();
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			strategyStore: store,
			buffer,
			sessionId: "graph-test",
			playerTools: [buildMixedOutcomePlayerTool(buffer)],
			playerBasePrompt: "",
			plannerBasePrompt: "",
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-compress5" }, recursionLimit: 200 },
		)) as TeamStateValue;

		// FR-006: 5 ended games (won/lost) → counter 5.
		expect(result.gameCounter).toBe(5);
		// FR-008/FR-012: each channel shrank to ONE meaningful summary
		// AIMessage (non-blank content).
		expect(result.playerMessages).toHaveLength(1);
		expect(result.playerMessages[0]._getType()).toBe("ai");
		expect(contentType(result.playerMessages[0] as BaseMessage)).toBe(
			"player 摘要内容",
		);
		expect(result.plannerMessages).toHaveLength(1);
		expect(contentType(result.plannerMessages[0] as BaseMessage)).toBe(
			"planner 摘要内容",
		);
		// FR-009: the strategy (long-term memory) is untouched by compression
		// (the planner's last update wins).
		expect(await store.get("graph-test")).toBe("v5");
		// FR-010: the graph routed compress → END, NOT back to the player —
		// the player model was called 5× (one move per game) + 1× (the
		// compress summary); a 6th player run would consume a 7th response.
		expect(playerModel.callCount).toBe(6);
		// D6 step 6: gameEnded stays cleared after the turn.
		expect(result.gameEnded).toBeNull();
	});

	it("resumes game 6 with the summary context after compression (FR-010, US2 AS4)", async () => {
		const playerModel = fiveGamesPlayerModel("player 摘要内容");
		playerModel.respondWithTools([
			{ name: "fake_saolei_move", args: { x: 6, y: 6 } },
		]);
		playerModel.respond(new AIMessage("idle, no new game"));
		const plannerModel = fiveGamesPlannerModel("planner 摘要内容");
		plannerModel.respondWithTools([
			{ name: "update_strategy", args: { content: "v6" } },
		]);
		plannerModel.respond(new AIMessage("v6 written"));
		const { graph } = buildTestGraph({ playerModel, plannerModel });
		const thread = "t-resume";

		// Games 1-5 + compression (gameCounter 0 → 5, channels shrink).
		const compressed = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: thread }, recursionLimit: 200 },
		)) as TeamStateValue;
		expect(compressed.gameCounter).toBe(5);
		expect(compressed.playerMessages).toHaveLength(1);

		// Game 6: a new user input starts a new turn; the player resumes with
		// the summary as its (only) channel history.
		const result6 = (await graph.invoke(
			{ playerMessages: [new HumanMessage("继续")] },
			{ configurable: { thread_id: thread }, recursionLimit: 200 },
		)) as TeamStateValue;
		expect(result6.gameCounter).toBe(6);
		// The player's game-6 first model input carries the summary context
		// (calls[0..4] = moves 1-5, calls[5] = the compress summary call).
		const game6Input = playerModel.calls[6].messages as BaseMessage[];
		expect(
			game6Input.some((m) => contentType(m).includes("player 摘要内容")),
		).toBe(true);
	});

	it("aborts when a compression LLM call throws (FR-013)", async () => {
		const playerModel = fakeModel();
		for (let i = 0; i < 5; i += 1) {
			playerModel.respondWithTools([
				{ name: "fake_saolei_move", args: { x: i + 1, y: i + 1 } },
			]);
		}
		// The compress node's player-channel summary call (6th) throws.
		playerModel.respond(new Error("compression llm down"));
		const plannerModel = fiveGamesPlannerModel("unused");
		const { graph } = buildTestGraph({ playerModel, plannerModel });

		// FR-013: the node re-throws → the graph invoke rejects (abort).
		await expect(
			graph.invoke(
				{ playerMessages: [new HumanMessage("开始游戏")] },
				{ configurable: { thread_id: "t-compress-fail" }, recursionLimit: 200 },
			),
		).rejects.toThrow("compression llm down");
	});

	it("skips an empty channel at compression time (FR-015)", async () => {
		const playerModel = fiveGamesPlayerModel("player 摘要内容");
		const plannerModel = fakeModel();
		// Every planner invoke fails → the node degrades and writes NO
		// plannerMessages, so the planner channel is empty when compression
		// triggers (5 games × MAX_PLANNER_ATTEMPTS=3 = 15 error responses).
		for (let i = 0; i < 15; i += 1) {
			plannerModel.respond(new Error("planner llm down"));
		}
		const { graph } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-empty-channel" }, recursionLimit: 200 },
		)) as TeamStateValue;

		// 5 games still counted (the degrade path increments too, FR-006).
		expect(result.gameCounter).toBe(5);
		// Non-empty player channel: compressed to one summary.
		expect(result.playerMessages).toHaveLength(1);
		expect(contentType(result.playerMessages[0] as BaseMessage)).toBe(
			"player 摘要内容",
		);
		// Empty planner channel: skipped — no summary message written.
		expect(result.plannerMessages).toEqual([]);
		// The planner model was never invoked for a summary (15 = the 5
		// degraded planner runs' retries only).
		expect(plannerModel.callCount).toBe(15);
	});

	it("does not compress at a non-5 gameCounter (FR-006: MUST NOT trigger)", async () => {
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 2, y: 2 } }])
			.respond(new AIMessage("idle, no new game"));
		const plannerModel = fakeModel()
			.respondWithTools([{ name: "update_strategy", args: { content: "v1" } }])
			.respond(new AIMessage("v1 written"))
			.respondWithTools([{ name: "update_strategy", args: { content: "v2" } }])
			.respond(new AIMessage("v2 written"));
		const { graph, store } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-no-compress" }, recursionLimit: 200 },
		)) as TeamStateValue;

		// 2 ended games → counter 2 (not a 5-multiple) → no compression.
		expect(result.gameCounter).toBe(2);
		// The player channel still holds the raw history (length > 1).
		expect(result.playerMessages.length).toBeGreaterThan(1);
		// The compress node never ran: no summary calls (player: 2 moves + 1
		// idle; planner: 2 runs × 2 calls).
		expect(playerModel.callCount).toBe(3);
		expect(plannerModel.callCount).toBe(4);
		expect(await store.get("graph-test")).toBe("v2");
	});

	it("emits player+planner summary frames carrying the summary message id (FR-011/SC-004, data-model.md §4)", async () => {
		const playerModel = fiveGamesPlayerModel("player 摘要内容");
		const plannerModel = fiveGamesPlannerModel("planner 摘要内容");
		const emitChannelFrame = vi.fn<
			(agent: string, content: string, frameId?: string) => void
		>();
		const { graph } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{
				configurable: { thread_id: "t-compress-frame", emitChannelFrame },
				recursionLimit: 200,
			},
		)) as TeamStateValue;

		// 5 planner review-input frames (US1, agent="planner") + 2 summary
		// frames (player channel, planner channel) = 7.
		expect(emitChannelFrame).toHaveBeenCalledTimes(7);
		const calls = emitChannelFrame.mock.calls;
		for (let i = 0; i < 5; i += 1) {
			expect(calls[i][0]).toBe(PLANNER_AGENT_NAME);
		}
		// Player summary frame: agent + content match the summary model output
		// (FR-011), and the frameId equals the summary message's id — the
		// desktop dedup anchor (frameId == msg.id, data-model.md §4 / D9).
		const [playerAgent, playerContent, playerFrameId] = calls[5];
		expect(playerAgent).toBe(PLAYER_AGENT_NAME);
		expect(playerContent).toBe("player 摘要内容");
		expect(playerFrameId).toBeDefined();
		expect(playerFrameId).toBe(result.playerMessages[0].id);
		// Planner summary frame: same for the planner channel.
		const [plannerAgent, plannerContent, plannerFrameId] = calls[6];
		expect(plannerAgent).toBe(PLANNER_AGENT_NAME);
		expect(plannerContent).toBe("planner 摘要内容");
		expect(plannerFrameId).toBeDefined();
		expect(plannerFrameId).toBe(result.plannerMessages[0].id);
	});

	it("clears the compressed channels and resets gameCounter on RefreshTeam (FR-014 / US2 AS8)", async () => {
		const playerModel = fiveGamesPlayerModel("player 摘要内容");
		const plannerModel = fiveGamesPlannerModel("planner 摘要内容");
		const { graph, store, sessionId } = buildTestGraph({
			playerModel,
			plannerModel,
		});
		// The invoke runs on the session thread (thread_id == sessionId) so
		// refreshTeamChannels(graph, sessionId) targets the same thread.
		await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: sessionId }, recursionLimit: 200 },
		);

		await refreshTeamChannels(graph, sessionId);

		const snapshot = (await graph.getState({
			configurable: { thread_id: sessionId },
		})) as unknown as { values: TeamStateValue };
		// The summaries (short-term messages) are cleared alongside the rest.
		expect(snapshot.values.playerMessages).toEqual([]);
		expect(snapshot.values.plannerMessages).toEqual([]);
		expect(snapshot.values.gameCounter).toBe(0);
		// The strategy (long-term memory) is untouched (FR-014).
		expect(await store.get(sessionId)).toBe("v5");
	});
});
