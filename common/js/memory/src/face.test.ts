/**
 * Memory/wiki face tests: scope binding at face creation (no agent-keyed
 * resolution), the frozen memory snapshot prefetch (fail-loud rejection), the
 * memory write forwarding and the wiki forwarding through the template-only
 * binding.
 *
 * Pattern (style/javascript.md Mock convention): the stores are injected
 * doubles (`vi.fn()`); no module interception.
 * Contracts: specs/074-dsh-grpc-runtime/contracts/memory-library.md §2;
 * specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1 义务 1.
 */

import { describe, expect, it, vi } from "vitest";

import { createMemoryFace, createWikiFace } from "./face.js";
import type { MemoryEntry, MemoryStoreRpcs, WikiStore } from "./store.js";

const SCOPE = { template: "saolei", preset: "preset-1" };

/**
 * A promise-based MemoryStoreRpcs double: the memory face consumes the domain
 * seam, so the fake need not be callback-style.
 */
function makeMemoryStore(entries: MemoryEntry[] = []) {
  const state: MemoryEntry[] = entries.map((e) => ({ ...e }));
  return {
    state,
    createMemory: vi.fn(
      async (_t: string, _p: string, id: string, content: string) => {
        state.push({ memory_id: id, content });
      },
    ),
    updateMemory: vi.fn(
      async (_t: string, _p: string, id: string, content: string) => {
        const e = state.find((x) => x.memory_id === id);
        if (e) {
          e.content = content;
        }
      },
    ),
    deleteMemory: vi.fn(async (_t: string, _p: string, id: string) => {
      const i = state.findIndex((x) => x.memory_id === id);
      if (i >= 0) {
        state.splice(i, 1);
      }
    }),
    listMemories: vi.fn(
      async (
        _t: string,
        _p: string,
        _options?: { orderBy?: string; pageSize?: number },
      ) => state.map((e) => ({ ...e })),
    ),
  };
}

type MemoryStoreFake = ReturnType<typeof makeMemoryStore>;

/** A WikiStore double carrying only the methods the wiki face calls. */
function makeWikiStore() {
  return {
    searchWikis: vi.fn(async () => ({ wikis: [], nextPageToken: "" })),
    getWiki: vi.fn(async () => ({ name: "", title: "", content: "" })),
    createWiki: vi.fn(async () => undefined),
    updateWiki: vi.fn(async () => undefined),
    deleteWiki: vi.fn(async () => undefined),
    listWikis: vi.fn(async () => ({ wikis: [], nextPageToken: "" })),
  };
}

function faceOf(store: MemoryStoreFake, scope = SCOPE) {
  return createMemoryFace(store as unknown as MemoryStoreRpcs, scope);
}

describe("createMemoryFace — scope binding and snapshot freeze", () => {
  it("prefetches the bound (template, preset) with the recency order/limit and renders the snapshot", async () => {
    const store = makeMemoryStore([
      { memory_id: "m2", content: "次新" },
      { memory_id: "m1", content: "最新" },
    ]);
    const face = faceOf(store);

    await face.prefetch();

    expect(store.listMemories).toHaveBeenCalledWith("saolei", "preset-1", {
      orderBy: "update_time desc",
      pageSize: 10,
    });
    expect(face.snapshotText()).toBe("长期记忆：\n次新\n最新");
    expect(face.snapshotText()).not.toContain("m1");
  });

  it("renders the empty string for a scope with no entries (section drops)", async () => {
    const face = faceOf(makeMemoryStore());
    await face.prefetch();
    expect(face.snapshotText()).toBe("");
  });

  it("rejects fail-loud and leaves no snapshot behind", async () => {
    const store = makeMemoryStore();
    store.listMemories.mockRejectedValueOnce(
      Object.assign(new Error("memory service unavailable"), { code: 14 }),
    );
    const face = faceOf(store);

    await expect(face.prefetch()).rejects.toThrow("memory service unavailable");

    expect(face.snapshotText()).toBe("");
    expect(store.createMemory).not.toHaveBeenCalled();
  });

  it("keys the snapshot to the face's own scope (two faces never share one)", async () => {
    const storeA = makeMemoryStore([{ memory_id: "m1", content: "第一条" }]);
    const storeB = makeMemoryStore([{ memory_id: "m2", content: "第二条" }]);
    const faceA = faceOf(storeA, { template: "saolei", preset: "preset-1" });
    const faceB = faceOf(storeB, { template: "saolei", preset: "preset-2" });

    await faceA.prefetch();
    await faceB.prefetch();

    expect(faceA.snapshotText()).toBe("长期记忆：\n第一条");
    expect(faceB.snapshotText()).toBe("长期记忆：\n第二条");
  });

  it("keeps the snapshot frozen across later writes", async () => {
    const store = makeMemoryStore([{ memory_id: "m1", content: "旧条目" }]);
    const face = faceOf(store);
    await face.prefetch();
    expect(face.snapshotText()).toBe("长期记忆：\n旧条目");

    await face.applyCall({ action: "add", content: "新洞察" });

    expect(store.state.map((entry) => entry.content)).toEqual(["旧条目", "新洞察"]);
    expect(face.snapshotText()).toBe("长期记忆：\n旧条目");
  });

  it("snapshotText is the empty string before the prefetch", () => {
    expect(faceOf(makeMemoryStore()).snapshotText()).toBe("");
  });
});

