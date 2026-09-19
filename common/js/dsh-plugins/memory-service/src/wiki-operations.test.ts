/**
 * Wiki operation-core tests — the semantics behind the three wiki tools:
 * title search folding, exact-title resolution (normalization alignment with
 * the service), upsert-by-title, batch atomicity, and the text-guidance
 * outcomes. The storage is a `vi.fn()`-backed in-memory fake (DI seam —
 * style/javascript.md §测试); no module interception.
 *
 * Contract: specs/068-wiki-round-pause/contracts/dsh-plugins.md §2;
 * specs/068-wiki-round-pause/data-model.md §5-7.
 */

import { beforeEach, describe, expect, it, vi } from "vitest";

import { wikiName } from "./wiki-client.js";
import type { WikiStore } from "./wiki-client.js";
import {
  WIKI_CONTENT_MAX_BYTES,
  WIKI_GET_EMPTY_TITLES_TEXT,
  WIKI_SEARCH_EMPTY_QUERY_TEXT,
  WIKI_SEARCH_PAGE_SIZE,
  WIKI_TITLE_MAX_RUNES,
  WIKI_WRITE_AMBIGUOUS_ARGS_TEXT,
  WIKI_WRITE_EMPTY_ENTRIES_TEXT,
  WIKI_WRITE_MISSING_ARGS_TEXT,
  generateWikiId,
  normalizeWikiTitle,
  wikiGet,
  wikiSearch,
  wikiWrite,
} from "./wiki-operations.js";

const TEMPLATE = "saolei";
const SESSION = "sess-1";

/** In-memory WikiStore double with the service's normalized-substring search. */
function makeFakeWikiStore() {
  const wikis = new Map<string, { title: string; content: string }>();
  return {
    wikis,
    createWiki: vi.fn(
      async (_t: string, _s: string, id: string, title: string, content: string) => {
        wikis.set(id, { title, content });
      },
    ),
    updateWiki: vi.fn(
      async (_t: string, _s: string, id: string, content: string) => {
        const wiki = wikis.get(id);
        if (wiki === undefined) {
          throw Object.assign(new Error(`wiki not found: ${id}`), { code: 5 });
        }
        wiki.content = content;
      },
    ),
    getWiki: vi.fn(async (t: string, s: string, id: string) => {
      const wiki = wikis.get(id);
      if (wiki === undefined) {
        throw Object.assign(new Error(`wiki not found: ${id}`), { code: 5 });
      }
      return { name: wikiName(t, s, id), title: wiki.title, content: wiki.content };
    }),
    searchWikis: vi.fn(async (t: string, s: string, query: string) => {
      const q = normalizeWikiTitle(query);
      const hits = [...wikis.entries()]
        .filter(([, wiki]) => normalizeWikiTitle(wiki.title).includes(q))
        .map(([id, wiki]) => ({ name: wikiName(t, s, id), title: wiki.title }));
      return { wikis: hits, nextPageToken: "" };
    }),
  };
}

type FakeStore = ReturnType<typeof makeFakeWikiStore>;

function seed(store: FakeStore, title: string, content: string): string {
  const id = generateWikiId(normalizeWikiTitle(title));
  store.wikis.set(id, { title, content });
  return id;
}

function search(
  store: FakeStore,
  query: string,
): Promise<string> {
  return wikiSearch(store as unknown as WikiStore, TEMPLATE, SESSION, query);
}

function get(store: FakeStore, titles: string[]): Promise<string> {
  return wikiGet(store as unknown as WikiStore, TEMPLATE, SESSION, titles);
}

function write(
  store: FakeStore,
  args: Parameters<typeof wikiWrite>[3],
): Promise<string> {
  return wikiWrite(store as unknown as WikiStore, TEMPLATE, SESSION, args);
}

describe("normalizeWikiTitle / generateWikiId", () => {
  it("normalizes full-width and case to the service's NFKC + lower form", () => {
    expect(normalizeWikiTitle("ＡＢＣ")).toBe("abc");
    expect(normalizeWikiTitle("Abc 全角")).toBe("abc 全角");
    expect(normalizeWikiTitle("中文标题")).toBe("中文标题");
  });

  it("generates a deterministic 32-hex id from the normalized title", () => {
    const id = generateWikiId(normalizeWikiTitle("开局策略"));
    expect(id).toMatch(/^[a-f0-9]{32}$/);
    expect(generateWikiId(normalizeWikiTitle("开局策略"))).toBe(id);
    // Case/full-width variants normalize to the same title ⇒ same id.
    expect(generateWikiId(normalizeWikiTitle("ＡＢＣ"))).toBe(
      generateWikiId(normalizeWikiTitle("abc")),
    );
  });
});

