/**
 * types.test.ts — Unit tests for the `isStandalone` helper.
 *
 * Pure value-in/value-out coverage of the four contract cases at
 * specs/020-agent-resources-layout/contracts/tool-definition.md lines 99–102
 * plus the additional "key absent" case (lines 92–94 of the helper contract).
 * No `vi.mock` — the helper is a pure reader over `tool.extras`, so the mock
 * tool is a plain object literal cast to `StructuredToolInterface` via the
 * same `as unknown as StructuredToolInterface` pattern used at
 * `projects/game/agent/src/mouse-tool.test.ts:33-40`.
 */

import type { StructuredToolInterface } from "@langchain/core/tools";

import { describe, expect, it } from "vitest";

import { isStandalone, type StandaloneExtras } from "./types";

/**
 * Build a minimal mock tool carrying only the `extras` field. The helper
 * under test reads nothing else off the tool, so this is sufficient.
 */
function makeTool(extras?: StandaloneExtras | Record<string, unknown>): StructuredToolInterface {
  return { extras } as unknown as StructuredToolInterface;
}

describe("isStandalone", () => {
  it("returns true when extras.standalone === true", () => {
    const tool = makeTool({ standalone: true });
    expect(isStandalone(tool)).toBe(true);
  });

  it("returns false when extras.standalone === false", () => {
    const tool = makeTool({ standalone: false });
    expect(isStandalone(tool)).toBe(false);
  });

  it("returns true when extras is undefined (default-when-omitted)", () => {
    const tool = makeTool(undefined);
    expect(isStandalone(tool)).toBe(true);
  });

  it("returns true when extras is present but the standalone key is absent", () => {
    const tool = makeTool({ other: "value" });
    expect(isStandalone(tool)).toBe(true);
  });
});
