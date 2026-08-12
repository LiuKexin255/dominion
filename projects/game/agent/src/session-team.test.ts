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
 * factory — no `vi.mock` (single exception: the module-level
 * `INIT_TURN_TIMEOUT_MS` constant override for the 043 US4 timeout test —
 * justified inline at the `vi.mock` site). The stream display sink (041 —
 * specs/041-realtime-init-push/contracts/realtime-channel-contract.md §1.1) is
 * the DI seam for frame capture: tests inject a recording closure / `vi.fn()`
 * via `bindStreamSink(emit, emit)` (the closure doubles as its own
 * compare-and-delete handle, operation-bridge.ts:77), and `submit` no longer
 * takes an emit parameter (041 T002 — the sink is bound at Connect).
 */

import { describe, expect, it, vi } from "vitest";
import * as grpc from "@grpc/grpc-js";
import { installReporter, type Reporter } from "@dominion/common-js-logs";
import { AIMessage, HumanMessage } from "@langchain/core/messages";
import { MemorySaver } from "@langchain/langgraph";
import { fakeModel } from "@langchain/core/testing";
import { createAgent, tool } from "langchain";
import { z } from "zod";
import type { GameState } from "@dominion/game-saolei-board";

import { SessionTeam, SessionTeamStore } from "./session-team";
import { OperationBridge } from "./operation-bridge";
import { extractToolCalls } from "./llm";
import type { MemoryClient } from "./memory-client";
import { createEphemeralGameBuffer, createTeamSink } from "./team/team-sink";
import { buildTeamGraph, type TeamGraphHandle } from "./team/graph";
import { FrozenMemorySnapshot } from "./team/memory-snapshot";
import type { TeamStateValue } from "./team/state";
import type { StructuredToolInterface } from "@langchain/core/tools";
import type { TeamFrame } from "../game_types/projects/game/TeamFrame";

/**
 * Residual constant-override mock (style/javascript.md §测试 — Mock 约定
 * 脆弱模式; the inline justification it requires): `INIT_TURN_TIMEOUT_MS` is
 * evaluated at llm.ts MODULE LOAD (`Number(process.env...) || 120_000`), so
 * no DI seam can shorten it for the 043 US4 (FR-009) timeout test below —
 * setting the env var inside a test body runs after the imports already
 * resolved. The mock preserves EVERY other real export via `importOriginal`
 * (same pattern as mcp-host.test.ts) and overrides only the single constant.
 * The mock is positively asserted by the timeout test: with the real 120s
 * value its deadline checks would fail — no silent bypass.
 */
vi.mock("./llm", async (importOriginal) => {
	const actual = await importOriginal<typeof import("./llm")>();
	return { ...actual, INIT_TURN_TIMEOUT_MS: 1000 };
});

/** Template id of the test sessions (saolei — UpdateTeam default in tests). */
const TID = "saolei";

/**
 * 039 Phase 5 (T019): the memory data-plane deps the graph now requires —
 * DI fakes (a no-op MemoryClient + a fresh empty snapshot), mirroring the
 * production server.ts wiring (memory-client / per-session snapshot).
 */
function memoryDeps() {
	const memoryClient = {
		listMemories: async () => [],
	} as unknown as MemoryClient;
	return {
		memoryClient,
		frozenSnapshot: new FrozenMemorySnapshot(),
		template: TID,
		plannerTools: [] as StructuredToolInterface[],
	};
}

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

/**
 * The planner's fake model for a session whose graph runs BOTH the one-shot
 * async initInstruction turn (triggered at FIRST materialization — T029,
 * contract §6) and the review turn. fakeModel consumes responses in order:
 * init (tool call + text) → review (tool call + text). The init turn runs
 * via `graph.invoke` (not streamed), so its text never appears in the
 * TurnLoop frames — only the review's does.
 */
function initThenReviewPlannerModel(initContent: string, reviewContent: string) {
	return fakeModel()
		.respondWithTools([
			{ name: "instruct_player", args: { content: initContent } },
		])
		.respond(new AIMessage("init done"))
		.respondWithTools([
			{ name: "instruct_player", args: { content: reviewContent } },
		])
		.respond(new AIMessage("review done"));
}

/** Build a fully-wired SessionTeam (real graph) for a session id. */
function buildTestTeam(
	sessionId: string,
	plannerModel?: ReturnType<typeof fakeModel>,
) {
	const buffer = createEphemeralGameBuffer();
	const handle = buildTeamGraph({
		playerModel: playOneGamePlayerModel(),
		plannerModel:
			plannerModel ?? initThenReviewPlannerModel("初始指令", "复盘指令"),
		buffer,
		sessionId,
		playerTools: [buildGameEndingPlayerTool(buffer)],
		playerBasePrompt: "",
		plannerBasePrompt: "",
		...memoryDeps(),
	});
	// Pre-built bridge/sink like the production factory (server.ts): the
	// SessionTeam constructor no longer creates them internally.
	const bridge = new OperationBridge();
	const sink = createTeamSink(buffer);
	const team = new SessionTeam(handle, buffer, sessionId, TID, bridge, sink);
	return { team, buffer, sessionId, bridge, sink };
}

/**
 * Build ONLY a graph handle (US3 rebuild seam — the same wiring as
 * {@link buildTestTeam}, minus the SessionTeam shell). A rebuild passes the
 * EXISTING checkpointer so the session state carries over (FR-005); the
 * optional `checkpointer` is forwarded verbatim. NOTE: the rebuild path does
 * NOT trigger the initInstruction turn (040 FR-005 — init runs once at first
 * materialization only), so the rebuild's planner model only serves review
 * turns.
 */
function buildTestHandle(sessionId: string, checkpointer?: MemorySaver) {
	const buffer = createEphemeralGameBuffer();
	return buildTeamGraph(
		{
			playerModel: playOneGamePlayerModel(),
			plannerModel: fakeModel()
				.respondWithTools([
					{ name: "instruct_player", args: { content: "复盘指令" } },
				])
				.respond(new AIMessage("review done")),
			buffer,
			sessionId,
			playerTools: [buildGameEndingPlayerTool(buffer)],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
		},
		checkpointer,
	);
}

/** Recording emit sink collecting every frame the loop pushes. */
function recordingEmit(): {
	emit: (f: TeamFrame) => void;
	frames: TeamFrame[];
} {
	const frames: TeamFrame[] = [];
	return { emit: (f) => frames.push(f), frames };
}

