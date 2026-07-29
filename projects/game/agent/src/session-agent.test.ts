/**
 * session-agent.test.ts — Tests for SessionAgent and SessionAgentStore.
 *
 * Covers: on-demand adapter creation, same-profile reuse, cached-adapter
 * behaviour under a differing profile (no implicit switch), rebuild after
 * Refresh (invalidateAdapter), getAdapterState, and SessionAgentStore
 * per-session isolation
 * (specs/021-agent-session-resync/quickstart.md Scenario 4;
 * specs/021-agent-session-resync/data-model.md §4).
 */

import { MemorySaver } from "@langchain/langgraph";
import { describe, expect, it, vi } from "vitest";

import type { ChatModel } from "./model-provider";
import { SessionAgent, SessionAgentStore } from "./session-agent";
import type { ProfileData, ProfileFetcher } from "./session-agent";
import type { AgentAdapter, AdapterFactory, ContentBlock, TurnContent } from "./llm";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";

function createMockAdapter(blocks: ContentBlock[] = []): AgentAdapter {
  return {
    async *generateTurn(): AsyncIterable<ContentBlock> {
      for (const b of blocks) yield b;
    },
    getState: vi.fn(async () => null),
    cleanup: vi.fn(),
  };
}

function createMockAdapterFactory(): {
  factory: AdapterFactory;
  created: AgentAdapter[];
} {
  const created: AgentAdapter[] = [];
  const factory: AdapterFactory = async (
    _getProvider,
    _systemPrompt,
    _toolNames,
    _bridge,
    _checkpointer,
    _mcpNames,
    _sessionId,
  ) => {
    const adapter = createMockAdapter([
      { type: "text", text: `adapter-${created.length}` },
    ]);
    created.push(adapter);
    return adapter;
  };
  return { factory, created };
}

const PROFILES: Record<string, ProfileData> = {
  alice: { model: "gpt-4o", systemPrompt: "You are Alice.", toolNames: [], mcpNames: [] },
  bob: { model: "minimax-m1", systemPrompt: "You are Bob.", toolNames: [], mcpNames: [] },
};

function profileFetcherFor(
  name: string,
  profiles: Record<string, ProfileData> = PROFILES,
): ProfileFetcher {
  return async () => {
    const p = profiles[name];
    if (!p) throw new Error(`Profile not found: ${name}`);
    return p;
  };
}

const throwProvider = async (_modelSpec: string): Promise<ChatModel> => {
  throw new Error("not used");
};

