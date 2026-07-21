/**
 * saolei-mcp.test.ts — Tests for the session-bound saolei MCP server.
 *
 * Coverage:
 *   - Phase 4 / US1: tool registration; `saolei_init` (FR-006/FR-027).
 *   - Phase 5 / US2: `saolei_click` dispatch + alternation + reject-no-lock
 *     (FR-007/FR-011/FR-013, Clarification Q3); `saolei_update` accept /
 *     reject / precondition (FR-010/FR-011/FR-013/FR-016).
 *
 * Pattern (style/javascript.md §测试): pure DI — a fake `OperationBridge`
 * is constructed via the existing test scaffolding; no `vi.mock` of the MCP
 * SDK is performed. Tools are listed via the McpServer's internal registry
 * (`server.server`'s `ListToolsRequestSchema` handler) and invoked via the
 * handler closure for each tool.
 */

import { describe, expect, it } from "vitest";

import { OperationBridge } from "../../operation-bridge";
import type { OperationResult } from "../../operation-bridge";
import { createSaoleiMcpServer, STATUS_SUCCEEDED } from "./saolei-mcp";
import { CellStatus } from "./game-state";
import { BOARD_ORIGIN_X_PX, BOARD_ORIGIN_Y_PX, CELL_SIZE_PX } from "./geometry";
import type { Part } from "../../../../game_types/projects/game/Part";
import type { AgentFrame } from "../../../../game_types/projects/game/AgentFrame";
import type { KeyboardPressPart } from "../../../../game_types/projects/game/KeyboardPressPart";
import type { MouseMoveAndClickPart } from "../../../../game_types/projects/game/MouseMoveAndClickPart";

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

/**
 * Fetch the McpServer's internal `tools/call` handler so a test can invoke
 * a registered tool by name with literal arguments (no HTTP round-trip).
 * The handler shape is part of the SDK's stable surface; accessing it via
 * `(server as any).server._requestHandlers` mirrors the existing Phase 4
 * tests.
 */
function callTool(
	server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer,
	name: string,
	arguments_: Record<string, unknown>,
): Promise<{ isError?: boolean; content: { type: string; text?: string }[] }> {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	const handler = (server as any).server._requestHandlers.get("tools/call");
	return handler({
		method: "tools/call",
		params: { name, arguments: arguments_ },
	});
}

/** Pixel centre of cell (x, y) per `geometry.center` (data-model.md §5). */
function centerX(x: number): number {
	return BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2;
}
function centerY(y: number): number {
	return BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2;
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

	// Phase 6 implements `saolei_flag` + `saolei_chord_click` (see the
	// dedicated dispatch tests below); no placeholder tools remain.
});

