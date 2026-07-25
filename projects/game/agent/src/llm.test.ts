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
		streamEvents: (
			input?: { messages: BaseMessage[] },
			config?: { signal?: AbortSignal; [k: string]: unknown },
		) => Promise<{
			messages: AsyncGenerator<{
				reasoning: AsyncGenerator<string>;
				text: AsyncGenerator<string>;
			}>;
			toolCalls?: AsyncIterable<{
				name: string;
				callId: string;
				input: unknown;
				output: Promise<unknown>;
			}>;
			output?: PromiseLike<unknown>;
		}>;
	};
};

/** An empty async iterable, used for the toolCalls projection on fakes that
 *  do not exercise tool emission. */
function emptyAsync<T>(): AsyncIterable<T> {
	return (async function* () {})();
}

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

function fakeTextModel(text: string, turns = 1) {
	// fakeModel().respond() queues one response consumed per model invocation;
	// a multi-turn test must queue one response per turn or FakeModel throws
	// "no response queued for invocation N".
	let model = fakeModel();
	for (let i = 0; i < turns; i++) {
		model = model.respond(
			new AIMessage({
				content: [{ type: "text", text }],
			}),
		);
	}
	return model;
}

function fakeThinkingModel(reasoning: string, text: string, turns = 1) {
	let model = fakeModel();
	for (let i = 0; i < turns; i++) {
		model = model.respond(
			new AIMessage({
				content: [
					{ type: "reasoning", reasoning },
					{ type: "text", text },
				],
			}),
		);
	}
	return model;
}

beforeEach(() => {
	vi.clearAllMocks();
});

// ===========================================================================
// buildTools — toolNames → StructuredToolInterface[] mapping
// ===========================================================================

