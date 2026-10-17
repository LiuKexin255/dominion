/**
 * Saolei hook engine package smoke tests
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md). The plugin
 * identity is the frozen face the composition manifest references, and the
 * resource caps are the engine-wide bounds pinned by
 * specs/078-saolei-js-hooks/data-model.md §2.3 / research.md D4.
 */

import { describe, expect, it } from "vitest";

import { inject, name, SAOLEI_HOOKS_SERVICE } from "./index.js";
import {
  EXECUTION_TIMEOUT_MS,
  MAX_HOOKS,
  MAX_OUTPUT_CHARS,
  MAX_SCRIPT_CHARS,
} from "./limits.js";

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
