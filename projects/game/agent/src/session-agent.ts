/**
 * session-agent.ts — Per-session agent adapter lifecycle management.
 *
 * Each SessionAgent owns exactly one AgentAdapter.  The adapter is created
 * on first bind and rebuilt only via Refresh (invalidateAdapter).  A bound
 * adapter is served cached for every turn; the profile-name guard
 * (specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §3)
 * ensures a mismatched turn never reaches it, so there is no implicit
 * per-turn switch.  The adapter factory receives a lazy getProvider callback
 * so test factories can skip provider creation entirely.  Old adapters are
 * dereferenced synchronously; their optional cleanup hook runs
 * asynchronously via setImmediate.
 */

import { info } from "@dominion/common-js-logs";
import { MemorySaver } from "@langchain/langgraph";

import { OperationBridge } from "./operation-bridge";
import type { ChatModel } from "./model-provider";
import type { AgentAdapter, AdapterFactory, TurnContent } from "./llm";
import { TurnLoop } from "./turn-loop";
import type { TurnLoopEmit } from "./turn-loop";

export interface ProfileData {
  model: string;
  systemPrompt: string;
  toolNames: string[];
  /**
   * MCP integrations enabled on the profile (spec 018-saolei-mcp FR-021).
   * Forwarded to the AdapterFactory so it can build MCP-client tools and
   * exclude raw mouse tools for saolei profiles (FR-012).
   */
  mcpNames: string[];
}

export type ProfileFetcher = () => Promise<ProfileData>;
export type ProviderLookupFn = (modelSpec: string) => Promise<ChatModel>;

// ---------------------------------------------------------------------------
// SessionAgent
// ---------------------------------------------------------------------------

export class SessionAgent {
  private adapter: AgentAdapter | null = null;
  private activeProfileName: string | null = null;
  private bindLock: Promise<void> = Promise.resolve();
  private readonly bridge: OperationBridge;
  private readonly sessionId: string;

  /**
   * Per-session TurnLoop — the LangGraph-native single-flight + queue owner
   * (`specs/030-queued-chat-input/contracts/turn-loop-contract.md`). Lazily
   * constructed on first {@link submit} so a session that never receives a
   * user-content frame pays no allocation. Conversation continuity across the
   * loop's auto-continued turns is provided by the {@link checkpointer}
   * forwarded to the adapter factory.
   *
   * The loop's `adapterProvider`/`emit` closures delegate to the swappable
   * {@link turnLoopProfileName}/{@link turnLoopFetcher}/{@link turnLoopEmit}
   * fields below: the loop instance is per-session (persistent, retains its
   * buffer across stream reconnects) while the emit sink + profile context are
   * per-stream (transient), installed on each {@link submit}.
   */
  private turnLoop: TurnLoop | null = null;
  private turnLoopProfileName: string | null = null;
  private turnLoopFetcher: ProfileFetcher | null = null;
  private turnLoopEmit: TurnLoopEmit | null = null;

  constructor(
    private readonly getProviderFn: ProviderLookupFn,
    private readonly adapterFactory: AdapterFactory,
    private readonly checkpointer: MemorySaver,
    sessionId: string,
  ) {
    this.bridge = new OperationBridge();
    this.sessionId = sessionId;
  }

  async getOrCreateAdapter(
    profileName: string,
    profileFetcher: ProfileFetcher,
  ): Promise<AgentAdapter> {
    if (this.adapter) {
      // Cached — serve. Profile match is ensured upstream by the turn-entry
      // guard; Refresh (invalidateAdapter) is the sole rebuild path
      // (specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §2).
      return this.adapter;
    }
    return this.serializeBind(profileName, profileFetcher);
  }

  getAdapter(): AgentAdapter | null {
    return this.adapter;
  }

  getAdapterState(): {
    activeProfileName: string | null;
    isBound: boolean;
  } {
    return {
      activeProfileName: this.activeProfileName,
      isBound: this.adapter !== null,
    };
  }

  /**
   * Drop the cached adapter so the next getOrCreateAdapter call rebuilds it
   * (e.g. after tool_names changed).  Caller MUST reject Refresh while a turn
   * is in-flight (the handler checks {@link isRunning}); invalidateAdapter
   * does not synchronize.
   */
  invalidateAdapter(): void {
    if (!this.adapter) {
      return;
    }
    const old = this.adapter;
    const oldProfile = this.activeProfileName;
    this.adapter = null;
    this.activeProfileName = null;
    setImmediate(() => {
      old.cleanup?.();
      info("adapter invalidated for refresh", { oldProfile });
    });
  }

