/**
 * llm.test.ts — Tests for the shared LLM types/helpers retained after the
 * single-agent adapter path was removed
 * (specs/031-team-template-mode/tasks.md T022): `buildTools`, `toParts`,
 * `buildContentBlocks`, and the message-history helpers.
 */

import { HumanMessage, AIMessage, ToolMessage } from "@langchain/core/messages";
import {
	isStructuredTool,
	tool,
	type StructuredToolInterface,
	type ToolRunnableConfig,
} from "@langchain/core/tools";
import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";

import {
	buildTools,
	buildContentBlocks,
	toParts,
	extractToolCalls,
	readToolResultStatus,
	withIdleHeartbeat,
	buildSaoleiMcpTools,
	buildMemoryMcpTools,
	TOOL_HEARTBEAT_INTERVAL_MS,
	STREAM_IDLE_TIMEOUT_MS,
	type ContentBlock,
	type TurnContent,
} from "./llm";
import { OperationBridge } from "./operation-bridge";

function noopBridge(): OperationBridge {
	return new OperationBridge();
}

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
// toParts — TurnContent normalization (spec 030 D3)
// ===========================================================================

describe("toParts", () => {
	it("passes through a non-empty parts array", () => {
		const parts = [{ text: "a" }, { text: "b" }];
		expect(toParts({ parts })).toBe(parts);
	});

	it("builds a single text part from the flat shape", () => {
		expect(toParts({ text: "hi" })).toEqual([{ text: "hi" }]);
	});

	it("builds a single image part with size annotations from the flat shape", () => {
		expect(
			toParts({
				imageData: "AAAA",
				imageMimeType: "image/png",
				imageWidthPx: 100,
				imageHeightPx: 200,
			}),
		).toEqual([
			{
				image: {
					data: "AAAA",
					mimeType: "image/png",
					widthPx: 100,
					heightPx: 200,
				},
			},
		]);
	});

	it("returns [] when the content is empty", () => {
		expect(toParts({})).toEqual([]);
	});
});

// ===========================================================================
// buildContentBlocks — TurnContent → model content blocks
// ===========================================================================

describe("buildContentBlocks", () => {
	it("maps text parts to text blocks", () => {
		expect(buildContentBlocks({ text: "go" })).toEqual([
			{ type: "text", text: "go" },
		]);
	});

	it("maps an image part to an image_url block plus a pixel-size annotation", () => {
		const blocks = buildContentBlocks({
			imageData: "AAAA",
			imageMimeType: "image/png",
			imageWidthPx: 640,
			imageHeightPx: 480,
		});
		expect(blocks[0]).toEqual({
			type: "image_url",
			image_url: { url: "data:image/png;base64,AAAA" },
		});
		expect(blocks[1]).toEqual({
			type: "text",
			text: "[图片像素尺寸：640×480（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]",
		});
	});

	it("skips the size annotation when dimensions are absent", () => {
		const blocks = buildContentBlocks({
			imageData: "AAAA",
			imageMimeType: "image/png",
		});
		expect(blocks).toHaveLength(1);
	});

	it("maps aggregated multi-part input FIFO", () => {
		const blocks = buildContentBlocks({
			parts: [
				{ text: "first" },
				{ text: "second" },
				{ image: { data: "BB", mimeType: "image/png" } },
			],
		});
		expect(blocks.map((b) => b.type)).toEqual([
			"text",
			"text",
			"image_url",
		]);
	});
});

// ===========================================================================
// extractToolCalls / readToolResultStatus — history helpers
// ===========================================================================

describe("extractToolCalls", () => {
	it("extracts tool_calls from an AIMessage", () => {
		const msg = new AIMessage({
			content: "calling",
			tool_calls: [
				{ name: "saolei_click", args: { x: 1 }, id: "tc-1" },
			],
		});
		expect(extractToolCalls(msg)).toEqual([
			{ name: "saolei_click", args: { x: 1 }, id: "tc-1" },
		]);
	});

	it("returns [] for messages without tool_calls", () => {
		expect(extractToolCalls(new HumanMessage("hi"))).toEqual([]);
	});
});

describe("readToolResultStatus", () => {
	it("reads the real status from additional_kwargs", () => {
		const msg = new ToolMessage({
			content: "ok",
			tool_call_id: "tc-1",
			additional_kwargs: { toolResultStatus: "TOOL_RESULT_STATUS_SUCCEEDED" },
		});
		expect(readToolResultStatus(msg)).toBe("TOOL_RESULT_STATUS_SUCCEEDED");
	});

	it("defaults to UNSPECIFIED (never FAILED) when absent", () => {
		const msg = new ToolMessage({ content: "ok", tool_call_id: "tc-1" });
		expect(readToolResultStatus(msg)).toBe("TOOL_RESULT_STATUS_UNSPECIFIED");
	});
});

