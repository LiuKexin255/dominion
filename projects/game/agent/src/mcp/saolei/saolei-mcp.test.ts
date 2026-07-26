/**
 * saolei-mcp.test.ts — Tests for the stateless saolei MCP server.
 *
 * Coverage (Phase 4 / US3,
 * `specs/023-saolei-mcp-refine/contracts/tool-dispatch-contract.md` §6;
 * `specs/023-saolei-mcp-refine/data-model.md` §7;
 * `specs/023-saolei-mcp-refine/research.md` D7/D12):
 *   - Exactly four tools are exposed (`saolei_init`, `saolei_click`,
 *     `saolei_flag`, `saolei_chord_click`); `saolei_update` is absent
 *     (FR-016).
 *   - `saolei_init` takes no `width`/`height` arguments (FR-019 / C11).
 *   - Each tool dispatches the unchanged proto operation Part
 *     (`KeyboardPressPart{F2}`, `MouseMoveAndClickPart{<ACTION>,
 *     WINDOW_MESSAGE}`) at the cell centre (FR-020).
 *   - Back-to-back `saolei_click` both dispatch (FR-021 — no alternation).
 *   - Each tool returns MCP content blocks with NO `additional_kwargs`
 *     (neutral status, D12).
 *
 * Pattern (`style/javascript.md` §测试): pure DI — a fake `OperationBridge`
 * is constructed via the existing test scaffolding; no `vi.mock` of the MCP
 * SDK is performed. Tools are listed via the McpServer's internal registry
 * (`server.server`'s `ListToolsRequestSchema` handler) and invoked via the
 * `tools/call` handler closure.
 */

import { describe, expect, it } from "vitest";

import { OperationBridge } from "../../operation-bridge";
import type { OperationResult } from "../../operation-bridge";
import { createSaoleiMcpServer } from "./saolei-mcp";
import { BOARD_ORIGIN_X_PX, BOARD_ORIGIN_Y_PX, CELL_SIZE_PX } from "./geometry";
import type { FlowPart } from "../../../../game_types/projects/game/FlowPart";
import type { AgentFrame } from "../../../../game_types/projects/game/AgentFrame";
import type { KeyboardPressPart } from "../../../../game_types/projects/game/KeyboardPressPart";
import type { MouseMoveAndClickPart } from "../../../../game_types/projects/game/MouseMoveAndClickPart";

/**
 * String status carried by `OperationResult.status` (proto enum). Used to
 * label the canned fake-bridge result.
 */
const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";

/** A captured operation FlowPart (the only kind saolei now dispatches). */
type CapturedPart = FlowPart;

/**
 * Build a fake OperationBridge whose dispatch records the dispatched FlowPart
 * and resolves a canned SUCCEEDED result. The fake simulates the desktop
 * side of the bidi stream — registerSink + handleResult — without spinning
 * up a real connection (`style/javascript.md` §测试 — DI seam).
 *
 * T028: the fake also records the AbortSignal each dispatch received (via a
 * dispatch wrapper) so tests can assert the MCP `extra.signal` is forwarded
 * to dispatch (`specs/023-saolei-mcp-refine/contracts/tool-dispatch-contract.md`
 * §6). Signal-less dispatches (the legacy single-arg form) are not used by
 * saolei anymore.
 *
 * Stateless saolei no longer forwards display-only results (no
 * `saolei_update`, no `pushResult`), so the sink captures ONLY operation
 * dispatches (FlowParts envelopes).
 */
