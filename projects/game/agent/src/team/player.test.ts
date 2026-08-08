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
 * - The player entry consumes `state.pendingInstruction` (the init/compact
 *   scenarios' deferred instruction slot — T028, contract §2.1): a non-null
 *   instruction is injected as a `HumanMessage` into the player input AND
 *   written back to the channel (accumulates in the conversation flow,
 *   survey D6), and the slot is cleared (`{pendingInstruction: null}`).
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

describe("player node — pendingInstruction consumption (039 US3 T028, contract §2.1)", () => {
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

	function stateWith(pendingInstruction: string | null): TeamStateValue {
		return {
			playerMessages: [new HumanMessage("历史消息")],
			plannerMessages: [],
			gameEnded: null,
			gameCounter: 0,
			pendingInstruction,
		};
	}

	it("injects a non-null pendingInstruction as a HumanMessage (first in input) and clears the slot", async () => {
		const { node, captured, createAgentFn } = buildCapturingPlayerNode();

		const result = await node(stateWith("优先清理边角雷区"), {
			configurable: { thread_id: "t" },
		});

		// The DI seam was actually exercised (style/javascript.md §测试).
		expect(createAgentFn).toHaveBeenCalled();
		// The instruction leads the model input (before the channel history)
		// — "与下次激活一同注入" (FR-015/FR-016).
		const input = captured[0]?.messages ?? [];
		expect(input[0]).toBeInstanceOf(HumanMessage);
		expect(String(input[0].content)).toBe("优先清理边角雷区");
		expect(input[1].content).toBe("历史消息");
		// The instruction is ALSO written back to the channel (survey D6 —
		// it accumulates in the player's conversation flow), and the slot is
		// cleared (delivered exactly once).
		const written = result.playerMessages ?? [];
		expect(written).toHaveLength(2);
		expect(written[0]).toBeInstanceOf(HumanMessage);
		expect(String(written[0].content)).toBe("优先清理边角雷区");
		expect(result.pendingInstruction).toBeNull();
	});

	it("does not inject nor clear when pendingInstruction is null", async () => {
		const { node, captured } = buildCapturingPlayerNode();

		const result = await node(stateWith(null), {
			configurable: { thread_id: "t" },
		});

		const input = captured[0]?.messages ?? [];
		// No instruction prepended — the history leads unchanged.
		expect(input).toHaveLength(1);
		expect(String(input[0].content)).toBe("历史消息");
		// Only the agent output is written back (no instruction message).
		const written = result.playerMessages ?? [];
		expect(written).toHaveLength(1);
		expect(result.pendingInstruction).toBeUndefined();
	});

	it("persists the instruction even when the agent invoke throws (try/finally)", async () => {
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

		const result = await node(stateWith("崩溃后仍保留指令"), {
			configurable: { thread_id: "t" },
		});

		// The instruction message survives the crashed invoke (Issue 1
		// try/finally semantics — the write-back still runs).
		const written = result.playerMessages ?? [];
		expect(written).toHaveLength(1);
		expect(String(written[0].content)).toBe("崩溃后仍保留指令");
		expect(result.pendingInstruction).toBeNull();
	});
});