// ===========================================================================
// withIdleHeartbeat — 043 US3 (T008c-r,
// specs/043-llm-stream-stall-recovery/tasks.md T008c-r; research.md R7.2;
// contracts/stall-recovery-contract.md §1.2): the client-side MCP heartbeat
// wrapper keeps LangGraph's idle timer alive during long MCP tool execution.
// The old dispatch-side heartbeat (T008c) was removed — the production tools
// cross the MCP HTTP boundary where `config.heartbeat` cannot reach
// `bridge.dispatch` (R7.1), so the refresh is driven client-side here.
// ===========================================================================

/**
 * Build a fake `StructuredToolInterface` whose invoke funnels to `onInvoke`
 * (DI seam — no module mocking, per style/javascript.md §Mock 约定).
 */
function makeFakeTool(
	onInvoke: (config?: unknown) => Promise<unknown>,
): StructuredToolInterface {
	return tool(
		async (_input, config) => onInvoke(config),
		{
			name: "fake_tool",
			description: "fake tool for withIdleHeartbeat tests",
			schema: z.object({ x: z.number().optional() }),
		},
	);
}

describe("withIdleHeartbeat", () => {
	it("calls heartbeat immediately and at TOOL_HEARTBEAT_INTERVAL_MS cadence while the tool hangs; clears the interval on resolve", async () => {
		vi.useFakeTimers();
		try {
			let resolveInvoke!: (value: string) => void;
			const fakeTool = makeFakeTool(
				() =>
					new Promise<string>((resolve) => {
						resolveInvoke = resolve;
					}),
			);
			const wrapped = withIdleHeartbeat(fakeTool);
			const heartbeat = vi.fn();

			const config: ToolRunnableConfig & { heartbeat: () => void } = { heartbeat };
			const promise = wrapped.invoke({ x: 1 }, config);

			// Immediate first call: the first setInterval tick is
			// TOOL_HEARTBEAT_INTERVAL_MS away, so the idle timer is refreshed
			// at invoke start (contract §1.2 — no initial gap).
			expect(heartbeat).toHaveBeenCalledTimes(1);

			// Elapse > STREAM_IDLE_TIMEOUT_MS with the tool still pending —
			// the window a bare idleTimeout would have fired in.
			const elapsedMs = STREAM_IDLE_TIMEOUT_MS + 5_000;
			await vi.advanceTimersByTimeAsync(elapsedMs);
			expect(heartbeat).toHaveBeenCalledTimes(
				1 + Math.floor(elapsedMs / TOOL_HEARTBEAT_INTERVAL_MS),
			);

			// Resolve the underlying tool → interval cleared (no leaked timers).
			resolveInvoke("done");
			await expect(promise).resolves.toBe("done");
			expect(vi.getTimerCount()).toBe(0);
		} finally {
			vi.useRealTimers();
		}
	});

	it("without heartbeat in config, invokes the underlying tool directly with no interval", async () => {
		vi.useFakeTimers();
		try {
			const innerInvoke = vi.fn(async () => "ok");
			const fakeTool = makeFakeTool(innerInvoke);
			const wrapped = withIdleHeartbeat(fakeTool);

			const result = await wrapped.invoke({ x: 1 });

			// Positive assertion that the mock was exercised (style/javascript.md
			// §规则：验证 mock 确实生效) — passthrough, no wrapper timers.
			expect(innerInvoke).toHaveBeenCalledOnce();
			expect(result).toBe("ok");
			expect(vi.getTimerCount()).toBe(0);
		} finally {
			vi.useRealTimers();
		}
	});

	it("clears the interval when the underlying tool rejects (finally block)", async () => {
		vi.useFakeTimers();
		try {
			let rejectInvoke!: (err: Error) => void;
			const fakeTool = makeFakeTool(
				() =>
					new Promise<string>((_resolve, reject) => {
						rejectInvoke = reject;
					}),
			);
			const wrapped = withIdleHeartbeat(fakeTool);
			const heartbeat = vi.fn();

			const config: ToolRunnableConfig & { heartbeat: () => void } = {
				heartbeat,
			};
			const promise = wrapped.invoke({ x: 1 }, config);
			expect(heartbeat).toHaveBeenCalledTimes(1);

			// Heartbeats keep firing while the tool is pending...
			await vi.advanceTimersByTimeAsync(2 * TOOL_HEARTBEAT_INTERVAL_MS);
			expect(heartbeat).toHaveBeenCalledTimes(3);

			// ...and the interval is still cleared on reject (finally).
			rejectInvoke(new Error("tool boom"));
			await expect(promise).rejects.toThrow("tool boom");
			expect(vi.getTimerCount()).toBe(0);
		} finally {
			vi.useRealTimers();
		}
	});

	it("preserves name/description/schema and remains a structured tool (createAgent-compatible)", () => {
		const fakeTool = makeFakeTool(async () => "ok");
		const wrapped = withIdleHeartbeat(fakeTool);

		expect(wrapped.name).toBe(fakeTool.name);
		expect(wrapped.description).toBe(fakeTool.description);
		expect(wrapped.schema).toBe(fakeTool.schema);
		expect(isStructuredTool(wrapped)).toBe(true);
	});
});

