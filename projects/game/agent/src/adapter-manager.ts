/**
 * adapter-manager.ts — Manages SessionAgent state and AgentAdapter lifecycle
 * per session.
 *
 * Key behaviors:
 * - Adapters are created on-demand via getOrCreateAdapter().
 * - Adapters stay bound after WebSocket disconnect — only profile switching
 *   triggers unbind + recreate.
 * - A→B→A creates a NEW adapter for A each time (no caching of previous
 *   adapters).
 * - Per-session mutex serializes adapter creation/switching (same
 *   promise-chain pattern as handler.ts).
 *
 * TODO: Rename LLMAdapter → AgentAdapter once Task 4 refactors llm.ts.
 */

import { info, warn } from "@dominion/common-js-logs";
import { MemorySaver } from "@langchain/langgraph";

import type { LLMAdapter, LLMProvider } from "./llm";
import { RealLLMAdapter } from "./llm";

// ---------------------------------------------------------------------------
// PromptClient interface (minimal — only what AdapterManager needs)
// ---------------------------------------------------------------------------

interface PromptClientProfile {
  model: string;
  systemPrompt: string;
}

interface PromptClient {
  getProfile(profileName: string): Promise<PromptClientProfile>;
}

// ---------------------------------------------------------------------------
// Internal binding record
// ---------------------------------------------------------------------------

interface AdapterBinding {
  adapter: LLMAdapter;
  profileName: string;
}

// ---------------------------------------------------------------------------
// AdapterManager
// ---------------------------------------------------------------------------

export class AdapterManager {
  /** Session → adapter binding. */
  private bindings: Map<string, AdapterBinding>;

  /** Per-session FIFO mutexes (promise chains). */
  private mutexes: Map<string, Promise<void>>;

  /** Base URL for LLM provider proxy endpoint. */
  private baseUrl: string;

  /** Optional provider hint (openai / anthropic). */
  private provider?: LLMProvider;

  constructor(baseUrl: string, provider?: LLMProvider) {
    this.bindings = new Map();
    this.mutexes = new Map();
    this.baseUrl = baseUrl;
    this.provider = provider;
  }

  // -----------------------------------------------------------------------
  // Same-session mutex helpers (FIFO, non-reentrant — identical to handler.ts)
  // -----------------------------------------------------------------------

  private async acquireMutex(sessionId: string): Promise<void> {
    const prev = this.mutexes.get(sessionId) ?? Promise.resolve();
    let release!: () => void;
    const next = new Promise<void>((r) => {
      release = r;
    });
    this.mutexes.set(sessionId, prev.then(() => next));
    await prev;
    (this.mutexes as any)[`_release_${sessionId}`] = release;
  }

  private releaseMutex(sessionId: string): void {
    const release = (this.mutexes as any)[`_release_${sessionId}`];
    if (release) release();
  }

  // -----------------------------------------------------------------------
  // getOrCreateAdapter
  // -----------------------------------------------------------------------

  /**
   * Get or create the LLM adapter bound for a session.
   *
   * Rules:
   * - No adapter bound → fetch profile from `promptClient`, create new
   *   adapter, bind, return it.
   * - Adapter bound for SAME `profileName` → return existing adapter (reuse).
   * - Adapter bound for DIFFERENT `profileName` → synchronously unbind old
   *   adapter, create new adapter for new profile, bind, return new.
   * - A→B→A creates a NEW adapter for A each time (no caching).
   *
   * @param sessionId    Stable session identifier.
   * @param profileName  Agent profile name to bind.
   * @param promptClient Client for fetching agent profiles.
   * @param _checkpointer Shared in-memory checkpointer (reserved for future
   *                      AgentAdapter constructor; currently unused).
   * @returns The bound LLMAdapter for this session+profile.
   */
  async getOrCreateAdapter(
    sessionId: string,
    profileName: string,
    promptClient: PromptClient,
    _checkpointer: MemorySaver,
  ): Promise<LLMAdapter> {
    await this.acquireMutex(sessionId);
    try {
      const existing = this.bindings.get(sessionId);

      // Same profile already bound → reuse.
      if (existing && existing.profileName === profileName) {
        info("adapter reused for session", { sessionId, profileName });
        return existing.adapter;
      }

      // Different profile bound → unbind synchronously.
      if (existing && existing.profileName !== profileName) {
        warn("unbinding adapter for profile switch", {
          sessionId,
          oldProfile: existing.profileName,
          newProfile: profileName,
        });
        this.bindings.delete(sessionId);
        // Dereference old adapter so A→B→A creates a new instance.
      }

      // Create new adapter.
      const profile = await promptClient.getProfile(profileName);
      // TODO: Replace RealLLMAdapter with AgentAdapter once Task 4 lands.
      const adapter = new RealLLMAdapter(this.baseUrl, this.provider);

      this.bindings.set(sessionId, {
        adapter,
        profileName,
      });

      info("adapter created and bound for session", {
        sessionId,
        profileName,
        model: profile.model,
      });

      return adapter;
    } finally {
      this.releaseMutex(sessionId);
    }
  }

  // -----------------------------------------------------------------------
  // getAdapterState
  // -----------------------------------------------------------------------

  /**
   * Query the binding state for a session.
   *
   * @param sessionId Session identifier.
   * @returns The active profile name and binding status.
   */
  getAdapterState(
    sessionId: string,
  ): { activeProfileName: string | null; isBound: boolean } {
    const binding = this.bindings.get(sessionId);
    if (!binding) {
      return { activeProfileName: null, isBound: false };
    }
    return {
      activeProfileName: binding.profileName,
      isBound: true,
    };
  }
}
