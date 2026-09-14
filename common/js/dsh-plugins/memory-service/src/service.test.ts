/**
 * PlannerMemoryService tests — the host service face: snapshot prefetch
 * (render + cache binding), fail-loud rejection, per-agent isolation, scope
 * cleanup on agent disposal, and the write path routing through the bound
 * `(template, session)` scope.
 *
 * Pattern (style/javascript.md Mock convention): the storage client and the
 * agent context are injected doubles (`vi.fn()`); no module interception.
 * Contract: specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §2.
 */

import type { Context } from "@deepseek-ai/cordis";
import type { Agent } from "@deepseek-ai/dsh-agent";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { MemoryEntry, MemoryStore } from "./client.js";
import { createPlannerMemory } from "./service.js";

/** Fake MemoryStore with in-memory state (DI seam). */
function makeFakeStore(entries: MemoryEntry[] = []) {
  const state: MemoryEntry[] = entries.map((e) => ({ ...e }));
  return {
    state,
    createMemory: vi.fn(
      async (_t: string, _s: string, id: string, content: string) => {
        state.push({ memory_id: id, content });
      },
    ),
    updateMemory: vi.fn(
      async (_t: string, _s: string, id: string, content: string) => {
        const e = state.find((x) => x.memory_id === id);
        if (e) {
          e.content = content;
        }
      },
    ),
    deleteMemory: vi.fn(async (_t: string, _s: string, id: string) => {
      const i = state.findIndex((x) => x.memory_id === id);
      if (i >= 0) {
        state.splice(i, 1);
      }
    }),
    listMemories: vi.fn(async () => state.map((e) => ({ ...e }))),
  };
}

interface AgentScope {
  readonly agent: Agent;
  readonly ctx: Context;
  readonly cleanups: Array<() => void>;
}

/** A fake agent scope: identity-keyed agent plus a controllable effect. */
function makeAgentScope(id = "planner"): AgentScope {
  const cleanups: Array<() => void> = [];
  const agent = { id } as unknown as Agent;
  const ctx = {
    agent,
    effect: vi.fn((register: () => () => void) => {
      cleanups.push(register());
      return vi.fn();
    }),
  } as unknown as Context;
  return { agent, ctx, cleanups };
}

const SCOPE = { template: "saolei", session: "sess-1" };

describe("createPlannerMemory — load (snapshot prefetch)", () => {
  let store: ReturnType<typeof makeFakeStore>;

  beforeEach(() => {
    store = makeFakeStore();
  });

  it("reads the bound scope and renders one line per entry, without ids", async () => {
    store = makeFakeStore([
      { memory_id: "m1", content: "开局先点中心更高效" },
      { memory_id: "m2", content: "player 常误标边角" },
    ]);
    const service = createPlannerMemory({ client: store as unknown as MemoryStore });
    const { agent, ctx } = makeAgentScope();

    await service.load(ctx, SCOPE);

    expect(store.listMemories).toHaveBeenCalledWith("saolei", "sess-1");
    const snapshot = service.snapshot(agent);
    expect(snapshot).toBe("长期记忆：\n开局先点中心更高效\nplayer 常误标边角");
    expect(snapshot).not.toContain("m1");
    expect(snapshot).not.toContain("m2");
  });

  it("renders an empty snapshot when the scope has no entries (section drops)", async () => {
    const service = createPlannerMemory({ client: store as unknown as MemoryStore });
    const { agent, ctx } = makeAgentScope();

    await service.load(ctx, SCOPE);

    expect(service.snapshot(agent)).toBe("");
  });

  it("rejects fail-loud and leaves no binding or snapshot behind", async () => {
    store.listMemories.mockRejectedValueOnce(
      Object.assign(new Error("memory service unavailable"), { code: 14 }),
    );
    const service = createPlannerMemory({ client: store as unknown as MemoryStore });
    const { agent, ctx } = makeAgentScope();

    await expect(service.load(ctx, SCOPE)).rejects.toThrow(
      "memory service unavailable",
    );

    expect(service.snapshot(agent)).toBeUndefined();
    await expect(
      service.applyCall(agent, { action: "add", content: "内容" }),
    ).rejects.toThrow("has no loaded memory scope");
    expect(store.createMemory).not.toHaveBeenCalled();
  });

  it("throws for an unscoped context (no agent) before any read", async () => {
    const service = createPlannerMemory({ client: store as unknown as MemoryStore });
    const ctx = { effect: vi.fn() } as unknown as Context;

    await expect(service.load(ctx, SCOPE)).rejects.toThrow(
      "requires an agent-scoped context",
    );
    expect(store.listMemories).not.toHaveBeenCalled();
  });

  it("keys the snapshot per agent (two planners never share one)", async () => {
    const service = createPlannerMemory({ client: store as unknown as MemoryStore });
    const first = makeAgentScope("planner-1");
    const second = makeAgentScope("planner-2");

    store.listMemories.mockResolvedValueOnce([{ memory_id: "m1", content: "第一条" }]);
    await service.load(first.ctx, { template: "saolei", session: "sess-1" });
    store.listMemories.mockResolvedValueOnce([{ memory_id: "m2", content: "第二条" }]);
    await service.load(second.ctx, { template: "saolei", session: "sess-2" });

    expect(service.snapshot(first.agent)).toBe("长期记忆：\n第一条");
    expect(service.snapshot(second.agent)).toBe("长期记忆：\n第二条");
  });

  it("snapshot(undefined) is undefined (scope-less assembly)", () => {
    const service = createPlannerMemory({ client: store as unknown as MemoryStore });
    expect(service.snapshot(undefined)).toBeUndefined();
  });

  it("removes the snapshot and binding when the agent scope unwinds", async () => {
    const service = createPlannerMemory({ client: store as unknown as MemoryStore });
    const { agent, ctx, cleanups } = makeAgentScope();
    await service.load(ctx, SCOPE);
    expect(service.snapshot(agent)).toBeDefined();

    for (const cleanup of cleanups) {
      cleanup();
    }

    expect(service.snapshot(agent)).toBeUndefined();
    await expect(
      service.applyCall(agent, { action: "add", content: "内容" }),
    ).rejects.toThrow("has no loaded memory scope");
  });
});

