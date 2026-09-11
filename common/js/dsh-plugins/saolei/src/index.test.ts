/**
 * saolei tools plugin tests (contract saolei-plugins.md §3/§7.3): the three
 * global tool registrations and the `saolei:guidance` prompt section, the
 * dual-form argument validation with the verbatim v1 literals, the exec-time
 * resolution of the agent-scoped `saoleiGame` service through
 * `exec.agent.ctx`, the ToolOutcome.isError → throw mapping, and the
 * fail-loud paths (no `exec.agent`; service absent from the caller's scope).
 * The guidance-ownership cases (tool usage only; game rules live in the
 * saolei-loop plugin's `saolei:game` section) follow
 * specs/060-agent-v2-team-optimize/contracts/prompt-sections.md §2.
 *
 * Pattern (style/javascript.md Mock convention): `apply` runs against a real
 * cordis Context whose `tools`/`systemPrompt` services are `vi.fn()`-based
 * doubles capturing registrations; tools execute directly against their
 * captured definitions with a fake ToolRunContext — no module interception.
 */

import { Context } from "@deepseek-ai/cordis";
import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import type { ToolRunContext } from "@deepseek-ai/dsh-tools";
import { describe, expect, it, vi } from "vitest";

import { apply } from "./index.js";
import {
  AMBIGUOUS_ARGS_TEXT,
  INCOMPLETE_ARGS_TEXT,
  MISSING_ARGS_TEXT,
  SAOLEI_GUIDANCE,
} from "./index.js";
import type { SaoleiGame } from "@dominion/dsh-saolei-loop";

/** Structural caller-agent face the plugin consumes (id + ctx). */
interface CallerAgent {
  id: string;
  ctx: Context;
}

/** A fake `ToolRunContext` over the given caller agent. */
function fakeExec(agent?: CallerAgent): ToolRunContext {
  return {
    callId: "call-1",
    rootCallId: "call-1",
    name: "saolei_init",
    arguments: {},
    signal: new AbortController().signal,
    ...(agent === undefined ? {} : { agent }),
    token: Symbol(),
    deferContext: vi.fn(),
  } as unknown as ToolRunContext;
}

/** A caller agent whose ctx exposes the given `saoleiGame` (or none). */
function fakeAgent(runtime?: SaoleiGame): CallerAgent {
  const ctx = new Context();
  if (runtime !== undefined) {
    ctx.provide("saoleiGame", runtime);
  }
  return { id: "templates/saolei/sessions/t1", ctx };
}

/** A stub runtime whose methods are vi.fn()s (fail-loud resolution tests use
 * it for call-through assertions; text results are canned per test). */
function stubRuntime(outcomes: Partial<Record<"init" | "operate" | "remain", unknown>>): SaoleiGame {
  return {
    init: vi.fn(async () => outcomes.init),
    operate: vi.fn(async () => outcomes.operate),
    remain: vi.fn(() => outcomes.remain),
    peekGameEvent: vi.fn(() => null),
  } as unknown as SaoleiGame;
}

/** Run `apply` against a harness context and capture registrations. */
function makeHarness() {
  const ctx = new Context();
  const tools = new Map<string, ToolDefinition>();
  ctx.provide("tools", {
    register: vi.fn((definition: ToolDefinition) => {
      tools.set(definition.name, definition);
      return () => {};
    }),
  });
  const sections: Array<{ name: string; order: number; text: string }> = [];
  ctx.provide("systemPrompt", {
    section: vi.fn((section: { name: string; order: number; text: string }) => {
      sections.push(section);
      return () => {};
    }),
  });
  apply(ctx);
  return { ctx, tools, sections };
}

describe("saolei plugin registration", () => {
  it("registers exactly the three tools and the guidance section", () => {
    const { tools, sections } = makeHarness();

    expect([...tools.keys()].sort()).toEqual(["saolei_init", "saolei_operate", "saolei_remain"]);
    expect(sections).toHaveLength(1);
    expect(sections[0]).toMatchObject({ name: "saolei:guidance", order: 100 });
    // The section carries the migrated guidance substance.
    expect(SAOLEI_GUIDANCE).toContain("saolei_operate");
    expect(SAOLEI_GUIDANCE).toContain("game status:");
    expect(SAOLEI_GUIDANCE).toContain("saolei_remain");
  });

  it("declares the dual-form operate parameters and empty schemas for the no-arg tools", () => {
    const { tools } = makeHarness();

    // The registry compiles the DSL to JSON Schema: {type: object, properties}.
    const propertiesOf = (name: string): Record<string, unknown> =>
      (tools.get(name)?.parameters as { properties?: Record<string, unknown> }).properties ?? {};

    expect(Object.keys(propertiesOf("saolei_init"))).toEqual([]);
    expect(Object.keys(propertiesOf("saolei_remain"))).toEqual([]);
    expect(Object.keys(propertiesOf("saolei_operate")).sort()).toEqual([
      "operations",
      "type",
      "x",
      "y",
    ]);
    // No parameter is schema-required: presence validation is semantic
    // (the verbatim literal bodies), not schema-level.
    const schema = tools.get("saolei_operate")?.parameters as { required?: string[] };
    expect(schema.required).toBeUndefined();
  });

  it("declares the canonical {result: string} output with a text render", async () => {
    const { tools } = makeHarness();
    const definition = tools.get("saolei_init");
    expect(definition?.output).toBeDefined();

    const rendered = definition!.output.render({}, { result: "board text" });
    expect(rendered).toEqual([{ type: "text", text: "board text" }]);
  });
});

