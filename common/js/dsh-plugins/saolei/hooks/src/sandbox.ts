/**
 * The hook sandbox executor (specs/078-saolei-js-hooks/contracts/
 * saolei-hooks-plugin.md §4; specs/078-saolei-js-hooks/spec.md FR-012/FR-013,
 * US3): registration-time compile validation and execution-time isolation for
 * planner-authored hook scripts.
 *
 * `node:vm` is the chosen engine (specs/078-saolei-js-hooks/research.md D1).
 * It is NOT a security mechanism (https://nodejs.org/api/vm.html#vm-executing-javascript):
 * the threat model is a first-party author (the planner LLM) whose runaway
 * resources — an endless loop, an oversized output — must not stall or flood
 * the game flow, not an adversarial escape
 * (specs/078-saolei-js-hooks/spec.md Assumptions). The defense in depth is
 * exactly the contract's list: banned-pattern validation at registration,
 * `codeGeneration` disabled, no host API injected (the context sees only the
 * ECMAScript builtins plus the data binding `board`), a timeout covering the
 * script body, and output truncation.
 */

import { createContext, Script } from "node:vm";

import type { GameLogEntry } from "@dominion/dsh-saolei";

import { EXECUTION_TIMEOUT_MS, MAX_OUTPUT_CHARS, MAX_SCRIPT_CHARS } from "./limits.js";

/**
 * The board a hook script sees as its `board` binding: the recognized board
 * state carried by a board-change entry (`GameLogEntry.state`), of which the
 * sandbox injects a JSON snapshot. The canonical type is
 * `@dominion/game-saolei-board`'s `GameState`; deriving it through the game
 * foundation's export face keeps this package's dependency edge on
 * `@dominion/dsh-saolei` alone
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §1,
 * specs/078-saolei-js-hooks/spec.md FR-016).
 */
export type GameState = GameLogEntry["state"];

/** Outcome of registration-time compilation: the reusable script, or the
 * text the registration is refused with. */
export type HookCompileResult =
  | { ok: true; script: Script }
  | { ok: false; rejection: string };

/**
 * The terminal state of one hook execution
 * (specs/078-saolei-js-hooks/data-model.md §2.2). The announcement layer
 * renders each variant
 * (specs/078-saolei-js-hooks/contracts/hook-announcement.md §2); `empty` — the
 * empty-string return alone — is never announced
 * (specs/078-saolei-js-hooks/spec.md FR-008).
 */
export type HookRunResult =
  | { kind: "output"; text: string }
  | { kind: "empty" }
  | { kind: "error"; message: string }
  | { kind: "timeout" }
  | { kind: "truncated"; text: string };

/**
 * Registration-time banned substrings
 * (specs/078-saolei-js-hooks/data-model.md §2.4,
 * specs/078-saolei-js-hooks/research.md D7): a best-effort filter against an
 * author reaching for host APIs the context does not provide. The first hit
 * refuses the script and names the pattern.
 */
const BANNED_PATTERNS: readonly string[] = [
  "require(",
  "import ",
  "process.",
  "globalThis.",
  "global.",
  "fetch(",
  "child_process",
  "eval(",
  "Function(",
];

/**
 * The message for a return value with no deterministic text form: a missing
 * `return` (`undefined`) or a value `JSON.stringify` refuses — a circular
 * structure, a function, a symbol (specs/078-saolei-js-hooks/spec.md FR-008).
 */
const NOT_SERIALIZABLE = "output is not serializable";

/**
 * Filename of compiled hook scripts: syntax-error and runtime-error locations
 * then name the script itself rather than `evalmachine.<anonymous>`.
 */
const SCRIPT_FILENAME = "hook.js";

/**
 * Compile a hook function-body source for later execution
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §3.1 layers
 * 4–6): length cap, banned patterns, then the wrapped `vm.Script`
 * construction, which parses the code without running it. The one compiled
 * script is reused across every context the event handler runs it in.
 */
export function compileHookScript(source: string): HookCompileResult {
  if (source.length > MAX_SCRIPT_CHARS) {
    return {
      ok: false,
      rejection:
        `hook script exceeds the ${MAX_SCRIPT_CHARS}-character limit` +
        ` (got ${source.length})`,
    };
  }
  const banned = BANNED_PATTERNS.find((pattern) => source.includes(pattern));
  if (banned !== undefined) {
    return { ok: false, rejection: `hook script uses a banned pattern: ${banned}` };
  }
  try {
    return {
      ok: true,
      script: new Script(wrapSource(source), {
        filename: SCRIPT_FILENAME,
        // The wrapper prepends one line (`(function(board) {`), so shifting
        // the reported line numbers back by one makes them the caller's own
        // script-body lines (https://nodejs.org/api/vm.html#new-vmscriptcode-options).
        lineOffset: -1,
      }),
    };
  } catch (error) {
    return { ok: false, rejection: compileRejection(error) };
  }
}

/**
 * Run one compiled hook script against a board snapshot
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §4). Every call
 * builds a fresh context whose only injected binding is the data parameter
 * `board` — a JSON snapshot of the entry's state, so a script writing to
 * `board` never reaches the game log — with dynamic code generation disabled;
 * the execution budget interrupts the script body itself.
 */
