/**
 * mcp-host.ts — Per-session MCP HTTP host for the game agent.
 *
 * Hosts a localhost Streamable HTTP MCP server that routes
 * `/internal/mcp/{session_id}` to a lazily-created, session-bound
 * `McpServer` (`specs/018-saolei-mcp/research.md` D3). Each session's
 * `McpServer` is built by `createSaoleiMcpServer` and closes over that
 * session's `OperationBridge` (FR-002 / FR-026). The saolei MCP is stateless
 * (`specs/023-saolei-mcp-refine/contracts/tool-dispatch-contract.md` §6), so
 * the server carries no per-session game state.
 *
 * Lifecycle (research.md D3 / FR-026): the host process owns the listener;
 * sessions map onto the existing `SessionAgent` lifecycle. Unknown
 * `{session_id}` → 404 "Session not found" (FR-003), no server created.
 *
 * DI seam (style/javascript.md §测试): the host takes a `SessionLookup`
 * function so tests can inject a fake store + fake bridge without spinning
 * up a real Express listener.
 */

import express from "express";
import { Server } from "node:http";
import { randomUUID } from "node:crypto";
import { info, warn } from "@dominion/common-js-logs";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import type { OperationBridge } from "./operation-bridge";
import { createSaoleiMcpServer } from "./mcp/saolei/saolei-mcp";

/**
 * Lookup result for a session id: the session's `OperationBridge`, used to
 * build the session-bound saolei `McpServer`. Returning `undefined` triggers
 * the FR-003 404 path.
 */
export interface SessionBridgeLookup {
	(sessionId: string):
		| { bridge: OperationBridge }
		| undefined;
}

/** Default localhost port for the MCP HTTP listener (FR-001). */
export const DEFAULT_MCP_PORT = 50052;

/** Session-keyed entry: the lazily-created server + transport pair. */
interface SessionEntry {
	mcp: McpServer;
	transport: StreamableHTTPServerTransport;
}

/**
 * Build the Express application that hosts the per-session MCP endpoints.
 *
 * The application is NOT started here; the caller binds it to a port via
 * `startMcpHost` (production) or `supertest`/`app.listen(0)` (tests).
 *
 * @param lookup Resolves a session id to its `OperationBridge` (DI seam).
 */
export function createMcpHostApp(lookup: SessionBridgeLookup): express.Express {
	const app = express();
	app.use(express.json());

	// Per-session McpServer + transport map. Keyed by the path session id
	// (NOT the Mcp-Session-Id header; research.md D3 — "we key by the path
	// {session_id} instead, which is the dominion-session identifier").
	const sessions = new Map<string, SessionEntry>();

	/**
	 * Lazily create (or fetch) the session-bound MCP server + transport.
	 * Returns `undefined` when the session is unknown (FR-003).
	 *
	 * Async because `McpServer.connect(transport)` is async and the first
	 * request must arrive at a transport whose server is already attached —
	 * otherwise `transport.handleRequest` may run before the request
	 * handlers are registered, causing a silent failure on the first
	 * session request. Awaiting connect here guarantees readiness before
	 * the entry is cached and handed to the handler.
	 */
	async function getOrCreateSession(
		sessionId: string,
	): Promise<SessionEntry | undefined> {
		const existing = sessions.get(sessionId);
		if (existing) return existing;

		const looked = lookup(sessionId);
		if (!looked) return undefined;

		const mcp = createSaoleiMcpServer(looked.bridge);
		const transport = new StreamableHTTPServerTransport({
			sessionIdGenerator: () => randomUUID(),
			onsessioninitialized: (mcpSessionId) => {
				info("mcp session initialized", {
					dominionSessionId: sessionId,
					mcpSessionId,
				});
			},
		});
		// Await connect so the first request arrives at a ready transport:
		// connect() attaches the server's request handlers to the transport;
		// racing a request in before it resolves can lose the initialise
		// handshake. The Map.set below caches the entry only after connect
		// resolves, so concurrent requests for the same session id re-enter
		// this function and either find a cached entry or rebuild (idempotent
		// for a fresh transport — McpServer.connect is one-shot per server).
		await mcp.connect(transport);

		const entry: SessionEntry = { mcp, transport };
		sessions.set(sessionId, entry);
		info("mcp session created", { dominionSessionId: sessionId });
		return entry;
	}

	// ── Routing: /internal/mcp/:sessionId for POST/GET/DELETE ───────────
	//
	// The same Express route handles the three MCP HTTP methods. Each request
	// resolves the session lazily; unknown session → 404 (FR-003).

	const handler = async (
		req: express.Request,
		res: express.Response,
	): Promise<void> => {
		const sessionId = String(req.params.sessionId);
		const entry = await getOrCreateSession(sessionId);
		if (!entry) {
			warn("mcp session not found", { dominionSessionId: sessionId });
			res.status(404).json({
				jsonrpc: "2.0",
				error: {
					code: -32001,
					message: "Session not found",
				},
			});
			return;
		}
		try {
			// `handleRequest` accepts an optional pre-parsed body; the
			// Express json() middleware leaves it on req.body.
			await entry.transport.handleRequest(req, res, req.body);
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			warn("mcp request handler error", {
				dominionSessionId: sessionId,
				error: msg,
			});
			if (!res.headersSent) {
				res.status(500).json({
					jsonrpc: "2.0",
					error: { code: -32603, message: msg },
				});
			}
		}
	};

	app.post("/internal/mcp/:sessionId", handler);
	app.get("/internal/mcp/:sessionId", handler);
	app.delete("/internal/mcp/:sessionId", handler);

	// Expose the session map for tests so they can assert lazily-created
	// servers without an HTTP round-trip.
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	(app as any)._mcpSessions = sessions;

	return app;
}

/**
 * Start a localhost MCP HTTP listener alongside the gRPC server (FR-001).
 *
 * @param lookup Resolves a session id to its `OperationBridge`.
 * @param port   TCP port (env `MCP_PORT`, default `DEFAULT_MCP_PORT`).
 * @returns The started `Server`; call `.close()` on shutdown.
 */
export function startMcpHost(
	lookup: SessionBridgeLookup,
	port: number = process.env.MCP_PORT
		? Number(process.env.MCP_PORT)
		: DEFAULT_MCP_PORT,
): Server {
	const app = createMcpHostApp(lookup);
	const server = app.listen(port, "127.0.0.1", () => {
		info("mcp host listening", { port, host: "127.0.0.1" });
	});
	return server;
}
