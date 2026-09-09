import { describe, expect, it, vi } from "vitest";

import { createMongoPresetStore, MongoPresetStore } from "./store.mongo.js";
import { PresetStoreError } from "./store.js";
import type {
  MongoPresetCollection,
  MongoPresetDocument,
  MongoPresetFilter,
} from "./store.mongo.js";

/**
 * MongoPresetStore unit tests over an injected collection double (DI seam,
 * style/javascript.md Mock convention — vi.fn() doubles, no module
 * interception): document shape, unique index, and the AIP error mapping
 * (ALREADY_EXISTS on duplicate id, NOT_FOUND on absent rows). The credentialed
 * URI resolution lives with the agent_v2 host (projects/game/agent_v2/src/
 * presets.test.ts) — this store consumes the resolved connection.
 */

function record(overrides: Partial<Parameters<MongoPresetStore["create"]>[0]> = {}) {
  return {
    id: "my-preset",
    template: "player",
    role: "player" as const,
    persona: "play carefully",
    displayName: undefined,
    createTime: new Date(1000),
    updateTime: new Date(2000),
    ...overrides,
  };
}

function document(overrides: Partial<MongoPresetDocument> = {}): MongoPresetDocument {
  return {
    id: "my-preset",
    template: "player",
    role: "player",
    persona: "play carefully",
    create_time: new Date(1000),
    update_time: new Date(2000),
    ...overrides,
  };
}

function fakeCollection(documents: MongoPresetDocument[] = []) {
  return {
    insertOne: vi.fn(async () => ({ insertedId: "db-generated" })),
    findOne: vi.fn(async (filter: MongoPresetFilter) =>
      documents.find((d) => d.id === filter.id) ?? null,
    ),
    replaceOne: vi.fn(async () => ({ matchedCount: 1 })),
    deleteOne: vi.fn(async () => ({ deletedCount: 1 })),
    deleteMany: vi.fn(async (filter: { id?: { $exists: boolean } }) => {
      // Simulate the driver's `$exists: false` match: only rows lacking the
      // field (https://www.mongodb.com/docs/manual/reference/operator/query/exists/).
      if (filter.id?.$exists !== false) {
        return { deletedCount: 0 };
      }
      const before = documents.length;
      for (let index = documents.length - 1; index >= 0; index--) {
        if (documents[index].id === undefined) {
          documents.splice(index, 1);
        }
      }
      return { deletedCount: before - documents.length };
    }),
    find: vi.fn((_filter: MongoPresetFilter) => ({
      toArray: async () => [...documents],
    })),
    createIndex: vi.fn(async () => "id_1"),
    dropIndex: vi.fn(async () => true),
  };
}

type FakeCollection = ReturnType<typeof fakeCollection>;

function asStore(fake: FakeCollection): { store: MongoPresetStore; fake: FakeCollection } {
  return { store: new MongoPresetStore(fake as unknown as MongoPresetCollection), fake };
}

describe("MongoPresetStore.create", () => {
  it("inserts the document without an _id (database-generated, style/mongo.md)", async () => {
    const { store, fake } = asStore(fakeCollection());

    await store.create(record());

    expect(fake.insertOne).toHaveBeenCalledOnce();
    const [doc] = fake.insertOne.mock.calls[0] as unknown as [MongoPresetDocument];
    expect(doc).toEqual({
      id: "my-preset",
      template: "player",
      role: "player",
      persona: "play carefully",
      create_time: new Date(1000),
      update_time: new Date(2000),
    });
    expect(doc).not.toHaveProperty("_id");
  });

  it("omits the display_name field when the record has none", async () => {
    const { store, fake } = asStore(fakeCollection());

    await store.create(record({ displayName: "Shown" }));

    const [doc] = fake.insertOne.mock.calls[0] as unknown as [MongoPresetDocument];
    expect(doc.display_name).toBe("Shown");
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

    const got = await store.get("my-preset");

    expect(got).toEqual(record());
    expect(fake.findOne).toHaveBeenCalledWith({ id: "my-preset" });
  });

  it("maps a missing document to NOT_FOUND", async () => {
    const { store } = asStore(fakeCollection());

    const err = await store.get("absent").catch((e: unknown) => e);

    expect(err).toBeInstanceOf(PresetStoreError);
    expect((err as PresetStoreError).code).toBe("NOT_FOUND");
  });
});