describe("createPlannerMemory — applyCall (write path)", () => {
  let store: ReturnType<typeof makeFakeStore>;

  beforeEach(() => {
    store = makeFakeStore();
  });

  async function loaded(entries: MemoryEntry[] = []) {
    store = makeFakeStore(entries);
    const service = createPlannerMemory({ client: store as unknown as MemoryStore });
    const scope = makeAgentScope();
    await service.load(scope.ctx, SCOPE);
    return { service, ...scope };
  }

  it("routes add through the bound (template, session) with immediate persistence", async () => {
    const { service, agent } = await loaded();

    const result = await service.applyCall(agent, { action: "add", content: "新洞察" });

    expect(result).toBe("memory added");
    expect(store.createMemory).toHaveBeenCalledWith(
      "saolei",
      "sess-1",
      expect.any(String),
      "新洞察",
    );
  });

  it("keeps the snapshot frozen at its load-time text across successful writes (decision ③)", async () => {
    const { service, agent } = await loaded([{ memory_id: "m1", content: "旧条目" }]);
    expect(service.snapshot(agent)).toBe("长期记忆：\n旧条目");

    await service.applyCall(agent, { action: "add", content: "新洞察" });

    // The write persists immediately, but the injected snapshot is fixed for
    // the agent's lifetime (survey/deepseek-harness-memory-plugin.md 决策 ③):
    // modifications surface through the tool call history, never by
    // refreshing the system prompt.
    expect(store.state.map((entry) => entry.content)).toEqual(["旧条目", "新洞察"]);
    expect(service.snapshot(agent)).toBe("长期记忆：\n旧条目");
  });

  it("routes replace and remove through the bound scope", async () => {
    const { service, agent } = await loaded([
      { memory_id: "m1", content: "旧条目" },
    ]);

    expect(
      await service.applyCall(agent, {
        action: "replace",
        old_text: "旧条目",
        content: "新条目",
      }),
    ).toBe("memory replaced");
    expect(store.updateMemory).toHaveBeenCalledWith("saolei", "sess-1", "m1", "新条目");

    expect(
      await service.applyCall(agent, { action: "remove", old_text: "新条目" }),
    ).toBe("memory removed");
    expect(store.deleteMemory).toHaveBeenCalledWith("saolei", "sess-1", "m1");
  });

  it("routes a batch through the same scope; preflight failure commits nothing", async () => {
    const { service, agent } = await loaded([{ memory_id: "m1", content: "唯一条目" }]);

    const result = await service.applyCall(agent, {
      operations: [
        { action: "add", content: "新条目" },
        { action: "remove", old_text: "无此内容" },
      ],
    });

    expect(result).toContain("No operations were applied");
    expect(store.createMemory).not.toHaveBeenCalled();
    expect(store.deleteMemory).not.toHaveBeenCalled();
  });

  it("returns domain text for a bad argument combination (never throws)", async () => {
    const { service, agent } = await loaded();
    await expect(service.applyCall(agent, {})).resolves.toContain("provide EITHER");
  });
});
