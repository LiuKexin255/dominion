/**
 * dsh-saolei plugin tests: the game FOUNDATION face only
 * (specs/075-saolei-game-foundation/contracts/saolei-foundation.md §6).
 *
 * The plugin owns the saolei game domain — the pure `createGameRuntime`
 * factory plus the frozen `saolei:game` section spec. It provides the
 * foundation face through `ctx.provide("saolei", ...)` and registers NOTHING
 * on the composition scope (no `ctx.tools.register` / `ctx.systemPrompt.
 * section` side effect): the orchestration registers the section per member,
 * and the model-facing tools/guidance live in `@dominion/dsh-saolei-tools`.
 *
 * Pattern (style/javascript.md Mock convention): `apply` runs against a real
 * cordis Context whose `tools`/`systemPrompt` services are `vi.fn()`-based
 * doubles capturing registrations (so an accidental global registration is
 * observable) — no module interception.
 */

import { Context } from "@deepseek-ai/cordis";
import { describe, expect, it, vi } from "vitest";

import * as plugin from "./index.js";
import {
  apply,
  inject,
  SAOLEI_FOUNDATION_SERVICE,
  SAOLEI_GAME_RULES,
  SAOLEI_GAME_SECTION,
} from "./index.js";
import type { SaoleiFoundation } from "./index.js";

interface Harness {
  readonly foundation: SaoleiFoundation;
  readonly toolsRegister: ReturnType<typeof vi.fn>;
  readonly sectionRegister: ReturnType<typeof vi.fn>;
}

/** Run `apply` against a harness context and capture the foundation face. */
function makeHarness(): Harness {
  const ctx = new Context();
  // Real registration seams (vi.fn doubles): an accidental global registration
  // by apply would be captured here instead of silently tolerated.
  const toolsRegister = vi.fn();
  const sectionRegister = vi.fn();
  ctx.provide("tools", { register: toolsRegister });
  ctx.provide("systemPrompt", { section: sectionRegister });
  apply(ctx);
  const foundation = (ctx as unknown as Record<string, unknown>)[
    SAOLEI_FOUNDATION_SERVICE
  ] as SaoleiFoundation;
  return { foundation, toolsRegister, sectionRegister };
}

/** Whether a module export is a tool definition value (structural check). */
function isToolDefinition(value: unknown): boolean {
  return (
    typeof value === "object" &&
    value !== null &&
    "execute" in value &&
    "parameters" in value &&
    "name" in value &&
    typeof (value as { name: unknown }).name === "string" &&
    (value as { name: string }).name.startsWith("saolei_")
  );
}

describe("saolei game foundation face", () => {
  it("provides the foundation face with the factory and the frozen game section", () => {
    const { foundation } = makeHarness();

    expect(typeof foundation.createGameRuntime).toBe("function");
    expect(foundation.gameSection).toEqual(SAOLEI_GAME_SECTION);
    expect(foundation.gameSection.name).toBe("saolei:game");
    expect(foundation.gameSection.order).toBe(50);
    expect(foundation.gameSection.text).toBe(SAOLEI_GAME_RULES);
  });

  it("opens the game section with the gameplay title", () => {
    const { foundation } = makeHarness();

    expect(foundation.gameSection.text.split("\n")[0]).toBe(
      "## 扫雷玩法与可用操作",
    );
  });

  it("exposes the pure factory: the static signature takes only the two domain inputs", () => {
    const { foundation } = makeHarness();

    // createGameRuntime(sessionName, desktopBridge) — no agent/ctx/scope
    // arguments, so the composition-level face cannot register state
    // (contracts/saolei-foundation.md §2/§6).
    expect(foundation.createGameRuntime.length).toBe(2);
  });

  it("is a pure provide: the foundation face registers nothing globally", () => {
    const { toolsRegister, sectionRegister } = makeHarness();

    // The gameplay section is registered by the orchestration per member, not
    // by this composition-level apply (contracts/saolei-foundation.md §5).
    expect(toolsRegister).not.toHaveBeenCalled();
    expect(sectionRegister).not.toHaveBeenCalled();
  });

  it("declares itself as a pure provider", () => {
    expect(inject).toEqual([]);
  });

  it("holds no tool asset (the model-facing tools live in dsh-saolei-tools)", () => {
    for (const [name, value] of Object.entries(plugin)) {
      expect(isToolDefinition(value), `export ${name} is a tool definition`).toBe(
        false,
      );
    }
    // The removed saoleiTools surface must not linger on the export face.
    const exports = plugin as unknown as Record<string, unknown>;
    expect(exports.SAOLEI_TOOLS_SERVICE).toBeUndefined();
    expect(exports.SaoleiToolsUnit).toBeUndefined();
    expect(exports.buildSaoleiToolsUnit).toBeUndefined();
    expect(exports.SAOLEI_GUIDANCE).toBeUndefined();
  });
});

describe("SAOLEI_GAME_RULES content (prompt-sections.md §1)", () => {
  it("states the authoritative classic game rules", () => {
    for (const rule of [
      "揭示全部非雷格",
      "不踩雷",
      "1–8",
      "八邻格",
      "级联展开",
      "标旗",
      "chord",
      "左右同击",
      "踩中雷",
      "获胜",
      "剩余雷数计数",
      "顶部计数器",
      "剩余未标记雷数",
      "不是旗子数量",
      "总雷数",
      "已标旗数",
      "可以为负",
    ]) {
      expect(SAOLEI_GAME_RULES).toContain(rule);
    }
  });

  it("declares the operation set as the saolei tools expose it", () => {
    for (const op of ["saolei_init", "saolei_operate", "saolei_remain"]) {
      expect(SAOLEI_GAME_RULES).toContain(op);
    }
    expect(SAOLEI_GAME_RULES).toContain("click（揭示）");
    expect(SAOLEI_GAME_RULES).toContain("flag（标旗/取消标旗）");
    expect(SAOLEI_GAME_RULES).toContain("chord（同击）");
  });

  it("stays out of the tool-guidance domain (call shapes and result formats)", () => {
    for (const guidanceOnly of [
      "type/x/y",
      "operations: [{",
      "game status:",
      "outcome",
      "col0",
      "row0",
      "no_active_game",
    ]) {
      expect(SAOLEI_GAME_RULES).not.toContain(guidanceOnly);
    }
  });

  it("stays out of the persona and team domains", () => {
    for (const foreign of ["你是扫雷", "## 团队", "<player-message>"]) {
      expect(SAOLEI_GAME_RULES).not.toContain(foreign);
    }
  });
});
