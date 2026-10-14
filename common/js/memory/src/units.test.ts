/**
 * Functional-unit and composition tests
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1/§2):
 *
 * - each unit mounts on its own — its faces/tools/sections/ready are usable
 *   and its tools resolve through its own face key with the other storage
 *   absent (memory tools via `ctx.memory`, wiki tools via `ctx.wiki`);
 * - the memory unit carries no guidance section;
 * - the composition's product is the frozen equivalence-anchor surface
 *   (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §2):
 *   the four tools, the section names and orders 110/120/200, the assembled
 *   texts and the memory prefetch.
 *
 * DI pattern (style/javascript.md §测试): the clients are callback-style
 * `vi.fn()` doubles and the unary policy is a `vi.fn()` pass-through double;
 * no module interception.
 */

import { Metadata } from "@grpc/grpc-js";
import type { ToolRunContext } from "@deepseek-ai/dsh-tools";
import { describe, expect, it, vi } from "vitest";

import { buildMemoryWikiComposition } from "./capabilities.js";
import type { MemoryFace, WikiFace } from "./face.js";
import {
  MEMORY_GUIDANCE,
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
  UnaryClient,
  UnaryInvoke,
  UnaryPolicy,
  WikiServiceCalls,
} from "./store.js";
import { executeMemoryTool } from "./tool.js";
import { buildMemoryUnit, buildWikiUnit } from "./units.js";
import type { CapabilityUnit } from "./units.js";
import {
  executeWikiGetTool,
  executeWikiSearchTool,
} from "./wiki-tools.js";

const SCOPE = { template: "saolei", preset: "preset-1" };

/** A `UnaryPolicy` pass-through double (the real policy's semantics). */
function policyDouble(): {
  readonly policy: UnaryPolicy;
  readonly spy: ReturnType<typeof vi.fn>;
} {
  const spy = vi.fn(
    <T>(
      client: UnaryClient,
      _label: string,
      invoke: UnaryInvoke<T>,
    ): Promise<T> =>
      new Promise<T>((resolve, reject) => {
        invoke(client, new Metadata(), { deadline: new Date() }, (err, res) => {
          if (err !== null) {
            reject(err);
            return;
          }
          resolve(res as T);
        });
      }),
  );
  return { policy: spy as unknown as UnaryPolicy, spy };
}

type CallbackDouble = (
  _req: unknown,
  _md: unknown,
  _opts: unknown,
  cb: (err: Error | null, response?: unknown) => void,
) => void;

/** A callback-style MemoryServiceCalls double; `entries` seeds ListMemories. */
function memoryCalls(entries: MemoryEntry[] = []) {
  const respond =
    (response: unknown): ReturnType<typeof vi.fn> =>
      vi.fn((_r: unknown, _m: unknown, _o: unknown, cb: CallbackDouble) =>
        cb(null, response),
      );
  return {
    createMemory: respond(undefined),
    updateMemory: respond(undefined),
    deleteMemory: respond(undefined),
    listMemories: respond({
      memories: entries.map((e) => ({
        name: `templates/saolei/presets/preset-1/memories/${e.memory_id}`,
        content: e.content,
      })),
      nextPageToken: "",
    }),
  };
}

/** A callback-style WikiServiceCalls double; SearchWikis answers empty. */
function wikiCalls() {
  const respond =
    (response: unknown): ReturnType<typeof vi.fn> =>
      vi.fn((_r: unknown, _m: unknown, _o: unknown, cb: CallbackDouble) =>
        cb(null, response),
      );
  return {
    searchWikis: respond({ wikis: [], nextPageToken: "" }),
    getWiki: respond({
      name: "templates/saolei/wikis/w1",
      title: "开局策略",
      content: "正文",
    }),
    createWiki: respond(undefined),
    updateWiki: respond(undefined),
    deleteWiki: respond(undefined),
    listWikis: respond({ wikis: [], nextPageToken: "" }),
  };
}

/** The face a unit registered under `key`. */
function faceOf<T>(unit: CapabilityUnit, key: string): T {
  const registration = unit.faces.find((face) => face.key === key);
  expect(registration).toBeDefined();
  return registration?.value as T;
}

/** A tool execution whose agent scope holds the given face registrations. */
function makeExec(ctx: Record<string, unknown>): ToolRunContext {
  return { agent: { id: "planner", ctx } } as unknown as ToolRunContext;
}

