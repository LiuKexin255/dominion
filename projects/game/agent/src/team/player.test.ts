/**
 * team/player.test.ts — Unit tests for the player node (Feature 038 T002,
 * US1 middleware wiring + 039 US3 T028 pending-instruction consumption).
 *
 * - The `queueDrain` `beforeModel` middleware drains the TurnLoop buffer
 *   through `runtime.configurable.drainQueuedInput` before EVERY model call
 *   and injects the queued content as a `HumanMessage` — the turn's first
 *   model call AND each call after a tool result (FR-001, spec v2 —
 *   `specs/038-queue-input-mid-turn/contracts/injection-seam-contract.md`
 *   §3).
 * - The player consumes calibration instructions as PLAIN channel history
 *   (039 US3): the init/compact instruction nodes write `playerMessages`
 *   directly (instruction-node.ts), so the node's input is simply
 *   `state.playerMessages` — no pending slot, no extra consumption step
 *   (FR-015/FR-016).
 *
 * Mock strategy (`style/javascript.md` §Mock): the node's `createAgentFn`
 * DI seam is injected as a `vi.fn()` spy that captures the middleware
 * config / invoke inputs; the hooks are then invoked directly with a fake
 * runtime — no `vi.mock` module interception (see
 * [vitest — Mocking Modules Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)).
 */