describe("createSaoleiMcpServer: saolei_click (FR-007 / FR-011 / FR-013)", () => {
	it("dispatches a MouseMoveAndClickPart{LEFT_CLICK, WINDOW_MESSAGE} at the cell centre (FR-007)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		// Initialise a 9x9 board so click has a valid INITIAL target.
		await callTool(server, "saolei_init", { width: 9, height: 9 });

		// Cell (3, 4): centre = (24 + 3*32 + 16, 200 + 4*32 + 16) = (136, 344).
		const result = await callTool(server, "saolei_click", { x: 3, y: 4 });

		// FR-007 / SC-002: exactly one MouseMoveAndClickPart dispatched
		// with LEFT_CLICK + WINDOW_MESSAGE + correct window-client coords.
		// (init also dispatched a KeyboardPressPart, hence length 2.)
		const clickDispatches = dispatched.filter((p) => p.mouseMoveAndClick);
		expect(clickDispatches).toHaveLength(1);
		const part = clickDispatches[0].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_LEFT_CLICK");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(part.xPx).toBe(centerX(3));
		expect(part.yPx).toBe(centerY(4));
		expect(part.toolId).toBeTruthy();

		// FR-011: dispatch enters the pending state.
		expect(state.pendingUpdate).toBe(true);
		expect(state.lastOp).toEqual({
			kind: "click",
			target: { x: 3, y: 4 },
		});

		// D8: normal MCP text result with the click-dispatched message.
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("click dispatched at (3,4)");
		expect(result.content[0].text).toContain("saolei_update");
	});

	it("forwards the desktop screenshot as an image block on accept", async () => {
		const { bridge } = makeFakeBridge({
			status: STATUS_SUCCEEDED,
			message: "ok",
			screenshot: { data: "aGVsbG8=", widthPx: 332, heightPx: 508 },
		});
		const { server } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 3, height: 3 });
		const result = await callTool(server, "saolei_click", { x: 0, y: 0 });

		// Text block + screenshot image block (research.md D8).
		expect(result.content).toHaveLength(2);
		expect(result.content[0].type).toBe("text");
		expect(result.content[1]).toEqual({
			type: "image",
			data: "aGVsbG8=",
			mimeType: "image/png",
		});
	});

	it("rejects a second click before saolei_update (FR-011 alternation)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 5, height: 5 });
		await callTool(server, "saolei_click", { x: 1, y: 1 });

		// A second click before saolei_update must be rejected and must
		// NOT dispatch a second Part (SC-003).
		const before = dispatched.length;
		const result = await callTool(server, "saolei_click", { x: 2, y: 2 });
		expect(dispatched.length).toBe(before);
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("saolei_update");

		// pendingUpdate stays true; lastOp unchanged.
		expect(state.pendingUpdate).toBe(true);
		expect(state.lastOp).toEqual({
			kind: "click",
			target: { x: 1, y: 1 },
		});
	});

	it("rejects a non-INITIAL target without dispatching and without locking (FR-013 + Clarification Q3)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 5, height: 5 });
		// Mark (0,0) as FLAG — a click there must be rejected pre-dispatch.
		state.grid[0][0] = CellStatus.FLAG;

		const before = dispatched.length;
		const result = await callTool(server, "saolei_click", { x: 0, y: 0 });

		// No new dispatch; state did not enter pending.
		expect(dispatched.length).toBe(before);
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("not INITIAL");
		expect(state.pendingUpdate).toBe(false);
		expect(state.lastOp).toBeNull();

		// Clarification Q3: the model may retry immediately with a valid
		// target — no saolei_update is required between the reject and
		// the retry.
		const retry = await callTool(server, "saolei_click", { x: 2, y: 2 });
		expect(retry.content[0].text).toContain("click dispatched at (2,2)");
		expect(state.pendingUpdate).toBe(true);
	});
});

