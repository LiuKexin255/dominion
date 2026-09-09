/**
 * PresetStore seam: the preset dynamic-field persistence face of the
 * preset-authoring plugin. The store records ONLY the dynamic field set
 * (specs/058-dsh-preset-roster-demo/data-model.md §2) — composition content
 * never crosses this seam (it lives in the template and the materialized
 * copy on disk, specs/058-dsh-preset-roster-demo/contracts/
 * preset-authoring-plugin.md §3).
 *
 * Errors carry stable codes the service layer maps onto gRPC statuses
 * (agent_v2 precedent: projects/game/agent_v2/src/presets.ts:40-50).
 */

/**
 * The preset pool a record belongs to — the store-level twin of the proto
 * `PresetRole` enum (PLAYER/PLANNER, specs/059-agent-v2-team-mode/
 * contracts/preset-api.md §2): the role decides the pool the preset was
 * materialized from and is immutable after create. Optional at the seam —
 * role-less consumers (the 058 demo) record presets without a pool; the
 * agent_v2 service face makes it REQUIRED.
 */
export type PresetRole = "player" | "planner";

/** One authored preset's dynamic fields (data-model.md §2). */
export interface PresetRecord {
  /** Preset id = resource id (`presets/{id}`) = the materialized directory name. */
  id: string;
  /** Source template id the copy was materialized from. */
  template: string;
  /** The pool the preset belongs to (immutable; absent for role-less consumers). */
  role?: PresetRole;
  /** Persona prose materialized into the copy's persona row `config.text`. */
  persona: string;
  /** Display name materialized into the copy's `preset.yml`; absent = the id. */
  displayName?: string;
  createTime: Date;
  updateTime: Date;
}

/** Stable storage-error codes (preset-authoring-plugin.md §3). */
export type PresetStoreErrorCode = "ALREADY_EXISTS" | "NOT_FOUND";

export class PresetStoreError extends Error {
  readonly code: PresetStoreErrorCode;

  constructor(code: PresetStoreErrorCode, message: string) {
    super(message);
    this.name = "PresetStoreError";
    this.code = code;
  }
}

/** The persistence face consumed by the preset-authoring service. */
export interface PresetStore {
  /** Insert a new record; ALREADY_EXISTS on a duplicate id. */
  create(record: PresetRecord): Promise<void>;
  /** Read one record; NOT_FOUND when absent. */
  get(id: string): Promise<PresetRecord>;
  /** Every stored record (authored copies only — templates are deployment data). */
  list(): Promise<PresetRecord[]>;
  /** Replace one record; NOT_FOUND when absent. */
  update(record: PresetRecord): Promise<void>;
  /** Delete one record; NOT_FOUND when absent. */
  remove(id: string): Promise<void>;
}

/**
 * In-memory PresetStore (demo consumer; records are process-state and lost
 * on restart — the known demo limitation, specs/058-dsh-preset-roster-demo/
 * spec.md Assumptions). agent_v2 productionization plugs a Mongo
 * implementation into the same seam later (preset-authoring-plugin.md §3).
 */
export class MemoryPresetStore implements PresetStore {
  private readonly records = new Map<string, PresetRecord>();

  async create(record: PresetRecord): Promise<void> {
    if (this.records.has(record.id)) {
      throw new PresetStoreError("ALREADY_EXISTS", `preset ${record.id} already exists`);
    }
    this.records.set(record.id, { ...record });
  }

  async get(id: string): Promise<PresetRecord> {
    const record = this.records.get(id);
    if (record === undefined) {
      throw new PresetStoreError("NOT_FOUND", `preset ${id} not found`);
    }
    return { ...record };
  }

  async list(): Promise<PresetRecord[]> {
    return [...this.records.values()].map((record) => ({ ...record }));
  }

  async update(record: PresetRecord): Promise<void> {
    if (!this.records.has(record.id)) {
      throw new PresetStoreError("NOT_FOUND", `preset ${record.id} not found`);
    }
    this.records.set(record.id, { ...record });
  }

  async remove(id: string): Promise<void> {
    if (!this.records.delete(id)) {
      throw new PresetStoreError("NOT_FOUND", `preset ${id} not found`);
    }
  }
}
