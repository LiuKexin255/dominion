/**
 * session.ts — game session ↔ dsh agent mapping for agent_v2.
 *
 * `AgentSessions` owns the get-or-create registry over `ctx.agents`
 * (session resource name → live dsh agent, host-chosen SessionId per
 * specs/049-agent-v2-dsh-init/data-model.md §2.2), a per-session FIFO queue
 * with a TurnRunner (mid-turn sends enqueue with a queued{position} frame
 * and auto-run at turn end, FR-012), and immediate-release dispose
 * (in-flight turn aborted, queued messages dropped, history dropped,
 * FR-015). Sessions are independent: each entry drives its own turns, and
 * concurrent sessions never block each other.
 *
 * The Context injected at construction is the dependency seam: unit tests
 * pass a mock `ctx` and drive the captured event listeners instead of
 * intercepting modules (style/javascript.md Mock convention).
 */

import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { error, info } from "@dominion/common-js-logs";
import type { Agent, AgentHandle, CreateAgentOptions } from "@deepseek-ai/dsh-agent";
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
const PROVIDER = "glm-responses";

/** Model id, aligned with the cordis.yml `models[]` catalog (data-model.md §2.6). */
const MODEL = process.env.GLM_MODEL || "glm-5.2";

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
  busy: boolean;
  /** Set by dispose; a turn started after it must abort immediately. */
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
 * Owns the live session entries, their FIFO queues, and turn serialization.
 */
export class AgentSessions {
  private readonly sessions = new Map<string, SessionEntry>();
  /** Single-flight creation per session resource name (official server pattern, specs/047-dsh-chat-demo/research.md D5). */
  private readonly creations = new Map<string, Promise<SessionEntry>>();

  constructor(private readonly ctx: DshContext) {}

  /**
   * Accept one user message for the session. Returns immediately: when a
   * turn is running the message is enqueued and its stream receives
   * queued{position} and stays open (FR-012); otherwise the turn starts.
   * Turn failures surface as turn_end{ERROR} frames — never as rejections —
   * so the process and session survive model endpoint failures.
   */
  send(session: string, text: string, stream: TurnStream): void {
    void this.enqueue(session, text, stream);
  }

  /**
   * In-memory conversation history for refresh/reconnect backfill
   * (ListAgentMessages, agent-api.md §2.3; 049 FR-014 semantics carried
   * over). Absent sessions get-or-create a fresh entry (data-model.md
   * §2.2), so a disposed session queried again yields an empty, brand-new
   * conversation.
   */
  async listMessages(session: string): Promise<HistoryMessage[]> {
    const entry = await this.getOrCreate(session);
    return entry.history.list();
  }

  /**
   * Release the session's dsh resources immediately (FR-015): the in-flight
   * turn (if any) and every queued message each receive one
   * turn_end{ABORTED} frame before their streams close, and the history is
   * dropped. Idempotent: an absent session is already released.
   */
  async dispose(session: string): Promise<void> {
    const entry = this.sessions.get(session);
    if (!entry) {
      return;
    }
    this.sessions.delete(session);
    this.abortEntry(entry);
    entry.collector.dispose();

    try {
      await entry.handle.dispose();
      info("agent session disposed", { session });
    } catch (err) {
      error("agent session dispose failed", {
        session,
        error: err instanceof Error ? err.message : String(err),
      });
      throw err;
    }
  }

  /**
   * Deliver turn_end{ABORTED} to every stream the entry still holds — the
   * queued messages and (when one is running) the in-flight turn — and close
   * each after its final frame. Shared by dispose and shutdown.
   */
  private abortEntry(entry: SessionEntry): void {
    entry.disposed = true;

    for (const message of entry.queue) {
      message.stream.write(turnEndEvent(entry.sessionName, message.turnId, { status: "ABORTED" }, undefined));
      message.stream.end();
    }
    entry.queue.length = 0;

    const inFlight = entry.collector.abort();
    if (inFlight !== undefined) {
      inFlight.stream.write(turnEndEvent(entry.sessionName, inFlight.turnId, { status: "ABORTED" }, undefined));
      inFlight.stream.end();
    }
  }

  /**
   * Dispose every session entry, then the composition's root fiber — the
   * shutdown order of the graceful chain (bootstrap: stop server → dispose
   * sessions → dispose fiber → flush OTel).
   */
  async shutdown(): Promise<void> {
    await Promise.allSettled([...this.creations.values()]);
    this.creations.clear();
    const entries = [...this.sessions.values()];
    this.sessions.clear();
    for (const entry of entries) {
      this.abortEntry(entry);
      entry.collector.dispose();
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

  private async enqueue(session: string, text: string, stream: TurnStream): Promise<void> {
    let entry: SessionEntry;
    try {
      entry = await this.getOrCreate(session);
    } catch (err) {
      error("agent session creation failed", {
        session,
        error: err instanceof Error ? err.message : String(err),
      });
      stream.write(
        turnEndEvent(session, mintTurnId(), {
          status: "ERROR",
          error: { code: "SESSION_CREATE", message: err instanceof Error ? err.message : String(err) },
        }, undefined),
      );
      stream.end();
      return;
    }

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
   * (FR-012). Serializes turns within the session; distinct sessions run on
   * their own entries and never block each other.
   */
  private async runTurn(entry: SessionEntry, session: string, message: QueuedMessage): Promise<void> {
    const { collector } = entry;
    if (entry.disposed) {
      // dispose raced ahead of the turn start: the queue is already dropped
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
        // dispose delivered the turn_end{ABORTED} frame and closed the
        // stream; nothing left to write on this path.
        return;
      }
      message.stream.write(turnEndEvent(session, message.turnId, settlement, settlement.usage));
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
      if (!entry.disposed) {
        message.stream.end();
      }
    }

    const next = entry.queue.shift();
    if (next !== undefined) {
      await this.runTurn(entry, session, next);
      return;
    }
    entry.busy = false;
  }

  private async getOrCreate(session: string): Promise<SessionEntry> {
    const existing = this.sessions.get(session);
    if (existing) {
      // Staleness re-validation (official server pattern): a loop-level
      // reload can dispose agents while our record survives; a retained
      // handle accepts followup() silently, so re-check the live registry.
      if (this.ctx.agents.get(existing.agent.id) === existing.agent) {
        return existing;
      }
      this.sessions.delete(session);
    }
    const pending = this.creations.get(session);
    if (pending) return pending;
    const creation = this.createSession(session);
    this.creations.set(session, creation);
    void creation.then(
      () => this.creations.delete(session),
      () => this.creations.delete(session),
    );
    return creation;
  }

  private async createSession(session: string): Promise<SessionEntry> {
    const handle = await this.ctx.agents.create({
      sessionId: session as SessionId,
      agentOptions: { provider: PROVIDER, model: MODEL },
    });
    const history = new SessionHistory();
    const collector = new TurnCollector(this.ctx, handle.agent, session, history);
    const entry: SessionEntry = {
      sessionName: session,
      agent: handle.agent,
      handle,
      collector,
      history,
      queue: [],
      busy: false,
      disposed: false,
    };
    this.sessions.set(session, entry);
    info("agent session created", { session, provider: PROVIDER, model: MODEL });
    return entry;
  }
}
