/**
 * Memory face and capability-unit tests: scope binding at face creation (no
 * agent-keyed resolution), frozen snapshot prefetch (fail-loud rejection), the
 * write/wiki forwarding through the bound scope, and the atomic capability
 * unit (faces/tools/sections/ready produced together).
 *
 * Pattern (style/javascript.md Mock convention): the store/clients are
 * injected doubles (`vi.fn()`); no module interception.
 * Contract: specs/074-dsh-grpc-runtime/contracts/memory-library.md §1-§2.
 */

import { describe, expect, it, vi } from "vitest";

import { buildMemoryCapabilities } from "./capabilities.js";
import { createMemoryFace, createMemoryStore } from "./face.js";
import {
  MEMORY_GUIDANCE,
  MEMORY_GUIDANCE_SECTION_NAME,
  MEMORY_GUIDANCE_SECTION_ORDER,
  WIKI_GUIDANCE,
  WIKI_GUIDANCE_SECTION_NAME,
  WIKI_GUIDANCE_SECTION_ORDER,
} from "./guidance.js";
import {
  MEMORY_SNAPSHOT_SECTION_NAME,
  MEMORY_SNAPSHOT_SECTION_ORDER,
} from "./snapshot.js";
import type {
  MemoryEntry,
  MemoryServiceCalls,
  MemoryStore,
  WikiServiceCalls,
} from "./store.js";

const SCOPE = { template: "saolei", preset: "preset-1" };

/**
 * A promise-based MemoryStore double for the face tests (the face consumes the
 * domain seam, so the fake need not be callback-style).
 */
function makeFakeStore(entries: MemoryEntry[] = []) {
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
    searchWikis: vi.fn(async () => ({ wikis: [], nextPageToken: "" })),
    getWiki: vi.fn(),
    createWiki: vi.fn(),
    updateWiki: vi.fn(),
    deleteWiki: vi.fn(),
    listWikis: vi.fn(async () => ({ wikis: [], nextPageToken: "" })),
  };
}

type FakeStore = ReturnType<typeof makeFakeStore>;

function faceOf(store: FakeStore, scope = SCOPE) {
  return createMemoryFace(store as unknown as MemoryStore, scope);
}