function makeFakeBridge(
	canned: OperationResult = { status: STATUS_SUCCEEDED, message: "ok" },
): { bridge: OperationBridge; dispatched: CapturedPart[]; signals: AbortSignal[] } {
	const bridge = new OperationBridge();
	const dispatched: CapturedPart[] = [];
	const signals: AbortSignal[] = [];
	bridge.registerSink((frame: AgentFrame) => {
		const op = frame.flowParts?.parts?.[0] as CapturedPart | undefined;
		if (!op) return;
		dispatched.push(op);
		// Resolve the dispatch with the canned result by feeding the bridge
		// a matching ToolResultPart for the tool_id the bridge minted.
		const toolId =
			op.keyboardPress?.toolId ??
			op.mouseMove?.toolId ??
			op.mouseClick?.toolId ??
			op.mouseMoveAndClick?.toolId ??
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
	// Wrap dispatch to capture the signal each tool handler forwards
	// (T028). The wrapper delegates to the real dispatch so the bridge's
	// pending-map / sink / handleResult mechanics stay intact.
	const origDispatch = bridge.dispatch.bind(bridge);
	bridge.dispatch = (part: FlowPart, signal?: AbortSignal) => {
		if (signal) signals.push(signal);
		return origDispatch(part, signal);
	};
	return { bridge, dispatched, signals };
}

/**
 * Invoke a registered tool by name with literal arguments via the McpServer's
 * internal `tools/call` handler (no HTTP round-trip). The handler shape is
 * part of the SDK's stable surface; accessing it via
 * `(server as any).server._requestHandlers` mirrors the existing test pattern.
 *
 * T028: the SDK `tools/call` handler signature is `(request, extra)` where
 * `extra: RequestHandlerExtra` carries the AbortSignal the production path
 * obtains from the transport. Tests pass a fake `extra` (a fresh, non-aborted
 * AbortSignal) so the tool handler can read `extra.signal` and forward it to
 * `bridge.dispatch`. Callers MAY override `extra` (e.g. to pass an aborted
 * signal) via the optional third argument.
 */
function callTool(
	server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer,
	name: string,
	arguments_: Record<string, unknown>,
	extra?: { signal: AbortSignal },
): Promise<{
	isError?: boolean;
	content: { type: string; text?: string; data?: string; mimeType?: string }[];
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	additional_kwargs?: any;
}> {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	const handler = (server as any).server._requestHandlers.get("tools/call");
	const fakeExtra = extra ?? { signal: new AbortController().signal };
	return handler({
		method: "tools/call",
		params: { name, arguments: arguments_ },
	}, fakeExtra);
}

/** Pixel centre of cell (x, y) per `geometry.center`
 * (specs/024-tool-render-coord-fix/data-model.md §3). */
function centerX(x: number): number {
	return BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2;
}
function centerY(y: number): number {
	return BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2;
}

describe("createSaoleiMcpServer: tool registration (FR-016)", () => {
	it("registers exactly the four stateless saolei tools (no saolei_update)", async () => {
		const { bridge } = makeFakeBridge();
		const server = createSaoleiMcpServer(bridge);

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
			].sort(),
		);
		// FR-016: saolei_update MUST be absent.
		expect(names).not.toContain("saolei_update");
	});

	it("saolei_init inputSchema has no width/height properties (FR-019 / C11)", async () => {
		const { bridge } = makeFakeBridge();
		const server = createSaoleiMcpServer(bridge);

		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const inner = (server as any).server;
		const handler = inner._requestHandlers.get("tools/list");
		const result = await handler({
			method: "tools/list",
			params: {},
		});

		const init = (result.tools as { name: string; inputSchema?: { properties?: Record<string, unknown> } }[]).find(
			(t) => t.name === "saolei_init",
		);
		expect(init).toBeDefined();
		const props = init?.inputSchema?.properties ?? {};
		expect(props).not.toHaveProperty("width");
		expect(props).not.toHaveProperty("height");
	});
});

describe("createSaoleiMcpServer: saolei_init (FR-019)", () => {
	it("dispatches an F2 KeyboardPressPart and returns neutral MCP content", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const server = createSaoleiMcpServer(bridge);

		const result = await callTool(server, "saolei_init", {});

		// FR-019: exactly one KeyboardPressPart{F2} dispatched.
		expect(dispatched).toHaveLength(1);
		const part = dispatched[0];
		const keyPart = part.keyboardPress as KeyboardPressPart | undefined;
		expect(keyPart).toBeDefined();
		expect(keyPart?.key).toBe("KEYBOARD_KEY_F2");
		expect(keyPart?.toolId).toBeTruthy();

		// D12: normal MCP text result with neutral status — isError absent
		// and NO additional_kwargs (the adapter-wrapped ToolMessage carries
		// TOOL_RESULT_STATUS_UNSPECIFIED).
		expect(result.isError).toBeFalsy();
		expect(Array.isArray(result.content)).toBe(true);
		expect(result.additional_kwargs).toBeUndefined();
		const textBlocks = result.content.filter((b) => b.type === "text");
		expect(textBlocks).toHaveLength(1);
		expect(textBlocks[0].text).toBe("saolei_init: F2 dispatched (new game)");
		// No screenshot in the canned result → no image block.
		const imageBlocks = result.content.filter((b) => b.type === "image");
		expect(imageBlocks).toHaveLength(0);
	});

	it("forwards the desktop screenshot as an image block", async () => {
		const { bridge } = makeFakeBridge({
			status: STATUS_SUCCEEDED,
			message: "ok",
			screenshot: {
				data: "aGVsbG8=",
				widthPx: 332,
				heightPx: 508,
			},
		});
		const server = createSaoleiMcpServer(bridge);

		const result = await callTool(server, "saolei_init", {});

		// Two blocks: the text status + the screenshot image.
		expect(result.content).toHaveLength(2);
		expect(result.content[0].type).toBe("text");
		expect(result.content[1]).toEqual({
			type: "image",
			data: "aGVsbG8=",
			mimeType: "image/png",
		});
		// D12: neutral — no additional_kwargs.
		expect(result.additional_kwargs).toBeUndefined();
	});

	it("re-calling re-dispatches F2 (FR-019 restart)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const server = createSaoleiMcpServer(bridge);

		await callTool(server, "saolei_init", {});
		await callTool(server, "saolei_init", {});

		// Two F2 dispatches; no state, no alternation.
		expect(dispatched).toHaveLength(2);
		expect((dispatched[0].keyboardPress as KeyboardPressPart).key).toBe(
			"KEYBOARD_KEY_F2",
		);
		expect((dispatched[1].keyboardPress as KeyboardPressPart).key).toBe(
			"KEYBOARD_KEY_F2",
		);
	});
});

