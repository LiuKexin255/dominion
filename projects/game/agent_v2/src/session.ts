/**
 * session.ts — game session ↔ dsh agent mapping for agent_v2.
 *
 * `AgentSessions` owns the materialization registry over `ctx.agents`
 * (session resource name → live dsh agent, host-chosen SessionId per
 * specs/049-agent-v2-dsh-init/data-model.md §2.2), a per-session FIFO queue
 * with a TurnRunner (mid-turn sends enqueue with a queued{position} frame
 * and auto-run at turn end, specs/049-agent-v2-dsh-init/spec.md FR-012), and
 * the UpdateAgent materialization semantics (specs/051-agent-v2-dsh-migration/data-model.md §2.2): agents
 * are created ONLY through {@link AgentSessions.materialize} — Send has no
 * lazy creation and fails FAILED_PRECONDITION on an unmaterialized session,
 * and re-materializing tears the in-flight turn down (turn_end{ABORTED},
 * queue dropped) before the agent is disposed and rebuilt, whatever the
 * configuration (refresh folded into Update). Everything except the preset
 * store is process memory (A2): shutdown disposes every entry and the
 * composition's root fiber. Sessions are independent: each entry drives its
 * own turns, and concurrent sessions never block each other.
 *
 * The Context injected at construction is the dependency seam: unit tests
 * pass a mock `ctx` and drive the captured event listeners instead of
 * intercepting modules (style/javascript.md Mock convention).
 */

import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { error, info } from "@dominion/common-js-logs";
import type { Agent, AgentHandle, CreateAgentOptions } from "@deepseek-ai/dsh-agent";
import { createAgentGameRuntime } from "@dominion/dsh-saolei-loop";
import type { PresetAuthoringService } from "@dominion/dsh-preset-authoring";
import type { HistoryMessage } from "../agent_v2_types/projects/game/v2/HistoryMessage.js";
import type { TurnEndEvent } from "../agent_v2_types/projects/game/v2/TurnEndEvent.js";
import type { TurnStartEvent } from "../agent_v2_types/projects/game/v2/TurnStartEvent.js";
import type { TurnUsage } from "../agent_v2_types/projects/game/v2/TurnUsage.js";
import type { QueuedEvent } from "../agent_v2_types/projects/game/v2/QueuedEvent.js";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";
import type { DshContext } from "./dsh.js";
import { mintTurnId, SessionHistory, TurnCollector } from "./history.js";
import type { DshTokenUsage, TurnOutcome, TurnSettlement, TurnStream } from "./history.js";

export type { TurnStream } from "./history.js";

/**
 * The dsh SessionId brand (`Branded<'SessionId'>` in dsh-session, a
 * transitive peer reached through `CreateAgentOptions`), so this package
 * never imports `@deepseek-ai/dsh-session` directly.
 */
type SessionId = CreateAgentOptions["sessionId"];

/** Adapter route registered by @dominion/dsh-llm-glm (contracts/glm-llm-plugin.md §2). */
export const PROVIDER = "glm-responses";

/**
 * Default model, aligned with the cordis.yml `models[]` catalog
 * (specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md §5).
 */
export const DEFAULT_MODEL = process.env.GLM_MODEL || "glm-5.3";

/**
 * Stable AIP error codes the gRPC layer maps onto statuses
 * (specs/051-agent-v2-dsh-migration/data-model.md §3).
 */
export type AgentSessionErrorCode = "INVALID_ARGUMENT" | "NOT_FOUND" | "FAILED_PRECONDITION";

/** A request-level session error carrying its stable AIP code. */
export class AgentSessionError extends Error {
  readonly code: AgentSessionErrorCode;

  constructor(code: AgentSessionErrorCode, message: string) {
    super(message);
    this.name = "AgentSessionError";
    this.code = code;
  }
}

/** The effective configuration an entry was materialized with. */
export interface MaterializedConfig {
  readonly preset: string;
  readonly model: string;
}

/** The Agent singleton resource projection served by GetAgent/UpdateAgent. */
export interface AgentView {
  readonly name: string;
  readonly preset: string;
  readonly model: string;
  readonly createTime: Date;
  readonly updateTime: Date;
}

/** The materialize request: preset reference and optional model. The
 * persona is NOT a materialization parameter — it lives in the preset's
 * persona row and reaches the agent through the roster mount (specs/
 * 059-agent-v2-team-mode/research.md R3). */