describe("wikiSearch", () => {
  let store: FakeStore;

  beforeEach(() => {
    store = makeFakeWikiStore();
  });

  it("folds next_page_token with page_size=100 and returns every matching title", async () => {
    store.searchWikis.mockImplementation(
      async (
        t: string,
        s: string,
        _query: string,
        options?: { pageSize?: number; pageToken?: string },
      ) => {
        if (!options?.pageToken) {
          return {
            wikis: [
              { name: wikiName(t, s, "w1"), title: "开局-中心优先" },
              { name: wikiName(t, s, "w2"), title: "开局-边角注意" },
            ],
            nextPageToken: "p2",
          };
        }
        return {
          wikis: [{ name: wikiName(t, s, "w3"), title: "开局-复盘要点" }],
          nextPageToken: "",
        };
      },
    );

    const result = await search(store, "开局");

    expect(store.searchWikis).toHaveBeenCalledTimes(2);
    expect(store.searchWikis).toHaveBeenNthCalledWith(
      1,
      TEMPLATE,
      SESSION,
      "开局",
      { pageSize: WIKI_SEARCH_PAGE_SIZE, pageToken: undefined },
    );
    expect(store.searchWikis).toHaveBeenNthCalledWith(
      2,
      TEMPLATE,
      SESSION,
      "开局",
      { pageSize: WIKI_SEARCH_PAGE_SIZE, pageToken: "p2" },
    );
    expect(result).toContain("开局-中心优先");
    expect(result).toContain("开局-边角注意");
    expect(result).toContain("开局-复盘要点");
    expect(result).toContain("3 title(s) matched");
  });

  it("deduplicates a title re-surfaced across pages by resource name", async () => {
    const hit = { name: wikiName(TEMPLATE, SESSION, "w1"), title: "重复标题" };
    store.searchWikis
      .mockResolvedValueOnce({ wikis: [hit], nextPageToken: "p2" })
      .mockResolvedValueOnce({ wikis: [hit], nextPageToken: "" });

    const result = await search(store, "重复");

    expect(result).toContain("1 title(s) matched");
    expect(result.match(/重复标题/g)).toHaveLength(1);
  });

  it("returns guidance for an empty result (never an error)", async () => {
    const result = await search(store, "不存在的关键词");
    expect(result).toContain("no titles matched '不存在的关键词'");
    expect(result).toContain("keyword");
  });

  it("returns guidance for an empty query without calling the store", async () => {
    const result = await search(store, "   ");
    expect(result).toBe(WIKI_SEARCH_EMPTY_QUERY_TEXT);
    expect(store.searchWikis).not.toHaveBeenCalled();
  });
});

describe("wikiGet", () => {
  let store: FakeStore;

  beforeEach(() => {
    store = makeFakeWikiStore();
  });

  it("resolves a title by normalized exact match and returns its content", async () => {
    seed(store, "ＡＢＣ 策略", "正文一");

    const result = await get(store, ["abc 策略"]);

    expect(result).toContain("## ＡＢＣ 策略");
    expect(result).toContain("正文一");
    expect(result).not.toContain("not found");
  });

  it("returns hits and lists the misses with search guidance", async () => {
    seed(store, "开局策略", "正文一");

    const result = await get(store, ["开局策略", "不存在的标题"]);

    expect(result).toContain("## 开局策略");
    expect(result).toContain("正文一");
    expect(result).toContain("not found: '不存在的标题'");
    expect(result).toContain("wiki_search");
  });

  it("does not return a search substring match as an exact hit", async () => {
    seed(store, "开局策略", "正文一");

    const result = await get(store, ["开局"]);

    expect(result).not.toContain("正文一");
    expect(result).toContain("not found: '开局'");
  });

  it("returns guidance for an empty titles list without any store call", async () => {
    const result = await get(store, []);
    expect(result).toBe(WIKI_GET_EMPTY_TITLES_TEXT);
    expect(store.searchWikis).not.toHaveBeenCalled();
  });

  it("lists a whitespace-only title in its trimmed form (not the raw padding)", async () => {
    const result = await get(store, ["   "]);

    expect(result).toContain("not found: ''");
    expect(result).not.toContain("'  '");
    expect(store.searchWikis).not.toHaveBeenCalled();
  });

  it("never renders the resource name or wiki_id", async () => {
    const id = seed(store, "开局策略", "正文一");

    const result = await get(store, ["开局策略"]);

    expect(result).not.toContain("templates/");
    expect(result).not.toContain(id);
  });
});

