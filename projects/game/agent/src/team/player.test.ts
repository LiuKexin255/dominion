/**
 * team/player.test.ts — Unit tests for the player node's `createAgent`
 * middleware wiring (Feature 038 T002, US1).
 *
 * The `queueDrain` `beforeModel` middleware drains the TurnLoop buffer
 * through `runtime.configurable.drainQueuedInput` before EVERY model call
 * and injects the queued content as a `HumanMessage` — the turn's first
 * model call AND each call after a tool result (FR-001, spec v2 —
 * `specs/038-queue-input-mid-turn/contracts/injection-seam-contract.md`
 * §3).
 *
 * Mock strategy (`style/javascript.md` §Mock): the node's `createAgentFn`
 * DI seam is injected as a `vi.fn()` spy that captures the middleware
 * config; the hooks are then invoked directly with a fake runtime — no
 * `vi.mock` module interception (see
 * [vitest — Mocking Modules Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)).
 */

import { describe, expect, it, vi } from "vitest";
import { HumanMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";

import { buildContentBlocks } from "../llm";
import type { TurnContent } from "../llm";
import type { ChatModel } from "../model-provider";
import { FakeStrategyStore } from "../strategy-store";
import { createEphemeralGameBuffer } from "./team-sink";
import { createPlayerNode } from "./player";

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
		strategyStore: new FakeStrategyStore(),
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