describe("createMemoryFace — memory write path", () => {
  it("routes add through the bound scope with immediate persistence", async () => {
    const store = makeMemoryStore();
    const face = faceOf(store);

    expect(await face.applyCall({ action: "add", content: "新洞察" })).toBe(
      "memory added",
    );
    expect(store.createMemory).toHaveBeenCalledWith(
      "saolei",
      "preset-1",
      expect.any(String),
      "新洞察",
    );
  });

  it("routes replace and remove through the bound scope", async () => {
    const store = makeMemoryStore([{ memory_id: "m1", content: "旧条目" }]);
    const face = faceOf(store);

    expect(
      await face.applyCall({
        action: "replace",
        old_text: "旧条目",
        content: "新条目",
      }),
    ).toBe("memory replaced");
    expect(store.updateMemory).toHaveBeenCalledWith(
      "saolei",
      "preset-1",
      "m1",
      "新条目",
    );

    expect(
      await face.applyCall({ action: "remove", old_text: "新条目" }),
    ).toBe("memory removed");
    expect(store.deleteMemory).toHaveBeenCalledWith("saolei", "preset-1", "m1");
  });

  it("routes a batch through the same scope; preflight failure commits nothing", async () => {
    const store = makeMemoryStore([{ memory_id: "m1", content: "唯一条目" }]);
    const face = faceOf(store);

    const result = await face.applyCall({
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
    const face = faceOf(makeMemoryStore());
    await expect(face.applyCall({})).resolves.toContain("provide EITHER");
  });

  it("propagates an infrastructure failure (the tool surface renders text)", async () => {
    const store = makeMemoryStore();
    store.listMemories.mockRejectedValue(
      Object.assign(new Error("memory service unavailable"), { code: 14 }),
    );
    const face = faceOf(store);

    await expect(
      face.applyCall({ action: "add", content: "内容" }),
    ).rejects.toThrow("memory service unavailable");
  });
});

describe("createWikiFace — template-only binding", () => {
  it("routes wikiSearch/wikiGet/wikiWrite through the bound template alone", async () => {
    const store = makeWikiStore();
    store.searchWikis.mockResolvedValue({
      wikis: [{ name: "templates/saolei/wikis/w1", title: "开局策略" }],
      nextPageToken: "",
    });
    store.getWiki.mockResolvedValue({
      name: "templates/saolei/wikis/w1",
      title: "开局策略",
      content: "正文",
    });
    const face = createWikiFace(store as unknown as WikiStore, "saolei");

    expect(await face.wikiSearch("开局")).toContain("开局策略");
    expect(store.searchWikis).toHaveBeenCalledWith("saolei", "开局", {
      pageSize: 100,
      pageToken: undefined,
    });

    await expect(face.wikiGet(["开局策略"])).resolves.toContain("正文");
    expect(store.getWiki).toHaveBeenCalledWith("saolei", "w1");

    store.searchWikis.mockResolvedValue({ wikis: [], nextPageToken: "" });
    await expect(
      face.wikiWrite({ title: "新条目", content: "正文" }),
    ).resolves.toContain("created: '新条目'");
    expect(store.createWiki).toHaveBeenCalledWith(
      "saolei",
      expect.any(String),
      "新条目",
      "正文",
    );
  });

  it("carries no preset half: two presets of one template share the collection", async () => {
    const store = makeWikiStore();
    const face = createWikiFace(store as unknown as WikiStore, "saolei");

    await face.wikiSearch("开局");

    expect(store.searchWikis).toHaveBeenCalledWith("saolei", "开局", {
      pageSize: 100,
      pageToken: undefined,
    });
  });
});