describe("wikiWrite", () => {
  let store: FakeStore;

  beforeEach(() => {
    store = makeFakeWikiStore();
  });

  it("creates an absent entry and reports it as created", async () => {
    const result = await write(store, { title: "新策略", content: "正文" });

    expect(store.createWiki).toHaveBeenCalledWith(
      TEMPLATE,
      SESSION,
      generateWikiId(normalizeWikiTitle("新策略")),
      "新策略",
      "正文",
    );
    expect(store.updateWiki).not.toHaveBeenCalled();
    expect(result).toContain("created: '新策略'");
    expect(result).not.toContain("templates/");
  });

  it("upserts by normalized title: an existing entry is updated, not duplicated", async () => {
    seed(store, "策略Ａ", "旧正文");

    const result = await write(store, { title: "策略a", content: "新正文" });

    expect(store.createWiki).not.toHaveBeenCalled();
    expect(store.updateWiki).toHaveBeenCalledWith(
      TEMPLATE,
      SESSION,
      generateWikiId(normalizeWikiTitle("策略Ａ")),
      "新正文",
    );
    expect(store.wikis.size).toBe(1);
    expect(result).toContain("updated: '策略a'");
  });

  it("batch: mixes created and updated entries and reports each", async () => {
    seed(store, "已有条目", "旧正文");

    const result = await write(store, {
      entries: [
        { title: "已有条目", content: "更新正文" },
        { title: "全新条目", content: "新建正文" },
      ],
    });

    expect(store.wikis.size).toBe(2);
    expect(result).toContain("updated: '已有条目'");
    expect(result).toContain("created: '全新条目'");
  });

  it("batch: an entry with an empty title aborts the whole batch (zero writes)", async () => {
    const result = await write(store, {
      entries: [
        { title: "合法条目", content: "正文" },
        { title: "   ", content: "正文" },
      ],
    });

    expect(result).toContain("entry 2 failed: title is required");
    expect(result).toContain("all-or-nothing");
    expect(store.createWiki).not.toHaveBeenCalled();
    expect(store.updateWiki).not.toHaveBeenCalled();
  });

  it("batch: an over-limit title aborts the whole batch (zero writes)", async () => {
    const result = await write(store, {
      entries: [
        { title: "合法条目", content: "正文" },
        { title: "t".repeat(WIKI_TITLE_MAX_RUNES + 1), content: "正文" },
      ],
    });

    expect(result).toContain("entry 2 failed");
    expect(result).toContain(`exceeds ${WIKI_TITLE_MAX_RUNES} characters`);
    expect(store.createWiki).not.toHaveBeenCalled();
  });

  it("batch: an over-limit content aborts the whole batch (zero writes)", async () => {
    const result = await write(store, {
      entries: [
        {
          title: "合法条目",
          content: "a".repeat(WIKI_CONTENT_MAX_BYTES + 1),
        },
      ],
    });

    expect(result).toContain("content exceeds");
    expect(store.createWiki).not.toHaveBeenCalled();
  });

  it("batch: an empty content entry aborts the whole batch", async () => {
    const result = await write(store, {
      entries: [{ title: "条目", content: "  " }],
    });

    expect(result).toContain("entry 1 failed: content is required");
    expect(store.createWiki).not.toHaveBeenCalled();
  });

  it("single form: a validation failure uses neutral, non-batch wording", async () => {
    const missingContent = await write(store, { title: "条目" });
    expect(missingContent).toBe("wiki: content is required. Nothing was written.");
    expect(missingContent).not.toContain("entry 1");
    expect(missingContent).not.toContain("batch");

    const emptyTitle = await write(store, { title: "   ", content: "正文" });
    expect(emptyTitle).toBe("wiki: title is required. Nothing was written.");
    expect(emptyTitle).not.toContain("batch");

    const overLimit = await write(store, {
      title: "t".repeat(WIKI_TITLE_MAX_RUNES + 1),
      content: "正文",
    });
    expect(overLimit).toContain(
      `wiki: title exceeds ${WIKI_TITLE_MAX_RUNES} characters. Nothing was written.`,
    );
    expect(overLimit).not.toContain("batch");
    expect(store.createWiki).not.toHaveBeenCalled();
  });

  it("returns guidance for an empty entries list without committing", async () => {
    const result = await write(store, { entries: [] });
    expect(result).toBe(WIKI_WRITE_EMPTY_ENTRIES_TEXT);
    expect(store.searchWikis).not.toHaveBeenCalled();
    expect(store.createWiki).not.toHaveBeenCalled();
  });

  it("refuses both forms and neither form as text (no store call)", async () => {
    const both = await write(store, {
      title: "条目",
      content: "正文",
      entries: [{ title: "条目", content: "正文" }],
    });
    expect(both).toBe(WIKI_WRITE_AMBIGUOUS_ARGS_TEXT);

    const neither = await write(store, {});
    expect(neither).toBe(WIKI_WRITE_MISSING_ARGS_TEXT);

    expect(store.createWiki).not.toHaveBeenCalled();
  });

  it("propagates an infrastructure failure (the tool maps it to `wiki failed`)", async () => {
    store.searchWikis.mockRejectedValue(
      Object.assign(new Error("memory service unavailable"), { code: 14 }),
    );

    await expect(
      write(store, { title: "条目", content: "正文" }),
    ).rejects.toThrow("memory service unavailable");
  });
});
