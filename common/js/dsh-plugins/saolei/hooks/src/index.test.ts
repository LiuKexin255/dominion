/**
 * Saolei hook engine package tests
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §1/§5/§6;
 * specs/078-saolei-js-hooks/spec.md FR-005/FR-006/FR-015/FR-016, US5, SC-005).
 *
 * Two groups: the plugin identity and resource caps the composition pins, and
 * the `saoleiHooks` provide face — the per-team factory whose unit shares ONE
 * registry between the planner's tool face and the player's board-change
 * handler, carries no prompt section, and keeps its dependency edges on the
 * game foundation alone (the static boundary check of SC-005).
 *
 * Pattern (style/javascript.md §Mock 约定): `apply` runs against a real cordis
 * Context (the saolei-tools package's provision-test form) whose `tools` /
 * `systemPrompt` services are `vi.fn()` doubles, the announcement seam is a
 * `vi.fn()`, and the dependency check reads this package's own sources and
 * manifest from disk — no module interception.
 */

import { Context } from "@deepseek-ai/cordis";
import type { ToolDefinition, ToolRunContext } from "@deepseek-ai/dsh-tools";
import type { GameLogEntry } from "@dominion/dsh-saolei";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";

import { apply, inject, name, SAOLEI_HOOKS_SERVICE } from "./index.js";
import type { SaoleiHooksMount, SaoleiHooksUnit } from "./index.js";
import {
  EXECUTION_TIMEOUT_MS,
  MAX_HOOKS,
  MAX_OUTPUT_CHARS,
  MAX_SCRIPT_CHARS,
} from "./limits.js";
import type { GameState } from "./sandbox.js";

describe("saolei-hooks plugin identity", () => {
  it("declares the plugin name the composition manifest references", () => {
    expect(name).toBe("saolei-hooks");
  });

  it("declares itself as a pure provider", () => {
    expect(inject).toEqual([]);
  });

  it("names the provide face the orchestration reads", () => {
    expect(SAOLEI_HOOKS_SERVICE).toBe("saoleiHooks");
  });
});

describe("saolei-hooks resource caps (data-model.md §2.3)", () => {
  it("pins the engine-wide limits from research.md D4", () => {
    expect(MAX_HOOKS).toBe(8);
    expect(MAX_SCRIPT_CHARS).toBe(16384);
    expect(MAX_OUTPUT_CHARS).toBe(4096);
    expect(EXECUTION_TIMEOUT_MS).toBe(500);
  });
});

/** A minimal board; the unit tests forward it as the entry state only. */
function fixtureBoard(): GameState {
  return {
    width: 1,
    height: 1,
    grid: [["INITIAL"]],
    mineCounter: { decoded: true, value: 1 },
  };
}

/** A board-change entry as the game runtime reports it. */
function fixtureEntry(): GameLogEntry {
  return {
    tool: "saolei_operate",
    operations: [],
    state: fixtureBoard(),
    status: "playing",
  };
}

/** A fake `ToolRunContext` for the unit's tool executions (the tool body reads
 * no execution field; common/js/dsh-plugins/saolei/hooks/src/tool.test.ts
 * precedent). */
function fakeExec(): ToolRunContext {
  return { callId: "call-1" } as unknown as ToolRunContext;
}

/** Read one field of a `{result: string}` outcome. */
function resultText(outcome: unknown): string {
  return (outcome as { result: string }).result;
}

/** Run `apply` against a real context and read the provided mount back. */
function providedMount(): SaoleiHooksMount {
  const ctx = new Context();
  apply(ctx);
  return (ctx as unknown as Record<string, unknown>)[
    SAOLEI_HOOKS_SERVICE
  ] as SaoleiHooksMount;
}

/** Register one hook through the unit's own tool face. */
async function setHook(
  unit: SaoleiHooksUnit,
  name: string,
  script: string,
): Promise<string> {
  const tool = unit.tools[0] as ToolDefinition;
  return resultText(await tool.execute({ action: "set", name, script }, fakeExec()));
}

