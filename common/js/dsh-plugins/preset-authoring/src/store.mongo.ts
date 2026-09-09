/**
 * Mongo PresetStore: the `PresetStore` seam implementation backing the
 * preset-authoring plugin's production deployment (agent_v2). The document
 * holds the dynamic field set (id/template/role/persona/displayName/
 * timestamps, specs/059-agent-v2-team-mode/contracts/preset-api.md §3);
 * composition content never crosses the store seam. `_id` is never written
 * — the database generates it — and the preset `id` carries a unique index
 * (style/mongo.md).
 *
 * The plugin is deployment-agnostic: the CONNECTION (credentialed URI,
 * database, collection) is injected by the host through the row config
 * (`mongoUri`/`mongoDatabase`/`mongoCollection`, resolved host-side — the
 * Dominion credential derivation lives in projects/game/agent_v2/src/
 * presets.ts and reaches the row via the `MONGO_URI` environment variable,
 * the established host-injection pattern). Tests inject plain collection
 * doubles — no module interception (style/javascript.md Mock convention).
 * Errors carry stable codes (ALREADY_EXISTS / NOT_FOUND,
 * https://google.aip.dev/133#user-specified-ids,
 * https://google.aip.dev/131#errors).
 */

import type { Collection as MongoCollection } from "mongodb";

import { PresetStoreError } from "./store.js";
import type { PresetRecord } from "./store.js";

/** Stored document shape (`_id` deliberately absent — database-generated). */
export interface MongoPresetDocument {
  id: string;
  template: string;
  role?: string;
  persona: string;
  display_name?: string;
  create_time: Date;
  update_time: Date;
}

export interface MongoPresetFilter {
  id?: string;
}

/**
 * The minimal collection surface the store consumes. Method shapes mirror
 * the mongodb driver so `mongoPresetCollection` adapts it verbatim.
 */
export interface MongoPresetCollection {
  insertOne(document: MongoPresetDocument): Promise<{ insertedId: unknown }>;
  findOne(filter: MongoPresetFilter): Promise<MongoPresetDocument | null>;
  replaceOne(
    filter: MongoPresetFilter,
    document: MongoPresetDocument,
  ): Promise<{ matchedCount: number }>;
  deleteOne(filter: MongoPresetFilter): Promise<{ deletedCount: number }>;
  /** Startup purge of the legacy-schema rows (see
   * MongoPresetStore.purgeLegacyDocuments). */
  deleteMany(filter: { id: { $exists: false } }): Promise<{ deletedCount: number }>;
  find(filter: MongoPresetFilter): { toArray(): Promise<MongoPresetDocument[]> };
  createIndex(key: Record<string, 1 | -1>, options: { unique: boolean }): Promise<string>;
  dropIndex(indexName: string): Promise<unknown>;
}

/** Adapt a real mongodb driver collection to the store's structural seam. */
export function mongoPresetCollection(
  collection: MongoCollection<MongoPresetDocument>,
): MongoPresetCollection {
  return {
    insertOne: (document) => collection.insertOne(document),
    findOne: (filter) => collection.findOne(filter as never),
    replaceOne: (filter, document) => collection.replaceOne(filter as never, document),
    deleteOne: (filter) => collection.deleteOne(filter as never),
    deleteMany: (filter) => collection.deleteMany(filter as never),
    find: (filter) => collection.find(filter as never),
    createIndex: (key, options) => collection.createIndex(key as never, options),
    dropIndex: (indexName) => collection.dropIndex(indexName),
  };
}

/** E11000 duplicate-key write error (v1 IsDuplicateKeyError precedent). */
function isDuplicateKeyError(err: unknown): boolean {
  if (typeof err !== "object" || err === null) {
    return false;
  }
  const view = err as { code?: unknown; codeName?: unknown };
  return view.code === 11000 || view.codeName === "DuplicateKey";
}

/**
 * The dropped legacy index cannot exist: IndexNotFound (mongo error code 27)
 * names an absent index on a live collection, and NamespaceNotFound (code
 * 26) means the collection itself was never created — as on a fresh mongo
 * without persistence, where `createIndex` is what first creates the
 * namespace. Both imply there is no legacy face to drop.
 */
function isLegacyIndexAbsentError(err: unknown): boolean {
  if (typeof err !== "object" || err === null) {
    return false;
  }
  const view = err as { code?: unknown; codeName?: unknown };
  return (
    view.code === 27 ||
    view.codeName === "IndexNotFound" ||
    view.code === 26 ||
    view.codeName === "NamespaceNotFound"
  );
}

function toDocument(record: PresetRecord): MongoPresetDocument {
  return {
    id: record.id,
    template: record.template,
    ...(record.role === undefined ? {} : { role: record.role }),
    persona: record.persona,
    ...(record.displayName === undefined ? {} : { display_name: record.displayName }),
    create_time: record.createTime,
    update_time: record.updateTime,
  };
}

function toRecord(document: MongoPresetDocument): PresetRecord {
  return {
    id: document.id,
    template: document.template,
    role: document.role,
    persona: document.persona,
    displayName: document.display_name,
    createTime: document.create_time,
    updateTime: document.update_time,
  };
}

/**
 * MongoDB-backed PresetStore. `id` is the identity and carries the unique
 * index; `_id` is database-generated and never projected (style/mongo.md).
 */
export class MongoPresetStore {
  constructor(private readonly collection: MongoPresetCollection) {}

  /** Create the unique id index (call once at storage startup). */
  async ensureIndexes(): Promise<void> {
    await this.collection.createIndex({ id: 1 }, { unique: true });
  }

