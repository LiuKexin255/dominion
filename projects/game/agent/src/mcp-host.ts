/**
 * mcp-host.ts — Per-session MCP HTTP host for the game agent.
 *
 * Hosts a localhost Streamable HTTP MCP server that routes
 * `/internal/mcp/{template}/{session}/{kind}` to a lazily-created,
 * session-bound, template-scoped `McpServer` (`specs/018-saolei-mcp/
 * research.md` D3; template-scoped multi-path scheme — R3,
 * `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md`
 * §4). Each mcp kind gets its OWN path (NOT one shared path):
 *
 *   - `/internal/mcp/{template}/{session}/saolei` — the player's saolei
 *     mcp (built by `createSaoleiMcpServer`, closing over the session's
 *     `OperationBridge` — FR-002/FR-026 — plus the optional team sink,
 *     `specs/031-team-template-mode/contracts/saolei-sink-contract.md` §6).
 *   - `/internal/mcp/{template}/{session}/memory` — the planner's memory
 *     mcp (built by `createMemoryMcpServer`, closing over the injected
 *     `MemoryClient`; the mcp server forwards via the agent and never
 *     connects to the memory service directly — FR-007).
 *
 * The two kinds are INDEPENDENT `McpServer` instances (player toolset =
 * saolei tools; planner toolset = `memory`) with independent
 * template-scoped paths (R3). The path carries the template (team-era path
 * style); the kind segment is the literal `saolei`/`memory` path-segment
 * name (contract §4 — "精确 path 段命名…由 plan 落实").
 *
 * Lifecycle (research.md D3 / FR-026): the host process owns the listener;
 * sessions map onto the existing `SessionAgent` lifecycle. Unknown
 * `{template}/{session}` or unknown `{kind}` → 404 "Session not found"
 * (FR-003), no server created.
 *
 * DI seam (style/javascript.md §测试): the host takes a `SessionBridgeLookup`
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
import type { SaoleiEventSink } from "./mcp/saolei/saolei-mcp";
import { createSaoleiMcpServer } from "./mcp/saolei/saolei-mcp";
import type { MemoryClient } from "./memory-client";
import { createMemoryMcpServer } from "./mcp/memory/memory-mcp";

/**
 * The mcp kinds hosted per (template, session). Each kind owns an
 * independent template-scoped path (R3, contract §4).
 */
export type McpKind = "saolei" | "memory";

/**
 * Lookup result for a (template, session, kind) triple: the per-kind
 * dependencies the host needs to build that session's `McpServer`.
 *
 * - `saolei`: the session's `OperationBridge`, used to build the
 *   session-bound saolei mcp, plus an optional out-of-band event sink
 *   supplied by the session's team (the team binds it to its ephemeral
 *   buffer).
 * - `memory`: the injected `MemoryClient` plus the template/session path
 *   segments (closed over by `createMemoryMcpServer`, FR-012).
 *
 * Returning `undefined` triggers the FR-003 404 path.
 */
export type SessionLookupResult =
	| {
			kind: "saolei";
			bridge: OperationBridge;
			sink?: SaoleiEventSink;
	  }
	| {
			kind: "memory";
			memoryClient: MemoryClient;
			template: string;
			session: string;
	  };

/**
 * Resolves a (template, session, kind) triple to its per-kind dependencies.
 */
export interface SessionBridgeLookup {
	(template: string, sessionId: string, kind: McpKind):
		| SessionLookupResult
		| undefined;
}

/** Default localhost port for the MCP HTTP listener (FR-001). */
export const DEFAULT_MCP_PORT = 50052;

/** Session+kind-keyed entry: the lazily-created server + transport pair. */
interface SessionEntry {
	mcp: McpServer;
	transport: StreamableHTTPServerTransport;
}

/** Cache key for one (template, session, kind) mcp instance. */
function mcpKey(template: string, sessionId: string, kind: McpKind): string {
	return `${template}/${sessionId}/${kind}`;
}

/**
 * Build the Express application that hosts the per-session MCP endpoints.
 *
 * The application is NOT started here; the caller binds it to a port via
 * `startMcpHost` (production) or `supertest`/`app.listen(0)` (tests).
 *
 * @param lookup Resolves a (template, session, kind) triple to its
 *   per-kind dependencies (DI seam).
 */
