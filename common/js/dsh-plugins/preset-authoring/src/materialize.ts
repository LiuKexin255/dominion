/**
 * Copy-then-patch materialization for the preset-authoring plugin:
 * roster copy → js-yaml round-trip patch of the copy's persona row →
 * best-effort rollback of a half-materialized copy.
 * Contract: specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md §4.
 *
 * The patch is a structured round trip (load → locate the persona row →
 * set `config.text` → dump) rather than a text-level replacement because the
 * template convention (composition-manifest.md §3: no comment dependence, no
 * `!!js`, exactly one persona row) makes round-trip formatting loss free
 * (specs/058-dsh-preset-roster-demo/research.md R3).
 *
 * Testability seams (contract §5): production code receives the roster and
 * filesystem collaborators through the {@link RosterSeam} / {@link MaterializeFs}
 * parameters, so tests inject `vi.fn()` doubles — no module interception
 * (style/javascript.md Mock convention).
 */

import { randomBytes } from "node:crypto";
import { readFile, rename, rm, writeFile } from "node:fs/promises";
import { dirname } from "node:path";

import type { Context } from "@deepseek-ai/cordis";
import type { AgentPreset } from "@deepseek-ai/dsh-agent-presets";
import {
  InvalidPresetIdError,
  PresetExistsError,
  PresetNotWritableError,
  UnknownPresetError,
} from "@deepseek-ai/dsh-agent-presets";
import { load, dump } from "js-yaml";

/**
 * Service-facing error codes (preset-authoring-plugin.md §6; the service
 * layer maps them onto gRPC statuses one-to-one).
 */
export type PresetAuthoringErrorCode =
  | "INVALID_ARGUMENT"
  | "ALREADY_EXISTS"
  | "NOT_FOUND"
  | "FAILED_PRECONDITION"
  | "INTERNAL";

export class PresetAuthoringError extends Error {
  readonly code: PresetAuthoringErrorCode;
  /** The originating error, when one was wrapped (diagnostic cause chain). */
  readonly cause?: unknown;

  constructor(code: PresetAuthoringErrorCode, message: string, cause?: unknown) {
    super(message);
    this.name = "PresetAuthoringError";
    this.code = code;
    if (cause !== undefined) {
      this.cause = cause;
    }
  }
}

/**
 * The roster surface the plugin consumes (official `AgentPresets` API subset,
 * preset-authoring-plugin.md §5 — the roster service mounted as
 * `ctx.agentPresets` satisfies it structurally).
 */
export interface RosterSeam {
  resolve(id?: string): Promise<AgentPreset>;
  mount(agentCtx: Context, id?: string): Promise<AgentPreset>;
  copy(from: string, id: string, name?: string): Promise<void>;
  remove(id: string): Promise<void>;
}

/** Filesystem operations the materializer performs (atomic-write contract). */
export interface MaterializeFs {
  readTextFile(path: string): Promise<string>;
  /** Durably replace a file: temp file in the same directory, then rename. */
  writeTextFileAtomic(path: string, data: string): Promise<void>;
  /** Recursive force removal (rollback of a half-materialized copy). */
  removeDeep(path: string): Promise<void>;
}

/** The production filesystem seam over node:fs/promises. */
export function nodeMaterializeFs(): MaterializeFs {
  return {
    readTextFile: (path) => readFile(path, "utf8"),
    writeTextFileAtomic: async (path, data) => {
      const temp = `${path}.${randomBytes(6).toString("hex")}.tmp`;
      await writeFile(temp, data, "utf8");
      await rename(temp, path);
    },
    removeDeep: (path) => rm(path, { recursive: true, force: true }),
  };
}

/** The persona row's package name — the patch target (composition-manifest.md §3). */
export const PERSONA_ROW_NAME = "@deepseek-ai/dsh-persona";

/**
 * The preset id grammar (data-model.md §1). The roster's own `PRESET_ID`
 * regex is not re-exported from the package root, so the same containment
 * grammar is pinned here: the id becomes a directory name, so this check is
 * a containment boundary, not a style rule.
 */
const PRESET_ID = /^[a-z0-9][a-z0-9-]*$/;

/** Composition rows are named plugin entries; config is optional row input. */
interface CompositionRow {
  name?: unknown;
  config?: Record<string, unknown>;
}