describe("createSaoleiMcpServer: saolei_update (FR-010 / FR-011 / FR-013 / FR-016)", () => {
	it("rejects saolei_update when no operation is pending (FR-011 precondition)", async () => {
		const { bridge } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 3, height: 3 });
		// init does not enter pending state → update must be rejected.
		const result = await callTool(server, "saolei_update", {
			cells: [{ x: 0, y: 0, status: "1" }],
		});
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("no operation awaiting update");
		// State unchanged.
		expect(state.pendingUpdate).toBe(false);
		expect(state.lastOp).toBeNull();
		expect(state.grid[0][0]).toBe(CellStatus.INITIAL);
	});

	it("accepts a connected click update and clears pendingUpdate (FR-013)", async () => {
		const { bridge } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 5, height: 5 });
		await callTool(server, "saolei_click", { x: 1, y: 1 });
		expect(state.pendingUpdate).toBe(true);

		// Connected cascade reveal containing the target.
		const result = await callTool(server, "saolei_update", {
			cells: [
				{ x: 1, y: 1, status: "0" },
				{ x: 2, y: 1, status: "0" },
				{ x: 1, y: 2, status: "1" },
			],
		});
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toBe(
			"state updated; 3 cells changed; ready for next operation",
		);

		// Applied to grid; pendingUpdate cleared (FR-011 alternation).
		expect(state.grid[1][1]).toBe(CellStatus.NUMBER_0);
		expect(state.grid[1][2]).toBe(CellStatus.NUMBER_0);
		expect(state.grid[2][1]).toBe(CellStatus.NUMBER_1);
		expect(state.pendingUpdate).toBe(false);
		expect(state.lastOp).toBeNull();
	});

	it("rejects a disconnected click update and leaves state unchanged (FR-013)", async () => {
		const { bridge } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 5, height: 5 });
		await callTool(server, "saolei_click", { x: 1, y: 1 });

		// (1,1) target + a disconnected number at (4,4) — not 8-connected.
		const result = await callTool(server, "saolei_update", {
			cells: [
				{ x: 1, y: 1, status: "0" },
				{ x: 4, y: 4, status: "1" },
			],
		});
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("8-connected");

		// State unchanged; pendingUpdate stays true (the model must send a
		// corrected saolei_update, not start a new operation).
		expect(state.grid[1][1]).toBe(CellStatus.INITIAL);
		expect(state.grid[4][4]).toBe(CellStatus.INITIAL);
		expect(state.pendingUpdate).toBe(true);
		expect(state.lastOp).toEqual({
			kind: "click",
			target: { x: 1, y: 1 },
		});

		// A corrected update is then accepted.
		const corrected = await callTool(server, "saolei_update", {
			cells: [{ x: 1, y: 1, status: "3" }],
		});
		expect(corrected.content[0].text).toContain("state updated");
		expect(state.grid[1][1]).toBe(CellStatus.NUMBER_3);
		expect(state.pendingUpdate).toBe(false);
	});

	it("rejects an out-of-bounds coordinate (FR-016)", async () => {
		const { bridge } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 3, height: 3 });
		await callTool(server, "saolei_click", { x: 0, y: 0 });

		const result = await callTool(server, "saolei_update", {
			cells: [
				{ x: 0, y: 0, status: "1" },
				{ x: 99, y: 0, status: "1" },
			],
		});
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("out of bounds");
		// State unchanged.
		expect(state.grid[0][0]).toBe(CellStatus.INITIAL);
		expect(state.pendingUpdate).toBe(true);
	});

	it("accepts HIT_MINE on the target with no connectivity requirement (FR-018)", async () => {
		const { bridge } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 5, height: 5 });
		await callTool(server, "saolei_click", { x: 2, y: 2 });

		// Game over: target is HIT_MINE; other mines may be revealed as
		// MINE at arbitrary positions (no connectivity check).
		const result = await callTool(server, "saolei_update", {
			cells: [
				{ x: 2, y: 2, status: "HIT_MINE" },
				{ x: 0, y: 0, status: "MINE" },
				{ x: 4, y: 4, status: "MINE" },
			],
		});
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("state updated");
		expect(state.grid[2][2]).toBe(CellStatus.HIT_MINE);
		expect(state.grid[0][0]).toBe(CellStatus.MINE);
		expect(state.grid[4][4]).toBe(CellStatus.MINE);
		expect(state.pendingUpdate).toBe(false);
	});
});

describe("createSaoleiMcpServer: init → click → update → click cycle", () => {
	it("after a successful update, the next click is allowed (alternation reset)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 5, height: 5 });
		await callTool(server, "saolei_click", { x: 0, y: 0 });
		await callTool(server, "saolei_update", {
			cells: [{ x: 0, y: 0, status: "1" }],
		});

		// Next operation is allowed (no longer pending).
		const before = dispatched.length;
		const result = await callTool(server, "saolei_click", { x: 2, y: 2 });
		expect(result.content[0].text).toContain("click dispatched at (2,2)");
		// A second MouseMoveAndClickPart was dispatched.
		const clickDispatches = dispatched
			.slice(before)
			.filter((p) => p.mouseMoveAndClick);
		expect(clickDispatches).toHaveLength(1);
	});
});

