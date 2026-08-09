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

import { appendSkillBodyToPrompt, loadSkillBody, SKILL_PROMPT_SEPARATOR } from "../skill-loader";
import { refreshTeamChannels } from "../context-middleware";
import type { MemoryClient } from "../memory-client";
import type { GameStats } from "../mcp/saolei/saolei-mcp";
import {
	createEphemeralGameBuffer,
	createTeamSink,
	type EphemeralGameBuffer,
} from "./team-sink";
import { buildTeamGraph, SAOLEI_TEAM_AGENTS } from "./graph";
import {
	FrozenMemorySnapshot,
	PLANNER_MEMORY_SNAPSHOT_ID,
} from "./memory-snapshot";
import { createPlayerNode, DEFAULT_PLAYER_BASE, PLAYER_AGENT_NAME } from "./player";
import type { CreateAgentFn } from "./player";
import { DEFAULT_PLANNER_BASE, PLANNER_AGENT_NAME } from "./planner";
import type { TeamStateValue } from "./state";

/** The built-in skill bodies (loaded from the test runfiles — skill-loader). */
const SAOLEI_SKILL_BODY = loadSkillBody("saolei");
const MEMORY_SKILL_BODY = loadSkillBody("memory");

/**
 * Prompt heuristic: the player's static systemPrompt carries the saolei
 * skill body (appendSkillBodyToPrompt(base, ["saolei"]), FR-034); the
 * planner's carries the memory skill body (FR-020 — 039 US2). Both contain
 * SKILL_PROMPT_SEPARATOR since 039 Phase 5, so the pre-039 "planner prompt
 * lacks the separator" heuristic no longer distinguishes them.
 */
function isPlayerPrompt(p: string): boolean {
	return p.includes(SAOLEI_SKILL_BODY);
}
function isPlannerPrompt(p: string): boolean {
	return p.includes(MEMORY_SKILL_BODY);
}

/**
 * Minimal fake MemoryClient (DI seam — `style/javascript.md` §测试: injected,
 * no `vi.mock`). `listMemories` returns the given entries; the fake is a
 * structural stand-in for the real gRPC client.
 */
function fakeMemoryClient(
	entries: Array<{ memory_id: string; content: string }> = [],
): MemoryClient {
	return {
		listMemories: vi.fn(async () => entries.map((e) => ({ ...e }))),
	} as unknown as MemoryClient;
}

/**
 * The fake planner memory tool (hermes-style single `memory` tool — FR-008):
 * production gets it via the mcp client (buildMemoryMcpTools); tests inject
 * this structural stand-in so the wiring (tool set + system prompt) is
 * exercised without an MCP server.
 */
function buildFakeMemoryTool(): StructuredToolInterface {
	return tool(
		async () => "memory ok",
		{
			name: "memory",
			description: "Manage the planner's long-term review memory (hermes-style single tool).",
			schema: z.object({
				action: z.enum(["add", "replace", "remove"]).optional(),
				content: z.string().optional(),
				old_text: z.string().optional(),
			}),
		},
	);
}

/**
 * Default 039 memory data-plane deps (T019) for inline `buildTeamGraph` call
 * sites that do not exercise the memory wiring themselves: a fake
 * MemoryClient, a fresh (empty) frozen snapshot, the template path segment,
 * and NO planner tools (the `instruct_player` tool is always built
 * internally — T027, Phase 6).
 */
function memoryDeps(
	overrides: {
		memoryClient?: MemoryClient;
		frozenSnapshot?: FrozenMemorySnapshot;
		template?: string;
		plannerTools?: StructuredToolInterface[];
	} = {},
) {
	return {
		memoryClient: overrides.memoryClient ?? fakeMemoryClient(),
		frozenSnapshot: overrides.frozenSnapshot ?? new FrozenMemorySnapshot(),
		template: overrides.template ?? "saolei",
		plannerTools: overrides.plannerTools ?? [],
	};
}

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

/**
 * The fake player tool that ends the game carrying per-game stats (037 US5 —
 * the MCP's onGameEnd third argument, FR-030/FR-031): the team sink stores
 * them into `buffer.gameEvent.stats`, which the planner's review input
 * renders (FR-032). A real operate (sink.onOperate) precedes the game end so
 * the gameLog holds an actual game-process entry — the stats-section ordering
 * assertion in the US5 test compares against a present entry instead of a
 * vacuous -1 (Phase 6 review fix).
 */
