import { describe, expect, it, vi } from "vitest";
import type { EndpointResolver } from "@dominion/common-js-resolver";

import {
  MONGO_TARGET,
  MongoPresetStore,
  PresetStoreError,
  resolveMongoUri,
} from "./presets.js";
import type { PresetCollection, PresetDocument, PresetFindOptions, PresetFilter } from "./presets.js";

/**
 * PresetStore unit tests over an injected collection double (DI seam,
 * style/javascript.md Mock convention — vi.fn() doubles, no module
 * interception): write targets, keyset pagination shape, and the AIP error
 * mapping (ALREADY_EXISTS on duplicate name, NOT_FOUND on absent rows).
 */

function record(overrides: Partial<Parameters<MongoPresetStore["create"]>[0]> = {}) {
  return {
    name: "templates/saolei/presets/p1",
    playerPrompt: "play carefully",
    createTime: new Date(1000),
    updateTime: new Date(2000),
    ...overrides,
  };
}

function document(overrides: Partial<PresetDocument> = {}): PresetDocument {
  return {
    name: "templates/saolei/presets/p1",
    player_prompt: "play carefully",
    create_time: new Date(1000),
    update_time: new Date(2000),
    ...overrides,
  };
}

type FindCall = { filter: PresetFilter; options: PresetFindOptions };

function fakeCollection(documents: PresetDocument[] = []) {
  return {
    insertOne: vi.fn(async () => ({ insertedId: "db-generated" })),
    findOne: vi.fn(async (filter: PresetFilter) =>
      documents.find((d) => d.name === filter.name) ?? null,
    ),
    replaceOne: vi.fn(async () => ({ matchedCount: 1 })),
    deleteOne: vi.fn(async () => ({ deletedCount: 1 })),
    find: vi.fn((filter: PresetFilter, options: PresetFindOptions) => ({
      toArray: async () => [...documents],
    })),
    createIndex: vi.fn(async () => "name_1"),
  };
}

type FakeCollection = ReturnType<typeof fakeCollection>;

function asStore(fake: FakeCollection): { store: MongoPresetStore; fake: FakeCollection } {
  return { store: new MongoPresetStore(fake as unknown as PresetCollection), fake };
}

describe("resolveMongoUri", () => {
  it("prefers a direct MONGO_URI without contacting the resolver", async () => {
    const resolve = vi.fn(async () => ["10.0.0.9:27017"]);

    const uri = await resolveMongoUri({
      env: { MONGO_URI: "mongodb://direct:27017" },
      resolver: { resolve } as unknown as EndpointResolver,
    });

    expect(uri).toBe("mongodb://direct:27017");
    expect(resolve).not.toHaveBeenCalled();
  });

  it("resolves the Dominion mongo target into a credentialed mongodb URI", async () => {
    const resolve = vi.fn(async () => ["10.0.0.9:27017"]);

    const uri = await resolveMongoUri({
      env: {},
      resolver: { resolve } as unknown as EndpointResolver,
    });

    expect(resolve).toHaveBeenCalledWith(MONGO_TARGET);
    // The password is the deterministic deployment derivation (same-source
    // with dominion/common/gopkg/mongo/credentials.go; the cross-implementation
    // match is pinned by that package's client_test.go vectors) for the
    // default environment.
    expect(uri).toBe(
      "mongodb://admin:JaOE4KM29XdamfOs9zUqhC2QHavC2UJn@10.0.0.9:27017/admin?authSource=admin",
    );
  });

  it("derives the credential from DOMINION_ENVIRONMENT", async () => {
    const resolve = vi.fn(async () => ["10.0.0.9:27017"]);

    const uri = await resolveMongoUri({
      env: { DOMINION_ENVIRONMENT: "test-env" },
      resolver: { resolve } as unknown as EndpointResolver,
    });

    expect(uri).toBe(
      "mongodb://admin:iiG62he1f7TPRHuY7ooNT2uVfVgJ4fKN@10.0.0.9:27017/admin?authSource=admin",
    );
  });

  it("fails loud when the resolver returns no endpoints", async () => {
    const resolve = vi.fn(async () => []);

    await expect(
      resolveMongoUri({ env: {}, resolver: { resolve } as unknown as EndpointResolver }),
    ).rejects.toThrow(/no endpoints/);
  });
});

describe("MongoPresetStore.create", () => {
  it("inserts the document without an _id (database-generated, style/mongo.md)", async () => {
    const { store, fake } = asStore(fakeCollection());

    await store.create(record());

    expect(fake.insertOne).toHaveBeenCalledOnce();
    const [doc] = fake.insertOne.mock.calls[0] as unknown as [PresetDocument];
    expect(doc).toEqual({
      name: "templates/saolei/presets/p1",
      player_prompt: "play carefully",
      create_time: new Date(1000),
      update_time: new Date(2000),
    });
    expect(doc).not.toHaveProperty("_id");
  });

  it("maps a duplicate-key write error to ALREADY_EXISTS", async () => {
    const fake = fakeCollection();
    fake.insertOne.mockRejectedValueOnce(
      Object.assign(new Error("E11000 duplicate key"), { code: 11000 }),
    );
    const { store } = asStore(fake);

    const err = await store.create(record()).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(PresetStoreError);
    expect((err as PresetStoreError).code).toBe("ALREADY_EXISTS");
  });

  it("re-throws non-duplicate write errors untouched", async () => {
    const fake = fakeCollection();
    fake.insertOne.mockRejectedValueOnce(new Error("connection refused"));
    const { store } = asStore(fake);

    await expect(store.create(record())).rejects.toThrow(/connection refused/);
  });
});

