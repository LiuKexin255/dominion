/**
 * mcp-host.test.ts — Tests for the per-session MCP HTTP host (spec 039
 * T016 — template-scoped multi-path scheme, R3,
 * `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md`
 * §4).
 *
 * Coverage:
 *   - Routing: `/internal/mcp/:template/:session/saolei` (player mcp) and
 *     `/internal/mcp/:template/:session/memory` (planner mcp) each resolve
 *     to an INDEPENDENT lazily-created, session-bound McpServer; unknown
 *     session → 404 "Session not found" (FR-003); unknown kind segment →
 *     404.
 *   - Lazy creation + caching per (template, session, kind) triple;
 *     isolation across sessions, across kinds, and across templates.
 *   - (Spec 031 / US3) The optional team sink returned by the saolei
 *     lookup is passed through to `createSaoleiMcpServer`
 *     (`specs/031-team-template-mode/contracts/saolei-sink-contract.md` §6);
 *     no sink ⇒ `undefined` (FR-020). The sink's callback behaviour itself
 *     is exhaustively covered by `saolei-mcp.test.ts` — this file only
 *     asserts the pass-through.
 *   - The memory kind builds the planner's memory McpServer with the
 *     injected MemoryClient (path-closure, FR-012).
 *
 * DI pattern (style/javascript.md §测试): the host takes a `SessionBridgeLookup`
 * function; tests inject a fake store + real `OperationBridge` + fake
 * MemoryClient. No `vi.mock` of Express or the MCP SDK is performed. The
 * single exception is the sink pass-through cases below, which spy-wrap OUR
 * OWN in-repo module `./mcp/saolei/saolei-mcp` (justified inline at the
 * mock) — the real implementation still runs; only the call arguments are
 * recorded.
 */

import { describe, expect, it, vi } from "vitest";

import { OperationBridge } from "./operation-bridge.js";
import {
	createSaoleiMcpServer,
	type SaoleiEventSink,
} from "./mcp/saolei/saolei-mcp.js";
import { createMcpHostApp, DEFAULT_MCP_PORT, type McpKind } from "./mcp-host.js";
import type { MemoryClient } from "./memory-client.js";

/**
 * Spy-wrap of OUR OWN module `./mcp/saolei/saolei-mcp` — NOT Express or the
 * MCP SDK (the file-header convention above stands). Residual-mock case of
 * `style/javascript.md` §测试 (Mock 约定 — 脆弱模式): the host calls
 * `createSaoleiMcpServer(looked.bridge, undefined, looked.sink)` with the
 * default recognition engine and keeps the server in a closure, so there is
 * no DI seam through which a test could observe the sink pass-through, and
 * injecting a fake boardApi at the host level is out of scope. The target
 * is an in-repo TS source module (loaded through the Vite pipeline, NOT a
 * node_modules compiled dependency). The mock preserves the REAL
 * implementation (`vi.fn(actual.createSaoleiMcpServer)` — the real server
 * and its `connect` keep running, so existing cases are unchanged) and is
 * positively asserted by every case below (`toHaveBeenCalled`).
 */
vi.mock("./mcp/saolei/saolei-mcp.js", async (importOriginal) => {
	const actual = await importOriginal<typeof import("./mcp/saolei/saolei-mcp.js")>();
	return {
		...actual,
		createSaoleiMcpServer: vi.fn(actual.createSaoleiMcpServer),
	};
});

/** Minimal fake MemoryClient surface (DI seam — never exercised). */
function makeFakeMemoryClient(): MemoryClient {
	return {
		createMemory: vi.fn(),
		updateMemory: vi.fn(),
		deleteMemory: vi.fn(),
		listMemories: vi.fn(),
	} as unknown as MemoryClient;
}

/**
 * Minimal in-memory fake of the (template, session, kind) lookup surface.
 * Maps saolei sessions to fresh `OperationBridge` instances (plus an
 * optional team sink per session) and memory sessions to a fake
 * MemoryClient. Matches the production registry surface the host consumes
 * (a per-kind result on hit, `undefined` on miss).
 */
function makeFakeStore(
	saoleiSessions: string[],
	sinks?: Record<string, SaoleiEventSink>,
): {
	saolei: Map<string, { bridge: OperationBridge; sink?: SaoleiEventSink }>;
	memory: Map<string, MemoryClient>;
	lookup: (
		template: string,
		sessionId: string,
		kind: McpKind,
	) =>
		| { kind: "saolei"; bridge: OperationBridge; sink?: SaoleiEventSink }
		| { kind: "memory"; memoryClient: MemoryClient; template: string; session: string }
		| undefined;
} {
	const saolei = new Map<string, { bridge: OperationBridge; sink?: SaoleiEventSink }>();
	for (const id of saoleiSessions) {
		saolei.set(id, { bridge: new OperationBridge(), sink: sinks?.[id] });
	}
	const memory = new Map<string, MemoryClient>();
	const lookup = (template: string, sessionId: string, kind: McpKind) => {
		if (kind === "saolei") {
			const entry = saolei.get(sessionId);
			return entry ? { kind: "saolei" as const, ...entry } : undefined;
		}
		const client = memory.get(sessionId);
		return client
			? { kind: "memory" as const, memoryClient: client, template, session: sessionId }
			: undefined;
	};
	return { saolei, memory, lookup };
}