function buildStatsPlayerTool(buffer: EphemeralGameBuffer, stats: GameStats) {
	const sink = createTeamSink(buffer);
	return tool(
		async ({ x, y }: { x: number; y: number }) => {
			await sink.onOperate([{ type: "click", x, y }], makeState());
			await sink.onGameEnd(makeState(), "lost", stats);
			return `moved to (${x},${y}); game lost`;
		},
		{
			name: "fake_saolei_move",
			description: "Fake saolei move that ends the game with stats.",
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

/**
 * The planner's fake model for a "review + send calibration instruction"
 * flow (039 US3, T027 — the review MAY call `instruct_player`, FR-014/
 * FR-017; the tool stages the content into the R1 external buffer and the
 * node appends it to playerMessages from its return value).
 */
function instructPlayerPlannerModel(content: string) {
	return fakeModel()
		.respondWithTools([{ name: "instruct_player", args: { content } }])
		.respond(new AIMessage("instruction sent"));
}

/** Common graph wiring shared by most tests. */
function buildTestGraph(
	overrides: {
		playerModel?: ReturnType<typeof playOneGamePlayerModel>;
		plannerModel?: ReturnType<typeof instructPlayerPlannerModel>;
		sessionId?: string;
		playerBasePrompt?: string;
		plannerBasePrompt?: string;
		createAgentFn?: CreateAgentFn;
		playerTools?: StructuredToolInterface[];
		// 039 Phase 5 (T019/T020): memory data-plane deps — injected fakes by
		// default; tests override to assert snapshot/skill wiring.
		memoryClient?: MemoryClient;
		frozenSnapshot?: FrozenMemorySnapshot;
		template?: string;
		plannerTools?: StructuredToolInterface[];
	} = {},
) {
	const buffer = createEphemeralGameBuffer();
	const sessionId = overrides.sessionId ?? "graph-test";
	const memoryClient =
		overrides.memoryClient ?? fakeMemoryClient();
	const frozenSnapshot =
		overrides.frozenSnapshot ?? new FrozenMemorySnapshot();
	const { graph, checkpointer } = buildTeamGraph({
		playerModel: overrides.playerModel ?? playOneGamePlayerModel(),
		plannerModel:
			overrides.plannerModel ?? instructPlayerPlannerModel("corner-first"),
		memoryClient,
		frozenSnapshot,
		template: overrides.template ?? "saolei",
		buffer,
		sessionId,
		playerTools:
			overrides.playerTools ?? [buildGameEndingPlayerTool(buffer)],
		plannerTools:
			overrides.plannerTools ?? [buildFakeMemoryTool()],
		playerBasePrompt: overrides.playerBasePrompt ?? "",
		plannerBasePrompt: overrides.plannerBasePrompt ?? "",
		createAgentFn: overrides.createAgentFn,
	});
	return { graph, checkpointer, buffer, sessionId, memoryClient, frozenSnapshot };
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
 * `instruct_player` tool call (the review MAY send a calibration instruction
 * — FR-014/FR-017) + a plain response (2 model calls per planner run), then
 * the compress node's planner-channel summary call returns `summaryContent`.
 */
function fiveGamesPlannerModel(summaryContent: string) {
	const model = fakeModel();
	for (let i = 0; i < 5; i += 1) {
		model.respondWithTools([
			{ name: "instruct_player", args: { content: `v${i + 1}` } },
		]);
		model.respond(new AIMessage(`v${i + 1} sent`));
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
	it("routes player→planner on game end, planner sends a calibration instruction into playerMessages and clears gameEnded, then returns to player (D6 + FR-017)", async () => {
		const playerModel = playOneGamePlayerModel();
		const plannerModel = instructPlayerPlannerModel("corner-first");
		const { graph } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{
				configurable: {
					thread_id: "t-flow",
					// R1 external buffer (contract §4) — the review's
					// instruct_player stages its content here.
					instructionBuffer: { content: null },
				},
				recursionLimit: 50,
			},
		)) as TeamStateValue;

		// D14 注意事项 3: the FINAL gameEnded is null (cleared by the planner).
		expect(result.gameEnded).toBeNull();

		// The planner RAN (game ended ⇒ routed to planner exactly once):
		// its channel carries the review request + AI + tool messages.
		expect(result.plannerMessages.length).toBeGreaterThan(0);

		// The review sent a calibration instruction into the player channel
		// (FR-017 — the instruction HumanMessage lands after the game-ending
		// tool_result; the planner's review itself stays in plannerMessages).
		const instruction = result.playerMessages.find(
			(m) =>
				typeof m.content === "string" && m.content.includes("corner-first"),
		);
		expect(instruction).toBeInstanceOf(HumanMessage);
		// planner→player edge: the player ran AGAIN after the planner
		// (idle — no new game ⇒ gameEnded stays null ⇒ END). The player
		// model was called 2 times: move / idle — the gameEndGuard
		// middleware stops the loop right after the game end, so the
		// pre-fix "stop" call no longer happens.
		expect(playerModel.calls).toHaveLength(2);
		expect(result.playerMessages.length).toBeGreaterThan(0);

		// No strategy message ever enters the channel (Phase 6 — the
		// shared-strategy injection path is gone, FR-013): the channel holds
		// NO system message. (Note: fakeModel derives tool-call message
		// content from the model input, so text may appear inside AI content
		// — the channel SHAPE, not the text, is the contract.)
		for (const m of result.playerMessages) {
			expect(m._getType()).not.toBe("system");
		}
	});

	it("routes player→END when no game ended (planner never runs)", async () => {
		const playerModel = fakeModel().respond(new AIMessage("just chatting"));
		const plannerModel = instructPlayerPlannerModel("should-not-run");
		const { graph } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("你好")] },
			{ configurable: { thread_id: "t-noend" }, recursionLimit: 50 },
		)) as TeamStateValue;

		expect(result.gameEnded).toBeNull();
		// Planner not triggered: no review messages, no strategy write.
		expect(result.plannerMessages).toEqual([]);
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

	it("delivers a calibration instruction already in playerMessages to the player's first activation (039 US3 — channel history flow, contract §2.1)", async () => {
		// The initInstruction turn wrote the instruction DIRECTLY into
		// `playerMessages`; the player's first activation reads it as plain
		// channel history — no pending slot (FR-015/FR-016).
		const playerModel = playOneGamePlayerModel();
		const { graph } = buildTestGraph({ playerModel });

		const result = (await graph.invoke(
			{
				playerMessages: [
					new HumanMessage("开局先点中心"),
					new HumanMessage("开始游戏"),
				],
			},
			{ configurable: { thread_id: "t-pending" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The instruction led the player's FIRST model input (the createAgent
		// prepends its system prompt, so the instruction is the first
		// non-system message).
		const firstCall = playerModel.calls[0]?.messages as BaseMessage[];
		expect(firstCall).toBeDefined();
		const instructionMsg = firstCall.find(
			(m) => m._getType() !== "system" && contentType(m).includes("开局先点中心"),
		);
		expect(instructionMsg).toBeInstanceOf(HumanMessage);
		// The instruction is part of the player channel history (D6 —
		// 累积可引用) and precedes the user input.
		expect(
			result.playerMessages.some(
				(m) =>
					typeof m.content === "string" &&
					m.content.includes("开局先点中心"),
			),
		).toBe(true);
	});

	it("no longer injects the strategy into the planner's system context — replaced by the frozen snapshot (Phase 5, T020; Phase 6 removes the last strategy path)", async () => {
		const plannerModel = instructPlayerPlannerModel("corner-first");
		const { graph } = buildTestGraph({ plannerModel });

		await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-planner-strategy" }, recursionLimit: 50 },
		);

		const firstCall = plannerModel.calls[0]?.messages as BaseMessage[];
		expect(firstCall).toBeDefined();
		// T020/T027: the strategy READ path (the old "当前态势" SystemMessage
		// injection, FR-014) is gone — no "当前策略"/strategy-text SystemMessage
		// reaches the model (the whole shared-strategy path was removed in
		// Phase 6, FR-013).
		const strategyMsg = firstCall.find(
			(m) => m._getType() === "system" && contentType(m).includes("corner-first"),
		);
		expect(strategyMsg).toBeUndefined();
		expect(
			firstCall.some(
				(m) => m._getType() === "system" && contentType(m).includes("当前策略"),
			),
		).toBe(false);
		// The frozen snapshot SystemMessage is injected instead (FR-011,
		// contract §3).
		expect(firstCall.some((m) => m.id === PLANNER_MEMORY_SNAPSHOT_ID)).toBe(true);
		// The review input (full gameLog rendering) is present as the
		// planner's prompt (Issue 2 — `specs/036-team-mode-bugfix/
		// contracts/team-graph-fix-contract.md` §2.2).
		expect(
			firstCall.some((m) => contentType(m).includes("本局游戏过程")),
		).toBe(true);
	});

	it("planner system context starts with the EMPTY frozen snapshot on a fresh session (T020 — the snapshot replaces the strategy)", async () => {
		const plannerModel = instructPlayerPlannerModel("first-strategy");
		const { graph } = buildTestGraph({ plannerModel });

		await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-empty-strategy" }, recursionLimit: 50 },
		);

		const firstCall = plannerModel.calls[0]?.messages as BaseMessage[];
		// The default (unbaked) snapshot renders the header-only
		// `长期记忆：` SystemMessage with the fixed snapshot id (FR-011).
		const snapshotMsg = firstCall.find(
			(m) => m.id === PLANNER_MEMORY_SNAPSHOT_ID,
		);
		expect(snapshotMsg).toBeDefined();
		expect(contentType(snapshotMsg as BaseMessage)).toContain("长期记忆");
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
		const plannerModel = instructPlayerPlannerModel("safer-play");
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [losingMoveTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-lost" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The planner RAN (game ended ⇒ routed exactly once): its channel
		// carries the review request and the strategy was written (FR-013).
		expect(result.plannerMessages.length).toBeGreaterThan(0);
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
		const node = createPlayerNode({
			model: playerModel,
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
		// (FR-034); the planner's carries the memory skill body (FR-020) —
		// same dispatch heuristic as captureSystemPrompts.
		const createAgentFn = vi.fn((config: { systemPrompt?: string }) => {
			if (isPlayerPrompt(config.systemPrompt ?? "")) {
				return {
					invoke: async () => {
						throw new Error("player agent loop crashed");
					},
				};
			}
			return createAgent(config as Parameters<typeof createAgent>[0]);
		});
		const plannerModel = instructPlayerPlannerModel("post-crash-strategy");
		const { graph } = buildTeamGraph({
			playerModel: fakeModel().respond(new AIMessage("unused")),
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
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
		expect(result.gameEnded).toBeNull();
	});
});

describe("team graph — Issue 2 (036): planner review input renders the full gameLog", () => {
	it("renders every gameLog entry — tool, coordinates, status and board — in the review input (US2 acceptance #1-4)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		// Two operates (still playing), then a losing game end — the sink
		// accumulates one gameLog entry per operate call (each carrying its
		// full operations list, FR-004 — Phase 2, T005/T006).
		const moveTool = tool(
			async ({ x, y }: { x: number; y: number }) => {
				if (x === 3 && y === 4) {
					sink.onOperate([{ type: "click", x: 3, y: 4 }], makeState());
				} else {
					sink.onOperate([{ type: "flag", x: 5, y: 2 }], makeState());
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
		const plannerModel = instructPlayerPlannerModel("safer-play");
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [moveTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-gamelog" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The review request (human message) renders every gameLog entry in
		// order — tool + operations + status + text board (FR-004: one
		// entry per saolei_operate call, rendering `saolei_operate(ops) →
		// status`).
		const reviewRequests = result.plannerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				contentType(m).includes("本局游戏过程"),
		);
		expect(reviewRequests).toHaveLength(1);
		const text = contentType(reviewRequests[0] as BaseMessage);
		expect(text).toContain("1. saolei_operate(click(3,4)) → playing");
		expect(text).toContain("2. saolei_operate(flag(5,2)) → playing");
		expect(text).toContain("3. (game-end) → lost");
		// Each step's board is text-rendered into the review input.
		expect(text).toContain("board size 3*3");
		// The review request ends with the "必要时才调用" instruction
		// (FR-014 — the calibration instruction is OPTIONAL, decided by the
		// planner LLM).
		expect(text).toContain(
			"请复盘本局游戏表现，判断策略是否有效；若你认为需要给 player 校准指令，",
		);
		expect(text).toContain("仅在必要时调用 instruct_player 发送指令。");
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
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
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

describe("team graph — US5 (037): planner review input renders game stats (FR-032)", () => {
	it("renders the stats section when gameEvent.stats is present (FR-032)", async () => {
		const buffer = createEphemeralGameBuffer();
		const stats: GameStats = {
			operationCount: 7,
			correctFlags: 3,
			avgOpsPerMine: 2.33,
		};
		const statsTool = buildStatsPlayerTool(buffer, stats);
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respond(new AIMessage("idle, no new game"));
		const plannerModel = instructPlayerPlannerModel("safer-play");
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [statsTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-stats" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The review request carries the full gameLog AND the stats section
		// (contracts/game-stats-contract.md §5): the stats appear after the
		// game-process lines and before the review instruction.
		const reviewRequests = result.plannerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				contentType(m).includes("本局游戏过程"),
		);
		expect(reviewRequests).toHaveLength(1);
		const text = contentType(reviewRequests[0] as BaseMessage);
		expect(text).toContain("本局统计数据：");
		expect(text).toContain("- 操作次数：7");
		expect(text).toContain("- 正确标记地雷数：3");
		expect(text).toContain("- 每雷平均操作数：2.33");
		expect(text).toContain(
			"请复盘本局游戏表现，判断策略是否有效；若你认为需要给 player 校准指令，",
		);
		// The game-process lines come from the sink-written gameLog (the
		// operate from onOperate, then the onGameEnd entry) — asserted
		// present so the ordering comparison below is not vacuous (Phase 6
		// review fix: the stats section must be verified AFTER a real
		// gameLog line).
		expect(text).toContain("1. saolei_operate");
		expect(text).toContain("2. (game-end)");
		// The stats section sits AFTER the game-process lines.
		expect(text.indexOf("本局统计数据：")).toBeGreaterThan(
			text.indexOf("2. (game-end)"),
		);
	});

	it("omits the stats section when gameEvent.stats is absent (backward compatible)", async () => {
		const buffer = createEphemeralGameBuffer();
		// The existing game-ending tool fires onGameEnd WITHOUT stats.
		const sink = createTeamSink(buffer);
		const plainTool = tool(
			async ({ x, y }: { x: number; y: number }) => {
				await sink.onGameEnd(makeState(), "lost");
				return `moved to (${x},${y}); game lost`;
			},
			{
				name: "fake_saolei_move",
				description: "Fake saolei move that ends the game (no stats).",
				schema: z.object({ x: z.number(), y: z.number() }),
			},
		);
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respond(new AIMessage("idle, no new game"));
		const plannerModel = instructPlayerPlannerModel("safer-play");
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [plainTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-no-stats" }, recursionLimit: 50 },
		)) as TeamStateValue;

		const reviewRequests = result.plannerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				contentType(m).includes("本局游戏过程"),
		);
		expect(reviewRequests).toHaveLength(1);
		const text = contentType(reviewRequests[0] as BaseMessage);
		expect(text).not.toContain("本局统计数据：");
		expect(text).not.toContain("操作次数");
	});

	it("renders '不可用' for a null correctFlags and 'N/A' for avgOpsPerMine (FR-032/FR-033)", async () => {
		const buffer = createEphemeralGameBuffer();
		// Undecodable init counter ⇒ correctFlags = null (FR-033): the
		// review input must degrade gracefully, not crash.
		const stats: GameStats = {
			operationCount: 5,
			correctFlags: null,
			avgOpsPerMine: "N/A",
		};
		const statsTool = buildStatsPlayerTool(buffer, stats);
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respond(new AIMessage("idle, no new game"));
		const plannerModel = instructPlayerPlannerModel("safer-play");
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [statsTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-null-stats" }, recursionLimit: 50 },
		)) as TeamStateValue;

		const reviewRequests = result.plannerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				contentType(m).includes("本局游戏过程"),
		);
		expect(reviewRequests).toHaveLength(1);
		const text = contentType(reviewRequests[0] as BaseMessage);
		expect(text).toContain("本局统计数据：");
		expect(text).toContain("- 操作次数：5");
		expect(text).toContain("- 正确标记地雷数：不可用");
		expect(text).toContain("- 每雷平均操作数：N/A");
	});
});

describe("team graph — US1 (037): planner review input real-time frame (FR-001/FR-004)", () => {
	it("emits the review input as a real-time frame with agent=planner when a game ends (FR-001/FR-002)", async () => {
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		// Two operates (still playing), then a losing game end — the sink
		// accumulates one gameLog entry per operate call, so the emitted frame
		// carries the full process (specs/037-saolei-team-optimize/spec.md
		// FR-002: live content == reloaded ListMessages content).
		const moveTool = tool(
			async ({ x, y }: { x: number; y: number }) => {
				if (x === 3 && y === 4) {
					sink.onOperate([{ type: "click", x: 3, y: 4 }], makeState());
				} else {
					sink.onOperate([{ type: "flag", x: 5, y: 2 }], makeState());
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
		const plannerModel = instructPlayerPlannerModel("safer-play");
		// DI recording callback (style/javascript.md §测试 — vi.fn() seam,
		// no vi.mock): injected via LangGraph `configurable` (tasks.md 决策
		// #1 — specs/037-saolei-team-optimize/plan.md).
		const emitChannelFrame = vi.fn<(agent: string, content: string) => void>();
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [moveTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
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
		// operations, status and text-rendered board (US1 AS2).
		expect(emittedContent).toContain("本局游戏过程");
		expect(emittedContent).toContain("1. saolei_operate(click(3,4)) → playing");
		expect(emittedContent).toContain("2. saolei_operate(flag(5,2)) → playing");
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
		const emitChannelFrame = vi.fn<(agent: string, content: string) => void>();
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
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
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel: instructPlayerPlannerModel("never"),
			buffer,
			sessionId: "graph-test",
			playerTools: [moveTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
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
			// The PLAYER's prompt carries the saolei skill body; the planner's
			// carries the memory skill body — both contain the separator since
			// 039 Phase 5, so the player is identified by its own skill body.
			if (isPlayerPrompt(config.systemPrompt ?? "")) {
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
							name: "instruct_player",
							args: { content: `v${i + 1}` },
						},
					],
				}),
			);
		}
		plannerModel.respond(new AIMessage("review done"));
		const { graph } = buildTestGraph({
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
	it("plays two games in one turn: planner fires once per game end and each review instruction lands in the player channel", async () => {
		// One move per game end (the gameEndGuard middleware stops the loop
		// right after each game end — the pre-fix "game N won" stop calls are
		// gone); the planner runs between games; the final idle ends the turn.
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 2, y: 2 } }])
			.respond(new AIMessage("idle, stopping"));
		const plannerModel = fakeModel()
			.respondWithTools([
				{ name: "instruct_player", args: { content: "v1" } },
			])
			.respond(new AIMessage("v1 sent"))
			.respondWithTools([
				{ name: "instruct_player", args: { content: "v2" } },
			])
			.respond(new AIMessage("v2 sent"));

		const { graph } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{
				configurable: {
					thread_id: "t-multi",
					instructionBuffer: { content: null },
				},
				recursionLimit: 100,
			},
		)) as TeamStateValue;

		// Planner ran twice (once per game end); both reviews sent a
		// calibration instruction into the player channel (FR-017 — they
		// accumulate in the player's conversation flow, D6). Count HUMAN
		// instructions only (fakeModel echoes input text into AI content, so
		// an AI message may contain the instruction text too).
		expect(result.gameEnded).toBeNull();
		const instructionCount = result.playerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				typeof m.content === "string" &&
				(m.content.includes("v1") || m.content.includes("v2")),
		).length;
		expect(instructionCount).toBe(2);
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
		const { graph } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-degrade" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The graph completed; gameEnded cleared despite the failure
		// (unconditional clear, D6 step 6 — no infinite planner re-trigger).
		expect(result.gameEnded).toBeNull();
	});
});

describe("team graph — strategy injection edge cases", () => {
	it("holds NO strategy/system messages in the player channel (Phase 6: shared strategy removed — FR-013, SC-005)", async () => {
		const playerModel = playOneGamePlayerModel();
		const { graph } = buildTestGraph({ playerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-no-state-strategy" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The player channel holds no strategy "当前态势" SystemMessage —
		// the strategy injection path is gone (Phase 6). The ONLY system
		// messages anywhere are the createAgent system prompt (not in the
		// channel); the channel shape is the contract.
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
		const plannerModel = instructPlayerPlannerModel("never");
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [failingTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
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
	});
});

/**
 * Spy createAgentFn (DI seam) capturing the `systemPrompt` of each node's
 * agent. The player's prompt always carries the appended saolei skill body;
 * the planner's carries the memory skill body (FR-020 — 039 US2) — the two
 * skill bodies distinguish the calls without depending on build order (both
 * prompts contain SKILL_PROMPT_SEPARATOR since 039 Phase 5).
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
		playerSystemPrompt: () => calls.find(isPlayerPrompt) ?? "",
		plannerSystemPrompt: () => calls.find(isPlannerPrompt) ?? "",
	};
}

describe("player/planner base prompts from the TeamProfile (FR-034 semantics A)", () => {

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
		// The default test player tool (fake_saolei_move) is NOT game-visible
		// (GAME_VISIBLE_PLAYER_TOOLS), so it would not be listed — pass a real
		// visible tool to exercise the US3 section alongside FR-034.
		// `saolei_operate`'s description covers its click/flag/chord operation
		// types (FR-005 — Phase 2 US1).
		const operateTool = tool(
			async () => "operated",
			{
				name: "saolei_operate",
				description: "执行落子操作，支持 click/flag/chord。",
				schema: z.object({
					operations: z
						.array(
							z.object({
								type: z.enum(["click", "flag", "chord"]),
								x: z.number(),
								y: z.number(),
							}),
						)
						.optional(),
					type: z.enum(["click", "flag", "chord"]).optional(),
					x: z.number().optional(),
					y: z.number().optional(),
				}),
			},
		);
		const { createAgentFn, plannerSystemPrompt } = captureSystemPrompts();
		buildTestGraph({
			plannerBasePrompt: profilePrompt,
			playerTools: [operateTool],
			createAgentFn,
		});

		// Semantics A: the profile prompt leads, unchanged (FR-034); the
		// memory skill body is appended on top (FR-020); US3 appends the
		// player tool description section AFTER the skill body (FR-016 —
		// specs/037-saolei-team-optimize/contracts/compression-contract.md
		// §4). The game-visible player tool is listed.
		expect(plannerSystemPrompt().startsWith(profilePrompt)).toBe(true);
		// The planner DOES append the memory skill body (FR-020 — 039 US2).
		expect(plannerSystemPrompt()).toContain(SKILL_PROMPT_SEPARATOR);
		expect(plannerSystemPrompt()).toContain(MEMORY_SKILL_BODY);
		expect(plannerSystemPrompt()).toContain("## Player 可用工具");
		expect(plannerSystemPrompt()).toContain(
			"saolei_operate: 执行落子操作，支持 click/flag/chord。",
		);
		expect(createAgentFn).toHaveBeenCalled();
	});

	it("planner: empty planner_prompt falls back to DEFAULT_PLANNER_BASE (FR-034)", () => {
		// A game-visible player tool so the US3 section is present (the
		// default fake_saolei_move test tool is not game-visible).
		const operateTool = tool(
			async () => "operated",
			{
				name: "saolei_operate",
				description: "执行落子操作，支持 click/flag/chord。",
				schema: z.object({
					operations: z
						.array(
							z.object({
								type: z.enum(["click", "flag", "chord"]),
								x: z.number(),
								y: z.number(),
							}),
						)
						.optional(),
					type: z.enum(["click", "flag", "chord"]).optional(),
					x: z.number().optional(),
					y: z.number().optional(),
				}),
			},
		);
		const { createAgentFn, plannerSystemPrompt } = captureSystemPrompts();
		buildTestGraph({ playerTools: [operateTool], createAgentFn }); // plannerBasePrompt defaults to ""

		// The default base leads (FR-034); the memory skill body follows
		// (FR-020); the US3 tool description section comes after the skill
		// body (FR-016 — compression-contract.md §4).
		expect(plannerSystemPrompt().startsWith(DEFAULT_PLANNER_BASE)).toBe(true);
		expect(plannerSystemPrompt()).toContain(SKILL_PROMPT_SEPARATOR);
		expect(plannerSystemPrompt()).toContain(MEMORY_SKILL_BODY);
		expect(plannerSystemPrompt()).toContain("## Player 可用工具");
		expect(createAgentFn).toHaveBeenCalled();
	});

	it("planner: empty player tool set appends NO tool section (US3 — no trailing markdown)", () => {
		const { createAgentFn, plannerSystemPrompt } = captureSystemPrompts();
		buildTestGraph({ playerTools: [], createAgentFn });

		// The spy was actually exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
		// Empty player tools ⇒ buildToolDescriptionSection returns "" — the
		// planner prompt is DEFAULT_PLANNER_BASE + the always-appended memory
		// skill body (FR-020), with no trailing markdown section
		// (compression-contract.md §4).
		expect(plannerSystemPrompt()).toBe(
			appendSkillBodyToPrompt(DEFAULT_PLANNER_BASE, ["memory"]),
		);
	});
});

describe("team graph — US3 (037): planner systemPrompt player tool descriptions (FR-016..FR-018)", () => {
	it("injects every game-visible player tool's name+description into the planner systemPrompt while keeping its tool set at memory + instruct_player only (FR-016/FR-018)", () => {
		// Both game-visible player tools (Phase 2 US1: the cell tools are
		// merged into saolei_operate — FR-001). `saolei_operate`'s
		// description carries the click/flag/chord operation types (FR-005),
		// so the injected section documents them for the planner.
		const operateTool = tool(
			async () => "operated",
			{
				name: "saolei_operate",
				description: "执行落子操作，支持 click/flag/chord。",
				schema: z.object({
					operations: z
						.array(
							z.object({
								type: z.enum(["click", "flag", "chord"]),
								x: z.number(),
								y: z.number(),
							}),
						)
						.optional(),
					type: z.enum(["click", "flag", "chord"]).optional(),
					x: z.number().optional(),
					y: z.number().optional(),
				}),
			},
		);
		const initTool = tool(
			async () => "started",
			{
				name: "saolei_init",
				description: "开始一局新游戏。",
				schema: z.object({}),
			},
		);
		// DI spy (style/javascript.md §测试 — no vi.mock): capture BOTH the
		// systemPrompt AND the tools array of each createAgent call, then
		// pick the planner's by its prompt carrying the memory skill body
		// (the player's carries the saolei skill body — same heuristic as
		// captureSystemPrompts).
		const calls: Array<{
			systemPrompt: string;
			tools: StructuredToolInterface[];
		}> = [];
		const createAgentFn = vi.fn(
			(config: { systemPrompt?: string; tools?: StructuredToolInterface[] }) => {
				calls.push({
					systemPrompt: config.systemPrompt ?? "",
					tools: config.tools ?? [],
				});
				return { invoke: async () => ({ messages: [] as BaseMessage[] }) };
			},
		);
		buildTestGraph({ playerTools: [operateTool, initTool], createAgentFn });

		// The spy was actually exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
		const plannerCall = calls.find((c) => isPlannerPrompt(c.systemPrompt));
		expect(plannerCall).toBeDefined();
		// FR-016: the section lists EVERY game-visible player tool's name and
		// description (specs/037-saolei-team-optimize/contracts/
		// compression-contract.md §4 — `- name: description` per tool).
		expect(plannerCall?.systemPrompt).toContain("## Player 可用工具");
		expect(plannerCall?.systemPrompt).toContain(
			"saolei_operate: 执行落子操作，支持 click/flag/chord。",
		);
		expect(plannerCall?.systemPrompt).toContain(
			"saolei_init: 开始一局新游戏。",
		);
		// FR-018: the player tools were NOT added as callable tools — the
		// planner's ACTUAL tool set is the memory tool (039 US2, FR-007/008 —
		// the injected fake from buildTestGraph's default plannerTools) plus
		// the internal `instruct_player` calibration tool (039 US3, T027).
		// The shared-strategy write path is GONE (Phase 6, FR-013).
		expect(plannerCall?.tools.map((t) => t.name)).toEqual([
			"memory",
			"instruct_player",
		]);
	});

	it("excludes read-only player tools the planner cannot observe in the game process (saolei_remain — FR-016 refine)", () => {
		const operateTool = tool(
			async () => "operated",
			{
				name: "saolei_operate",
				description: "执行落子操作，支持 click/flag/chord。",
				schema: z.object({
					operations: z
						.array(
							z.object({
								type: z.enum(["click", "flag", "chord"]),
								x: z.number(),
								y: z.number(),
							}),
						)
						.optional(),
					type: z.enum(["click", "flag", "chord"]).optional(),
					x: z.number().optional(),
					y: z.number().optional(),
				}),
			},
		);
		const remainTool = tool(
			async () => "3 mines remaining",
			{
				name: "saolei_remain",
				description: "查询剩余地雷数。",
				schema: z.object({}),
			},
		);
		const calls: Array<{ systemPrompt: string; tools: StructuredToolInterface[] }> = [];
		const createAgentFn = vi.fn(
			(config: { systemPrompt?: string; tools?: StructuredToolInterface[] }) => {
				calls.push({
					systemPrompt: config.systemPrompt ?? "",
					tools: config.tools ?? [],
				});
				return { invoke: async () => ({ messages: [] as BaseMessage[] }) };
			},
		);
		buildTestGraph({ playerTools: [operateTool, remainTool], createAgentFn });

		// The spy was actually exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
		const plannerCall = calls.find((c) => isPlannerPrompt(c.systemPrompt));
		expect(plannerCall).toBeDefined();
		// The game-visible tool IS listed...
		expect(plannerCall?.systemPrompt).toContain(
			"saolei_operate: 执行落子操作，支持 click/flag/chord。",
		);
		// ...while the read-only saolei_remain (no gameLog trace — the
		// planner cannot observe its use) is NOT injected (FR-016 refine).
		expect(plannerCall?.systemPrompt).not.toContain("saolei_remain");
		// FR-018 unchanged (the player tools stay out of the tool set): the
		// planner's tools are the memory tool + the internal instruct_player
		// calibration tool (Phase 6 — no shared-strategy tool).
		expect(plannerCall?.tools.map((t) => t.name)).toEqual([
			"memory",
			"instruct_player",
		]);
	});
});

describe("team graph — 039 US2: planner memory data plane (T020/T021/T022)", () => {
	it("planner systemPrompt carries the memory skill body + SKILL_PROMPT_SEPARATOR; the player's does not (FR-020/SC-009)", () => {
		const { createAgentFn, plannerSystemPrompt, playerSystemPrompt } =
			captureSystemPrompts();
		buildTestGraph({ createAgentFn });

		// The spy was actually exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
		// FR-020 / memory-skill-contract.md §2: the planner's static
		// systemPrompt = appendSkillBodyToPrompt(base, ["memory"]) — the
		// memory skill body is appended after SKILL_PROMPT_SEPARATOR.
		const prompt = plannerSystemPrompt();
		expect(prompt).toContain(SKILL_PROMPT_SEPARATOR);
		expect(prompt).toContain(MEMORY_SKILL_BODY);
		// The memory skill body starts after the separator (not baked into
		// the base) — static assembly, prefix-cache friendly (§2).
		expect(
			prompt.indexOf(MEMORY_SKILL_BODY),
		).toBeGreaterThan(prompt.indexOf(SKILL_PROMPT_SEPARATOR));
		// SC-009: the player's systemPrompt carries ONLY the saolei skill —
		// no memory skill (player holds no memory tools, FR-009).
		expect(playerSystemPrompt()).not.toContain(MEMORY_SKILL_BODY);
		expect(playerSystemPrompt()).toContain(SAOLEI_SKILL_BODY);
	});

	it("planner tool set = the memory tool + the internal instruct_player calibration tool (T020/T027)", () => {
		const calls: Array<{ systemPrompt: string; tools: StructuredToolInterface[] }> = [];
		const createAgentFn = vi.fn(
			(config: { systemPrompt?: string; tools?: StructuredToolInterface[] }) => {
				calls.push({
					systemPrompt: config.systemPrompt ?? "",
					tools: config.tools ?? [],
				});
				return { invoke: async () => ({ messages: [] as BaseMessage[] }) };
			},
		);
		buildTestGraph({ createAgentFn });

		expect(createAgentFn).toHaveBeenCalled();
		const plannerCall = calls.find((c) => isPlannerPrompt(c.systemPrompt));
		expect(plannerCall).toBeDefined();
		// The injected memory MCP tool (single hermes-style `memory` tool,
		// FR-007/FR-008) plus the internal `instruct_player` calibration tool
		// (039 US3, T027 — the shared-strategy path was removed in Phase 6,
		// T030/T031, FR-013).
		expect(plannerCall?.tools.map((t) => t.name)).toEqual([
			"memory",
			"instruct_player",
		]);
	});

	it("injects the frozen snapshot as a pure-content SystemMessage into the planner input and filters it from the channel write-back (FR-011, contract §3)", async () => {
		// Pre-baked snapshot (team-init boundary bake) with two entries; the
		// memory_id stays internal and never appears in LLM-visible text.
		const memoryClient = fakeMemoryClient([
			{ memory_id: "m1", content: "player 常误标边角" },
			{ memory_id: "m2", content: "开局先点中心更高效" },
		]);
		const frozenSnapshot = new FrozenMemorySnapshot();
		await frozenSnapshot.refresh(memoryClient, "saolei", "graph-test");
		const plannerModel = instructPlayerPlannerModel("corner-first");
		const { graph } = buildTestGraph({
			plannerModel,
			memoryClient,
			frozenSnapshot,
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-memory-input" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// The planner's FIRST model call receives the snapshot as an input
		// SystemMessage with the fixed snapshot id (contract §3).
		const firstCall = plannerModel.calls[0]?.messages as BaseMessage[];
		expect(firstCall).toBeDefined();
		const snapshotMsg = firstCall.find(
			(m) => m.id === PLANNER_MEMORY_SNAPSHOT_ID,
		);
		expect(snapshotMsg).toBeInstanceOf(SystemMessage);
		const snapshotText = String((snapshotMsg as BaseMessage).content);
		// 纯内容 (hermes style): each entry rendered as its content only —
		// NO memory_id prefixes (FR-011/Session 2026-08-08).
		expect(snapshotText).toContain("长期记忆：");
		expect(snapshotText).toContain("player 常误标边角");
		expect(snapshotText).toContain("开局先点中心更高效");
		expect(snapshotText).not.toContain("m1");
		expect(snapshotText).not.toContain("m2");
		// The snapshot SystemMessage sits BEFORE the review input in the
		// model call (input order: snapshot, plannerMessages, reviewInput —
		// contract §3; the createAgent may prepend its own system message, so
		// the assertion is relative ordering, not absolute position).
		const snapshotIdx = firstCall.findIndex(
			(m) => m.id === PLANNER_MEMORY_SNAPSHOT_ID,
		);
		const reviewIdx = firstCall.findIndex((m) =>
			contentType(m).includes("本局游戏过程"),
		);
		expect(snapshotIdx).toBeGreaterThanOrEqual(0);
		expect(reviewIdx).toBeGreaterThan(snapshotIdx);
		// The snapshot is filtered from the channel write-back — it must not
		// enter the short-term plannerMessages channel (contract §3).
		for (const m of result.plannerMessages) {
			expect(m.id).not.toBe(PLANNER_MEMORY_SNAPSHOT_ID);
		}
	});

	it("review does NOT refresh the frozen snapshot (FR-010 — refresh boundary is compress only)", async () => {
		const memoryClient = fakeMemoryClient([
			{ memory_id: "m1", content: "entry" },
		]);
		const frozenSnapshot = new FrozenMemorySnapshot();
		// Team-init bake (the ONLY pre-review refresh).
		await frozenSnapshot.refresh(memoryClient, "saolei", "graph-test");
		const listMemories = memoryClient.listMemories as ReturnType<typeof vi.fn>;
		const { graph } = buildTestGraph({ memoryClient, frozenSnapshot });

		await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-no-refresh" }, recursionLimit: 50 },
		);

		// The review node ran (planner channel non-empty) but the snapshot
		// was NOT re-read — exactly the one team-init bake (FR-010).
		expect(listMemories).toHaveBeenCalledTimes(1);
	});

	it("compress refreshes the frozen snapshot at the compression boundary (T021, contract §2.4 — after review, before END)", async () => {
		// The fake client's entries CHANGE between the team-init bake and the
		// compression-boundary re-read (simulating mid-session memory writes
		// landing in the memory service).
		const listMemories = vi
			.fn()
			.mockResolvedValueOnce([
				{ memory_id: "m1", content: "开局先点中心更高效" },
			])
			.mockResolvedValue([
				{ memory_id: "m1", content: "开局先点中心更高效" },
				{ memory_id: "m2", content: "player 在边角频繁误标地雷" },
			]);
		const memoryClient = { listMemories } as unknown as MemoryClient;
		const frozenSnapshot = new FrozenMemorySnapshot();
		await frozenSnapshot.refresh(memoryClient, "saolei", "graph-test");
		expect(String(frozenSnapshot.toSystemMessage().content)).toBe(
			"长期记忆：\n开局先点中心更高效",
		);

		const buffer = createEphemeralGameBuffer();
		const playerModel = fiveGamesPlayerModel("player 摘要内容");
		const plannerModel = fiveGamesPlannerModel("planner 摘要内容");
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			memoryClient,
			frozenSnapshot,
			template: "saolei",
			buffer,
			sessionId: "graph-test",
			playerTools: [buildMixedOutcomePlayerTool(buffer)],
			plannerTools: [buildFakeMemoryTool()],
			playerBasePrompt: "",
			plannerBasePrompt: "",
		});

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-compress-refresh" }, recursionLimit: 200 },
		)) as TeamStateValue;

		// The compression boundary ran (gameCounter 5 + compressed channels).
		expect(result.gameCounter).toBe(5);
		// Exactly TWO re-reads: the team-init bake + the compress-boundary
		// refresh (the 5 reviews did NOT refresh — FR-010).
		expect(listMemories).toHaveBeenCalledTimes(2);
		expect(listMemories).toHaveBeenLastCalledWith("saolei", "graph-test");
		// The snapshot was re-baked with the LATEST entries (D4: re-read →
		// re-bake at the compress boundary).
		expect(String(frozenSnapshot.toSystemMessage().content)).toBe(
			"长期记忆：\n开局先点中心更高效\nplayer 在边角频繁误标地雷",
		);
	});

	it("rebuild against the injected checkpointer recompiles the planner with the SAME memory tools and frozen snapshot (T022 — 040 rebuild seam)", async () => {
		const memoryClient = fakeMemoryClient([
			{ memory_id: "m1", content: "开局先点中心更高效" },
		]);
		const frozenSnapshot = new FrozenMemorySnapshot();
		await frozenSnapshot.refresh(memoryClient, "saolei", "graph-test");
		const memoryTool = buildFakeMemoryTool();
		const calls: Array<{ systemPrompt: string; tools: StructuredToolInterface[] }> = [];
		const createAgentFn = vi.fn(
			(config: { systemPrompt?: string; tools?: StructuredToolInterface[] }) => {
				calls.push({
					systemPrompt: config.systemPrompt ?? "",
					tools: config.tools ?? [],
				});
				return { invoke: async () => ({ messages: [] as BaseMessage[] }) };
			},
		);
		const buffer = createEphemeralGameBuffer();
		const deps = {
			playerModel: playOneGamePlayerModel(),
			plannerModel: instructPlayerPlannerModel("corner-first"),
				memoryClient,
			frozenSnapshot,
			template: "saolei",
			buffer,
			sessionId: "graph-test",
			playerTools: [buildGameEndingPlayerTool(buffer)],
			plannerTools: [memoryTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			createAgentFn,
		};

		// First build (first-build factory call site) then rebuild against the
		// EXISTING checkpointer (040 rebuild closure call site — server.ts
		// injects the same memory assembly at BOTH call sites, T022).
		const handle1 = buildTeamGraph(deps);
		const handle2 = buildTeamGraph(deps, handle1.checkpointer);

		// The spy was actually exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
		// team-rebuild-contract.md §7: the checkpointer reference is the SAME.
		expect(handle2.checkpointer).toBe(handle1.checkpointer);
		// BOTH builds' planner createAgent calls hold the memory tool — the
		// rebuilt graph's planner carries the IDENTICAL memory-tool instance
		// (reference equality) and the same frozen snapshot deps (per-session
		// state must survive the rebuild — T022 requirement).
		const plannerCalls = calls.filter((c) => isPlannerPrompt(c.systemPrompt));
		expect(plannerCalls).toHaveLength(2);
		for (const c of plannerCalls) {
			expect(c.tools.map((t) => t.name)).toEqual(["memory", "instruct_player"]);
			expect(c.tools[0]).toBe(memoryTool);
		}
		// The rebuilt graph's planner input still renders the SAME snapshot
		// content (the shared instance's baked entries).
		const rebuiltInput = frozenSnapshot.toSystemMessage();
		expect(String(rebuiltInput.content)).toContain("开局先点中心更高效");
	});
});

describe("team graph — US2 (037): 5-game compression (FR-006..FR-015)", () => {
	it("compresses both channels into one summary after 5 ended games, player stops (FR-006/FR-008/FR-009/FR-010/FR-012)", async () => {
		// Mixed won AND lost outcomes — both count a game (FR-006). The tool
		// closes over the SAME buffer the graph uses (sink → buffer → node).
		const buffer = createEphemeralGameBuffer();
		const playerModel = fiveGamesPlayerModel("player 摘要内容");
		const plannerModel = fiveGamesPlannerModel("planner 摘要内容");
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			buffer,
			sessionId: "graph-test",
			playerTools: [buildMixedOutcomePlayerTool(buffer)],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
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
			{ name: "instruct_player", args: { content: "v6" } },
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

	it("normalizes a content-blocks summary response to a plain string (FR-008/FR-012 — the @langchain/core output_version v1 shape)", async () => {
		// The deployed OpenAI-compatible adapters may return the model text in
		// the STANDARD CONTENT-BLOCKS shape (`[{type:"text",text}]` — produced
		// when @langchain/core stamps `response_metadata.output_version: "v1"`
		// and the message carries the blocks array as `content`). The pre-fix
		// `typeof content !== "string"` check aborted compression on that shape
		// (observed in the 037 large test: "compression failed: summary model
		// returned non-string content"); extractTextContent must normalize it
		// to the plain string summary.
		const playerModel = fakeModel();
		for (let i = 0; i < 5; i += 1) {
			playerModel.respondWithTools([
				{ name: "fake_saolei_move", args: { x: i + 1, y: i + 1 } },
			]);
		}
		playerModel.respond(
			new AIMessage([{ type: "text", text: "player 摘要内容" }]),
		);
		const plannerModel = fakeModel();
		for (let i = 0; i < 5; i += 1) {
			plannerModel.respondWithTools([
				{ name: "instruct_player", args: { content: `v${i + 1}` } },
			]);
			plannerModel.respond(new AIMessage(`v${i + 1} written`));
		}
		plannerModel.respond(
			new AIMessage([{ type: "text", text: "planner 摘要内容" }]),
		);
		const { graph } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-blocks-summary" }, recursionLimit: 200 },
		)) as TeamStateValue;

		// Both channels shrank to one summary whose content is the plain text
		// extracted from the blocks (FR-008/FR-012 — non-blank string).
		expect(result.playerMessages).toHaveLength(1);
		expect(contentType(result.playerMessages[0] as BaseMessage)).toBe(
			"player 摘要内容",
		);
		expect(result.plannerMessages).toHaveLength(1);
		expect(contentType(result.plannerMessages[0] as BaseMessage)).toBe(
			"planner 摘要内容",
		);
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
		// The planner model was never invoked for a summary (18 = the 5
		// degraded review runs' retries (5×3) + the postCompactInstruction
		// node's own degraded retries (3) — 039 US3).
		expect(plannerModel.callCount).toBe(18);
	});

	it("does not compress at a non-5 gameCounter (FR-006: MUST NOT trigger)", async () => {
		const playerModel = fakeModel()
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
			.respondWithTools([{ name: "fake_saolei_move", args: { x: 2, y: 2 } }])
			.respond(new AIMessage("idle, no new game"));
		const plannerModel = fakeModel()
			.respondWithTools([{ name: "instruct_player", args: { content: "v1" } }])
			.respond(new AIMessage("v1 sent"))
			.respondWithTools([{ name: "instruct_player", args: { content: "v2" } }])
			.respond(new AIMessage("v2 sent"));
		const { graph } = buildTestGraph({ playerModel, plannerModel });

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
				configurable: {
					thread_id: "t-compress-frame",
					emitChannelFrame,
					instructionBuffer: { content: null },
				},
				recursionLimit: 200,
			},
		)) as TeamStateValue;

		// 5 planner review-input frames (US1, agent="planner") + 2 summary
		// frames (player channel, planner channel) + 1 postCompactInstruction
		// request frame (039 US3 — the compact scenario emits its request as
		// a planner frame, typing-state coordination, contract §6) = 8.
		expect(emitChannelFrame).toHaveBeenCalledTimes(8);
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
		// The postCompactInstruction request frame (agent=planner, carries the
		// compact scenario prompt — FR-016).
		expect(calls[7][0]).toBe(PLANNER_AGENT_NAME);
		expect(String(calls[7][1])).toContain("上下文刚被压缩");
	});

	it("clears the compressed channels and resets gameCounter on RefreshTeam — including any instruction in playerMessages (FR-014 / US2 AS8; 039 contract §7)", async () => {
		const playerModel = fiveGamesPlayerModel("player 摘要内容");
		const plannerModel = fiveGamesPlannerModel("planner 摘要内容");
		const { graph, sessionId } = buildTestGraph({
			playerModel,
			plannerModel,
		});
		// The invoke runs on the session thread (thread_id == sessionId) so
		// refreshTeamChannels(graph, sessionId) targets the same thread.
		await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: sessionId }, recursionLimit: 200 },
		);

		// A stale instruction must not survive the refresh (contract §7 —
		// 039 US3: instructions live IN playerMessages, so the channel clear
		// covers them).
		await graph.updateState(
			{ configurable: { thread_id: sessionId } },
			{ playerMessages: [new HumanMessage("过期指令")] },
		);
		await refreshTeamChannels(graph, sessionId);

		const snapshot = (await graph.getState({
			configurable: { thread_id: sessionId },
		})) as unknown as { values: TeamStateValue };
		// The summaries (short-term messages) are cleared alongside the rest.
		expect(snapshot.values.playerMessages).toEqual([]);
		expect(snapshot.values.plannerMessages).toEqual([]);
		expect(snapshot.values.gameCounter).toBe(0);
	});
});

describe("team graph — checkpointer injection (US3 rebuild seam, specs/040-team-singleton-conformance)", () => {
	it("recompiling with an injected checkpointer restores the SAME thread state (FR-005, team-rebuild-contract.md §2/§7)", async () => {
		// First build: the default (fresh MemorySaver) path.
		const buffer = createEphemeralGameBuffer();
		const handle1 = buildTeamGraph({
			playerModel: playOneGamePlayerModel(),
			plannerModel: instructPlayerPlannerModel("corner-first"),
			buffer,
			sessionId: "graph-test",
			playerTools: [buildGameEndingPlayerTool(buffer)],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
		});
		await handle1.graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-rebuild" }, recursionLimit: 50 },
		);
		const before = (await handle1.graph.getState({
			configurable: { thread_id: "t-rebuild" },
		})) as unknown as { values: TeamStateValue };
		expect(before.values.playerMessages.length).toBeGreaterThan(0);
		expect(before.values.plannerMessages.length).toBeGreaterThan(0);

		// Rebuild: recompile against the EXISTING checkpointer (never a new
		// MemorySaver — that would drop the history). MemorySaver is a
		// per-thread_id KV store decoupled from the graph instance, so the
		// recompiled graph restores the same thread's channels.
		const buffer2 = createEphemeralGameBuffer();
		const handle2 = buildTeamGraph(
			{
				playerModel: playOneGamePlayerModel(),
				plannerModel: instructPlayerPlannerModel("new-strategy"),
				buffer: buffer2,
				sessionId: "graph-test",
				playerTools: [buildGameEndingPlayerTool(buffer2)],
				playerBasePrompt: "",
				plannerBasePrompt: "",
				...memoryDeps(),
			},
			handle1.checkpointer,
		);
		// team-rebuild-contract.md §7: the checkpointer reference is the SAME.
		expect(handle2.checkpointer).toBe(handle1.checkpointer);
		const after = (await handle2.graph.getState({
			configurable: { thread_id: "t-rebuild" },
		})) as unknown as { values: TeamStateValue };
		// History is preserved with zero loss / zero duplication.
		expect(after.values.playerMessages).toEqual(before.values.playerMessages);
		expect(after.values.plannerMessages).toEqual(
			before.values.plannerMessages,
		);
		expect(after.values.gameCounter).toBe(before.values.gameCounter);
	});
});

describe("team graph — 039 US3: init/compact instruction scenarios (T025/T026, contract §2.3/§2.5/§4)", () => {
	it("routes the async init turn to initInstruction → END: writes the instruction into playerMessages, does NOT invoke the player (FR-015)", async () => {
		// The init turn (session-team.ts triggerInitInstruction — R2) runs
		// with the `runInitInstruction` configurable flag; the START
		// conditional edge routes it to initInstruction → END. The player is
		// NOT invoked — the instruction lands in `playerMessages` (channel
		// write-back, same as the review node) and is delivered with the
		// player's next activation (FR-015 "不立即激活 player").
		const playerModel = fakeModel().respond(new AIMessage("unused"));
		const initPlanner = fakeModel()
			.respondWithTools([
				{ name: "instruct_player", args: { content: "开局先点中心" } },
			])
			.respond(new AIMessage("init done"));
		const { graph } = buildTestGraph({ playerModel, plannerModel: initPlanner });

		const result = (await graph.invoke(
			{},
			{
				configurable: {
					thread_id: "t-init-turn",
					runInitInstruction: true,
					// R1 external buffer (contract §4) — the instruction node
					// reads the staged content after the invoke returns.
					instructionBuffer: { content: null },
				},
				recursionLimit: 50,
			},
		)) as TeamStateValue;

		// The LLM decided to call instruct_player → the no-game-history
		// instruction was produced into `playerMessages` as a HumanMessage
		// (R4 — prompt 要求给指令, 无强制检验; no pending slot).
		const instructionMsg = result.playerMessages.find(
			(m) =>
				typeof m.content === "string" &&
				m.content.includes("开局先点中心"),
		);
		expect(instructionMsg).toBeInstanceOf(HumanMessage);
		// No player activation happened during the init turn.
		expect(playerModel.calls).toHaveLength(0);
		// No game / review activity either (plannerMessages holds only the
		// instruction node's own exchange).
		expect(result.gameEnded).toBeNull();
	});

	it("ordinary turns skip initInstruction entirely (no runInitInstruction flag → START → player)", async () => {
		const playerModel = fakeModel().respond(new AIMessage("hi"));
		const { graph } = buildTestGraph({ playerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("你好")] },
			{ configurable: { thread_id: "t-normal-turn" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// No game ended → no planner; the init node never ran (no
		// instruction in playerMessages, no planner messages). The channel
		// holds exactly the single user input HumanMessage.
		expect(
			result.playerMessages.filter((m) => m._getType() === "human"),
		).toHaveLength(1);
		expect(result.plannerMessages).toEqual([]);
		expect(playerModel.calls).toHaveLength(1);
	});

	it("postCompactInstruction runs after compress → END: writes the instruction into playerMessages, the player does NOT continue (FR-016, 037 压缩后自动停下)", async () => {
		const buffer = createEphemeralGameBuffer();
		const playerModel = fiveGamesPlayerModel("player 摘要内容");
		const plannerModel = fiveGamesPlannerModel("planner 摘要内容");
		// The compact scenario's own LLM run (post-compact instruction):
		// prompt 要求给指令, LLM decides to call instruct_player (R4).
		plannerModel.respondWithTools([
			{ name: "instruct_player", args: { content: "重建引导：保持节奏" } },
		]);
		plannerModel.respond(new AIMessage("compact instruction sent"));
		const { graph } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{
				configurable: {
					thread_id: "t-compact-instr",
					instructionBuffer: { content: null },
				},
				recursionLimit: 200,
			},
		)) as TeamStateValue;

		// 5 games counted; the compression ran (channels shrank to summaries).
		expect(result.gameCounter).toBe(5);
		// The compressed player summary + the compact instruction (written
		// into playerMessages — delivered with the player's NEXT activation,
		// FR-016; NOT a pending slot).
		expect(result.playerMessages).toHaveLength(2);
		const instructionMsg = result.playerMessages.find(
			(m) =>
				typeof m.content === "string" &&
				m.content.includes("重建引导：保持节奏"),
		);
		expect(instructionMsg).toBeInstanceOf(HumanMessage);
		// The player stopped after the compression: 5 move calls + 1 summary
		// call = 6; a 7th call would mean the graph routed back to the player
		// after postCompactInstruction (it must END instead).
		expect(playerModel.callCount).toBe(6);
	});

	it("the compact instruction uses the compress-boundary-refreshed frozen snapshot (T021 → T025 chain)", async () => {
		// The fake client's entries CHANGE between the team-init bake and the
		// compression-boundary re-read.
		const listMemories = vi
			.fn()
			.mockResolvedValueOnce([
				{ memory_id: "m1", content: "开局先点中心更高效" },
			])
			.mockResolvedValue([
				{ memory_id: "m1", content: "开局先点中心更高效" },
				{ memory_id: "m2", content: "player 在边角频繁误标地雷" },
			]);
		const memoryClient = { listMemories } as unknown as MemoryClient;
		const frozenSnapshot = new FrozenMemorySnapshot();
		await frozenSnapshot.refresh(memoryClient, "saolei", "graph-test");

		const buffer = createEphemeralGameBuffer();
		const playerModel = fiveGamesPlayerModel("player 摘要内容");
		const plannerModel = fiveGamesPlannerModel("planner 摘要内容");
		plannerModel.respondWithTools([
			{ name: "instruct_player", args: { content: "重建引导" } },
		]);
		plannerModel.respond(new AIMessage("compact instruction sent"));
		const { graph } = buildTeamGraph({
			playerModel,
			plannerModel,
			memoryClient,
			frozenSnapshot,
			template: "saolei",
			buffer,
			sessionId: "graph-test",
			playerTools: [buildMixedOutcomePlayerTool(buffer)],
			plannerTools: [buildFakeMemoryTool()],
			playerBasePrompt: "",
			plannerBasePrompt: "",
		});

		await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{
				configurable: {
					thread_id: "t-compact-snapshot",
					instructionBuffer: { content: null },
				},
				recursionLimit: 200,
			},
		);

		// The postCompactInstruction model call received the REFRESHED
		// snapshot (baked after the compress-boundary re-read — the last
		// planner-family call, after the 5 reviews and the summary).
		expect(listMemories).toHaveBeenCalledTimes(2);
		const lastCall = plannerModel.calls.at(-1)?.messages as BaseMessage[];
		const snapshotMsg = lastCall?.find(
			(m) => m.id === PLANNER_MEMORY_SNAPSHOT_ID,
		);
		expect(snapshotMsg).toBeDefined();
		expect(String(snapshotMsg?.content)).toContain("player 在边角频繁误标地雷");
	});
});

describe("team graph — 039 US3: review calibration instruction (T027, contract §2.2/§4 — FR-017)", () => {
	it("the review instruction lands AFTER the game-ending tool_result and BEFORE the player's next output in the player channel (FR-017 order)", async () => {
		const playerModel = playOneGamePlayerModel();
		const plannerModel = instructPlayerPlannerModel("保持节奏");
		const { graph } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{
				configurable: {
					thread_id: "t-fr017",
					instructionBuffer: { content: null },
				},
				recursionLimit: 50,
			},
		)) as TeamStateValue;

		// Player channel order: user input → tool_calling (AI with tool
		// calls) → tool_result (game end) → planner instruction (H) →
		// player next output (idle AI). Assert the instruction sits strictly
		// between the last tool message and the last AI message.
		const msgs = result.playerMessages;
		const types = msgs.map((m) => m._getType());
		const lastToolIdx = types.lastIndexOf("tool");
		const lastAiIdx = types.lastIndexOf("ai");
		const instrIdx = msgs.findIndex(
			(m) =>
				typeof m.content === "string" && m.content.includes("保持节奏"),
		);
		expect(lastToolIdx).toBeGreaterThanOrEqual(0);
		expect(lastAiIdx).toBeGreaterThan(lastToolIdx);
		expect(instrIdx).toBeGreaterThan(lastToolIdx);
		expect(instrIdx).toBeLessThan(lastAiIdx);
		expect(msgs[instrIdx]).toBeInstanceOf(HumanMessage);
	});

	it("routes back to the player even when the review sends NO instruction (FR-014 — the instruction is optional)", async () => {
		const playerModel = playOneGamePlayerModel();
		// The planner's review does NOT call instruct_player (LLM decided
		// nothing needs calibrating).
		const plannerModel = fakeModel().respond(new AIMessage("复盘完成，无需指令"));
		const { graph } = buildTestGraph({ playerModel, plannerModel });

		const result = (await graph.invoke(
			{ playerMessages: [new HumanMessage("开始游戏")] },
			{ configurable: { thread_id: "t-no-instr" }, recursionLimit: 50 },
		)) as TeamStateValue;

		// No instruction in the player channel — but the graph still routed
		// back to the player (its idle output is present; FR-009).
		expect(
			result.playerMessages.some(
				(m) =>
					typeof m.content === "string" && m.content.includes("保持节奏"),
			),
		).toBe(false);
		expect(playerModel.calls).toHaveLength(2);
		expect(result.gameEnded).toBeNull();
	});
});
