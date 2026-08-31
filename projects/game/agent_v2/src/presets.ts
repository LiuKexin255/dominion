/**
 * presets.ts — PresetStore: the preset persistence module of agent_v2
 * (FR-005; specs/051-agent-v2-dsh-migration/data-model.md §2.1,
 * research.md D2).
 *
 * Storage is MongoDB db `game_agent_v2`, collection `presets` (per-service
 * database isolation, style/mongo.md): `_id` is never written — the
 * database generates it — and the full preset resource `name` carries a
 * unique index (v1 prompt-service precedent:
 * projects/game/prompt/runtime/mongo/repository.go). List paginates by
 * keyset over the name sort (repository.go list precedent; AIP-158:
 * https://google.aip.dev/158).
 *
 * The store consumes a structural collection seam instead of the raw
 * mongodb driver type: production wires `mongoPresetCollection(client.db()
 * .collection(...))`, tests inject `vi.fn()` doubles — no module
 * interception (style/javascript.md Mock convention). Errors carry stable
 * AIP codes (ALREADY_EXISTS / NOT_FOUND,
 * https://google.aip.dev/133#user-specified-ids,
 * https://google.aip.dev/131#errors) that the gRPC layer maps to status.
 */

import { createResolver } from "@dominion/common-js-resolver";
import type { EndpointResolver } from "@dominion/common-js-resolver";
import type { Collection as MongoCollection } from "mongodb";

/** The preset resource projection served by AgentService (agent-api.md §1). */
export interface PresetRecord {
  /** Full resource name: templates/{template}/presets/{preset}. */
  name: string;
  /** Player prompt; empty = fall back to the default base at materialization. */
  playerPrompt: string;
  createTime: Date;
  updateTime: Date;
}

/** Stable storage-error codes the gRPC layer maps onto AIP statuses. */
export type PresetStoreErrorCode = "ALREADY_EXISTS" | "NOT_FOUND";

export class PresetStoreError extends Error {
  readonly code: PresetStoreErrorCode;

  constructor(code: PresetStoreErrorCode, message: string) {
    super(message);
    this.name = "PresetStoreError";
    this.code = code;
  }
}

/** Stored document shape (`_id` deliberately absent — database-generated). */
export interface PresetDocument {
  name: string;
  player_prompt: string;
  create_time: Date;
  update_time: Date;
}

export interface PresetFilter {
  name?: string | Record<string, unknown>;
}

export interface PresetFindOptions {
  sort?: Record<string, 1 | -1>;
  limit?: number;
}

/**
 * The minimal collection surface the store consumes. Method shapes mirror
 * the mongodb driver so `mongoPresetCollection` adapts it verbatim.
 */
export interface PresetCollection {
  insertOne(document: PresetDocument): Promise<{ insertedId: unknown }>;
  findOne(filter: PresetFilter): Promise<PresetDocument | null>;
  replaceOne(filter: PresetFilter, document: PresetDocument): Promise<{ matchedCount: number }>;
  deleteOne(filter: PresetFilter): Promise<{ deletedCount: number }>;
  find(
    filter: PresetFilter,
    options: PresetFindOptions,
  ): { toArray(): Promise<PresetDocument[]> };
  createIndex(key: Record<string, 1 | -1>, options: { unique: boolean }): Promise<string>;
}

/** Adapt a real mongodb driver collection to the store's structural seam. */
export function mongoPresetCollection(
  collection: MongoCollection<PresetDocument>,
): PresetCollection {
  return {
    insertOne: (document) => collection.insertOne(document),
    findOne: (filter) => collection.findOne(filter as never),
    replaceOne: (filter, document) => collection.replaceOne(filter as never, document),
    deleteOne: (filter) => collection.deleteOne(filter as never),
    find: (filter, options) =>
      collection.find(filter as never, { sort: options.sort, limit: options.limit }),
    createIndex: (key, options) => collection.createIndex(key as never, options),
  };
}

/** One page of a keyset List. */
export interface PresetPage {
  presets: PresetRecord[];
  nextPageToken: string;
}

/** The preset persistence face consumed by the AgentService handlers. */
export interface PresetStore {
  /** Insert a new preset; ALREADY_EXISTS on a duplicate resource name. */
  create(preset: PresetRecord): Promise<void>;
  /** Read one preset; NOT_FOUND when absent. */
  get(name: string): Promise<PresetRecord>;
  /** Keyset List under a parent template, sorted by name. */
  list(parent: string, pageSize: number, pageToken: string): Promise<PresetPage>;
  /** Replace one preset's content; NOT_FOUND when absent. */
  update(preset: PresetRecord): Promise<void>;
  /** Delete one preset; NOT_FOUND when absent. */
  delete(name: string): Promise<void>;
}

