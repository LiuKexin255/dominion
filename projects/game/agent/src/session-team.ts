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
 * doc — this breaks the UpdateTeam→MCP-host circular dependency).
 *
 * - **Turn runner**: {@link SessionTeam.runTeamTurn} drives the compiled
 *   graph's `streamEvents` on the session's thread (thread_id = session id,
 *   FR-013) and converts each node's channel update into a stream of
 *   `TurnBlock`s tagged with the producing agent (`player`/`planner`, D12).
 * - **Async initInstruction (039 US3, T029 — contract §6, FR-015/R2)**:
 *   {@link SessionTeam.triggerInitInstruction} runs ONE background graph
 *   turn right after the graph FIRST materialization (called by
 *   `SessionTeamStore.update` — profile-change rebuilds never re-trigger it,
 *   040 FR-005). The turn carries the `runInitInstruction` configurable flag
 *   (the START conditional edge routes it to the `initInstruction` node →
 *   END — the player is NOT invoked) and produces the no-game-history
 *   calibration instruction into `playerMessages` (as a HumanMessage — same
 *   channel write-back as the review node's `instruct_player`). The RPC
 *   does NOT wait for it (fire-and-forget, `UpdateTeam` 物化即返回); a user
 *   message arriving meanwhile is queued by the TurnLoop and its turn awaits
 *   the init turn completion, so the instruction is already in the channel
 *   BEFORE the user input is appended (FR-015 — no pending slot needed).
 * - **RefreshTeam** (FR-018): {@link SessionTeam.refreshTeam} clears BOTH
 *   short-term message channels via `graph.updateState` (the
 *   `context-middleware` helpers); the long-term memory (memory service /
 *   frozen snapshot) is untouched. 039 US3 (contract §7): init/compact
 *   instructions live IN `playerMessages`, so the channel clear covers them
 *   (no stale instructions).
 * - **MCP host surface**: {@link SessionTeam.getBridge} / {@link SessionTeam.getSink}
 *   feed the `SessionBridgeLookup` (mcp-host.ts) so the saolei MCP server is
 *   built per session with the team sink bound to this session's buffer. The
 *   instances are the SAME ones injected by the store factory (server.ts
 *   pre-registers them before `buildSaoleiMcpTools` connects — see
 *   `server.ts` for the circular-dependency rationale).
 *
 * `SessionTeamStore` (replaces `SessionAgentStore`) maps session id →
 * `SessionTeam`, materializing entries ONLY through the explicit
 * {@link SessionTeamStore.update} (AIP-134 create-or-update `allow_missing`:
 * https://google.aip.dev/134#create-or-update — the singleton's only
 * materialization point, replacing the former AIP-133 CreateTeam; no lazy
 * creation). Re-entry is profile-conditional: same profile → idempotent;
 * different profile → team-graph rebuild against the existing checkpointer
 * (US3 of specs/040-team-singleton-conformance/spec.md FR-005 — state
 * preserved; rejected FAILED_PRECONDITION while a turn is in-flight,
 * FR-006).
 */

import { HumanMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import * as grpc from "@grpc/grpc-js";
import { warn } from "@dominion/common-js-logs";

import type { TeamFrame } from "../game_types/projects/game/TeamFrame";
import type { MessagePart } from "../game_types/projects/game/MessagePart";
import type { MessageRole } from "../game_types/projects/game/MessageRole";

import type { OperationBridge, SinkHandle } from "./operation-bridge";
import { TurnLoop, buildTeamFrame } from "./turn-loop";
import type { TurnBlock } from "./turn-loop";
import type { ContentBlock, TurnContent } from "./llm";
import {
	buildContentBlocks,
	extractToolCalls,
	readToolResultStatus,
	RECURSION_LIMIT,
	STATUS_UNSPECIFIED,
} from "./llm";
import { parseToolResultFields } from "./tools/shared/result-blocks";
import { refreshTeamChannels } from "./context-middleware";
import type { TeamGraphHandle } from "./team/graph";
import type { MemorySaver } from "@langchain/langgraph";
import type { TeamStateValue } from "./team/state";
import type { EphemeralGameBuffer } from "./team/team-sink";
import type { SaoleiEventSink } from "./mcp/saolei/saolei-mcp";

/** The session's primary agent — stamped on control frames (D12). */
export const PRIMARY_AGENT_NAME = "player";

/**
 * Factory that builds a fully-wired `SessionTeam` for a session id
 * (DI seam — `style/javascript.md` §测试: the store never constructs team
 * internals itself, so tests inject fakes without `vi.mock`). The template
 * and TeamProfile name come from the UpdateTeam request (AIP-134): the
 * production factory resolves that profile's player/planner models
 * (prompt-client.getTeamProfile) — there is no fixed default profile anymore
 * (Agent 移除懒加载模式, spec 031-team-template-mode design decision).
 */
export type SessionTeamFactory = (
	sessionId: string,
	template: string,
	profileName: string,
) => Promise<SessionTeam>;

/**
 * Rebuild-only factory (US3 profile-change rebuild — DI seam, same rationale
 * as {@link SessionTeamFactory}). Builds a NEW compiled team graph handle
 * against the EXISTING checkpointer, reusing the session's
 * profile-independent instances (buffer/bridge/sink/MCP tools — the
 * production implementation lives in server.ts, team-rebuild-contract.md
 * §3/§5); it NEVER creates a MemorySaver — that would drop the session's
 * history (FR-005).
 *
 * @param sessionId The dominion session id (thread id of the checkpoint).
 * @param template  The session's template path segment (unchanged by the
 *   rebuild — the profile's template segment matches the team's, FR-008).
 * @param profileName The NEW TeamProfile id to rebuild with.
 * @param existingCheckpointer The existing team's outer MemorySaver — the
 *   recompiled graph restores the session state from it by `thread_id`.
 * @returns The new `TeamGraphHandle` (its `checkpointer` is the injected
 *   `existingCheckpointer` — same reference). The caller replaces the
 *   SessionTeam's handle on success.
 */
export type SessionTeamRebuilder = (
	sessionId: string,
	template: string,
	profileName: string,
	existingCheckpointer: MemorySaver,
) => Promise<TeamGraphHandle>;

/**
 * Emit a non-model-produced channel message as a real-time agent frame
 * (specs/037-saolei-team-optimize/data-model.md §4 — planner review input,
 * compression summaries). Passed to the graph nodes via the LangGraph
 * `configurable` object (tasks.md 决策 #1) rather than TeamGraphDeps, so the
 * node signature stays DI-free; {@link SessionTeam.runTeamTurn} installs it in
 * the `streamEvents` config, and planner/compress nodes read
 * `config?.configurable?.emitChannelFrame` (type `ChannelFrameEmitter | undefined`).
 *
 * `frameId` is an optional dedup anchor: the compress node passes its summary
 * AIMessage's id so the live frame and the reloaded ListMessages entry share
 * one id (data-model.md §4 去重规则, research.md D9 — desktop
 * `renderedMessageIds` dedups on `frameId == msg.id`). The planner's
 * review-input emission omits it (the frame gets a fresh randomUUID, the
 * historical behavior — the review-input dedup gap is a US1 follow-up).
 *
 * `role` is an optional explicit `MessageRole` proto name (e.g.
 * `"MESSAGE_ROLE_USER"`). `buildTeamFrame` defaults messageParts frames to
 * `MESSAGE_ROLE_AGENT`; passing a role overrides it. The planner passes
 * `"MESSAGE_ROLE_USER"` for the review input — it is a HumanMessage, so
 * ListMessages returns it as USER and the desktop renders it through the
 * pre-wrap text path (game-board newlines preserved). Without the override the
 * frame would render as AGENT markdown, which collapses the single newlines
 * that lay out the board grid (the US1 format-loss bug).
 *
 * 041 Phase 3 (T006, specs/041-realtime-init-push/research.md D3 — faithful
 * message→frame mirroring): the emitter ALSO accepts pre-built
 * `MessagePart[]` (second overload) so the
 * init instruction node can emit the planner's tool-call response as a
 * `toolCall` part, rendering identically to the reloaded ListMessages entry.
 * The two overloads keep the existing text-only call sites (compress.ts:244-262,
 * planner.ts:337-342) type-checked unchanged (backward compatible).
 */
export type ChannelFrameEmitter = {
	(agent: string, content: string, frameId?: string, role?: MessageRole): void;
	(
		agent: string,
		parts: MessagePart[],
		frameId?: string,
		role?: MessageRole,
	): void;
};

// ---------------------------------------------------------------------------
// SessionTeam
// ---------------------------------------------------------------------------

export class SessionTeam {
	private graphHandle: TeamGraphHandle;
	private readonly buffer: EphemeralGameBuffer;
	private readonly bridge: OperationBridge;
	private readonly sink: SaoleiEventSink;
	private readonly sessionId: string;
	/** The session's template path segment (e.g. "saolei") — the templateId
	 * stamped on every outbound TeamFrame (REQUIRED, api-contract.md §3.6). */
	private readonly template: string;

	private turnLoop: TurnLoop | null = null;

	/**
	 * The stream-bound display sink (041 — specs/041-realtime-init-push/
	 * contracts/realtime-channel-contract.md §1.1, FR-010): the
	 * single write target for ALL display-channel frames from this session —
	 * the TurnLoop's display frames, the compress/review channel frames, and
	 * (once installed, 041 Phase 3 T005) the init turn's `emitChannelFrame`
	 * (contract §1.2 unified read path). Bound by {@link bindStreamSink} when
	 * the Connect handler sees the first per-session inbound frame; cleared
	 * on stream end/error via {@link clearStreamSink} (compare-and-delete on
	 * {@link streamSinkHandle}). `null` while unbound — emitting through it
	 * is a no-op (best-effort, specs/041-realtime-init-push/research.md D9).
	 */
	private streamSink: ((frame: TeamFrame) => void) | null = null;
	/**
	 * The handle of the currently-bound {@link streamSink} (opaque per-stream
	 * identity, specs/041-realtime-init-push/data-model.md §1.1):
	 * `clearStreamSink` only clears when the
	 * passed handle matches this one, so a stale stream's cleanup cannot
	 * clobber a newer binding (mirrors `OperationBridge.unregisterSink`,
	 * operation-bridge.ts:163-170).
	 */
	private streamSinkHandle: SinkHandle | null = null;

	/**
	 * The one-shot async initInstruction turn (039 US3, T029 — contract §6,
	 * FR-015/R2): set by {@link triggerInitInstruction} after graph FIRST
	 * materialization and awaited by {@link runTeamTurn} so a user message
	 * arriving during the async init runs AFTER the instruction was produced
	 * (the pending slot is consumed before the user input — FR-015).
	 * `null` until triggered; never re-triggered by profile rebuilds (040
	 * FR-005).
	 */
	private initTurn: Promise<void> | null = null;

	/**
	 * True while the one-shot async initInstruction turn is actually
	 * executing (039 US3 — Phase 6 review Issue #3/#5). Distinct from
	 * {@link initTurn} (which stays non-null forever once triggered):
	 *
	 * - feeds {@link isBusy}, so RefreshTeam / profile-change rebuild are
	 *   rejected with FAILED_PRECONDITION during the init (the refresh
	 *   would otherwise clear a freshly written instruction in
	 *   `playerMessages` — contract §7). It does NOT feed {@link isRunning}:
	 *   the Connect status probe must not report ACTIVE for the init turn —
	 *   the init runs outside the TurnLoop and emits no `wait`, so the
	 *   desktop's typing indicator would stay stuck ON after the init
	 *   completes (one-shot probe, no re-poll; the typing state is driven by
	 *   real user turns instead, which await {@link initTurn} in
	 *   {@link runTeamTurn});
	 * - cleared in `finally` (also on the degrade path), so it never
	 *   blocks the team after the init turn completes.
	 */
	private initInFlight = false;

	/**
	 * @param graphHandle The compiled team graph + outer MemorySaver (built
	 *   by the store factory — server.ts wires models/memory/MCP tools).
	 * @param buffer      The per-session ephemeral game buffer (D7). Owned by
	 *   this session; the sink writes it, the graph nodes read it.
	 * @param sessionId   The dominion session id (the checkpoint thread id).
	 * @param template    The session's template path segment (from the
	 *   UpdateTeam team.name, AIP-134).
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
	 * `updateState` (context-middleware), reset `gameCounter`. 039 US3
	 * (contract §7): init/compact instructions live IN `playerMessages`, so
	 * the channel clear removes any stale instruction. The long-term memory
	 * (memory service / frozen snapshot) and `gameEnded` are untouched.
	 * Caller MUST reject while a turn is in flight ({@link isRunning}).
	 */
	async refreshTeam(): Promise<void> {
		await refreshTeamChannels(this.graphHandle.graph, this.sessionId);
	}

	/**
	 * Trigger the ONE-SHOT async initInstruction turn (039 US3, T029 —
	 * contract §6, FR-015/R2). Called by `SessionTeamStore.update` right
	 * after the graph FIRST materialization; profile-change rebuilds
	 * (040 FR-005) never call it.
	 *
	 * Fire-and-forget: `UpdateTeam` 物化即返回（不等 LLM）。The turn runs the
	 * graph with the `runInitInstruction` configurable flag — the START
	 * conditional edge routes it to the `initInstruction` node → END (the
	 * player is NOT invoked); the produced instruction lands in
	 * `playerMessages` (as a HumanMessage — same channel write-back as the
	 * review node) and is delivered with the player's next activation. A
	 * user message arriving during the async produce is queued by the
	 * TurnLoop, and its turn awaits {@link initTurn} first — so the
	 * instruction is already in the channel before the user input is
	 * appended (FR-015).
	 *
	 * Failure degrades (contract §6): a planner-model outage skips the
	 * instruction (logged), never blocks the team or the materialization.
	 */
	triggerInitInstruction(): void {
		if (this.initTurn) return;
		this.initInFlight = true;
		this.initTurn = this.runInitTurn().finally(() => {
			// Cleared on BOTH the success and the degrade path (runInitTurn
			// swallows errors internally, so this finally never observes a
			// rejection).
			this.initInFlight = false;
		});
	}

	/**
	 * The init turn runner: ONE graph `invoke` on the session thread with
	 * the `runInitInstruction` flag + a fresh R1 instruction buffer.
	 *
	 * 041 Phase 3 (T005 — specs/041-realtime-init-push/research.md D1/D2,
	 * specs/041-realtime-init-push/contracts/realtime-channel-contract.md
	 * §2.1): the configurable NOW installs `emitChannelFrame` (the same
	 * key/shape the user-turn path uses, {@link SessionTeam.runTeamTurn}), so
	 * the instruction node pushes the produced instruction frames through the
	 * stream-bound display sink (research.md D1 — bound at Connect,
	 * contract §1.1) as soon as the connection is up. The init turn runs
	 * right after `UpdateTeam` materialization, which the desktop may await
	 * BEFORE connecting (desktop App.svelte: `await updateTeam(...)` →
	 * `continueSessionEntry` → `await handleConnect()` — serial); the
	 * emission is best-effort (research.md D9): with the sink still unbound
	 * it is a no-op, and the instruction is already in the checkpoint, so
	 * the one-shot seed + `loadAgentHistories` deliver it on connect
	 * instead. A planner-model failure degrades inside the instruction node
	 * (warn + `return {}` — no frame emitted, contract §2.3) and the init
	 * promise still resolves. The init turn is a fire-and-forget background
	 * task: {@link isRunning} (the Connect status probe) deliberately
	 * EXCLUDES {@link initInFlight}, so the desktop connects to IDLE and is
	 * immediately ready for input — reporting ACTIVE here would stick its
	 * typing indicator on (the init emits no `wait` and the probe is
	 * one-shot). The init activity's persistent visibility comes from the
	 * checkpointer (the instruction node's plannerMessages output shows up
	 * in ListMessages after Connect) and from the instruction already in
	 * `playerMessages` before the first user turn (which awaits
	 * {@link initTurn} first, FR-015).
	 *
	 * Errors are swallowed (degrade,
	 * specs/039-planner-memory-calibration/contracts/team-graph-contract.md
	 * §6) so the awaited
	 * {@link initTurn} never rejects a subsequent user turn.
	 */
	private async runInitTurn(): Promise<void> {
		try {
			await this.graphHandle.graph.invoke(
				{},
				{
					configurable: {
						thread_id: this.sessionId,
						// The START conditional edge routes this turn to the
						// `initInstruction` node (graph.ts routeAfterStart).
						runInitInstruction: true,
						// R1 external buffer (contract §4 — instruction-tool.ts).
						instructionBuffer: { content: null },
						// 041 Phase 3 (T005 — research.md D2): install the
						// channel-frame emitter so the init instruction node
						// pushes the produced frames (request / tool-call
						// response / player write-back, contract §2.2) through
						// the stream-bound display sink ({@link streamSink},
						// research.md D1). Best-effort: null sink → no-op
						// (research.md D9).
						emitChannelFrame: (
							agent: string,
							content: string | MessagePart[],
							frameId?: string,
							role?: MessageRole,
						) => this.emitChannelFrame(agent, content, frameId, role),
					},
					metadata: { session_id: this.sessionId },
					recursionLimit: RECURSION_LIMIT,
				},
			);
		} catch (err) {
			// contract §6 降级：init 产出失败（如 planner model 不可用）→ 记
			// 日志、跳过初始指令；不阻断 team 运行与 UpdateTeam 物化。
			const message = err instanceof Error ? err.message : String(err);
			warn("init instruction turn failed; skipping initial instruction", {
				sessionId: this.sessionId,
				error: message,
			});
		}
	}

	/**
	 * Bind the Connect stream's display sink (041 — specs/041-realtime-init-push/
	 * contracts/realtime-channel-contract.md §1.1): the
	 * handler calls this on the FIRST inbound frame carrying this session on
	 * a stream (in practice the status probe), passing a write closure over
	 * that stream plus an opaque handle for compare-and-delete. The sink is
	 * independent of the TurnLoop lifecycle: the fire-and-forget init turn
	 * (and any other background producer) emits through it too, so an
	 * in-flight init can reach the desktop as soon as the connection is up
	 * (research.md D1). A rebind (new Connect) replaces the previous
	 * sink/handle pair — the superseded stream's {@link clearStreamSink}
	 * then no-ops (compare-and-delete, FR-010).
	 *
	 * @param sink   The stream write target: `(frame) => safeWrite(stream,
	 *   frame, sessionId)` (handler.ts).
	 * @param handle Opaque per-stream identity for compare-and-delete
	 *   (data-model.md §1.1). The OperationBridge convention — the sink
	 *   closure itself — suffices (operation-bridge.ts:77).
	 */
	bindStreamSink(sink: (frame: TeamFrame) => void, handle: SinkHandle): void {
		this.streamSink = sink;
		this.streamSinkHandle = handle;
	}

	/**
	 * Clear the bound display sink (041 — specs/041-realtime-init-push/
	 * contracts/realtime-channel-contract.md §1.3, FR-010), but only
	 * when `handle` identifies the CURRENT binding (compare-and-delete): a
	 * stale stream's end/error must not clobber a sink bound by a newer
	 * connection. After clear, in-flight background emissions (e.g. a
	 * still-running init turn) hit `null` and are dropped — no write to a
	 * dead connection (best-effort, research.md D9).
	 */
	clearStreamSink(handle: SinkHandle): void {
		if (this.streamSinkHandle === handle) {
			this.streamSink = null;
			this.streamSinkHandle = null;
		}
	}

	/**
	 * Build + emit one display `TeamFrame` through the stream-bound display
	 * sink (041 — specs/041-realtime-init-push/contracts/
	 * realtime-channel-contract.md §1.2, unified read path;
	 * specs/041-realtime-init-push/research.md D2). The
	 * shared implementation behind the `configurable.emitChannelFrame`
	 * closures installed by BOTH the user-turn runner ({@link runTeamTurn})
	 * and the init-turn runner ({@link runInitTurn}), so every
	 * display-channel producer — the TurnLoop, the compress/review nodes and
	 * the init instruction node — resolves `this.streamSink` LIVE over
	 * `this`: a rebind/clear during the connection reflects on the next
	 * emit, and a `null` sink is a no-op (best-effort, research.md D9).
	 *
	 * `content` is either plain text (built into a `text` part — the
	 * pre-existing call shape, compress.ts:244-262 / planner.ts:337-342) or
	 * pre-built `MessagePart[]` (041 T006 — the init instruction node passes
	 * the planner response's `toolCall` part for faithful mirroring,
	 * research.md D3). `frameId` is the producing message's id when the
	 * caller has one (dedup anchor `frameId == msg.id`, data-model.md §4 —
	 * compress node and init instruction node); `role` overrides the
	 * messageParts default AGENT (e.g. `MESSAGE_ROLE_USER` for
	 * HumanMessage-sourced frames, {@link ChannelFrameEmitter}).
	 */
	private emitChannelFrame(
		agent: string,
		content: string | MessagePart[],
		frameId?: string,
		role?: MessageRole,
	): void {
		const frame: TeamFrame = buildTeamFrame(
			this.sessionId,
			this.template,
			{
				agent,
				messageParts: {
					parts:
						typeof content === "string"
							? [{ text: { content } }]
							: content,
				},
			},
			frameId,
		);
		if (role) frame.role = role;
		this.streamSink?.(frame);
	}

	/**
	 * Route a user-content submission to the per-session {@link TurnLoop}
	 * (single-flight owner; one user input = one team turn). Lazily
	 * constructs the loop on first use with the team-graph turn runner; the
	 * loop's display frames are emitted through the stream-bound display
	 * sink ({@link streamSink}, bound at Connect via {@link bindStreamSink}
	 * — specs/041-realtime-init-push/contracts/realtime-channel-contract.md
	 * §1.2), resolved LIVE over `this` so a rebind/clear is
	 * reflected on the next emit without reconstructing the loop.
	 * Non-blocking: returns once the content is started (IDLE) or buffered
	 * (RUNNING) — see
	 * `specs/030-queued-chat-input/contracts/turn-loop-contract.md`.
	 */
	submit(content: TurnContent): void {
		if (!this.turnLoop) {
			this.turnLoop = new TurnLoop(
				this.sessionId,
				this.template,
				(content_, signal) => this.runTeamTurn(content_, signal),
				(frame) => this.streamSink?.(frame),
				PRIMARY_AGENT_NAME,
			);
		}
		this.turnLoop.submit(content);
	}

	/**
	 * True iff a REAL team turn is in flight (TurnLoop running or draining
	 * queued work).
	 *
	 * NOTE: the one-shot async initInstruction turn is deliberately EXCLUDED
	 * (039 US3 — deploy bugfix): this method feeds the Connect status probe
	 * (handler.ts), and the init turn runs OUTSIDE the TurnLoop via
	 * `graph.invoke` (fire-and-forget) — it emits no `wait` frame, and the
	 * probe is one-shot, so reporting ACTIVE for it would leave the desktop
	 * stuck on "Agent is typing" forever after the init completes. The
	 * desktop instead sees IDLE on connect and the typing indicator is
	 * driven by actual user turns (which await {@link initTurn} first, so
	 * the instruction already in `playerMessages` still precedes the first
	 * activation, FR-015).
	 * The init turn IS still gated for the destructive operations
	 * (RefreshTeam / profile-change rebuild) via {@link isBusy}.
	 */
	isRunning(): boolean {
		return this.turnLoop?.isRunning() ?? false;
	}

	/**
	 * True iff the per-session team has work that must gate destructive
	 * operations: a real turn (TurnLoop) OR the one-shot async
	 * initInstruction turn (039 US3, Phase 6 review Issue #3/#5). Used by
	 * RefreshTeam and the profile-change rebuild to reject with
	 * FAILED_PRECONDITION while the planner is producing the initial
	 * instruction (a refresh could clear a freshly written instruction in
	 * `playerMessages`, contract §7; an in-flight turn must not be
	 * disturbed, FR-006). Deliberately SEPARATE from {@link isRunning} (the
	 * status probe — init excluded): the init must gate the refresh/rebuild
	 * but must NOT drive the desktop's typing indicator.
	 */
	isBusy(): boolean {
		return this.initInFlight || this.isRunning();
	}

	/**
	 * The outer `MemorySaver` bound to the current graph handle (US3
	 * profile-change rebuild — team-rebuild-contract.md §2: the rebuilt graph
	 * MUST receive this SAME checkpointer, never a fresh one, or the
	 * session's per-thread state is lost, violating FR-005).
	 */
	getCheckpointer(): MemorySaver {
		return this.graphHandle.checkpointer;
	}

	/**
	 * Replace the compiled graph handle with a rebuilt one (US3 — profile
	 * change on a materialized team, team-rebuild-contract.md §5). The new
	 * handle was compiled against the SAME outer checkpointer
	 * ({@link getCheckpointer}), so the session's conversation/game state
	 * carries over (FR-005); buffer/bridge/sink/MCP-host/tools are untouched
	 * (profile-independent, §3). Takes effect on the NEXT turn — the
	 * TurnLoop resolves `this` per submit, so in-flight state is unaffected
	 * (the caller MUST have verified {@link isRunning} === false, FR-006).
	 */
	rebuildProfile(newHandle: TeamGraphHandle): void {
		this.graphHandle = newHandle;
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
	 * The outer graph's v3 `streamEvents` yields granular protocol events
	 * from INSIDE the createAgent nodes (verified empirically against
	 * `@langchain/langgraph` 1.4.8 — see the event shapes below; the prebuilt
	 * agent is a subgraph of the outer team graph, so its events bubble up
	 * with `params.namespace[0]` = `<outerNode>:<taskId>`, e.g.
	 * `"player:…"`/`"planner:…"`):
	 *
	 * - **`messages`** (chat-model protocol events,
	 *   `node_modules/@langchain/langgraph/dist/pregel/messages-v2.js`
	 *   `StreamProtocolMessagesHandler`): each text/reasoning block is
	 *   emitted as `content-block-start` → N × `content-block-delta`
	 *   (incremental) → `content-block-finish` (complete). The runner
	 *   consumes ONLY the `content-block-delta`s — they provide the
	 *   token-granular updates the desktop expects (the pre-team
	 *   single-agent path streamed the same deltas via `stream.messages`).
	 *   Models that do not stream still emit one full-text delta through
	 *   `emitFinalMessage`/`deltaFor`, so nothing is lost. Tool calls are
	 *   NOT part of this stream (the handler only replays `message.content`,
	 *   and `AIMessage.tool_calls` live outside it) — so tool calls are
	 *   sourced from the `tools` stream instead.
	 * - **`tools`** (`node_modules/@langchain/langgraph/dist/pregel/stream.js`
	 *   `StreamToolsHandler`):
	 *   `tool-started` fires BEFORE the tool function runs (so it is emitted
	 *   while a saolei `bridge.dispatch` may still be awaiting the desktop),
	 *   carrying `tool_call_id`/`tool_name`/`input` (args, JSON-encoded);
	 *   `tool-finished` carries `output` = the ToolMessage the agent loop
	 *   appends (the same message the node channel write-back would carry, so
	 *   the live and history `tool_result` blocks are identical).
	 *
	 * This replaces the former per-NODE `updates` replay (which only fired
	 * after the player's whole createAgent loop completed — T030 deadlock:
	 * the desktop waits for the tool_call frame before replying to the
	 * operation, so a node-level stream never unblocks it). Dedup: since the
	 * node `updates` events are NOT subscribed anymore, every message is
	 * emitted exactly once by its granular event (no replay, no
	 * double-output); the emitted set is exactly what the channel replay
	 * would have produced (AIMessage text/reasoning + tool_calls + one
	 * ToolMessage per finished tool, minus human/system input and the
	 * strategy injection (removed in 039 Phase 6) which never produce
	 * model/tool events). The former
	 * {@link emittedCounts} watermark is gone with the `updates` path.
	 */
	private async *runTeamTurn(
		content: TurnContent,
		signal?: AbortSignal,
	): AsyncIterable<TurnBlock> {
		// 039 US3 (T029 — FR-015): a user turn must run AFTER the one-shot
		// async initInstruction turn — the instruction is written into
		// `playerMessages` first, so it precedes the user input appended
		// below (异步产出期间到达的 user message 排在指令之后).
		if (this.initTurn) {
			await this.initTurn;
		}
		const stream = (await this.graphHandle.graph.streamEvents(
			{
				playerMessages: [
					new HumanMessage({ content: buildContentBlocks(content) }),
				],
			},
			{
				// `configurable.emitChannelFrame` carries the channel-frame
				// emitter to the nodes (tasks.md 决策 #1 — configurable instead
				// of TeamGraphDeps, see {@link ChannelFrameEmitter}): planner /
				// compress read it as `config?.configurable?.emitChannelFrame`.
				configurable: {
					thread_id: this.sessionId,
					// 041 Phase 3 (T005): the closure now delegates to the
					// shared {@link SessionTeam.emitChannelFrame}
					// (specs/041-realtime-init-push/contracts/
					// realtime-channel-contract.md §1.2 unified read path) so
					// the user-turn and init-turn
					// runners build frames identically (research.md D2).
					emitChannelFrame: (
						agent: string,
						content: string | MessagePart[],
						frameId?: string,
						role?: MessageRole,
					) => this.emitChannelFrame(agent, content, frameId, role),
					// Feature 038 (US1): mid-turn drain seam — the player's
					// `queueDrain` beforeModel middleware calls this before
					// every model call to inject queued user messages
					// (`specs/038-queue-input-mid-turn/contracts/
					// injection-seam-contract.md` §2; FR-001).
					drainQueuedInput: () => this.turnLoop?.drainQueue() ?? null,
					// 039 US3 (T029 — contract §4, R1): the fresh per-turn
					// external instruction buffer. The `instruct_player` tool
					// stages its content here; the review node reads it after
					// the agent invoke returns and appends the instruction to
					// `playerMessages` from its return value (FR-017).
					instructionBuffer: { content: null },
				},
				metadata: { session_id: this.sessionId },
				version: "v3",
				recursionLimit: RECURSION_LIMIT,
				signal,
			},
		)) as unknown as {
			output: Promise<unknown>;
			[Symbol.asyncIterator](): AsyncIterator<TeamStreamEvent>;
		};

		for await (const event of stream) {
			const agent = agentFromNamespace(event.params?.namespace);
			if (!agent) continue;

			if (event.method === "messages") {
				// Consume ONLY the deltas: `content-block-delta` yields the
				// incremental text/reasoning chunks of the model's answer,
				// keeping the desktop's conversation live (regression fix —
				// the pre-team path streamed these same deltas via
				// `stream.messages`, spec 031). Consuming
				// `content-block-finish` as well would double-emit the text,
				// and `content-block-start` carries no content yet.
				const data = event.params?.data as
					| { event?: string; delta?: unknown; content?: unknown }
					| undefined;
				if (data?.event !== "content-block-delta") continue;
				// Protocol-compliant models carry the typed delta on
				// `event.delta` (`{ type: "text-delta", text }` /
				// `{ type: "reasoning-delta", reasoning }`); a few older
				// adapters still emit the content-shaped form
				// (`{ type: "text", text }`) on `event.content` instead —
				// normalize both (Core `getEventDelta` tolerance,
				// `@langchain/core/dist/language_models/stream.js`).
				const delta = data.delta as
					| { type?: string; text?: string; reasoning?: string }
					| undefined;
				const content = data.content as
					| { type?: string; text?: string; reasoning?: string }
					| undefined;
				const typedDelta =
					delta != null &&
					typeof delta === "object" &&
					(delta.type === "text-delta" ||
						delta.type === "reasoning-delta")
						? delta
						: undefined;
				const block = typedDelta ?? content;
				if (!block || typeof block !== "object") continue;
				// Same empty-content filtering as messageToContentBlocks
				// (a model answer with only tool calls yields no text delta,
				// and empty chunks must not be displayed).
				if (
					(block.type === "text" || block.type === "text-delta") &&
					typeof block.text === "string"
				) {
					if (!block.text) continue;
					yield { agent, block: { type: "text", text: block.text } };
				} else if (
					(block.type === "reasoning" ||
						block.type === "reasoning-delta") &&
					typeof block.reasoning === "string"
				) {
					if (!block.reasoning) continue;
					yield {
						agent,
						block: { type: "reasoning", reasoning: block.reasoning },
					};
				}
				// Skip `block-delta` (tool_call_chunk): tool calls are
				// sourced from the `tools` stream below.
			} else if (event.method === "tools") {
				const data = event.params?.data as
					| {
							event?: string;
							tool_call_id?: string;
							tool_name?: string;
							input?: unknown;
							output?: unknown;
							message?: string;
					  }
					| undefined;
				if (data?.event === "tool-started") {
					// Real-time: emitted before the tool function runs, so a
					// tool awaiting the desktop still streams its tool_call.
					yield {
						agent,
						block: {
							type: "tool_call",
							name: data.tool_name ?? "",
							args: parseToolArgs(data.input),
							toolCallId: data.tool_call_id ?? "",
						},
					};
				} else if (data?.event === "tool-finished") {
					// `output` is the ToolMessage the agent loop appended
					// (empirically verified — the `tools` stream carries the
					// same message the channel write-back would).
					const blocks = messageToContentBlocks(
						data.output as unknown as BaseMessage,
					);
					if (blocks.length > 0) {
						for (const block of blocks) yield { agent, block };
					} else {
						// Defensive fallback for a non-message-shaped output.
						yield {
							agent,
							block: {
								type: "tool_result",
								toolCallId: data.tool_call_id ?? "",
								status: STATUS_UNSPECIFIED,
								message:
									typeof data.output === "string"
										? data.output
										: "",
							},
						};
					}
				} else if (data?.event === "tool-error") {
					// A throwing tool produces no ToolMessage in the stream;
					// surface the error text so the desktop still sees a
					// tool_result (status stays neutral — the ToolNode's
					// error ToolMessage carries no toolResultStatus either).
					yield {
						agent,
						block: {
							type: "tool_result",
							toolCallId: data.tool_call_id ?? "",
							status: STATUS_UNSPECIFIED,
							message: data.message ?? "",
						},
					};
				}
			}
		}

		await stream.output;
	}
}

// ---------------------------------------------------------------------------
// SessionTeamStore
// ---------------------------------------------------------------------------

export class SessionTeamStore {
	private teams = new Map<
		string,
		{ team: SessionTeam; profileName: string }
	>();
	private pending = new Map<string, Promise<SessionTeam>>();

	/**
	 * @param factory Builds a wired `SessionTeam` for a session id (DI seam).
	 * @param rebuilder Builds a REBUILT graph handle for a profile change
	 *   (US3 — DI seam; optional so tests that never rebuild can construct
	 *   the store with the factory alone). When omitted, a profile change
	 *   rejects with a configuration error (a production store always passes
	 *   it — server.ts).
	 */
	constructor(
		private readonly factory: SessionTeamFactory,
		private readonly rebuilder?: SessionTeamRebuilder,
	) {}

	/**
	 * Materialize-or-update the session's Team (AIP-134 create-or-update
	 * `allow_missing`: https://google.aip.dev/134#create-or-update — the
	 * singleton's ONLY materialization point, replacing the former AIP-133
	 * `create`; the team is NOT created implicitly by Connect/ListMessages
	 * anymore, and the profile is supplied by the caller instead of a fixed
	 * default).
	 *
	 * Dispatch (specs/040-team-singleton-conformance/contracts/
	 * api-contract.md §2.3):
	 *
	 * - missing + `allowMissing=true` → build via the factory (the caller
	 *   materializes the Team — FR-001/FR-002);
	 * - missing + `allowMissing=false` → NOT_FOUND (standard AIP-134 Update
	 *   semantics — specs/040-team-singleton-conformance/data-model.md §4);
	 * - existing + SAME profile → return the existing team (idempotent,
	 *   FR-002 — repeated calls are safe);
	 * - existing + DIFFERENT profile → rebuild the team graph against the
	 *   existing checkpointer (FR-005 — see {@link rebuild}), rejecting
	 *   FAILED_PRECONDITION while a turn is in-flight (FR-006).
	 *
	 * Concurrent updates for the same session are single-flighted: the second
	 * caller awaits the first's team, then re-enters the dispatch above (a
	 * loser carrying a different profile rebuilds after the winner completes).
	 */
	update(
		sessionId: string,
		template: string,
		profileName: string,
		allowMissing: boolean,
	): Promise<SessionTeam> {
		const existing = this.teams.get(sessionId);
		if (existing) {
			if (existing.profileName !== profileName) {
				// FR-005: a profile change rebuilds the team graph reusing the
				// existing checkpointer (state preserved); FR-006: rejected
				// while a turn is in-flight.
				return this.rebuild(sessionId, template, profileName, existing);
			}
			return Promise.resolve(existing.team);
		}
		if (!allowMissing) {
			// Standard AIP-134 Update on a missing singleton → NOT_FOUND. The
			// handler propagates the gRPC status code unchanged (the same
			// pass-through it applies to downstream errors).
			return Promise.reject(
				Object.assign(
					new Error(
						`team not materialized for session '${sessionId}'; ` +
							"update with allow_missing=true to materialize (AIP-134)",
					),
					{ code: grpc.status.NOT_FOUND },
				),
			);
		}
		const inFlight = this.pending.get(sessionId);
		if (inFlight) {
			// Single-flight: await the winner, then re-enter the dispatch
			// (the winner's profile may differ from this caller's).
			return inFlight.then(() =>
				this.update(sessionId, template, profileName, allowMissing),
			);
		}

		const building = this.factory(sessionId, template, profileName)
			.then((team) => {
				this.teams.set(sessionId, { team, profileName });
				// 039 US3 (T029 — contract §6, FR-015/R2): the team graph
				// FIRST materialization asynchronously triggers the one-shot
				// initInstruction turn (fire-and-forget — `UpdateTeam` 物化即
				// 返回，不等 LLM). Profile-change rebuilds go through
				// {@link rebuild}, which NEVER re-triggers init (040 FR-005).
				team.triggerInitInstruction();
				return team;
			})
			.finally(() => {
				this.pending.delete(sessionId);
			});
		this.pending.set(sessionId, building);
		return building;
	}

	/**
	 * Rebuild the team graph for a profile change (US3, FR-005/FR-006;
	 * team-rebuild-contract.md §5):
	 *
	 * 1. in-flight guard: a running turn rejects FAILED_PRECONDITION (same
	 *    guard semantics as RefreshTeam — handler.ts), the existing team and
	 *    the in-flight turn stay untouched. `isBusy()` (not `isRunning()`)
	 *    also gates the one-shot async initInstruction turn (a rebuild
	 *    during the init would race the freshly written instruction in
	 *    `playerMessages`, contract §7);
	 * 2. single-flight via the shared `pending` map (a concurrent rebuild
	 *    awaits the winner, then re-enters `update` — the winner may have
	 *    already applied this profile);
	 * 3. rebuild via the rebuilder (server.ts) with the existing
	 *    `handle.checkpointer` (NEVER a new MemorySaver — state preservation);
	 * 4. on success, swap the SessionTeam's graphHandle and record the new
	 *    profileName; on failure the existing team/profile are left
	 *    unchanged (no half-rebuilt state — the pending entry is cleared).
	 */
	private rebuild(
		sessionId: string,
		template: string,
		profileName: string,
		existing: { team: SessionTeam; profileName: string },
	): Promise<SessionTeam> {
		if (existing.team.isBusy()) {
			return Promise.reject(
				Object.assign(
					new Error(
						`cannot change team profile for session '${sessionId}' while a turn is in-flight`,
					),
					{ code: grpc.status.FAILED_PRECONDITION },
				),
			);
		}
		const inFlight = this.pending.get(sessionId);
		if (inFlight) {
			// Single-flight: await the winner, then re-enter the dispatch (the
			// winner may have already applied the requested profile; if not,
			// this caller rebuilds in turn).
			return inFlight.then(() =>
				this.update(sessionId, template, profileName, true),
			);
		}
		if (!this.rebuilder) {
			return Promise.reject(
				new Error("team graph rebuild is not configured"),
			);
		}

		const rebuilding = this.rebuilder(
			sessionId,
			template,
			profileName,
			existing.team.getCheckpointer(),
		)
			.then((handle) => {
				existing.team.rebuildProfile(handle);
				this.teams.set(sessionId, { team: existing.team, profileName });
				return existing.team;
			})
			.finally(() => {
				this.pending.delete(sessionId);
			});
		this.pending.set(sessionId, rebuilding);
		return rebuilding;
	}

	/** Synchronous lookup (mcp-host `SessionBridgeLookup`; misses = 404). */
	get(sessionId: string): SessionTeam | undefined {
		return this.teams.get(sessionId)?.team;
	}

	/**
	 * The profile the session's team was materialized with (FR-004 — GetTeam
	 * reads it back into the Team resource body). Undefined when the team is
	 * missing (GetTeam reports NOT_FOUND first).
	 */
	getProfileName(sessionId: string): string | undefined {
		return this.teams.get(sessionId)?.profileName;
	}
}

// ---------------------------------------------------------------------------
// Message → ContentBlock conversion
// ---------------------------------------------------------------------------

/**
 * One v3 `streamEvents` protocol event (the shape actually consumed by
 * {@link SessionTeam.runTeamTurn}). Only `method`/`params.namespace`/
 * `params.data` are read; the structure is defined by
 * `@langchain/langgraph`'s `stream/convert.ts` (`convertToProtocolEvent`).
 */
interface TeamStreamEvent {
	method?: string;
	params?: {
		namespace?: string[];
		data?: unknown;
	};
}

/**
 * Attribute a bubbling subgraph event to its outer team node
 * (`player`/`planner`). createAgent is a subgraph of the outer team graph,
 * so its internal events carry `params.namespace[0]` = `<outerNode>:<taskId>`
 * (e.g. `"player:c68039a1-…"` — the checkpoint namespace prefix, verified
 * empirically against `@langchain/langgraph` 1.4.8, `dist/stream/convert.js`
 * `unwrapMessagesPayload`/`dist/pregel/messages-v2.js`). Root-namespace
 * events (the outer graph's own `updates`/`values`/`tasks`/…) yield
 * `undefined` and are skipped by the turn runner.
 */
function agentFromNamespace(namespace: string[] | undefined): string | undefined {
	const segment = namespace?.[0];
	if (typeof segment !== "string") return undefined;
	const sep = segment.indexOf(":");
	return sep > 0 ? segment.slice(0, sep) : undefined;
}

/**
 * Decode the `tool-started` event's `input` into the tool_call args object.
 * The `tools` stream carries the args JSON-encoded (empirically verified —
 * `node_modules/@langchain/langgraph/dist/pregel/stream.js`
 * `StreamToolsHandler`), while the display contract expects the parsed object
 * (`turn-loop.ts` `displayFrame` JSON-stringifies `block.args`).
 */
function parseToolArgs(input: unknown): unknown {
	if (typeof input === "string") {
		try {
			return JSON.parse(input);
		} catch {
			return input;
		}
	}
	return input ?? {};
}

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
