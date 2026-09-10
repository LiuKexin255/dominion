/**
 * cordis plugin entry for the preset-authoring base plugin: C1
 * copy-then-patch preset authoring over the official roster. Export shape
 * follows the function-form plugin contract (name / inject / Config / apply,
 * common/js/dsh-plugins/llm-glm/src/index.ts precedent); registration is
 * effect-based and disposed with the fiber.
 *
 * `ctx.presetAuthoring` is the ONLY face the service layer consumes —
 * zero roster API and zero filesystem access outside this plugin
 * (FR-005 in specs/058-dsh-preset-roster-demo/spec.md; the plugin itself
 * carries no RPC/transport concepts).
 * Contract: specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md.
 */

import type { Context } from "@deepseek-ai/cordis";
import { UnknownPresetError } from "@deepseek-ai/dsh-agent-presets";
import type { AgentPreset } from "@deepseek-ai/dsh-agent-presets";
import z from "@deepseek-ai/schemastery";

import {
  mapRosterError,
  materializeCopy,
  nodeMaterializeFs,
  PresetAuthoringError,
  removeMaterialization,
  updateMaterialization,
  validateTemplateRows,
} from "./materialize.js";
import type { MaterializeFs, RosterSeam, TemplateRowRules } from "./materialize.js";
import { MemoryPresetStore, PresetStoreError } from "./store.js";
import type { PresetRecord, PresetStore } from "./store.js";
import { createMongoPresetStore } from "./store.mongo.js";

export { PERSONA_ROW_NAME, PresetAuthoringError, validateTemplateRows } from "./materialize.js";
export type { PresetAuthoringErrorCode, TemplateRowRules } from "./materialize.js";
export { MemoryPresetStore, PresetStoreError } from "./store.js";
export type { PresetRecord, PresetStore, PresetStoreErrorCode } from "./store.js";
export { createMongoPresetStore, mongoPresetCollection, MongoPresetStore } from "./store.mongo.js";
export type { MongoPresetConnection } from "./store.mongo.js";

export const name = "preset-authoring";

/** Hard dependency on the official roster being composed. */
export const inject = ["agentPresets"];

/** Validated configuration owned by the plugin. */
export interface PresetAuthoringConfig {
  /** The preset record storage: in-memory (demo) or Mongo (agent_v2). */
  storage: "memory" | "mongo";
  /**
   * Caller-declared composition row rules, keyed by template id: the
   * hosting composition pins each pool template's role plugin rows (e.g.
   * "player" → exactly the saolei row, never a memory row) and every copy
   * validates against them before landing
   * (specs/059-agent-v2-team-mode/contracts/preset-api.md §2). Omitted
   * template ids skip validation — generic consumers stay scene-agnostic.
   */
  templateRules?: Record<string, TemplateRowRules>;
  /**
   * storage=mongo connection inputs, injected through the row config. The
   * credentialed URI is resolved HOST-side (deployment credential logic
   * never crosses this seam — agent_v2 derives it in
   * projects/game/agent_v2/src/presets.ts and injects it via the
   * MONGO_URI environment variable, the GLM_BASE_URL injection pattern).
   */
  mongoUri?: string;
  mongoDatabase?: string;
  mongoCollection?: string;
}

// Schemastery's ObjectS input shape is wider than the validated output type;
// the cast mirrors the official adapters' declared `Config: z<Config>` shape
// (common/js/dsh-plugins/llm-glm/src/index.ts precedent).
export const Config: z<PresetAuthoringConfig> = z.object({
  storage: z.union([z.const("memory"), z.const("mongo")]).default("memory"),
  mongoUri: z.string(),
  mongoDatabase: z.string(),
  mongoCollection: z.string(),
  templateRules: z.dict(
    z.object({
      // Explicit defaults: a host rule table may declare only one half, and
      // the validator must then iterate an empty list instead of undefined.
      required: z.array(z.string()).default([]),
      forbidden: z.array(z.string()).default([]),
    }),
  ),
}) as unknown as z<PresetAuthoringConfig>;

