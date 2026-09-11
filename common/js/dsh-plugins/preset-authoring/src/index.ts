/**
 * cordis plugin entry for the preset-authoring plugin: preset CRUD over the
 * store (the single source of truth) and use-time composition derivation over
 * the official roster. Create/Update/Remove write the store only; `compose`
 * reads the record, derives the composition rows from the pool template into
 * a throwaway file and mounts them through the official `mountPreset`.
 * Export shape follows the function-form plugin contract (name / inject /
 * Config / apply, common/js/dsh-plugins/llm-glm/src/index.ts precedent);
 * registration is effect-based and disposed with the fiber.
 *
 * `ctx.presetAuthoring` is the ONLY face the service layer consumes —
 * zero roster API and zero filesystem access outside this plugin
 * (FR-005 in specs/058-dsh-preset-roster-demo/spec.md; the plugin itself
 * carries no RPC/transport concepts).
 * Contract: specs/060-agent-v2-team-optimize/contracts/preset-derivation.md;
 * specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md.
 */

import type { Context } from "@deepseek-ai/cordis";
import { mountPreset } from "@deepseek-ai/dsh-agent-presets";
import type { AgentPreset } from "@deepseek-ai/dsh-agent-presets";
import z from "@deepseek-ai/schemastery";

import {
  deriveComposition,
  nodeDeriveFs,
  PresetAuthoringError,
  validateTemplateRows,
} from "./derive.js";
import type { DeriveFs, RosterSeam, TemplateRowRules } from "./derive.js";
import { MemoryPresetStore, PresetStoreError } from "./store.js";
import type { PresetRecord, PresetStore } from "./store.js";
import { createMongoPresetStore } from "./store.mongo.js";

export { PERSONA_ROW_NAME, PresetAuthoringError, validateTemplateRows } from "./derive.js";
export type { PresetAuthoringErrorCode, TemplateRowRules } from "./derive.js";
export { MemoryPresetStore, PresetStoreError } from "./store.js";
export type { PresetRecord, PresetStore, PresetStoreErrorCode } from "./store.js";
export { createMongoPresetStore, mongoPresetCollection, MongoPresetStore } from "./store.mongo.js";
export type { MongoPresetConnection } from "./store.mongo.js";

export const name = "preset-authoring";

/** Hard dependency on the official roster being composed. */
export const inject = ["agentPresets"];

/**
 * The preset id grammar (specs/059-agent-v2-team-mode/contracts/preset-api.md
 * §2 caller-supplied ids; the official roster's own PRESET_ID is not
 * re-exported from the package root, so the same containment grammar is
 * pinned here). The id is the store key and the preset resource id, and stays
 * interchangeable with the roster's shipped template ids; malformed ids are
 * rejected at the create edge.
 */
const PRESET_ID = /^[a-z0-9][a-z0-9-]*$/;

