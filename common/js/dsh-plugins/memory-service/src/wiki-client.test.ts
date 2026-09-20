/**
 * WikiClient tests — the WikiStore gRPC pass-through. The DI seam is the
 * injected gRPC client, so every RPC test passes a `vi.fn()`-backed fake and
 * needs no module-level `vi.mock` (style/javascript.md §测试).
 *
 * Coverage: name construction/parsing, the request shapes against the
 * WikiService contract (specs/069-wiki-template-web/contracts/dsh-plugins.md §3,
 * specs/069-wiki-template-web/contracts/wiki-service.md §2), the lightweight
 * `{name, title}` search decoding, list decoding, and error propagation.
 */

import { beforeEach, describe, expect, it, vi } from "vitest";

import { WikiClient, parseWikiName, wikiName } from "./wiki-client.js";

/**
 * The real gRPC stub contract is `rpc(request, metadata, options, callback)`;
 * fakes must match that 4-arg shape so the 4th positional arg is the callback.
 */
interface FakeRpc {
  searchWikis: ReturnType<typeof vi.fn>;
  getWiki: ReturnType<typeof vi.fn>;
  createWiki: ReturnType<typeof vi.fn>;
  updateWiki: ReturnType<typeof vi.fn>;
  deleteWiki: ReturnType<typeof vi.fn>;
  listWikis: ReturnType<typeof vi.fn>;
  close: ReturnType<typeof vi.fn>;
}

function makeFakeClient(): FakeRpc {
  return {
    searchWikis: vi.fn(),
    getWiki: vi.fn(),
    createWiki: vi.fn(),
    updateWiki: vi.fn(),
    deleteWiki: vi.fn(),
    listWikis: vi.fn(),
    close: vi.fn(),
  };
}

/** Resolve a fake RPC successfully (or reject with `err` when given). */
function respond(
  fn: ReturnType<typeof vi.fn>,
  response: unknown,
  err?: Error,
): void {
  fn.mockImplementation(
    (
      _req: unknown,
      _metadata: unknown,
      _options: { deadline: Date },
      cb: (e: Error | null, r: unknown) => void,
    ) => {
      cb(err ?? null, response);
    },
  );
}

