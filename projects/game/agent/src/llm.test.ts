/**
 * llm.test.ts — Tests for the shared LLM types/helpers retained after the
 * single-agent adapter path was removed
 * (specs/031-team-template-mode/tasks.md T022): `buildTools`, `toParts`,
 * `buildContentBlocks`, and the message-history helpers.
 */

import { HumanMessage, AIMessage, ToolMessage } from "@langchain/core/messages";
import { describe, expect, it } from "vitest";

import {
	buildTools,
	buildContentBlocks,
	toParts,
	extractToolCalls,
	readToolResultStatus,
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