export interface MaterializeOptions {
  /** Full preset resource name (validated by the caller, server.ts). */
  readonly preset: string;
  /** Model id; empty/undefined = the process default. */
  readonly model?: string;
}

interface QueuedMessage {
  readonly text: string;
  readonly stream: TurnStream;
  /** Minted at enqueue time; the queued frame and this message's turn share it. */
  readonly turnId: string;
}

interface SessionEntry {
  readonly sessionName: string;
  readonly agent: Agent;
  readonly handle: AgentHandle;
  readonly collector: TurnCollector;
  readonly history: SessionHistory;
  readonly queue: QueuedMessage[];
  readonly config: MaterializedConfig;
  readonly createTime: Date;
  updateTime: Date;
  busy: boolean;
  /** Set by teardown; a turn started after it must abort immediately. */
  disposed: boolean;
}

function queuedEvent(sessionName: string, turnId: string, position: number): ChatEvent {
  const queued: QueuedEvent = { position };
  return { session: sessionName, turnId, queued };
}

function turnStartEvent(sessionName: string, turnId: string): ChatEvent {
  const turnStart: TurnStartEvent = {};
  return { session: sessionName, turnId, turnStart };
}

function usageEvent(usage: { inputTokens: number; outputTokens: number; reasoningTokens?: number } | undefined): TurnUsage | undefined {
  if (usage === undefined) {
    return undefined;
  }
  // int64 fields are materialized as strings under proto-loader longs:String.
  const mapped: TurnUsage = {
    inputTokens: String(usage.inputTokens),
    outputTokens: String(usage.outputTokens),
  };
  if (usage.reasoningTokens !== undefined) {
    mapped.reasoningTokens = String(usage.reasoningTokens);
  }
  return mapped;
}

function turnEndEvent(
  sessionName: string,
  turnId: string,
  outcome: TurnOutcome,
  usage: DshTokenUsage | undefined,
): ChatEvent {
  const end: TurnEndEvent = { status: `TURN_STATUS_${outcome.status}` };
  if (outcome.error !== undefined) {
    end.error = { code: outcome.error.code, message: outcome.error.message };
  }
  const mappedUsage = usageEvent(usage);
  if (mappedUsage !== undefined) {
    end.usage = mappedUsage;
  }
  return { session: sessionName, turnId, turnEnd: end };
}

/**
 * Owns the live session entries, their FIFO queues, turn serialization, and
 * the UpdateAgent materialization semantics.
 */
export class AgentSessions {
  private readonly sessions = new Map<string, SessionEntry>();
  /** Serializes re-materializations per session (data-model.md §2.2 concurrency rule). */
  private readonly materializations = new Map<string, Promise<unknown>>();

  constructor(private readonly ctx: DshContext) {}

  /**
   * Accept one user message for a MATERIALIZED session. Returns immediately:
   * when a turn is running the message is enqueued and its stream receives
   * queued{position} and stays open (specs/049-agent-v2-dsh-init/spec.md
   * FR-012); otherwise the turn starts.
   * Turn failures surface as turn_end{ERROR} frames — never as rejections —
   * so the process and session survive model endpoint failures.
   *
   * There is no lazy creation: an unmaterialized (or stale — e.g. post-
   * restart) session throws FAILED_PRECONDITION before any frame is written
   * (specs/051-agent-v2-dsh-migration/data-model.md §3;
   * specs/051-agent-v2-dsh-migration/spec.md FR-007).
   */
  send(session: string, text: string, stream: TurnStream): void {
    const entry = this.liveEntry(session);
    if (entry === undefined) {
      throw new AgentSessionError(
        "FAILED_PRECONDITION",
        `agent not materialized for session ${session}; send UpdateAgent first`,
      );
    }
    void this.enqueue(entry, text, stream);
  }

  /**
   * In-memory conversation history for refresh/reconnect backfill
   * (ListAgentMessages, agent-api.md §2.3;
   * specs/049-agent-v2-dsh-init/spec.md FR-014 semantics carried
   * over). Messages live with the materialized agent — an unmaterialized
   * session has none (NOT_FOUND, data-model.md §3).
   */
  async listMessages(session: string): Promise<HistoryMessage[]> {
    const entry = this.requireEntry(session, "NOT_FOUND");
    return entry.history.list();
  }