describe("MongoPresetStore.get", () => {
  it("projects the stored document back to the resource record", async () => {
    const { store, fake } = asStore(fakeCollection([document()]));

    const got = await store.get("templates/saolei/presets/p1");

    expect(got).toEqual(record());
    expect(fake.findOne).toHaveBeenCalledWith({ name: "templates/saolei/presets/p1" });
  });

  it("maps a missing document to NOT_FOUND", async () => {
    const { store } = asStore(fakeCollection());

    const err = await store.get("templates/saolei/presets/absent").catch((e: unknown) => e);

    expect(err).toBeInstanceOf(PresetStoreError);
    expect((err as PresetStoreError).code).toBe("NOT_FOUND");
  });
});

describe("MongoPresetStore.update", () => {
  it("replaces the row matched by name", async () => {
    const { store, fake } = asStore(fakeCollection());

    await store.update(record({ updateTime: new Date(3000) }));

    expect(fake.replaceOne).toHaveBeenCalledOnce();
    const [filter, doc] = fake.replaceOne.mock.calls[0] as unknown as [PresetFilter, PresetDocument];
    expect(filter).toEqual({ name: "templates/saolei/presets/p1" });
    expect(doc.update_time).toEqual(new Date(3000));
  });

  it("maps a zero-match replace to NOT_FOUND", async () => {
    const fake = fakeCollection();
    fake.replaceOne.mockResolvedValueOnce({ matchedCount: 0 });
    const { store } = asStore(fake);

    const err = await store.update(record()).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(PresetStoreError);
    expect((err as PresetStoreError).code).toBe("NOT_FOUND");
  });
});

describe("MongoPresetStore.delete", () => {
  it("maps a zero-deletion result to NOT_FOUND", async () => {
    const fake = fakeCollection();
    fake.deleteOne.mockResolvedValueOnce({ deletedCount: 0 });
    const { store } = asStore(fake);

    const err = await store.delete("templates/saolei/presets/absent").catch((e: unknown) => e);

    expect(err).toBeInstanceOf(PresetStoreError);
    expect((err as PresetStoreError).code).toBe("NOT_FOUND");
  });
});

describe("MongoPresetStore.list", () => {
  it("queries the parent subtree in name order with a page-probe limit", async () => {
    const documents = [
      document({ name: "templates/saolei/presets/a" }),
      document({ name: "templates/saolei/presets/b" }),
    ];
    const fake = fakeCollection(documents);
    const { store } = asStore(fake);

    const page = await store.list("templates/saolei", 2, "");

    expect(page.presets).toHaveLength(2);
    expect(page.nextPageToken).toBe("");
    expect(fake.find).toHaveBeenCalledOnce();
    const [filter, options] = fake.find.mock.calls[0] as unknown as [PresetFilter, PresetFindOptions];
    // Parent filter: the name prefix under templates/{template}/presets/,
    // without a keyset bound on the first page.
    expect(filter.name).toMatchObject({
      $regex: "^templates/saolei/presets/",
    });
    expect(filter.name).not.toHaveProperty("$gt");
    expect(options.sort).toEqual({ name: 1 });
    expect(options.limit).toBe(3);
  });

  it("emits a keyset token and applies it as $gt on the next page", async () => {
    const documents = [
      document({ name: "templates/saolei/presets/a" }),
      document({ name: "templates/saolei/presets/b" }),
      document({ name: "templates/saolei/presets/c" }),
    ];
    const fake = fakeCollection(documents);
    const { store } = asStore(fake);

    const page = await store.list("templates/saolei", 2, "");

    // The extra row proves a next page exists; the token is the last
    // returned name and the returned page is truncated to the page size.
    expect(page.presets.map((p) => p.name)).toEqual([
      "templates/saolei/presets/a",
      "templates/saolei/presets/b",
    ]);
    expect(page.nextPageToken).toBe("templates/saolei/presets/b");

    await store.list("templates/saolei", 2, page.nextPageToken);
    const [filter] = fake.find.mock.calls[1] as unknown as [PresetFilter];
    expect(filter.name).toMatchObject({
      $regex: "^templates/saolei/presets/",
      $gt: "templates/saolei/presets/b",
    });
  });

  it("defaults an unspecified page size to 100 and coerces above-max values", async () => {
    const fake = fakeCollection();
    const { store } = asStore(fake);

    await store.list("templates/saolei", 0, "");
    let [filter, options] = fake.find.mock.calls[0] as unknown as [PresetFilter, PresetFindOptions];
    expect(options.limit).toBe(101);

    await store.list("templates/saolei", 5000, "");
    [filter, options] = fake.find.mock.calls[1] as unknown as [PresetFilter, PresetFindOptions];
    expect(options.limit).toBe(1001);
  });
});

describe("MongoPresetStore.ensureIndexes", () => {
  it("creates the unique name index", async () => {
    const { store, fake } = asStore(fakeCollection());

    await store.ensureIndexes();

    expect(fake.createIndex).toHaveBeenCalledOnce();
    const [key, options] = fake.createIndex.mock.calls[0] as unknown as [
      Record<string, 1 | -1>,
      { unique: boolean },
    ];
    expect(key).toEqual({ name: 1 });
    expect(options.unique).toBe(true);
  });
});