describe("createSaoleiMcpServer: saolei_flag (FR-008 / FR-011 / FR-014)", () => {
	it("dispatches a MouseMoveAndClickPart{RIGHT_CLICK, WINDOW_MESSAGE} at the cell centre (FR-008)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 9, height: 9 });

		// Cell (2, 3): centre = (24 + 2*32 + 16, 200 + 3*32 + 16) = (104, 312).
		const result = await callTool(server, "saolei_flag", { x: 2, y: 3 });

		// FR-008 / SC-002: exactly one MouseMoveAndClickPart dispatched
		// with RIGHT_CLICK + WINDOW_MESSAGE + correct window-client coords.
		const flagDispatches = dispatched.filter((p) => p.mouseMoveAndClick);
		expect(flagDispatches).toHaveLength(1);
		const part = flagDispatches[0].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_RIGHT_CLICK");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(part.xPx).toBe(centerX(2));
		expect(part.yPx).toBe(centerY(3));
		expect(part.toolId).toBeTruthy();

		// FR-011: dispatch enters the pending state with kind=flag.
		expect(state.pendingUpdate).toBe(true);
		expect(state.lastOp).toEqual({
			kind: "flag",
			target: { x: 2, y: 3 },
		});

		// D8: normal MCP text result with the flag-dispatched message.
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("flag dispatched at (2,3)");
		expect(result.content[0].text).toContain("saolei_update");
	});

	it("rejects a second flag before saolei_update (FR-011 alternation)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 5, height: 5 });
		await callTool(server, "saolei_flag", { x: 1, y: 1 });

		const before = dispatched.length;
		const result = await callTool(server, "saolei_flag", { x: 2, y: 2 });
		expect(dispatched.length).toBe(before);
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("saolei_update");

		// pendingUpdate stays true; lastOp unchanged.
		expect(state.pendingUpdate).toBe(true);
		expect(state.lastOp).toEqual({
			kind: "flag",
			target: { x: 1, y: 1 },
		});
	});

	it("rejects a non-INITIAL target without dispatching and without locking (FR-014 + Clarification Q3)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 5, height: 5 });
		// Mark (0,0) as a revealed number — a flag there must be rejected.
		state.grid[0][0] = CellStatus.NUMBER_1;

		const before = dispatched.length;
		const result = await callTool(server, "saolei_flag", { x: 0, y: 0 });

		// No new dispatch; state did not enter pending.
		expect(dispatched.length).toBe(before);
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("not INITIAL");
		expect(state.pendingUpdate).toBe(false);
		expect(state.lastOp).toBeNull();

		// Clarification Q3: the model may retry immediately with a valid
		// target — no saolei_update is required between the reject and
		// the retry.
		const retry = await callTool(server, "saolei_flag", { x: 2, y: 2 });
		expect(retry.content[0].text).toContain("flag dispatched at (2,2)");
		expect(state.pendingUpdate).toBe(true);
	});
});