describe("SessionAgent", () => {
  it("creates adapter on first getOrCreateAdapter", async () => {
    const { factory, created } = createMockAdapterFactory();
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver(), "sid-test");

    expect(agent.getAdapterState()).toEqual({
      activeProfileName: null,
      isBound: false,
    });

    const fetcher = vi.fn(profileFetcherFor("alice"));
    const adapter = await agent.getOrCreateAdapter("alice", fetcher);

    expect(created).toHaveLength(1);
    expect(adapter).toBe(created[0]);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(agent.getAdapterState()).toEqual({
      activeProfileName: "alice",
      isBound: true,
    });
  });

  it("reuses same adapter for identical profile (fast path)", async () => {
    const { factory, created } = createMockAdapterFactory();
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver(), "sid-test");

    const first = await agent.getOrCreateAdapter("alice", profileFetcherFor("alice"));
    const second = await agent.getOrCreateAdapter("alice", profileFetcherFor("alice"));

    expect(second).toBe(first);
    expect(created).toHaveLength(1);
  });

  it("returns cached adapter for a differing profile (no implicit switch)", async () => {
    const { factory, created } = createMockAdapterFactory();
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver(), "sid-test");

    const aliceAdapter = await agent.getOrCreateAdapter("alice", profileFetcherFor("alice"));
    // A differing profile name MUST NOT rebuild — the cached adapter is
    // served as-is (Refresh is the sole rebuild path; the turn-entry guard
    // ensures a bound adapter is never asked to serve a mismatched profile).
    const fetcherBob = vi.fn(profileFetcherFor("bob"));
    const bobResult = await agent.getOrCreateAdapter("bob", fetcherBob);

    expect(bobResult).toBe(aliceAdapter);
    expect(created).toHaveLength(1);
    // The differing-profile fetcher is never exercised (no rebuild).
    expect(fetcherBob).not.toHaveBeenCalled();
    expect(agent.getAdapterState().activeProfileName).toBe("alice");
  });

  it("builds a new adapter after invalidateAdapter (Refresh) and cleans up old", async () => {
    const { factory, created } = createMockAdapterFactory();
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver(), "sid-test");

    const aliceAdapter = await agent.getOrCreateAdapter("alice", profileFetcherFor("alice"));
    expect(agent.getAdapterState()).toEqual({
      activeProfileName: "alice",
      isBound: true,
    });

    agent.invalidateAdapter();
    expect(agent.getAdapterState()).toEqual({
      activeProfileName: null,
      isBound: false,
    });

    // Post-Refresh, the next getOrCreateAdapter rebuilds for the new profile.
    const bobAdapter = await agent.getOrCreateAdapter("bob", profileFetcherFor("bob"));

    expect(bobAdapter).not.toBe(aliceAdapter);
    expect(created).toHaveLength(2);
    expect(agent.getAdapterState().activeProfileName).toBe("bob");

    // invalidateAdapter schedules async cleanup of the dropped adapter.
    await new Promise((r) => setImmediate(r));
    expect(aliceAdapter.cleanup).toHaveBeenCalled();
  });

  it("returns null adapter and unbound state before first bind", () => {
    const { factory } = createMockAdapterFactory();
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver(), "sid-test");

    expect(agent.getAdapter()).toBeNull();
    expect(agent.getAdapterState()).toEqual({
      activeProfileName: null,
      isBound: false,
    });
  });

  it("serializes concurrent binds for same agent", async () => {
    const { factory, created } = createMockAdapterFactory();
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver(), "sid-test");

    const [a1, a2] = await Promise.all([
      agent.getOrCreateAdapter("alice", profileFetcherFor("alice")),
      agent.getOrCreateAdapter("alice", profileFetcherFor("alice")),
    ]);

    expect(a1).toBe(a2);
    expect(created).toHaveLength(1);
  });
});

// ===========================================================================
// TurnLoop ownership (spec 030 — Phase 2 T003)
// ===========================================================================

/** An adapter whose turn blocks on a gate before yielding (in-flight turn). */
function makeGatedAdapter(gate: {
  promise: Promise<void>;
  resolve: () => void;
}): AgentAdapter {
  return {
    async *generateTurn(
      _threadId: string,
      content: TurnContent,
      signal?: AbortSignal,
    ): AsyncIterable<ContentBlock> {
      // Abort-aware wait so abort() unblocks the in-flight turn.
      const abort = new Promise<never>((_, reject) => {
        if (signal?.aborted) reject(new Error("aborted"));
        signal?.addEventListener("abort", () => reject(new Error("aborted")), {
          once: true,
        });
      });
      await Promise.race([gate.promise, abort]).catch(() => undefined);
      if (signal?.aborted) return;
      yield { type: "text", text: `reply:${content.text ?? ""}` };
    },
    async getState() {
      return null;
    },
  };
}