function isCompositionRow(value: unknown): value is CompositionRow {
  return typeof value === "object" && value !== null;
}

/**
 * Map a roster rejection onto the service error surface (§6). Discrimination
 * is `instanceof` against the roster package's exported error classes
 * (`UnknownPresetError`: lib/types/preset.d.ts; `InvalidPresetIdError` /
 * `PresetExistsError` / `PresetNotWritableError`: lib/types/authoring.d.ts of
 * `@deepseek-ai/dsh-agent-presets@0.1.1-rc.2`) — never message-string
 * matching. Anything else is an unexpected failure and maps to INTERNAL with
 * the original error preserved as `cause`.
 */
export function mapRosterError(err: unknown): PresetAuthoringError {
  if (err instanceof UnknownPresetError) {
    return new PresetAuthoringError(
      "INVALID_ARGUMENT",
      `unknown preset "${err.presetId}"; available: ${err.available.join(", ")}`,
      err,
    );
  }
  if (err instanceof PresetExistsError) {
    return new PresetAuthoringError(
      "ALREADY_EXISTS",
      `preset "${err.presetId}" already exists on disk`,
      err,
    );
  }
  if (err instanceof InvalidPresetIdError) {
    return new PresetAuthoringError(
      "INVALID_ARGUMENT",
      `preset id "${err.presetId}" is not a usable preset id`,
      err,
    );
  }
  if (err instanceof PresetNotWritableError) {
    return new PresetAuthoringError(
      "FAILED_PRECONDITION",
      `preset "${err.presetId}" is not writable: ${err.message}`,
      err,
    );
  }
  return new PresetAuthoringError(
    "INTERNAL",
    `roster operation failed: ${err instanceof Error ? err.message : String(err)}`,
    err,
  );
}

async function removeCopyBestEffort(roster: RosterSeam, id: string): Promise<void> {
  try {
    await roster.remove(id);
  } catch {
    // Best-effort per contract §4: a rollback failure must not mask the
    // original error; the leftover directory surfaces as a broken roster row.
  }
}

/** Parse composition rows, rejecting a non-list composition (fail loud). */
function parseCompositionRows(text: string, compositionPath: string): CompositionRow[] {
  let parsed: unknown;
  try {
    parsed = load(text);
  } catch (err) {
    throw new PresetAuthoringError(
      "INTERNAL",
      `composition ${compositionPath} does not parse: ${err instanceof Error ? err.message : String(err)}`,
    );
  }
  if (!Array.isArray(parsed) || !parsed.every(isCompositionRow)) {
    throw new PresetAuthoringError(
      "INTERNAL",
      `composition ${compositionPath} is not a list of plugin rows`,
    );
  }
  return parsed;
}

/**
 * Rewrite the copy's persona row `config.text` in place (atomic write).
 * Throws INVALID_ARGUMENT when the template convention is violated (the
 * persona row count is not exactly one) — the C1 patch precondition
 * (composition-manifest.md §3).
 */
export async function patchPersona(
  compositionPath: string,
  persona: string,
  fs: MaterializeFs,
): Promise<void> {
  const rows = parseCompositionRows(await fs.readTextFile(compositionPath), compositionPath);
  const personaIndexes = rows
    .map((row, index) => (row.name === PERSONA_ROW_NAME ? index : -1))
    .filter((index) => index >= 0);
  if (personaIndexes.length !== 1) {
    throw new PresetAuthoringError(
      "INVALID_ARGUMENT",
      `template convention violated in ${compositionPath}: expected exactly one ` +
        `${PERSONA_ROW_NAME} row, found ${personaIndexes.length}`,
    );
  }
  const row = rows[personaIndexes[0]];
  row.config = { ...row.config, text: persona };
  // lineWidth: -1 keeps long persona prose unwrapped across the round trip.
  await fs.writeTextFileAtomic(compositionPath, dump(rows, { lineWidth: -1 }));
}

/**
 * Rewrite the copy's display metadata (`name` + the template's `description`;
 * the composition file is untouched, so the generation stamp does not move —
 * data-model.md §2 update(displayName) semantics).
 */