  /**
   * One-time startup purge of legacy-schema documents, MUST run before
   * {@link ensureIndexes}. The retired single-agent preset storage wrote
   * rows shaped `{name, player_prompt}` with no `id` field (the v1→v2
   * preset comparison, specs/059-agent-v2-team-mode/spec.md). Such rows
   * cannot map onto the roster-era record — they carry no template/role to
   * (re)materialize a composition copy from — so dropping them is safe, and
   * this store is the new schema's source of truth from which copies are
   * rebuilt (specs/059-agent-v2-team-mode/contracts/preset-api.md §3).
   * Keeping them would break startup or poison reads: with ≥2 legacy rows
   * the unique id index build fails on their colliding null keys (E11000 →
   * plugin activation failure); with fewer, `list()` projects them back as
   * `id: undefined` ghosts. The purge matches the US1 clean-v2-baseline
   * goal (specs/059-agent-v2-team-mode/spec.md User Story 1): the collection
   * carries only well-formed v2 documents.
   */
  async purgeLegacyDocuments(): Promise<void> {
    await this.collection.deleteMany({ id: { $exists: false } });
  }

  /**
   * One-time startup drop of the retired store's unique `name` index, MUST
   * run after {@link purgeLegacyDocuments} and before {@link ensureIndexes}.
   * The retired single-agent preset storage built `createIndex({name: 1},
   * {unique: true})` over this same persistent collection
   * (specs/051-agent-v2-dsh-migration/research.md), and deleting its rows
   * does not drop the index. v2 documents carry no `name` field, so a
   * non-sparse unique index keys them all as null
   * (https://www.mongodb.com/docs/manual/core/index-unique/#unique-index-and-missing-field):
   * on an upgraded deployment the first create claims the null key and
   * every later insert dies E11000 — mapped to ALREADY_EXISTS by
   * {@link isDuplicateKeyError} even though the preset id is fresh — making
   * preset authoring unusable after exactly one create. An absent drop
   * target (IndexNotFound, or NamespaceNotFound on a collection that was
   * never created — see {@link isLegacyIndexAbsentError}) is tolerated:
   * it means the collection never carried the legacy face, so there is
   * nothing to drop.
   */
  async dropLegacyNameIndex(): Promise<void> {
    try {
      await this.collection.dropIndex("name_1");
    } catch (err) {
      if (!isLegacyIndexAbsentError(err)) {
        throw err;
      }
    }
  }

  async create(record: PresetRecord): Promise<void> {
    try {
      await this.collection.insertOne(toDocument(record));
    } catch (err) {
      if (isDuplicateKeyError(err)) {
        throw new PresetStoreError("ALREADY_EXISTS", `preset ${record.id} already exists`);
      }
      throw err;
    }
  }

  async get(id: string): Promise<PresetRecord> {
    const document = await this.collection.findOne({ id });
    if (document === null) {
      throw new PresetStoreError("NOT_FOUND", `preset ${id} not found`);
    }
    return toRecord(document);
  }

  async list(): Promise<PresetRecord[]> {
    const documents = await this.collection.find({}).toArray();
    return documents.map(toRecord);
  }

  async update(record: PresetRecord): Promise<void> {
    const result = await this.collection.replaceOne({ id: record.id }, toDocument(record));
    if (result.matchedCount === 0) {
      throw new PresetStoreError("NOT_FOUND", `preset ${record.id} not found`);
    }
  }

  async remove(id: string): Promise<void> {
    const result = await this.collection.deleteOne({ id });
    if (result.deletedCount === 0) {
      throw new PresetStoreError("NOT_FOUND", `preset ${id} not found`);
    }
  }
}

/** The minimal driver face {@link createMongoPresetStore} consumes. */
export interface MongoClientLike {
  connect(): Promise<unknown>;
  close(): Promise<void>;
  db(name: string): {
    collection(name: string): MongoCollection<MongoPresetDocument>;
  };
}

/** Connection inputs the host resolves (row config / env injection). */
export interface MongoPresetConnection {
  /** Credentialed `mongodb://` URI, resolved host-side. REQUIRED. */
  uri: string;
  database?: string;
  collection?: string;
}

/** What {@link createMongoPresetStore} hands back: store plus owned client. */
export interface MongoPresetStoreHandle {
  store: MongoPresetStore;
  /** The connected client — the caller owns closing it on shutdown. */
  client: MongoClientLike;
}

/**
 * Connect to the configured Mongo deployment and return the indexed preset
 * store over the live collection. The returned client is owned by the caller
 * (the plugin registers its dispose as a composition effect; awaiting this
 * handle before the composition settles keeps a connect/index failure
 * fail-loud at boot).
 */
export async function createMongoPresetStore(
  connection: MongoPresetConnection,
  deps: { connectClient?: (uri: string) => Promise<MongoClientLike> } = {},
): Promise<MongoPresetStoreHandle> {
  if (connection.uri === "") {
    throw new Error(
      "preset-authoring storage=mongo requires a mongoUri (row config or the host-injected MONGO_URI)",
    );
  }
  const { MongoClient } = await import("mongodb");
  const connectClient =
    deps.connectClient ??
    (async (uri: string) => {
      const client = new MongoClient(uri) as unknown as MongoClientLike;
      await client.connect();
      return client;
    });
  const client = await connectClient(connection.uri);
  const collection = mongoPresetCollection(
    client.db(connection.database ?? "presets").collection(connection.collection ?? "presets"),
  );
  const store = new MongoPresetStore(collection);
  // The purge MUST precede the index build — legacy rows would trip E11000
  // on the unique id index (see purgeLegacyDocuments).
  await store.purgeLegacyDocuments();
  // The legacy name-unique index MUST drop after the purge and before the
  // index build (see dropLegacyNameIndex).
  await store.dropLegacyNameIndex();
  await store.ensureIndexes();
  return { store, client };
}