describe("saolei-hooks mount face (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §1/§5)", () => {
  it("provides the mount on apply and registers nothing on the composition scope", () => {
    const ctx = new Context();
    // Real registration seams (vi.fn doubles): an accidental composition-level
    // registration would be captured here instead of silently tolerated
    // (common/js/dsh-plugins/saolei/tools/src/index.test.ts form).
    const toolsRegister = vi.fn();
    const sectionRegister = vi.fn();
    ctx.provide("tools", { register: toolsRegister });
    ctx.provide("systemPrompt", { section: sectionRegister });

    apply(ctx);

    const mount = (ctx as unknown as Record<string, unknown>)[
      SAOLEI_HOOKS_SERVICE
    ] as SaoleiHooksMount;
    expect(typeof mount.buildHooksUnit).toBe("function");
    expect(toolsRegister).not.toHaveBeenCalled();
    expect(sectionRegister).not.toHaveBeenCalled();
  });

  it("shares one registry between the unit's tool face and its board-change handler", async () => {
    const announce = vi.fn();
    const unit = providedMount().buildHooksUnit({ announce });

    // The planner writes through the tool face; the handler reads the same
    // store per event (specs/078-saolei-js-hooks/data-model.md §3.1).
    await expect(setHook(unit, "safe-cells", 'return "two open cells";')).resolves.toContain(
      'registered hook "safe-cells"',
    );
    const listed = resultText(
      await (unit.tools[0] as ToolDefinition).execute({ action: "list" }, fakeExec()),
    );
    expect(listed).toContain("safe-cells");

    await expect(
      setHook(unit, "counts", "return String(board.width);"),
    ).resolves.toContain("registered hook");

    unit.onBoardChange(fixtureEntry());

    expect(announce.mock.calls.map((call) => call[0])).toEqual([
      "[hook safe-cells] two open cells",
      "[hook counts] 1",
    ]);
  });

  it("returns an independent registry per buildHooksUnit call (team isolation)", async () => {
    const mount = providedMount();
    const firstAnnounce = vi.fn();
    const secondAnnounce = vi.fn();
    const first = mount.buildHooksUnit({ announce: firstAnnounce });
    const second = mount.buildHooksUnit({ announce: secondAnnounce });

    await setHook(first, "leaked", 'return "x";');

    // The second unit is a fresh team materialization: nothing the first unit
    // registered is visible to it (specs/078-saolei-js-hooks/spec.md FR-005).
    const listed = resultText(
      await (second.tools[0] as ToolDefinition).execute({ action: "list" }, fakeExec()),
    );
    expect(listed).toBe("saolei_hook → no hooks registered");
    second.onBoardChange(fixtureEntry());
    expect(secondAnnounce).not.toHaveBeenCalled();
  });

  it("exposes exactly the saolei_hook tool and an empty section list", () => {
    const unit = providedMount().buildHooksUnit({ announce: vi.fn() });

    expect(unit.tools).toHaveLength(1);
    expect(unit.tools[0]?.name).toBe("saolei_hook");
    // Zero prompt section (specs/078-saolei-js-hooks/spec.md FR-006): the whole
    // guidance is the tool description.
    expect(unit.sections).toEqual([]);
  });
});

/**
 * The production sources of this package. The dependency scan skips the test
 * files: their fixtures legitimately name the forbidden packages.
 */
function productionSources(): Array<{ readonly file: string; readonly text: string }> {
  const srcDir = import.meta.dirname;
  return readdirSync(srcDir)
    .filter((entry) => entry.endsWith(".ts") && !entry.endsWith(".test.ts"))
    .map((entry) => ({
      file: entry,
      text: readFileSync(join(srcDir, entry), "utf8"),
    }));
}

/** One import/export specifier per statement (`from "…"`, `import("…")`,
 * side-effect `import "…"`). */
const SPECIFIER_PATTERN = /(?:from|import)\s*\(?\s*["']([^"']+)["']/g;

describe("saolei-hooks dependency edges (specs/078-saolei-js-hooks/spec.md FR-016, SC-005)", () => {
  it("imports no saolei-tools / saolei-loop / team / announcer module", () => {
    // The hook engine may depend on the game foundation and the framework
    // faces only; both directions of the saolei-tools edge are forbidden
    // (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §1).
    const forbidden = [
      "@dominion/dsh-saolei-tools",
      "@dominion/dsh-saolei-loop",
      "@dominion/dsh-team",
      "announcer",
    ];
    const violations: string[] = [];
    for (const { file, text } of productionSources()) {
      for (const match of text.matchAll(SPECIFIER_PATTERN)) {
        const specifier = match[1] as string;
        if (forbidden.some((edge) => specifier.includes(edge))) {
          violations.push(`${file} imports ${specifier}`);
        }
      }
    }

    expect(violations).toEqual([]);
  });

  it("declares only the game foundation dependency and the framework peers", () => {
    const manifest = JSON.parse(
      readFileSync(join(import.meta.dirname, "..", "package.json"), "utf8"),
    ) as {
      dependencies?: Record<string, string>;
      peerDependencies?: Record<string, string>;
    };

    expect(Object.keys(manifest.dependencies ?? {})).toEqual([
      "@dominion/dsh-saolei",
    ]);
    expect(Object.keys(manifest.peerDependencies ?? {}).sort()).toEqual([
      "@deepseek-ai/cordis",
      "@deepseek-ai/dsh-system-prompt",
      "@deepseek-ai/dsh-tools",
    ]);
  });
});
