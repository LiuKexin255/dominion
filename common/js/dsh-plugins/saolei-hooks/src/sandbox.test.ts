/**
 * Sandbox executor tests (specs/078-saolei-js-hooks/contracts/
 * saolei-hooks-plugin.md §6 group 3; specs/078-saolei-js-hooks/spec.md
 * FR-012/FR-013, US3, SC-004): the host-API surface, the execution budget,
 * output classification, the isolation between executions and from the
 * recognized state, and the registration-time validation.
 *
 * Cases that need a host global cannot go through `compileHookScript` — the
 * banned-pattern filter refuses `process.`, `eval(` and friends at
 * registration (specs/078-saolei-js-hooks/data-model.md §2.4) — so they build
 * a raw `vm.Script` with the executor's own wrapper and drive it through
 * `runHookScript`
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §6 group 3).
 */

import { Script } from "node:vm";

import { describe, expect, it } from "vitest";

import { EXECUTION_TIMEOUT_MS, MAX_OUTPUT_CHARS, MAX_SCRIPT_CHARS } from "./limits.js";
import { compileHookScript, runHookScript } from "./sandbox.js";
import type { GameState, HookRunResult } from "./sandbox.js";

/** A 2×2 recognized board; the executor only ever sees it as a JSON snapshot. */
function fixtureBoard(): GameState {
  return {
    width: 2,
    height: 2,
    grid: [
      ["0", "1"],
      ["FLAG", "INITIAL"],
    ],
    mineCounter: { decoded: true, value: 1 },
  };
}

/** Compile through the production path, failing loudly on a rejection. */
function compile(source: string): Script {
  const result = compileHookScript(source);
  if (!result.ok) {
    throw new Error(`unexpected compile rejection: ${result.rejection}`);
  }
  return result.script;
}

function run(source: string, board: GameState = fixtureBoard()): HookRunResult {
  return runHookScript(compile(source), board);
}

/** A raw script under the executor's wrapper, bypassing the banned-pattern
 * filter — the only way to reach the context's own missing bindings. */
function runRaw(body: string, board: GameState = fixtureBoard()): HookRunResult {
  return runHookScript(new Script(`(function(board) {\n${body}\n})(board)`), board);
}

describe("saolei hook sandbox environment", () => {
  it("sees no host globals", () => {
    expect(
      run('return [typeof process, typeof require, typeof fetch].join(",");'),
    ).toEqual({ kind: "output", text: "undefined,undefined,undefined" });
  });

  it("sees only the ECMAScript builtins and the board data binding", () => {
    expect(
      run(
        'return [typeof Math, typeof JSON, typeof Array, board.width, board.grid[1][0], board.mineCounter.value].join(",");',
      ),
    ).toEqual({ kind: "output", text: "object,object,function,2,FLAG,1" });
  });
});

describe("saolei hook execution budget", () => {
  it("interrupts a runaway loop in the function body", () => {
    const started = Date.now();
    expect(run("while (true) {}")).toEqual({ kind: "timeout" });
    // Flake guard: the interrupt must land far below an unhung call rather
    // than exactly on the constant.
    expect(Date.now() - started).toBeLessThan(EXECUTION_TIMEOUT_MS * 10);
  });
});

describe("saolei hook result classification", () => {
  it("classifies an empty-string return as empty", () => {
    expect(run('return "";')).toEqual({ kind: "empty" });
  });

  it("classifies an output beyond the cap as truncated to exactly the cap", () => {
    expect(run(`return "a".repeat(${MAX_OUTPUT_CHARS + 1});`)).toEqual({
      kind: "truncated",
      text: "a".repeat(MAX_OUTPUT_CHARS),
    });
  });

  it("keeps an output exactly at the cap intact", () => {
    expect(run(`return "a".repeat(${MAX_OUTPUT_CHARS});`)).toEqual({
      kind: "output",
      text: "a".repeat(MAX_OUTPUT_CHARS),
    });
  });

  it("renders an object return deterministically through JSON.stringify", () => {
    const script = compile('return { safe: [[0, 0], [1, 1]], note: "ok" };');
    const first = runHookScript(script, fixtureBoard());
    expect(first).toEqual({
      kind: "output",
      text: '{"safe":[[0,0],[1,1]],"note":"ok"}',
    });
    expect(runHookScript(script, fixtureBoard())).toEqual(first);
  });

  it("reports a circular return value as not serializable", () => {
    expect(
      run('const self = { name: "loop" }; self.self = self; return self;'),
    ).toEqual({ kind: "error", message: "output is not serializable" });
  });

  it("reports a missing return as not serializable rather than empty", () => {
    expect(run("const unused = 1;")).toEqual({
      kind: "error",
      message: "output is not serializable",
    });
  });

  it("carries the message of a script-thrown error", () => {
    expect(run('throw new Error("boom");')).toEqual({
      kind: "error",
      message: "boom",
    });
  });

  it("classifies a direct host-global reference as a runtime error", () => {
    expect(runRaw("return process.cwd();")).toEqual({
      kind: "error",
      message: "process is not defined",
    });
  });

  it("refuses in-context code generation", () => {
    expect(runRaw('return String(eval("1 + 1"));')).toEqual({
      kind: "error",
      message: "Code generation from strings disallowed for this context",
    });
  });
});

describe("saolei hook execution isolation", () => {
  it("starts every execution from a fresh context", () => {
    // The body assigns an implicit global; a shared context would make the
    // second run observe the first one's write.
    const script = compile(
      'hookRuns = (typeof hookRuns === "number" ? hookRuns : 0) + 1; return hookRuns;',
    );
    expect(runHookScript(script, fixtureBoard())).toEqual({ kind: "output", text: "1" });
    expect(runHookScript(script, fixtureBoard())).toEqual({ kind: "output", text: "1" });
  });

  it("hands the script a snapshot, not the recognized state", () => {
    const board = fixtureBoard();
    const result = run(
      'board.width = 99; board.grid[0][0] = "MINE"; delete board.mineCounter; return String(board.width);',
      board,
    );
    expect(result).toEqual({ kind: "output", text: "99" });
    expect(board).toEqual(fixtureBoard());
  });
});

describe("saolei hook registration validation", () => {
  it.each([
    "require(",
    "import ",
    "process.",
    "globalThis.",
    "global.",
    "fetch(",
    "child_process",
    "eval(",
    "Function(",
  ])("refuses a source carrying the banned pattern %j", (pattern) => {
    const result = compileHookScript(`return ${JSON.stringify(pattern)};`);
    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("expected the banned pattern to refuse the registration");
    }
    expect(result.rejection).toContain(pattern);
  });

  it("compiles a source that only uses ECMAScript builtins", () => {
    expect(
      compileHookScript('return [typeof Math, typeof JSON, typeof Array].join(",");'),
    ).toEqual({ ok: true, script: expect.any(Script) });
  });

  it("refuses a source beyond the length cap", () => {
    const result = compileHookScript("a".repeat(MAX_SCRIPT_CHARS + 1));
    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("expected the length cap to refuse the registration");
    }
    expect(result.rejection).toContain(String(MAX_SCRIPT_CHARS));
  });

  it("compiles a source exactly at the length cap", () => {
    expect(compileHookScript("a".repeat(MAX_SCRIPT_CHARS)).ok).toBe(true);
  });

  it("reports a syntax error with a locatable position", () => {
    const result = compileHookScript("const half = (\nreturn half;");
    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("expected the syntax error to refuse the registration");
    }
    expect(result.rejection).toContain("SyntaxError");
    expect(result.rejection).toMatch(/:\d+/);
  });
});