function textBlocks(frames: TeamFrame[]): { agent?: string; text: string }[] {
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

function waitCount(frames: TeamFrame[]): number {
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

/** A releasable gate so a player turn can be held in-flight. */
interface Gate {
	promise: Promise<void>;
	resolve: () => void;
}

function makeGate(): Gate {
	let resolve!: () => void;
	const promise = new Promise<void>((r) => {
		resolve = r;
	});
	return { promise, resolve };
}

/** Poll `frames` until `predicate` matches (or fail after `timeoutMs`). */
async function waitForFrame(
	frames: TeamFrame[],
	predicate: (f: TeamFrame) => boolean,
	timeoutMs: number,
): Promise<void> {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		if (frames.some(predicate)) return;
		await new Promise((r) => setTimeout(r, 10));
	}
	throw new Error("timed out waiting for frame");
}

/** Collect messageParts frames carrying a `toolCall` / `toolResult` part. */
function partFrames(frames: TeamFrame[], kind: "toolCall" | "toolResult"): TeamFrame[] {
	return frames.filter((f) => {
		const fr = f as Record<string, unknown>;
		if (fr.payload !== "messageParts") return false;
		return (fr.messageParts as { parts: Record<string, unknown>[] }).parts.some(
			(p) => kind in p,
		);
	});
}

describe("SessionTeam", () => {
	it("drives one team turn per submit: player + planner blocks streamed with agent tags, then wait", async () => {
		const { team } = buildTestTeam("st-turn-1");
		const { emit, frames } = recordingEmit();

		team.bindStreamSink(emit, emit);
		team.submit({ text: "开始游戏" });
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

	it("streams model text from content-block-delta events exactly once (regression: no finish double-emit)", async () => {
		// The turn runner must consume ONLY `content-block-delta` events for
		// text/reasoning (token-granular live updates; the pre-team
		// single-agent path streamed the same deltas via `stream.messages`).
		// Consuming `content-block-finish` on top would double-emit each
		// model response, and skipping the deltas entirely would drop text
		// until the next tool call (spec 031 desktop batching regression).
		// This test constructs the SessionTeam directly (no SessionTeamStore
		// materialization) — the one-shot initInstruction turn is NOT
		// triggered here, so the planner model serves the review only (2
		// responses: instruct_player call + the review answer).
		const { team } = buildTestTeam(
			"st-delta",
			fakeModel()
				.respondWithTools([
					{ name: "instruct_player", args: { content: "复盘指令" } },
				])
				.respond(new AIMessage("review done")),
		);
		const { emit, frames } = recordingEmit();

		team.bindStreamSink(emit, emit);
		team.submit({ text: "开始游戏" });
		await flush();

		const texts = textBlocks(frames);
		const count = (needle: string) =>
			texts.filter((t) => t.text.trim() === needle).length;
		// Each model response's text appears exactly once across the streamed
		// frames (the player's "won, stopping" answer and the planner's
		// post-review answer; the third queued player response is never
		// invoked — the player agent loop stops after a text-only answer).
		// The initInstruction turn's "init done" text is NOT here — it runs
		// via `graph.invoke` (not streamed), T029.
		expect(count("won, stopping")).toBe(1);
		expect(count("review done")).toBe(1);
		expect(count("init done")).toBe(0);
	});

	it("getTeamState reconstructs both per-agent channels from the single checkpointer (A3)", async () => {
		const { team, sessionId } = buildTestTeam("st-state");
		const { emit } = recordingEmit();
		team.bindStreamSink(emit, emit);
		team.submit({ text: "开始游戏" });
		await flush();

		const state = await team.getTeamState();
		expect(state).not.toBeNull();
		expect((state as TeamStateValue).playerMessages.length).toBeGreaterThan(0);
		// The planner ran (game ended) — its channel holds the review.
		expect((state as TeamStateValue).plannerMessages.length).toBeGreaterThan(0);
		expect(sessionId).toBe("st-state");
	});

	it("refreshTeam clears BOTH channels, then triggers a fresh instruction turn (042 US3 — FR-008/FR-009/FR-012/FR-013)", async () => {
		const { team } = buildTestTeam(
			"st-refresh",
			fakeModel()
				.respondWithTools([
					{ name: "instruct_player", args: { content: "复盘指令" } },
				])
				.respond(new AIMessage("review done"))
				.respondWithTools([
					{ name: "instruct_player", args: { content: "刷新指令" } },
				])
				.respond(new AIMessage("refresh done")),
		);
		const { emit } = recordingEmit();
		team.bindStreamSink(emit, emit);
		team.submit({ text: "开始游戏" });
		await flush();

		// Pre-refresh: the review turn wrote an instruction INTO
		// playerMessages (no slot) — the refresh must clear the channel
		// including it (039 contract §7 — no expired instruction survives).
		const before = (await team.getTeamState()) as TeamStateValue;
		expect(before.playerMessages.length).toBeGreaterThan(0);

		await team.refreshTeam();

		// Post-refresh instruction turn in-flight (fire-and-forget, FR-009 —
		// the refresh returned after the channel clear): the busy gate holds
		// (isBusy() = true — FR-012, a second refresh/rebuild would be
		// rejected) while the status probe stays IDLE (isRunning() = false —
		// FR-011, no typing indicator, same as the team-init turn).
		expect(team.isBusy()).toBe(true);
		expect(team.isRunning()).toBe(false);

		await flush(0);
		expect(team.isBusy()).toBe(false);

		// The refresh triggered a NEW no-game-history instruction into the
		// CLEARED playerMessages (FR-008 — contract §2.3): the old
		// review/instruction messages are gone, exactly one fresh
		// instruction remains. gameEnded stays untouched (FR-018).
		const state = (await team.getTeamState()) as TeamStateValue;
		expect(state.playerMessages.length).toBe(1);
		expect(
			typeof state.playerMessages[0].content === "string" &&
				state.playerMessages[0].content.includes("刷新指令"),
		).toBe(true);
		expect(state.gameEnded).toBeNull();
		// BOTH channels were cleared: the old planner review output is gone
		// (the instruction turn's own planner request/response replaced it).
		expect(
			state.plannerMessages.some(
				(m) =>
					typeof m.content === "string" && m.content.includes("review done"),
			),
		).toBe(false);
	});

	it("triggers a fresh instruction on EVERY refresh (FR-013 — repeatable, unlike the one-shot team-init)", async () => {
		const { team } = buildTestTeam(
			"st-refresh-repeat",
			fakeModel()
				.respondWithTools([
					{ name: "instruct_player", args: { content: "复盘指令" } },
				])
				.respond(new AIMessage("review done"))
				.respondWithTools([
					{ name: "instruct_player", args: { content: "刷新指令一" } },
				])
				.respond(new AIMessage("refresh done 1"))
				.respondWithTools([
					{ name: "instruct_player", args: { content: "刷新指令二" } },
				])
				.respond(new AIMessage("refresh done 2")),
		);
		const { emit } = recordingEmit();
		team.bindStreamSink(emit, emit);
		team.submit({ text: "开始游戏" });
		await flush();

		const instructionCount = (state: TeamStateValue, needle: string) =>
			state.playerMessages.filter(
				(m) =>
					m._getType() === "human" &&
					typeof m.content === "string" &&
					m.content.includes(needle),
			).length;

		// Refresh #1: the cleared channel receives ONE new instruction.
		await team.refreshTeam();
		await flush(0);
		let state = (await team.getTeamState()) as TeamStateValue;
		expect(instructionCount(state, "刷新指令一")).toBe(1);
		expect(instructionCount(state, "刷新指令二")).toBe(0);

		// Refresh #2 (after the first completed): triggers ANOTHER
		// instruction — the previous one is cleared with the channels, the
		// fresh turn writes its own (FR-013 — non-one-shot, unlike the
		// team-init triggerInitInstruction guard).
		await team.refreshTeam();
		await flush(0);
		state = (await team.getTeamState()) as TeamStateValue;
		expect(instructionCount(state, "刷新指令一")).toBe(0);
		expect(instructionCount(state, "刷新指令二")).toBe(1);
	});

	it("excludes the post-refresh instruction turn from isRunning() but keeps it in isBusy() (FR-011/FR-012 — same as the team-init turn)", async () => {
		// Hold the post-refresh instruction turn's agent invoke on a gate
		// (the createAgentFn DI seam, graph.ts:230-231 — same pattern as
		// handler.test.ts createGatedInitStore) so the in-flight window is
		// deterministic: the status probe must report IDLE (isRunning() =
		// false — no typing indicator, FR-011) while the busy gate rejects a
		// second refresh/rebuild (isBusy() = true — FR-012).
		const gate = makeGate();
		const buffer = createEphemeralGameBuffer();
		const handle = buildTeamGraph({
			playerModel: playOneGamePlayerModel(),
			plannerModel: fakeModel()
				.respondWithTools([
					{ name: "instruct_player", args: { content: "刷新指令" } },
				])
				.respond(new AIMessage("refresh done")),
			buffer,
			sessionId: "st-refresh-busy",
			playerTools: [buildGameEndingPlayerTool(buffer)],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
			createAgentFn: (config) => {
				const agent = createAgent(config);
				const wrapped: {
					invoke: (input: unknown, cfg?: unknown) => Promise<unknown>;
				} = Object.create(agent);
				wrapped.invoke = async (input, cfg) => {
					await gate.promise;
					return agent.invoke(input as never, cfg as never);
				};
				return wrapped;
			},
		});
		const team = new SessionTeam(
			handle,
			buffer,
			"st-refresh-busy",
			TID,
			new OperationBridge(),
			createTeamSink(buffer),
		);
		const { emit } = recordingEmit();
		team.bindStreamSink(emit, emit);

		// No user turn: only the post-refresh instruction turn runs. The
		// refresh returns right after the channel clear (FR-009 — the
		// instruction turn stays in-flight on the gate).
		await team.refreshTeam();
		expect(team.isBusy()).toBe(true);
		expect(team.isRunning()).toBe(false);

		// The gate releases → the instruction completes → the busy gate
		// clears and the fresh instruction lands in playerMessages.
		gate.resolve();
		await flush(0);
		expect(team.isBusy()).toBe(false);
		const state = (await team.getTeamState()) as TeamStateValue;
		expect(
			state.playerMessages.some(
				(m) =>
					m._getType() === "human" &&
					typeof m.content === "string" &&
					m.content.includes("刷新指令"),
			),
		).toBe(true);
	});

	it("abort stops a running turn and emits wait", async () => {
		// A player model that never stops would hang the test; instead use a
		// gate-less quick model and abort mid-flight via a slow fake: the
		// echo runner here is the REAL graph, so use a plain turn and abort
		// immediately after submit (best-effort — loop observes abort).
		const { team } = buildTestTeam("st-abort");
		const { emit, frames } = recordingEmit();
		team.bindStreamSink(emit, emit);
		team.submit({ text: "hi" });
		team.abort();
		await flush();
		expect(team.isRunning()).toBe(false);
	});

	it("getBridge returns the session bridge and getSink binds the ephemeral buffer", async () => {
		const { team, buffer, sessionId, bridge, sink } = buildTestTeam("st-surface");
		// The injected instances ARE the exposed ones (the MCP host and the
		// graph player must share one bridge / one sink — server.ts factory).
		expect(team.getBridge()).toBe(bridge);
		expect(team.getSink()).toBe(sink);
		// The sink writes the session's ephemeral buffer (D7).
		sink.onGameEnd(makeState(), "lost");
		expect(buffer.gameEvent).not.toBeNull();
		expect(buffer.gameEvent?.status).toBe("lost");
		expect(sessionId).toBe("st-surface");
	});

	it("streams the player's tool_call frame BEFORE the tool's awaited operation resolves (async dispatch)", async () => {
		// Regression for the T030 deadlock (031-team-template-mode large test):
		// the saolei tools await the desktop's FlowResult via
		// `OperationBridge.dispatch` (operation-bridge.ts `dispatch`), so the
		// player createAgent loop cannot finish while a dispatch is pending —
		// a node-level `updates` stream would never emit the tool_call frame,
		// and the desktop (which replies only after seeing tool_call +
		// operation) deadlocks. The turn runner must emit tool_call frames
		// from the fine-grained stream events BEFORE the tool resolves.
		const buffer = createEphemeralGameBuffer();
		const bridge = new OperationBridge();
		const sink = createTeamSink(buffer);
		const asyncMove = tool(
			async ({ x, y }: { x: number; y: number }) => {
				// Real OperationBridge: awaits the desktop's FlowResultPart.
				const result = await bridge.dispatch({
					keyboardPress: { key: "KEYBOARD_KEY_F2" },
				});
				return `moved to (${x},${y}); status=${result.status}`;
			},
			{
				name: "async_saolei_move",
				description: "Async saolei move awaiting the desktop.",
				schema: z.object({ x: z.number(), y: z.number() }),
			},
		);
		const handle = buildTeamGraph({
			playerModel: fakeModel()
				.respondWithTools([{ name: "async_saolei_move", args: { x: 1, y: 1 } }])
				.respond(new AIMessage("done, stopping")),
			plannerModel: fakeModel().respond(new AIMessage("ok")),
			buffer,
			sessionId: "st-async-dispatch",
			playerTools: [asyncMove],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
		});
		const team = new SessionTeam(
			handle,
			buffer,
			"st-async-dispatch",
			TID,
			bridge,
			sink,
		);
		const { emit, frames } = recordingEmit();
		const dispatched: TeamFrame[] = [];
		bridge.registerSink((frame) => dispatched.push(frame));

		team.bindStreamSink(emit, emit);
		team.submit({ text: "开始游戏" });

		// Do NOT wait for the turn to complete: poll until the tool_call
		// frame is emitted while the tool is still awaiting its operation.
		await waitForFrame(frames, (f) => partFrames([f], "toolCall").length > 0, 5000);

		// The tool has NOT resolved yet — the player node cannot have
		// completed, so the turn must still be running (this is exactly the
		// condition under which the old node-updates stream deadlocked).
		expect(team.isRunning()).toBe(true);
		// The operation was dispatched to the desktop (operation channel).
		expect(dispatched.length).toBe(1);
		const toolId = (
			(dispatched[0] as Record<string, unknown>).flowParts as {
				parts: { keyboardPress: { toolId: string } }[];
			}
		).parts[0].keyboardPress.toolId;

		// The desktop replies → the dispatch resolves → the agent loop
		// continues → the turn completes.
		bridge.handleResult({
			toolId,
			status: "TOOL_RESULT_STATUS_SUCCEEDED",
			message: "ok",
		});
		await flush();

		expect(team.isRunning()).toBe(false);
		expect(waitCount(frames)).toBe(1);
		// The turn streamed the full player exchange (tool_call + tool_result),
		// with the same block content the channel replay would have produced.
		const callFrames = partFrames(frames, "toolCall");
		expect(callFrames.length).toBe(1);
		const callPart = (
			(callFrames[0] as Record<string, unknown>).messageParts as {
				parts: { toolCall: { name: string; argsJson: string; toolId: string } }[];
			}
		).parts[0].toolCall;
		expect(callPart.name).toBe("async_saolei_move");
		expect(JSON.parse(callPart.argsJson)).toEqual({ x: 1, y: 1 });
		const resFrames = partFrames(frames, "toolResult");
		expect(resFrames.length).toBe(1);
		const resPart = (
			(resFrames[0] as Record<string, unknown>).messageParts as {
				parts: { toolResult: { status: string; message: string } }[];
			}
		).parts[0].toolResult;
		// The tool returned a plain string (no ToolMessage), so the status is
		// neutral UNSPECIFIED — same as the checkpointed ToolMessage.
		expect(resPart.status).toBe("TOOL_RESULT_STATUS_UNSPECIFIED");
		expect(resPart.message).toContain("status=TOOL_RESULT_STATUS_SUCCEEDED");
	});

	it("degrades a stalled init instruction turn within INIT_TURN_TIMEOUT_MS and does not block the next user turn (043 US4 — FR-009/FR-010)", async () => {
		// Fake graph whose `invoke` NEVER resolves on its own — it only
		// rejects when the config's `signal` aborts, mirroring what real
		// LangGraph does when the `AbortSignal.timeout` expires (runInitTurn
		// passes the signal in the invoke config — contract §4.1,
		// research.md R5). The degrade is exactly the existing catch: warn +
		// resolve (contract §4.2 UNCHANGED).
		const invoke = vi.fn(
			(_input: unknown, config?: { signal?: AbortSignal }) =>
				new Promise((_resolve, reject) => {
					const signal = config?.signal;
					signal?.addEventListener(
						"abort",
						() => reject(signal.reason ?? new Error("aborted")),
						{ once: true },
					);
				}),
		);
		// Post-degrade user turn: an empty stream completes immediately
		// (runTeamTurn awaits the already-resolved initTurn, then streams).
		const streamEvents = vi.fn(async function* () {});
		const handle = {
			graph: { invoke, streamEvents },
			checkpointer: new MemorySaver(),
		} as unknown as TeamGraphHandle;
		const buffer = createEphemeralGameBuffer();
		const team = new SessionTeam(
			handle,
			buffer,
			"s-043-init-timeout",
			TID,
			new OperationBridge(),
			createTeamSink(buffer),
		);

		// Capture the degrade warning through the logs package's Reporter
		// seam (style/javascript.md — DI, no module mock).
		const warnMessages: string[] = [];
		const reporter: Reporter = {
			write: (level, msg) => {
				if (level === "warn") warnMessages.push(msg);
			},
		};
		const uninstall = installReporter(reporter);
		try {
			team.triggerInitInstruction();

			// Positive assertion — the invoke was reached with the timeout
			// signal (the constant mock took effect; a missing signal would
			// leave the promise pending and fail the deadline below).
			expect(invoke).toHaveBeenCalledOnce();
			const config = invoke.mock.calls[0][1] as
				| { signal?: AbortSignal }
				| undefined;
			expect(config?.signal).toBeInstanceOf(AbortSignal);
			expect(config?.signal?.aborted).toBe(false);

			// The timeout fires at ~1000ms → the fake invoke rejects with
			// the signal reason → the existing catch degrades (warn +
			// resolve). isBusy() flips false exactly when the init promise
			// resolves (startInstructionTurn's finally).
			const deadline = Date.now() + 2000;
			while (team.isBusy() && Date.now() < deadline) {
				await new Promise((r) => setTimeout(r, 10));
			}
			expect(team.isBusy()).toBe(false);
			expect(team.isRunning()).toBe(false);
			expect(
				warnMessages.some((m) =>
					m.includes("init instruction turn failed; skipping initial instruction"),
				),
			).toBe(true);

			// FR-010: the degraded init promise no longer blocks the first
			// user turn — submit runs a normal turn against the fake graph's
			// empty stream and returns to idle.
			const { emit, frames } = recordingEmit();
			team.bindStreamSink(emit, emit);
			team.submit({ text: "hi" });
			expect(team.isRunning()).toBe(true);
			await flush();
			expect(team.isRunning()).toBe(false);
			expect(waitCount(frames)).toBe(1);
		} finally {
			uninstall();
		}
	});
});

describe("SessionTeam stream display sink (041 — contract §1.1-§1.3, FR-010)", () => {
	it("emitting while unbound is a no-op (best-effort, research.md D9)", async () => {
		const { team } = buildTestTeam("st-sink-unbound");
		const sink = vi.fn<(frame: TeamFrame) => void>();

		// No bindStreamSink: the full user turn (TurnLoop + compress/review
		// channel frames) emits into the void — nothing crashes, nothing is
		// delivered (specs/041-realtime-init-push/contracts/
		// realtime-channel-contract.md §1.2 "null → no-op"; the
		// seed/history path covers delivery instead — specs/041-realtime-init-push/
		// research.md D7 case A).
		team.submit({ text: "开始游戏" });
		await flush();

		expect(team.isRunning()).toBe(false);
		expect(sink).not.toHaveBeenCalled();
	});

	it("bound sink receives the turn's frames incl. the emitChannelFrame path (unified read path, contract §1.2)", async () => {
		const { team } = buildTestTeam("st-sink-bound");
		const sink = vi.fn<(frame: TeamFrame) => void>();

		team.bindStreamSink(sink, sink);
		team.submit({ text: "开始游戏" });
		await flush();

		expect(team.isRunning()).toBe(false);
		expect(sink).toHaveBeenCalled();
		// Both emission paths resolve the SAME sink
		// (specs/041-realtime-init-push/contracts/realtime-channel-contract.md
		// §1.2): the
		// TurnLoop's display frames (player agent) and the planner node's
		// emitChannelFrame review-input frame (planner.ts:321-344 —
		// agent=planner, role=USER, only reachable via emitChannelFrame).
		const frames = sink.mock.calls.map(([f]) => f);
		expect(frames.some((f) => f.agent === "player")).toBe(true);
		expect(
			frames.some(
				(f) => f.agent === "planner" && f.role === "MESSAGE_ROLE_USER",
			),
		).toBe(true);
	});

	it("clearStreamSink drops an in-flight turn's later emissions (contract §1.3 — no write to a dead connection)", async () => {
		// Hold the player turn in-flight on the gate, bind a sink, then clear
		// it (the stream died); releasing the turn must not reach the sink.
		const gate = makeGate();
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const gatedTool = tool(
			async ({ x, y }: { x: number; y: number }) => {
				await gate.promise;
				await sink.onGameEnd(makeState(), "won");
				return `moved to (${x},${y}); game won`;
			},
			{
				name: "fake_saolei_move",
				description: "Gated fake saolei move (holds the turn).",
				schema: z.object({ x: z.number(), y: z.number() }),
			},
		);
		const handle = buildTeamGraph({
			playerModel: playOneGamePlayerModel(),
			plannerModel: initThenReviewPlannerModel("初始指令", "复盘指令"),
			buffer,
			sessionId: "st-sink-clear",
			playerTools: [gatedTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
		});
		const team = new SessionTeam(
			handle,
			buffer,
			"st-sink-clear",
			TID,
			new OperationBridge(),
			sink,
		);
		const displaySink = vi.fn<(frame: TeamFrame) => void>();
		const received: TeamFrame[] = [];
		team.bindStreamSink((f) => {
			received.push(f);
			displaySink(f);
		}, displaySink);

		team.submit({ text: "开始游戏" });
		// Wait until the player's tool_call frame arrived through the sink
		// (the tool is still awaiting the gate — the turn is mid-flight).
		await waitForFrame(
			received,
			(f) => partFrames([f], "toolCall").length > 0,
			5000,
		);
		expect(team.isRunning()).toBe(true);
		const callsBeforeClear = displaySink.mock.calls.length;
		expect(callsBeforeClear).toBeGreaterThan(0);

		// The stream dies mid-turn → the handler clears the sink (contract
		// §1.3, FR-010). Releasing the gate completes the turn; every frame
		// emitted after the clear must be dropped (null sink).
		team.clearStreamSink(displaySink);
		gate.resolve();
		await flush();

		expect(team.isRunning()).toBe(false);
		expect(displaySink.mock.calls.length).toBe(callsBeforeClear);
	});

	it("clearStreamSink(oldHandle) does not clear a sink bound by a newer handle (compare-and-delete)", async () => {
		const { team } = buildTestTeam("st-sink-cmp");
		const oldSink = vi.fn<(frame: TeamFrame) => void>();
		const newSink = vi.fn<(frame: TeamFrame) => void>();

		team.bindStreamSink(oldSink, oldSink);
		team.bindStreamSink(newSink, newSink);
		// A superseded stream's end/error clears with ITS OWN handle — the
		// newer binding must survive
		// (specs/041-realtime-init-push/contracts/realtime-channel-contract.md
		// §1.1 compare-and-delete).
		team.clearStreamSink(oldSink);

		team.submit({ text: "开始游戏" });
		await flush();

		expect(newSink).toHaveBeenCalled();
		expect(oldSink).not.toHaveBeenCalled();
	});
});

describe("SessionTeam — init instruction frames (041 US1, T005/T006 — contract §2, FR-004/FR-006)", () => {
	it("emits the three init frames through a bound sink, each frameId == message id (contract §2.2 / data-model §3, §4)", async () => {
		const { team, sessionId } = buildTestTeam("s-041-init-frames");
		const store = new SessionTeamStore(async () => team);
		const sink = vi.fn<(frame: TeamFrame) => void>();

		// Bind BEFORE materialization: `update` triggers the one-shot init
		// turn fire-and-forget (R2 — 物化即返回，不等 LLM); with the fake
		// planner resolving synchronously the whole invoke completes inside
		// the microtask chain, so the sink must already be bound
		// (specs/041-realtime-init-push/research.md
		// D1 — in practice the Connect handler binds it on the first inbound
		// frame, specs/041-realtime-init-push/contracts/
		// realtime-channel-contract.md §1.1).
		team.bindStreamSink(sink, sink);
		await store.update(sessionId, "saolei", "default", true);
		await flush(0);

		const frames = sink.mock.calls.map(([f]) => f);
		const msgFrames = frames.filter((f) => {
			const fr = f as Record<string, unknown>;
			return fr.payload === "messageParts";
		});

		// Exactly the three init frames in production order (request →
		// response → write-back, specs/041-realtime-init-push/data-model.md
		// §3.3): planner request USER,
		// planner response toolCall AGENT, player write-back USER.
		expect(msgFrames.length).toBe(3);
		const requestFrame = msgFrames[0];
		expect(requestFrame.agent).toBe("planner");
		expect(requestFrame.role).toBe("MESSAGE_ROLE_USER");
		const responseFrames = partFrames(msgFrames, "toolCall");
		expect(responseFrames.length).toBe(1);
		expect(responseFrames[0].agent).toBe("planner");
		expect(responseFrames[0].role).toBe("MESSAGE_ROLE_AGENT");
		const writeBackFrame = msgFrames[2];
		expect(writeBackFrame.agent).toBe("player");
		expect(writeBackFrame.role).toBe("MESSAGE_ROLE_USER");

		// frameId == message id (dedup anchor,
		// specs/041-realtime-init-push/contracts/realtime-channel-contract.md
		// §4 / FR-004): every
		// frame's frameId equals the persisted message's id, so the seed /
		// history / real-time paths share one id namespace and the desktop
		// renders each message exactly once.
		const state = (await team.getTeamState()) as TeamStateValue;
		const requestMsg = state.plannerMessages.find(
			(m) =>
				typeof m.content === "string" && m.content.includes("团队初始化"),
		);
		const responseMsg = state.plannerMessages.find(
			(m) => m._getType() === "ai" && extractToolCalls(m).length > 0,
		);
		const writeBackMsg = state.playerMessages.find(
			(m) =>
				typeof m.content === "string" && m.content.includes("初始指令"),
		);
		expect(requestMsg).toBeDefined();
		expect(responseMsg).toBeDefined();
		expect(writeBackMsg).toBeDefined();
		expect(requestFrame.frameId).toBe(requestMsg?.id);
		expect(responseFrames[0].frameId).toBe(responseMsg?.id);
		expect(writeBackFrame.frameId).toBe(writeBackMsg?.id);
	});

	it("init emission with an unbound sink is a no-op; the instruction still persists (best-effort, research.md D9 — D7 case A)", async () => {
		const { team, sessionId } = buildTestTeam("s-041-init-unbound");
		const store = new SessionTeamStore(async () => team);
		const sink = vi.fn<(frame: TeamFrame) => void>();

		// Simulate "init completes before the desktop connects"
		// (specs/041-realtime-init-push/research.md
		// D7 case A): bind then immediately clear — the init turn's emits
		// resolve to null and are dropped
		// (specs/041-realtime-init-push/contracts/realtime-channel-contract.md
		// §1.2 no-op). The
		// persisted instruction is delivered by the one-shot seed /
		// loadAgentHistories on connect instead.
		team.bindStreamSink(sink, sink);
		team.clearStreamSink(sink);
		await store.update(sessionId, "saolei", "default", true);
		await flush(0);

		expect(sink).not.toHaveBeenCalled();
		const state = (await team.getTeamState()) as TeamStateValue;
		expect(
			state.playerMessages.some(
				(m) =>
					typeof m.content === "string" && m.content.includes("初始指令"),
			),
		).toBe(true);
	});
});

describe("SessionTeam — continuous-channel producers (041 US3 T009 — spec edge case 4)", () => {
	it("init emitter and a review-style emitter interleave through the same bound sink, agent-tagged, no frame lost (FR-006)", async () => {
		const { team, sessionId } = buildTestTeam("s-041-interleave");
		const store = new SessionTeamStore(async () => team);
		const sink = vi.fn<(frame: TeamFrame) => void>();
		team.bindStreamSink(sink, sink);

		// Producer A — the one-shot init turn (fire-and-forget via
		// store.update, session-team.ts triggerInitInstruction): the
		// instruction node emits its three frames (planner request / planner
		// toolCall response / player write-back,
		// specs/041-realtime-init-push/contracts/realtime-channel-contract.md
		// §2.2) through the
		// bound sink (specs/041-realtime-init-push/contracts/
		// realtime-channel-contract.md §2).
		await store.update(sessionId, "saolei", "default", true);
		await flush(0);

		// Producer B — a user turn whose review planner emits its review-input
		// frame through the SAME emitChannelFrame path (planner.ts:321-344 —
		// the compress/review-style emitter), interleaved with the TurnLoop's
		// player frames on the same sink. JS 单线程下 "并发" = 多个发射源在统一
		// sink 上按发射顺序交错，互不覆盖（spec edge case 4 — 每个任务经实时
		// 通道独立交付，标记产生它的 agent）。
		team.submit({ text: "开始游戏" });
		await flush();

		const msgFrames = sink.mock.calls
			.map(([f]) => f as TeamFrame)
			.filter(
				(f) => (f as Record<string, unknown>).payload === "messageParts",
			);

		// No frame lost: every init frame arrived on the one sink, keyed by
		// frameId == message id (dedup anchor,
		// specs/041-realtime-init-push/contracts/realtime-channel-contract.md
		// §4 / FR-004).
		const state = (await team.getTeamState()) as TeamStateValue;
		const requestMsg = state.plannerMessages.find(
			(m) =>
				typeof m.content === "string" && m.content.includes("团队初始化"),
		);
		const responseMsg = state.plannerMessages.find(
			(m) => m._getType() === "ai" && extractToolCalls(m).length > 0,
		);
		const writeBackMsg = state.playerMessages.find(
			(m) =>
				typeof m.content === "string" && m.content.includes("初始指令"),
		);
		expect(requestMsg).toBeDefined();
		expect(responseMsg).toBeDefined();
		expect(writeBackMsg).toBeDefined();
		const byFrameId = new Map(msgFrames.map((f) => [f.frameId, f]));
		expect(byFrameId.get(requestMsg?.id)?.agent).toBe("planner");
		expect(byFrameId.get(responseMsg?.id)?.agent).toBe("planner");
		expect(byFrameId.get(writeBackMsg?.id)?.agent).toBe("player");

		// The review-style emitter's frame (agent=planner, role=USER — the
		// review input HumanMessage, planner.ts:321-344). Its frameId is a
		// fresh randomUUID (buildTeamFrame default, turn-loop.ts:139) — NOT a
		// persisted message id — so it never collides with the init frames'
		// ids on the same sink (contract §4 dedup anchor).
		const initIds = new Set([requestMsg?.id, responseMsg?.id, writeBackMsg?.id]);
		expect(
			msgFrames.some(
				(f) =>
					f.agent === "planner" &&
					f.role === "MESSAGE_ROLE_USER" &&
					!initIds.has(f.frameId),
			),
		).toBe(true);

		// The TurnLoop's player frames also arrived, agent-tagged.
		expect(
			msgFrames.some(
				(f) => f.agent === "player" && f.role === "MESSAGE_ROLE_AGENT",
			),
		).toBe(true);

		// Every frame carries a producing agent for tab routing (FR-006).
		for (const f of msgFrames) {
			expect(["player", "planner"]).toContain(f.agent);
		}
	});
});

describe("SessionTeamStore", () => {
	it("update materializes once per session and forwards template+profile; get returns the cached team", async () => {
		const created: string[] = [];
		const seenArgs: Array<[string, string]> = [];
		const store = new SessionTeamStore(async (sessionId, template, profileName) => {
			created.push(sessionId);
			seenArgs.push([template, profileName]);
			return buildTestTeam(sessionId).team;
		});

		const t1 = await store.update("s-1", "saolei", "default", true);
		const t2 = await store.update("s-1", "saolei", "default", true);
		expect(t1).toBe(t2);
		expect(created).toEqual(["s-1"]);
		expect(seenArgs).toEqual([["saolei", "default"]]);
		expect(store.get("s-1")).toBe(t1);
		expect(store.getProfileName("s-1")).toBe("default");
		expect(store.get("s-2")).toBeUndefined();
	});

	it("update is idempotent for the same profile on an existing session (allow_missing irrelevant once materialized)", async () => {
		const created: string[] = [];
		const store = new SessionTeamStore(async (sessionId) => {
			created.push(sessionId);
			return buildTestTeam(sessionId).team;
		});

		const t1 = await store.update("s-2", "saolei", "default", true);
		const t2 = await store.update("s-2", "saolei", "default", false);
		expect(t1).toBe(t2);
		expect(created).toEqual(["s-2"]);
	});

	it("rebuilds the graph for a DIFFERENT profile, preserving state via the SAME checkpointer (FR-005)", async () => {
		const created: string[] = [];
		const rebuilt: string[] = [];
		const store = new SessionTeamStore(
			async (sessionId) => {
				created.push(sessionId);
				return buildTestTeam(sessionId).team;
			},
			// US3 rebuild seam: pass the existing checkpointer through (the
			// production rebuilder lives in server.ts — here we emulate it
			// with the same real graph wiring).
			async (sessionId, _template, profileName, existingCheckpointer) => {
				rebuilt.push(`${sessionId}:${profileName}`);
				return buildTestHandle(sessionId, existingCheckpointer);
			},
		);

		const t1 = await store.update("s-2", "saolei", "default", true);
		// Produce conversation/game history on the session thread.
		const { emit } = recordingEmit();
		t1.bindStreamSink(emit, emit);
		t1.submit({ text: "开始游戏" });
		await flush();
		const before = (await t1.getTeamState()) as TeamStateValue;
		expect(before.playerMessages.length).toBeGreaterThan(0);
		expect(before.plannerMessages.length).toBeGreaterThan(0);
		const checkpointerBefore = t1.getCheckpointer();

		// A different profile triggers the rebuild — regardless of
		// allow_missing (the team already exists, FR-002/FR-005).
		const t2 = await store.update("s-2", "saolei", "other", true);
		const t3 = await store.update("s-2", "saolei", "other", false);
		expect(t2).toBe(t1);
		expect(t3).toBe(t1);
		// The team's profile (GetTeam source) is the NEW one.
		expect(store.getProfileName("s-2")).toBe("other");
		// team-rebuild-contract.md §7: the checkpointer reference is UNCHANGED
		// (the rebuild must never create a new MemorySaver — that would drop
		// the history, violating FR-005).
		expect(t1.getCheckpointer()).toBe(checkpointerBefore);
		// History is preserved: same message count AND same content (零丢失/
		// 零重复 — the thread's channel state survives the recompile).
		const after = (await t1.getTeamState()) as TeamStateValue;
		expect(after.playerMessages).toEqual(before.playerMessages);
		expect(after.plannerMessages).toEqual(before.plannerMessages);
		// Exactly ONE first build and ONE rebuild (the repeated "other" call
		// was idempotent — profile already applied).
		expect(created).toEqual(["s-2"]);
		expect(rebuilt).toEqual(["s-2:other"]);
		// The same-profile idempotent path still works after the rebuild.
		expect(await store.update("s-2", "saolei", "other", true)).toBe(t1);
		expect(rebuilt).toEqual(["s-2:other"]);
	});

	it("update returns NOT_FOUND for a missing session when allow_missing=false (AIP-134)", async () => {
		const created: string[] = [];
		const store = new SessionTeamStore(async (sessionId) => {
			created.push(sessionId);
			return buildTestTeam(sessionId).team;
		});

		await expect(
			store.update("s-missing", "saolei", "default", false),
		).rejects.toMatchObject({ code: grpc.status.NOT_FOUND });
		// No factory call, no team row — the failure is a pure NOT_FOUND.
		expect(created).toEqual([]);
		expect(store.get("s-missing")).toBeUndefined();
	});

	it("single-flights concurrent updates for the same session", async () => {
		const created: string[] = [];
		const store = new SessionTeamStore(async (sessionId) => {
			created.push(sessionId);
			return buildTestTeam(sessionId).team;
		});

		const [t1, t2] = await Promise.all([
			store.update("s-3", "saolei", "default", true),
			store.update("s-3", "saolei", "default", true),
		]);
		expect(t1).toBe(t2);
		expect(created).toEqual(["s-3"]);
	});

	it("single-flights concurrent REBUILDS for the same session (exactly one rebuild, team-rebuild-contract.md §7)", async () => {
		const created: string[] = [];
		const rebuilt: string[] = [];
		const store = new SessionTeamStore(
			async (sessionId) => {
				created.push(sessionId);
				return buildTestTeam(sessionId).team;
			},
			async (sessionId, _template, profileName, existingCheckpointer) => {
				rebuilt.push(`${sessionId}:${profileName}`);
				// Give the second caller time to join the in-flight promise.
				await flush(0);
				return buildTestHandle(sessionId, existingCheckpointer);
			},
		);
		await store.update("s-sf", "saolei", "default", true);
		// Wait for the one-shot async initInstruction turn (triggered at
		// materialization — T029): the rebuild gate (`isBusy()`) includes
		// initInFlight (Phase 6 review Issue #5), so a rebuild during the
		// init would be rejected FAILED_PRECONDITION.
		await flush(0);

		const [r1, r2] = await Promise.allSettled([
			store.update("s-sf", "saolei", "p-a", true),
			store.update("s-sf", "saolei", "p-a", true),
		]);
		// The second caller single-flighted onto the first's rebuild and then
		// re-entered the dispatch idempotently — exactly ONE rebuild ran.
		expect(r1.status).toBe("fulfilled");
		expect(r2.status).toBe("fulfilled");
		expect(created).toEqual(["s-sf"]);
		expect(rebuilt).toEqual(["s-sf:p-a"]);
		expect(store.getProfileName("s-sf")).toBe("p-a");
	});

	it("rejects a profile-change rebuild while a turn is in-flight with FAILED_PRECONDITION (FR-006)", async () => {
		let release!: () => void;
		const gate = new Promise<void>((r) => {
			release = r;
		});
		// A team whose player tool blocks on the gate keeps the turn in-flight.
		const buffer = createEphemeralGameBuffer();
		const sink = createTeamSink(buffer);
		const gatedTool = tool(
			async ({ x, y }: { x: number; y: number }) => {
				await gate;
				await sink.onGameEnd(makeState(), "won");
				return `moved to (${x},${y}); game won`;
			},
			{
				name: "fake_saolei_move",
				description: "Gated fake saolei move (holds the turn).",
				schema: z.object({ x: z.number(), y: z.number() }),
			},
		);
		const handle = buildTeamGraph({
			playerModel: fakeModel()
				.respondWithTools([
					{ name: "fake_saolei_move", args: { x: 1, y: 1 } },
				])
				.respond(new AIMessage("won, stopping")),
			// 4 responses: the async initInstruction turn (triggered once at
			// materialization — T029) + the review turn after the gate
			// release.
			plannerModel: initThenReviewPlannerModel("初始指令", "复盘指令"),
			buffer,
			sessionId: "s-inflight",
			playerTools: [gatedTool],
			playerBasePrompt: "",
			plannerBasePrompt: "",
			...memoryDeps(),
		});
		const team = new SessionTeam(
			handle,
			buffer,
			"s-inflight",
			TID,
			new OperationBridge(),
			sink,
		);
		const store = new SessionTeamStore(
			async () => team,
			async (sessionId, _template, profileName, existingCheckpointer) => {
				return buildTestHandle(sessionId, existingCheckpointer);
			},
		);
		await store.update("s-inflight", "saolei", "default", true);
		expect(store.getProfileName("s-inflight")).toBe("default");

		// Start a turn that blocks on the gate.
		const { emit } = recordingEmit();
		team.bindStreamSink(emit, emit);
		team.submit({ text: "开始游戏" });
		await flush(0);
		expect(team.isRunning()).toBe(true);

		// FR-006: rejected with FAILED_PRECONDITION; the existing team and the
		// in-flight turn are untouched.
		await expect(
			store.update("s-inflight", "saolei", "other", true),
		).rejects.toMatchObject({ code: grpc.status.FAILED_PRECONDITION });
		expect(store.getProfileName("s-inflight")).toBe("default");
		expect(store.get("s-inflight")).toBe(team);
		expect(team.isRunning()).toBe(true);

		// Release the gate: the turn completes, and the rebuild then succeeds.
		release();
		await flush();
		expect(team.isRunning()).toBe(false);
		const t2 = await store.update("s-inflight", "saolei", "other", true);
		expect(t2).toBe(team);
		expect(store.getProfileName("s-inflight")).toBe("other");
	});

	it("rejects a profile-change rebuild while the INIT turn is in-flight (041 FR-007)", async () => {
		// FR-007 (specs/041-realtime-init-push/spec.md): the init turn gates
		// destructive operations through `isBusy()` while `isRunning()` (the
		// status probe) excludes it — session-team.ts:546-563,
		// specs/041-realtime-init-push/contracts/realtime-channel-contract.md
		// §5. The user-turn case is
		// covered above; this is the init-only scenario (no user turn at
		// all). A rebuild during the init would race the freshly written
		// instruction in `playerMessages`
		// (specs/039-planner-memory-calibration/contracts/team-graph-contract.md
		// §7).
		const store = new SessionTeamStore(
			async (sessionId) => buildTestTeam(sessionId).team,
			async (sessionId, _template, _profileName, existingCheckpointer) =>
				buildTestHandle(sessionId, existingCheckpointer),
		);
		const t1 = await store.update("s-rebuild-init", "saolei", "default", true);
		// Fire-and-forget init turn (session-team.ts:925 — 物化即返回) is
		// still in-flight right after materialization.
		expect(t1.isRunning()).toBe(false);
		expect(t1.isBusy()).toBe(true);

		await expect(
			store.update("s-rebuild-init", "saolei", "other", true),
		).rejects.toMatchObject({ code: grpc.status.FAILED_PRECONDITION });
		expect(store.getProfileName("s-rebuild-init")).toBe("default");
		expect(store.get("s-rebuild-init")).toBe(t1);

		// The init finishes → busy clears → the rebuild now succeeds.
		await flush(0);
		expect(t1.isBusy()).toBe(false);
		const t2 = await store.update("s-rebuild-init", "saolei", "other", true);
		expect(t2).toBe(t1);
		expect(store.getProfileName("s-rebuild-init")).toBe("other");
	});

	it("leaves the existing team unchanged when the rebuild fails (no half-rebuilt state)", async () => {
		const store = new SessionTeamStore(
			async (sessionId) => {
				return buildTestTeam(sessionId).team;
			},
			async () => {
				throw new Error("new model unavailable");
			},
		);
		const t1 = await store.update("s-fail", "saolei", "default", true);
		const { emit } = recordingEmit();
		t1.bindStreamSink(emit, emit);
		t1.submit({ text: "开始游戏" });
		await flush();
		const before = (await t1.getTeamState()) as TeamStateValue;
		expect(before.playerMessages.length).toBeGreaterThan(0);
		const checkpointerBefore = t1.getCheckpointer();

		// The rebuild fails (e.g. the new profile's model cannot be resolved):
		// the existing team, profile, checkpointer and history are unchanged
		// (team-rebuild-contract.md §5 异常路径 — no half-rebuilt state).
		await expect(
			store.update("s-fail", "saolei", "other", true),
		).rejects.toThrow("new model unavailable");
		expect(store.getProfileName("s-fail")).toBe("default");
		expect(store.get("s-fail")).toBe(t1);
		expect(t1.getCheckpointer()).toBe(checkpointerBefore);
		const after = (await t1.getTeamState()) as TeamStateValue;
		expect(after.playerMessages).toEqual(before.playerMessages);
		expect(after.plannerMessages).toEqual(before.plannerMessages);
		// The existing team still serves normally (idempotent same-profile
		// update).
		expect(await store.update("s-fail", "saolei", "default", true)).toBe(t1);
	});

	it("single-flight loser with a DIFFERENT profile rebuilds after the winner completes (US3)", async () => {
		const created: string[] = [];
		const rebuilt: string[] = [];
		const store = new SessionTeamStore(
			async (sessionId) => {
				created.push(sessionId);
				return buildTestTeam(sessionId).team;
			},
			async (sessionId, _template, profileName, existingCheckpointer) => {
				rebuilt.push(`${sessionId}:${profileName}`);
				return buildTestHandle(sessionId, existingCheckpointer);
			},
		);

		// Materialize "default" first and wait for the one-shot async
		// initInstruction turn (triggered at materialization — T029):
		// the rebuild gate (`isBusy()`) includes initInFlight (Phase 6
		// review Issue #5), so a rebuild racing the init would be rejected
		// FAILED_PRECONDITION. The concurrent different-profile updates
		// below then exercise the single-flight loser-rebuilds-after-winner
		// path.
		await store.update("s-3", "saolei", "default", true);
		await flush(0);

		const [t1, t2] = await Promise.allSettled([
			store.update("s-3", "saolei", "other", true),
			store.update("s-3", "saolei", "other", true),
		]);
		// US3: the loser (different profile) re-enters the dispatch after the
		// winner materialized "default" and REBUILDS to "other" — both
		// succeed (the MVP rejection is gone, FR-005/FR-007).
		expect(t1.status).toBe("fulfilled");
		expect(t2.status).toBe("fulfilled");
		expect(store.get("s-3")).toBe(
			t1.status === "fulfilled" ? t1.value : undefined,
		);
		expect(store.getProfileName("s-3")).toBe("other");
		// Only ONE team was ever built (single-flight) and exactly ONE rebuild
		// ran for the loser's profile.
		expect(created).toEqual(["s-3"]);
		expect(rebuilt).toEqual(["s-3:other"]);
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

describe("SessionTeamStore — async initInstruction (039 US3 T029, contract §6 / FR-015/R2)", () => {
	it("triggers the one-shot initInstruction on FIRST materialization; the instruction precedes the first user turn", async () => {
		const created: string[] = [];
		const store = new SessionTeamStore(async (sessionId) => {
			created.push(sessionId);
			return buildTestTeam(sessionId).team;
		});

		// `UpdateTeam(allow_missing=true)` 物化即返回（R2 — 不等 LLM）；init
		// turn 异步触发（fakeModel 同步响应，故在微任务内完成）。
		const team = await store.update("s-init", "saolei", "default", true);
		expect(created).toEqual(["s-init"]);
		await flush(0);

		// The init turn produced the no-game-history instruction into
		// `playerMessages` (LLM decided to call instruct_player — channel
		// write-back, no pending slot).
		const state = await team.getTeamState();
		expect(
			state?.playerMessages.some(
				(m) =>
					typeof m.content === "string" && m.content.includes("初始指令"),
			),
		).toBe(true);

		// First user turn: the instruction is already in the player channel
		// BEFORE the user input is appended (FR-015 — user message 排在指令
		// 之后) and stays in the history (累积可引用).
		const { emit } = recordingEmit();
		team.bindStreamSink(emit, emit);
		team.submit({ text: "开始游戏" });
		await flush();

		const after = (await team.getTeamState()) as TeamStateValue;
		// The instruction remains part of the player's channel history (D6 —
		// visible, referenceable, compressible).
		expect(
			after.playerMessages.some(
				(m) =>
					typeof m.content === "string" && m.content.includes("初始指令"),
			),
		).toBe(true);
	});

	it("queues a user message arriving during the async init AFTER the instruction (FR-015 ordering)", async () => {
		// The init turn is triggered fire-and-forget by `update` (R2 — 物化即
		// 返回，不等 LLM): the async graph.invoke is still in its microtask
		// chain when the user message arrives. The TurnLoop queues the
		// message and its turn awaits the init turn completion first — so the
		// produced instruction is already in `playerMessages` BEFORE the
		// queued user input is appended (player 首次激活先注入指令).
		const { team } = buildTestTeam("s-init-order");
		const store = new SessionTeamStore(async () => team);
		await store.update("s-init-order", "saolei", "default", true);

		// Submit synchronously right after materialization — the async init
		// turn has started but not completed yet.
		const { emit } = recordingEmit();
		team.bindStreamSink(emit, emit);
		team.submit({ text: "开始游戏" });
		await flush();

		const after = (await team.getTeamState()) as TeamStateValue;
		// The instruction HumanMessage was written into the player channel
		// BEFORE the first activation ran (累积可引用, survey D6) and
		// precedes the queued user input (FR-015 order).
		const instructionIndex = after.playerMessages.findIndex(
			(m) =>
				m._getType() === "human" &&
				typeof m.content === "string" &&
				m.content.includes("初始指令"),
		);
		const userIndex = after.playerMessages.findIndex(
			(m) =>
				m._getType() === "human" &&
				// The user turn message carries content-BLOCKS (built by
				// buildContentBlocks in runTeamTurn), not a plain string.
				typeof m.content !== "string" &&
				JSON.stringify(m.content).includes("开始游戏"),
		);
		expect(instructionIndex).toBeGreaterThanOrEqual(0);
		expect(instructionIndex).toBeLessThan(userIndex);
	});

	it("does NOT re-run initInstruction on a profile-change rebuild (040 FR-005 — init runs once at first materialization only)", async () => {
		const store = new SessionTeamStore(
			async (sessionId) => buildTestTeam(sessionId).team,
			async (sessionId, _template, _profileName, existingCheckpointer) => {
				return buildTestHandle(sessionId, existingCheckpointer);
			},
		);
		const t1 = await store.update("s-reinit", "saolei", "default", true);
		await flush(0);
		expect(
			(await t1.getTeamState())?.playerMessages.some(
				(m) =>
					typeof m.content === "string" && m.content.includes("初始指令"),
			),
		).toBe(true);

		// First activation: the instruction is in the channel history.
		const { emit } = recordingEmit();
		t1.bindStreamSink(emit, emit);
		t1.submit({ text: "开始游戏" });
		await flush();

		// Profile-change rebuild: the graph is recompiled against the
		// existing checkpointer but init is NOT re-triggered (the rebuild
		// handle's planner model serves only review turns — a re-run would
		// consume it and write a fresh instruction).
		const t2 = await store.update("s-reinit", "saolei", "other", true);
		expect(t2).toBe(t1);
		const state = (await t1.getTeamState()) as TeamStateValue;
		// The channel still holds exactly ONE instruction HumanMessage (no
		// re-run — a re-run would append a SECOND "初始指令").
		const instructionCount = state.playerMessages.filter(
			(m) =>
				m._getType() === "human" &&
				typeof m.content === "string" &&
				m.content.includes("初始指令"),
		).length;
		expect(instructionCount).toBe(1);
		// Session history survived the rebuild (FR-005).
		expect(state.playerMessages.length).toBeGreaterThan(0);
	});

	it("excludes the init turn from isRunning() but keeps it in isBusy() (status probe vs. rebuild/refresh gate)", async () => {
		// Deploy bugfix (039 US3): the Connect status probe derives its
		// signal from `isRunning()` alone. The one-shot async init turn runs
		// OUTSIDE the TurnLoop (graph.invoke, fire-and-forget) and emits no
		// `wait` frame — reporting ACTIVE for it would stick the desktop's
		// typing indicator on with no way to clear it (one-shot probe).
		// Hence isRunning() must exclude initInFlight while isBusy() (the
		// RefreshTeam / profile-change rebuild gate, contract §7) must keep
		// it.
		const store = new SessionTeamStore(async (sessionId) =>
			buildTestTeam(sessionId).team,
		);
		const team = await store.update("s-init-busy", "saolei", "default", true);

		// Right after materialization the async init turn is in-flight (R2 —
		// fire-and-forget, 物化即返回): no TurnLoop turn has started, so the
		// status probe must NOT report ACTIVE while the busy gate still
		// rejects a destructive operation.
		expect(team.isRunning()).toBe(false);
		expect(team.isBusy()).toBe(true);

		await flush(0);
		expect(team.isRunning()).toBe(false);
		expect(team.isBusy()).toBe(false);
	});
});