export function createMcpHostApp(lookup: SessionBridgeLookup): express.Express {
	const app = express();
	app.use(express.json());

	// Per-(template, session, kind) McpServer + transport map. Keyed by the
	// path triple (NOT the Mcp-Session-Id header; research.md D3 — "we key
	// by the path {session_id} instead, which is the dominion-session
	// identifier").
	const sessions = new Map<string, SessionEntry>();

	/**
	 * Lazily create (or fetch) the session-bound MCP server + transport for
	 * one (template, session, kind). Returns `undefined` when the session
	 * (or the kind) is unknown (FR-003).
	 *
	 * Async because `McpServer.connect(transport)` is async and the first
	 * request must arrive at a transport whose server is already attached —
	 * otherwise `transport.handleRequest` may run before the request
	 * handlers are registered, causing a silent failure on the first
	 * session request. Awaiting connect here guarantees readiness before
	 * the entry is cached and handed to the handler.
	 */
	async function getOrCreateSession(
		template: string,
		sessionId: string,
		kind: McpKind,
	): Promise<SessionEntry | undefined> {
		const key = mcpKey(template, sessionId, kind);
		const existing = sessions.get(key);
		if (existing) return existing;

		const looked = lookup(template, sessionId, kind);
		if (!looked) return undefined;

		// Per-kind factory dispatch (contract §4): each kind gets its own
		// McpServer instance; the two toolsets never cross (FR-009/FR-010).
		const mcp =
			looked.kind === "saolei"
				? createSaoleiMcpServer(looked.bridge, undefined, looked.sink)
				: createMemoryMcpServer(
						looked.memoryClient,
						looked.template,
						looked.session,
					);
		const transport = new StreamableHTTPServerTransport({
			sessionIdGenerator: () => randomUUID(),
			onsessioninitialized: (mcpSessionId) => {
				info("mcp session initialized", {
					template,
					dominionSessionId: sessionId,
					kind,
					mcpSessionId,
				});
			},
		});
		// Await connect so the first request arrives at a ready transport:
		// connect() attaches the server's request handlers to the transport;
		// racing a request in before it resolves can lose the initialise
		// handshake. The Map.set below caches the entry only after connect
		// resolves, so concurrent requests for the same triple re-enter
		// this function and either find a cached entry or rebuild (idempotent
		// for a fresh transport — McpServer.connect is one-shot per server).
		await mcp.connect(transport);

		const entry: SessionEntry = { mcp, transport };
		sessions.set(key, entry);
		info("mcp session created", {
			template,
			dominionSessionId: sessionId,
			kind,
		});
		return entry;
	}

	// ── Routing: /internal/mcp/:template/:session/:kind ──────────────────
	//
	// The same Express route handles the three MCP HTTP methods. Each request
	// resolves the session lazily; unknown session/kind → 404 (FR-003).

	const handler = async (
		req: express.Request,
		res: express.Response,
	): Promise<void> => {
		const template = String(req.params.template);
		const sessionId = String(req.params.session);
		const kind = String(req.params.kind);
		if (kind !== "saolei" && kind !== "memory") {
			// Unknown mcp kind segment — no server can exist for it (R3:
			// one path per kind; unknown paths are not routable).
			warn("mcp kind not found", { template, dominionSessionId: sessionId, kind });
			res.status(404).json({
				jsonrpc: "2.0",
				error: {
					code: -32001,
					message: "Session not found",
				},
			});
			return;
		}
		const entry = await getOrCreateSession(template, sessionId, kind);
		if (!entry) {
			warn("mcp session not found", { template, dominionSessionId: sessionId, kind });
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
				template,
				dominionSessionId: sessionId,
				kind,
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

	app.post("/internal/mcp/:template/:session/:kind", handler);
	app.get("/internal/mcp/:template/:session/:kind", handler);
	app.delete("/internal/mcp/:template/:session/:kind", handler);

	// Expose the session map for tests so they can assert lazily-created
	// servers without an HTTP round-trip.
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	(app as any)._mcpSessions = sessions;

	return app;
}

/**
 * Start a localhost MCP HTTP listener alongside the gRPC server (FR-001).
 *
 * @param lookup Resolves a (template, session, kind) triple to its
 *   per-kind dependencies.
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
