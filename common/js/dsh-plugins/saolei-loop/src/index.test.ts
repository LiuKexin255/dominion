/**
 * saolei-loop plugin entry tests (prompt ownership contract
 * specs/060-agent-v2-team-optimize/contracts/prompt-sections.md §1): `apply`
 * registers the host-scope `saolei:game` section (order 50, the game-rules
 * text), and {@link SAOLEI_GAME_RULES} carries the authoritative classic
 * Minesweeper rules plus the operation set the saolei tools expose — and
 * nothing from the tool-guidance / persona / team domains.
 *
 * Pattern (style/javascript.md Mock convention): `apply` runs against a real
 * cordis Context whose `systemPrompt` service is a `vi.fn()` double capturing
 * registrations; no module interception.
 */

import { Context } from "@deepseek-ai/cordis";
import { describe, expect, it, vi } from "vitest";

import { apply, SAOLEI_GAME_RULES } from "./index.js";

/** Run `apply` against a harness context and capture section registrations. */
function makeHarness(): {
  sections: Array<{ name: string; order: number; text: string }>;
} {
  const ctx = new Context();
  const sections: Array<{ name: string; order: number; text: string }> = [];
  ctx.provide("systemPrompt", {
    section: vi.fn((section: { name: string; order: number; text: string }) => {
      sections.push(section);
      return () => {};
    }),
  });
  apply(ctx);
  return { sections };
}

describe("saolei-loop plugin registration", () => {
  it("registers exactly the saolei:game section with the exported rules text", () => {
    const { sections } = makeHarness();

    expect(sections).toEqual([{ name: "saolei:game", order: 50, text: SAOLEI_GAME_RULES }]);
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
