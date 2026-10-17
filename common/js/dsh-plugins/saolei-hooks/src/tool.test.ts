/**
 * `saolei_hook` tool tests (specs/078-saolei-js-hooks/contracts/
 * saolei-hooks-plugin.md §6 groups 1–2; specs/078-saolei-js-hooks/spec.md
 * FR-003/FR-004/FR-006, US2, SC-008): the eight-layer argument validation, the
 * describe content, and the constant `{result: string}` plain-text outcome.
 *
 * The tool executes against a direct registry instance and a fake
 * `ToolRunContext` (common/js/dsh-plugins/saolei-tools/src/index.test.ts
 * precedent) — no module interception (style/javascript.md §Mock 约定).
 *
 * One case is answered before `execute` runs: an action outside the schema's
 * `enum` is refused by the enforced-subset argument validation with an
 * INVALID_ARGS `ToolArgsError` (surfaced to the model as an error result) — the
 * execute-layer action check stays total for callers that bypass validation.
 */

import { ToolArgsError } from "@deepseek-ai/dsh-tools";
import type { ToolDefinition, ToolRunContext } from "@deepseek-ai/dsh-tools";
import { describe, expect, it, vi } from "vitest";

import {
  EXECUTION_TIMEOUT_MS,
  MAX_HOOKS,
  MAX_OUTPUT_CHARS,
  MAX_SCRIPT_CHARS,
} from "./limits.js";
import { HookRegistry } from "./registry.js";
import {
  createSaoleiHookTool,
  SAOLEI_HOOK_DESCRIPTION,
  SAOLEI_HOOK_TOOL_NAME,
} from "./tool.js";

/** A minimal body that compiles; returns the recognized board width. */
const SCRIPT = "return String(board.width);";

/** A fake `ToolRunContext`: the tool body reads no execution field, so the
 * double only fills the required identity shape. */
function fakeExec(): ToolRunContext {
  return {
    callId: "call-1",
    rootCallId: "call-1",
    name: SAOLEI_HOOK_TOOL_NAME,
    arguments: {},
    signal: new AbortController().signal,
    token: Symbol(),
    deferContext: vi.fn(),
    concludeTurn: vi.fn(),
  } as unknown as ToolRunContext;
}

/** Run one call and assert the canonical `{result: string}` shape — exactly one
 * key, and a string value (the tool never throws for an accepted call;
 * specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §3.3). */
async function call(tool: ToolDefinition, args: unknown): Promise<string> {
  const value = await tool.execute(args, fakeExec());
  const record = value as { result?: unknown };
  expect(Object.keys(record)).toEqual(["result"]);
  if (typeof record.result !== "string") {
    throw new Error(`expected a {result: string} outcome, got ${JSON.stringify(value)}`);
  }
  return record.result;
}

/** One call of a fresh tool over the given registry. */
function run(args: unknown, registry = new HookRegistry()): Promise<string> {
  return call(createSaoleiHookTool(registry), args);
}

describe("saolei_hook action validation", () => {
  it("refuses a call without an action", async () => {
    const result = await run({});

    expect(result).toContain(SAOLEI_HOOK_TOOL_NAME);
    expect(result).toContain("rejected");
    expect(result).toContain("action");
  });

  it("refuses name and script on the list action, naming both", async () => {
    const result = await run({ action: "list", name: "alpha", script: SCRIPT });

    expect(result).toContain("rejected");
    expect(result).toContain("list");
    expect(result).toContain("name");
    expect(result).toContain("script");
  });

  it("refuses script on the remove action, naming it", async () => {
    const result = await run({ action: "remove", name: "alpha", script: SCRIPT });

    expect(result).toContain("rejected");
    expect(result).toContain("remove");
    expect(result).toContain("script");
  });

  it("refuses an undeclared root field by name", async () => {
    const result = await run({ action: "list", typo: 1 });

    expect(result).toContain("rejected");
    expect(result).toContain("typo");
  });

  it("refuses a set without name and script, naming both", async () => {
    const result = await run({ action: "set" });

    expect(result).toContain("rejected");
    expect(result).toContain("name");
    expect(result).toContain("script");
  });

  it("refuses a set with only a name, naming script", async () => {
    const result = await run({ action: "set", name: "alpha" });

    expect(result).toContain("rejected");
    expect(result).toContain("script");
  });

  it("refuses a remove without a name", async () => {
    const result = await run({ action: "remove" });

    expect(result).toContain("rejected");
    expect(result).toContain("name");
  });

  it("refuses an action outside the enum at the schema layer", async () => {
    await expect(
      createSaoleiHookTool(new HookRegistry()).execute(
        { action: "reset" },
        fakeExec(),
      ),
    ).rejects.toBeInstanceOf(ToolArgsError);
  });
});