function makeGate(): { promise: Promise<void>; resolve: () => void } {
  let resolve!: () => void;
  const promise = new Promise<void>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function waitFrameCount(frames: AgentFrame[]): number {
  return frames.filter((f) => {
    const fr = f as Record<string, unknown>;
    return (
      fr.payload === "flowParts" &&
      (fr.flowParts as { parts: Record<string, unknown>[] } | undefined)?.parts?.some(
        (p) => "wait" in p,
      ) === true
    );
  }).length;
}

describe("SessionAgent TurnLoop ownership", () => {
  it("lazily constructs the TurnLoop on first submit and drives a turn", async () => {
    const { factory } = createMockAdapterFactory();
    const agent = new SessionAgent(
      throwProvider,
      factory,
      new MemorySaver(),
      "sid-loop",
    );

    // Before any submission there is no loop → not running.
    expect(agent.isRunning()).toBe(false);

    const emitted: AgentFrame[] = [];
    const fetcher = profileFetcherFor("alice");
    agent.submit(
      { text: "hello" },
      "alice",
      fetcher,
      (f) => emitted.push(f),
    );

    // Loop started synchronously.
    expect(agent.isRunning()).toBe(true);

    // Let the (synchronous-yield) mock turn complete + emit wait.
    await new Promise((r) => setTimeout(r, 10));

    expect(agent.isRunning()).toBe(false);
    expect(waitFrameCount(emitted)).toBe(1);
  });

  it("isRunning reflects the loop state across submit/abort", async () => {
    const gate = makeGate();
    const gatedAdapter = makeGatedAdapter(gate);
    const factory: AdapterFactory = async () => gatedAdapter;
    const agent = new SessionAgent(
      throwProvider,
      factory,
      new MemorySaver(),
      "sid-abort",
    );

    const emitted: AgentFrame[] = [];
    agent.submit({ text: "msg-1" }, "alice", profileFetcherFor("alice"), (f) =>
      emitted.push(f),
    );
    await new Promise((r) => setTimeout(r, 10));

    // Turn is in flight (blocked on gate).
    expect(agent.isRunning()).toBe(true);

    // Abort delegates to the loop → clears queue, emits wait, → IDLE.
    agent.abort();
    await new Promise((r) => setTimeout(r, 20));

    expect(agent.isRunning()).toBe(false);
    expect(waitFrameCount(emitted)).toBe(1);
  });

  it("queues a second submission while running (no second concurrent turn)", async () => {
    const gate = makeGate();
    const gatedAdapter = makeGatedAdapter(gate);
    const factory: AdapterFactory = async () => gatedAdapter;
    const agent = new SessionAgent(
      throwProvider,
      factory,
      new MemorySaver(),
      "sid-queue",
    );

    const emitted: AgentFrame[] = [];
    agent.submit({ text: "msg-1" }, "alice", profileFetcherFor("alice"), (f) =>
      emitted.push(f),
    );
    await new Promise((r) => setTimeout(r, 10));

    // Second submission while the first turn is in flight is buffered, not
    // run concurrently.
    agent.submit({ text: "msg-2" }, "alice", profileFetcherFor("alice"), (f) =>
      emitted.push(f),
    );
    expect(agent.isRunning()).toBe(true);

    gate.resolve();
    await new Promise((r) => setTimeout(r, 20));

    // Exactly one terminal wait (after both turns drain).
    expect(agent.isRunning()).toBe(false);
    expect(waitFrameCount(emitted)).toBe(1);
  });
});

describe("SessionAgentStore", () => {
  it("returns same SessionAgent for same sessionId", () => {
    const { factory } = createMockAdapterFactory();
    const store = new SessionAgentStore(throwProvider, factory, new MemorySaver());

    const a1 = store.getOrCreate("s1");
    const a2 = store.getOrCreate("s1");

    expect(a1).toBe(a2);
  });

  it("returns different SessionAgents for different sessionIds", () => {
    const { factory } = createMockAdapterFactory();
    const store = new SessionAgentStore(throwProvider, factory, new MemorySaver());

    const a1 = store.getOrCreate("s1");
    const a2 = store.getOrCreate("s2");

    expect(a1).not.toBe(a2);
  });

  it("manages independent adapters across sessions", async () => {
    const { factory, created } = createMockAdapterFactory();
    const store = new SessionAgentStore(throwProvider, factory, new MemorySaver());

    const sa1 = store.getOrCreate("s1");
    const sa2 = store.getOrCreate("s2");

    await sa1.getOrCreateAdapter("alice", profileFetcherFor("alice"));
    await sa2.getOrCreateAdapter("bob", profileFetcherFor("bob"));

    expect(sa1.getAdapterState().activeProfileName).toBe("alice");
    expect(sa2.getAdapterState().activeProfileName).toBe("bob");
    expect(created).toHaveLength(2);
  });

  it("get returns undefined for unknown sessionId", () => {
    const { factory } = createMockAdapterFactory();
    const store = new SessionAgentStore(throwProvider, factory, new MemorySaver());

    expect(store.get("nonexistent")).toBeUndefined();
  });
});
