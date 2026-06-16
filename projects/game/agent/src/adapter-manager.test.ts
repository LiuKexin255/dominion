/**
 * adapter-manager.test.ts — Tests for AdapterManager lifecycle.
 *
 * Covers: on-demand creation, same-profile reuse, profile switching,
 * A→B→A new-instance guarantee, getAdapterState, and reconnect reuse.
 */

import { MemorySaver } from "@langchain/langgraph";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AdapterManager } from "./adapter-manager";

// ---------------------------------------------------------------------------
// Mock PromptClient
// ---------------------------------------------------------------------------

function createMockPromptClient(
  profiles: Record<string, { model: string; systemPrompt: string }>,
) {
  return {
    getProfile: vi.fn(async (name: string) => {
      const profile = profiles[name];
      if (!profile) {
        throw new Error(`Profile not found: ${name}`);
      }
      return profile;
    }),
  };
}

const MOCK_PROFILES = {
  alice: { model: "opencode-go/gpt-4o", systemPrompt: "You are Alice." },
  bob: { model: "opencode-go/minimax-m1", systemPrompt: "You are Bob." },
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("AdapterManager", () => {
  const baseUrl = "http://localhost:8080/v1";
  let manager: AdapterManager;
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let checkpointer: MemorySaver;

  beforeEach(() => {
    manager = new AdapterManager(baseUrl);
    promptClient = createMockPromptClient(MOCK_PROFILES);
    checkpointer = new MemorySaver();
  });

  // -----------------------------------------------------------------------
  // On-demand creation
  // -----------------------------------------------------------------------

  it("creates adapter on first getOrCreateAdapter and updates state", async () => {
    const sessionId = "session-1";

    // Initial state: nothing bound.
    expect(manager.getAdapterState(sessionId)).toEqual({
      activeProfileName: null,
      isBound: false,
    });

    const adapter = await manager.getOrCreateAdapter(
      sessionId,
      "alice",
      promptClient,
      checkpointer,
    );

    expect(adapter).toBeDefined();
    expect(promptClient.getProfile).toHaveBeenCalledWith("alice");
    expect(promptClient.getProfile).toHaveBeenCalledTimes(1);

    // State should reflect the binding.
    expect(manager.getAdapterState(sessionId)).toEqual({
      activeProfileName: "alice",
      isBound: true,
    });
  });

  // -----------------------------------------------------------------------
  // Same-profile reuse
  // -----------------------------------------------------------------------

  it("reuses same adapter instance for identical profile", async () => {
    const sessionId = "session-2";

    const first = await manager.getOrCreateAdapter(
      sessionId,
      "alice",
      promptClient,
      checkpointer,
    );

    const second = await manager.getOrCreateAdapter(
      sessionId,
      "alice",
      promptClient,
      checkpointer,
    );

    // Same instance reused — no extra profile fetch.
    expect(second).toBe(first);
    expect(promptClient.getProfile).toHaveBeenCalledTimes(1);

    expect(manager.getAdapterState(sessionId)).toEqual({
      activeProfileName: "alice",
      isBound: true,
    });
  });

  // -----------------------------------------------------------------------
  // Profile switching
  // -----------------------------------------------------------------------

  it("unbinds old adapter and creates new one on profile switch", async () => {
    const sessionId = "session-3";

    const aliceAdapter = await manager.getOrCreateAdapter(
      sessionId,
      "alice",
      promptClient,
      checkpointer,
    );

    const bobAdapter = await manager.getOrCreateAdapter(
      sessionId,
      "bob",
      promptClient,
      checkpointer,
    );

    // Different instances.
    expect(bobAdapter).not.toBe(aliceAdapter);

    // Profile fetch called for both.
    expect(promptClient.getProfile).toHaveBeenCalledTimes(2);

    // State now shows bob.
    expect(manager.getAdapterState(sessionId)).toEqual({
      activeProfileName: "bob",
      isBound: true,
    });
  });

  // -----------------------------------------------------------------------
  // A→B→A creates new adapter each switch
  // -----------------------------------------------------------------------

  it("A→B→A creates new adapter for A each time (no caching)", async () => {
    const sessionId = "session-4";

    // First A.
    const aliceFirst = await manager.getOrCreateAdapter(
      sessionId,
      "alice",
      promptClient,
      checkpointer,
    );

    // Switch to B.
    await manager.getOrCreateAdapter(
      sessionId,
      "bob",
      promptClient,
      checkpointer,
    );

    // Switch back to A.
    const aliceSecond = await manager.getOrCreateAdapter(
      sessionId,
      "alice",
      promptClient,
      checkpointer,
    );

    // Second A is a new instance — NOT the same as first A.
    expect(aliceSecond).not.toBe(aliceFirst);

    // Profile fetched 3 times: alice, bob, alice again.
    expect(promptClient.getProfile).toHaveBeenCalledTimes(3);
    expect(promptClient.getProfile).toHaveBeenNthCalledWith(1, "alice");
    expect(promptClient.getProfile).toHaveBeenNthCalledWith(2, "bob");
    expect(promptClient.getProfile).toHaveBeenNthCalledWith(3, "alice");

    // State bound to alice.
    expect(manager.getAdapterState(sessionId)).toEqual({
      activeProfileName: "alice",
      isBound: true,
    });
  });

  // -----------------------------------------------------------------------
  // Reconnect reuses bound adapter
  // -----------------------------------------------------------------------

  it("reuses bound adapter on reconnect (simulated disconnect)", async () => {
    const sessionId = "session-5";

    // First connect: bind alice.
    const aliceAdapter = await manager.getOrCreateAdapter(
      sessionId,
      "alice",
      promptClient,
      checkpointer,
    );

    expect(manager.getAdapterState(sessionId)).toEqual({
      activeProfileName: "alice",
      isBound: true,
    });

    // Simulate disconnect: do NOT unbind. The adapter stays bound.

    // Reconnect with same profile: should return the SAME adapter.
    const reconnected = await manager.getOrCreateAdapter(
      sessionId,
      "alice",
      promptClient,
      checkpointer,
    );

    expect(reconnected).toBe(aliceAdapter);
    expect(promptClient.getProfile).toHaveBeenCalledTimes(1); // Only first connect fetched profile.

    expect(manager.getAdapterState(sessionId)).toEqual({
      activeProfileName: "alice",
      isBound: true,
    });
  });

  // -----------------------------------------------------------------------
  // getAdapterState for never-connected session
  // -----------------------------------------------------------------------

  it("returns null/unbound for never-connected session", () => {
    expect(manager.getAdapterState("nonexistent")).toEqual({
      activeProfileName: null,
      isBound: false,
    });
  });

  // -----------------------------------------------------------------------
  // Multiple independent sessions
  // -----------------------------------------------------------------------

  it("manages multiple sessions independently", async () => {
    const s1 = "session-a";
    const s2 = "session-b";

    const a1 = await manager.getOrCreateAdapter(
      s1,
      "alice",
      promptClient,
      checkpointer,
    );
    const b1 = await manager.getOrCreateAdapter(
      s2,
      "bob",
      promptClient,
      checkpointer,
    );

    // Different sessions, different adapters.
    expect(a1).not.toBe(b1);

    expect(manager.getAdapterState(s1)).toEqual({
      activeProfileName: "alice",
      isBound: true,
    });
    expect(manager.getAdapterState(s2)).toEqual({
      activeProfileName: "bob",
      isBound: true,
    });

    // Reuse within same session.
    const a1Again = await manager.getOrCreateAdapter(
      s1,
      "alice",
      promptClient,
      checkpointer,
    );
    expect(a1Again).toBe(a1);

    // Switch s1 to bob.
    const a1Bob = await manager.getOrCreateAdapter(
      s1,
      "bob",
      promptClient,
      checkpointer,
    );
    expect(a1Bob).not.toBe(a1);
    expect(manager.getAdapterState(s1)).toEqual({
      activeProfileName: "bob",
      isBound: true,
    });

    // s2 still has bob.
    const b1Again = await manager.getOrCreateAdapter(
      s2,
      "bob",
      promptClient,
      checkpointer,
    );
    expect(b1Again).toBe(b1);
  });
});
