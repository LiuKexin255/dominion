/**
 * saolei-mcp.test.ts — Tests for the session-bound saolei MCP server.
 *
 * Coverage (Phase 4 / US1):
 *   - All five tools are registered with the contracted names.
 *   - `saolei_init(width, height)` dispatches a `KeyboardPressPart{F2}` and
 *     initialises the per-session GameState (FR-006/FR-027).
 *
 * Pattern (style/javascript.md §测试): pure DI — a fake `OperationBridge`
 * is constructed via the existing test scaffolding; no `vi.mock` of the MCP
 * SDK is performed. Tools are listed via the McpServer's internal registry
 * (`server.server`'s `ListToolsRequestSchema` handler) and invoked via the
 * handler closure for `saolei_init`.
 */

import { describe, expect, it, vi } from "vitest";

import { OperationBridge } from "../../operation-bridge";
import type { OperationResult } from "../../operation-bridge";
import { createSaoleiMcpServer, STATUS_SUCCEEDED } from "./saolei-mcp";
import { CellStatus } from "./game-state";
import type { Part } from "../../../../game_types/projects/game/Part";
import type { AgentFrame } from "../../../../game_types/projects/game/AgentFrame";
import type { KeyboardPressPart } from "../../../../game_types/projects/game/KeyboardPressPart";

/**
 * Build a fake OperationBridge whose dispatch records the dispatched Part
 * and resolves a canned SUCCEEDED result. The fake simulates the desktop
 * side of the bidi stream — registerSink + handleResult — without spinning
 * up a real connection (style/javascript.md §测试 — DI seam).
 */
function makeFakeBridge(
	canned: OperationResult = { status: STATUS_SUCCEEDED, message: "ok" },
): { bridge: OperationBridge; dispatched: Part[] } {
	const bridge = new OperationBridge();
	const dispatched: Part[] = [];
	bridge.registerSink((frame: AgentFrame) => {
		const part = frame.content?.parts?.[0];
		if (part) dispatched.push(part);
		// Resolve the dispatch with the canned result by feeding the bridge
		// a matching ToolResultPart for each tool_id observed on the part.
		const toolId =
			part?.keyboardPress?.toolId ??
			part?.mouseMove?.toolId ??
			part?.mouseClick?.toolId ??
			part?.mouseMoveAndClick?.toolId ??
			"";
		if (toolId) {
			bridge.handleResult({
				toolId,
				status: canned.status,
				message: canned.message,
				screenshot: canned.screenshot,
			} as any);
		}
	});
	return { bridge, dispatched };
}