describe("saolei_hook set validation", () => {
  it("refuses a script beyond the length cap and names the cap", async () => {
    const result = await run({
      action: "set",
      name: "alpha",
      script: "a".repeat(MAX_SCRIPT_CHARS + 1),
    });

    expect(result).toContain("rejected");
    expect(result).toContain(String(MAX_SCRIPT_CHARS));
  });

  it("refuses a set beyond the hook cap and names the cap", async () => {
    const registry = new HookRegistry();
    for (let index = 0; index < MAX_HOOKS; index += 1) {
      registry.set(`hook-${index}`, SCRIPT);
    }

    const result = await run(
      { action: "set", name: "overflow", script: SCRIPT },
      registry,
    );

    expect(result).toContain("rejected");
    expect(result).toContain(String(MAX_HOOKS));
    expect(registry.list()).toHaveLength(MAX_HOOKS);
  });

  it("refuses a banned pattern, naming the pattern", async () => {
    const registry = new HookRegistry();

    const result = await run(
      { action: "set", name: "alpha", script: 'return require("fs");' },
      registry,
    );

    expect(result).toContain("rejected");
    expect(result).toContain("require(");
    expect(registry.list()).toEqual([]);
  });

  it("refuses a script that does not compile and reports the syntax error", async () => {
    const registry = new HookRegistry();

    const result = await run(
      { action: "set", name: "alpha", script: "const half = (\nreturn half;" },
      registry,
    );

    expect(result).toContain("rejected");
    expect(result).toContain("SyntaxError");
    expect(registry.list()).toEqual([]);
  });
});

describe("saolei_hook remove validation", () => {
  it("refuses an unknown name and lists the registered names", async () => {
    const registry = new HookRegistry();
    registry.set("alpha", SCRIPT);

    const result = await run({ action: "remove", name: "beta" }, registry);

    expect(result).toContain("rejected");
    expect(result).toContain("beta");
    expect(result).toContain("alpha");
    expect(registry.list()).toHaveLength(1);
  });
});

describe("saolei_hook actions", () => {
  it("registers a script and reports it through list", async () => {
    const registry = new HookRegistry();

    const confirmation = await run(
      { action: "set", name: "safe-cells", script: SCRIPT },
      registry,
    );
    const listed = await run({ action: "list" }, registry);

    expect(confirmation).toContain("safe-cells");
    expect(listed).toContain("safe-cells");
    expect(listed).toContain("order 0");
    expect(listed).toContain(`${SCRIPT.length} chars`);
    expect(registry.list()).toHaveLength(1);
  });

  it("updates a script in place through a same-name set", async () => {
    const registry = new HookRegistry();
    await run({ action: "set", name: "alpha", script: SCRIPT }, registry);
    await run({ action: "set", name: "beta", script: SCRIPT }, registry);

    await run({ action: "set", name: "alpha", script: 'return "new";' }, registry);

    const entries = registry.entriesInOrder();
    expect(entries.map((entry) => entry.name)).toEqual(["alpha", "beta"]);
    expect(entries[0].source).toBe('return "new";');
  });

  it("removes a script so it disappears from list", async () => {
    const registry = new HookRegistry();
    await run({ action: "set", name: "alpha", script: SCRIPT }, registry);

    const confirmation = await run({ action: "remove", name: "alpha" }, registry);
    const listed = await run({ action: "list" }, registry);

    expect(confirmation).toContain("alpha");
    expect(listed).not.toContain("alpha");
    expect(registry.list()).toEqual([]);
  });

  it("reports an empty registry through list", async () => {
    expect(await run({ action: "list" })).toContain("no hooks registered");
  });

  it("answers a refusal as a resolved text result, never a throw", async () => {
    await expect(
      run({ action: "set", name: "alpha", script: 'return require("fs");' }),
    ).resolves.toContain("rejected");
  });
});