describe("createMemoryFace — scope binding and snapshot freeze", () => {
  it("prefetches the bound (template, preset) with the recency order/limit and renders the snapshot", async () => {
    const store = makeFakeStore([
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
    const face = faceOf(makeFakeStore());
    await face.prefetch();
    expect(face.snapshotText()).toBe("");
  });

  it("rejects fail-loud and leaves no snapshot behind", async () => {
    const store = makeFakeStore();
    store.listMemories.mockRejectedValueOnce(
      Object.assign(new Error("memory service unavailable"), { code: 14 }),
    );
    const face = faceOf(store);

    await expect(face.prefetch()).rejects.toThrow("memory service unavailable");

    expect(face.snapshotText()).toBe("");
    expect(store.createMemory).not.toHaveBeenCalled();
  });

  it("keys the snapshot to the face's own scope (two faces never share one)", async () => {
    const storeA = makeFakeStore([{ memory_id: "m1", content: "第一条" }]);
    const storeB = makeFakeStore([{ memory_id: "m2", content: "第二条" }]);
    const faceA = faceOf(storeA, { template: "saolei", preset: "preset-1" });
    const faceB = faceOf(storeB, { template: "saolei", preset: "preset-2" });

    await faceA.prefetch();
    await faceB.prefetch();

    expect(faceA.snapshotText()).toBe("长期记忆：\n第一条");
    expect(faceB.snapshotText()).toBe("长期记忆：\n第二条");
  });

  it("keeps the snapshot frozen across later writes", async () => {
    const store = makeFakeStore([{ memory_id: "m1", content: "旧条目" }]);
    const face = faceOf(store);
    await face.prefetch();
    expect(face.snapshotText()).toBe("长期记忆：\n旧条目");

    await face.applyCall({ action: "add", content: "新洞察" });

    expect(store.state.map((entry) => entry.content)).toEqual(["旧条目", "新洞察"]);
    expect(face.snapshotText()).toBe("长期记忆：\n旧条目");
  });

  it("snapshotText is the empty string before the prefetch", () => {
    expect(faceOf(makeFakeStore()).snapshotText()).toBe("");
  });
});

describe("createMemoryFace — memory write path", () => {
  it("routes add through the bound scope with immediate persistence", async () => {
    const store = makeFakeStore();
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
    const store = makeFakeStore([{ memory_id: "m1", content: "旧条目" }]);
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
    const store = makeFakeStore([{ memory_id: "m1", content: "唯一条目" }]);
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
    const face = faceOf(makeFakeStore());
    await expect(face.applyCall({})).resolves.toContain("provide EITHER");
  });

  it("propagates an infrastructure failure (the tool surface renders text)", async () => {
    const store = makeFakeStore();
    store.listMemories.mockRejectedValue(
      Object.assign(new Error("memory service unavailable"), { code: 14 }),
    );
    const face = faceOf(store);

    await expect(
      face.applyCall({ action: "add", content: "内容" }),
    ).rejects.toThrow("memory service unavailable");
  });
});

describe("createMemoryFace — wiki face (template-only scope)", () => {
  it("routes wikiSearch/wikiGet/wikiWrite through the bound template alone", async () => {
    const store = makeFakeStore();
    store.searchWikis.mockResolvedValue({
      wikis: [{ name: "templates/saolei/wikis/w1", title: "开局策略" }],
      nextPageToken: "",
    });
    store.getWiki.mockResolvedValue({
      name: "templates/saolei/wikis/w1",
      title: "开局策略",
      content: "正文",
    });
    const face = faceOf(store, { template: "saolei", preset: "preset-1" });

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
});

describe("createMemoryStore — client adaptation", () => {
  it("composes the memory and wiki clients into one domain seam", async () => {
    const listMemories = vi.fn(
      (
        _req: unknown,
        _md: unknown,
        _opts: unknown,
        cb: (e: Error | null, r: unknown) => void,
      ) => cb(null, { memories: [], nextPageToken: "" }),
    );
    const searchWikis = vi.fn(
      (
        _req: unknown,
        _md: unknown,
        _opts: unknown,
        cb: (e: Error | null, r: unknown) => void,
      ) => cb(null, { wikis: [], nextPageToken: "" }),
    );
    const memoryClient = { listMemories } as unknown as MemoryServiceCalls;
    const wikiClient = { searchWikis } as unknown as WikiServiceCalls;

    const store = createMemoryStore(memoryClient, wikiClient);

    await store.listMemories("saolei", "preset-1");
    await store.searchWikis("saolei", "开局");

    expect(listMemories).toHaveBeenCalledTimes(1);
    expect(searchWikis).toHaveBeenCalledTimes(1);
  });
});

/** A callback-style service-client double responding with `methods[name]()`. */
function callbackClient(
  methods: Readonly<Record<string, () => unknown>>,
): Record<string, ReturnType<typeof vi.fn>> {
  const client: Record<string, ReturnType<typeof vi.fn>> = {};
  for (const name of Object.keys(methods)) {
    client[name] = vi.fn(
      (
        _req: unknown,
        _md: unknown,
        _opts: unknown,
        cb: (e: Error | null, r?: unknown) => void,
      ) => cb(null, methods[name]()),
    );
  }
  return client;
}

describe("buildMemoryCapabilities — atomic capability unit", () => {
  function clients(entries: MemoryEntry[] = []) {
    const memoryClient = callbackClient({
      createMemory: () => undefined,
      updateMemory: () => undefined,
      deleteMemory: () => undefined,
      listMemories: () => ({
        memories: entries.map((e) => ({
          name: `templates/saolei/presets/preset-1/memories/${e.memory_id}`,
          content: e.content,
        })),
        nextPageToken: "",
      }),
    });
    const wikiClient = callbackClient({
      searchWikis: () => ({ wikis: [], nextPageToken: "" }),
      getWiki: () => undefined,
      createWiki: () => undefined,
      updateWiki: () => undefined,
      deleteWiki: () => undefined,
      listWikis: () => ({ wikis: [], nextPageToken: "" }),
    });
    return {
      memoryClient: memoryClient as unknown as MemoryServiceCalls,
      wikiClient: wikiClient as unknown as WikiServiceCalls,
    };
  }

  it("produces faces, the four tools, the three sections and ready together", () => {
    const unit = buildMemoryCapabilities({ scope: SCOPE, ...clients() });

    expect(unit.faces.map((f) => f.key)).toEqual(["memory"]);
    expect(unit.tools.map((tool) => tool.name)).toEqual([
      "memory",
      "wiki_search",
      "wiki_get",
      "wiki_write",
    ]);
    expect(unit.sections.map((section) => section.name)).toEqual([
      MEMORY_GUIDANCE_SECTION_NAME,
      WIKI_GUIDANCE_SECTION_NAME,
      MEMORY_SNAPSHOT_SECTION_NAME,
    ]);
    expect(unit.sections.map((section) => section.order)).toEqual([
      MEMORY_GUIDANCE_SECTION_ORDER,
      WIKI_GUIDANCE_SECTION_ORDER,
      MEMORY_SNAPSHOT_SECTION_ORDER,
    ]);
    expect(unit.sections[0].text).toBe(MEMORY_GUIDANCE);
    expect(unit.sections[1].text).toBe(WIKI_GUIDANCE);
    expect(typeof unit.sections[2].text).toBe("function");
    expect(typeof unit.ready).toBe("function");
  });

  it("binds the unit's own face to the agent-scoped registration", () => {
    const unit = buildMemoryCapabilities({ scope: SCOPE, ...clients() });
    expect(unit.faces[0].value.snapshotText()).toBe("");
  });

  it("ready() prefetches the scope and freezes the snapshot the section reads", async () => {
    const unit = buildMemoryCapabilities({
      scope: SCOPE,
      ...clients([{ memory_id: "m1", content: "洞察" }]),
    });

    await unit.ready();

    const text = unit.sections[2].text as () => string;
    expect(text()).toBe("长期记忆：\n洞察");
    expect(unit.faces[0].value.snapshotText()).toBe("长期记忆：\n洞察");
  });

  it("ready() rejects fail-loud on a prefetch failure", async () => {
    const listMemories = vi.fn(
      (
        _req: unknown,
        _md: unknown,
        _opts: unknown,
        cb: (e: Error | null, r: unknown) => void,
      ) =>
        cb(Object.assign(new Error("memory service unavailable"), { code: 14 })),
    );
    const memoryClient = {
      createMemory: vi.fn(),
      updateMemory: vi.fn(),
      deleteMemory: vi.fn(),
      listMemories,
    } as unknown as MemoryServiceCalls;
    const wikiClient = callbackClient({
      searchWikis: () => ({ wikis: [], nextPageToken: "" }),
    }) as unknown as WikiServiceCalls;

    const unit = buildMemoryCapabilities({
      scope: SCOPE,
      memoryClient,
      wikiClient,
    });

    await expect(unit.ready()).rejects.toThrow("memory service unavailable");
  });
});
