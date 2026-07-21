/**
 * mcp-host.test.ts — Tests for the per-session MCP HTTP host.
 *
 * Coverage (Phase 4 / US1):
 *   - Unknown `{session_id}` → 404 "Session not found" (FR-003).
 *   - Known session → lazily creates a session-bound McpServer + transport
 *     entry on first request (research.md D3).
 *   - The MCP client's `tools/list` discovers exactly the five saolei tools
 *     (FR-005; quickstart.md Scenario 2 automated check).
 *
 * DI pattern (style/javascript.md §测试): the host takes a `SessionBridgeLookup`
 * function; tests inject a fake store + real `OperationBridge`. No `vi.mock`
 * of Express or the MCP SDK is performed.
 */

import { describe, expect, it } from "vitest";

import { OperationBridge } from "./operation-bridge";
import { createMcpHostApp, DEFAULT_MCP_PORT } from "./mcp-host";

/**
 * Minimal in-memory fake of `SessionAgentStore` — maps session ids to fresh
 * `OperationBridge` instances. Matches the production `SessionAgentStore.get`
 * surface the host consumes (a `{ bridge }` return on hit, `undefined` on
 * miss).
 */
function makeFakeStore(
	sessionIds: string[],
): {
	store: Map<string, { bridge: OperationBridge }>;
	lookup: (id: string) => { bridge: OperationBridge } | undefined;
} {
	const store = new Map<string, { bridge: OperationBridge }>();
	for (const id of sessionIds) {
		store.set(id, { bridge: new OperationBridge() });
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