describe("saolei_hook description", () => {
  it("covers the three actions and their parameter shapes", () => {
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("action=set");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("action=remove");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("action=list");
  });

  it("documents the board input table", () => {
    for (const token of [
      "board.width",
      "board.height",
      "board.grid[y][x]",
      "row-major",
      '"INITIAL"',
      '"0".."8"',
      '"FLAG"',
      '"HIT_MINE"',
      '"MINE"',
      '"UNKNOWN"',
      "mineCounter",
      "decoded: true, value",
      "decoded: false",
      "NEGATIVE",
    ]) {
      expect(SAOLEI_HOOK_DESCRIPTION).toContain(token);
    }
  });

  it("documents the return contract, the empty-output rule and the low-noise guidance", () => {
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("return");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("JSON.stringify");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("empty-string return announces nothing");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("low-noise");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("error: output is not serializable");
  });

  it("documents the no-persistence lifetime without naming a storage tool", () => {
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("Nothing is persisted");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("saved by the caller");
    expect(SAOLEI_HOOK_DESCRIPTION.toLowerCase()).not.toContain("memory");
  });

  it("documents the sandbox boundary and the resource caps", () => {
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("no host APIs");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("require, process, fetch");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain(`${EXECUTION_TIMEOUT_MS}ms`);
    expect(SAOLEI_HOOK_DESCRIPTION).toContain(String(MAX_OUTPUT_CHARS));
    expect(SAOLEI_HOOK_DESCRIPTION).toContain(String(MAX_SCRIPT_CHARS));
    expect(SAOLEI_HOOK_DESCRIPTION).toContain(`${MAX_HOOKS} hooks`);
  });

  it("documents the trigger semantics including the start/end exclusion", () => {
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("registration order");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("does NOT trigger");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("next step of the same turn");
  });

  it("documents the update-in-place semantics of a same-name set", () => {
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("updates that script in place");
    expect(SAOLEI_HOOK_DESCRIPTION).toContain("position in the execution order");
  });
});

describe("saolei_hook declaration", () => {
  const tool = createSaoleiHookTool(new HookRegistry());

  it("declares the tool name", () => {
    expect(tool.name).toBe(SAOLEI_HOOK_TOOL_NAME);
  });

  it("declares the canonical {result} output and renders plain text", () => {
    // The schema DSL lifts the property-level `required: true` into the
    // object's `required` array.
    expect(tool.output.schema).toEqual({
      type: "object",
      properties: {
        result: {
          type: "string",
          description: expect.any(String),
        },
      },
      required: ["result"],
      additionalProperties: false,
    });

    expect(tool.output.render({}, { result: "hello" })).toEqual([
      { type: "text", text: "hello" },
    ]);
  });

  it("declares the three optional root fields with enforced-subset keywords", () => {
    const parameters = compiledParameters(tool);

    expect(parameters.type).toBe("object");
    expect(Object.keys(parameters.properties ?? {}).sort()).toEqual([
      "action",
      "name",
      "script",
    ]);
    expect(parameters.properties?.action).toEqual({
      type: "string",
      enum: ["set", "remove", "list"],
      description: expect.any(String),
    });
    expect(parameters.required).toBeUndefined();
  });
});

/** The compiled parameter schema inspected structurally: `ToolSchema.parameters`
 * is typed as a loose record, and the enforced subset is validated by
 * `defineTool` at construction. */
function compiledParameters(tool: ToolDefinition): {
  type?: unknown;
  required?: unknown;
  properties?: Record<string, Record<string, unknown>>;
} {
  return tool.parameters as unknown as {
    type?: unknown;
    required?: unknown;
    properties?: Record<string, Record<string, unknown>>;
  };
}