describe("WikiClient", () => {
  let fake: FakeRpc;

  beforeEach(() => {
    fake = makeFakeClient();
  });

  describe("wikiName/parseWikiName (construction and parsing)", () => {
    it("builds templates/{template}/wikis/{wikiId}", () => {
      expect(wikiName("saolei", "wiki-abc")).toBe(
        "templates/saolei/wikis/wiki-abc",
      );
    });

    it("parseWikiName inverts wikiName (round trip)", () => {
      expect(parseWikiName("templates/saolei/wikis/wiki-abc")).toBe(
        "wiki-abc",
      );
      expect(parseWikiName(wikiName("saolei", "wiki-abc"))).toBe("wiki-abc");
    });
  });

  describe("searchWikis", () => {
    it("returns {name, title} summaries and forwards query/pageSize/pageToken", async () => {
      respond(fake.searchWikis, {
        wikis: [
          {
            name: "templates/saolei/wikis/w1",
            title: "开局策略",
          },
        ],
        nextPageToken: "p2",
      });
      const client = new WikiClient(fake as never);

      const page = await client.searchWikis("saolei", "开局", {
        pageSize: 100,
        pageToken: "p1",
      });

      expect(page).toEqual({
        wikis: [
          {
            name: "templates/saolei/wikis/w1",
            title: "开局策略",
          },
        ],
        nextPageToken: "p2",
      });
      expect(fake.searchWikis).toHaveBeenCalledTimes(1);
      expect(fake.searchWikis).toHaveBeenCalledWith(
        {
          parent: "templates/saolei",
          query: "开局",
          pageToken: "p1",
          pageSize: 100,
        },
        expect.any(Object),
        expect.any(Object),
        expect.any(Function),
      );
    });

    it("omits pageSize/pageToken when they are not supplied", async () => {
      respond(fake.searchWikis, { wikis: [], nextPageToken: "" });
      const client = new WikiClient(fake as never);

      const page = await client.searchWikis("saolei", "x");

      expect(page).toEqual({ wikis: [], nextPageToken: "" });
      expect(fake.searchWikis).toHaveBeenCalledWith(
        {
          parent: "templates/saolei",
          query: "x",
          pageToken: undefined,
        },
        expect.any(Object),
        expect.any(Object),
        expect.any(Function),
      );
    });

    it("drops entries missing name or title (wire-shape guard)", async () => {
      respond(fake.searchWikis, {
        wikis: [
          { name: "templates/saolei/wikis/w1", title: "甲" },
          { title: "无name" },
          { name: "templates/saolei/wikis/w2" },
        ],
        nextPageToken: "",
      });
      const client = new WikiClient(fake as never);

      const page = await client.searchWikis("saolei", "x");

      expect(page.wikis).toEqual([
        { name: "templates/saolei/wikis/w1", title: "甲" },
      ]);
    });

    it("rejects with the gRPC error", async () => {
      respond(
        fake.searchWikis,
        null,
        Object.assign(new Error("memory service unavailable"), { code: 14 }),
      );
      const client = new WikiClient(fake as never);

      await expect(client.searchWikis("saolei", "x")).rejects.toThrow(
        "memory service unavailable",
      );
    });
  });

  describe("getWiki", () => {
    it("calls GetWiki by name and returns the content", async () => {
      respond(fake.getWiki, {
        name: "templates/saolei/wikis/w1",
        title: "开局策略",
        content: "正文",
      });
      const client = new WikiClient(fake as never);

      const wiki = await client.getWiki("saolei", "w1");

      expect(wiki).toEqual({
        name: "templates/saolei/wikis/w1",
        title: "开局策略",
        content: "正文",
      });
      expect(fake.getWiki).toHaveBeenCalledWith(
        { name: "templates/saolei/wikis/w1" },
        expect.any(Object),
        expect.any(Object),
        expect.any(Function),
      );
    });

    it("rejects with the gRPC error (NOT_FOUND)", async () => {
      respond(
        fake.getWiki,
        null,
        Object.assign(new Error("wiki not found"), { code: 5 }),
      );
      const client = new WikiClient(fake as never);

      await expect(client.getWiki("saolei", "w1")).rejects.toThrow(
        "wiki not found",
      );
    });
  });

  describe("createWiki", () => {
    it("calls CreateWiki with the embedded Wiki body + wiki_id (AIP-133 shape)", async () => {
      respond(fake.createWiki, {});
      const client = new WikiClient(fake as never);

      await client.createWiki("saolei", "w1", "开局策略", "正文");

      expect(fake.createWiki).toHaveBeenCalledTimes(1);
      expect(fake.createWiki).toHaveBeenCalledWith(
        {
          parent: "templates/saolei",
          wikiId: "w1",
          wiki: {
            name: "templates/saolei/wikis/w1",
            title: "开局策略",
            content: "正文",
          },
        },
        expect.any(Object),
        expect.any(Object),
        expect.any(Function),
      );
    });

    it("rejects with the gRPC error (ALREADY_EXISTS)", async () => {
      respond(
        fake.createWiki,
        null,
        Object.assign(new Error("wiki already exists"), { code: 6 }),
      );
      const client = new WikiClient(fake as never);

      await expect(
        client.createWiki("saolei", "w1", "标题", "正文"),
      ).rejects.toThrow("wiki already exists");
    });
  });

  describe("updateWiki", () => {
    it("calls UpdateWiki with Wiki{name, content} + a content-only FieldMask", async () => {
      respond(fake.updateWiki, {});
      const client = new WikiClient(fake as never);

      await client.updateWiki("saolei", "w1", { content: "新正文" });

      expect(fake.updateWiki).toHaveBeenCalledWith(
        {
          wiki: {
            name: "templates/saolei/wikis/w1",
            content: "新正文",
          },
          updateMask: { paths: ["content"] },
        },
        expect.any(Object),
        expect.any(Object),
        expect.any(Function),
      );
    });

    it("rejects with the gRPC error (NOT_FOUND)", async () => {
      respond(
        fake.updateWiki,
        null,
        Object.assign(new Error("wiki not found"), { code: 5 }),
      );
      const client = new WikiClient(fake as never);

      await expect(
        client.updateWiki("saolei", "w1", { content: "新正文" }),
      ).rejects.toThrow("wiki not found");
    });
  });

  describe("deleteWiki", () => {
    it("calls DeleteWiki by name (AIP-135 shape)", async () => {
      respond(fake.deleteWiki, {});
      const client = new WikiClient(fake as never);

      await client.deleteWiki("saolei", "w1");

      expect(fake.deleteWiki).toHaveBeenCalledWith(
        { name: "templates/saolei/wikis/w1" },
        expect.any(Object),
        expect.any(Object),
        expect.any(Function),
      );
    });

    it("rejects with the gRPC error (NOT_FOUND)", async () => {
      respond(
        fake.deleteWiki,
        null,
        Object.assign(new Error("wiki not found"), { code: 5 }),
      );
      const client = new WikiClient(fake as never);

      await expect(client.deleteWiki("saolei", "w1")).rejects.toThrow(
        "wiki not found",
      );
    });
  });

  describe("listWikis", () => {
    it("returns full resources and forwards pageSize/pageToken", async () => {
      respond(fake.listWikis, {
        wikis: [
          {
            name: "templates/saolei/wikis/w1",
            title: "开局策略",
            content: "正文",
          },
        ],
        nextPageToken: "p2",
      });
      const client = new WikiClient(fake as never);

      const page = await client.listWikis("saolei", {
        pageSize: 20,
        pageToken: "p1",
      });

      expect(page).toEqual({
        wikis: [
          {
            name: "templates/saolei/wikis/w1",
            title: "开局策略",
            content: "正文",
          },
        ],
        nextPageToken: "p2",
      });
      expect(fake.listWikis).toHaveBeenCalledWith(
        {
          parent: "templates/saolei",
          pageToken: "p1",
          pageSize: 20,
        },
        expect.any(Object),
        expect.any(Object),
        expect.any(Function),
      );
    });

    it("omits pageSize/pageToken when they are not supplied", async () => {
      respond(fake.listWikis, { wikis: [], nextPageToken: "" });
      const client = new WikiClient(fake as never);

      const page = await client.listWikis("saolei");

      expect(page).toEqual({ wikis: [], nextPageToken: "" });
      expect(fake.listWikis).toHaveBeenCalledWith(
        {
          parent: "templates/saolei",
          pageToken: undefined,
        },
        expect.any(Object),
        expect.any(Object),
        expect.any(Function),
      );
    });

    it("drops entries missing name or title and defaults missing content", async () => {
      respond(fake.listWikis, {
        wikis: [
          { name: "templates/saolei/wikis/w1", title: "甲" },
          { title: "无name" },
          { name: "templates/saolei/wikis/w2", content: "只有正文" },
        ],
        nextPageToken: "",
      });
      const client = new WikiClient(fake as never);

      const page = await client.listWikis("saolei");

      expect(page.wikis).toEqual([
        { name: "templates/saolei/wikis/w1", title: "甲", content: "" },
      ]);
    });

    it("rejects with the gRPC error", async () => {
      respond(
        fake.listWikis,
        null,
        Object.assign(new Error("memory service unavailable"), { code: 14 }),
      );
      const client = new WikiClient(fake as never);

      await expect(client.listWikis("saolei")).rejects.toThrow(
        "memory service unavailable",
      );
    });
  });

  describe("close", () => {
    it("closes the underlying gRPC client", () => {
      const client = new WikiClient(fake as never);
      client.close();
      expect(fake.close).toHaveBeenCalledTimes(1);
    });
  });
});