  /**
   * The session's agent singleton projection (GetAgent); NOT_FOUND while
   * unmaterialized (data-model.md §3, AIP-156:
   * https://google.aip.dev/156).
   */
  getAgent(session: string): AgentView {
    const entry = this.requireEntry(session, "NOT_FOUND");
    return toAgentView(entry);
  }

  /**
   * Cancel the session's in-flight turn and land queued messages (the
   * `:cancel` semantics, specs/054-agent-v2-bugfixes/contracts/
   * agent-api-changes.md §3; data-model.md §1.3): the model stream and
   * in-flight tools propagate cancellation, every affected stream receives
   * turn_end{CANCELED}, and the session immediately accepts a new Send.
   * Queued user messages stay in history — enqueue already appended them —
   * so clearing the queue lands them without triggering a turn (data-model.md
   * §3; the user ruling that departs from the official client's kept queue,
   * specs/054-agent-v2-bugfixes/research.md D2). Idempotent: with no
   * in-flight turn and an empty queue this is a successful no-op.
   * Unmaterialized sessions fail like Send (FAILED_PRECONDITION before any
   * frame).
   */
  cancel(session: string): void {
    const entry = this.liveEntry(session);
    if (entry === undefined) {
      throw new AgentSessionError(
        "FAILED_PRECONDITION",
        `agent not materialized for session ${session}; send UpdateAgent first`,
      );
    }
    this.cancelEntry(entry);
  }

  /**
   * Materialize (or refresh) the session's agent singleton — the UpdateAgent
   * semantics (data-model.md §2.2): any existing entry is torn down first
   * (in-flight turn receives turn_end{ABORTED}, queued messages dropped,
   * history/game state released with the agent) and a clean agent is created
   * with the given configuration — even when the configuration is unchanged
   * (refresh folded into Update). Validation of the preset/model happened in
   * the caller; this method cannot produce a half-materialized state.
   * Concurrent materializations of one session serialize; different sessions
   * never block each other.
   */
  async materialize(session: string, options: MaterializeOptions): Promise<AgentView> {
    const previous = this.materializations.get(session) ?? Promise.resolve();
    const job = previous.then(() => this.doMaterialize(session, options));
    this.materializations.set(
      session,
      job.then(
        () => undefined,
        () => undefined,
      ),
    );
    return job;
  }

  private async doMaterialize(session: string, options: MaterializeOptions): Promise<AgentView> {
    const existing = this.sessions.get(session);
    if (existing) {
      this.sessions.delete(session);
      this.teardownEntry(existing);
      try {
        await existing.handle.dispose();
      } catch (err) {
        error("agent session dispose failed", {
          session,
          error: err instanceof Error ? err.message : String(err),
        });
        throw err;
      }
    }

    const model = options.model || DEFAULT_MODEL;
    // The singleton's create_time survives re-materialization (AIP-134
    // output-only create_time); update_time refreshes on every UpdateAgent
    // (data-model.md §2.2).
    const createTime = existing?.createTime ?? new Date();
    const updateTime = new Date();
    // Compose resolves BEFORE the factory call so the resolved preset id is
    // snapshotted into the creation meta (`meta.agentPreset`, the official
    // composeAgent wiring shape) and an unresolvable/broken preset fails
    // before any session exists; the mount happens in the factory's `setup`
    // hook, where a rejection rolls the whole creation back — no
    // half-composed session (roster-verification §2.2). The same setup
    // registers the agent-scoped game runtime (`saoleiGame`), the
    // registration point moved here from the removed saolei-loop factory
    // (specs/059-agent-v2-team-mode/research.md R6).
    const authoring = this.ctx.get("presetAuthoring") as PresetAuthoringService;
    const presetId = options.preset.split("/").pop() ?? "";
    const composed = await authoring.compose(presetId);
    let handle: AgentHandle;
    try {
      handle = await this.ctx.agents.create({
        sessionId: session as SessionId,
        meta: { cwd: process.cwd(), agentPreset: composed.agentPreset },
        agentOptions: { provider: PROVIDER, model },
        setup: async (agentCtx) => {
          await composed.setup(agentCtx);
          createAgentGameRuntime(agentCtx.agent as Agent, this.ctx.desktopBridge);
        },
      });
    } catch (err) {
      error("agent materialization failed", {
        session,
        error: err instanceof Error ? err.message : String(err),
      });
      throw err;
    }
    const history = new SessionHistory();
    const collector = new TurnCollector(this.ctx, handle.agent, session, history);
    const entry: SessionEntry = {
      sessionName: session,
      agent: handle.agent,
      handle,
      collector,
      history,
      queue: [],
      config: { preset: options.preset, model },
      createTime,
      updateTime,
      busy: false,
      disposed: false,
    };
    this.sessions.set(session, entry);
    info("agent session materialized", {
      session,
      provider: PROVIDER,
      model,
      preset: composed.agentPreset,
      replaced: existing !== undefined,
    });
    return toAgentView(entry);
  }