  getBridge(): OperationBridge {
    return this.bridge;
  }

  /**
   * Route a user-content submission to the per-session {@link TurnLoop} (the
   * single-flight owner that replaces the per-frame mutex path). Installs the
   * per-stream `emit` sink and profile context, lazily constructing the loop
   * on first use (its `adapterProvider` reuses {@link getOrCreateAdapter} and
   * the shared `MemorySaver` checkpointer). Non-blocking: returns once the
   * content is started (IDLE) or buffered (RUNNING) — see
   * `specs/030-queued-chat-input/contracts/turn-loop-contract.md`.
   *
   * (`style/javascript.md` §Mock — dependency injection: the adapter provider
   * and emit sink are injected, so tests pass fakes rather than `vi.mock`.)
   */
  submit(
    content: TurnContent,
    profileName: string,
    profileFetcher: ProfileFetcher,
    emit: TurnLoopEmit,
  ): void {
    this.turnLoopProfileName = profileName;
    this.turnLoopFetcher = profileFetcher;
    this.turnLoopEmit = emit;
    if (!this.turnLoop) {
      this.turnLoop = new TurnLoop(
        this.sessionId,
        async () => {
          // Resolve the bound adapter using the currently-installed profile
          // context (stable per session — the profile-name guard upstream
          // ensures a mismatched turn never reaches the loop).
          const pn = this.turnLoopProfileName ?? profileName;
          const fetcher = this.turnLoopFetcher ?? profileFetcher;
          return this.getOrCreateAdapter(pn, fetcher);
        },
        (frame) => this.turnLoopEmit?.(frame),
        profileName,
      );
    }
    this.turnLoop.submit(content);
  }

  /**
   * True iff the per-session TurnLoop has a turn in flight or is draining
   * queued work. The single source for `deriveStatusSignal(isInFlight=…)`
   * (replaces the former per-frame mutex-held check).
   */
  isRunning(): boolean {
    return this.turnLoop?.isRunning() ?? false;
  }

  /**
   * Abort the in-flight turn and clear the queue (FR-011). Called by the
   * handler on stream end/error so the loop stops emitting to a dead peer.
   * No-op if no turn is in flight.
   */
  abort(): void {
    this.turnLoop?.abort();
  }

  private async serializeBind(
    profileName: string,
    profileFetcher: ProfileFetcher,
  ): Promise<AgentAdapter> {
    const prev = this.bindLock;
    let release!: () => void;
    this.bindLock = new Promise<void>((r) => {
      release = r;
    });
    await prev;

    try {
      // A concurrent bind may have populated the adapter while we waited on
      // the lock; return the cached instance rather than rebuilding. With
      // getOrCreateAdapter returning the cache before calling serializeBind
      // and the turn-entry guard blocking mismatched profiles, Refresh
      // (invalidateAdapter) is the sole rebuild path — there is no implicit
      // per-turn switch to clean up here.
      // (specs/021-agent-session-resync/data-model.md §4;
      // specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §2)
      if (this.adapter) {
        return this.adapter;
      }

      const profile = await profileFetcher();

      const adapter = await this.adapterFactory(
        () => this.getProviderFn(profile.model),
        profile.systemPrompt,
        profile.toolNames,
        this.bridge,
        this.checkpointer,
        profile.mcpNames,
        this.sessionId,
      );

      this.adapter = adapter;
      this.activeProfileName = profileName;

      info("adapter bound for session", {
        profileName,
        model: profile.model,
      });

      return adapter;
    } finally {
      release();
    }
  }
}

// ---------------------------------------------------------------------------
// SessionAgentStore
// ---------------------------------------------------------------------------

export class SessionAgentStore {
  private agents = new Map<string, SessionAgent>();

  constructor(
    private readonly getProviderFn: ProviderLookupFn,
    private readonly adapterFactory: AdapterFactory,
    private readonly checkpointer: MemorySaver,
  ) {}

  getOrCreate(sessionId: string): SessionAgent {
    let agent = this.agents.get(sessionId);
    if (!agent) {
      agent = new SessionAgent(
        this.getProviderFn,
        this.adapterFactory,
        this.checkpointer,
        sessionId,
      );
      this.agents.set(sessionId, agent);
    }
    return agent;
  }

  get(sessionId: string): SessionAgent | undefined {
    return this.agents.get(sessionId);
  }
}
