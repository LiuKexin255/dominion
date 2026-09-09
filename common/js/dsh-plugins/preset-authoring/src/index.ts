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
import type { AgentPreset } from "@deepseek-ai/dsh-agent-presets";
import z from "@deepseek-ai/schemastery";

import {
  mapRosterError,
  materializeCopy,
  nodeMaterializeFs,
  PresetAuthoringError,
  removeMaterialization,
  updateMaterialization,
} from "./materialize.js";
import type { MaterializeFs, RosterSeam } from "./materialize.js";
import { MemoryPresetStore, PresetStoreError } from "./store.js";
import type { PresetRecord, PresetStore } from "./store.js";

export { PERSONA_ROW_NAME, PresetAuthoringError } from "./materialize.js";
export type { PresetAuthoringErrorCode } from "./materialize.js";
export { MemoryPresetStore, PresetStoreError } from "./store.js";
export type { PresetRecord, PresetStore, PresetStoreErrorCode } from "./store.js";

export const name = "preset-authoring";

/** Hard dependency on the official roster being composed. */
export const inject = ["agentPresets"];

/** Validated configuration owned by the plugin. */
export interface PresetAuthoringConfig {
  storage: "memory";
}

// Schemastery's ObjectS input shape is wider than the validated output type;
// the cast mirrors the official adapters' declared `Config: z<Config>` shape
// (common/js/dsh-plugins/llm-glm/src/index.ts precedent).
export const Config: z<PresetAuthoringConfig> = z.object({
  storage: z.const("memory").default("memory"),
}) as unknown as z<PresetAuthoringConfig>;

/** The preset resource projection served to the service layer (contract §2). */
export interface PresetView {
  id: string;
  template: string;
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

/** The service face mounted as `ctx.presetAuthoring` (contract §2, R10). */
export interface PresetAuthoringService {
  compose(presetId?: string): Promise<ComposeResult>;
  create(input: { id: string; template: string; persona: string; displayName?: string }): Promise<PresetView>;
  get(id: string): Promise<PresetView>;
  list(): Promise<PresetView[]>;
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
}

function toView(record: PresetRecord): PresetView {
  return {
    id: record.id,
    template: record.template,
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

  const storeGet = async (id: string): Promise<PresetRecord> => {
    try {
      return await store.get(id);
    } catch (err) {
      throw mapStoreError(err);
    }
  };

  return {
    async compose(presetId?: string): Promise<ComposeResult> {
      // Resolve up front so the id is snapshotted into the creation meta and
      // a broken preset fails BEFORE any session exists (V3-3 fail-fast).
      // Error split per contract §6: roster semantic misses (unknown id) are
      // INVALID_ARGUMENT; unexpected failures are INTERNAL (mapRosterError).
      let preset: AgentPreset;
      try {
        preset = await roster.resolve(presetId);
      } catch (err) {
        throw mapRosterError(err);
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

      await materializeCopy({ roster, fs }, input);

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

    async list(): Promise<PresetView[]> {
      // Authored copies only — templates are deployment data, not resources (R5).
      return (await store.list()).map(toView);
    },

    async update(id, patch): Promise<PresetView> {
      const record = await storeGet(id);
      if (patch.persona === undefined && patch.displayName === undefined) {
        return toView(record);
      }
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
      // trust), not a store lookup miss.
      await removeMaterialization(roster, id);
      try {
        await store.remove(id);
      } catch (err) {
        throw mapStoreError(err);
      }
    },
  };
}

/** Function-form cordis plugin providing `ctx.presetAuthoring`. */
export function apply(ctx: Context, _config: PresetAuthoringConfig): void {
  ctx.provide("presetAuthoring", createPresetAuthoring(ctx));
}