describe("createSaoleiMcpServer: saolei_chord_click (FR-009 / FR-011 / FR-015)", () => {
	/**
	 * Build a session whose grid has target (1,1) as a "2" with adjacent
	 * flags at (0,0) and (2,2) — a satisfied chord target. Used by the
	 * chord dispatch / accept tests.
	 */
	async function makeSatisfiedChordSession(): Promise<{
		server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer;
		state: import("./game-state").GameState;
		dispatched: Part[];
		bridge: OperationBridge;
	}> {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);
		await callTool(server, "saolei_init", { width: 3, height: 3 });
		state.grid[1][1] = CellStatus.NUMBER_2;
		state.grid[0][0] = CellStatus.FLAG;
		state.grid[2][2] = CellStatus.FLAG;
		return { server, state, dispatched, bridge };
	}

	it("dispatches a MouseMoveAndClickPart{LEFT_RIGHT_PRESS, WINDOW_MESSAGE} at the cell centre (FR-009)", async () => {
		const { server, state, dispatched } = await makeSatisfiedChordSession();

		// Cell (1, 1): centre = (24 + 1*32 + 16, 200 + 1*32 + 16) = (72, 248).
		const result = await callTool(server, "saolei_chord_click", { x: 1, y: 1 });

		// FR-009 / SC-002: exactly one MouseMoveAndClickPart dispatched
		// with LEFT_RIGHT_PRESS + WINDOW_MESSAGE + correct window-client
		// coords. This is a single simultaneous left+right press — NOT two
		// clicks and NOT a double-click (research.md D7).
		const chordDispatches = dispatched.filter((p) => p.mouseMoveAndClick);
		expect(chordDispatches).toHaveLength(1);
		const part = chordDispatches[0].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(part.xPx).toBe(centerX(1));
		expect(part.yPx).toBe(centerY(1));
		expect(part.toolId).toBeTruthy();

		// FR-011: dispatch enters the pending state with kind=chord_click.
		expect(state.pendingUpdate).toBe(true);
		expect(state.lastOp).toEqual({
			kind: "chord_click",
			target: { x: 1, y: 1 },
		});

		// D8: normal MCP text result with the chord-dispatched message.
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("chord_click dispatched at (1,1)");
		expect(result.content[0].text).toContain("saolei_update");
	});

	it("rejects a chord on an unsatisfied number (flag count ≠ number) without dispatching (FR-015)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 3, height: 3 });
		// Target (1,1) is "3" but only 2 flags adjacent — unsatisfied.
		state.grid[1][1] = CellStatus.NUMBER_3;
		state.grid[0][0] = CellStatus.FLAG;
		state.grid[2][2] = CellStatus.FLAG;

		const before = dispatched.length;
		const result = await callTool(server, "saolei_chord_click", { x: 1, y: 1 });

		// No dispatch; state unchanged; not pending (Clarification Q3).
		expect(dispatched.length).toBe(before);
		expect(result.isError).toBeFalsy();
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("number=3");
		expect(result.content[0].text).toContain("adjacent flags=2");
		expect(state.pendingUpdate).toBe(false);
		expect(state.lastOp).toBeNull();
	});

	it("rejects a chord on a non-number target without dispatching (FR-015)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 3, height: 3 });
		// Target (1,1) is INITIAL — chord requires a non-0 number.
		const before = dispatched.length;
		const result = await callTool(server, "saolei_chord_click", { x: 1, y: 1 });

		expect(dispatched.length).toBe(before);
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("non-0 number 1..8");
		expect(state.pendingUpdate).toBe(false);
	});

	it("rejects a second chord before saolei_update (FR-011 alternation)", async () => {
		const { server, dispatched, state } = await makeSatisfiedChordSession();

		await callTool(server, "saolei_chord_click", { x: 1, y: 1 });

		const before = dispatched.length;
		const result = await callTool(server, "saolei_chord_click", { x: 1, y: 1 });
		expect(dispatched.length).toBe(before);
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("saolei_update");
		expect(state.pendingUpdate).toBe(true);
		expect(state.lastOp).toEqual({
			kind: "chord_click",
			target: { x: 1, y: 1 },
		});
	});

	it("rejects a chord with a non-adjacent HIT_MINE in the update (FR-015 mine-hit adjacency)", async () => {
		// Use a 6x6 grid so (5,5) is in-bounds but far from target (1,1).
		const { bridge } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);
		await callTool(server, "saolei_init", { width: 6, height: 6 });
		state.grid[1][1] = CellStatus.NUMBER_2;
		state.grid[0][0] = CellStatus.FLAG;
		state.grid[2][2] = CellStatus.FLAG;
		await callTool(server, "saolei_chord_click", { x: 1, y: 1 });

		const result = await callTool(server, "saolei_update", {
			cells: [{ x: 5, y: 5, status: "HIT_MINE" }],
		});
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("hit-mine");
		// State unchanged — pendingUpdate stays true.
		expect(state.pendingUpdate).toBe(true);
	});
});

