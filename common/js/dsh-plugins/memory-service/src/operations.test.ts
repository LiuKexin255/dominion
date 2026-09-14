/**
 * Memory operation-core tests — the hermes-style semantics migrated from the
 * v1 agent's `memory-mcp.test.ts` (spec 039 T015; removed with agent v1 in
 * spec 059 US1). Coverage: old_text substring matching (case sensitivity,
 * 0/multiple/all-identical hits, empty locator), deterministic memory_id
 * generation, and `applyMemoryCall` conversion (add dedupe + ALREADY_EXISTS
 * race, replace/remove location, single-vs-batch argument forms, batch
 * preflight atomicity, commit-phase infrastructure failure).
 *
 * DI pattern (style/javascript.md §测试): the storage client is a
 * `vi.fn()`-backed double — no module-level `vi.mock`.
 */

import { beforeEach, describe, expect, it, vi } from "vitest";

import type { MemoryEntry, MemoryStore } from "./client.js";
import {
  MEMORY_ACTIONS,
  applyMemoryCall,
  generateMemoryId,
  matchBySubstring,
} from "./operations.js";
import type { MemoryToolArgs } from "./operations.js";

/** Fake MemoryStore surface with in-memory state (DI seam). */
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

function call(
  store: ReturnType<typeof makeFakeStore>,
  args: MemoryToolArgs,
): Promise<string> {
  return applyMemoryCall(store as unknown as MemoryStore, TEMPLATE, SESSION, args);
}

const TEMPLATE = "saolei";
const SESSION = "sess-1";

describe("matchBySubstring (hermes old_text semantics)", () => {
  const entries: MemoryEntry[] = [
    { memory_id: "m1", content: "player 常误标边角" },
    { memory_id: "m2", content: "开局先点中心更高效" },
    { memory_id: "m3", content: "player 常误标边角" }, // identical to m1
  ];

  it("matches by case-sensitive substring containment", () => {
    const hit = matchBySubstring(entries, "误标边角");
    expect("entry" in hit && hit.entry.memory_id).toBe("m1");
  });

  it("is case-sensitive (no match for a different case)", () => {
    const hit = matchBySubstring(
      [{ memory_id: "m1", content: "CaseSensitive" }],
      "casesensitive",
    );
    expect("error" in hit).toBe(true);
  });

  it("0 hits → error text containing the FULL current entry list, no id", () => {
    const hit = matchBySubstring(entries, "不存在的子串");
    expect("error" in hit).toBe(true);
    if ("error" in hit) {
      expect(hit.error).toContain("no entry matched '不存在的子串'");
      expect(hit.error).toContain("player 常误标边角");
      expect(hit.error).toContain("开局先点中心更高效");
      expect(hit.error).not.toContain("m1");
    }
  });

  it("multiple DISTINCT hits → error text with hit previews (be more specific)", () => {
    const multi = matchBySubstring(
      [
        { memory_id: "m1", content: "player 过度标记" },
        { memory_id: "m2", content: "player 过度谨慎" },
      ],
      "过度",
    );
    expect("error" in multi).toBe(true);
    if ("error" in multi) {
      expect(multi.error).toContain("multiple entries matched '过度'");
      expect(multi.error).toContain("player 过度标记");
      expect(multi.error).toContain("player 过度谨慎");
    }
  });

  it("all-identical hits → act on the FIRST entry (dedupe)", () => {
    const hit = matchBySubstring(entries, "误标边角");
    expect("entry" in hit && hit.entry).toEqual({
      memory_id: "m1",
      content: "player 常误标边角",
    });
  });

  it("empty/whitespace old_text → error text + current entries", () => {
    for (const empty of ["", "   "]) {
      const hit = matchBySubstring(entries, empty);
      expect("error" in hit).toBe(true);
      if ("error" in hit) {
        expect(hit.error).toContain("old_text cannot be empty");
        expect(hit.error).toContain("player 常误标边角");
      }
    }
  });

  it("0 hits on an empty store → 'no entries' placeholder", () => {
    const hit = matchBySubstring([], "x");
    expect("error" in hit && hit.error).toContain("(no entries)");
  });
});

