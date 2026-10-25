/**
 * The planner-facing `saolei_hook` tool
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §3;
 * specs/078-saolei-js-hooks/spec.md FR-003/FR-004/FR-006): a single-action
 * tool over the shared hook registry — set (register/update a script), remove
 * and list.
 *
 * The tool is the only description face of the hook capability: there is no
 * prompt section at all (specs/078-saolei-js-hooks/spec.md FR-006/FR-007), so
 * `SAOLEI_HOOK_DESCRIPTION` carries the script shape, the board input table,
 * the output and triggering contracts and the resource caps in full.
 *
 * Every validation lives in `execute` and answers with a plain text result —
 * the enforced JSON Schema subset has no mutual-exclusion or numeric-bound
 * keyword, so a schema-level `oneOf`/`maxLength` would turn a recoverable
 * model mistake into an INVALID_ARGS failure (memory-tool precedent,
 * common/js/memory/src/tool.ts; survey/saolei-hook-scripts.md §1.5).
 */

import { defineTool } from "@deepseek-ai/dsh-tools";
import type { ToolDefinition } from "@deepseek-ai/dsh-tools";

import {
  EXECUTION_TIMEOUT_MS,
  MAX_HOOKS,
  MAX_OUTPUT_CHARS,
  MAX_SCRIPT_CHARS,
} from "./limits.js";
import type { HookRegistry } from "./registry.js";

/** The tool name; the planner-only write/read face for the hook registry. */
export const SAOLEI_HOOK_TOOL_NAME = "saolei_hook";

/** The three mutually exclusive actions. */
const SAOLEI_HOOK_ACTIONS = ["set", "remove", "list"] as const;

type SaoleiHookAction = (typeof SAOLEI_HOOK_ACTIONS)[number];

/**
 * Root fields of the parameter schema — all optional; which combination is
 * legal per action is checked in `execute` and answered with text
 * (specs/078-saolei-js-hooks/data-model.md §3.2).
 */
export interface SaoleiHookToolArgs {
  action?: SaoleiHookAction;
  /** set/remove: the hook name (the registry identity). */
  name?: string;
  /** set: the function-body script source. */
  script?: string;
}

/** The non-`action` fields each action accepts; any other field present is a
 * recoverable argument mistake answered with text
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §3.1 layer 2).
 * `action` is exempt from the check — it selects the action rather than being
 * one of its fields. */
const ACCEPTED_FIELDS: Readonly<Record<SaoleiHookAction, readonly string[]>> = {
  set: ["name", "script"],
  remove: ["name"],
  list: [],
};

/**
 * The model-facing description: a generic statement of the tool's own function
 * (no deployment-specific scenario nouns; memory-tool wording discipline,
 * common/js/memory/src/tool.ts, specs/078-saolei-js-hooks/research.md D9)
 * covering the eight items of
 * specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §3.2. The resource
 * caps are interpolated from ./limits.js so the text cannot drift from the
 * enforced bounds.
 */
