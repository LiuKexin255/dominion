/**
 * Composition derivation for the preset-authoring plugin: the store record is
 * the single source of truth, and the registry definition a record materializes
 * into is DERIVED in memory — the hosting composition's template snapshot
 * rows with the record's persona patched into the persona row — and handed to
 * `ctx.agentPresets.register`.
 * Contract: specs/070-dsh-upgrade/contracts/preset-model-mapping.md §2.1/§3;
 * specs/070-dsh-upgrade/data-model.md §3.5.
 *
 * The patch is a structural copy (locate the persona row → set
 * `config.prefix`), never a mutation of the injected template snapshot: the
 * same snapshot object backs every record derived from that template, so an
 * in-place patch would leak one record's persona into the next derivation. An
 * EMPTY record persona leaves the template's persona prefix untouched — the
 * role's default base.
 */

import type { PresetDefinition } from "@deepseek-ai/dsh-agent-preset-registry";

import type { PresetRecord } from "./store.js";

/**
 * Service-facing error codes
 * (specs/070-dsh-upgrade/contracts/preset-model-mapping.md §1; the service
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

/** The persona row's package name — the patch target
 * (specs/070-dsh-upgrade/contracts/composition-manifest.md §2 persona 子行). */
export const PERSONA_ROW_NAME = "@deepseek-ai/dsh-persona";

/**
 * One child plugin row of a declared preset, in the registry's entry-list
 * dialect: identity and display metadata live on the preset, not the row
 * (`@deepseek-ai/dsh-agent-preset-registry` `PresetDefinition`).
 */
export type CompositionRow = PresetDefinition["plugins"][number];

/**
 * A hosting composition's template definition snapshot, injected through
 * Config (`templates`,
 * specs/070-dsh-upgrade/contracts/preset-model-mapping.md §3): the pool
 * template's rows plus its display metadata, and the single source of truth
 * for that template (specs/070-dsh-upgrade/research.md D4 终态结论).
 * preset-authoring never reads the template filesystem.
 */
export interface TemplateDeclaration {
  readonly name?: string;
  readonly description?: string;
  readonly plugins: readonly CompositionRow[];
}

/**
 * Clone one template row (and its object config) so derivation never mutates
 * the injected snapshot. Non-object configs are impossible for the persona
 * row but copied as-is for non-persona rows.
 */
function cloneRow(row: CompositionRow): CompositionRow {
  const config = row.config;
  if (config === null || typeof config !== "object") {
    return { ...row };
  }
  return { ...row, config: { ...config } };
}

/**
 * Locate the single persona row, rejecting a non-exactly-one row count with
 * INVALID_ARGUMENT — the persona patch precondition
 * (specs/070-dsh-upgrade/contracts/composition-manifest.md §2 persona 子行;
 * the convention is preserved from the directory era).
 */
function locatePersonaRow(rows: readonly CompositionRow[], templateId: string): number {
  const personaIndexes = rows
    .map((row, index) => (row.name === PERSONA_ROW_NAME ? index : -1))
    .filter((index) => index >= 0);
  if (personaIndexes.length !== 1) {
    throw new PresetAuthoringError(
      "INVALID_ARGUMENT",
      `template "${templateId}" convention violated: expected exactly one ` +
        `${PERSONA_ROW_NAME} row, found ${personaIndexes.length}`,
    );
  }
  return personaIndexes[0];
}

/**
 * Derive the plugin rows of one record over a template declaration: the record
 * persona becomes the persona row's `config.prefix`; an EMPTY record persona
 * leaves the template rows untouched (the role's default base,
 * specs/059-agent-v2-team-mode/contracts/preset-api.md §2 persona 空值). The
 * same record derives content-equivalent rows on every call — the derivation
 * is a pure function of its inputs.
 */
function deriveRows(
  rows: readonly CompositionRow[],
  persona: string,
  templateId: string,
): CompositionRow[] {
  const copy = rows.map(cloneRow);
  if (persona === "") {
    return copy;
  }
  const index = locatePersonaRow(copy, templateId);
  const config = copy[index].config;
  copy[index] = {
    ...copy[index],
    config: { ...(config !== null && typeof config === "object" ? config : {}), prefix: persona },
  };
  return copy;
}

/**
 * Derive one stored record's registry registration input over its template
 * declaration: the template rows with the record persona patched in, plus the
 * preset identity and display metadata. The record's `displayName` publishes as
 * the registry name; the template declaration supplies the description (the
 * role's shared purpose). The result is passed straight to
 * `ctx.agentPresets.register`
 * (specs/070-dsh-upgrade/contracts/preset-model-mapping.md §2.1).
 */
export function deriveComposition(
  record: PresetRecord,
  template: TemplateDeclaration,
): PresetDefinition {
  return {
    id: record.id,
    plugins: deriveRows(template.plugins, record.persona, record.template),
    ...(record.displayName === undefined ? {} : { name: record.displayName }),
    ...(template.description === undefined ? {} : { description: template.description }),
  };
}