export async function rewritePresetMetadata(
  presetDir: string,
  displayName: string,
  description: string | undefined,
  fs: MaterializeFs,
): Promise<void> {
  const metadata = description === undefined ? { name: displayName } : { name: displayName, description };
  await fs.writeTextFileAtomic(`${presetDir}/preset.yml`, dump(metadata, { lineWidth: -1 }));
}

/** Input of {@link materializeCopy} (contract §4 create steps 2-4). */
export interface MaterializeCopyInput {
  id: string;
  template: string;
  persona: string;
  displayName?: string;
}

/**
 * Create the materialized copy: resolve the template, roster-copy it under
 * the writable root (the display name rides the copy's third parameter into
 * the copy's `preset.yml`), then patch the copy's persona row. Any failure
 * after the copy landed rolls the half-materialized directory back
 * (best-effort) before the error propagates — store state is the caller's
 * concern, so the caller rolls its own record back on its own failures.
 */
export async function materializeCopy(
  deps: { roster: RosterSeam; fs: MaterializeFs },
  input: MaterializeCopyInput,
): Promise<void> {
  if (!PRESET_ID.test(input.id)) {
    throw new PresetAuthoringError(
      "INVALID_ARGUMENT",
      `preset id "${input.id}" must match ${PRESET_ID.source}`,
    );
  }

  let template: AgentPreset;
  try {
    template = await deps.roster.resolve(input.template);
  } catch (err) {
    throw mapRosterError(err);
  }
  if (template.broken !== undefined) {
    throw new PresetAuthoringError(
      "INVALID_ARGUMENT",
      `template "${template.id}" is broken: ${template.broken}`,
    );
  }

  try {
    await deps.roster.copy(input.template, input.id, input.displayName ?? input.id);
  } catch (err) {
    throw mapRosterError(err);
  }

  try {
    const copy = await deps.roster.resolve(input.id);
    await patchPersona(copy.path, input.persona, deps.fs);
  } catch (err) {
    await removeCopyBestEffort(deps.roster, input.id);
    if (err instanceof PresetAuthoringError) {
      throw err;
    }
    throw new PresetAuthoringError(
      "INTERNAL",
      `materializing preset ${input.id} failed: ${err instanceof Error ? err.message : String(err)}`,
      err,
    );
  }
}

/** Input of {@link updateMaterialization} (contract §4 update). */
export interface UpdatePatch {
  persona?: string;
  displayName?: string;
}

/**
 * Apply an update to the copy's files: `persona` re-patches the composition
 * (a new file stamp → new generation for later sessions); `displayName`
 * rewrites only `preset.yml` (name + the template's description).
 */
export async function updateMaterialization(
  deps: { roster: RosterSeam; fs: MaterializeFs },
  input: { id: string; template: string; patch: UpdatePatch },
): Promise<void> {
  let copy: AgentPreset;
  try {
    copy = await deps.roster.resolve(input.id);
  } catch (err) {
    // The store record exists, so the copy directory must too; a missing
    // directory means store/disk divergence, which fails loud as INTERNAL.
    if (err instanceof UnknownPresetError) {
      throw new PresetAuthoringError(
        "INTERNAL",
        `preset ${input.id} is recorded but its copy is missing from the roster`,
      );
    }
    throw mapRosterError(err);
  }

  if (input.patch.persona !== undefined) {
    await patchPersona(copy.path, input.patch.persona, deps.fs);
  }

  if (input.patch.displayName !== undefined) {
    let template: AgentPreset;
    try {
      template = await deps.roster.resolve(input.template);
    } catch (err) {
      throw mapRosterError(err);
    }
    await rewritePresetMetadata(
      dirname(copy.path),
      input.patch.displayName,
      template.description,
      deps.fs,
    );
  }
}

/**
 * Remove the materialized copy's directory (contract §4 remove). Roster
 * refusals surface verbatim through {@link mapRosterError}: a system-trust
 * template hits FAILED_PRECONDITION; an unknown id maps to NOT_FOUND.
 */
export async function removeMaterialization(roster: RosterSeam, id: string): Promise<void> {
  try {
    await roster.remove(id);
  } catch (err) {
    if (err instanceof UnknownPresetError) {
      throw new PresetAuthoringError("NOT_FOUND", `preset ${id} not found`);
    }
    throw mapRosterError(err);
  }
}
