/**
 * Wiki tool tests — the three model-facing definitions' parameter schemas,
 * execution-time service binding, text-result failure semantics, and the
 * tool→service→operation-core chain (search paging aggregation and the
 * empty-entries guidance) over a fake WikiStore.
 *
 * DI pattern (style/javascript.md §测试): the host service and the tool
 * execution context are injected doubles (`vi.fn()`); no module interception.
 * Contract: specs/068-wiki-round-pause/contracts/dsh-plugins.md §2-3.
 */

import { validateArgs } from "@deepseek-ai/dsh-tools";
import type { ToolRunContext } from "@deepseek-ai/dsh-tools";
import { describe, expect, it, vi } from "vitest";

import {
  WIKI_WRITE_EMPTY_ENTRIES_TEXT,
  normalizeWikiTitle,
  wikiSearch as coreWikiSearch,
  wikiWrite as coreWikiWrite,
} from "@dominion/dsh-memory-service";
import type {
  PlannerMemoryService,
  WikiStore,
} from "@dominion/dsh-memory-service";
import {
  WIKI_GET_TOOL_DESCRIPTION,
  WIKI_GET_TOOL_NAME,
  WIKI_GET_TOOL_PARAMETERS,
  WIKI_SEARCH_TOOL_DESCRIPTION,
  WIKI_SEARCH_TOOL_NAME,
  WIKI_SEARCH_TOOL_PARAMETERS,
  WIKI_WRITE_TOOL_DESCRIPTION,
  WIKI_WRITE_TOOL_NAME,
  WIKI_WRITE_TOOL_PARAMETERS,
  createWikiGetToolDefinition,
  createWikiSearchToolDefinition,
  createWikiWriteToolDefinition,
  executeWikiGetTool,
  executeWikiSearchTool,
  executeWikiWriteTool,
} from "./wiki-tools.js";
import type { WikiToolService } from "./wiki-tools.js";

/** A tool execution carrying only the calling agent (the service is bound at
 * registration, NOT resolved through agent.ctx — see tool.ts). */
function makeExec(agentId = "planner"): ToolRunContext {
  return { agent: { id: agentId, ctx: {} } } as unknown as ToolRunContext;
}

/** The bound service double. */
function makeService(overrides: Partial<
  Pick<PlannerMemoryService, "wikiSearch" | "wikiGet" | "wikiWrite">
> = {}) {
  return {
    wikiSearch: vi.fn(async () => "wiki: 0 title(s) matched 'x':"),
    wikiGet: vi.fn(async () => "wiki: 0 entry(ies) retrieved."),
    wikiWrite: vi.fn(async () => "wiki: 0 entry(ies) applied"),
    ...overrides,
  } as unknown as Pick<PlannerMemoryService, "wikiSearch" | "wikiGet" | "wikiWrite">;
}

describe("wiki tool descriptions (only the tool's own function)", () => {
  it("registers the three names and non-empty English descriptions", () => {
    expect(WIKI_SEARCH_TOOL_NAME).toBe("wiki_search");
    expect(WIKI_GET_TOOL_NAME).toBe("wiki_get");
    expect(WIKI_WRITE_TOOL_NAME).toBe("wiki_write");
    for (const description of [
      WIKI_SEARCH_TOOL_DESCRIPTION,
      WIKI_GET_TOOL_DESCRIPTION,
      WIKI_WRITE_TOOL_DESCRIPTION,
    ]) {
      expect(description.length).toBeGreaterThan(0);
      // English model-facing text: no CJK code points
      // (specs/068-wiki-round-pause/spec.md FR-012/013).
      expect(/[\u4e00-\u9fff]/.test(description)).toBe(false);
    }
  });

  it("does not carry another wiki tool's usage in a description", () => {
    const all = {
      [WIKI_SEARCH_TOOL_NAME]: WIKI_SEARCH_TOOL_DESCRIPTION,
      [WIKI_GET_TOOL_NAME]: WIKI_GET_TOOL_DESCRIPTION,
      [WIKI_WRITE_TOOL_NAME]: WIKI_WRITE_TOOL_DESCRIPTION,
    };
    for (const [name, description] of Object.entries(all)) {
      for (const other of Object.keys(all)) {
        if (other !== name) {
          expect(description).not.toContain(other);
        }
      }
    }
  });
});

