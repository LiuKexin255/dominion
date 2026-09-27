/**
 * cordis plugin entry for the preset-authoring plugin: preset CRUD over the
 * store (the single source of truth) and materialization of each record into
 * the official agent-preset registry. Create/Update/Remove write the store and
 * keep the registry projection in step (register on create/boot,
 * unregister+register on update, unregister on remove); `compose` reads the
 * record and binds a new agent to its registered revision.
 * Export shape follows the function-form plugin contract (name / inject /
 * Config / apply, common/js/dsh-plugins/llm-glm/src/index.ts precedent);
 * registration is effect-based and disposed with the fiber.
 *
 * `ctx.presetAuthoring` is the ONLY face the service layer consumes — zero
 * registry API and zero template filesystem access outside this plugin
 * (FR-005 in specs/058-dsh-preset-roster-demo/spec.md; the plugin itself
 * carries no RPC/transport concepts).
 * Contract: specs/070-dsh-upgrade/contracts/preset-model-mapping.md.
 */

import type { Context } from "@deepseek-ai/cordis";
import type {
  AgentPreset,
  PresetDefinition,
} from "@deepseek-ai/dsh-agent-preset-registry";
import z from "@deepseek-ai/schemastery";

import {
  deriveComposition,
  PresetAuthoringError,
  validateTemplateRows,
} from "./derive.js";
import type { TemplateDeclaration, TemplateRowRules } from "./derive.js";
import { MemoryPresetStore, PresetStoreError } from "./store.js";
import type { PresetRecord, PresetStore } from "./store.js";
import { createMongoPresetStore } from "./store.mongo.js";

export { PERSONA_ROW_NAME, PresetAuthoringError, validateTemplateRows } from "./derive.js";
export type { PresetAuthoringErrorCode, TemplateDeclaration, TemplateRowRules, CompositionRow } from "./derive.js";
export { MemoryPresetStore, PresetStoreError } from "./store.js";
export type { PresetRecord, PresetStore, PresetStoreErrorCode } from "./store.js";
export { createMongoPresetStore, mongoPresetCollection, MongoPresetStore } from "./store.mongo.js";
export type { MongoPresetConnection } from "./store.mongo.js";

export const name = "preset-authoring";

/** Hard dependency on the official agent-preset registry being composed. */
export const inject = ["agentPresets"];

/**
 * The preset id grammar (specs/059-agent-v2-team-mode/contracts/preset-api.md
 * §2 caller-supplied ids; the registry's own PRESET_ID is not re-exported from
 * the package root, so the same containment grammar is pinned here). The id is
 * the store key and the preset resource id, and stays interchangeable with the
 * declared template ids; malformed ids are rejected at the create edge.
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
   * Template declaration snapshots keyed by template id, mirrored from the
   * hosting composition's `@deepseek-ai/dsh-agent-preset` rows: the rows a
   * record derives over (persona prefix + role tool rows) plus display
   * metadata (specs/070-dsh-upgrade/contracts/preset-model-mapping.md §2.3).
   * The plugin never reads a template directory.
   */
  templates?: Record<string, TemplateDeclaration>;
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
  templates: z.dict(
    z.object({
      name: z.string(),
      description: z.string(),
      plugins: z.array(z.any()).required(),
    }),
  ),
}) as unknown as z<PresetAuthoringConfig>;

/** The preset resource projection served to the service layer (contract §1). */
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
   * Agent-factory setup hook: binds the new agent to the record's registered
   * registry revision. Called from `ctx.agents.create({meta, setup})`; a
   * rejection there rolls the whole agent creation back, so no half-composed
   * session can exist.
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

/** The service face mounted as `ctx.presetAuthoring` (contract §1). */
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

/**
 * The agent-preset registry subset the plugin consumes
 * (`@deepseek-ai/dsh-agent-preset-registry`, `ctx.agentPresets`):
 * `register` owns eager activation and returns the declaring plugin's
 * unregister disposer; `resolve` reports the current activation diagnostic;
 * `mount` binds a new agent to the registered revision. The subset stays
 * structural so tests inject `vi.fn()` doubles (style/javascript.md Mock
 * convention).
 */
export interface AgentPresetsSeam {
  register(definition: PresetDefinition): Promise<() => Promise<void>>;
  resolve(id: string): Promise<AgentPreset>;
  mount(agentCtx: Context, id: string): Promise<AgentPreset>;
}

/** Injectable collaborators (contract §2 seam convention). */
export interface PresetAuthoringDeps {
  /** Agent-preset registry lifecycle; defaults to `ctx.agentPresets`. */
  registry?: AgentPresetsSeam;
  store?: PresetStore;
  /** Per-template declaration snapshots (see {@link PresetAuthoringConfig.templates}). */
  templates?: Record<string, TemplateDeclaration>;
  /** Per-template composition row rules (see {@link PresetAuthoringConfig.templateRules}). */
  templateRules?: Record<string, TemplateRowRules>;
}