  /**
   * Shutdown path only (the Dispose RPC face is gone —
   * specs/051-agent-v2-dsh-migration/spec.md FR-007): dispose
   * every session entry, then the composition's root fiber — the graceful
   * order (bootstrap: stop server → dispose agents → dispose fiber → mongo →
   * flush OTel).
   */
  async shutdown(): Promise<void> {
    const entries = [...this.sessions.values()];
    this.sessions.clear();
    for (const entry of entries) {
      this.teardownEntry(entry);
    }
    const results = await Promise.allSettled(
      entries.map((entry) => entry.handle.dispose()),
    );
    await this.ctx.fiber.dispose();
    const failure = results.find(
      (result): result is PromiseRejectedResult => result.status === "rejected",
    );
    if (failure) {
      throw failure.reason;
    }
  }

  private requireEntry(
    session: string,
    code: AgentSessionErrorCode,
  ): SessionEntry {
    const entry = this.liveEntry(session);
    if (entry === undefined) {
      throw new AgentSessionError(
        code,
        `agent not materialized for session ${session}; send UpdateAgent first`,
      );
    }
    return entry;
  }

  /**
   * The live entry for a session, or undefined while unmaterialized. A
   * retained record whose agent left the dsh registry (loop-level reload) is
   * stale: it is dropped here so every face answers uniformly as
   * unmaterialized.
   */
  private liveEntry(session: string): SessionEntry | undefined {
    const entry = this.sessions.get(session);
    if (entry === undefined) {
      return undefined;
    }
    if (this.ctx.agents.get(entry.agent.id) !== entry.agent) {
      this.sessions.delete(session);
      return undefined;
    }
    return entry;
  }

  /**
   * Deliver one terminal `turn_end{outcome}` frame to every stream the entry
   * still holds — the queued messages and (when one is running) the in-flight
   * turn — and close each after its final frame, then clear the queue. The
   * collector settles through {@link TurnCollector.abort} before the caller
   * stops the turn at its source, so the driver's idle convergence cannot
   * re-settle the slot as COMPLETED. Shared skeleton of the two teardown
   * shapes: the dispose path (teardownEntry, agent stop owned by the
   * caller's handle.dispose) and the user cancel path (cancelEntry, agent
   * stopped here via Agent.cancel). Returns whether a turn was in flight.
   */
  private drainEntry(entry: SessionEntry, outcome: TurnOutcome): boolean {
    for (const message of entry.queue) {
      message.stream.write(turnEndEvent(entry.sessionName, message.turnId, outcome, undefined));
      message.stream.end();
    }
    entry.queue.length = 0;

    const inFlight = entry.collector.abort(outcome);
    if (inFlight !== undefined) {
      inFlight.stream.write(turnEndEvent(entry.sessionName, inFlight.turnId, outcome, undefined));
      inFlight.stream.end();
      return true;
    }
    return false;
  }

  /**
   * Dispose path only (the Dispose RPC face is gone —
   * specs/051-agent-v2-dsh-migration/spec.md FR-007): mark the entry
   * disposed and settle every held stream with turn_end{ABORTED}. The agent
   * teardown itself belongs to the caller (materialize replaces, shutdown
   * releases).
   */
  private teardownEntry(entry: SessionEntry): void {
    entry.disposed = true;
    this.drainEntry(entry, { status: "ABORTED" });
    entry.collector.dispose();
  }