/**
 * Issue a JSON-RPC `initialize` request to a (template, session, kind)
 * path. Returns the `Mcp-Session-Id` header the server stamps on the
 * response (or undefined when the request was rejected). Used as the
 * prerequisite for follow-up requests on stateful Streamable HTTP servers.
 */
async function initialize(
	app: ReturnType<typeof createMcpHostApp>,
	template: string,
	sessionId: string,
	kind: McpKind,
	initBody: Record<string, unknown> = {
		jsonrpc: "2.0",
		id: "init",
		method: "initialize",
		params: {
			protocolVersion: "2025-03-26",
			capabilities: {},
			clientInfo: { name: "test-client", version: "0.0.1" },
		},
	},
): Promise<{
	status: number;
	body: unknown;
	mcpSessionId?: string;
}> {
	const { default: http } = await import("node:http");
	const server = http.createServer(app);
	await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	const addr = (server.address() as any);
	const url = `http://127.0.0.1:${addr.port}/internal/mcp/${template}/${sessionId}/${kind}`;

	const res = await fetch(url, {
		method: "POST",
		headers: {
			"content-type": "application/json",
			accept: "application/json, text/event-stream",
		},
		body: JSON.stringify(initBody),
	});
	const body = await res.text();
	server.close();
	let parsed: unknown = body;
	try {
		parsed = JSON.parse(body);
	} catch {
		// SSE / non-JSON response — keep raw text.
	}
	return {
		status: res.status,
		body: parsed,
		mcpSessionId: res.headers.get("mcp-session-id") ?? undefined,
	};
}

const TEMPLATE = "saolei";

describe("createMcpHostApp — routing (R3 template-scoped paths)", () => {
	it("returns 404 'Session not found' for an unknown session (FR-003)", async () => {
		const { lookup } = makeFakeStore(["sess-known"]);
		const app = createMcpHostApp(lookup);

		const res = await initialize(app, TEMPLATE, "sess-unknown", "saolei");

		expect(res.status).toBe(404);
		expect(res.body).toMatchObject({
			jsonrpc: "2.0",
			error: { code: -32001, message: "Session not found" },
		});
	});

	it("returns 404 for an unknown kind path segment (no server can exist)", async () => {
		const { lookup } = makeFakeStore(["sess-a"]);
		const app = createMcpHostApp(lookup);

		// The kind segment is not a routable mcp kind → 404, and NO entry is
		// created for the session.
		const res = await initialize(app, TEMPLATE, "sess-a", "unknown-kind" as McpKind);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const sessions = (app as any)._mcpSessions as Map<string, unknown>;

		expect(res.status).toBe(404);
		expect(sessions.size).toBe(0);
	});

	it("routes the saolei path to a lazily-created session-bound server (first request)", async () => {
		const { saolei, lookup } = makeFakeStore(["sess-a"]);
		const app = createMcpHostApp(lookup);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const sessions = (app as any)._mcpSessions as Map<string, unknown>;
		expect(sessions.size).toBe(0);

		const res = await initialize(app, TEMPLATE, "sess-a", "saolei");

		expect(res.status).toBe(200);
		expect(res.mcpSessionId).toBeTruthy();
		expect(sessions.size).toBe(1);
		expect(sessions.has(`${TEMPLATE}/sess-a/saolei`)).toBe(true);
		expect(saolei.get("sess-a")).toBeDefined();
	});

	it("routes the memory path to an independent memory McpServer with the injected MemoryClient", async () => {
		const { memory, lookup } = makeFakeStore([]);
		const fakeClient = makeFakeMemoryClient();
		memory.set("sess-mem", fakeClient);
		const app = createMcpHostApp(lookup);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const sessions = (app as any)._mcpSessions as Map<string, unknown>;

		const res = await initialize(app, TEMPLATE, "sess-mem", "memory");

		expect(res.status).toBe(200);
		expect(res.mcpSessionId).toBeTruthy();
		// The memory path created its own session-bound entry …
		const entryKey = `${TEMPLATE}/sess-mem/memory`;
		expect(sessions.has(entryKey)).toBe(true);
		// … whose server exposes EXACTLY the planner's single `memory` tool
		// (FR-007/FR-008 — the planner toolset, not the saolei toolset).
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const entry = (sessions.get(entryKey) as any);
		expect(Object.keys(entry.mcp._registeredTools)).toEqual(["memory"]);
	});

	it("reuses the same entry on subsequent requests to the same triple", async () => {
		const { lookup } = makeFakeStore(["sess-b"]);
		const app = createMcpHostApp(lookup);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const sessions = (app as any)._mcpSessions as Map<string, unknown>;

		await initialize(app, TEMPLATE, "sess-b", "saolei");
		await initialize(app, TEMPLATE, "sess-b", "saolei");

		expect(sessions.size).toBe(1);
	});

	it("isolates entries across distinct sessions (FR-026)", async () => {
		const { lookup } = makeFakeStore(["sess-1", "sess-2"]);
		const app = createMcpHostApp(lookup);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const sessions = (app as any)._mcpSessions as Map<string, unknown>;

		await initialize(app, TEMPLATE, "sess-1", "saolei");
		await initialize(app, TEMPLATE, "sess-2", "saolei");

		expect(sessions.size).toBe(2);
	});

	it("isolates entries across KINDS for the same session (saolei ≠ memory, R3)", async () => {
		const { memory, lookup } = makeFakeStore(["sess-x"]);
		memory.set("sess-x", makeFakeMemoryClient());
		const app = createMcpHostApp(lookup);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const sessions = (app as any)._mcpSessions as Map<string, unknown>;

		await initialize(app, TEMPLATE, "sess-x", "saolei");
		await initialize(app, TEMPLATE, "sess-x", "memory");

		// Two INDEPENDENT McpServer instances, one per kind (contract §4 —
		// "两者独立 McpServer 实例（player 工具集 = saolei_operate 等；
		// planner 工具集 = memory…）").
		expect(sessions.size).toBe(2);
		expect(sessions.has(`${TEMPLATE}/sess-x/saolei`)).toBe(true);
		expect(sessions.has(`${TEMPLATE}/sess-x/memory`)).toBe(true);
	});

	it("isolates entries across TEMPLATES for the same session (template-scoped)", async () => {
		const { lookup } = makeFakeStore(["sess-y"]);
		const app = createMcpHostApp(lookup);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const sessions = (app as any)._mcpSessions as Map<string, unknown>;

		await initialize(app, "template-a", "sess-y", "saolei");
		await initialize(app, "template-b", "sess-y", "saolei");

		expect(sessions.size).toBe(2);
	});

	it("exposes DEFAULT_MCP_PORT as a stable default (FR-001)", () => {
		// FR-001: a localhost port distinct from gRPC 50051. The exact value
		// is configurable via MCP_PORT; DEFAULT_MCP_PORT pins the default.
		expect(DEFAULT_MCP_PORT).not.toBe(50051);
		expect(DEFAULT_MCP_PORT).toBeGreaterThan(0);
	});
});