describe("buildTools", () => {
	it("returns a mouse_move tool when toolNames includes 'mouse_move'", () => {
		const tools = buildTools(["mouse_move"], noopBridge());
		expect(tools).toHaveLength(1);
		expect(tools[0].name).toBe("mouse_move");
	});

	it("returns a mouse_click tool when toolNames includes 'mouse_click'", () => {
		const tools = buildTools(["mouse_click"], noopBridge());
		expect(tools).toHaveLength(1);
		expect(tools[0].name).toBe("mouse_click");
	});

	it("returns empty array when toolNames is empty", () => {
		expect(buildTools([], noopBridge())).toEqual([]);
	});

	it("silently skips unknown tool names", () => {
		expect(buildTools(["unknown-tool"], noopBridge())).toEqual([]);
	});

	it("does not register the legacy 'mouse' name", () => {
		expect(buildTools(["mouse"], noopBridge())).toEqual([]);
	});

	it("maps multiple known tool names", () => {
		const tools = buildTools(["mouse_move", "mouse_click"], noopBridge());
		expect(tools).toHaveLength(2);
		expect(tools[0].name).toBe("mouse_move");
		expect(tools[1].name).toBe("mouse_click");
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

	it("generateTurn accepts 3 parameters (threadId, content, signal)", () => {
		const adapter = new AgentAdapterImpl(
			fakeTextModel("hi"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);
		expect(adapter.generateTurn.length).toBe(3);
	});

	it("constructs without error when toolNames includes the mouse tools", () => {
		expect(
			() =>
				new AgentAdapterImpl(
					fakeTextModel("hi"),
					"prompt",
					["mouse_move", "mouse_click"],
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

	it("generateTurn respects AbortSignal — stream stops on abort", async () => {
		const adapter = new AgentAdapterImpl(
			fakeTextModel("unused"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);
		let capturedSignal: AbortSignal | undefined;
		(adapter as unknown as FakeStreamAgent).agent = {
			streamEvents: async (
				_input?: { messages: BaseMessage[] },
				config?: { signal?: AbortSignal; [k: string]: unknown },
			) => {
				const signal = config?.signal;
				capturedSignal = signal;
				return {
					messages: (async function* () {
						yield {
							reasoning: (async function* () {})(),
							text: (async function* () {
								yield "first";
								await new Promise<void>((resolve) => {
									if (signal) {
										if (signal.aborted) resolve();
										else
											signal.addEventListener(
												"abort",
												() => resolve(),
												{ once: true },
											);
									} else {
										resolve();
									}
								});
							})(),
						};
					})(),
				};
			},
		};

		const controller = new AbortController();
		const blocks: ContentBlock[] = [];
		for await (const block of adapter.generateTurn(
			"t-abort",
			{ text: "hi" },
			controller.signal,
		)) {
			blocks.push(block);
			if (blocks.length === 1) {
				controller.abort();
			}
		}

 		expect(blocks).toHaveLength(1);
 		expect(blocks[0]).toEqual({ type: "text", text: "first" });
 		expect(capturedSignal).toBeTruthy();
 		expect(capturedSignal?.aborted).toBe(true);
 	});
});

// ===========================================================================
// AgentAdapterImpl.generateTurn — tool_call / tool_result emission (T006;
// contracts/tool-dispatch-contract.md §7; research.md D5; quickstart §4)
// ===========================================================================

describe("AgentAdapterImpl.generateTurn tool_call/tool_result streaming", () => {
	it("yields a tool_call block then a tool_result block from stream.toolCalls", async () => {
		const adapter = new AgentAdapterImpl(
			fakeTextModel("unused"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);

		// Fake stream: one text delta, plus one ToolCallStream whose .output
		// resolves to content blocks carrying a message and a screenshot.
		// stream.messages ignores tool-role messages (LangGraph), so tool
		// events come solely from stream.toolCalls.
		const outputContent = [
			{ type: "text", text: "ok" },
			{
				type: "image_url",
				image_url: { url: "data:image/png;base64,AAAA" },
			},
			{
				type: "text",
				text: "[图片像素尺寸：10×20（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]",
			},
		];
		(adapter as unknown as FakeStreamAgent).agent = {
			streamEvents: async () => ({
				messages: (async function* () {
					yield {
						reasoning: (async function* () {})(),
						text: (async function* () {
							yield "calling tool";
						})(),
					};
				})(),
				toolCalls: (async function* () {
					yield {
						name: "saolei_click",
						callId: "call_1",
						input: { x: 3, y: 4 },
						output: Promise.resolve(outputContent),
					};
				})(),
				output: Promise.resolve(),
			}),
		};

		const collected = await collect(
			adapter.generateTurn("t-tool", { text: "go" }),
		);

		// tool_call precedes tool_result and carries the model's invocation.
		const callIdx = collected.findIndex((b) => b.type === "tool_call");
		expect(callIdx).toBeGreaterThanOrEqual(0);
		expect(collected[callIdx]).toEqual({
			type: "tool_call",
			name: "saolei_click",
			args: { x: 3, y: 4 },
			toolCallId: "call_1",
		});

		const resultBlock = collected.find(
			(b): b is Extract<ContentBlock, { type: "tool_result" }> =>
				b.type === "tool_result",
		);
		expect(resultBlock).toBeDefined();
		expect(resultBlock!.toolCallId).toBe("call_1");
		// US1: status is UNSPECIFIED on the live path (additional_kwargs not
		// reachable via the normalised toolCalls.output projection).
		expect(resultBlock!.status).toBe("TOOL_RESULT_STATUS_UNSPECIFIED");
		expect(resultBlock!.message).toBe("ok");
		expect(resultBlock!.screenshot).toBeDefined();
		expect(resultBlock!.screenshot!.data).toBe("AAAA");
		expect(resultBlock!.screenshot!.widthPx).toBe(10);
		expect(resultBlock!.screenshot!.heightPx).toBe(20);

		// tool_call precedes its tool_result.
		const resultIdx = collected.findIndex((b) => b.type === "tool_result");
		expect(callIdx).toBeLessThan(resultIdx);
	});

	it("emits no tool blocks when stream.toolCalls is absent", async () => {
		// Defensive: a stream without the toolCalls projection (none of the
		// existing fakes provide it) yields only text/reasoning — consumeToolCalls
		// is a no-op.
		const adapter = new AgentAdapterImpl(
			fakeTextModel("plain"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
		);
		const blocks = await collect(
			adapter.generateTurn("t-no-tools", { text: "hi" }),
		);
		expect(blocks.some((b) => b.type.startsWith("tool"))).toBe(false);
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
		const model = fakeTextModel("response", 2);
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
		const model = fakeModel()
			.respond(
				new AIMessage({ content: [{ type: "text", text: "OK" }] }),
			)
			.respond(
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
			// Per 011 (research.md L117-126) createAgent injects the current
			// profile's systemPrompt as a SystemMessage each turn; the middleware
			// strips only STALE cross-profile SystemMessages (plan.md L255), so
			// the current systemPrompt remains. Same profile ("system-prompt-1")
			// both turns ⇒ exactly one SystemMessage carrying the current prompt,
			// with no stale/duplicate contamination.
			expect(systemMsgs).toHaveLength(1);
			// createAgent injects the systemPrompt as a SystemMessage whose
			// content is a text content-block array.
			expect(systemMsgs[0].content).toEqual([
				{ type: "text", text: "system-prompt-1" },
			]);
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

// ===========================================================================
// AgentAdapterImpl.create — saolei MCP integration (spec 018-saolei-mcp T011)
// ===========================================================================
//
// The async factory merges MCP-client tools (FR-002b) with mouse-filtered
// native tools (FR-012). Tests inject a fake `mcpClientFactory` so no real
// HTTP loopback is required (style/javascript.md §测试 — DI seam).

import type { StructuredToolInterface } from "@langchain/core/tools";
import type { McpClientFactory } from "./llm";

function fakeTool(name: string): StructuredToolInterface {
	// Minimal StructuredToolInterface stub; only `name` is observed by the
	// tests in this block.
	return { name } as unknown as StructuredToolInterface;
}

describe("AgentAdapterImpl.create saolei integration", () => {
	it("non-saolei profile: skips MCP client, keeps mouse tools (backward-compat)", async () => {
		const createAgentFn = vi.fn((config: any) => ({ config }));
		const mcpClientFactory = vi.fn<McpClientFactory>();

		const adapter = await AgentAdapterImpl.create(
			fakeTextModel("hi"),
			"prompt",
			["mouse_move", "mouse_click"],
			noopBridge(),
			new MemorySaver(),
			[], // mcpNames — no saolei
			"sess-no-mcp",
			{ createAgentFn, mcpClientFactory },
		);

		expect(adapter).toBeInstanceOf(AgentAdapterImpl);
		// No MCP client is built for non-saolei profiles.
		expect(mcpClientFactory).not.toHaveBeenCalled();
		// Mouse tools are kept verbatim (no FR-012 filtering).
		const config = createAgentFn.mock.calls[0][0];
		const toolNames = config.tools.map((t: StructuredToolInterface) => t.name);
		expect(toolNames).toEqual(["mouse_move", "mouse_click"]);
	});

	it("saolei profile: excludes mouse tools (FR-012)", async () => {
		const createAgentFn = vi.fn((config: any) => ({ config }));
		const mcpClientFactory = vi.fn<McpClientFactory>(async (_config) => ({
			getTools: async () => [],
		}));

		await AgentAdapterImpl.create(
			fakeTextModel("hi"),
			"prompt",
			["mouse_move", "mouse_click"],
			noopBridge(),
			new MemorySaver(),
			["saolei"], // mcpNames — saolei
			"sess-saolei",
			{ createAgentFn, mcpClientFactory },
		);

		const config = createAgentFn.mock.calls[0][0];
		const toolNames = config.tools.map((t: StructuredToolInterface) => t.name);
		// FR-012: mouse tools MUST NOT be exposed for saolei profiles.
		expect(toolNames).not.toContain("mouse_move");
		expect(toolNames).not.toContain("mouse_click");
	});

	it("saolei profile: builds MCP client at the per-session URL (FR-002b)", async () => {
		const createAgentFn = vi.fn((config: any) => ({ config }));
		const mcpClientFactory = vi.fn<McpClientFactory>(async (config) => {
			// Assert the URL passed to MultiServerMCPClient matches the
			// per-session endpoint form (FR-001 / FR-002b).
			const entry = (config as { saolei?: { url?: string } }).saolei;
			expect(entry?.url).toBe(
				"http://localhost:9999/internal/mcp/sess-url-test",
			);
			return {
				getTools: async () => [
					fakeTool("saolei_init"),
					fakeTool("saolei_click"),
					fakeTool("saolei_flag"),
					fakeTool("saolei_chord_click"),
					fakeTool("saolei_update"),
				],
			};
		});

		await AgentAdapterImpl.create(
			fakeTextModel("hi"),
			"prompt",
			[],
			noopBridge(),
			new MemorySaver(),
			["saolei"],
			"sess-url-test",
			{ createAgentFn, mcpClientFactory, mcpPort: 9999 },
		);

		// The MCP client was constructed exactly once with the per-session URL.
		expect(mcpClientFactory).toHaveBeenCalledTimes(1);
	});

	it("saolei profile: getTools() output is fed to createAgent", async () => {
		const createAgentFn = vi.fn((config: any) => ({ config }));
		const saoleiTools = [
			fakeTool("saolei_init"),
			fakeTool("saolei_click"),
			fakeTool("saolei_flag"),
			fakeTool("saolei_chord_click"),
			fakeTool("saolei_update"),
		];
		const mcpClientFactory = vi.fn<McpClientFactory>(async () => ({
			getTools: async () => saoleiTools,
		}));

		await AgentAdapterImpl.create(
			fakeTextModel("hi"),
			"prompt",
			["mouse_move"], // present in toolNames but must be filtered out
			noopBridge(),
			new MemorySaver(),
			["saolei"],
			"sess-tools",
			{ createAgentFn, mcpClientFactory, mcpPort: 12345 },
		);

		const config = createAgentFn.mock.calls[0][0];
		const toolNames = config.tools.map((t: StructuredToolInterface) => t.name);
		// The five saolei tools are present; mouse_move is absent.
		expect(toolNames).toEqual([
			"saolei_init",
			"saolei_click",
			"saolei_flag",
			"saolei_chord_click",
			"saolei_update",
		]);
	});
});

// ===========================================================================
// AgentAdapterImpl.create — built-in skill injection (spec 018-saolei-mcp T018)
// ===========================================================================
//
// FR-023/024/025 + research.md D9: when mcpNames includes "saolei", the
// built-in saolei skill body is appended to the systemPrompt before the
// adapter is constructed. Non-saolei profiles MUST NOT receive the body.
// quickstart.md Scenario 5 is the independent validation.

import { SKILL_PROMPT_SEPARATOR, loadSkillBody } from "./skill-loader";

describe("AgentAdapterImpl.create skill injection (FR-023/024/025)", () => {
	it("saolei profile: appends the skill body to the systemPrompt", async () => {
		const createAgentFn = vi.fn((config: any) => ({ config }));
		const mcpClientFactory = vi.fn<McpClientFactory>(async () => ({
			getTools: async () => [],
		}));

		await AgentAdapterImpl.create(
			fakeTextModel("hi"),
			"base-system-prompt",
			[],
			noopBridge(),
			new MemorySaver(),
			["saolei"],
			"sess-skill-on",
			{ createAgentFn, mcpClientFactory },
		);

		const config = createAgentFn.mock.calls[0][0];
		const prompt: string = config.systemPrompt;
		// The original prompt is preserved as the prefix.
		expect(prompt.startsWith("base-system-prompt")).toBe(true);
		// The skill body is appended after the separator (research.md D9).
		expect(prompt).toContain(
			"base-system-prompt" + SKILL_PROMPT_SEPARATOR + "# saolei",
		);
		// Stable content markers from the saolei skill body.
		expect(prompt).toContain("saolei_init");
		expect(prompt).toContain("saolei_update");
		expect(prompt).toContain("Cell status enum");
	});

	it("saolei profile: appended body equals loadSkillBody('saolei') verbatim", async () => {
		const createAgentFn = vi.fn((config: any) => ({ config }));
		const mcpClientFactory = vi.fn<McpClientFactory>(async () => ({
			getTools: async () => [],
		}));

		await AgentAdapterImpl.create(
			fakeTextModel("hi"),
			"p",
			[],
			noopBridge(),
			new MemorySaver(),
			["saolei"],
			"sess-skill-body",
			{ createAgentFn, mcpClientFactory },
		);

		const prompt: string = createAgentFn.mock.calls[0][0].systemPrompt;
		const expected = "p" + SKILL_PROMPT_SEPARATOR + loadSkillBody("saolei");
		expect(prompt).toBe(expected);
	});

	it("non-saolei profile: does NOT inject the skill body (FR-024 negative)", async () => {
		const createAgentFn = vi.fn((config: any) => ({ config }));
		const mcpClientFactory = vi.fn<McpClientFactory>();

		await AgentAdapterImpl.create(
			fakeTextModel("hi"),
			"base-system-prompt",
			["mouse_move"],
			noopBridge(),
			new MemorySaver(),
			[], // mcpNames — no saolei
			"sess-skill-off",
			{ createAgentFn, mcpClientFactory },
		);

		const prompt: string = createAgentFn.mock.calls[0][0].systemPrompt;
		// The prompt is returned unchanged — no separator, no skill markers.
		expect(prompt).toBe("base-system-prompt");
		expect(prompt).not.toContain("# saolei");
		expect(prompt).not.toContain(SKILL_PROMPT_SEPARATOR);
		expect(prompt).not.toContain("saolei_init");
	});

	it("profile with an unknown mcp_name: does NOT inject any skill body", async () => {
		// FR-025 scope guard + FR-024: only registered built-in skills are
		// injected; an unknown mcp_name triggers no injection.
		const createAgentFn = vi.fn((config: any) => ({ config }));
		const mcpClientFactory = vi.fn<McpClientFactory>();

		await AgentAdapterImpl.create(
			fakeTextModel("hi"),
			"base",
			["mouse_move"],
			noopBridge(),
			new MemorySaver(),
			["some-other-mcp"],
			"sess-unknown-mcp",
			{ createAgentFn, mcpClientFactory },
		);

		const prompt: string = createAgentFn.mock.calls[0][0].systemPrompt;
		expect(prompt).toBe("base");
		expect(prompt).not.toContain(SKILL_PROMPT_SEPARATOR);
	});
});