import { describe, expect, it, vi } from "vitest";
import { AIMessage, HumanMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import { NodeTimeoutError } from "@langchain/langgraph";

import { buildContentBlocks } from "../llm";
import type { TurnContent } from "../llm";
import type { ChatModel } from "../model-provider";
import { createEphemeralGameBuffer } from "./team-sink";
import { createPlayerNode } from "./player";
import type { TeamStateValue } from "./state";

/** The middleware config shape the node passes to `createAgent`. */
interface CapturedMiddleware {
	name: string;
	beforeModel: {
		canJumpTo?: string[];
		hook: (state: unknown, runtime: unknown) => unknown;
	};
}

/**
 * Build the player node with a fake `createAgentFn` (DI seam) and capture
 * the middleware array it receives. The fake agent is never invoked — only
 * the middleware config is asserted.
 */
function buildPlayerWithMiddleware(): { middleware: CapturedMiddleware[] } {
	const createAgentFn = vi.fn(
		(config: { middleware?: CapturedMiddleware[] }) => ({
			invoke: async () => ({ messages: [] as BaseMessage[] }),
		}),
	);
	createPlayerNode({
		model: {} as ChatModel,
		buffer: createEphemeralGameBuffer(),
		sessionId: "player-test",
		tools: [],
		playerBasePrompt: "",
		createAgentFn,
	});
	// Positive assertion the DI seam was actually exercised
	// (style/javascript.md §测试: a silent, un-intercepted fake is a
	// false-green).
	expect(createAgentFn).toHaveBeenCalled();
	const middleware = createAgentFn.mock.calls[0]?.[0].middleware;
	if (!middleware) throw new Error("createAgent middleware was not captured");
	return { middleware };
}

/** Find the `queueDrain` middleware entry and return its beforeModel hook. */
function queueDrainHook(
	middleware: CapturedMiddleware[],
): (state: unknown, runtime: unknown) => unknown {
	const entry = middleware.find((m) => m.name === "queueDrain");
	if (!entry) throw new Error("queueDrain middleware not registered");
	return entry.beforeModel.hook;
}

describe("player createAgent middleware — queueDrain (feature 038 T002)", () => {
	it("registers queueDrain as the second middleware entry after gameEndGuard", () => {
		const { middleware } = buildPlayerWithMiddleware();
		expect(middleware.map((m) => m.name)).toEqual([
			"gameEndGuard",
			"queueDrain",
		]);
	});

	it("returns { messages: [HumanMessage] } when the drain callback returns content", () => {
		const { middleware } = buildPlayerWithMiddleware();
		const hook = queueDrainHook(middleware);
		const drained: TurnContent = { text: "steer mid-turn" };
		const drain = vi.fn(() => drained);

		const result = hook(
			{ messages: [] },
			{ configurable: { thread_id: "t", drainQueuedInput: drain } },
		) as { messages: BaseMessage[] };

		// The drain callback was actually exercised (style/javascript.md).
		expect(drain).toHaveBeenCalledOnce();
		expect(result).toBeDefined();
		const [msg] = result.messages;
		// The injected message is a HumanMessage whose content is built by
		// the shared buildContentBlocks — the identical content-block shape
		// as the turn-start message (injection-seam-contract.md §3).
		expect(msg).toBeInstanceOf(HumanMessage);
		expect(msg.content).toEqual(buildContentBlocks(drained));
	});

	it("returns undefined (no-op) when the drain callback returns null", () => {
		const { middleware } = buildPlayerWithMiddleware();
		const hook = queueDrainHook(middleware);
		const drain = vi.fn(() => null);

		const result = hook(
			{ messages: [] },
			{ configurable: { thread_id: "t", drainQueuedInput: drain } },
		);

		expect(drain).toHaveBeenCalledOnce();
		expect(result).toBeUndefined();
	});

	it("returns undefined (no-op) when drainQueuedInput is absent from configurable", () => {
		const { middleware } = buildPlayerWithMiddleware();
		const hook = queueDrainHook(middleware);

		// No drainQueuedInput key — and no configurable at all (the runtime
		// field is optional per the middleware Runtime type): both are no-ops.
		expect(
			hook({ messages: [] }, { configurable: { thread_id: "t" } }),
		).toBeUndefined();
		expect(hook({ messages: [] }, {})).toBeUndefined();
	});
});

describe("player node — channel history flow (039 US3 — instructions arrive as plain playerMessages)", () => {
	/** Build a node whose fake agent captures the invoke input. */
	function buildCapturingPlayerNode() {
		const captured: { messages: BaseMessage[] }[] = [];
		const createAgentFn = vi.fn(() => ({
			invoke: async ({ messages }: { messages: BaseMessage[] }) => {
				captured.push({ messages });
				return { messages: [new AIMessage("收到，继续游戏")] };
			},
		}));
		const node = createPlayerNode({
			model: {} as ChatModel,
			buffer: createEphemeralGameBuffer(),
			sessionId: "player-test",
			tools: [],
			playerBasePrompt: "",
			createAgentFn,
		});
		return { node, captured, createAgentFn };
	}

	function stateWith(playerMessages: BaseMessage[]): TeamStateValue {
		return {
			playerMessages,
			plannerMessages: [],
			gameEnded: null,
			gameCounter: 0,
		};
	}

	it("passes the channel history (including a calibration instruction HumanMessage) straight into the model input", async () => {
		const { node, captured, createAgentFn } = buildCapturingPlayerNode();
		// The instruction node wrote the calibration instruction into
		// `playerMessages`; the user's message follows it (channel order).
		const state = stateWith([
			new HumanMessage("优先清理边角雷区"),
			new HumanMessage("历史消息"),
		]);

		const result = await node(state, {
			configurable: { thread_id: "t" },
		});

		// The DI seam was actually exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
		// The model input is the channel history verbatim — the instruction
		// is consumed as a normal conversation message, no extra step
		// (FR-015/FR-016 — "后续输入只需正常拼接 history").
		const input = captured[0]?.messages ?? [];
		expect(input).toHaveLength(2);
		expect(String(input[0].content)).toBe("优先清理边角雷区");
		expect(String(input[1].content)).toBe("历史消息");
		// Only the agent output is returned (the channel's existing messages
		// are preserved by the messagesStateReducer append/dedup).
		const written = result.playerMessages ?? [];
		expect(written).toHaveLength(1);
		expect(String(written[0].content)).toBe("收到，继续游戏");
	});

	it("passes an empty channel as an empty input", async () => {
		const { node, captured } = buildCapturingPlayerNode();

		const result = await node(stateWith([]), {
			configurable: { thread_id: "t" },
		});

		const input = captured[0]?.messages ?? [];
		expect(input).toHaveLength(0);
		const written = result.playerMessages ?? [];
		expect(written).toHaveLength(1);
	});

	it("returns the agent's output messages when the invoke throws (try/finally — Issue 1 semantics)", async () => {
		const createAgentFn = vi.fn(() => ({
			invoke: async () => {
				throw new Error("player agent loop crashed");
			},
		}));
		const node = createPlayerNode({
			model: {} as ChatModel,
			buffer: createEphemeralGameBuffer(),
			sessionId: "player-test",
			tools: [],
			playerBasePrompt: "",
			createAgentFn,
		});

		const result = await node(
			stateWith([new HumanMessage("指令在通道中")]),
			{ configurable: { thread_id: "t" } },
		);

		// The invoke threw → no output messages; the channel write-back is
		// empty (the channel's existing instruction stays untouched — the
		// messagesStateReducer appends only what the node returns).
		const written = result.playerMessages ?? [];
		expect(written).toHaveLength(0);
	});
});

describe("player node — stall classification (043 US1: NodeTimeoutError re-throw)", () => {
	function stateWith(playerMessages: BaseMessage[]): TeamStateValue {
		return {
			playerMessages,
			plannerMessages: [],
			gameEnded: null,
			gameCounter: 0,
		};
	}

	/** Buffer pre-loaded with an unconsumed game-end event (the post-process
	 *  must consume it on EVERY path — contract §2.3). */
	function bufferWithGameEvent() {
		const buffer = createEphemeralGameBuffer();
		buffer.gameEvent = {
			state: { width: 1, height: 1, grid: [["0"]] },
			status: "won",
			endedAt: Date.now(),
			consumed: false,
		};
		return buffer;
	}

	it("re-throws NodeTimeoutError from the agent invoke (the stall must reach the runLoop, not be swallowed)", async () => {
		const buffer = bufferWithGameEvent();
		// The real LangGraph error class: `isNodeTimeoutError` is a duck-typed
		// guard (`e.name === "NodeTimeoutError"` — dist/errors.js), and the
		// real class produces exactly that name plus the node/kind/elapsed
		// fields (errors.d.ts).
		const timeoutError = new NodeTimeoutError({
			node: "player",
			kind: "idle",
			idleTimeout: 30000,
			elapsed: 30001,
		});
		const createAgentFn = vi.fn(() => ({
			invoke: async () => {
				throw timeoutError;
			},
		}));
		const node = createPlayerNode({
			model: {} as ChatModel,
			buffer,
			sessionId: "player-test",
			tools: [],
			playerBasePrompt: "",
			createAgentFn,
		});

		// The DI seam was actually exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
		await expect(
			node(stateWith([new HumanMessage("开始游戏")]), {
				configurable: { thread_id: "t-timeout" },
			}),
		).rejects.toMatchObject({
			name: "NodeTimeoutError",
			node: "player",
			kind: "idle",
		});
		// `specs/043-llm-stream-stall-recovery/contracts/stall-recovery-contract.md` §2.3:
		// the game-end event was consumed on the timeout path too (the
		// re-throw happens AFTER consumeGameEvent).
		expect(buffer.gameEvent?.consumed).toBe(true);
	});

	it("swallows a generic Error and returns normally (FR-036 FR-002 compatibility)", async () => {
		const buffer = bufferWithGameEvent();
		const createAgentFn = vi.fn(() => ({
			invoke: async () => {
				throw new Error("player agent loop crashed");
			},
		}));
		const node = createPlayerNode({
			model: {} as ChatModel,
			buffer,
			sessionId: "player-test",
			tools: [],
			playerBasePrompt: "",
			createAgentFn,
		});

		const result = await node(
			stateWith([new HumanMessage("指令在通道中")]),
			{ configurable: { thread_id: "t-swallow" } },
		);

		// The node returned normally (did NOT re-throw) — the invoke threw →
		// no output messages, but the game-end event was consumed and set
		// gameEnded (the conditional edge routes to the planner, FR-036).
		expect(createAgentFn).toHaveBeenCalled();
		expect(result.playerMessages ?? []).toHaveLength(0);
		expect(result.gameEnded).toBe("won");
		expect(buffer.gameEvent?.consumed).toBe(true);
	});
});
