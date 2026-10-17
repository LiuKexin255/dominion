/**
 * Library export-surface tests: the package is a pure library — no cordis
 * plugin form (`name`/`inject`/`apply`) — and exposes the functional-unit and
 * composition factories, the primitives and the tool/section constants
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1/§2).
 */

import { describe, expect, it } from "vitest";

import * as library from "./index.js";

describe("@dominion/common-js-memory export surface", () => {
  it("has no cordis plugin form", () => {
    expect("name" in library).toBe(false);
    expect("inject" in library).toBe(false);
    expect("apply" in library).toBe(false);
  });

  it("exposes the functional-unit and composition factories", () => {
    expect(typeof library.buildMemoryUnit).toBe("function");
    expect(typeof library.buildWikiUnit).toBe("function");
    expect(typeof library.buildMemoryWikiComposition).toBe("function");
  });

  it("keeps no combined-surface factory exports (final state, constitution VII)", () => {
    expect("buildMemoryCapabilities" in library).toBe(false);
    expect("createMemoryStore" in library).toBe(false);
  });

  it("exposes the primitives", () => {
    expect(typeof library.createMemoryFace).toBe("function");
    expect(typeof library.createWikiFace).toBe("function");
    expect(typeof library.renderMemorySnapshot).toBe("function");
    expect(typeof library.memorySnapshotSection).toBe("function");
  });

  it("exposes the tool definitions and section identity", () => {
    expect(library.MEMORY_TOOL_NAME).toBe("memory");
    expect(library.WIKI_SEARCH_TOOL_NAME).toBe("wiki_search");
    expect(library.WIKI_GET_TOOL_NAME).toBe("wiki_get");
    expect(library.WIKI_WRITE_TOOL_NAME).toBe("wiki_write");
    expect(library.MEMORY_GUIDANCE_SECTION_NAME).toBe("memory:guidance");
    expect(library.WIKI_GUIDANCE_SECTION_NAME).toBe("wiki:guidance");
    expect(library.MEMORY_SNAPSHOT_SECTION_NAME).toBe("memory:snapshot");
  });
});