/**
 * The plugin-owned engine: the frozen service face plus the registry lifecycle
 * the hosting fiber drives. `restore` materializes the store into the registry
 * at boot; `dispose` releases every registration this plugin owns at unload.
 */
export interface PresetAuthoringEngine {
  service: PresetAuthoringService;
  /** Materialize every stored record into the registry (process-local projection recovery). */
  restore(): Promise<void>;
  /** Release every registration this plugin owns (plugin unload). */
  dispose(): Promise<void>;
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

/** Map store rejections onto the service error surface. */
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

/** Map registry rejections onto the service error surface. */
function mapRegistryError(err: unknown): PresetAuthoringError {
  if (err instanceof PresetAuthoringError) {
    return err;
  }
  return new PresetAuthoringError(
    "INTERNAL",
    `agent preset registry operation failed: ${err instanceof Error ? err.message : String(err)}`,
    err,
  );
}

/**
 * Assemble the preset-authoring engine. Production resolves every
 * collaborator from the composition context; tests inject `vi.fn()` doubles
 * (style/javascript.md Mock convention — no module interception).
 */
export function createPresetAuthoring(
  ctx: Context,
  deps: PresetAuthoringDeps = {},
): PresetAuthoringEngine {
  const registry = deps.registry ?? (ctx.agentPresets as AgentPresetsSeam);
  const store = deps.store ?? new MemoryPresetStore();
  const templates = deps.templates ?? {};
  const templateRules = deps.templateRules;
  /** Live registrations this plugin owns: record id → registry unregister disposer. */
  const disposers = new Map<string, () => Promise<void>>();

  const warn = (message: string): void => {
    ctx.logger?.warn?.(message);
  };

  const storeGet = async (id: string): Promise<PresetRecord> => {
    try {
      return await store.get(id);
    } catch (err) {
      throw mapStoreError(err);
    }
  };

  /** Resolve a template declaration; an unknown id is the caller's INVALID_ARGUMENT. */
  const templateFor = (templateId: string): TemplateDeclaration => {
    const template = templates[templateId];
    if (template === undefined) {
      throw new PresetAuthoringError(
        "INVALID_ARGUMENT",
        `unknown template "${templateId}"; available: ${Object.keys(templates).join(", ")}`,
      );
    }
    return template;
  };

  /**
   * Register one store record and retain its unregister disposer. Returns the
   * registry's activation diagnostic after eager activation — undefined when
   * the definition is usable, the reason when it stayed in the roster broken
   * (the registry never rejects for an activation failure).
   */
  const registerRecord = async (
    record: PresetRecord,
    template: TemplateDeclaration,
  ): Promise<string | undefined> => {
    let disposer: () => Promise<void>;
    try {
      disposer = await registry.register(deriveComposition(record, template));
    } catch (err) {
      throw mapRegistryError(err);
    }
    disposers.set(record.id, disposer);
    let state: AgentPreset;
    try {
      state = await registry.resolve(record.id);
    } catch (err) {
      // Release the just-landed registration before surfacing the failure so
      // the registry projection stays consistent with the store: the caller's
      // catch paths roll the store back, and a retained disposer would leave
      // an orphan registry definition behind until plugin dispose.
      disposers.delete(record.id);
      await disposer().catch(() => undefined);
      throw mapRegistryError(err);
    }
    return state.broken;
  };

  const service: PresetAuthoringService = {
    async compose(presetId?: string): Promise<ComposeResult> {
      // Preset selection is mandatory in this deployment (the registry default
      // points at no preset): an id-less compose fails INVALID_ARGUMENT
      // before anything resolves.
      if (presetId === undefined) {
        throw new PresetAuthoringError(
          "INVALID_ARGUMENT",
          "preset id is required: this deployment configures no default preset",
        );
      }
      // Store first: the record is the single source of truth, so a missing
      // record fails NOT_FOUND before any template lookup or member creation
      // (fail-fast semantics unchanged). The template declaration is then
      // checked so a deployment gap surfaces here too, still before setup can
      // create any session.
      const record = await storeGet(presetId);
      templateFor(record.template);
      return {
        agentPreset: record.id,
        setup: async (agentCtx: Context) => {
          // Registry semantics pass through: an unregistered or broken
          // definition fails the agent's binding and rolls creation back.
          await registry.mount(agentCtx, record.id);
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
      const template = templateFor(input.template);
      // Template ids and user preset ids share the registry's global roster
      // namespace: claiming a declared template id is the caller's
      // ALREADY_EXISTS (contract §3).
      if (Object.prototype.hasOwnProperty.call(templates, input.id)) {
        throw new PresetAuthoringError(
          "ALREADY_EXISTS",
          `preset "${input.id}" is already supplied by the deployment`,
        );
      }
      const rules = templateRules?.[input.template];
      if (rules !== undefined) {
        validateTemplateRows(input.template, template.plugins, rules);
      }

      const now = new Date();
      const record: PresetRecord = { ...input, createTime: now, updateTime: now };
      try {
        await store.create(record);
      } catch (err) {
        throw mapStoreError(err);
      }

      let broken: string | undefined;
      try {
        broken = await registerRecord(record, template);
      } catch (err) {
        // Registration itself failed (no roster entry landed): roll the store
        // back so the record never diverges from the roster projection.
        await store.remove(input.id).catch(() => undefined);
        throw err;
      }
      // Creation validation and activation audit are separate: templateRules
      // reject before persistence, an activation failure leaves the definition
      // (and its record) in the roster but reports INVALID_ARGUMENT
      // (contract §2.2).
      if (broken !== undefined) {
        throw new PresetAuthoringError(
          "INVALID_ARGUMENT",
          `preset "${input.id}" activation failed: ${broken}`,
        );
      }
      return toView(await storeGet(input.id));
    },

    async get(id): Promise<PresetView> {
      return toView(await storeGet(id));
    },

    async list(role?: string): Promise<PresetView[]> {
      // Authored records only — templates are deployment data, not resources.
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
      const template = templateFor(record.template);
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

      // Store commit then registration swap: the registry retires the former
      // revision, and running agents keep their reference until release
      // (contract §2.2).
      const previous = disposers.get(id);
      if (previous !== undefined) {
        disposers.delete(id);
        try {
          await previous();
        } catch (err) {
          throw mapRegistryError(err);
        }
      }
      const broken = await registerRecord(updated, template);
      if (broken !== undefined) {
        throw new PresetAuthoringError(
          "INVALID_ARGUMENT",
          `preset "${id}" activation failed: ${broken}`,
        );
      }
      return toView(updated);
    },

    async remove(id): Promise<void> {
      // Store is the fact source: the record goes away first, then the
      // registry releases the revision (contract §2.2). An id with no record
      // is NOT_FOUND; templates carry no store record, so they are never
      // deletable through this face.
      try {
        await store.remove(id);
      } catch (err) {
        throw mapStoreError(err);
      }
      const disposer = disposers.get(id);
      if (disposer !== undefined) {
        disposers.delete(id);
        try {
          await disposer();
        } catch (err) {
          throw mapRegistryError(err);
        }
      }
    },
  };

  return {
    service,

    async restore(): Promise<void> {
      // Mongo is the single source of truth: materialize every stored record
      // into the process-local registry projection at boot. A record whose
      // template is undeclared, or whose registration rejects, is logged and
      // skipped rather than failing application startup (US3 场景 5).
      let records: PresetRecord[];
      try {
        records = await store.list();
      } catch (err) {
        throw mapStoreError(err);
      }
      for (const record of records) {
        const template = templates[record.template];
        if (template === undefined) {
          warn(
            `preset-authoring: stored preset "${record.id}" references undeclared template ` +
              `"${record.template}"; leaving it out of the roster`,
          );
          continue;
        }
        try {
          await registerRecord(record, template);
        } catch (err) {
          warn(
            `preset-authoring: stored preset "${record.id}" failed to register: ` +
              `${err instanceof Error ? err.message : String(err)}`,
          );
        }
      }
    },

    async dispose(): Promise<void> {
      const pending = [...disposers.values()];
      disposers.clear();
      for (const disposer of pending) {
        try {
          await disposer();
        } catch (err) {
          warn(
            `preset-authoring: unregister failed: ${err instanceof Error ? err.message : String(err)}`,
          );
        }
      }
    },
  };
}

/** Function-form cordis plugin providing `ctx.presetAuthoring`.
 *
 * With `storage: "mongo"` the Mongo store connects and indexes BEFORE the
 * plugin's activation settles — the composition boot fails loud on a
 * storage failure — and the client closes when the plugin's fiber unwinds.
 * Stored user presets are materialized into the registry before the service
 * is provided, so a session created right after boot sees the full roster.
 */
export async function apply(ctx: Context, config: PresetAuthoringConfig): Promise<void> {
  let store: PresetStore;
  if (config.storage === "mongo") {
    const created = await createMongoPresetStore({
      uri: config.mongoUri ?? process.env.MONGO_URI ?? "",
      database: config.mongoDatabase,
      collection: config.mongoCollection,
    });
    ctx.effect(
      () => () => created.client.close(),
      "preset-authoring.storage()",
    );
    store = created.store;
  } else {
    store = new MemoryPresetStore();
  }

  const engine = createPresetAuthoring(ctx, {
    store,
    templates: config.templates,
    templateRules: config.templateRules,
  });
  ctx.effect(
    () => () => engine.dispose(),
    "preset-authoring.registry",
  );
  await engine.restore();
  ctx.provide("presetAuthoring", engine.service);
}
