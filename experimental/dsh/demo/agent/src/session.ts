/**
 * session.ts — explicit conversation ↔ dsh agent session mapping for the
 * chat demo.
 *
 * `AgentSessions` owns the conversation registry over `ctx.agents`
 * (conversation id → live agent + resolved preset binding) and drives one
 * round per `send` via `agent.followup`, settling the reply when the agent
 * returns to idle: the concatenated text blocks of the round's LAST
 * `assistant/message` event, or the empty string when none arrived
 * (specs/047-dsh-chat-demo/research.md D3/D5).
 *
 * Conversations exist only through `create()` (specs/058-dsh-preset-roster-demo/
 * data-model.md §3): the preset composition resolves through
 * `ctx.presetAuthoring.compose()` BEFORE the agent factory call so the resolved
 * id is snapshotted into the creation meta (`meta.agentPreset`, the official
 * composeAgent wiring shape) and the mount happens in the factory's `setup`
 * hook, where a failure rolls the whole creation back. Preset selection is
 * mandatory (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md
 * §2/§3): `presetId` names a STORE preset, and an absent id is rejected
 * INVALID_ARGUMENT by `compose()`. Same id + same preset is
 * an idempotent no-op; same id + a different preset disposes the old agent and
 * rebuilds (R4 in specs/058-dsh-preset-roster-demo/research.md). `send()` on a
 * conversation that was never created throws `ConversationNotCreatedError` —
 * no lazy creation (FR-002).
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
import type { PresetAuthoringService } from "@dominion/dsh-preset-authoring";
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
  /** Resolved preset id the conversation is bound to (data-model.md §3). */
  readonly preset: string;
  readonly createTime: Date;
  /** Tail of the per-session round serialization; never rejects. */
  chain: Promise<unknown>;
}

/** The resource projection `create()` returns (chat-api.md §1.1). */
export interface ConversationView {
  name: string;
  preset: string;
  createTime: Date;
}

/**
 * Thrown by `send()` when the conversation was never created (or its agent
 * was disposed externally): the server maps this to gRPC
 * FAILED_PRECONDITION — "call CreateConversation first" (FR-002, chat-api.md
 * §1.2).
 */
export class ConversationNotCreatedError extends Error {
  readonly conversationId: string;

  constructor(conversationId: string) {
    super(`conversation ${conversationId} not created; call CreateConversation first`);
    this.name = "ConversationNotCreatedError";
    this.conversationId = conversationId;
  }
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

/** Owns the live conversation registry and their round serialization. */
export class AgentSessions {
  private readonly sessions = new Map<string, SessionEntry>();
  /** Serializes create/rebuild per conversation id. */
  private readonly creations = new Map<string, Promise<ConversationView>>();

  constructor(private readonly ctx: DshContext) {}

  /**
   * Create the conversation bound to `presetId`, a store preset id —
   * preset selection is mandatory under the 060 derivation semantics, so an
   * absent id reaches `compose(undefined)` and is rejected INVALID_ARGUMENT
   * (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §2/§3).
   *
   * Same conversation id with the same resolved preset returns the existing
   * view without any side effect (idempotent, R4); with a different preset the
   * old agent is disposed first — in-flight rounds fail through the
   * `agent/disposed` round hook — and the agent is rebuilt on the new preset.
   */
  async create(conversationId: string, presetId?: string): Promise<ConversationView> {
    // Serialize per conversation so a rebuild never races a pending create:
    // the later create wins, matching the create-or-update semantics of the
    // RPC face (chat-api.md §1.1, AIP-134 allow_missing).
    //
    // Known boundary: the single creations slot only chains the FIRST
    // pending create — with 3+ concurrent creates where a middle one
    // rebuilds, the tail creates can observe a registry already deleted by
    // the rebuild and compose overlapping agents, and the handle the map
    // overwrites would leak undisposed. Acceptable for this demo: one
    // process, low conversational concurrency, and a rebuild on the same
    // conversation id is a rare caller pattern; a full per-conversation
    // mutex would complicate the common path for no demo-reachable gain.
    const pending = this.creations.get(conversationId);
    if (pending) {
      await pending.catch(() => undefined);
    }
    const creation = this.doCreate(conversationId, presetId);
    this.creations.set(conversationId, creation);
    try {
      return await creation;
    } finally {
      if (this.creations.get(conversationId) === creation) {
        this.creations.delete(conversationId);
      }
    }
  }

