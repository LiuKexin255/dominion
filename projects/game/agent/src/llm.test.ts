/**
 * llm.test.ts — Tests for AgentAdapterImpl.
 *
 * The adapter receives a pre-created ChatModel (from ModelProviderCache)
 * at construction time.  createAgent compiles eagerly.  generateTurn takes
 * threadId and a multimodal TurnContent (text + optional image).
 */

import { AIMessage, type BaseMessage } from "@langchain/core/messages";
import { fakeModel } from "@langchain/core/testing";
import { MemorySaver } from "@langchain/langgraph";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
	AgentAdapterImpl,
	buildTools,
	type ContentBlock,
	type TurnContent,
} from "./llm";
import { OperationBridge } from "./operation-bridge";

// Helpers

type FakeStreamAgent = {
	agent: {
		streamEvents: (input?: { messages: BaseMessage[] }) => Promise<{
			messages: AsyncGenerator<{
				reasoning: AsyncGenerator<string>;
				text: AsyncGenerator<string>;
			}>;
		}>;
	};
};

function noopBridge(): OperationBridge {
	return new OperationBridge();
}

async function collect(
	iter: AsyncIterable<ContentBlock>,
): Promise<ContentBlock[]> {
	const blocks: ContentBlock[] = [];
	for await (const block of iter) {
		blocks.push(block);
	}
	return blocks;
}

function fakeTextModel(text: string) {
	return fakeModel().respond(
		new AIMessage({
			content: [{ type: "text", text }],
		}),
	);
}

function fakeThinkingModel(reasoning: string, text: string) {
	return fakeModel().respond(
		new AIMessage({
			content: [
				{ type: "reasoning", reasoning },
				{ type: "text", text },
			],
		}),
	);
}

beforeEach(() => {
	vi.clearAllMocks();
});

// ===========================================================================
// buildTools — toolNames → StructuredToolInterface[] mapping
// ===========================================================================

describe("buildTools", () => {
	it("returns a mouse tool when toolNames includes 'mouse'", () => {
		const tools = buildTools(["mouse"], noopBridge());
		expect(tools).toHaveLength(1);
		expect(tools[0].name).toBe("mouse");
	});

	it("returns empty array when toolNames is empty", () => {
		expect(buildTools([], noopBridge())).toEqual([]);
	});

	it("silently skips unknown tool names", () => {
		expect(buildTools(["unknown-tool"], noopBridge())).toEqual([]);
	});

	it("maps multiple known tool names", () => {
		const tools = buildTools(["mouse", "mouse"], noopBridge());
		expect(tools).toHaveLength(2);
		expect(tools[0].name).toBe("mouse");
		expect(tools[1].name).toBe("mouse");
	});
});

// ===========================================================================
// AgentAdapterImpl constructor
// ===========================================================================

