/**
 * saolei tools plugin tests
 * (`@dominion/dsh-saolei-tools`, specs/075-saolei-game-foundation/contracts/
 * saolei-tools-plugin.md). The plugin provides one face — the saoleiTools
 * capability unit (`ctx.provide("saoleiTools", ...)`,
 * specs/074-dsh-grpc-runtime/spec.md FR-020;
 * specs/074-dsh-grpc-runtime/contracts/team-assembly.md §3.1/§6): the three
 * tool definitions and the narrowed
 * `[saolei:guidance]` section list, provided together and registering NOTHING
 * on the composition scope (no `ctx.tools.register` / `ctx.systemPrompt.section`
 * side effect). The unit content is the frozen saolei contract: tool
 * descriptions/schemas/outputs and the section name/order/text are asserted
 * verbatim. The gameplay section `saolei:game` is owned by the game foundation
 * and registered by the orchestration — the unit must NOT carry it.
 *
 * The behavior cases stay: dual-form `saolei_operate` validation with the
 * verbatim literals, the exec-time resolution of the agent-scoped
 * `saoleiGame` handle through `exec.agent.ctx`, the ToolOutcome.isError →
 * throw mapping, the ToolOutcome.concludesTurn → `exec.concludeTurn()`
 * mapping (specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md
 * §2), and the fail-loud paths (no `exec.agent`; handle absent from the
 * caller's scope). Ownership boundary of the sections follows
 * specs/060-agent-v2-team-optimize/contracts/prompt-sections.md §2.
 *
 * Pattern (style/javascript.md Mock convention): `apply` runs against a real
 * cordis Context whose `tools`/`systemPrompt` services are `vi.fn()`-based
 * doubles capturing registrations (so an accidental global registration is
 * observable); tools execute directly against the unit's captured definitions
 * with a fake ToolRunContext — no module interception.
 */

import { Context } from "@deepseek-ai/cordis";
import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import type { ToolRunContext } from "@deepseek-ai/dsh-tools";
import { describe, expect, it, vi } from "vitest";

import {
  AMBIGUOUS_ARGS_TEXT,
  apply,
  INCOMPLETE_ARGS_TEXT,
  inject,
  MISSING_ARGS_TEXT,
  SAOLEI_GUIDANCE,
  SAOLEI_TOOLS_SERVICE,
} from "./index.js";
import type { SaoleiToolsUnit } from "./index.js";
import type { SaoleiGame } from "@dominion/dsh-saolei";

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
    concludeTurn: vi.fn(),
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

interface Harness {
  readonly ctx: Context;
  readonly unit: SaoleiToolsUnit;
  readonly tools: Map<string, ToolDefinition>;
  readonly toolsRegister: ReturnType<typeof vi.fn>;
  readonly sectionRegister: ReturnType<typeof vi.fn>;
}

/** Run `apply` against a harness context and capture the provided unit. */
function makeHarness(): Harness {
  const ctx = new Context();
  // Real registration seams (vi.fn doubles): an accidental global registration
  // by apply would be captured here instead of silently tolerated.
  const toolsRegister = vi.fn();
  const sectionRegister = vi.fn();
  ctx.provide("tools", { register: toolsRegister });
  ctx.provide("systemPrompt", { section: sectionRegister });
  apply(ctx);
  const provided = ctx as unknown as Record<string, unknown>;
  const unit = provided[SAOLEI_TOOLS_SERVICE] as SaoleiToolsUnit;
  const tools = new Map(
    unit.tools.map((definition) => [definition.name, definition]),
  );
  return { ctx, unit, tools, toolsRegister, sectionRegister };
}