  /**
   * Run one chat round on the conversation's agent and return its reply.
   *
   * The conversation must have been created explicitly — there is no lazy
   * creation (FR-002). Concurrent sends on the same conversation are
   * serialized so each round's event collection observes exactly its own
   * turn; sends on distinct conversations run independently. A failed round
   * rejects but leaves the session registered — later sends on the same
   * conversation reuse it and can succeed again (fake-llm unreachable edge
   * case: the process stays alive and recovers).
   */
  async send(conversationId: string, text: string): Promise<string> {
    const entry = this.liveEntry(conversationId);
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

  private async doCreate(
    conversationId: string,
    presetId?: string,
  ): Promise<ConversationView> {
    // Compose resolves BEFORE the factory call so the resolved id lands in
    // the creation meta (the session boundary snapshots meta before async
    // setup begins) and an unresolvable (no store record) preset fails before
    // any session exists (the composeAgent wiring shape; V3-3 fail-fast).
    const authoring = this.ctx.get("presetAuthoring") as PresetAuthoringService;
    const composed = await authoring.compose(presetId);

    const existing = this.sessions.get(conversationId);
    if (existing && existing.preset === composed.agentPreset) {
      // Staleness re-validation mirrors send(): a loop-level reload can
      // dispose the agent while our record survives. A stale same-preset
      // record falls through to the rebuild path below.
      if (this.ctx.agents.get(existing.agent.id) === existing.agent) {
        return this.viewOf(existing);
      }
    }
    if (existing) {
      // Different preset (or stale record): dispose the old agent — an
      // in-flight round rejects through its agent/disposed hook — then
      // rebuild on the new preset (R4).
      this.sessions.delete(conversationId);
      await existing.handle.dispose();
    }

    const handle = await this.ctx.agents.create({
      sessionId: conversationId as SessionId,
      meta: { cwd: process.cwd(), agentPreset: composed.agentPreset },
      agentOptions: { provider: PROVIDER, model: MODEL },
      setup: composed.setup,
    });
    const entry: SessionEntry = {
      agent: handle.agent,
      handle,
      preset: composed.agentPreset,
      createTime: new Date(),
      chain: Promise.resolve(),
    };
    this.sessions.set(conversationId, entry);
    info("agent conversation created", {
      conversationId,
      preset: composed.agentPreset,
      provider: PROVIDER,
      model: MODEL,
    });
    return this.viewOf(entry);
  }

  private viewOf(entry: SessionEntry): ConversationView {
    return {
      name: `conversations/${entry.agent.id}`,
      preset: entry.preset,
      createTime: entry.createTime,
    };
  }

  /**
   * The live entry for a conversation, or a `ConversationNotCreatedError`.
   * A record whose agent left the live registry (loop-level reload) counts
   * as not created: the preset binding cannot be re-derived lazily, so the
   * caller must CreateConversation again.
   */
  private liveEntry(conversationId: string): SessionEntry {
    const entry = this.sessions.get(conversationId);
    if (!entry) {
      throw new ConversationNotCreatedError(conversationId);
    }
    if (this.ctx.agents.get(entry.agent.id) !== entry.agent) {
      this.sessions.delete(conversationId);
      throw new ConversationNotCreatedError(conversationId);
    }
    return entry;
  }

  /**
   * One round: arm the collectors, deliver the followup, await the idle
   * transition, then extract the reply (or rethrow the round's failure).
   *
   * The listeners are armed BEFORE `followup` — the wake enters `running`
   * synchronously, so a later subscription could race the turn's opening
   * events (specs/047-dsh-chat-demo/research.md D3 rationale for the collection pattern).
   * The `agent/disposed` hook is what makes a mid-round rebuild fail the
   * round deterministically instead of hanging on the idle await (R4).
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
    const offDisposed = this.ctx.on("agent/disposed", (payload) => {
      if (payload.agent !== agent) return;
      failure ??= {
        error: new Error(`agent for conversation ${conversationId} was disposed mid-round`),
      };
      settle();
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
      offDisposed();
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
