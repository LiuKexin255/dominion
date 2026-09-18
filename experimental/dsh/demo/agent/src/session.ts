/**
 * session.ts — conversation ↔ dsh agent session mapping for the chat demo.
 *
 * `AgentSessions` owns the get-or-create registry over `ctx.agents`
 * (conversation id → live agent, host-chosen SessionId per
 * specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §3), drives one
 * round per `send` via `agent.followup`, and settles the reply when the
 * agent returns to idle: the concatenated text blocks of the round's LAST
 * `assistant/message` event, or the empty string when none arrived
 * (specs/047-dsh-chat-demo/research.md D3/D5).
 *
 * The Context injected at construction is the dependency seam: unit tests
 * pass a mock `ctx` and drive the captured event listeners instead of
 * intercepting modules (style/javascript.md Mock convention).
 */

import type {
  Agent,
  AgentHandle,
  CreateAgentOptions,
} from "@deepseek-ai/dsh-agent";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { error, info } from "@dominion/common-js-logs";
import type { DshContext } from "./dsh.js";

/**
 * The dsh SessionId brand (`Branded<'SessionId'>` in dsh-session, a
 * transitive peer reached through `CreateAgentOptions`), so this package
 * never imports `@deepseek-ai/dsh-session` directly.
 */
type SessionId = CreateAgentOptions["sessionId"];

/** Official adapter route registered by `@deepseek-ai/dsh-llm-deepseek`. */
const PROVIDER = "deepseek-official";

/** Fake model id, aligned with the cordis.yml `models[]` catalog. */
const MODEL = "fake-chat-v1";

/** Structural subset of a dsh `session/event` payload read by the collector. */
interface RoundEvent {
  type: string;
  data?: unknown;
}

/** The `assistant/message` event shape the reply is extracted from. */
interface AssistantMessageEvent extends RoundEvent {
  type: "assistant/message";
  data: { message: { content: ReadonlyArray<{ type: string; text?: string }> } };
}

interface SessionEntry {
  readonly agent: Agent;
  readonly handle: AgentHandle;
  /** Tail of the per-session round serialization; never rejects. */
  chain: Promise<unknown>;
}

/**
 * Concatenated text of the round's last assistant message, or `''` when the
 * round produced none. Mirrors the official SDK client's `finalResponse`
 * (https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/client/src/api.ts,
 * specs/047-dsh-chat-demo/research.md D3).
 */
export function finalResponse(events: readonly RoundEvent[]): string {
  for (let index = events.length - 1; index >= 0; index--) {
    const event = events[index];
    if (event?.type !== "assistant/message") continue;
    const message = (event as AssistantMessageEvent).data.message;
    return message.content
      .filter((block) => block.type === "text")
      .map((block) => block.text ?? "")
      .join("");
  }
  return "";
}

/** Owns the live conversation agents and their round serialization. */
export class AgentSessions {
  private readonly sessions = new Map<string, SessionEntry>();
  /** Single-flight creation per conversation id (https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/server/src/server.ts). */
  private readonly creations = new Map<string, Promise<SessionEntry>>();

  constructor(private readonly ctx: DshContext) {}

  /**
   * Run one chat round on the conversation's agent and return its reply.
   *
   * Concurrent sends on the same conversation are serialized so each round's
   * event collection observes exactly its own turn; sends on distinct
   * conversations run independently (US2 scenario 3). A failed round rejects
   * but leaves the session registered — later sends on the same conversation
   * reuse it and can succeed again (fake-llm unreachable edge case: the
   * process stays alive and recovers).
   */
  async send(conversationId: string, text: string): Promise<string> {
    const entry = await this.getOrCreate(conversationId);
    const round = entry.chain.then(() =>
      this.runRound(entry.agent, conversationId, text),
    );
    entry.chain = round.then(
      () => undefined,
      () => undefined,
    );
    return round;
  }

  /**
   * Dispose every agent handle, then the composition's root fiber — the
    * contract's shutdown order (specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §1).
   */
  async shutdown(): Promise<void> {
    await Promise.allSettled([...this.creations.values()]);
    this.creations.clear();
    const entries = [...this.sessions.values()];
    this.sessions.clear();
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

  private async getOrCreate(conversationId: string): Promise<SessionEntry> {
    const existing = this.sessions.get(conversationId);
    if (existing) {
      // Staleness re-validation: a loop-level reload can dispose agents
      // while our record survives; a retained handle accepts followup()
      // silently, so re-check the live registry (https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/server/src/server.ts).
      if (this.ctx.agents.get(existing.agent.id) === existing.agent) {
        return existing;
      }
      this.sessions.delete(conversationId);
    }
    const pending = this.creations.get(conversationId);
    if (pending) return pending;
    const creation = this.createSession(conversationId);
    this.creations.set(conversationId, creation);
    void creation.then(
      () => this.creations.delete(conversationId),
      () => this.creations.delete(conversationId),
    );
    return creation;
  }

  private async createSession(conversationId: string): Promise<SessionEntry> {
    const handle = await this.ctx.agents.create({
      sessionId: conversationId as SessionId,
      meta: { cwd: process.cwd() },
      agentOptions: { provider: PROVIDER, model: MODEL },
    });
    const entry: SessionEntry = { agent: handle.agent, handle, chain: Promise.resolve() };
    this.sessions.set(conversationId, entry);
    info("agent session created", { conversationId, provider: PROVIDER, model: MODEL });
    return entry;
  }

  /**
   * One round: arm the collectors, deliver the followup, await the idle
   * transition, then extract the reply (or rethrow the round's failure).
   *
   * The listeners are armed BEFORE `followup` — the wake enters `running`
   * synchronously, so a later subscription could race the turn's opening
    * events (specs/047-dsh-chat-demo/research.md D3 rationale for the collection pattern).
   */
  private async runRound(
    agent: Agent,
    conversationId: string,
    text: string,
  ): Promise<string> {
    const events: RoundEvent[] = [];
    let failure: { error: unknown } | undefined;
    let settle!: () => void;
    const idle = new Promise<void>((resolve) => {
      settle = resolve;
    });

    const offEvent = this.ctx.on("session/event", (session, event) => {
      if (session.id !== agent.session.id) return;
      events.push(event as unknown as RoundEvent);
    });
    const offStatus = this.ctx.on("agent/status", (payload) => {
      if (payload.agent !== agent || payload.status !== "idle") return;
      settle();
    });
    const offError = this.ctx.on("agent/error", (payload) => {
      if (payload.agent !== agent) return;
      failure ??= { error: payload.error };
    });

    try {
      agent.followup(
        createUserMessage({
          content: [{ type: "text", text }],
          source: { kind: "user" },
        }),
      );
      await idle;
    } finally {
      offEvent();
      offStatus();
      offError();
    }

    if (failure !== undefined) {
      const err = failure.error;
      error("agent round failed", {
        conversationId,
        error: err instanceof Error ? err.message : String(err),
      });
      throw err instanceof Error ? err : new Error(String(err));
    }
    return finalResponse(events);
  }
}