describe("saolei capability unit provision", () => {
  it("provides the unit and registers nothing on the composition scope", () => {
    const { unit, toolsRegister, sectionRegister } = makeHarness();

    expect([...unit.tools].map((definition) => definition.name).sort()).toEqual(
      ["saolei_init", "saolei_operate", "saolei_remain"],
    );
    expect(unit.sections).toEqual([
      { name: "saolei:guidance", order: 100, text: SAOLEI_GUIDANCE },
    ]);
    // Composition-level global registration is exactly what
    // specs/074-dsh-grpc-runtime/spec.md FR-020 forbids
    // (it would leak the tools/sections to every member).
    expect(toolsRegister).not.toHaveBeenCalled();
    expect(sectionRegister).not.toHaveBeenCalled();
  });

  it("narrows the unit sections to the guidance section only", () => {
    const { unit } = makeHarness();

    // `saolei:game` belongs to the game foundation (@dominion/dsh-saolei) and
    // is registered by the orchestration for both roles; the unit carries the
    // tool-usage guidance only
    // (specs/075-saolei-game-foundation/contracts/saolei-tools-plugin.md §2).
    expect(unit.sections.map((section) => section.name)).toEqual(["saolei:guidance"]);
  });

  it("declares itself as a pure provider", () => {
    expect(inject).toEqual([]);
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

  it("declares the canonical {result: string} output with a text render", () => {
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

  it("states the remain semantics unambiguously in description and guidance (064 contract §2/§4)", () => {
    const { tools } = makeHarness();
    const description = tools.get("saolei_remain")!.description;

    for (const phrase of ["mines still unmarked", "NOT the count of flags"]) {
      expect(description).toContain(phrase);
      expect(SAOLEI_GUIDANCE).toContain(phrase);
    }
    // The existing boundaries stay stated with the new primary meaning.
    expect(description).toContain("no_active_game");
    expect(description).toContain("terminal board");
    expect(SAOLEI_GUIDANCE).toContain("Not blocked by a terminal board");
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

describe("saolei plugin turn conclusion mapping (062 contract §2)", () => {
  it("calls exec.concludeTurn exactly once for a marked successful outcome", async () => {
    const { tools } = makeHarness();
    const runtime = stubRuntime({
      operate: {
        isError: false,
        text: "saolei_operate → stopped at click(1,1) (won)",
        concludesTurn: true,
      },
    });
    const exec = fakeExec(fakeAgent(runtime));

    const outcome = await tools.get("saolei_operate")!.execute(
      { type: "click", x: 1, y: 1 },
      exec,
    );

    expect(outcome).toEqual({ result: "saolei_operate → stopped at click(1,1) (won)" });
    expect(exec.concludeTurn).toHaveBeenCalledOnce();
  });

  it("does not call concludeTurn for an unmarked outcome", async () => {
    const { tools } = makeHarness();
    const runtime = stubRuntime({
      init: { isError: false, text: "new game started\ngame status: playing" },
    });
    const exec = fakeExec(fakeAgent(runtime));

    const outcome = await tools.get("saolei_init")!.execute({}, exec);

    expect(outcome).toEqual({ result: "new game started\ngame status: playing" });
    expect(exec.concludeTurn).not.toHaveBeenCalled();
  });

  it("throws on an isError outcome without calling concludeTurn", async () => {
    const { tools } = makeHarness();
    const runtime = stubRuntime({
      operate: { isError: true, error: { message: "desktop disconnected" } },
    });
    const exec = fakeExec(fakeAgent(runtime));

    await expect(
      tools.get("saolei_operate")!.execute({ type: "click", x: 0, y: 0 }, exec),
    ).rejects.toThrow("desktop disconnected");
    expect(exec.concludeTurn).not.toHaveBeenCalled();
  });

  it("does not call concludeTurn when the argument combination is refused before the runtime", async () => {
    const { tools } = makeHarness();
    const runtime = stubRuntime({});
    const exec = fakeExec(fakeAgent(runtime));

    const outcome = await tools.get("saolei_operate")!.execute(
      { type: "click", x: 0, y: 0, operations: [{ type: "click", x: 0, y: 0 }] },
      exec,
    );

    expect(outcome).toEqual({ result: AMBIGUOUS_ARGS_TEXT });
    expect(runtime.operate).not.toHaveBeenCalled();
    expect(exec.concludeTurn).not.toHaveBeenCalled();
  });
});
