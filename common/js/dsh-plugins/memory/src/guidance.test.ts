/**
 * Guidance-section tests: the two prompt sections the memory plugin row owns
 * (`memory:guidance` — memory-vs-wiki division of labour; `wiki:guidance` —
 * the wiki cross-tool workflow) and the ownership boundaries
 * between them.
 *
 * The section texts are static English markdown; the assertions here pin the
 * boundaries from specs/068-wiki-round-pause/spec.md FR-013 and
 * specs/068-wiki-round-pause/contracts/dsh-plugins.md §3: the division
 * section carries no tool usage/parameters, the workflow section restates no
 * division, and neither uses third-party memory-framework terminology. The
 * division section additionally carries the memory entry size limit and the
 * wiki's cross-session persistence
 * (specs/069-wiki-template-web/contracts/dsh-plugins.md §5).
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
    expect(MEMORY_GUIDANCE).toContain("**Wiki** is a large knowledge base");
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

  it("states the memory entry size limit and routes longer knowledge to the wiki", () => {
    expect(MEMORY_GUIDANCE).toContain("1000 characters");
    expect(MEMORY_GUIDANCE).toMatch(/longer knowledge/i);
  });

  it("states that the wiki persists across sessions", () => {
    expect(MEMORY_GUIDANCE).toMatch(/persists across sessions/i);
  });
});

describe("WIKI_GUIDANCE: cross-tool workflow only", () => {
  it("describes the discover -> retrieve -> persist workflow of the three tools", () => {
    for (const toolName of WIKI_TOOL_NAMES) {
      expect(WIKI_GUIDANCE).toContain(toolName);
    }
    expect(WIKI_GUIDANCE).toMatch(/upsert/i);
    const lower = WIKI_GUIDANCE.toLowerCase();
    expect(lower).not.toContain("all-or-nothing");
    expect(lower).not.toContain("atomic");
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

  it("carries no memory entry size limit (the division section owns it)", () => {
    const text = WIKI_GUIDANCE.toLowerCase();
    expect(text).not.toContain("1000");
    expect(text).not.toContain("character");
    expect(text).not.toContain("limit");
  });
});

describe("guidance sections: shared constraints (specs/068-wiki-round-pause/spec.md FR-013)", () => {
  it("names the store wiki: `knowledge base` is only the one-time description (specs/068-wiki-round-pause/contracts/dsh-plugins.md §3)", () => {
    // The division section permits exactly one `knowledge base` — wiki's
    // description; any additional occurrence (whatever its article) would be a
    // referential use. The workflow section forbids the term entirely.
    expect((MEMORY_GUIDANCE.match(/knowledge base/gi) ?? []).length).toBe(1);
    expect(MEMORY_GUIDANCE).toMatch(/large knowledge base/);
    expect((WIKI_GUIDANCE.match(/knowledge base/gi) ?? []).length).toBe(0);
    // The hyphenated form is never allowed.
    for (const text of [MEMORY_GUIDANCE, WIKI_GUIDANCE]) {
      expect(text.toLowerCase()).not.toContain("knowledge-base");
    }
  });

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
