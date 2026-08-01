/**
 * mcp-host.test.ts — Tests for the per-session MCP HTTP host.
 *
 * Coverage (Phase 4 / US1):
 *   - Unknown `{session_id}` → 404 "Session not found" (FR-003).
 *   - Known session → lazily creates a session-bound McpServer + transport
 *     entry on first request (research.md D3).
 *   - (Spec 031 / US3) The optional team sink returned by the lookup is
 *     passed through to `createSaoleiMcpServer` (`specs/031-team-template-mode/
 *     contracts/saolei-sink-contract.md` §6); no sink ⇒ `undefined` (FR-020).
 *     The sink's callback behaviour itself is exhaustively covered by
 *     `saolei-mcp.test.ts` — this file only asserts the pass-through.
 *
 * DI pattern (style/javascript.md §测试): the host takes a `SessionBridgeLookup`
 * function; tests inject a fake store + real `OperationBridge`. No `vi.mock`
 * of Express or the MCP SDK is performed. The single exception is the sink
 * pass-through cases below, which spy-wrap OUR OWN in-repo module
 * `./mcp/saolei/saolei-mcp` (justified inline at the mock) — the real
 * implementation still runs; only the call arguments are recorded.
 */

import { describe, expect, it, vi } from "vitest";

import { OperationBridge } from "./operation-bridge";
import {
	createSaoleiMcpServer,
	type SaoleiEventSink,
} from "./mcp/saolei/saolei-mcp";
import { createMcpHostApp, DEFAULT_MCP_PORT } from "./mcp-host";

/**
 * Spy-wrap of OUR OWN module `./mcp/saolei/saolei-mcp` — NOT Express or the
 * MCP SDK (the file-header convention above stands). Residual-mock case of
 * `style/javascript.md` §测试 (Mock 约定 — 脆弱模式): the host calls
 * `createSaoleiMcpServer(looked.bridge, undefined, looked.sink)` with the
 * default recognition engine and keeps the server in a closure, so there is
 * no DI seam through which a test could observe the sink pass-through, and
 * injecting a fake boardApi at the host level is out of T009's scope. The
 * target is an in-repo TS source module (loaded through the Vite pipeline,
 * NOT a node_modules compiled dependency). The mock preserves the REAL
 * implementation (`vi.fn(actual.createSaoleiMcpServer)` — the real server
 * and its `connect` keep running, so existing cases are unchanged) and is
 * positively asserted by every case below (`toHaveBeenCalled`).
 */
vi.mock("./mcp/saolei/saolei-mcp", async (importOriginal) => {
	const actual = await importOriginal<typeof import("./mcp/saolei/saolei-mcp")>();
	return {
		...actual,
		createSaoleiMcpServer: vi.fn(actual.createSaoleiMcpServer),
	};
});

/**
 * Minimal in-memory fake of the session lookup surface — maps session ids to
 * fresh `OperationBridge` instances, plus an optional team sink per session
 * (spec 031 — the lookup result shape is `{ bridge, sink? }`). Matches the
 * production `SessionTeamStore.get` surface the host consumes (a `{ bridge,
 * sink? }` return on hit, `undefined` on miss).
 */
function makeFakeStore(
	sessionIds: string[],
	sinks?: Record<string, SaoleiEventSink>,
): {
	store: Map<string, { bridge: OperationBridge; sink?: SaoleiEventSink }>;
	lookup: (
		id: string,
	) => { bridge: OperationBridge; sink?: SaoleiEventSink } | undefined;
} {
	const store = new Map<string, { bridge: OperationBridge; sink?: SaoleiEventSink }>();
	for (const id of sessionIds) {
		store.set(id, { bridge: new OperationBridge(), sink: sinks?.[id] });
	}
	const lookup = (id: string) => store.get(id);
	return { store, lookup };
}

/**
 * Issue a JSON-RPC `initialize` request to a session path. Returns the
 * `Mcp-Session-Id` header the server stamps on the response (or undefined
 * when the request was rejected). Used as the prerequisite for follow-up
 * requests on stateful Streamable HTTP servers.
 */
async function initialize(
	app: ReturnType<typeof createMcpHostApp>,
	sessionId: string,
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
	// Wrap Express's Request/Response in a minimal supertest-like shim
	// using the app.handle API directly.
	const { default: http } = await import("node:http");
	const server = http.createServer(app);
	await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	const addr = (server.address() as any);
	const url = `http://127.0.0.1:${addr.port}/internal/mcp/${sessionId}`;

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

describe("createMcpHostApp", () => {
	it("returns 404 'Session not found' for unknown session_id (FR-003)", async () => {
		const { lookup } = makeFakeStore(["sess-known"]);
		const app = createMcpHostApp(lookup);

		const res = await initialize(app, "sess-unknown");

		expect(res.status).toBe(404);
		expect(res.body).toMatchObject({
			jsonrpc: "2.0",
			error: { code: -32001, message: "Session not found" },
		});
	});

	it("creates a session-bound MCP server on first request for a known session", async () => {
		const { store, lookup } = makeFakeStore(["sess-a"]);
		const app = createMcpHostApp(lookup);

		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const sessions = (app as any)._mcpSessions as Map<string, unknown>;
		expect(sessions.size).toBe(0);

		const res = await initialize(app, "sess-a");

		// Known session: initialize must succeed and stamp a Mcp-Session-Id.
		expect(res.status).toBe(200);
		expect(res.mcpSessionId).toBeTruthy();
		// The entry was created lazily.
		expect(sessions.size).toBe(1);
		expect(store.get("sess-a")).toBeDefined();
	});

	it("reuses the same entry on subsequent requests to the same session_id", async () => {
		const { lookup } = makeFakeStore(["sess-b"]);
		const app = createMcpHostApp(lookup);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const sessions = (app as any)._mcpSessions as Map<string, unknown>;

		await initialize(app, "sess-b");
		await initialize(app, "sess-b");

		// Same session id → exactly one lazily-created entry.
		expect(sessions.size).toBe(1);
	});

	it("isolates entries across distinct session_ids (FR-026)", async () => {
		const { lookup } = makeFakeStore(["sess-1", "sess-2"]);
		const app = createMcpHostApp(lookup);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const sessions = (app as any)._mcpSessions as Map<string, unknown>;

		await initialize(app, "sess-1");
		await initialize(app, "sess-2");

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
// `SessionBridgeLookup` now carries an optional `sink?: SaoleiEventSink`
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
			onMove: () => undefined,
			onGameEnd: () => undefined,
		};
		const { lookup } = makeFakeStore(["sess-sink"], {
			"sess-sink": teamSink,
		});
		const app = createMcpHostApp(lookup);

		// The initialize handshake lazily creates the session-bound server
		// (research.md D3), which is where the lookup result is consumed.
		const res = await initialize(app, "sess-sink");

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

		const res = await initialize(app, "sess-plain");

		expect(res.status).toBe(200);
		expect(vi.mocked(createSaoleiMcpServer)).toHaveBeenCalledOnce();
		const [, boardApi, sink] = vi.mocked(createSaoleiMcpServer).mock.calls[0];
		expect(boardApi).toBeUndefined();
		// No sink ⇒ the default `undefined` third argument (zero behaviour
		// change, FR-020).
		expect(sink).toBeUndefined();
	});
});