describe("AgentAdapterImpl constructor", () => {
	it("implements the AgentAdapter interface", () => {
		const adapter = new AgentAdapterImpl(
			fakeTextModel("hi"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);
		expect(typeof adapter.generateTurn).toBe("function");
	});

	it("generateTurn accepts 2 parameters (threadId, content)", () => {
		const adapter = new AgentAdapterImpl(
			fakeTextModel("hi"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);
		expect(adapter.generateTurn.length).toBe(2);
	});

	it("constructs without error when toolNames includes 'mouse'", () => {
		expect(
			() =>
				new AgentAdapterImpl(
					fakeTextModel("hi"),
					"prompt",
					["mouse"],
					noopBridge(),
					new MemorySaver(),
				),
		).not.toThrow();
	});
});

// ===========================================================================
// AgentAdapterImpl.generateTurn — ContentBlock streaming
// ===========================================================================

describe("AgentAdapterImpl.generateTurn ContentBlock streaming", () => {
	it("yields text ContentBlock for text-only response", async () => {
		const model = fakeTextModel("The answer is 42.");
		const adapter = new AgentAdapterImpl(
			model,
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);
		const blocks = await collect(
			adapter.generateTurn("t-text", { text: "Hi" }),
		);

		expect(blocks.length).toBeGreaterThan(0);
		const textBlocks = blocks.filter((b) => b.type === "text");
		expect(textBlocks.length).toBeGreaterThanOrEqual(1);
		expect(textBlocks[0].text).toBe("The answer is 42.");
	});

	it("yields reasoning ContentBlock before text ContentBlock", async () => {
		const model = fakeThinkingModel("Let me think...", "Done.");
		const adapter = new AgentAdapterImpl(
			model,
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);
		const blocks = await collect(
			adapter.generateTurn("t-think", { text: "Why?" }),
		);

		const reasoningBlocks = blocks.filter((b) => b.type === "reasoning");
		const textBlocks = blocks.filter((b) => b.type === "text");

		expect(reasoningBlocks.length).toBeGreaterThanOrEqual(1);
		expect(textBlocks.length).toBeGreaterThanOrEqual(1);

		let lastReasoningIdx = -1;
		let firstTextIdx = -1;
		for (let i = 0; i < blocks.length; i++) {
			if (blocks[i].type === "reasoning") lastReasoningIdx = i;
			if (blocks[i].type === "text" && firstTextIdx === -1) firstTextIdx = i;
		}
		if (lastReasoningIdx >= 0 && firstTextIdx >= 0) {
			expect(lastReasoningIdx).toBeLessThan(firstTextIdx);
		}
	});

	it("all yielded blocks have type 'reasoning' or 'text'", async () => {
		const model = fakeThinkingModel("Thinking", "Text");
		const adapter = new AgentAdapterImpl(
			model,
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);
		const blocks = await collect(
			adapter.generateTurn("t-types", { text: "go" }),
		);

		for (const block of blocks) {
			expect(["reasoning", "text"]).toContain(block.type);
		}
	});

	it("yields reasoning from additional_kwargs before text blocks", async () => {
		const adapter = new AgentAdapterImpl(
			fakeTextModel("unused"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);

		(adapter as unknown as FakeStreamAgent).agent = {
			streamEvents: async () => ({
				messages: (async function* () {
					yield {
						reasoning: (async function* () {
							yield "I should greet the user";
						})(),
						text: (async function* () {
							yield "Hello";
						})(),
					};
				})(),
			}),
		};

		const blocks = await collect(
			adapter.generateTurn("t-additional-kwargs", { text: "Hi" }),
		);

		expect(blocks).toHaveLength(2);
		expect(blocks[0]).toEqual({
			type: "reasoning",
			reasoning: "I should greet the user",
		});
		expect(blocks[1]).toEqual({ type: "text", text: "Hello" });
	});

	it("yields text blocks when additional_kwargs is absent", async () => {
		const adapter = new AgentAdapterImpl(
			fakeTextModel("unused"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);

		(adapter as unknown as FakeStreamAgent).agent = {
			streamEvents: async () => ({
				messages: (async function* () {
					yield {
						reasoning: (async function* () {})(),
						text: (async function* () {
							yield "Just text";
						})(),
					};
				})(),
			}),
		};

		const blocks = await collect(
			adapter.generateTurn("t-no-additional-kwargs", { text: "Hi" }),
		);

		expect(blocks).toHaveLength(1);
		expect(blocks[0]).toEqual({ type: "text", text: "Just text" });
	});
});

// ===========================================================================
// AgentAdapterImpl.generateTurn — multimodal HumanMessage construction
// ===========================================================================

describe("AgentAdapterImpl.generateTurn multimodal HumanMessage", () => {
	function captureAgent(): {
		adapter: AgentAdapterImpl;
		capturedMessages: BaseMessage[];
	} {
		const adapter = new AgentAdapterImpl(
			fakeTextModel("unused"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);
		const capturedMessages: BaseMessage[] = [];
		(adapter as unknown as FakeStreamAgent).agent = {
			streamEvents: async (input?: { messages: BaseMessage[] }) => {
				if (input) capturedMessages.push(...input.messages);
				return {
					messages: (async function* () {
						yield {
							reasoning: (async function* () {})(),
							text: (async function* () {
								yield "ok";
							})(),
						};
					})(),
				};
			},
		};
		return { adapter, capturedMessages };
	}

	it("builds both text and image blocks when text + image provided", async () => {
		const { adapter, capturedMessages } = captureAgent();
		const content: TurnContent = {
			text: "click here",
			imageData: "base64data",
			imageMimeType: "image/png",
		};

		await collect(adapter.generateTurn("t-multi", content));

		expect(capturedMessages).toHaveLength(1);
		const msg = capturedMessages[0] as BaseMessage & {
			_getType?: () => string;
			content: unknown;
		};
		expect(msg._getType?.()).toBe("human");
		expect(Array.isArray(msg.content)).toBe(true);
		expect(msg.content).toEqual([
			{ type: "text", text: "click here" },
			{
				type: "image",
				source_type: "base64",
				data: "base64data",
				mime_type: "image/png",
			},
		]);
	});

	it("builds a single text block when only text is provided", async () => {
		const { adapter, capturedMessages } = captureAgent();

		await collect(adapter.generateTurn("t-text-only", { text: "hello" }));

		expect(capturedMessages).toHaveLength(1);
		const msg = capturedMessages[0] as BaseMessage & { content: unknown };
		expect(msg.content).toEqual([{ type: "text", text: "hello" }]);
	});

	it("builds an image block plus default text when image is present without text", async () => {
		const { adapter, capturedMessages } = captureAgent();
		const content: TurnContent = {
			imageData: "imgdata",
			imageMimeType: "image/jpeg",
		};

		await collect(adapter.generateTurn("t-img-only", content));

		expect(capturedMessages).toHaveLength(1);
		const msg = capturedMessages[0] as BaseMessage & { content: unknown };
		expect(msg.content).toEqual([
			{ type: "text", text: "如图所示" },
			{
				type: "image",
				source_type: "base64",
				data: "imgdata",
				mime_type: "image/jpeg",
			},
		]);
	});

	it("does not add default text when neither text nor image is provided", async () => {
		const { adapter, capturedMessages } = captureAgent();

		await collect(adapter.generateTurn("t-empty", {}));

		expect(capturedMessages).toHaveLength(1);
		const msg = capturedMessages[0] as BaseMessage & { content: unknown };
		expect(msg.content).toEqual([]);
	});
});

// ===========================================================================
// AgentAdapterImpl — Checkpoint persistence
// ===========================================================================

describe("AgentAdapterImpl checkpoint persistence", () => {
	it("getState returns both HumanMessage and AIMessage after generateTurn", async () => {
		const model = fakeThinkingModel("Let me think...", "Done.");
		const cp = new MemorySaver();
		const adapter = new AgentAdapterImpl(
			model,
			"prompt",
			[],
			noopBridge(),
			cp,
		);

		await collect(adapter.generateTurn("ckpt-1", { text: "Hello" }));

		const state = await adapter.getState("ckpt-1");
		expect(state).not.toBeNull();
		const messages = state!.values.messages ?? [];
		expect(messages.length).toBeGreaterThanOrEqual(2);

		const hasHuman = messages.some(
			(m: BaseMessage) => (m as any)._getType?.() === "human",
		);
		const hasAI = messages.some(
			(m: BaseMessage) => (m as any)._getType?.() === "ai",
		);
		expect(hasHuman).toBe(true);
		expect(hasAI).toBe(true);
	});

	it("getState returns accumulated messages after multiple turns", async () => {
		const model = fakeTextModel("response");
		const cp = new MemorySaver();
		const adapter = new AgentAdapterImpl(
			model,
			"prompt",
			[],
			noopBridge(),
			cp,
		);

		await collect(adapter.generateTurn("ckpt-2", { text: "first" }));
		await collect(adapter.generateTurn("ckpt-2", { text: "second" }));

		const state = await adapter.getState("ckpt-2");
		expect(state).not.toBeNull();
		const messages = state!.values.messages ?? [];
		expect(messages.length).toBeGreaterThanOrEqual(4);
	});
});

// ===========================================================================
// AgentAdapterImpl — WrapModelCall middleware (SystemMessage stripping)
// ===========================================================================

describe("AgentAdapterImpl WrapModelCall middleware", () => {
	it("strips SystemMessages from state before model invocation", async () => {
		const model = fakeModel().respond(
			new AIMessage({ content: [{ type: "text", text: "OK" }] }),
		);
		const cp = new MemorySaver();

		const adapter = new AgentAdapterImpl(
			model,
			"system-prompt-1",
			[],
			noopBridge(),
			cp,
		);

		await collect(adapter.generateTurn("t-mw", { text: "msg1" }));
		await collect(adapter.generateTurn("t-mw", { text: "msg2" }));

		for (const call of model.calls) {
			const systemMsgs = call.messages.filter(
				(m: any) => m._getType?.() === "system",
			);
			expect(systemMsgs).toHaveLength(0);
		}
	});
});

// ===========================================================================
// AgentAdapterImpl.generateTurn — error propagation
// ===========================================================================

describe("AgentAdapterImpl.generateTurn error propagation", () => {
	it("propagates error when the model responds with an error", async () => {
		const model = fakeModel().respond(new Error("SIMULATED MODEL ERROR"));
		const adapter = new AgentAdapterImpl(
			model,
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);

		await expect(
			collect(adapter.generateTurn("t-err2", { text: "hi" })),
		).rejects.toThrow();
	});
});