describe("buildSaoleiMcpTools applies withIdleHeartbeat", () => {
	it("wraps every client tool so heartbeat keeps the idle timer alive during invoke", async () => {
		vi.useFakeTimers();
		try {
			let resolveInvoke!: (value: string) => void;
			const clientFactory = vi.fn(async () => ({
				getTools: async () => [
					makeFakeTool(
						() =>
							new Promise<string>((resolve) => {
								resolveInvoke = resolve;
							}),
					),
				],
			}));

			const tools = await buildSaoleiMcpTools(
				"saolei",
				"sess-1",
				8080,
				clientFactory,
			);
			expect(tools).toHaveLength(1);

			const heartbeat = vi.fn();
			const config: ToolRunnableConfig & { heartbeat: () => void } = { heartbeat };
			const promise = tools[0]!.invoke({ x: 1 }, config);
			await vi.advanceTimersByTimeAsync(TOOL_HEARTBEAT_INTERVAL_MS);

			// Immediate + one tick: the wrapper is active on the production
			// choke point (R7.2 — applied in buildSaoleiMcpTools).
			expect(heartbeat).toHaveBeenCalledTimes(2);

			resolveInvoke("ok");
			await promise;
			expect(vi.getTimerCount()).toBe(0);
		} finally {
			vi.useRealTimers();
		}
	});
});

describe("buildMemoryMcpTools applies withIdleHeartbeat", () => {
	it("wraps every client tool so heartbeat keeps the idle timer alive during invoke", async () => {
		vi.useFakeTimers();
		try {
			let resolveInvoke!: (value: string) => void;
			const clientFactory = vi.fn(async () => ({
				getTools: async () => [
					makeFakeTool(
						() =>
							new Promise<string>((resolve) => {
								resolveInvoke = resolve;
							}),
					),
				],
			}));

			const tools = await buildMemoryMcpTools(
				"saolei",
				"sess-1",
				8080,
				clientFactory,
			);
			expect(tools).toHaveLength(1);

			const heartbeat = vi.fn();
			const config: ToolRunnableConfig & { heartbeat: () => void } = { heartbeat };
			const promise = tools[0]!.invoke({ x: 1 }, config);
			await vi.advanceTimersByTimeAsync(TOOL_HEARTBEAT_INTERVAL_MS);

			// Immediate + one tick: the wrapper is active on the production
			// choke point (R7.2 — applied in buildMemoryMcpTools, defense-in-depth).
			expect(heartbeat).toHaveBeenCalledTimes(2);

			resolveInvoke("ok");
			await promise;
			expect(vi.getTimerCount()).toBe(0);
		} finally {
			vi.useRealTimers();
		}
	});
});

// ===========================================================================
// STREAM_IDLE_TIMEOUT_MS / STREAM_IDLE_TIMEOUT_EXPLICIT — 044 US1 (T002,
// specs/044-llm-stall-recovery-fix/tasks.md T002): default raised to 120s,
// values below the 60s minimum clamped to the default, explicit env config
// >= 60s honored as-is, and the explicit-flag distinguishing "operator set
// the env var" from "default+floor" (contracts/idle-timeout-contract.md §1).
// Both constants are evaluated at module load, so each case re-imports the
// module after mutating the env var (vi.resetModules + dynamic import — no
// module mocking, per style/javascript.md §Mock 约定).
// ===========================================================================

