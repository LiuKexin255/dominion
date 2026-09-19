/**
 * Guidance-section tests: the two prompt sections the memory plugin row owns
 * (`memory:guidance` — memory-vs-wiki division of labour; `wiki:guidance` —
 * the knowledge-base cross-tool workflow) and the ownership boundaries
 * between them.
 *
 * The section texts are static English markdown; the assertions here pin the
 * boundaries from specs/068-wiki-round-pause/spec.md FR-013 and
 * specs/068-wiki-round-pause/contracts/dsh-plugins.md §3: the division
 * section carries no tool usage/parameters, the workflow section restates no
 * division, and neither uses third-party memory-framework terminology.
 */

import { describe, expect, it } from "vitest";

import {
  MEMORY_GUIDANCE,
  MEMORY_GUIDANCE_SECTION_NAME,
  MEMORY_GUIDANCE_SECTION_ORDER,
  WIKI_GUIDANCE,
  WIKI_GUIDANCE_SECTION_NAME,
  WIKI_GUIDANCE_SECTION_ORDER,
} from "./guidance.js";

const WIKI_TOOL_NAMES = ["wiki_search", "wiki_get", "wiki_write"];

/** True when the text contains a CJK character (the sections are English). */
function hasCjk(text: string): boolean {
  return /[\u4e00-\u9fff]/.test(text);
}

describe("guidance section identity", () => {
  it("declares the tool-guidance-band names and orders", () => {
    expect(MEMORY_GUIDANCE_SECTION_NAME).toBe("memory:guidance");
    expect(MEMORY_GUIDANCE_SECTION_ORDER).toBe(110);
    expect(WIKI_GUIDANCE_SECTION_NAME).toBe("wiki:guidance");
    expect(WIKI_GUIDANCE_SECTION_ORDER).toBe(120);
  });
});

describe("MEMORY_GUIDANCE: division of labour only", () => {
  it("describes the roles of the two stores and how each reaches the context", () => {
    expect(MEMORY_GUIDANCE).toContain("10 most recently updated");
    expect(MEMORY_GUIDANCE).toContain("system prompt");
    expect(MEMORY_GUIDANCE).toMatch(/on demand/i);
    expect(MEMORY_GUIDANCE).toContain("knowledge base");
  });

  it("does not describe any tool usage or parameters", () => {
    const text = MEMORY_GUIDANCE.toLowerCase();
    for (const usageToken of [
      ...WIKI_TOOL_NAMES,
      "old_text",
      "query",
      "action",
      "operations",
      "add",
      "replace",
      "remove",
      "upsert",
    ]) {
      expect(text).not.toContain(usageToken);
    }
  });
});

describe("WIKI_GUIDANCE: cross-tool workflow only", () => {
  it("describes the discover -> retrieve -> persist workflow of the three tools", () => {
    for (const toolName of WIKI_TOOL_NAMES) {
      expect(WIKI_GUIDANCE).toContain(toolName);
    }
    expect(WIKI_GUIDANCE).toMatch(/upsert/i);
    expect(WIKI_GUIDANCE).toContain("all-or-nothing");
  });

  it("does not restate the memory-vs-wiki division", () => {
    const text = WIKI_GUIDANCE.toLowerCase();
    for (const divisionToken of [
      "memory",
      "system prompt",
      "snapshot",
      "on demand",
      "recency",
      "always visible",
    ]) {
      expect(text).not.toContain(divisionToken);
    }
  });
});

describe("guidance sections: shared constraints (FR-013)", () => {
  it("uses no third-party memory-framework terminology", () => {
    for (const text of [MEMORY_GUIDANCE, WIKI_GUIDANCE]) {
      const lower = text.toLowerCase();
      for (const term of [
        "memgpt",
        "letta",
        "memfs",
        "archival",
        "core memory",
        "recall",
      ]) {
        expect(lower).not.toContain(term);
      }
    }
  });

  it("is English text rendered as a markdown section", () => {
    for (const text of [MEMORY_GUIDANCE, WIKI_GUIDANCE]) {
      expect(hasCjk(text)).toBe(false);
      expect(text.startsWith("## ")).toBe(true);
    }
  });
});