export function runHookScript(script: Script, board: GameState): HookRunResult {
  const context = createContext(
    { board: snapshot(board) },
    { codeGeneration: { strings: false, wasm: false } },
  );
  let returned: unknown;
  try {
    returned = script.runInContext(context, { timeout: EXECUTION_TIMEOUT_MS });
  } catch (error) {
    if (isTimeout(error)) {
      return { kind: "timeout" };
    }
    return { kind: "error", message: errorMessage(error) };
  }
  return classifyOutput(returned);
}

/**
 * The compile wrapper: the hook's function-body source becomes an
 * immediately-invoked function over the data binding `board`. The call form is
 * what puts the body inside `runInContext`, so the execution budget interrupts
 * a runaway loop in the body — a host-side call of a returned function would
 * not be interruptible
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §4).
 */
function wrapSource(source: string): string {
  return `(function(board) {\n${source}\n})(board)`;
}

/**
 * Deep copy of the recognized board through JSON — the snapshot semantics
 * specs/078-saolei-js-hooks/spec.md FR-012 requires: a script mutating `board`
 * mutates only its own copy, never the game log's entry (US3 scenario 6).
 */
function snapshot(board: GameState): GameState {
  const copy: GameState = JSON.parse(JSON.stringify(board));
  return copy;
}

/**
 * Map a script return value onto the result union
 * (specs/078-saolei-js-hooks/data-model.md §2.2): a string is announced as-is;
 * any other serializable value becomes its deterministic JSON text;
 * `undefined` (no `return`) and values with no text form are
 * `error: output is not serializable` — never `empty`, which is the empty
 * string alone (specs/078-saolei-js-hooks/spec.md FR-008).
 */
function classifyOutput(returned: unknown): HookRunResult {
  if (typeof returned === "string") {
    return textResult(returned);
  }
  if (returned === undefined) {
    return { kind: "error", message: NOT_SERIALIZABLE };
  }
  let serialized: unknown;
  try {
    serialized = JSON.stringify(returned);
  } catch {
    return { kind: "error", message: NOT_SERIALIZABLE };
  }
  if (typeof serialized !== "string") {
    return { kind: "error", message: NOT_SERIALIZABLE };
  }
  return textResult(serialized);
}

/** Length classification shared by string returns and serialized returns. */
function textResult(text: string): HookRunResult {
  if (text.length === 0) {
    return { kind: "empty" };
  }
  if (text.length > MAX_OUTPUT_CHARS) {
    return { kind: "truncated", text: text.slice(0, MAX_OUTPUT_CHARS) };
  }
  return { kind: "output", text };
}

/**
 * Whether the thrown value is the execution-budget interrupt: Node throws an
 * `Error` carrying `code === "ERR_SCRIPT_EXECUTION_TIMEOUT"`
 * (https://nodejs.org/api/vm.html#scriptrunincontextcontextifiedobject-options:
 * "If execution is terminated, an Error will be thrown").
 */
function isTimeout(error: unknown): boolean {
  return errorText(error, "code") === "ERR_SCRIPT_EXECUTION_TIMEOUT";
}

/** The registration refusal text for a source that failed to compile: the
 * `SyntaxError` location (filename, line, offending source line with its
 * caret, error message), so the planner can locate the fix. */
function compileRejection(error: unknown): string {
  return `hook script does not compile:\n${compileDetail(error)}`;
}

/**
 * The compile failure detail: the V8 stack head — the position block plus the
 * `<ErrorName>: <message>` line — with the host frames below it dropped as
 * noise. Falls back to `<name>: <message>` when the runtime carries no stack.
 */
function compileDetail(error: unknown): string {
  const block = stackHead(errorText(error, "stack"));
  if (block !== undefined) {
    return block;
  }
  const name = errorText(error, "name");
  const message = errorMessage(error);
  return name === undefined ? message : `${name}: ${message}`;
}

/** The V8 stack's leading block: `<file>:<line>`, the offending source line
 * with its caret, then `<ErrorName>: <message>` (V8 separates the position
 * block from the error line by a blank line; the frames follow). */
function stackHead(stack: string | undefined): string | undefined {
  if (stack === undefined) {
    return undefined;
  }
  const sections = stack.split("\n\n");
  const position = sections[0].trim();
  const errorLine = sections[1] === undefined ? "" : sections[1].split("\n")[0].trim();
  const text = [position, errorLine].filter((part) => part.length > 0).join("\n");
  return text.length === 0 ? undefined : text;
}

function errorMessage(error: unknown): string {
  return errorText(error, "message") ?? String(error);
}

/**
 * Realm-safe property read of a thrown value. A value thrown inside a vm
 * context is not `instanceof` this module's `Error` — every context carries
 * its own intrinsics, and Node's own execution-budget interrupt is such a
 * value — so thrown values are described through the string properties they
 * carry.
 */
function errorText(
  error: unknown,
  key: "code" | "message" | "name" | "stack",
): string | undefined {
  if (typeof error !== "object" || error === null) {
    return undefined;
  }
  switch (key) {
    case "code":
      return "code" in error && typeof error.code === "string" ? error.code : undefined;
    case "message":
      return "message" in error && typeof error.message === "string"
        ? error.message
        : undefined;
    case "name":
      return "name" in error && typeof error.name === "string" ? error.name : undefined;
    case "stack":
      return "stack" in error && typeof error.stack === "string" ? error.stack : undefined;
  }
}