describe("buildMemoryUnit — memory unit mounted alone", () => {
  function memoryUnit(entries: MemoryEntry[] = []) {
    const calls = memoryCalls(entries);
    const { policy } = policyDouble();
    const unit = buildMemoryUnit({
      scope: SCOPE,
      memoryClient: calls as unknown as MemoryServiceCalls,
      callUnary: policy,
    });
    return { unit, calls };
  }

  it("produces the memory face, the single tool and the snapshot section", () => {
    const { unit } = memoryUnit();

    expect(unit.faces.map((face) => face.key)).toEqual(["memory"]);
    expect(unit.tools.map((tool) => tool.name)).toEqual(["memory"]);
    expect(unit.sections.map((section) => section.name)).toEqual([
      MEMORY_SNAPSHOT_SECTION_NAME,
    ]);
    expect(unit.sections.map((section) => section.order)).toEqual([
      MEMORY_SNAPSHOT_SECTION_ORDER,
    ]);
    expect(typeof unit.ready).toBe("function");
  });

  it("carries no guidance section (the tool description is self-contained)", () => {
    const { unit } = memoryUnit();

    expect(unit.sections.some((s) => s.name.includes("guidance"))).toBe(false);
  });

  it("serves its tool through the memory face key with no wiki face in scope", async () => {
    const { unit, calls } = memoryUnit();

    const result = await executeMemoryTool(
      { action: "add", content: "新洞察" },
      makeExec({ memory: faceOf<MemoryFace>(unit, "memory") }),
    );

    expect(result.result).toBe("memory added");
    expect(calls.createMemory).toHaveBeenCalledTimes(1);
  });

  it("prefetches the scope through the injected policy on ready()", async () => {
    const calls = memoryCalls([{ memory_id: "m1", content: "洞察" }]);
    const { policy, spy } = policyDouble();
    const unit = buildMemoryUnit({
      scope: SCOPE,
      memoryClient: calls as unknown as MemoryServiceCalls,
      callUnary: policy,
    });

    await unit.ready();

    expect(spy).toHaveBeenCalledWith(
      calls,
      "memory client call",
      expect.any(Function),
    );
    expect(calls.listMemories).toHaveBeenCalledWith(
      expect.objectContaining({
        parent: "templates/saolei/presets/preset-1",
        orderBy: "update_time desc",
        pageSize: 10,
      }),
      expect.any(Object),
      expect.any(Object),
      expect.any(Function),
    );
    const face = faceOf<MemoryFace>(unit, "memory");
    expect(face.snapshotText()).toBe("长期记忆：\n洞察");
    expect((unit.sections[0].text as () => string)()).toBe("长期记忆：\n洞察");
  });

  it("rejects fail-loud on a prefetch failure", async () => {
    const calls = memoryCalls();
    calls.listMemories.mockImplementation(
      (_r: unknown, _m: unknown, _o: unknown, cb: CallbackDouble) =>
        cb(Object.assign(new Error("memory service unavailable"), { code: 14 })),
    );
    const { policy } = policyDouble();
    const unit = buildMemoryUnit({
      scope: SCOPE,
      memoryClient: calls as unknown as MemoryServiceCalls,
      callUnary: policy,
    });

    await expect(unit.ready()).rejects.toThrow("memory service unavailable");
  });
});