describe("wiki tool parameter schemas", () => {
  it("wiki_search declares query as its only parameter", () => {
    expect(Object.keys(WIKI_SEARCH_TOOL_PARAMETERS)).toEqual(["query"]);
  });

  it("wiki_get declares titles as a string array", () => {
    expect(Object.keys(WIKI_GET_TOOL_PARAMETERS)).toEqual(["titles"]);
    expect(WIKI_GET_TOOL_PARAMETERS.titles.type).toBe("array");
    expect(WIKI_GET_TOOL_PARAMETERS.titles.items).toEqual({ type: "string" });
  });

  it("wiki_write declares the single XOR batch form; entries reject unknown keys", () => {
    expect(Object.keys(WIKI_WRITE_TOOL_PARAMETERS).sort()).toEqual([
      "content",
      "entries",
      "title",
    ]);
    const items = WIKI_WRITE_TOOL_PARAMETERS.entries.items;
    expect(items.type).toBe("object");
    expect(items.additionalProperties).toBe(false);
    expect(Object.keys(items.properties).sort()).toEqual(["content", "title"]);
    expect(items.properties.title.required).toBe(true);
    expect(items.properties.content.required).toBe(true);
  });

  it("schema validation matrix: root stays open, entry shape is enforced", () => {
    expect(validateArgs(WIKI_SEARCH_TOOL_PARAMETERS, {})).toEqual([]);
    expect(validateArgs(WIKI_SEARCH_TOOL_PARAMETERS, { query: "x" })).toEqual([]);
    expect(validateArgs(WIKI_GET_TOOL_PARAMETERS, {})).toEqual([]);
    expect(validateArgs(WIKI_GET_TOOL_PARAMETERS, { titles: ["a", "b"] })).toEqual(
      [],
    );
    // An entry with an unknown key is rejected (a typo must not silently
    // apply a different write).
    expect(
      validateArgs(WIKI_WRITE_TOOL_PARAMETERS, {
        entries: [{ title: "a", content: "b", extra: 1 }],
      }),
    ).not.toEqual([]);
    // An entry missing its required fields is rejected.
    expect(
      validateArgs(WIKI_WRITE_TOOL_PARAMETERS, { entries: [{ title: "a" }] }),
    ).not.toEqual([]);
    // The root combination contract stays a runtime text answer.
    expect(
      validateArgs(WIKI_WRITE_TOOL_PARAMETERS, {
        title: "a",
        content: "b",
        entries: [{ title: "c", content: "d" }],
      }),
    ).toEqual([]);
  });
});

describe("createWiki*ToolDefinition", () => {
  it("registers each tool with its name/description/schema and renders {result}", () => {
    const service = makeService();
    const definitions = [
      createWikiSearchToolDefinition(service),
      createWikiGetToolDefinition(service),
      createWikiWriteToolDefinition(service),
    ];

    expect(definitions.map((definition) => definition.name)).toEqual([
      "wiki_search",
      "wiki_get",
      "wiki_write",
    ]);
    expect(definitions[0].description).toBe(WIKI_SEARCH_TOOL_DESCRIPTION);
    expect(definitions[1].description).toBe(WIKI_GET_TOOL_DESCRIPTION);
    expect(definitions[2].description).toBe(WIKI_WRITE_TOOL_DESCRIPTION);
    expect(
      definitions[0].output.render({}, { result: "listed titles" }),
    ).toEqual([{ type: "text", text: "listed titles" }]);
  });
});

describe("executeWiki*Tool (text results, never throws)", () => {
  it("wiki_search passes the query through and returns the aggregated text verbatim", async () => {
    const aggregated = [
      "wiki: 2 title(s) matched '开局':",
      "- 开局-中心优先",
      "- 开局-边角注意",
    ].join("\n");
    const wikiSearch = vi.fn(async () => aggregated);
    const exec = makeExec();

    const result = await executeWikiSearchTool(
      makeService({ wikiSearch }),
      { query: "开局" },
      exec,
    );

    expect(result).toEqual({ result: aggregated });
    expect(wikiSearch).toHaveBeenCalledWith(exec.agent, "开局");
  });

  it("wiki_get passes the titles through and returns the service text", async () => {
    const wikiGet = vi.fn(async () => "content body");
    const exec = makeExec();

    const result = await executeWikiGetTool(
      makeService({ wikiGet }),
      { titles: ["甲", "乙"] },
      exec,
    );

    expect(result).toEqual({ result: "content body" });
    expect(wikiGet).toHaveBeenCalledWith(exec.agent, ["甲", "乙"]);
  });

  it("wiki_write passes both forms through and forwards the empty-entries guidance", async () => {
    const wikiWrite = vi.fn(async () => WIKI_WRITE_EMPTY_ENTRIES_TEXT);
    const exec = makeExec();

    const single = await executeWikiWriteTool(
      makeService({ wikiWrite }),
      { title: "甲", content: "正文" },
      exec,
    );
    expect(single.result).toBe(WIKI_WRITE_EMPTY_ENTRIES_TEXT);
    expect(wikiWrite).toHaveBeenLastCalledWith(exec.agent, {
      title: "甲",
      content: "正文",
      entries: undefined,
    });

    const batch = await executeWikiWriteTool(
      makeService({ wikiWrite }),
      { entries: [{ title: "乙", content: "正文" }] },
      exec,
    );
    expect(batch.result).toBe(WIKI_WRITE_EMPTY_ENTRIES_TEXT);
  });

  it("does NOT resolve the service through agent.ctx (the T023 regression)", async () => {
    const wikiSearch = vi.fn(async () => "ok");
    const exec = { agent: { id: "planner", ctx: {} } } as unknown as ToolRunContext;

    const result = await executeWikiSearchTool(
      makeService({ wikiSearch }),
      { query: "x" },
      exec,
    );

    expect(result).toEqual({ result: "ok" });
    expect(wikiSearch).toHaveBeenCalledTimes(1);
  });

  it("an infrastructure failure becomes `wiki failed: …` TEXT (no throw)", async () => {
    const wikiSearch = vi.fn(async () => {
      throw Object.assign(new Error("memory service unavailable"), { code: 14 });
    });

    const result = await executeWikiSearchTool(
      makeService({ wikiSearch }),
      { query: "x" },
      makeExec(),
    );

    expect(result).toEqual({
      result: "wiki failed: memory service unavailable",
    });
  });

  it("a missing agent caller becomes TEXT (no throw)", async () => {
    const result = await executeWikiSearchTool(
      makeService(),
      { query: "x" },
      {} as ToolRunContext,
    );

    expect(result.result).toContain("wiki failed:");
    expect(result.result).toContain("no agent on the tool execution");
  });
});