/** The agent_v2 service's own Mongo database (style/mongo.md isolation). */
export const PRESET_DATABASE = "game_agent_v2";

/** The preset collection inside {@link PRESET_DATABASE}. */
export const PRESET_COLLECTION_NAME = "presets";

/** Default List page size (agent-api.md §2.5: personal scale defaults to 100). */
const DEFAULT_PAGE_SIZE = 100;

/** Maximum List page size (values above are coerced down, AIP-158). */
const MAX_PAGE_SIZE = 1000;

/** Logical Dominion target backing agent_v2's preset storage (research D2). */
export const MONGO_TARGET = "dominion:///game/mongo:27017";

/**
 * Endpoint precedence (research D2): `MONGO_URI` as-is (tests/local direct
 * connect) > the Dominion resolver answer for {@link MONGO_TARGET}, turned
 * into a `mongodb://` URI. Injectable for tests (style/javascript.md Mock
 * convention).
 */
export async function resolveMongoUri(
  deps: { env?: Record<string, string | undefined>; resolver?: EndpointResolver } = {},
): Promise<string> {
  const env = deps.env ?? process.env;
  const direct = env.MONGO_URI;
  if (direct) {
    return direct;
  }
  const resolver = deps.resolver ?? createResolver();
  const endpoints = await resolver.resolve(MONGO_TARGET);
  if (endpoints.length === 0) {
    throw new Error(`resolver returned no endpoints for ${MONGO_TARGET}`);
  }
  return `mongodb://${endpoints[0]}`;
}

/** E11000 duplicate-key write error (v1 IsDuplicateKeyError precedent). */
function isDuplicateKeyError(err: unknown): boolean {
  if (typeof err !== "object" || err === null) {
    return false;
  }
  const view = err as { code?: unknown; codeName?: unknown };
  return view.code === 11000 || view.codeName === "DuplicateKey";
}

function toDocument(preset: PresetRecord): PresetDocument {
  return {
    name: preset.name,
    player_prompt: preset.playerPrompt,
    create_time: preset.createTime,
    update_time: preset.updateTime,
  };
}

function toRecord(document: PresetDocument): PresetRecord {
  return {
    name: document.name,
    playerPrompt: document.player_prompt,
    createTime: document.create_time,
    updateTime: document.update_time,
  };
}

/** Escape a parent resource name for the List name-prefix regex. */
function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * MongoDB-backed PresetStore. `name` (full resource name) is the identity
 * and carries the unique index; `_id` is database-generated and never
 * projected (style/mongo.md).
 */
export class MongoPresetStore implements PresetStore {
  constructor(private readonly collection: PresetCollection) {}

  /** Create the unique name index (call once at service startup). */
  async ensureIndexes(): Promise<void> {
    await this.collection.createIndex({ name: 1 }, { unique: true });
  }

  async create(preset: PresetRecord): Promise<void> {
    try {
      await this.collection.insertOne(toDocument(preset));
    } catch (err) {
      if (isDuplicateKeyError(err)) {
        throw new PresetStoreError("ALREADY_EXISTS", `preset ${preset.name} already exists`);
      }
      throw err;
    }
  }

  async get(name: string): Promise<PresetRecord> {
    const document = await this.collection.findOne({ name });
    if (document === null) {
      throw new PresetStoreError("NOT_FOUND", `preset ${name} not found`);
    }
    return toRecord(document);
  }

  async list(parent: string, pageSize: number, pageToken: string): Promise<PresetPage> {
    // Coerce per AIP-158: unspecified/0 → default, above max → max.
    const size = pageSize <= 0 ? DEFAULT_PAGE_SIZE : Math.min(pageSize, MAX_PAGE_SIZE);
    const nameFilter: Record<string, unknown> = {
      $regex: `^${escapeRegex(parent)}/presets/`,
    };
    if (pageToken !== "") {
      nameFilter.$gt = pageToken;
    }
    const cursor = this.collection.find({ name: nameFilter }, {
      sort: { name: 1 },
      // One extra row detects a next page without a second query.
      limit: size + 1,
    });
    const documents = await cursor.toArray();

    let nextPageToken = "";
    if (documents.length > size) {
      nextPageToken = documents[size - 1].name;
      documents.length = size;
    }
    return {
      presets: documents.map(toRecord),
      nextPageToken,
    };
  }

  async update(preset: PresetRecord): Promise<void> {
    const result = await this.collection.replaceOne({ name: preset.name }, toDocument(preset));
    if (result.matchedCount === 0) {
      throw new PresetStoreError("NOT_FOUND", `preset ${preset.name} not found`);
    }
  }

  async delete(name: string): Promise<void> {
    const result = await this.collection.deleteOne({ name });
    if (result.deletedCount === 0) {
      throw new PresetStoreError("NOT_FOUND", `preset ${name} not found`);
    }
  }
}