/** The preset resource projection served to the service layer (contract §2). */
export interface PresetView {
  id: string;
  template: string;
  /** The caller-defined pool label (opaque to the plugin; immutable after
   * create; absent for role-less consumers — see
   * common/js/dsh-plugins/preset-authoring/src/store.ts `PresetRecord.role`). */
  role?: string;
  persona: string;
  displayName?: string;
  createTime: Date;
  updateTime: Date;
}

/** The compose() return: the meta snapshot plus the agent-factory setup hook. */
export interface ComposeResult {
  /** Resolved preset id; the service records it in the creation meta. */
  agentPreset: string;
  /**
   * Agent-factory setup hook: mounts the preset's standing composition.
   * Called from `ctx.agents.create({meta, setup})`; a rejection there rolls
   * the whole agent creation back, so no half-composed session can exist.
   */
  setup(agentCtx: Context): Promise<void>;
}

/** The create input: the dynamic field set of a new authored preset. */
export interface CreatePresetInput {
  id: string;
  template: string;
  /** The caller-defined pool label (opaque to the plugin); the agent_v2
   * face requires it (specs/059-agent-v2-team-mode/contracts/preset-api.md §2). */
  role?: string;
  persona: string;
  displayName?: string;
}

/** The service face mounted as `ctx.presetAuthoring` (contract §2, R10). */
export interface PresetAuthoringService {
  compose(presetId?: string): Promise<ComposeResult>;
  create(input: CreatePresetInput): Promise<PresetView>;
  get(id: string): Promise<PresetView>;
  /** Authored presets, optionally narrowed to one pool label. */
  list(role?: string): Promise<PresetView[]>;
  update(id: string, patch: { persona?: string; displayName?: string }): Promise<PresetView>;
  remove(id: string): Promise<void>;
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    presetAuthoring: PresetAuthoringService;
  }
}

/** Injectable collaborators (contract §5 seam convention). */
export interface PresetAuthoringDeps {
  roster?: RosterSeam;
  store?: PresetStore;
  fs?: MaterializeFs;
  /** Per-template composition row rules (see {@link PresetAuthoringConfig.templateRules}). */
  templateRules?: Record<string, TemplateRowRules>;
}

function toView(record: PresetRecord): PresetView {
  return {
    id: record.id,
    template: record.template,
    role: record.role,
    persona: record.persona,
    displayName: record.displayName,
    createTime: record.createTime,
    updateTime: record.updateTime,
  };
}

/** Map store rejections onto the service error surface (contract §6). */
function mapStoreError(err: unknown): PresetAuthoringError {
  if (err instanceof PresetStoreError) {
    return new PresetAuthoringError(err.code, err.message, err);
  }
  return new PresetAuthoringError(
    "INTERNAL",
    `preset store operation failed: ${err instanceof Error ? err.message : String(err)}`,
    err,
  );
}

/**
 * Assemble the preset-authoring service. Production resolves every
 * collaborator from the composition context; tests inject `vi.fn()` doubles
 * (style/javascript.md Mock convention — no module interception).
 */
