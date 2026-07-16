/**
 * build-tools.test.ts — Tests for the buildTools name→factory registry (T014).
 *
 * Unlike llm-tools.test.ts (which spies on createAgent via vi.mock("langchain")
 * — a mock that does not reliably intercept the pre-compiled :lib's langchain
 * import under Bazel), this file exercises buildTools DIRECTLY. buildTools only
 * needs the real `tool()` factory (proven to work under Bazel by the saolei
 * tool tests), so no module mock is required and the results are identical
 * whether vitest transpiles source or runs the compiled :lib.
 *
 * Covers: toolNames resolution, mcpNames=["saolei"] → the five saolei tools
 * bound to the session-scoped SaoleiMcp + bridge (plan.md Changes verdict),
 * the "saolei declared but no instance" warning path, and unknown-name
 * skipping (FR-035).
 */

import { describe, expect, it } from "vitest";

import { buildTools } from "./llm";
import { SaoleiMcp } from "./mcp/saolei/saolei-mcp";
import { OperationBridge } from "./operation-bridge";

function toolNames(tools: ReturnType<typeof buildTools>): string[] {
  return tools.map((t) => t.name);
}

describe("buildTools registry", () => {
  it("resolves individual toolNames (mouse_move, mouse_click)", () => {
    const tools = buildTools(
      ["mouse_move", "mouse_click"],
      [],
      new OperationBridge(),
      null,
    );
    expect(toolNames(tools)).toEqual(["mouse_move", "mouse_click"]);
  });

  it("resolves mcpNames=['saolei'] into the five saolei tools bound to the MCP + bridge", () => {
    const tools = buildTools(
      ["mouse_move"],
      ["saolei"],
      new OperationBridge(),
      new SaoleiMcp(),
    );
    // mouse_move (1) + the five saolei tools = 6 total.
    expect(tools).toHaveLength(6);
    expect(toolNames(tools)).toEqual(
      expect.arrayContaining([
        "mouse_move",
        "saolei_init",
        "saolei_click",
        "saolei_flag",
        "saolei_double_click",
        "saolei_update",
      ]),
    );
  });

  it("omits saolei tools when mcpNames does not declare saolei", () => {
    const tools = buildTools(["mouse_move"], [], new OperationBridge(), null);
    expect(toolNames(tools)).toEqual(["mouse_move"]);
  });

  it("omits saolei tools when saolei is declared but no instance is provided", () => {
    // FR-035: a declared MCP with no session instance is skipped (warned) rather
    // than crashing — buildTools returns no saolei tools.
    const tools = buildTools([], ["saolei"], new OperationBridge(), null);
    expect(tools).toHaveLength(0);
  });

  it("skips unknown tool names and unknown mcp names (FR-035)", () => {
    const tools = buildTools(
      ["bogus_tool"],
      ["bogus_mcp"],
      new OperationBridge(),
      null,
    );
    expect(tools).toHaveLength(0);
  });

  it("returns no tools for empty toolNames and mcpNames", () => {
    const tools = buildTools([], [], new OperationBridge(), null);
    expect(tools).toEqual([]);
  });
});