export const SAOLEI_HOOK_DESCRIPTION =
  "Manage the minesweeper hook scripts. A hook script runs automatically " +
  "against the recognized board after each batch of cell operations that " +
  "changes the board, and every non-empty result is broadcast to the team. " +
  "Three mutually exclusive actions: action=set registers or updates one " +
  "script (name and script); action=remove deletes one script (name); " +
  "action=list reports the registered scripts (no other fields). " +
  "\n\n" +
  "A script is the BODY of a synchronous function whose input is the board as " +
  "a JSON object `board`: board.width and board.height give the board size; " +
  "board.grid[y][x] is one cell symbol, row-major (grid[0] is the top row), " +
  'with symbols "INITIAL" (unrevealed cell), "0".."8" (revealed number), ' +
  '"FLAG", "HIT_MINE", "MINE" and "UNKNOWN" (recognition uncertain); ' +
  "board.mineCounter is optional and holds either {decoded: true, value}, " +
  "where value = remaining mines minus flags (NEGATIVE when over-flagged), or " +
  "{decoded: false} when the counter could not be read (then it is absent). " +
  "The body must `return` a string or a JSON-serializable object, which is " +
  "rendered through JSON.stringify. The board is a JSON snapshot: writing to " +
  "`board` never changes the game state. " +
  "\n\n" +
  "An empty-string return announces nothing, so return text only when it " +
  "carries a useful conclusion and keep the scripts low-noise. A body with no " +
  "`return` (undefined) or a non-serializable return value is announced as " +
  "`error: output is not serializable`. " +
  "\n\n" +
  "Nothing is persisted: the registry is empty for every team refresh/rebuild " +
  "and is discarded when the team is disposed. A script that should survive a " +
  "refresh must be saved by the caller and registered again in the new team. " +
  "\n\n" +
  "Scripts run synchronously in a restricted sandbox with no host APIs: " +
  "require, process, fetch, timers and network are unavailable; only the " +
  "ECMAScript builtins and the `board` data are visible. Limits: one run may " +
  `take at most ${EXECUTION_TIMEOUT_MS}ms (a longer run is interrupted and ` +
  "announced as a timeout); output beyond " +
  `${MAX_OUTPUT_CHARS} characters is truncated with a marker; a script body ` +
  `may be at most ${MAX_SCRIPT_CHARS} characters; at most ${MAX_HOOKS} hooks ` +
  "may be registered. " +
  "\n\n" +
  "Triggering: after a board-changing batch of cell operations, every " +
  "registered hook runs in registration order on the new board and each " +
  "non-empty result is broadcast to the team — a player in the middle of its " +
  "turn sees it at the next step of the same turn. Starting or ending a game " +
  "does NOT trigger the hooks. " +
  "\n\n" +
  "action=set on an existing name updates that script in place and keeps its " +
  "position in the execution order.";

/** The canonical `{result: string}` output (saolei tools' declaration shape;
 * specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §3.3). */
const RESULT_OUTPUT_SCHEMA = {
  type: "object",
  properties: {
    result: {
      type: "string",
      required: true,
      description:
        "The saolei_hook result text: the action confirmation, the script list, or `saolei_hook → rejected: …`",
    },
  },
  additionalProperties: false,
} as const;

/**
 * The parameter schema. All three root fields are optional (the action
 * combination is a runtime contract answered with text) and only
 * `@deepseek-ai/dsh-tools` `CONSTRAINT_KEYWORDS`-subset keywords are used —
 * the enforced subset has no numeric-bound keyword, so the caps are checked in
 * `execute` (survey/saolei-hook-scripts.md §1.5).
 *
 * The subset's parameter root is an implicit OPEN object (dsh-tools'
 * `parameterSchemaSpecToJsonSchema` emits `type`/`properties`/`required`
 * only), so `additionalProperties: false` cannot be declared at the root;
 * `unexpectedFields` enforces the same strict-root contract in `execute`,
 * where an unknown key is refused by name together with the non-applicable
 * fields (a silently ignored typo would be worse than a text refusal the model
 * can correct — common/js/memory/src/tool.ts precedent).
 */
export const SAOLEI_HOOK_PARAMETERS = {
  action: {
    type: "string",
    enum: SAOLEI_HOOK_ACTIONS,
    description:
      "the operation: set (register/update a script), remove (delete a script) or list (report the registered scripts)",
  },
  name: {
    type: "string",
    description: "the hook name (set/remove)",
  },
  script: {
    type: "string",
    description: "the hook function-body source (set)",
  },
} as const;

/** One validated call, narrowed to the fields its action uses. */
type NormalizedCall =
  | { readonly action: "set"; readonly name: string; readonly script: string }
  | { readonly action: "remove"; readonly name: string }
  | { readonly action: "list" };

/**
 * Build the registry-ready `saolei_hook` tool over one registry: the tool
 * writes to the same store the player-side handler reads, so a unit constructs
 * both over one instance
 * (specs/078-saolei-js-hooks/data-model.md §3.1).
 */
export function createSaoleiHookTool(registry: HookRegistry): ToolDefinition {
  return defineTool({
    name: SAOLEI_HOOK_TOOL_NAME,
    description: SAOLEI_HOOK_DESCRIPTION,
    parameters: SAOLEI_HOOK_PARAMETERS,
    output: {
      schema: RESULT_OUTPUT_SCHEMA,
      render: (_args, value) => [{ type: "text", text: value.result }],
    },
    execute: (args) => Promise.resolve(executeSaoleiHook(args, registry)),
  });
}