// ── team sink pass-through (spec 031 / US3) ─────────────────────────────────
//
// The saolei lookup carries an optional `sink?: SaoleiEventSink`
// (`specs/031-team-template-mode/contracts/saolei-sink-contract.md` §6). The
// host's only responsibility is passing it into `createSaoleiMcpServer` as
// the third argument — the sink's callback semantics are covered by
// `saolei-mcp.test.ts`, so this file asserts the pass-through via the
// `createSaoleiMcpServer` spy-wrap above (positive assertion of the mock, per
// `style/javascript.md` §测试 — 规则：验证 mock 确实生效).

describe("createMcpHostApp: team sink pass-through (031 saolei-sink-contract §6)", () => {
	it("passes the lookup's sink to createSaoleiMcpServer as the third argument", async () => {
		vi.mocked(createSaoleiMcpServer).mockClear();
		const teamSink: SaoleiEventSink = {
			onGameStart: () => undefined,
			onOperate: () => undefined,
			onGameEnd: () => undefined,
		};
		const { lookup } = makeFakeStore(["sess-sink"], {
			"sess-sink": teamSink,
		});
		const app = createMcpHostApp(lookup);

		// The initialize handshake lazily creates the session-bound server
		// (research.md D3), which is where the lookup result is consumed.
		const res = await initialize(app, TEMPLATE, "sess-sink", "saolei");

		expect(res.status).toBe(200);
		expect(res.mcpSessionId).toBeTruthy();
		// Positive assertion that the mock was exercised (style/javascript.md
		// §测试 — 规则：验证 mock 确实生效).
		expect(vi.mocked(createSaoleiMcpServer)).toHaveBeenCalledOnce();
		const [bridge, boardApi, sink] = vi.mocked(createSaoleiMcpServer).mock.calls[0];
		expect(bridge).toBeInstanceOf(OperationBridge);
		// mcp-host passes no custom boardApi (default recognition engine).
		expect(boardApi).toBeUndefined();
		// The exact sink object returned by the lookup is passed through.
		expect(sink).toBe(teamSink);
	});

	it("passes undefined when the lookup carries no sink (FR-020 back-compat)", async () => {
		vi.mocked(createSaoleiMcpServer).mockClear();
		const { lookup } = makeFakeStore(["sess-plain"]);
		const app = createMcpHostApp(lookup);

		const res = await initialize(app, TEMPLATE, "sess-plain", "saolei");

		expect(res.status).toBe(200);
		expect(vi.mocked(createSaoleiMcpServer)).toHaveBeenCalledOnce();
		const [, boardApi, sink] = vi.mocked(createSaoleiMcpServer).mock.calls[0];
		expect(boardApi).toBeUndefined();
		// No sink ⇒ the default `undefined` third argument (zero behaviour
		// change, FR-020).
		expect(sink).toBeUndefined();
	});
});