/** Validated configuration owned by the plugin. */
export interface PresetAuthoringConfig {
  /** The preset record storage: in-memory (demo) or Mongo (agent_v2). */
  storage: "memory" | "mongo";
  /**
   * Caller-declared composition row rules, keyed by template id: the
   * hosting composition pins each pool template's role plugin rows (e.g.
   * "player" → exactly the saolei row, never a memory row) and every create
   * validates against them before the record lands
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
   * Agent-factory setup hook: mounts the record's DERIVED composition (the
   * throwaway file `deriveComposition` wrote for this call). Called from
   * `ctx.agents.create({meta, setup})`; a rejection there rolls the whole
   * agent creation back, so no half-composed session can exist.
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
  /** Roster discovery (pool templates) plus template-id protection. */
  roster?: RosterSeam;
  store?: PresetStore;
  fs?: DeriveFs;
  /** Mounts the derived composition; defaults to the official `mountPreset`. */
  mount?: (agentCtx: Context, preset: AgentPreset) => Promise<void>;
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
 * Read the roster's current presets, mapping an unexpected discovery failure
 * onto the service surface (INTERNAL, cause preserved) — the same mapping the
 * retired `resolve`-based path produced. Discovery is a live root scan, so a
 * failure here is a real deployment error, not a miss.
 */
async function listRosterPresets(roster: RosterSeam): Promise<AgentPreset[]> {
  try {
    return await roster.list();
  } catch (err) {
    throw new PresetAuthoringError(
      "INTERNAL",
      `roster operation failed: ${err instanceof Error ? err.message : String(err)}`,
      err,
    );
  }
}

/**
 * Resolve a pool template by scanning the roster's current presets. A miss is
 * the caller's INVALID_ARGUMENT (the same message shape the roster's own
 * unknown-preset error produced), and a broken composition is refused up
 * front; neither path depends on roster error-class identity, so the check
 * holds when the host composes a different package instance of the roster.
 */
async function resolveTemplate(roster: RosterSeam, templateId: string): Promise<AgentPreset> {
  const presets = await listRosterPresets(roster);
  const template = presets.find((preset) => preset.id === templateId);
  if (template === undefined) {
    throw new PresetAuthoringError(
      "INVALID_ARGUMENT",
      `unknown preset "${templateId}"; available: ${presets.map((preset) => preset.id).join(", ")}`,
    );
  }
  if (template.broken !== undefined) {
    throw new PresetAuthoringError(
      "INVALID_ARGUMENT",
      `template "${template.id}" is broken: ${template.broken}`,
    );
  }
  return template;
}

/**
 * The roster preset an id resolves to, or `undefined` when no root supplies
 * it. The pool templates are system trust; both the create collision check
 * and the remove protection read this (a list scan, not exception control
 * flow — the roster instance may come from a different package link).
 */
async function rosterPresetFor(roster: RosterSeam, id: string): Promise<AgentPreset | undefined> {
  const presets = await listRosterPresets(roster);
  return presets.find((preset) => preset.id === id);
}

/**
 * Assemble the preset-authoring service. Production resolves every
 * collaborator from the composition context; tests inject `vi.fn()` doubles
 * (style/javascript.md Mock convention — no module interception).
 */
export function createPresetAuthoring(ctx: Context, deps: PresetAuthoringDeps = {}): PresetAuthoringService {
  const roster = deps.roster ?? (ctx.agentPresets as RosterSeam);
  const store = deps.store ?? new MemoryPresetStore();
  const fs = deps.fs ?? nodeDeriveFs();
  const mount = deps.mount ?? mountPreset;
  const templateRules = deps.templateRules;

  const storeGet = async (id: string): Promise<PresetRecord> => {
    try {
      return await store.get(id);
    } catch (err) {
      throw mapStoreError(err);
    }
  };

  return {
    async compose(presetId?: string): Promise<ComposeResult> {
      // Preset selection is mandatory in this deployment (the roster default
      // points at no preset): an id-less compose fails INVALID_ARGUMENT
      // before anything resolves.
      if (presetId === undefined) {
        throw new PresetAuthoringError(
          "INVALID_ARGUMENT",
          "preset id is required: this deployment configures no default preset",
        );
      }
      // Store first: the record is the single source of truth, so a missing
      // record fails NOT_FOUND before any template read or member creation
      // (fail-fast semantics unchanged). The composition is then derived for
      // this use — there is no maintained copy and no rebuild path to
      // reconcile
      // (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §2).
      // Template problems surface here too, still before setup can create any
      // session.
      const record = await storeGet(presetId);
      const template = await resolveTemplate(roster, record.template);
      let derived: AgentPreset;
      try {
        derived = await deriveComposition(record, template, fs);
      } catch (err) {
        if (err instanceof PresetAuthoringError) {
          throw err;
        }
        throw new PresetAuthoringError(
          "INTERNAL",
          `deriving preset "${record.id}" failed: ${err instanceof Error ? err.message : String(err)}`,
          err,
        );
      }
      return {
        agentPreset: record.id,
        setup: async (agentCtx: Context) => {
          // Official direct mount: the derived file is the composition, so no
          // roster root discovery is involved; the subtree is owned by
          // agentCtx's fiber and unwinds with the member agent.
          await mount(agentCtx, derived);
        },
      };
    },

    async create(input): Promise<PresetView> {
      // A stored record already claims the id → ALREADY_EXISTS; a store miss
      // (NOT_FOUND) is the pass case.
      let claimed = true;
      try {
        await storeGet(input.id);
      } catch (err) {
        if (!(err instanceof PresetAuthoringError) || err.code !== "NOT_FOUND") {
          throw err;
        }
        claimed = false;
      }
      if (claimed) {
        throw new PresetAuthoringError("ALREADY_EXISTS", `preset ${input.id} already exists`);
      }

      if (!PRESET_ID.test(input.id)) {
        throw new PresetAuthoringError(
          "INVALID_ARGUMENT",
          `preset id "${input.id}" must match ${PRESET_ID.source}`,
        );
      }

      // Template validation before any store write: a role-broken or missing
      // template must never produce a record (INVALID_ARGUMENT, no residue).
      const template = await resolveTemplate(roster, input.template);
      const rules = templateRules?.[input.template];
      if (rules !== undefined) {
        await validateTemplateRows(template, rules, fs);
      }
      // A roster root already supplying this id keeps it: the shipped pool
      // templates are system trust, cannot be shadowed, and a record claiming
      // one could never be deleted.
      if ((await rosterPresetFor(roster, input.id)) !== undefined) {
        throw new PresetAuthoringError(
          "ALREADY_EXISTS",
          `preset "${input.id}" is already supplied by a roster root`,
        );
      }

      const now = new Date();
      try {
        await store.create({ ...input, createTime: now, updateTime: now });
      } catch (err) {
        throw mapStoreError(err);
      }
      return toView(await storeGet(input.id));
    },

    async get(id): Promise<PresetView> {
      return toView(await storeGet(id));
    },

    async list(role?: string): Promise<PresetView[]> {
      // Authored records only — templates are deployment data, not resources (R5).
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
      // Store-only: the record IS the composition source, so the next
      // materialization derives the new persona with no other write
      // (already-materialized members keep their composition — unchanged).
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
      // Template protection: an id a roster root supplies is system trust
      // and can never be deleted (FAILED_PRECONDITION), not a store lookup
      // miss. A non-roster id is the normal authored case — the store record
      // goes away and nothing else does (no copy, no fan-out).
      if ((await rosterPresetFor(roster, id)) !== undefined) {
        throw new PresetAuthoringError(
          "FAILED_PRECONDITION",
          `preset "${id}" is not writable: it ships with the deployment`,
        );
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