describe("MongoPresetStore.list", () => {
  it("returns every stored record projected to records", async () => {
    const fake = fakeCollection([document(), document({ id: "other", role: "planner" })]);
    const { store } = asStore(fake);

    const listed = await store.list();

    expect(listed.map((r) => r.id).sort()).toEqual(["my-preset", "other"]);
    expect(listed[1].role).toBe("planner");
  });
});

describe("MongoPresetStore.update", () => {
  it("replaces the row matched by id", async () => {
    const { store, fake } = asStore(fakeCollection());

    await store.update(record({ updateTime: new Date(3000) }));

    expect(fake.replaceOne).toHaveBeenCalledOnce();
    const [filter, doc] = fake.replaceOne.mock.calls[0] as unknown as [
      MongoPresetFilter,
      MongoPresetDocument,
    ];
    expect(filter).toEqual({ id: "my-preset" });
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

describe("MongoPresetStore.remove", () => {
  it("maps a zero-deletion result to NOT_FOUND", async () => {
    const fake = fakeCollection();
    fake.deleteOne.mockResolvedValueOnce({ deletedCount: 0 });
    const { store } = asStore(fake);

    const err = await store.remove("absent").catch((e: unknown) => e);

    expect(err).toBeInstanceOf(PresetStoreError);
    expect((err as PresetStoreError).code).toBe("NOT_FOUND");
  });
});

describe("MongoPresetStore.ensureIndexes", () => {
  it("creates the unique id index", async () => {
    const { store, fake } = asStore(fakeCollection());

    await store.ensureIndexes();

    expect(fake.createIndex).toHaveBeenCalledOnce();
    const [key, options] = fake.createIndex.mock.calls[0] as unknown as [
      Record<string, 1 | -1>,
      { unique: boolean },
    ];
    expect(key).toEqual({ id: 1 });
    expect(options.unique).toBe(true);
  });
});

describe("MongoPresetStore.purgeLegacyDocuments", () => {
  /** Pre-roster single-agent row shape: `name`/`player_prompt`, no `id`
   * (specs/059-agent-v2-team-mode/spec.md v1→v2 preset comparison). */
  function legacyDocument(): MongoPresetDocument {
    return { name: "default", player_prompt: "play carefully" } as unknown as MongoPresetDocument;
  }

  it("issues the exact id-missing purge filter", async () => {
    const { store, fake } = asStore(fakeCollection());

    await store.purgeLegacyDocuments();

    expect(fake.deleteMany).toHaveBeenCalledOnce();
    const [filter] = fake.deleteMany.mock.calls[0] as unknown as [{ id: { $exists: false } }];
    expect(filter).toEqual({ id: { $exists: false } });
  });

  it("deletes the legacy id-less rows while keeping every document that carries an id", async () => {
    const fake = fakeCollection([legacyDocument(), document({ id: "keep-me" })]);
    const { store } = asStore(fake);

    await store.purgeLegacyDocuments();

    expect(fake.deleteMany).toHaveBeenCalledOnce();
    // The ghost projection (`id: undefined`) is gone; the well-formed row
    // survives untouched.
    const listed = await store.list();
    expect(listed.map((r) => r.id)).toEqual(["keep-me"]);
  });
});

describe("MongoPresetStore.dropLegacyNameIndex", () => {
  it("drops the exact retired unique name index", async () => {
    const { store, fake } = asStore(fakeCollection());

    await store.dropLegacyNameIndex();

    expect(fake.dropIndex).toHaveBeenCalledOnce();
    expect(fake.dropIndex).toHaveBeenCalledWith("name_1");
  });

  it("swallows an absent drop target (IndexNotFound / NamespaceNotFound) and rethrows every other drop failure", async () => {
    for (const absent of [
      { code: 27, codeName: "IndexNotFound" },
      { code: 26, codeName: "NamespaceNotFound" },
    ]) {
      const tolerant = fakeCollection();
      tolerant.dropIndex.mockRejectedValueOnce(
        Object.assign(new Error(`ns not found`), absent),
      );
      await expect(asStore(tolerant).store.dropLegacyNameIndex()).resolves.toBeUndefined();
    }

    const fatal = fakeCollection();
    fatal.dropIndex.mockRejectedValueOnce(new Error("connection refused"));
    await expect(asStore(fatal).store.dropLegacyNameIndex()).rejects.toThrow(
      /connection refused/,
    );
  });
});

describe("createMongoPresetStore", () => {
  it("connects the injected uri, indexes, and hands back the owned client", async () => {
    const deleteMany = vi.fn(async () => ({ deletedCount: 0 }));
    const dropIndex = vi.fn(async () => true);
    const createIndex = vi.fn(async () => "id_1");
    const connect = vi.fn(async () => ({
      connect: vi.fn(async () => undefined),
      close: vi.fn(async () => undefined),
      db: vi.fn(() => ({
        collection: vi.fn(() => ({ deleteMany, dropIndex, createIndex }) as never),
      })),
    }));

    const handle = await createMongoPresetStore(
      { uri: "mongodb://resolved:27017", database: "game_agent_v2", collection: "presets" },
      { connectClient: connect },
    );

    // The URI is consumed verbatim (credential resolution is the host's),
    // and the startup index creation runs over the selected collection.
    const [uri] = connect.mock.calls[0] as unknown as [string];
    expect(uri).toBe("mongodb://resolved:27017");
    expect(createIndex).toHaveBeenCalledWith({ id: 1 }, { unique: true });
    expect(handle.store).toBeInstanceOf(MongoPresetStore);
    expect(handle.client.close).toBeTypeOf("function");
  });

  it("runs purge → dropIndex(name_1) → ensureIndexes on startup", async () => {
    const callOrder: string[] = [];
    const deleteMany = vi.fn(async () => {
      callOrder.push("purgeLegacyDocuments");
      return { deletedCount: 0 };
    });
    const dropIndex = vi.fn(async () => {
      callOrder.push("dropLegacyNameIndex");
      return true;
    });
    const createIndex = vi.fn(async () => {
      callOrder.push("ensureIndexes");
      return "id_1";
    });
    const connect = vi.fn(async () => ({
      connect: vi.fn(async () => undefined),
      close: vi.fn(async () => undefined),
      db: vi.fn(() => ({
        collection: vi.fn(() => ({ deleteMany, dropIndex, createIndex }) as never),
      })),
    }));

    const handle = await createMongoPresetStore(
      { uri: "mongodb://resolved:27017" },
      { connectClient: connect },
    );

    // Legacy rows missing `id` would collide on the unique id index's null
    // key (E11000), so the purge must complete first; the legacy non-sparse
    // unique name index keys every name-less v2 document as null, so its
    // drop must land between the purge and the id index build.
    expect(deleteMany).toHaveBeenCalledOnce();
    expect(deleteMany).toHaveBeenCalledWith({ id: { $exists: false } });
    expect(dropIndex).toHaveBeenCalledOnce();
    expect(dropIndex).toHaveBeenCalledWith("name_1");
    expect(callOrder).toEqual(["purgeLegacyDocuments", "dropLegacyNameIndex", "ensureIndexes"]);
    expect(handle.store).toBeInstanceOf(MongoPresetStore);
  });

  it("tolerates a missing legacy index at startup and still activates the store", async () => {
    const dropIndex = vi.fn(async () => {
      // A fresh mongo without persistence never created the presets
      // collection — the guitar topology — so the drop lands on
      // NamespaceNotFound, not IndexNotFound.
      throw Object.assign(new Error("ns not found game_agent_v2.presets"), {
        code: 26,
        codeName: "NamespaceNotFound",
      });
    });
    const createIndex = vi.fn(async () => "id_1");
    const deleteMany = vi.fn(async () => ({ deletedCount: 0 }));
    const connect = vi.fn(async () => ({
      connect: vi.fn(async () => undefined),
      close: vi.fn(async () => undefined),
      db: vi.fn(() => ({
        collection: vi.fn(() => ({ deleteMany, dropIndex, createIndex }) as never),
      })),
    }));

    const handle = await createMongoPresetStore(
      { uri: "mongodb://resolved:27017" },
      { connectClient: connect },
    );

    // An absent index means no legacy surface: startup proceeds and the
    // unique id index is still built.
    expect(dropIndex).toHaveBeenCalledOnce();
    expect(dropIndex).toHaveBeenCalledWith("name_1");
    expect(createIndex).toHaveBeenCalledWith({ id: 1 }, { unique: true });
    expect(handle.store).toBeInstanceOf(MongoPresetStore);
  });

  it("fails loud on an empty uri instead of connecting nowhere", async () => {
    await expect(
      createMongoPresetStore(
        { uri: "" },
        {
          connectClient: async () => {
            throw new Error("must not connect");
          },
        },
      ),
    ).rejects.toThrow(/mongoUri/);
  });
});
