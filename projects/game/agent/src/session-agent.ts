/**
 * session-agent.ts — Per-session agent adapter lifecycle management.
 *
 * Each SessionAgent owns exactly one AgentAdapter.  The adapter is created
 * on first bind (or recreated on profile switch).  The adapter factory
 * receives a lazy getProvider callback so test factories can skip provider
 * creation entirely.  Old adapters are dereferenced synchronously; their
 * optional cleanup hook runs asynchronously via setImmediate.
 */

import { info } from "@dominion/common-js-logs";
import { MemorySaver } from "@langchain/langgraph";

import { OperationBridge } from "./operation-bridge";
import type { ChatModel } from "./model-provider";
import type { AgentAdapter, AdapterFactory } from "./llm";

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
    if (this.adapter && this.activeProfileName === profileName) {
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
   * (e.g. after tool_names changed).  Caller MUST reject concurrent turns via
   * the per-session mutex; invalidateAdapter does not synchronize.
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
      if (this.adapter && this.activeProfileName === profileName) {
        return this.adapter;
      }

      if (this.adapter) {
        const old = this.adapter;
        const oldProfile = this.activeProfileName;
        this.adapter = null;
        this.activeProfileName = null;
        setImmediate(() => {
          old.cleanup?.();
          info("old adapter cleaned up", { oldProfile });
        });
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