  /**
   * User cancel (specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md
   * §3): every held stream learns turn_end{CANCELED}, the queue lands
   * without triggering a turn, and the driver propagates cancellation to the
   * LLM stream and in-flight tools (the saolei-loop driver aborts the active
   * turn, settling in-flight desktop operations through its existing abort
   * semantics). Unlike teardown the entry stays live — runTurn drains the
   * (now empty) queue and resets busy, so the session accepts a new Send
   * immediately.
   */
  private cancelEntry(entry: SessionEntry): void {
    // Propagation only concerns an in-flight turn — with none, a dsh cancel
    // would be an inbox-clearing no-op touching a settled agent for nothing.
    // The collector already settled inside drainEntry, so the driver's
    // cancellation converging to idle cannot re-settle the slot COMPLETED.
    if (this.drainEntry(entry, { status: "CANCELED" })) {
      entry.agent.cancel({ kind: "user" });
    }
  }

  private async enqueue(entry: SessionEntry, text: string, stream: TurnStream): Promise<void> {
    const session = entry.sessionName;
    entry.history.appendUser(text);
    const message: QueuedMessage = { text, stream, turnId: mintTurnId() };
    if (entry.busy) {
      entry.queue.push(message);
      message.stream.write(queuedEvent(session, message.turnId, entry.queue.length));
      return;
    }
    entry.busy = true;
    await this.runTurn(entry, session, message);
  }

  /**
   * TurnRunner: run one turn to completion, then auto-run the queue head
   * (specs/049-agent-v2-dsh-init/spec.md FR-012). Serializes turns within
   * the session; distinct sessions run on
   * their own entries and never block each other.
   */
  private async runTurn(entry: SessionEntry, session: string, message: QueuedMessage): Promise<void> {
    const { collector } = entry;
    if (entry.disposed) {
      // teardown raced ahead of the turn start: the queue is already dropped
      // and the caller's stream gets the ABORTED frame here.
      message.stream.write(turnEndEvent(session, message.turnId, { status: "ABORTED" }, undefined));
      message.stream.end();
      return;
    }
    collector.begin(message.turnId, message.stream);
    // Contract §3-2: turn_start precedes every block_*/delta frame of the
    // turn (both the direct-start and the queued-turn-takes-over paths run
    // through here). specs/049-agent-v2-dsh-init/contracts/conversation-api.md
    message.stream.write(turnStartEvent(session, message.turnId));
    try {
      entry.agent.followup(
        createUserMessage({
          content: [{ type: "text", text: message.text }],
          source: { kind: "user" },
        }),
      );
      const settlement: TurnSettlement = await collector.awaitSettled();
      if (settlement.status === "ABORTED") {
        // Teardown delivered the turn_end{ABORTED} frame and the entry is
        // disposed — neither the queue drain nor the busy reset below
        // applies. The finally below still closes this stream.
        return;
      }
      if (settlement.status !== "CANCELED") {
        message.stream.write(turnEndEvent(session, message.turnId, settlement, settlement.usage));
      }
      // CANCELED: the cancel call delivered the turn_end{CANCELED} frame
      // itself. Unlike ABORTED the entry stays live, so the runner falls
      // through to the (cancel-cleared) queue drain and the busy reset —
      // the session accepts a new Send with no cooldown
      // (specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md §3).
    } catch (err) {
      // followup rejection (e.g. disposed agent): the request-level failure
      // becomes a turn_end{ERROR} frame — the process stays alive.
      error("agent turn failed", {
        session,
        error: err instanceof Error ? err.message : String(err),
      });
      message.stream.write(
        turnEndEvent(session, message.turnId, {
          status: "ERROR",
          error: { code: "TURN_FAILED", message: err instanceof Error ? err.message : String(err) },
        }, undefined),
      );
    } finally {
      // Every turn path converges here for the EOF: the terminal frame may
      // have been written above or delivered by the teardown (ABORTED), but
      // this is the only close that runs on every path. grpc-js
      // Writable.end() is idempotent, so overlapping with the teardown's own
      // end() is harmless (the server.ts end() adapter keeps its try/catch).
      message.stream.end();
    }

    const next = entry.queue.shift();
    if (next !== undefined) {
      await this.runTurn(entry, session, next);
      return;
    }
    entry.busy = false;
  }
}

function toAgentView(entry: SessionEntry): AgentView {
  return {
    name: `${entry.sessionName}/agent`,
    preset: entry.config.preset,
    model: entry.config.model,
    createTime: entry.createTime,
    updateTime: entry.updateTime,
  };
}
