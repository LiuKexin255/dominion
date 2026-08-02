/**
 * session-team.ts — Per-session team graph lifecycle management.
 *
 * `SessionTeam` (replaces `SessionAgent`, specs/031-team-template-mode/
 * research.md D10) owns exactly one compiled saolei team graph instance
 * (`buildTeamGraph` result), the per-session ephemeral game buffer
 * (`team-sink.ts`), the session-scoped `OperationBridge` (player-exclusive,
 * FR-010), and the per-session `TurnLoop` (spec 030 single-flight + queue
 * semantics — one user input = one team turn = one graph invoke; `gameEnded`
 * is handled inside the turn by the conditional edge, so a turn never
 * requires external continuation and single-flight is preserved).
 *
 * The `OperationBridge` and team `SaoleiEventSink` are NOT constructed here:
 * the store factory (server.ts) pre-builds them against the session's
 * ephemeral buffer and injects them via the constructor (see the constructor
 * doc — this breaks the CreateTeam→MCP-host circular dependency).
 *
 * - **Turn runner**: {@link SessionTeam.runTeamTurn} drives the compiled
 *   graph's `streamEvents` on the session's thread (thread_id = session id,
 *   FR-013) and converts each node's channel update into a stream of
 *   `TurnBlock`s tagged with the producing agent (`player`/`planner`, D12).
 * - **RefreshTeam** (FR-018): {@link SessionTeam.refreshTeam} clears BOTH
 *   short-term message channels via `graph.updateState` (the
 *   `context-middleware` helpers); the strategy (StrategyStore/mongo) is
 *   untouched.
 * - **MCP host surface**: {@link SessionTeam.getBridge} / {@link SessionTeam.getSink}
 *   feed the `SessionBridgeLookup` (mcp-host.ts) so the saolei MCP server is
 *   built per session with the team sink bound to this session's buffer. The
 *   instances are the SAME ones injected by the store factory (server.ts
 *   pre-registers them before `buildSaoleiMcpTools` connects — see
 *   `server.ts` for the circular-dependency rationale).
 *
 * `SessionTeamStore` (replaces `SessionAgentStore`) maps session id →
 * `SessionTeam`, creating entries ONLY through the explicit
 * {@link SessionTeamStore.create} (AIP-133 CreateTeam; no lazy creation —
 * the profile is supplied per request, server.ts wiring; tests inject a fake
 * factory). Re-entry is profile-conditional: same profile → idempotent;
 * different profile → {@link TeamAlreadyExistsError} (user refinement,
 * api-contract.md §2.2).
 */

import { HumanMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";

import type { OperationBridge } from "./operation-bridge";
import { TurnLoop } from "./turn-loop";
import type { TurnLoopEmit } from "./turn-loop";
import type { TurnBlock } from "./turn-loop";
import type { ContentBlock, TurnContent } from "./llm";
import { buildContentBlocks, extractToolCalls, readToolResultStatus, RECURSION_LIMIT } from "./llm";
import { parseToolResultFields } from "./tools/shared/result-blocks";
import { refreshTeamChannels } from "./context-middleware";
import type { TeamGraphHandle } from "./team/graph";
import type { TeamStateValue } from "./team/state";
import type { EphemeralGameBuffer } from "./team/team-sink";
import type { SaoleiEventSink } from "./mcp/saolei/saolei-mcp";

/** The session's primary agent — stamped on control frames (D12). */
export const PRIMARY_AGENT_NAME = "player";

/**
 * Factory that builds a fully-wired `SessionTeam` for a session id
 * (DI seam — `style/javascript.md` §测试: the store never constructs team
 * internals itself, so tests inject fakes without `vi.mock`). The template
 * and TeamProfile name come from the CreateTeam request (AIP-133): the
 * production factory resolves that profile's player/planner models
 * (prompt-client.getTeamProfile) — there is no fixed default profile anymore
 * (Agent 移除懒加载模式, spec 031-team-template-mode design decision).
 */
export type SessionTeamFactory = (
	sessionId: string,
	template: string,
	profileName: string,
) => Promise<SessionTeam>;

// ---------------------------------------------------------------------------
// SessionTeam
// ---------------------------------------------------------------------------

export class SessionTeam {
	private readonly graphHandle: TeamGraphHandle;
	private readonly buffer: EphemeralGameBuffer;
	private readonly bridge: OperationBridge;
	private readonly sink: SaoleiEventSink;
	private readonly sessionId: string;
	/** The session's template path segment (e.g. "saolei") — the templateId
	 * stamped on every outbound AgentFrame (REQUIRED, api-contract.md §3.6). */
	private readonly template: string;

	private turnLoop: TurnLoop | null = null;
	private turnLoopEmit: TurnLoopEmit | null = null;
	/** Per-agent message counts already streamed — for node-output dedup. */
	private readonly emittedCounts = new Map<string, number>();

	/**
	 * @param graphHandle The compiled team graph + outer MemorySaver (built
	 *   by the store factory — server.ts T021 wires models/strategy/MCP tools).
	 * @param buffer      The per-session ephemeral game buffer (D7). Owned by
	 *   this session; the sink writes it, the graph nodes read it.
	 * @param sessionId   The dominion session id (thread id + strategy key).
	 * @param template    The session's template path segment (from the
	 *   CreateTeam parent, AIP-133).
	 * @param bridge      The session's `OperationBridge` (player-exclusive,
	 *   FR-010), pre-built by the store factory (server.ts). NOT constructed
	 *   here: the factory registers it with the MCP-host bridge registry
	 *   BEFORE `buildSaoleiMcpTools` connects (so the host's
	 *   `SessionBridgeLookup` hits during team creation) and then injects the
	 *   SAME instance here — the graph player's operation bridge and the
	 *   mcp-host-served one must be identical (specs/031-team-template-mode/
	 *   contracts/saolei-sink-contract.md §6).
	 * @param sink        The team `SaoleiEventSink` bound to `buffer`
	 *   (`createTeamSink`), pre-built by the store factory for the same
	 *   reason — one sink per session, shared by the McpServer and the team.
	 */
	constructor(
		graphHandle: TeamGraphHandle,
		buffer: EphemeralGameBuffer,
		sessionId: string,
		template: string,
		bridge: OperationBridge,
		sink: SaoleiEventSink,
	) {
		this.graphHandle = graphHandle;
		this.buffer = buffer;
		this.sessionId = sessionId;
		this.template = template;
		this.bridge = bridge;
		this.sink = sink;
	}

	/** The session's `OperationBridge` (player-exclusive, FR-010). */
	getBridge(): OperationBridge {
		return this.bridge;
	}

	/**
	 * The team `SaoleiEventSink` bound to this session's ephemeral buffer —
	 * injected into the session-bound saolei McpServer via `mcp-host.ts`
	 * (specs/031-team-template-mode/contracts/saolei-sink-contract.md §6).
	 */
	getSink(): SaoleiEventSink {
		return this.sink;
	}

	/**
	 * Read the session thread's latest team state (per-agent channel
	 * reconstruction from the single outer MemorySaver — spike A3).
	 * Returns `null` when no checkpoint exists yet.
	 */
	async getTeamState(): Promise<TeamStateValue | null> {
		const snapshot = await this.graphHandle.graph.getState({
			configurable: { thread_id: this.sessionId },
		});
		return snapshot ? snapshot.values : null;
	}

	/**
	 * RefreshTeam (FR-018): clear BOTH short-term message channels
	 * (`playerMessages`/`plannerMessages`) via the outer graph's
	 * `updateState` (context-middleware). The strategy and `gameEnded` are
	 * untouched. Caller MUST reject while a turn is in flight
	 * ({@link isRunning}).
	 */
	async refreshTeam(): Promise<void> {
		await refreshTeamChannels(this.graphHandle.graph, this.sessionId);
	}

	/**
	 * Route a user-content submission to the per-session {@link TurnLoop}
	 * (single-flight owner; one user input = one team turn). Installs the
	 * per-stream `emit` sink, lazily constructing the loop on first use with
	 * the team-graph turn runner. Non-blocking: returns once the content is
	 * started (IDLE) or buffered (RUNNING) — see
	 * `specs/030-queued-chat-input/contracts/turn-loop-contract.md`.
	 */
	submit(content: TurnContent, emit: TurnLoopEmit): void {
		this.turnLoopEmit = emit;
		if (!this.turnLoop) {
			this.turnLoop = new TurnLoop(
				this.sessionId,
				this.template,
				(content_, signal) => this.runTeamTurn(content_, signal),
				(frame) => this.turnLoopEmit?.(frame),
				PRIMARY_AGENT_NAME,
			);
		}
		this.turnLoop.submit(content);
	}

	/** True iff the per-session TurnLoop has a turn in flight or is draining. */
	isRunning(): boolean {
		return this.turnLoop?.isRunning() ?? false;
	}

	/** Abort the in-flight turn and clear the queue (FR-011). */
	abort(): void {
		this.turnLoop?.abort();
	}

	/**
	 * The team turn runner (TurnLoop dependency, D10): one team turn = one
	 * graph `streamEvents` on the session's thread, with the user content as
	 * a new `playerMessages` entry. Streams the produced messages back as
	 * {@link TurnBlock}s tagged with the producing agent.
	 *
	 * The outer graph's `streamEvents` yields `updates` events per NODE
	 * completion (`{node: "player"|"planner", values: {playerMessages|
	 * plannerMessages: [...]}}` — the node's channel write-back, empirically
	 * verified against `@langchain/langgraph` ^1.4.8). Each node's channel
	 * array is the FULL history for that channel, so only the messages past
	 * the per-agent {@link emittedCounts} watermark are converted (node
	 * output dedup across repeated player runs in one turn / across turns).
	 *
	 * `gameEnded` is handled INSIDE the turn by the conditional edge (D6) —
	 * the turn always completes on its own, preserving TurnLoop single-flight.
	 */
	private async *runTeamTurn(
		content: TurnContent,
		signal?: AbortSignal,
	): AsyncIterable<TurnBlock> {
		const stream = (await this.graphHandle.graph.streamEvents(
			{
				playerMessages: [
					new HumanMessage({ content: buildContentBlocks(content) }),
				],
			},
			{
				configurable: { thread_id: this.sessionId },
				metadata: { session_id: this.sessionId },
				version: "v3",
				recursionLimit: RECURSION_LIMIT,
				signal,
			},
		)) as unknown as {
			output: Promise<unknown>;
			[Symbol.asyncIterator](): AsyncIterator<{
				method?: string;
				params?: { node?: string; data?: { values?: Record<string, BaseMessage[]> } };
			}>;
		};

		// Node-completion updates: `updates` events at the root namespace
		// carry `params.node` = the outer node name (player/planner) and
		// `params.data.values` = the node's channel write-back.
		for await (const event of stream) {
			if (event.method !== "updates") continue;
			const node = event.params?.node ?? "";
			const values = event.params?.data?.values;
			if (!node || !values) continue;

			const channel =
				node === "player"
					? values.playerMessages
					: node === "planner"
						? values.plannerMessages
						: undefined;
			if (!channel) continue;

			const emitted = this.emittedCounts.get(node) ?? 0;
			const fresh = channel.slice(emitted);
			if (fresh.length > 0) {
				this.emittedCounts.set(node, emitted + fresh.length);
			}
			for (const msg of fresh) {
				for (const block of messageToContentBlocks(msg)) {
					yield { agent: node, block };
				}
			}
		}

		await stream.output;
	}
}

// ---------------------------------------------------------------------------
// SessionTeamStore
// ---------------------------------------------------------------------------

/**
 * Re-entry conflict thrown by {@link SessionTeamStore.create} when the
 * session already has a team created with a DIFFERENT profile (user
 * refinement, api-contract.md §2.2): the create is not an idempotent retry
 * but a configuration mismatch, so it maps to gRPC ALREADY_EXISTS in the
 * handler (handler.ts CreateTeam), carrying the existing profile for
 * diagnostics.
 */
export class TeamAlreadyExistsError extends Error {
	constructor(
		readonly sessionId: string,
		readonly existingProfile: string,
		readonly requestedProfile: string,
	) {
		super(
			`team already exists for session '${sessionId}' with profile '${existingProfile}'; ` +
				`cannot re-create with profile '${requestedProfile}'`,
		);
		this.name = "TeamAlreadyExistsError";
	}
}

export class SessionTeamStore {
	private teams = new Map<
		string,
		{ team: SessionTeam; profileName: string }
	>();
	private pending = new Map<string, Promise<SessionTeam>>();

	/**
	 * @param factory Builds a wired `SessionTeam` for a session id (DI seam).
	 */
	constructor(private readonly factory: SessionTeamFactory) {}

	/**
	 * Explicitly create the session's team (AIP-133 CreateTeam — replaces the
	 * former lazy `getOrCreate`: the team is NOT created implicitly by
	 * Connect/ListMessages anymore, and the profile is supplied by the
	 * caller instead of a fixed default).
	 *
	 * Re-entry is profile-conditional (user refinement — api-contract.md
	 * §2.2): the Team is a per-session singleton, so a repeated create with
	 * the SAME profile returns the existing team (idempotent — the desktop's
	 * create-if-missing flow retries safely); a repeated create with a
	 * DIFFERENT profile rejects with {@link TeamAlreadyExistsError} (carrying
	 * the existing profile), since that is a configuration mismatch rather
	 * than an idempotent retry. Strict AIP-133 ALREADY_EXISTS for same-profile
	 * retries is deliberately NOT returned — rationale in api-contract.md §2.2.
	 *
	 * Concurrent creates for the same session are single-flighted: the second
	 * caller awaits the first's team, then re-enters the profile comparison
	 * above (a loser carrying a different profile rejects after the winner
	 * completes).
	 */
	create(
		sessionId: string,
		template: string,
		profileName: string,
	): Promise<SessionTeam> {
		const existing = this.teams.get(sessionId);
		if (existing) {
			if (existing.profileName !== profileName) {
				return Promise.reject(
					new TeamAlreadyExistsError(
						sessionId,
						existing.profileName,
						profileName,
					),
				);
			}
			return Promise.resolve(existing.team);
		}
		const inFlight = this.pending.get(sessionId);
		if (inFlight) {
			// Single-flight: await the winner, then apply the same re-entry rule
			// (the winner's profile may differ from this caller's).
			return inFlight.then(() => this.create(sessionId, template, profileName));
		}

		const building = this.factory(sessionId, template, profileName)
			.then((team) => {
				this.teams.set(sessionId, { team, profileName });
				return team;
			})
			.finally(() => {
				this.pending.delete(sessionId);
			});
		this.pending.set(sessionId, building);
		return building;
	}

	/** Synchronous lookup (mcp-host `SessionBridgeLookup`; misses = 404). */
	get(sessionId: string): SessionTeam | undefined {
		return this.teams.get(sessionId)?.team;
	}
}

// ---------------------------------------------------------------------------
// Message → ContentBlock conversion
// ---------------------------------------------------------------------------

/**
 * Convert a checkpointed `BaseMessage` into display `ContentBlock`s (the
 * same shape `turn-loop.ts` frames — so live streaming and history render
 * identically, spec 023 FR-009):
 *
 * - `human` → none (the user's own input is not re-emitted as agent output);
 * - `ai` → reasoning/text blocks from the content, plus one `tool_call`
 *   block per `AIMessage.tool_calls`;
 * - `tool` → one `tool_result` block (status read verbatim from
 *   `additional_kwargs.toolResultStatus`, UNSPECIFIED when absent — NEVER
 *   FAILED, spec FR-014/FR-015); message + screenshot parsed via the shared
 *   `parseToolResultFields`.
 */
export function messageToContentBlocks(msg: BaseMessage): ContentBlock[] {
	const msgType = msg._getType?.() ?? "";
	if (msgType === "human" || msgType === "system") {
		return [];
	}

	const blocks: ContentBlock[] = [];
	if (msgType === "ai") {
		if (typeof msg.content === "string") {
			if (msg.content) blocks.push({ type: "text", text: msg.content });
		} else if (Array.isArray(msg.content)) {
			for (const b of msg.content as { type?: string; [k: string]: unknown }[]) {
				if (b.type === "reasoning" && typeof b.reasoning === "string") {
					blocks.push({ type: "reasoning", reasoning: b.reasoning });
				} else if (b.type === "text" && typeof b.text === "string") {
					blocks.push({ type: "text", text: b.text });
				}
			}
		}
		for (const call of extractToolCalls(msg)) {
			blocks.push({
				type: "tool_call",
				name: call.name ?? "",
				args: call.args ?? {},
				toolCallId: call.id ?? "",
			});
		}
		return blocks;
	}

	if (msgType === "tool") {
		const toolCallId = (msg as unknown as { tool_call_id?: string })
			.tool_call_id;
		const parsed = parseToolResultFields(msg.content);
		const block: ContentBlock = {
			type: "tool_result",
			toolCallId: toolCallId ?? "",
			status: readToolResultStatus(msg),
			message: parsed.message,
		};
		if (parsed.screenshot) {
			block.screenshot = parsed.screenshot;
		}
		return [block];
	}

	return [];
}
