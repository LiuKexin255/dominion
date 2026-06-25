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
				type: "image_url",
				image_url: { url: "data:image/png;base64,base64data" },
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

	it("does not inject default text when image is present without text", async () => {
		// Regression guard: the old code injected "如图所示" when text was
		// empty and an image was present. That default is removed — the
		// desktop SendUserTurn now requires non-empty text, and the adapter
		// no longer fabricates placeholder instructions.
		const { adapter, capturedMessages } = captureAgent();
		const content: TurnContent = {
			imageData: "imgdata",
			imageMimeType: "image/jpeg",
		};

		await collect(adapter.generateTurn("t-img-only", content));

		expect(capturedMessages).toHaveLength(1);
		const msg = capturedMessages[0] as BaseMessage & { content: unknown };
		expect(msg.content).toEqual([
			{
				type: "image_url",
				image_url: { url: "data:image/jpeg;base64,imgdata" },
			},
		]);
	});

	it("appends a size-annotation text block after image when dimensions are provided", async () => {
		const { adapter, capturedMessages } = captureAgent();
		const content: TurnContent = {
			text: "click the centre",
			imageData: "pngdata",
			imageMimeType: "image/png",
			imageWidthPx: 332,
			imageHeightPx: 508,
		};

		await collect(adapter.generateTurn("t-size", content));

		expect(capturedMessages).toHaveLength(1);
		const msg = capturedMessages[0] as BaseMessage & { content: unknown };
		const blocks = msg.content as Array<{ type: string; text?: string }>;
		expect(blocks).toHaveLength(3);
		expect(blocks[0]).toEqual({ type: "text", text: "click the centre" });
		expect(blocks[1].type).toBe("image_url");
		expect(blocks[2]).toEqual({
			type: "text",
			text: "[图片像素尺寸：332×508（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]",
		});
	});

	it("does not append size annotation when image dimensions are missing", async () => {
		const { adapter, capturedMessages } = captureAgent();
		const content: TurnContent = {
			text: "hello",
			imageData: "imgdata",
			imageMimeType: "image/png",
		};

		await collect(adapter.generateTurn("t-no-dims", content));

		const msg = capturedMessages[0] as BaseMessage & { content: unknown };
		const blocks = msg.content as Array<{ type: string }>;
		expect(blocks).toHaveLength(2);
		expect(blocks[0].type).toBe("text");
		expect(blocks[1].type).toBe("image_url");
	});

	it("does not append size annotation when image dimensions are zero or negative", async () => {
		// Regression guard for the `w > 0 && h > 0` precondition at
		// llm.ts:223. FR-014's annotation must only appear when both
		// dimensions are strictly positive; zero/negative — which would
		// produce a meaningless "0×0" annotation — must skip it. This
		// complements the absent-dims test above by covering the boundary.
		const { adapter, capturedMessages } = captureAgent();
		const content: TurnContent = {
			text: "hello",
			imageData: "imgdata",
			imageMimeType: "image/png",
			imageWidthPx: 0,
			imageHeightPx: -1,
		};

		await collect(adapter.generateTurn("t-zero-dims", content));

		const msg = capturedMessages[0] as BaseMessage & { content: unknown };
		const blocks = msg.content as Array<{ type: string }>;
		expect(blocks).toHaveLength(2);
		expect(blocks[0].type).toBe("text");
		expect(blocks[1].type).toBe("image_url");
	});

	it("does not add default text when neither text nor image is provided", async () => {
		const { adapter, capturedMessages } = captureAgent();

		await collect(adapter.generateTurn("t-empty", {}));

		expect(capturedMessages).toHaveLength(1);
		const msg = capturedMessages[0] as BaseMessage & { content: unknown };
		expect(msg.content).toEqual([]);
	});

	it("passes session_id metadata to streamEvents so LLM observability sees the session", async () => {
		// The threadId IS the sessionId. LangChain invocation metadata flows
		// through the callback/tracing pipeline so LLM-side consoles can
		// correlate model calls back to the originating session.
		const adapter = new AgentAdapterImpl(
			fakeTextModel("unused"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);
		let capturedConfig: Record<string, unknown> | undefined;
		(adapter as unknown as FakeStreamAgent).agent = {
			streamEvents: async (
				_input?: { messages: BaseMessage[] },
				config?: Record<string, unknown>,
			) => {
				if (config) capturedConfig = config;
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

		await collect(adapter.generateTurn("sess-metadata", { text: "hi" }));

		expect(capturedConfig).toBeDefined();
		expect(capturedConfig!.metadata).toMatchObject({
			session_id: "sess-metadata",
		});
		expect(capturedConfig!.configurable).toMatchObject({
			thread_id: "sess-metadata",
		});
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