describe("createSaoleiMcpServer", () => {
	it("registers exactly the five contracted saolei tools", async () => {
		const { bridge } = makeFakeBridge();
		const { server } = createSaoleiMcpServer(bridge);

		// The McpServer exposes its registered tools through the underlying
		// Server's ListToolsRequest handler. Invoke it directly so the test
		// asserts the wire-visible tool list without an HTTP round-trip.
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const inner = (server as any).server;
		const handler = inner._requestHandlers.get("tools/list");
		expect(handler).toBeDefined();

		const result = await handler({
			method: "tools/list",
			params: {},
		});

		const names = (result.tools as { name: string }[]).map((t) => t.name);
		expect(names.sort()).toEqual(
			[
				"saolei_chord_click",
				"saolei_click",
				"saolei_flag",
				"saolei_init",
				"saolei_update",
			].sort(),
		);
	});

	it("saolei_init dispatches an F2 KeyboardPressPart and inits GameState", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		expect(state.initialized).toBe(false);

		// Call the saolei_init handler directly through the McpServer's
		// internal tool-call plumbing.
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const inner = (server as any).server;
		const handler = inner._requestHandlers.get("tools/call");
		expect(handler).toBeDefined();

		const result = await handler({
			method: "tools/call",
			params: { name: "saolei_init", arguments: { width: 9, height: 9 } },
		});

		// FR-006 (a): exactly one KeyboardPressPart{F2} dispatched.
		expect(dispatched).toHaveLength(1);
		const part = dispatched[0];
		const keyPart = part.keyboardPress as KeyboardPressPart | undefined;
		expect(keyPart).toBeDefined();
		expect(keyPart?.key).toBe("KEYBOARD_KEY_F2");
		expect(keyPart?.toolId).toBeTruthy();

		// FR-006 (b): GameState initialised to 9x9 INITIAL, alternation off.
		expect(state.initialized).toBe(true);
		expect(state.width).toBe(9);
		expect(state.height).toBe(9);
		expect(state.grid).toHaveLength(9);
		expect(state.grid[0]).toHaveLength(9);
		expect(state.grid[0][0]).toBe(CellStatus.INITIAL);
		expect(state.pendingUpdate).toBe(false);
		expect(state.lastOp).toBeNull();

		// research.md D8: result is a normal MCP text result, isError absent.
		expect(result.isError).toBeFalsy();
		expect(Array.isArray(result.content)).toBe(true);
		const textBlocks = result.content.filter((b: { type: string }) => b.type === "text");
		expect(textBlocks.length).toBeGreaterThanOrEqual(1);
		expect(textBlocks[0].text).toContain("game initialized");
		expect(textBlocks[0].text).toContain("9x9");
		// ISSUE-5 contract alignment: the text matches
		// contracts/mcp-tool-contract.md `saolei_init` Result verbatim
		// ("(F2 dispatched)" — no extra status fragment).
		expect(textBlocks[0].text).toBe(
			"game initialized (F2 dispatched); grid 9x9, all cells INITIAL",
		);
		// No screenshot in the canned result → no image block.
		const imageBlocks = result.content.filter((b: { type: string }) => b.type === "image");
		expect(imageBlocks).toHaveLength(0);
	});

	it("saolei_init returns the desktop screenshot as an image block (BUG-1 regression)", async () => {
		// FR-006 / contracts/mcp-tool-contract.md `saolei_init` Result: a
		// text block plus, if the desktop returned a screenshot, an image
		// content block. The image MUST be forwarded verbatim so the model
		// can read the post-init board.
		//
		// The MCP SDK validates `image.data` as base64 — use a real base64
		// string (aGVsbG8= is "hello") so the result passes schema validation.
		const { bridge } = makeFakeBridge({
			status: STATUS_SUCCEEDED,
			message: "ok",
			screenshot: {
				data: "aGVsbG8=",
				widthPx: 332,
				heightPx: 508,
			},
		});
		const { server } = createSaoleiMcpServer(bridge);

		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const handler = (server as any).server._requestHandlers.get("tools/call");

		const result = await handler({
			method: "tools/call",
			params: { name: "saolei_init", arguments: { width: 9, height: 9 } },
		});

		// Two blocks: the text status + the screenshot image.
		expect(result.content).toHaveLength(2);
		expect(result.content[0].type).toBe("text");
		expect(result.content[1]).toEqual({
			type: "image",
			data: "aGVsbG8=",
			mimeType: "image/png",
		});
	});

	it("saolei_init re-init resets state and re-dispatches F2 (FR-027)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const handler = (server as any).server._requestHandlers.get("tools/call");

		await handler({
			method: "tools/call",
			params: { name: "saolei_init", arguments: { width: 9, height: 9 } },
		});

		// Mutate state to simulate play in progress.
		state.pendingUpdate = true;
		state.lastOp = { kind: "click", target: { x: 0, y: 0 } };
		state.grid[0][0] = CellStatus.NUMBER_1;

		await handler({
			method: "tools/call",
			params: { name: "saolei_init", arguments: { width: 16, height: 16 } },
		});

		// FR-027: re-init resets state and discards prior dimensions.
		expect(dispatched).toHaveLength(2); // F2 re-dispatched
		expect((dispatched[1].keyboardPress as KeyboardPressPart).key).toBe(
			"KEYBOARD_KEY_F2",
		);
		expect(state.width).toBe(16);
		expect(state.height).toBe(16);
		expect(state.grid).toHaveLength(16);
		expect(state.grid[0]).toHaveLength(16);
		expect(state.grid[0][0]).toBe(CellStatus.INITIAL);
		expect(state.pendingUpdate).toBe(false);
		expect(state.lastOp).toBeNull();
	});

	it("placeholder tools return 'not yet implemented' as a normal result", async () => {
		const { bridge } = makeFakeBridge();
		const { server } = createSaoleiMcpServer(bridge);

		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const handler = (server as any).server._requestHandlers.get("tools/call");

		for (const name of [
			"saolei_click",
			"saolei_flag",
			"saolei_chord_click",
			"saolei_update",
		]) {
			const args =
				name === "saolei_update"
					? { cells: [{ x: 0, y: 0, status: "1" }] }
					: { x: 0, y: 0 };
			const result = await handler({
				method: "tools/call",
				params: { name, arguments: args },
			});
			expect(result.isError).toBeFalsy();
			expect(result.content[0].text).toContain("not yet implemented");
		}
	});
});