describe("saolei guidance ownership (prompt-sections.md §2)", () => {
  it("keeps the tool-usage substance after the game-rules split", () => {
    for (const kept of [
      "## saolei (Minesweeper tools)",
      "| `*` |",
      "| `F` |",
      "| `X` |",
      "| `M` |",
      "| `?` |",
      "col0 col1",
      "(x, y)",
      "game status:",
      "saolei_init()",
      "saolei_operate",
      "saolei_remain()",
      "SKIPPED",
      "STOPS",
      "no_active_game",
      "Example flow",
      "Do not",
    ]) {
      expect(SAOLEI_GUIDANCE).toContain(kept);
    }
  });

  it("states no game rules (they live in the saolei:game section)", () => {
    for (const removed of [
      "cascade",
      "stepped on",
      "game lost",
      "all mines",
      "marker for your reasoning",
      "over-flagged",
      "satisfies the number",
      "every cell revealed",
      "number of mines adjacent",
      "reveals its unflagged neighbors",
    ]) {
      expect(SAOLEI_GUIDANCE).not.toContain(removed);
    }
  });
});

describe("saolei plugin argument validation (v1 literals)", () => {
  it("refuses both forms with the AMBIGUOUS literal", async () => {
    const { tools } = makeHarness();
    const outcome = await tools.get("saolei_operate")!.execute(
      { type: "click", x: 0, y: 0, operations: [{ type: "click", x: 0, y: 0 }] },
      fakeExec(fakeAgent(stubRuntime({}))),
    );
    expect(outcome).toEqual({ result: AMBIGUOUS_ARGS_TEXT });
  });

  it("refuses the empty call with the MISSING literal", async () => {
    const { tools } = makeHarness();
    const outcome = await tools.get("saolei_operate")!.execute({}, fakeExec(fakeAgent(stubRuntime({}))));
    expect(outcome).toEqual({ result: MISSING_ARGS_TEXT });
  });

  it("refuses a partial single form with the INCOMPLETE literal", async () => {
    const { tools } = makeHarness();
    const outcome = await tools.get("saolei_operate")!.execute(
      { type: "click", x: 1 },
      fakeExec(fakeAgent(stubRuntime({}))),
    );
    expect(outcome).toEqual({ result: INCOMPLETE_ARGS_TEXT });
  });
});

describe("saolei plugin exec forwarding", () => {
  it("forwards init/operate/remain to the caller's saoleiGame with the exec signal", async () => {
    const { tools } = makeHarness();
    const signal = new AbortController().signal;
    const runtime = stubRuntime({
      init: { isError: false, text: "new game started" },
      operate: { isError: false, text: "saolei_operate → executed 1 ops" },
      remain: { isError: false, text: "saolei_remain → computed" },
    });
    const agent = fakeAgent(runtime);

    const initExec = { ...fakeExec(agent), signal };
    await tools.get("saolei_init")!.execute({}, initExec as unknown as ToolRunContext);
    expect(runtime.init).toHaveBeenCalledWith(signal);

    const operateExec = { ...fakeExec(agent), signal };
    await tools.get("saolei_operate")!.execute(
      { type: "click", x: 1, y: 2 },
      operateExec as unknown as ToolRunContext,
    );
    expect(runtime.operate).toHaveBeenCalledWith({ type: "click", x: 1, y: 2 }, signal);

    await tools.get("saolei_remain")!.execute({}, fakeExec(agent));
    expect(runtime.remain).toHaveBeenCalledOnce();

    // The batch form normalizes through untouched.
    const batchExec = { ...fakeExec(agent), signal };
    await tools.get("saolei_operate")!.execute(
      { operations: [{ type: "flag", x: 0, y: 1 }] },
      batchExec as unknown as ToolRunContext,
    );
    expect(runtime.operate).toHaveBeenLastCalledWith(
      { operations: [{ type: "flag", x: 0, y: 1 }] },
      signal,
    );
  });

  it("maps a normal rejection text through as the result (rejections are not errors)", async () => {
    const { tools } = makeHarness();
    const runtime = stubRuntime({
      init: { isError: false, text: "rejected: no_active_game\n\ncall saolei_init first to start a game." },
    });

    const outcome = await tools.get("saolei_init")!.execute({}, fakeExec(fakeAgent(runtime)));

    expect(outcome).toEqual({
      result: "rejected: no_active_game\n\ncall saolei_init first to start a game.",
    });
  });

  it("throws on an isError outcome (model-visible failure, no fabricated success)", async () => {
    const { tools } = makeHarness();
    const runtime = stubRuntime({
      init: { isError: true, error: { message: "desktop disconnected" } },
    });

    await expect(
      tools.get("saolei_init")!.execute({}, fakeExec(fakeAgent(runtime))),
    ).rejects.toThrow("desktop disconnected");
  });

  it("fails loud when the execution carries no calling agent", async () => {
    const { tools } = makeHarness();

    await expect(
      tools.get("saolei_init")!.execute({}, fakeExec(undefined)),
    ).rejects.toThrow("no agent on the tool execution");
  });

  it("fails loud when the caller's scope has no saoleiGame service", async () => {
    const { tools } = makeHarness();

    await expect(
      tools.get("saolei_init")!.execute({}, fakeExec(fakeAgent(undefined))),
    ).rejects.toThrow("saoleiGame");
  });
});