describe("STREAM_IDLE_TIMEOUT_MS / STREAM_IDLE_TIMEOUT_EXPLICIT (044 US1 T002)", () => {
	const envKey = "GAME_STREAM_IDLE_TIMEOUT_MS";
	const original = process.env[envKey];

	afterEach(() => {
		if (original === undefined) delete process.env[envKey];
		else process.env[envKey] = original;
	});

	async function reloadConstants(): Promise<typeof import("./llm")> {
		vi.resetModules();
		return await import("./llm");
	}

	it("defaults to 120_000 with EXPLICIT=false when the env var is unset", async () => {
		delete process.env[envKey];
		const mod = await reloadConstants();
		expect(mod.STREAM_IDLE_TIMEOUT_MS).toBe(120_000);
		expect(mod.STREAM_IDLE_TIMEOUT_EXPLICIT).toBe(false);
	});

	it("clamps an explicit value below the 60s minimum to 120_000 (EXPLICIT=true)", async () => {
		process.env[envKey] = "45000";
		const mod = await reloadConstants();
		expect(mod.STREAM_IDLE_TIMEOUT_MS).toBe(120_000);
		expect(mod.STREAM_IDLE_TIMEOUT_EXPLICIT).toBe(true);
	});

	it("honors an explicit value >= 60s as-is (EXPLICIT=true)", async () => {
		process.env[envKey] = "90000";
		const mod = await reloadConstants();
		expect(mod.STREAM_IDLE_TIMEOUT_MS).toBe(90_000);
		expect(mod.STREAM_IDLE_TIMEOUT_EXPLICIT).toBe(true);
	});

	it("honors an explicit value exactly at the 60s minimum", async () => {
		process.env[envKey] = "60000";
		const mod = await reloadConstants();
		expect(mod.STREAM_IDLE_TIMEOUT_MS).toBe(60_000);
		expect(mod.STREAM_IDLE_TIMEOUT_EXPLICIT).toBe(true);
	});

	it("treats an explicit '0' as EXPLICIT=true (clamped to 120_000) — uses !== undefined, not Number truthiness", async () => {
		// tasks.md T002 names env="0" as the key boundary: "0" is explicitly
		// SET (so EXPLICIT must be true for T003's resolution rule), while its
		// numeric value 0 < 60s clamps to the default. A future "simplification"
		// to `Number(env) > 0` would flip this to EXPLICIT=false and break the
		// rule ordering in contracts/idle-timeout-contract.md §1.
		process.env[envKey] = "0";
		const mod = await reloadConstants();
		expect(mod.STREAM_IDLE_TIMEOUT_MS).toBe(120_000);
		expect(mod.STREAM_IDLE_TIMEOUT_EXPLICIT).toBe(true);
	});

	it("treats a non-numeric explicit value as EXPLICIT=true (NaN clamps to 120_000)", async () => {
		// "abc" parses to NaN: not a valid config, so the value falls back to
		// the 120s default via the 60s clamp — but the var IS explicitly set,
		// so EXPLICIT stays true (same `!== undefined` semantics as env="0").
		process.env[envKey] = "abc";
		const mod = await reloadConstants();
		expect(mod.STREAM_IDLE_TIMEOUT_MS).toBe(120_000);
		expect(mod.STREAM_IDLE_TIMEOUT_EXPLICIT).toBe(true);
	});

	it("defaults all four constants when neither env nor DOMINION_CONFIG_DIR is set (044 T015)", async () => {
		// The constants are now sourced from resolveAgentTimeouts (T015):
		// with no env and no config directory the resolver's default path
		// must reproduce the pre-amendment values exactly.
		// (specs/044-llm-stall-recovery-fix/contracts/idle-timeout-contract.md §5)
		const initEnvKey = "GAME_INIT_TURN_TIMEOUT_MS";
		const configDirKey = "DOMINION_CONFIG_DIR";
		const initOriginal = process.env[initEnvKey];
		const configDirOriginal = process.env[configDirKey];
		try {
			delete process.env[envKey];
			delete process.env[initEnvKey];
			delete process.env[configDirKey];
			const mod = await reloadConstants();
			expect(mod.STREAM_IDLE_TIMEOUT_MS).toBe(120_000);
			expect(mod.STREAM_IDLE_TIMEOUT_EXPLICIT).toBe(false);
			expect(mod.INIT_TURN_TIMEOUT_MS).toBe(120_000);
			expect(mod.TOOL_HEARTBEAT_INTERVAL_MS).toBe(10_000);
		} finally {
			if (initOriginal === undefined) delete process.env[initEnvKey];
			else process.env[initEnvKey] = initOriginal;
			if (configDirOriginal === undefined) delete process.env[configDirKey];
			else process.env[configDirKey] = configDirOriginal;
		}
	});
});