describe("createSaoleiMcpServer: saolei_click (FR-020 / FR-021)", () => {
	it("dispatches a MouseMoveAndClickPart{LEFT_CLICK, WINDOW_MESSAGE} at the cell centre", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const server = createSaoleiMcpServer(bridge);

		// Cell (3, 4): centre = (24 + 3*32 + 16, 104 + 4*32 + 16) = (136, 248)
		// in WM_* client space (BOARD_ORIGIN_Y_PX = 200 − CHROME_OFFSET_Y_PX 96 = 104;
		// specs/024-tool-render-coord-fix/data-model.md §3).
		const result = await callTool(server, "saolei_click", { x: 3, y: 4 });

		// FR-020: exactly one MouseMoveAndClickPart dispatched with LEFT_CLICK
		// + WINDOW_MESSAGE + correct window-client coords. The desktop-facing
		// proto operation Part fields are unchanged from spec 018.
		expect(dispatched).toHaveLength(1);
		const part = dispatched[0].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_LEFT_CLICK");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(part.xPx).toBe(centerX(3));
		expect(part.yPx).toBe(centerY(4));
		expect(part.toolId).toBeTruthy();

		// D12: neutral MCP content (no additional_kwargs).
		expect(result.isError).toBeFalsy();
		expect(result.additional_kwargs).toBeUndefined();
		expect(result.content[0].type).toBe("text");
		expect(result.content[0].text).toBe(
			"saolei_click dispatched at (3,4)",
		);
	});

	it("accepts a second click immediately with no intervening step (FR-021)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const server = createSaoleiMcpServer(bridge);

		// FR-021: tools are callable back-to-back; a second operation MUST NOT
		// be rejected for lack of an update (no alternation, no saolei_update).
		const r1 = await callTool(server, "saolei_click", { x: 3, y: 4 });
		const r2 = await callTool(server, "saolei_click", { x: 5, y: 6 });

		// Both dispatched successfully — no "must update first" rejection.
		expect(dispatched).toHaveLength(2);
		const p1 = dispatched[0].mouseMoveAndClick as MouseMoveAndClickPart;
		const p2 = dispatched[1].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(p1.click).toBe("MOUSE_CLICK_ACTION_LEFT_CLICK");
		expect(p1.xPx).toBe(centerX(3));
		expect(p1.yPx).toBe(centerY(4));
		expect(p2.click).toBe("MOUSE_CLICK_ACTION_LEFT_CLICK");
		expect(p2.xPx).toBe(centerX(5));
		expect(p2.yPx).toBe(centerY(6));
		// Each dispatched operation has its own bridge-minted tool_id (D10).
		expect(p1.toolId).toBeTruthy();
		expect(p2.toolId).toBeTruthy();
		expect(p1.toolId).not.toBe(p2.toolId);

		// Both results are neutral MCP content (D12).
		expect(r1.additional_kwargs).toBeUndefined();
		expect(r2.additional_kwargs).toBeUndefined();
		expect(r1.content[0].text).toBe("saolei_click dispatched at (3,4)");
		expect(r2.content[0].text).toBe("saolei_click dispatched at (5,6)");
	});

	it("forwards the desktop screenshot as an image block", async () => {
		const { bridge } = makeFakeBridge({
			status: STATUS_SUCCEEDED,
			message: "ok",
			screenshot: { data: "aGVsbG8=", widthPx: 332, heightPx: 508 },
		});
		const server = createSaoleiMcpServer(bridge);

		const result = await callTool(server, "saolei_click", { x: 0, y: 0 });

		// Text block + screenshot image block.
		expect(result.content).toHaveLength(2);
		expect(result.content[0].type).toBe("text");
		expect(result.content[1]).toEqual({
			type: "image",
			data: "aGVsbG8=",
			mimeType: "image/png",
		});
		expect(result.additional_kwargs).toBeUndefined();
	});
});

