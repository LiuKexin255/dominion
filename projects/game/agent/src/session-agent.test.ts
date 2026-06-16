/**
 * session-agent.test.ts — Tests for SessionAgent and SessionAgentStore.
 *
 * Covers: on-demand adapter creation, same-profile reuse, profile switching
 * with async cleanup, A→B→A new-instance guarantee, getAdapterState, and
 * SessionAgentStore per-session isolation.
 */

import { MemorySaver } from "@langchain/langgraph";
import { describe, expect, it, vi } from "vitest";

import type { ChatModel } from "./model-provider";
import { SessionAgent, SessionAgentStore } from "./session-agent";
import type { ProfileData, ProfileFetcher } from "./session-agent";
import type { AgentAdapter, AdapterFactory, ContentBlock } from "./llm";

function createMockAdapter(blocks: ContentBlock[] = []): AgentAdapter {
  return {
    async *generateTurn(): AsyncIterable<ContentBlock> {
      for (const b of blocks) yield b;
    },
    cleanup: vi.fn(),
  };
}

function createMockAdapterFactory(): {
  factory: AdapterFactory;
  created: AgentAdapter[];
} {
  const created: AgentAdapter[] = [];
  const factory: AdapterFactory = async () => {
    const adapter = createMockAdapter([
      { type: "text", text: `adapter-${created.length}` },
    ]);
    created.push(adapter);
    return adapter;
  };
  return { factory, created };
}

const PROFILES: Record<string, ProfileData> = {
  alice: { model: "gpt-4o", systemPrompt: "You are Alice." },
  bob: { model: "minimax-m1", systemPrompt: "You are Bob." },
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
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver());

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
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver());

    const first = await agent.getOrCreateAdapter("alice", profileFetcherFor("alice"));
    const second = await agent.getOrCreateAdapter("alice", profileFetcherFor("alice"));

    expect(second).toBe(first);
    expect(created).toHaveLength(1);
  });

  it("creates new adapter on profile switch and cleans up old", async () => {
    const { factory, created } = createMockAdapterFactory();
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver());

    const aliceAdapter = await agent.getOrCreateAdapter("alice", profileFetcherFor("alice"));
    const bobAdapter = await agent.getOrCreateAdapter("bob", profileFetcherFor("bob"));

    expect(bobAdapter).not.toBe(aliceAdapter);
    expect(created).toHaveLength(2);
    expect(agent.getAdapterState().activeProfileName).toBe("bob");

    await new Promise((r) => setImmediate(r));
    expect(aliceAdapter.cleanup).toHaveBeenCalled();
  });

  it("A→B→A creates new adapter for A each time", async () => {
    const { factory, created } = createMockAdapterFactory();
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver());

    const aliceFirst = await agent.getOrCreateAdapter("alice", profileFetcherFor("alice"));
    await agent.getOrCreateAdapter("bob", profileFetcherFor("bob"));
    const aliceSecond = await agent.getOrCreateAdapter("alice", profileFetcherFor("alice"));

    expect(aliceSecond).not.toBe(aliceFirst);
    expect(created).toHaveLength(3);
  });

  it("returns null adapter and unbound state before first bind", () => {
    const { factory } = createMockAdapterFactory();
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver());

    expect(agent.getAdapter()).toBeNull();
    expect(agent.getAdapterState()).toEqual({
      activeProfileName: null,
      isBound: false,
    });
  });

  it("serializes concurrent binds for same agent", async () => {
    const { factory, created } = createMockAdapterFactory();
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver());

    const [a1, a2] = await Promise.all([
      agent.getOrCreateAdapter("alice", profileFetcherFor("alice")),
      agent.getOrCreateAdapter("alice", profileFetcherFor("alice")),
    ]);

    expect(a1).toBe(a2);
    expect(created).toHaveLength(1);
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
