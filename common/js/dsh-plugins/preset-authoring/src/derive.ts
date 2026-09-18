/**
 * Composition derivation for the preset-authoring plugin: the store record is
 * the single source of truth, and the composition a member materializes from
 * is DERIVED at use time — the pool template's rows with the record's persona
 * patched into the persona row — written to a throwaway file under the system
 * temp directory, then mounted through the official `mountPreset`.
 * Contract: specs/060-agent-v2-team-optimize/contracts/preset-derivation.md
 * §1/§2; specs/060-agent-v2-team-optimize/data-model.md §3.2 (DerivedComposition).
 *
 * The patch is a structured round trip (load → locate the persona row → set
 * `config.text` → dump) rather than a text-level replacement because the
 * template convention
 * (specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md §3: no
 * comment dependence, no `!!js`, exactly one persona row) makes round-trip
 * formatting loss free (specs/058-dsh-preset-roster-demo/research.md R3). An
 * EMPTY record persona leaves the template rows untouched — the role's
 * default base.
 *
 * Testability seams
 * (specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md §5
 * convention): production code receives the filesystem collaborator through
 * the {@link DeriveFs} parameter so tests inject `vi.fn()` doubles — no
 * module interception (style/javascript.md Mock convention).
 */

import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import type { AgentPreset } from "@deepseek-ai/dsh-agent-presets";
import { dump, load } from "js-yaml";

import type { PresetRecord } from "./store.js";

/**
 * Service-facing error codes
 * (specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md §6;
 * the service layer maps them onto gRPC statuses one-to-one).
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
 * specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md §5 —
 * the roster service mounted as `ctx.agentPresets` satisfies it structurally).
 * Discovery is read through `list()`: one live root scan serves both
 * pool-template lookup and the id-taken/protection checks, and no roster
 * error-class identity crosses the package boundary — the repo's link layouts
 * can load two instances of the same roster version in one process, which
 * breaks cross-instance `instanceof`
 * (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §3 roster
 * 服务面保留 resolve/list).
 */
export interface RosterSeam {
  list(): Promise<AgentPreset[]>;
}

/** Filesystem operations the composition derivation performs. */
export interface DeriveFs {
  readTextFile(path: string): Promise<string>;
  /** Create a fresh unique directory for one derived composition (mkdtemp semantics). */
  createTempDir(prefix: string): Promise<string>;
  writeTextFile(path: string, data: string): Promise<void>;
}

/** The production filesystem seam over node:fs/promises. */
export function nodeDeriveFs(): DeriveFs {
  return {
    readTextFile: (path) => readFile(path, "utf8"),
    createTempDir: (prefix) => mkdtemp(prefix),
    writeTextFile: (path, data) => writeFile(path, data, "utf8"),
  };
}

/** The persona row's package name — the patch target (specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md §3). */
export const PERSONA_ROW_NAME = "@deepseek-ai/dsh-persona";

/** The composition file name a preset directory carries (roster discovery, specs/060-agent-v2-team-optimize/data-model.md §3.2). */
export const COMPOSITION_FILE_NAME = "agent.cordis.yml";

/** Composition rows are named plugin entries; config is optional row input. */
interface CompositionRow {
  name?: unknown;
  config?: Record<string, unknown>;
}

function isCompositionRow(value: unknown): value is CompositionRow {
  return typeof value === "object" && value !== null;
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
 * Composition row rules for one template (the caller's scene lock, e.g.
 * "the player pool template carries exactly the saolei tool row and no
 * memory row"). The rule table is CALLER data: the plugin validates package
 * names as opaque strings, so no scene vocabulary enters this package
 * (specs/059-agent-v2-team-mode/contracts/preset-api.md §2; the rules are
 * declared by the hosting composition — agent_v2's cordis.yml).
 */
export interface TemplateRowRules {
  /** Package names the template must contain EXACTLY ONCE each. */
  readonly required: readonly string[];
  /** Package names the template must NOT contain. */
  readonly forbidden: readonly string[];
}

/**
 * Validate a resolved template's composition rows against the caller's
 * rules. Runs BEFORE the store write so a role-broken template can never
 * produce a preset record (fail-fast, INVALID_ARGUMENT —
 * specs/059-agent-v2-team-mode/contracts/preset-api.md §2 "模板与创作": each
 * pool template ships its role's plugin rows).
 */
export async function validateTemplateRows(
  template: AgentPreset,
  rules: TemplateRowRules,
  fs: DeriveFs,
): Promise<void> {
  const rows = parseCompositionRows(await fs.readTextFile(template.path), template.path);
  const names = rows.map((row) => (typeof row.name === "string" ? row.name : ""));
  // `?? []` keeps the validator total for callers that bypass Config and
  // pass a one-sided rule table (the schema fills the omitted half).
  for (const required of rules.required ?? []) {
    const count = names.filter((name) => name === required).length;
    if (count !== 1) {
      throw new PresetAuthoringError(
        "INVALID_ARGUMENT",
        `template "${template.id}" must contain exactly one "${required}" row, found ${count}` +
          ` (${template.path})`,
      );
    }
  }
  for (const forbidden of rules.forbidden ?? []) {
    if (names.includes(forbidden)) {
      throw new PresetAuthoringError(
        "INVALID_ARGUMENT",
        `template "${template.id}" must not contain the "${forbidden}" row (${template.path})`,
      );
    }
  }
}

/**
 * Locate the single persona row, rejecting a non-exactly-one row count with
 * INVALID_ARGUMENT — the persona patch precondition
 * (specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md §3).
 */
function locatePersonaRow(rows: CompositionRow[], compositionPath: string): CompositionRow {
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
  return rows[personaIndexes[0]];
}

/**
 * Derive the composition rows of one record over a resolved pool template:
 * the record persona replaces the persona row's `config.text`; an EMPTY
 * record persona leaves the template rows untouched (the role's default base,
 * specs/059-agent-v2-team-mode/contracts/preset-api.md §2 persona 空值). The
 * same record derives content-equivalent rows on every call — the derivation
 * is a pure function of its inputs.
 */
function deriveRows(rows: CompositionRow[], persona: string, compositionPath: string): CompositionRow[] {
  if (persona === "") {
    return rows;
  }
  const row = locatePersonaRow(rows, compositionPath);
  row.config = { ...row.config, text: persona };
  return rows;
}

/**
 * Derive one stored record's composition over its pool template:
 * `template.path` is read and its rows are patched with the record persona,
 * then serialized into a fresh file under the system temp directory and
 * returned as a synthesized {@link AgentPreset} (`trust: "user"`).
 *
 * The file is a PURE temporary product
 * (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §2): it is
 * not tracked, has no cleanup promise (its lifetime is the
 * process/container), deleting it is side-effect free, and a re-derivation
 * re-creates content-equivalent rows. Use is one mount per member creation.
 */
export async function deriveComposition(
  record: PresetRecord,
  template: AgentPreset,
  fs: DeriveFs,
): Promise<AgentPreset> {
  const rows = deriveRows(
    parseCompositionRows(await fs.readTextFile(template.path), template.path),
    record.persona,
    template.path,
  );
  const dir = await fs.createTempDir(join(tmpdir(), "dsh-preset-"));
  const path = join(dir, COMPOSITION_FILE_NAME);
  // lineWidth: -1 keeps long persona prose unwrapped across the round trip,
  // matching how the pool templates were serialized
  // (specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md §3),
  // so a re-derivation of the same record is byte-equivalent.
  await fs.writeTextFile(path, dump(rows, { lineWidth: -1 }));
  return { id: record.id, trust: "user", path };
}