describe("createSaoleiMcpServer: saolei_flag (FR-020)", () => {
	it("dispatches a MouseMoveAndClickPart{RIGHT_CLICK, WINDOW_MESSAGE} at the cell centre", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const server = createSaoleiMcpServer(bridge);

		// Cell (2, 3): centre = (24 + 2*32 + 16, 104 + 3*32 + 16) = (104, 216)
		// in WM_* client space (BOARD_ORIGIN_Y_PX = 200 − CHROME_OFFSET_Y_PX 96 = 104;
		// specs/024-tool-render-coord-fix/data-model.md §3).
		const result = await callTool(server, "saolei_flag", { x: 2, y: 3 });

		expect(dispatched).toHaveLength(1);
		const part = dispatched[0].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_RIGHT_CLICK");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(part.xPx).toBe(centerX(2));
		expect(part.yPx).toBe(centerY(3));
		expect(part.toolId).toBeTruthy();

		expect(result.additional_kwargs).toBeUndefined();
		expect(result.content[0].text).toBe("saolei_flag dispatched at (2,3)");
	});
});

describe("createSaoleiMcpServer: saolei_chord_click (FR-020)", () => {
	it("dispatches a MouseMoveAndClickPart{LEFT_RIGHT_PRESS, WINDOW_MESSAGE} at the cell centre", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const server = createSaoleiMcpServer(bridge);

		// Cell (1, 1): centre = (24 + 1*32 + 16, 104 + 1*32 + 16) = (72, 152)
		// in WM_* client space (BOARD_ORIGIN_Y_PX = 200 − CHROME_OFFSET_Y_PX 96 = 104;
		// specs/024-tool-render-coord-fix/data-model.md §3).
		const result = await callTool(server, "saolei_chord_click", { x: 1, y: 1 });

		// FR-020: a single simultaneous left+right press (LEFT_RIGHT_PRESS),
		// NOT two clicks and NOT a double-click (research.md D7).
		expect(dispatched).toHaveLength(1);
		const part = dispatched[0].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(part.xPx).toBe(centerX(1));
		expect(part.yPx).toBe(centerY(1));
		expect(part.toolId).toBeTruthy();

		expect(result.additional_kwargs).toBeUndefined();
		expect(result.content[0].text).toBe(
			"saolei_chord_click dispatched at (1,1)",
		);
	});
});

// T028 (contracts/tool-dispatch-contract.md §6 / research.md D10): each saolei
// tool forwards the MCP `RequestHandlerExtra.signal` to `bridge.dispatch` so a
// cancelled MCP request aborts the in-flight desktop dispatch (resolves FAILED
// "aborted" rather than waiting the full 20-min backstop).
describe("createSaoleiMcpServer: signal forwarding (T028)", () => {
	it("forwards the MCP extra.signal to bridge.dispatch", async () => {
		const { bridge, signals } = makeFakeBridge();
		const server = createSaoleiMcpServer(bridge);

		const controller = new AbortController();
		await callTool(server, "saolei_init", {}, { signal: controller.signal });

		// The signal carried by the MCP extra reached dispatch verbatim.
		expect(signals).toHaveLength(1);
		expect(signals[0]).toBe(controller.signal);
	});

	it("aborted signal short-circuits dispatch (no FlowPart written to the sink)", async () => {
		// The bridge short-circuits an already-aborted signal BEFORE writing to
		// the sink (operation-bridge.ts dispatch). The tool still returns MCP
		// content blocks built from its result; the structured status remains
		// neutral (D12 — no additional_kwargs). The canned SUCCEEDED result is
		// never produced because dispatch returns FAILED "aborted" without
		// touching the sink.
		const { bridge, dispatched, signals } = makeFakeBridge({
			status: "TOOL_RESULT_STATUS_SUCCEEDED",
			message: "should-not-happen",
		});
		const server = createSaoleiMcpServer(bridge);

		const controller = new AbortController();
		controller.abort();
		const result = await callTool(server, "saolei_click", { x: 0, y: 0 }, {
			signal: controller.signal,
		});

		// The aborted signal reached dispatch verbatim.
		expect(signals).toHaveLength(1);
		expect(signals[0]).toBe(controller.signal);
		// Dispatch was short-circuited: no FlowPart was written to the sink.
		expect(dispatched).toHaveLength(0);
		// The tool still returns a well-formed MCP content block (its template
		// text); the structured status is neutral (D12).
		expect(result.additional_kwargs).toBeUndefined();
		expect(result.content[0].type).toBe("text");
	});
});