describe("buildWikiUnit — wiki unit mounted alone", () => {
  function wikiUnit() {
    const calls = wikiCalls();
    const { policy, spy } = policyDouble();
    const unit = buildWikiUnit({
      template: "saolei",
      wikiClient: calls as unknown as WikiServiceCalls,
      callUnary: policy,
    });
    return { unit, calls, policySpy: spy };
  }

  it("produces the wiki face, the three tools and the workflow section", () => {
    const { unit } = wikiUnit();

    expect(unit.faces.map((face) => face.key)).toEqual(["wiki"]);
    expect(unit.tools.map((tool) => tool.name)).toEqual([
      "wiki_search",
      "wiki_get",
      "wiki_write",
    ]);
    expect(unit.sections.map((section) => section.name)).toEqual([
      WIKI_GUIDANCE_SECTION_NAME,
    ]);
    expect(unit.sections.map((section) => section.order)).toEqual([
      WIKI_GUIDANCE_SECTION_ORDER,
    ]);
    expect(unit.sections[0].text).toBe(WIKI_GUIDANCE);
  });

  it("serves its tools through the wiki face key with no memory face in scope", async () => {
    const { unit, calls, policySpy } = wikiUnit();
    const ctx = { wiki: faceOf<WikiFace>(unit, "wiki") };

    expect((await executeWikiSearchTool({ query: "开局" }, makeExec(ctx))).result)
      .toContain("wiki: no titles matched '开局'");

    calls.searchWikis.mockImplementation(
      (_r: unknown, _m: unknown, _o: unknown, cb: CallbackDouble) =>
        cb(null, {
          wikis: [{ name: "templates/saolei/wikis/w1", title: "开局策略" }],
          nextPageToken: "",
        }),
    );
    expect((await executeWikiGetTool({ titles: ["开局策略"] }, makeExec(ctx))).result)
      .toContain("正文");
    expect(calls.searchWikis).toHaveBeenCalledTimes(2);
    expect(calls.getWiki).toHaveBeenCalledTimes(1);
    expect(policySpy).toHaveBeenCalledWith(
      calls,
      "wiki client call",
      expect.any(Function),
    );
  });

  it("ready() is a no-op that touches no client", async () => {
    const { unit, calls } = wikiUnit();

    await unit.ready();

    expect(calls.searchWikis).not.toHaveBeenCalled();
    expect(calls.listWikis).not.toHaveBeenCalled();
  });
});

describe("buildMemoryWikiComposition — equivalence anchor", () => {
  function composition(entries: MemoryEntry[] = []) {
    const memoryCalls_ = memoryCalls(entries);
    const wikiCalls_ = wikiCalls();
    const { policy } = policyDouble();
    const unit = buildMemoryWikiComposition({
      scope: SCOPE,
      memoryClient: memoryCalls_ as unknown as MemoryServiceCalls,
      wikiClient: wikiCalls_ as unknown as WikiServiceCalls,
      callUnary: policy,
    });
    return { unit, memoryCalls: memoryCalls_, wikiCalls: wikiCalls_ };
  }

  it("keeps the equivalence-anchor tool set in order", () => {
    const { unit } = composition();

    expect(unit.tools.map((tool) => tool.name)).toEqual([
      "memory",
      "wiki_search",
      "wiki_get",
      "wiki_write",
    ]);
  });

  it("keeps the equivalence-anchor section names and orders (110/120/200)", () => {
    const { unit } = composition();

    expect(unit.sections.map((section) => section.name)).toEqual([
      "memory:guidance",
      "wiki:guidance",
      "memory:snapshot",
    ]);
    expect(unit.sections.map((section) => section.order)).toEqual([110, 120, 200]);
  });

  it("keeps the equivalence-anchor section texts (division static, workflow static, snapshot live)", () => {
    const { unit } = composition();

    expect(unit.sections[0].text).toBe(MEMORY_GUIDANCE);
    expect(unit.sections[1].text).toBe(WIKI_GUIDANCE);
    expect(typeof unit.sections[2].text).toBe("function");
  });

  it("registers both unit face keys so every tool resolves", async () => {
    const { unit, memoryCalls: memory, wikiCalls: wiki } = composition();
    const ctx = {
      memory: faceOf<MemoryFace>(unit, "memory"),
      wiki: faceOf<WikiFace>(unit, "wiki"),
    };

    expect((await executeMemoryTool({ action: "add", content: "x" }, makeExec(ctx))).result)
      .toBe("memory added");
    expect((await executeWikiSearchTool({ query: "x" }, makeExec(ctx))).result)
      .toContain("wiki: no titles matched 'x'");
    expect(memory.createMemory).toHaveBeenCalledTimes(1);
    expect(wiki.searchWikis).toHaveBeenCalledTimes(1);
  });

  it("ready() is the memory prefetch and the snapshot section reads its result", async () => {
    const { unit, memoryCalls: memory } = composition([
      { memory_id: "m1", content: "洞察" },
    ]);

    await unit.ready();

    expect(memory.listMemories).toHaveBeenCalledTimes(1);
    expect((unit.sections[2].text as () => string)()).toBe("长期记忆：\n洞察");
  });
});