/**
 * Run one call, mapping the validation layers
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §3.1) onto the
 * canonical text result: the action, then the per-action field check, then the
 * registry write, whose rejection already carries the cap/pattern/compile
 * detail and the registered-name list.
 */
function executeSaoleiHook(
  args: SaoleiHookToolArgs,
  registry: HookRegistry,
): { result: string } {
  const normalized = normalizeCall(args);
  if ("rejection" in normalized) {
    return { result: normalized.rejection };
  }
  switch (normalized.action) {
    case "set": {
      const write = registry.set(normalized.name, normalized.script);
      return {
        result: write.ok
          ? `registered hook "${normalized.name}"`
          : refuse(write.rejection),
      };
    }
    case "remove": {
      const write = registry.remove(normalized.name);
      return {
        result: write.ok
          ? `removed hook "${normalized.name}"`
          : refuse(write.rejection),
      };
    }
    case "list":
      return { result: listText(registry) };
  }
}

/**
 * The eight-layer validation prefix: layers 1–3 (action, per-action field
 * combination, required fields) — everything the registry does not decide. The
 * returned rejection is the full model-facing text.
 */
function normalizeCall(
  args: SaoleiHookToolArgs,
): NormalizedCall | { rejection: string } {
  const action = readAction(args);
  if (action === undefined) {
    return {
      rejection: refuse(
        `provide exactly one action: ${SAOLEI_HOOK_ACTIONS.join(", ")}`,
      ),
    };
  }
  const unexpected = unexpectedFields(args, action);
  if (unexpected.length > 0) {
    return {
      rejection: refuse(
        `the ${action} action does not accept: ${unexpected.join(", ")}`,
      ),
    };
  }
  switch (action) {
    case "set": {
      if (args.name === undefined || args.script === undefined) {
        const missing: string[] = [];
        if (args.name === undefined) {
          missing.push("name");
        }
        if (args.script === undefined) {
          missing.push("script");
        }
        return {
          rejection: refuse(`the set action requires ${missing.join(" and ")}`),
        };
      }
      return { action, name: args.name, script: args.script };
    }
    case "remove":
      if (args.name === undefined) {
        return { rejection: refuse("the remove action requires name") };
      }
      return { action, name: args.name };
    case "list":
      return { action };
  }
}

/** The action, or undefined when it is absent or outside the enum. The enum is
 * also enforced by the parameter schema; the check keeps the validation
 * contract total for direct callers. */
function readAction(args: SaoleiHookToolArgs): SaoleiHookAction | undefined {
  const action: unknown = args.action;
  return typeof action === "string" &&
    (SAOLEI_HOOK_ACTIONS as readonly string[]).includes(action)
    ? (action as SaoleiHookAction)
    : undefined;
}

/**
 * The fields present that the action does not accept, sorted for a stable
 * text: the declared-but-inapplicable ones (name/script on list, script on
 * remove) plus any key outside the declared root (the strict-root contract the
 * subset cannot express; see {@link SAOLEI_HOOK_PARAMETERS}).
 */
function unexpectedFields(
  args: SaoleiHookToolArgs,
  action: SaoleiHookAction,
): string[] {
  // The declared interface has no index signature, so the dynamic key walk
  // goes through a plain record view.
  const record = args as unknown as Record<string, unknown>;
  return Object.keys(record)
    .filter(
      (key) => key !== "action" && !ACCEPTED_FIELDS[action].includes(key),
    )
    .filter((key) => record[key] !== undefined)
    .sort();
}

/** The `list` action's text: the registered hooks in execution order with
 * their order and script length (specs/078-saolei-js-hooks/spec.md US2
 * scenario 1). */
function listText(registry: HookRegistry): string {
  const entries = registry.list();
  if (entries.length === 0) {
    return `${SAOLEI_HOOK_TOOL_NAME} → no hooks registered`;
  }
  const lines = entries.map(
    (entry) => `- ${entry.name} (order ${entry.order}, ${entry.length} chars)`,
  );
  return `registered hooks (execution order):\n${lines.join("\n")}`;
}

/** The shared refusal prefix, mirroring the saolei_operate `→ rejected:` shape
 * (common/js/dsh-plugins/saolei/tools/src/index.ts). */
function refuse(detail: string): string {
  return `${SAOLEI_HOOK_TOOL_NAME} → rejected: ${detail}`;
}