describe("generateMemoryId (agent-side id, invisible to the LLM)", () => {
  it("is deterministic for the same content", () => {
    expect(generateMemoryId("内容")).toBe(generateMemoryId("内容"));
  });

  it("differs for different content", () => {
    expect(generateMemoryId("内容A")).not.toBe(generateMemoryId("内容B"));
  });

  it("matches the memory service memory_id charset [a-z0-9_-]+", () => {
    for (const content of ["player 过度标记", "win-rate", "1+1=2"]) {
      expect(generateMemoryId(content)).toMatch(/^[a-z0-9_-]+$/);
    }
  });
});

describe("applyMemoryCall — hermes conversion", () => {
  let fake: ReturnType<typeof makeFakeStore>;

  beforeEach(() => {
    fake = makeFakeStore();
  });

  describe("add", () => {
    it("generates the internal id and calls createMemory (add → CreateMemory)", async () => {
      const text = "player 重复误标地雷";
      const result = await call(fake, { action: "add", content: text });

      expect(result).toBe("memory added");
      expect(fake.createMemory).toHaveBeenCalledTimes(1);
      expect(fake.createMemory).toHaveBeenCalledWith(
        TEMPLATE,
        SESSION,
        generateMemoryId(text),
        text,
      );
      expect(fake.listMemories).toHaveBeenCalledTimes(1); // dedupe check
    });

    it("dedupes an equivalent existing content → success, NO createMemory", async () => {
      fake = makeFakeStore([{ memory_id: "m1", content: "已存在的洞察" }]);
      const result = await call(fake, {
        action: "add",
        content: "已存在的洞察",
      });

      expect(result).toBe("memory already exists (no duplicate added)");
      expect(fake.createMemory).not.toHaveBeenCalled();
    });

    it("empty content → error text", async () => {
      const result = await call(fake, { action: "add", content: "  " });
      expect(result).toBe("memory: content cannot be empty");
      expect(fake.createMemory).not.toHaveBeenCalled();
    });

    it("a CreateMemory ALREADY_EXISTS race → dedupe SUCCESS text (code 6)", async () => {
      fake.createMemory.mockRejectedValueOnce(
        Object.assign(new Error("memory already exists"), { code: 6 }),
      );

      const result = await call(fake, { action: "add", content: "竞态内容" });

      expect(result).toBe("memory already exists (no duplicate added)");
      expect(fake.createMemory).toHaveBeenCalledTimes(1);
    });

    it("a NON-ALREADY_EXISTS createMemory failure still propagates as an infra error", async () => {
      fake.createMemory.mockRejectedValueOnce(
        Object.assign(new Error("memory service unavailable"), { code: 14 }),
      );

      await expect(call(fake, { action: "add", content: "内容" })).rejects.toThrow(
        "memory service unavailable",
      );
    });
  });

  describe("replace", () => {
    it("locates by old_text substring and calls updateMemory (replace → UpdateMemory)", async () => {
      fake = makeFakeStore([
        { memory_id: "m1", content: "player 常误标边角" },
        { memory_id: "m2", content: "开局先点中心更高效" },
      ]);
      const result = await call(fake, {
        action: "replace",
        old_text: "误标边角",
        content: "player 已改掉误标习惯",
      });

      expect(result).toBe("memory replaced");
      expect(fake.updateMemory).toHaveBeenCalledTimes(1);
      expect(fake.updateMemory).toHaveBeenCalledWith(
        TEMPLATE,
        SESSION,
        "m1",
        "player 已改掉误标习惯",
      );
      expect(fake.deleteMemory).not.toHaveBeenCalled();
    });

    it("0 hits → error text with the current entries, NO write", async () => {
      fake = makeFakeStore([{ memory_id: "m1", content: "唯一条目" }]);
      const result = await call(fake, {
        action: "replace",
        old_text: "无此内容",
        content: "新内容",
      });

      expect(result).toContain("no entry matched '无此内容'");
      expect(result).toContain("唯一条目");
      expect(fake.updateMemory).not.toHaveBeenCalled();
    });

    it("multiple distinct hits → error text (be more specific), NO write", async () => {
      fake = makeFakeStore([
        { memory_id: "m1", content: "player 过度标记" },
        { memory_id: "m2", content: "player 过度谨慎" },
      ]);
      const result = await call(fake, {
        action: "replace",
        old_text: "过度",
        content: "新内容",
      });

      expect(result).toContain("multiple entries matched '过度'");
      expect(result).toContain("player 过度标记");
      expect(result).toContain("player 过度谨慎");
      expect(fake.updateMemory).not.toHaveBeenCalled();
    });

    it("all-identical hits → updates the FIRST entry", async () => {
      fake = makeFakeStore([
        { memory_id: "m1", content: "重复条目" },
        { memory_id: "m2", content: "重复条目" },
      ]);
      const result = await call(fake, {
        action: "replace",
        old_text: "重复",
        content: "合并后的条目",
      });

      expect(result).toBe("memory replaced");
      expect(fake.updateMemory).toHaveBeenCalledWith(
        TEMPLATE,
        SESSION,
        "m1",
        "合并后的条目",
      );
    });

    it("missing old_text → error text with current entries", async () => {
      fake = makeFakeStore([{ memory_id: "m1", content: "条目" }]);
      const result = await call(fake, {
        action: "replace",
        content: "新内容",
      });
      expect(result).toContain("old_text cannot be empty");
      expect(result).toContain("条目");
      expect(fake.updateMemory).not.toHaveBeenCalled();
    });
  });

  describe("remove", () => {
    it("locates by old_text substring and calls deleteMemory (remove → DeleteMemory)", async () => {
      fake = makeFakeStore([
        { memory_id: "m1", content: "过时的条目" },
        { memory_id: "m2", content: "保留的条目" },
      ]);
      const result = await call(fake, { action: "remove", old_text: "过时的" });

      expect(result).toBe("memory removed");
      expect(fake.deleteMemory).toHaveBeenCalledTimes(1);
      expect(fake.deleteMemory).toHaveBeenCalledWith(TEMPLATE, SESSION, "m1");
      expect(fake.updateMemory).not.toHaveBeenCalled();
    });

    it("0 hits → error text, NO delete", async () => {
      fake = makeFakeStore([{ memory_id: "m1", content: "条目" }]);
      const result = await call(fake, { action: "remove", old_text: "无" });
      expect(result).toContain("no entry matched '无'");
      expect(fake.deleteMemory).not.toHaveBeenCalled();
    });
  });

  describe("argument forms (single vs batch, mutually exclusive)", () => {
    it("rejects providing BOTH action and operations", async () => {
      const result = await call(fake, {
        action: "add",
        content: "x",
        operations: [{ action: "add", content: "y" }],
      });
      expect(result).toContain("not both");
      expect(fake.createMemory).not.toHaveBeenCalled();
    });

    it("rejects providing NEITHER", async () => {
      const result = await call(fake, {});
      expect(result).toContain("provide EITHER");
      expect(fake.createMemory).not.toHaveBeenCalled();
    });

    it("rejects an action outside add/replace/remove (defense beyond the schema)", async () => {
      const result = await call(fake, {
        action: "read" as never,
      });
      expect(result).toContain("memory: invalid call");
      expect(fake.listMemories).not.toHaveBeenCalled();
    });
  });

  describe("operations batch (atomic all-or-nothing preflight)", () => {
    it("applies a mixed batch in order (add + replace + remove)", async () => {
      fake = makeFakeStore([
        { memory_id: "m1", content: "旧条目A" },
        { memory_id: "m2", content: "待删条目" },
      ]);
      const result = await call(fake, {
        operations: [
          { action: "add", content: "新条目" },
          { action: "replace", old_text: "旧条目A", content: "更新后的A" },
          { action: "remove", old_text: "待删" },
        ],
      });

      expect(result).toBe("memory: applied 3 operation(s)");
      expect(fake.createMemory).toHaveBeenCalledWith(
        TEMPLATE,
        SESSION,
        generateMemoryId("新条目"),
        "新条目",
      );
      expect(fake.updateMemory).toHaveBeenCalledWith(
        TEMPLATE,
        SESSION,
        "m1",
        "更新后的A",
      );
      expect(fake.deleteMemory).toHaveBeenCalledWith(TEMPLATE, SESSION, "m2");
    });

    it("skips duplicate adds inside the batch (idempotent, hermes)", async () => {
      fake = makeFakeStore([{ memory_id: "m1", content: "已存在" }]);
      const result = await call(fake, {
        operations: [
          { action: "add", content: "已存在" },
          { action: "add", content: "全新" },
        ],
      });

      expect(result).toBe("memory: applied 1 operation(s)");
      expect(fake.createMemory).toHaveBeenCalledTimes(1);
      expect(fake.createMemory).toHaveBeenCalledWith(
        TEMPLATE,
        SESSION,
        generateMemoryId("全新"),
        "全新",
      );
    });

    it("a failing op aborts the WHOLE batch — nothing is written (preflight)", async () => {
      fake = makeFakeStore([{ memory_id: "m1", content: "唯一条目" }]);
      const result = await call(fake, {
        operations: [
          { action: "add", content: "新条目" },
          { action: "remove", old_text: "无此内容" },
        ],
      });

      expect(result).toContain("operation 2 failed");
      expect(result).toContain("No operations were applied");
      expect(result).toContain("唯一条目");
      expect(fake.createMemory).not.toHaveBeenCalled();
      expect(fake.deleteMemory).not.toHaveBeenCalled();
    });

    it("a missing replace/remove field aborts the batch in preflight", async () => {
      fake = makeFakeStore([{ memory_id: "m1", content: "唯一条目" }]);
      const result = await call(fake, {
        operations: [
          { action: "add", content: "新条目" },
          { action: "replace", content: "新内容" },
        ],
      });

      expect(result).toContain("operation 2 failed");
      expect(result).toContain("old_text is required");
      expect(fake.createMemory).not.toHaveBeenCalled();
      expect(fake.updateMemory).not.toHaveBeenCalled();
    });

    it("empty operations list → error text", async () => {
      const result = await call(fake, { operations: [] });
      expect(result).toBe("memory: operations list is empty");
      expect(fake.createMemory).not.toHaveBeenCalled();
    });

    it("commit-phase infrastructure failure propagates (earlier writes stay applied)", async () => {
      fake = makeFakeStore([{ memory_id: "m1", content: "旧条目A" }]);
      fake.updateMemory.mockRejectedValueOnce(
        Object.assign(new Error("memory service unavailable"), { code: 14 }),
      );

      await expect(
        call(fake, {
          operations: [
            { action: "add", content: "新条目" },
            { action: "replace", old_text: "旧条目A", content: "更新后的A" },
          ],
        }),
      ).rejects.toThrow("memory service unavailable");

      expect(fake.createMemory).toHaveBeenCalledTimes(1);
      expect(fake.state.some((e) => e.content === "新条目")).toBe(true);
      expect(fake.state.some((e) => e.content === "更新后的A")).toBe(false);
    });
  });
});

describe("MEMORY_ACTIONS (no read action exists)", () => {
  it("is restricted to add/replace/remove", () => {
    expect(MEMORY_ACTIONS).toEqual(["add", "replace", "remove"]);
    expect(MEMORY_ACTIONS).not.toContain("read");
  });
});