export function createPresetAuthoring(ctx: Context, deps: PresetAuthoringDeps = {}): PresetAuthoringService {
  const roster = deps.roster ?? (ctx.agentPresets as RosterSeam);
  const store = deps.store ?? new MemoryPresetStore();
  const fs = deps.fs ?? nodeMaterializeFs();
  const templateRules = deps.templateRules;

  const storeGet = async (id: string): Promise<PresetRecord> => {
    try {
      return await store.get(id);
    } catch (err) {
      throw mapStoreError(err);
    }
  };

  /**
   * Rebuild a missing materialized copy from its store record. The copy is a
   * derived artifact and the store is the source of truth
   * (specs/059-agent-v2-team-mode/data-model.md §2, contracts/preset-api.md
   * §3, research.md R3 实现注意③): after a pod restart empties the writable
   * layer, a recorded preset is re-materialized through the SAME
   * copy-then-patch function the create path uses, so the rebuilt copy is
   * content-equivalent to the create product and lands at the same writable
   * root (the next compose hits it — idempotent). A store miss means the
   * preset truly does not exist → NOT_FOUND.
   */
  const rebuildCopy = async (id: string, cause: unknown): Promise<AgentPreset> => {
    let record: PresetRecord;
    try {
      record = await store.get(id);
    } catch (err) {
      if (err instanceof PresetStoreError && err.code === "NOT_FOUND") {
        throw new PresetAuthoringError(
          "NOT_FOUND",
          `preset "${id}" not found: no materialized copy and no store record`,
          cause,
        );
      }
      throw mapStoreError(err);
    }

    try {
      await materializeCopy({ roster, fs }, {
        id: record.id,
        template: record.template,
        persona: record.persona,
        ...(record.displayName === undefined ? {} : { displayName: record.displayName }),
        ...(templateRules?.[record.template] === undefined
          ? {}
          : { templateRules: templateRules[record.template] }),
      });
    } catch (err) {
      // A concurrent rebuild may have won the copy race; resolving below
      // then serves the winner's copy instead of failing the compose.
      if (!(err instanceof PresetAuthoringError) || err.code !== "ALREADY_EXISTS") {
        throw err;
      }
    }

    try {
      return await roster.resolve(id);
    } catch (err) {
      throw new PresetAuthoringError(
        "INTERNAL",
        `preset "${id}" was rebuilt from its store record but is still not resolvable: ${err instanceof Error ? err.message : String(err)}`,
        err,
      );
    }
  };

  /**
   * Resolve an authored preset, transparently rebuilding its missing copy
   * from the store record via {@link rebuildCopy}. Management operations
   * share this so every preset that GET/LIST still serve also stays
   * PATCHable and DELETEable after a pod restart empties the writable layer
   * (specs/059-agent-v2-team-mode/data-model.md §2; contracts/preset-api.md
   * §3).
   */
  const resolveOrRebuild = async (id: string): Promise<AgentPreset> => {
    try {
      return await roster.resolve(id);
    } catch (err) {
      if (!(err instanceof UnknownPresetError)) {
        throw mapRosterError(err);
      }
      return rebuildCopy(id, err);
    }
  };

  return {
    async compose(presetId?: string): Promise<ComposeResult> {
      // Resolve up front so the id is snapshotted into the creation meta and
      // a broken preset fails BEFORE any session exists (V3-3 fail-fast).
      // A concrete id whose copy is missing is the pod-restart state (empty
      // writable layer, store record survives), so the copy is REBUILT from
      // the record and re-resolved (data-model.md §2 source-of-truth
      // semantics). A concrete id without a store record → NOT_FOUND; the
      // roster's default-preset resolve keeps its existing mapping
      // (INVALID_ARGUMENT/INTERNAL — mapRosterError).
      let preset: AgentPreset;
      if (presetId === undefined) {
        try {
          preset = await roster.resolve(undefined);
        } catch (err) {
          throw mapRosterError(err);
        }
      } else {
        preset = await resolveOrRebuild(presetId);
      }
      if (preset.broken !== undefined) {
        throw new PresetAuthoringError(
          "INVALID_ARGUMENT",
          `preset "${preset.id}" is broken: ${preset.broken}`,
        );
      }
      return {
        agentPreset: preset.id,
        setup: async (agentCtx: Context) => {
          await roster.mount(agentCtx, preset.id);
        },
      };
    },

    async create(input): Promise<PresetView> {
      try {
        await storeGet(input.id);
        throw new PresetAuthoringError("ALREADY_EXISTS", `preset ${input.id} already exists`);
      } catch (err) {
        // NOT_FOUND is the pass case (id unclaimed); anything else surfaces.
        if (!(err instanceof PresetAuthoringError) || err.code !== "NOT_FOUND") {
          throw err;
        }
      }

      await materializeCopy({ roster, fs }, {
        id: input.id,
        template: input.template,
        persona: input.persona,
        ...(input.displayName === undefined ? {} : { displayName: input.displayName }),
        ...(templateRules?.[input.template] === undefined
          ? {}
          : { templateRules: templateRules[input.template] }),
      });

      const now = new Date();
      try {
        await store.create({ ...input, createTime: now, updateTime: now });
      } catch (err) {
        // Store/目录同生同灭: a failed store write must not leave the copy on
        // disk (data-model.md §2; rollback is best-effort, contract §4).
        try {
          await removeMaterialization(roster, input.id);
        } catch {
          // Rollback failure must not mask the original error.
        }
        throw mapStoreError(err);
      }
      return toView(await storeGet(input.id));
    },

    async get(id): Promise<PresetView> {
      return toView(await storeGet(id));
    },

    async list(role?: string): Promise<PresetView[]> {
      // Authored copies only — templates are deployment data, not resources (R5).
      const records = await store.list();
      return records
        .filter((record) => role === undefined || record.role === role)
        .map(toView);
    },

    async update(id, patch): Promise<PresetView> {
      const record = await storeGet(id);
      if (patch.persona === undefined && patch.displayName === undefined) {
        return toView(record);
      }
      // The copy may be missing after a pod restart; rebuild it from the
      // record first so PATCH keeps working on every preset GET still serves
      // (same transparent rebuild as compose).
      await resolveOrRebuild(id);
      await updateMaterialization(
        { roster, fs },
        { id, template: record.template, patch },
      );
      const updated: PresetRecord = {
        ...record,
        persona: patch.persona ?? record.persona,
        displayName: patch.displayName ?? record.displayName,
        updateTime: new Date(),
      };
      try {
        await store.update(updated);
      } catch (err) {
        throw mapStoreError(err);
      }
      return toView(updated);
    },

    async remove(id): Promise<void> {
      // Roster first: a template id must surface FAILED_PRECONDITION (system
      // trust), not a store lookup miss. A missing copy is the pod-restart
      // state: there is nothing to delete on disk, so the store record is
      // removed directly — rebuilding a copy only to delete it would also
      // make deletion depend on a template that may itself be gone. A truly
      // unknown id stays NOT_FOUND (from the store miss); the end state
      // matches the copy-present path (no copy, no record).
      try {
        await removeMaterialization(roster, id);
      } catch (err) {
        if (!(err instanceof PresetAuthoringError) || err.code !== "NOT_FOUND") {
          throw err;
        }
      }
      try {
        await store.remove(id);
      } catch (err) {
        throw mapStoreError(err);
      }
    },
  };
}

/** Function-form cordis plugin providing `ctx.presetAuthoring`.
 *
 * With `storage: "mongo"` the Mongo store connects and indexes BEFORE the
 * plugin's activation settles — the composition boot fails loud on a
 * storage failure — and the client closes when the plugin's fiber unwinds.
 */
export async function apply(ctx: Context, config: PresetAuthoringConfig): Promise<void> {
  if (config.storage === "mongo") {
    const { store, client } = await createMongoPresetStore({
      uri: config.mongoUri ?? process.env.MONGO_URI ?? "",
      database: config.mongoDatabase,
      collection: config.mongoCollection,
    });
    ctx.effect(
      () => () => client.close(),
      "preset-authoring.storage()",
    );
    ctx.provide("presetAuthoring", createPresetAuthoring(ctx, {
      store,
      templateRules: config.templateRules,
    }));
    return;
  }
  ctx.provide("presetAuthoring", createPresetAuthoring(ctx, {
    templateRules: config.templateRules,
  }));
}