describe("createSaoleiMcpServer: saolei_update routes by lastOp.kind", () => {
	it("flag update: applies a single-cell INITIAL→FLAG toggle (FR-014)", async () => {
		const { bridge } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 5, height: 5 });
		await callTool(server, "saolei_flag", { x: 2, y: 2 });

		// Apply the toggle: only (2,2) → FLAG.
		const result = await callTool(server, "saolei_update", {
			cells: [{ x: 2, y: 2, status: "FLAG" }],
		});
		expect(result.content[0].text).toContain("state updated");
		expect(state.grid[2][2]).toBe(CellStatus.FLAG);
		expect(state.pendingUpdate).toBe(false);
		expect(state.lastOp).toBeNull();
	});

	it("flag update: rejects an extraneous cell alongside the toggle (FR-014)", async () => {
		const { bridge } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 5, height: 5 });
		await callTool(server, "saolei_flag", { x: 2, y: 2 });

		const result = await callTool(server, "saolei_update", {
			cells: [
				{ x: 2, y: 2, status: "FLAG" },
				{ x: 0, y: 0, status: "1" },
			],
		});
		expect(result.content[0].text).toContain("rejected");
		expect(result.content[0].text).toContain("must change only the target cell");
		// State unchanged.
		expect(state.grid[2][2]).toBe(CellStatus.INITIAL);
		expect(state.grid[0][0]).toBe(CellStatus.INITIAL);
		expect(state.pendingUpdate).toBe(true);
	});

	it("chord update: applies a satisfied chord revealing every non-flag neighbour (FR-015)", async () => {
		const { bridge } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 3, height: 3 });
		state.grid[1][1] = CellStatus.NUMBER_2;
		state.grid[0][0] = CellStatus.FLAG;
		state.grid[2][2] = CellStatus.FLAG;
		await callTool(server, "saolei_chord_click", { x: 1, y: 1 });

		// Reveal the 6 non-flag neighbours of (1,1).
		const result = await callTool(server, "saolei_update", {
			cells: [
				{ x: 0, y: 1, status: "1" },
				{ x: 0, y: 2, status: "1" },
				{ x: 1, y: 0, status: "1" },
				{ x: 1, y: 2, status: "1" },
				{ x: 2, y: 0, status: "1" },
				{ x: 2, y: 1, status: "1" },
			],
		});
		expect(result.content[0].text).toContain("state updated");
		expect(state.grid[0][1]).toBe(CellStatus.NUMBER_1);
		expect(state.grid[0][0]).toBe(CellStatus.FLAG); // preserved
		expect(state.grid[2][2]).toBe(CellStatus.FLAG); // preserved
		expect(state.pendingUpdate).toBe(false);
	});

	it("chord update: accepts a mine-hit exception with only the hit mine (FR-019)", async () => {
		const { bridge } = makeFakeBridge();
		const { server, state } = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", { width: 3, height: 3 });
		state.grid[1][1] = CellStatus.NUMBER_2;
		state.grid[0][0] = CellStatus.FLAG;
		state.grid[2][2] = CellStatus.FLAG;
		await callTool(server, "saolei_chord_click", { x: 1, y: 1 });

		// Misplaced flag — chord detonates the mine at (2,0). The other
		// neighbours are NOT required to be updated.
		const result = await callTool(server, "saolei_update", {
			cells: [{ x: 2, y: 0, status: "HIT_MINE" }],
		});
		expect(result.content[0].text).toContain("state updated");
		// grid is indexed grid[y][x]; (x=2, y=0) → grid[0][2].
		expect(state.grid[0][2]).toBe(CellStatus.HIT_MINE);
		expect(state.pendingUpdate).toBe(false);
	});
});
