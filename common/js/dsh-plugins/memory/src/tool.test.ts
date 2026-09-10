/**
 * Memory tool tests — the `memory` single tool's argument schema (v1
 * `memory-mcp.ts:427-471`), execution-time service binding, and the
 * text-result failure semantics.
 *
 * Pattern (style/javascript.md Mock convention): the host service and the
 * tool execution context are injected doubles (`vi.fn()`); no module
 * interception. Contract: specs/059-agent-v2-team-mode/contracts/dsh-plugins.md
 * §3 item 1.
 */

import { validateArgs } from "@deepseek-ai/dsh-tools";
import type { ToolRunContext } from "@deepseek-ai/dsh-tools";
import { describe, expect, it, vi } from "vitest";

import type { PlannerMemoryService } from "./service.js";
import {
  MEMORY_TOOL_DESCRIPTION,
  MEMORY_TOOL_NAME,
  MEMORY_TOOL_PARAMETERS,
  createMemoryToolDefinition,
  executeMemoryTool,
} from "./tool.js";

/** A tool execution carrying only the calling agent (the service is bound at
 * registration, NOT resolved through agent.ctx — see createMemoryToolDefinition). */
function makeExec(agentId = "planner"): ToolRunContext {
  return { agent: { id: agentId, ctx: {} } } as unknown as ToolRunContext;
}

/** The bound service double. */
function makeService(applyCall: (agent: unknown, args: unknown) => Promise<string>) {
  return { applyCall } as unknown as Pick<PlannerMemoryService, "applyCall">;
}

describe("MEMORY_TOOL_PARAMETERS (v1 schema parity)", () => {
  it("declares exactly action/content/old_text/operations (no read, no id)", () => {
    expect(Object.keys(MEMORY_TOOL_PARAMETERS).sort()).toEqual([
      "action",
      "content",
      "old_text",
      "operations",
    ]);
    expect(Object.keys(MEMORY_TOOL_PARAMETERS)).not.toContain("memory_id");
    expect(Object.keys(MEMORY_TOOL_PARAMETERS)).not.toContain("read");
  });

  it("restricts action and batch item action to add/replace/remove", () => {
    expect(MEMORY_TOOL_PARAMETERS.action.enum).toEqual([
      "add",
      "replace",
      "remove",
    ]);
    expect(MEMORY_TOOL_PARAMETERS.operations.items.properties.action.enum).toEqual([
      "add",
      "replace",
      "remove",
    ]);
    expect(MEMORY_TOOL_PARAMETERS.operations.items.properties.action.required).toBe(
      true,
    );
  });

  it("schema validation matrix: invalid action / missing batch action rejected", () => {
    // The single-operation enum is enforced by the schema (the framework
    // turns a violation into an INVALID_ARGS result, same as v1's zod layer).
    expect(validateArgs(MEMORY_TOOL_PARAMETERS, { action: "read", content: "x" })).not.toEqual([]);
    // A batch item without action is rejected.
    expect(validateArgs(MEMORY_TOOL_PARAMETERS, { operations: [{}] })).not.toEqual([]);
    expect(
      validateArgs(MEMORY_TOOL_PARAMETERS, { operations: [{ action: "delete" }] }),
    ).not.toEqual([]);
    // The root stays open (the single-XOR-batch check is a runtime text
    // contract, not a schema failure — a model mistake stays recoverable).
    expect(validateArgs(MEMORY_TOOL_PARAMETERS, {})).toEqual([]);
    expect(
      validateArgs(MEMORY_TOOL_PARAMETERS, {
        operations: [{ action: "add", content: "x" }],
      }),
    ).toEqual([]);
    expect(
      validateArgs(MEMORY_TOOL_PARAMETERS, {
        action: "add",
        content: "x",
        old_text: "y",
      }),
    ).toEqual([]);
  });
});

describe("createMemoryToolDefinition", () => {
  const service = makeService(async () => "memory added");

  it("registers exactly the `memory` tool with a non-empty description", () => {
    const definition = createMemoryToolDefinition(service);
    expect(definition.name).toBe(MEMORY_TOOL_NAME);
    expect(definition.name).toBe("memory");
    expect(definition.description).toBe(MEMORY_TOOL_DESCRIPTION);
    expect(definition.description.length).toBeGreaterThan(0);
    expect(MEMORY_TOOL_DESCRIPTION).toContain("fixed at agent start");
    expect(MEMORY_TOOL_DESCRIPTION).not.toContain("skill");
    expect(MEMORY_TOOL_DESCRIPTION).not.toContain("compression");
  });

  it("renders a successful canonical value as one text block", () => {
    const definition = createMemoryToolDefinition(service);
    const content = definition.output.render({}, { result: "memory added" });
    expect(content).toEqual([{ type: "text", text: "memory added" }]);
  });
});

describe("executeMemoryTool (text results, never throws)", () => {
  it("uses the bound service and returns its text for the calling agent", async () => {
    const applyCall = vi.fn(async () => "memory added");
    const exec = makeExec();

    const result = await executeMemoryTool(
      makeService(applyCall),
      { action: "add", content: "一条记忆" },
      exec,
    );

    expect(result).toEqual({ result: "memory added" });
    expect(applyCall).toHaveBeenCalledWith(exec.agent, {
      action: "add",
      content: "一条记忆",
    });
  });

  it("does NOT resolve through agent.ctx (the regression T023 caught)", async () => {
    // The agent context deliberately carries no plannerMemory: the host
    // service lives in the host realm and the agent scope's isolate boundary
    // rejects the property walk, so the tool must use the service the preset
    // row bound at registration.
    const applyCall = vi.fn(async () => "memory added");
    const exec = { agent: { id: "planner", ctx: {} } } as unknown as ToolRunContext;

    const result = await executeMemoryTool(
      makeService(applyCall),
      { action: "add", content: "x" },
      exec,
    );

    expect(result).toEqual({ result: "memory added" });
    expect(applyCall).toHaveBeenCalledTimes(1);
  });

  it("keeps the argument combination contract ON the service (single XOR batch)", async () => {
    const applyCall = vi.fn(async () => "memory: provide EITHER action/content/old_text (single operation) OR operations (batch).");
    const result = await executeMemoryTool(
      makeService(applyCall),
      { action: "add", content: "x", operations: [{ action: "add", content: "y" }] },
      makeExec(),
    );

    expect(result.result).toContain("provide EITHER");
    expect(applyCall).toHaveBeenCalledTimes(1);
  });

  it("an infrastructure failure becomes `memory failed: …` TEXT (no throw)", async () => {
    const applyCall = vi.fn(async () => {
      throw Object.assign(new Error("memory service unavailable"), { code: 14 });
    });

    const result = await executeMemoryTool(
      makeService(applyCall),
      { action: "add", content: "x" },
      makeExec(),
    );

    expect(result).toEqual({ result: "memory failed: memory service unavailable" });
  });

  it("a missing agent caller becomes TEXT (no throw)", async () => {
    const result = await executeMemoryTool(
      makeService(async () => "memory added"),
      { action: "add", content: "x" },
      {} as ToolRunContext,
    );

    expect(result.result).toContain("memory failed:");
    expect(result.result).toContain("no agent on the tool execution");
  });
});