describe("wiki tools over the real operation core (fake WikiStore)", () => {
  /**
   * A fake WikiStore that hands the operation core a two-page search result,
   * so the tool-level test proves `wiki_search` folds `next_page_token` with
   * `page_size` 100 end to end (and `wiki_write` short-circuits an empty
   * `entries` list before any write).
   */
  function makeFakeWikiStore() {
    const wikis = new Map<string, { title: string; content: string }>();
    return {
      wikis,
      searchWikis: vi.fn(
        async (
          _t: string,
          _s: string,
          _query: string,
          options?: { pageSize?: number; pageToken?: string },
        ) => {
          if (!options?.pageToken) {
            return {
              wikis: [
                { name: "templates/saolei/sessions/sess-1/wikis/w1", title: "开局-中心优先" },
                { name: "templates/saolei/sessions/sess-1/wikis/w2", title: "开局-边角注意" },
              ],
              nextPageToken: "p2",
            };
          }
          return {
            wikis: [
              { name: "templates/saolei/sessions/sess-1/wikis/w3", title: "开局-复盘要点" },
            ],
            nextPageToken: "",
          };
        },
      ),
      getWiki: vi.fn(),
      createWiki: vi.fn(
        async (_t: string, _s: string, id: string, title: string, content: string) => {
          wikis.set(id, { title, content });
        },
      ),
      updateWiki: vi.fn(),
    };
  }

  function coreService(store: ReturnType<typeof makeFakeWikiStore>): WikiToolService {
    const wiki = store as unknown as WikiStore;
    return {
      wikiSearch: (_agent, query) =>
        coreWikiSearch(wiki, "saolei", "sess-1", query),
      wikiGet: async () => "wiki: 0 entry(ies) retrieved.",
      wikiWrite: (_agent, input) => coreWikiWrite(wiki, "saolei", "sess-1", input),
    };
  }

  it("wiki_search aggregates every page before rendering the titles", async () => {
    const store = makeFakeWikiStore();

    const result = await executeWikiSearchTool(
      coreService(store),
      { query: "开局" },
      makeExec(),
    );

    expect(store.searchWikis).toHaveBeenCalledTimes(2);
    expect(store.searchWikis).toHaveBeenNthCalledWith(
      1,
      "saolei",
      "sess-1",
      "开局",
      { pageSize: 100, pageToken: undefined },
    );
    expect(store.searchWikis).toHaveBeenNthCalledWith(
      2,
      "saolei",
      "sess-1",
      "开局",
      { pageSize: 100, pageToken: "p2" },
    );
    expect(result.result).toContain("开局-中心优先");
    expect(result.result).toContain("开局-复盘要点");
  });

  it("wiki_write returns the empty-entries guidance without writing", async () => {
    const store = makeFakeWikiStore();

    const result = await executeWikiWriteTool(
      coreService(store),
      { entries: [] },
      makeExec(),
    );

    expect(result.result).toBe(WIKI_WRITE_EMPTY_ENTRIES_TEXT);
    expect(store.createWiki).not.toHaveBeenCalled();
    expect(store.updateWiki).not.toHaveBeenCalled();
  });

  it("wiki_write resolves a create through the deterministic title digest", async () => {
    const store = makeFakeWikiStore();

    const result = await executeWikiWriteTool(
      coreService(store),
      { title: "新策略", content: "正文" },
      makeExec(),
    );

    expect(result.result).toContain("created: '新策略'");
    expect(store.createWiki).toHaveBeenCalledWith(
      "saolei",
      "sess-1",
      expect.stringMatching(/^[a-f0-9]{32}$/),
      "新策略",
      "正文",
    );
    // The digest is derived from the normalized title (not sent to the model).
    const [, , id] = store.createWiki.mock.calls[0] as [string, string, string];
    expect(store.wikis.has(id)).toBe(true);
    expect(normalizeWikiTitle("新策略")).toBe("新策略");
  });
});
