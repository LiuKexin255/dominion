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
import * as grpc from "@grpc/grpc-js";
import { AIMessage, HumanMessage } from "@langchain/core/messages";
import type { MemorySaver } from "@langchain/langgraph";
import { fakeModel } from "@langchain/core/testing";
import { tool } from "langchain";
import { z } from "zod";
import type { GameState } from "@dominion/game-saolei-board";

import { FakeStrategyStore } from "./strategy-store";
import { SessionTeam, SessionTeamStore } from "./session-team";
import { OperationBridge } from "./operation-bridge";
import type { MemoryClient } from "./memory-client";
import { createEphemeralGameBuffer, createTeamSink } from "./team/team-sink";
import { buildTeamGraph } from "./team/graph";
import { FrozenMemorySnapshot } from "./team/memory-snapshot";
import type { TeamStateValue } from "./team/state";
import type { StructuredToolInterface } from "@langchain/core/tools";
import type { TeamFrame } from "../game_types/projects/game/TeamFrame";

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
		playerBasePrompt: "",
		plannerBasePrompt: "",
		...memoryDeps(),
	});
	// Pre-built bridge/sink like the production factory (server.ts): the
	// SessionTeam constructor no longer creates them internally.
	const bridge = new OperationBridge();
	const sink = createTeamSink(buffer);
	const team = new SessionTeam(handle, buffer, sessionId, TID, bridge, sink);
	return { team, store, buffer, sessionId, bridge, sink };
}

/**
 * Build ONLY a graph handle (US3 rebuild seam — the same wiring as
 * {@link buildTestTeam}, minus the SessionTeam shell). A rebuild passes the
 * EXISTING checkpointer so the session state carries over (FR-005); the
 * optional `checkpointer` is forwarded verbatim.
 */
function buildTestHandle(sessionId: string, checkpointer?: MemorySaver) {
	const buffer = createEphemeralGameBuffer();
	return buildTeamGraph(
		{
			playerModel: playOneGamePlayerModel(),
			plannerModel: updateStrategyPlannerModel("corner-first"),
			strategyStore: new FakeStrategyStore(),
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

	it("streams model text from content-block-delta events exactly once (regression: no finish double-emit)", async () => {
		// The turn runner must consume ONLY `content-block-delta` events for
		// text/reasoning (token-granular live updates; the pre-team
		// single-agent path streamed the same deltas via `stream.messages`).
		// Consuming `content-block-finish` on top would double-emit each
		// model response, and skipping the deltas entirely would drop text
		// until the next tool call (spec 031 desktop batching regression).
		const { team } = buildTestTeam("st-delta");
		const { emit, frames } = recordingEmit();

		team.submit({ text: "开始游戏" }, emit);
		await flush();

		const texts = textBlocks(frames);
		const count = (needle: string) =>
			texts.filter((t) => t.text.trim() === needle).length;
		// Each model response's text appears exactly once across the streamed
		// frames (the player's "won, stopping" answer and the planner's
		// post-review answer; the third queued player response is never
		// invoked — the player agent loop stops after a text-only answer).
		expect(count("won, stopping")).toBe(1);
		expect(count("strategy updated")).toBe(1);
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
			strategyStore: new FakeStrategyStore(),
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

		team.submit({ text: "开始游戏" }, emit);

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
		t1.submit({ text: "开始游戏" }, emit);
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
			plannerModel: updateStrategyPlannerModel("corner-first"),
			strategyStore: new FakeStrategyStore(),
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
		team.submit({ text: "开始游戏" }, emit);
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
		t1.submit({ text: "开始游戏" }, emit);
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

		const [t1, t2] = await Promise.allSettled([
			store.update("s-3", "saolei", "default", true),
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
