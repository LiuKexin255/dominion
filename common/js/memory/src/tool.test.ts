/**
 * Memory tool tests — the `memory` tool's argument schema (v1
 * `memory-mcp.ts:427-471`), execution-time face resolution through
 * `exec.agent.ctx.memory`, and the text-result failure semantics.
 *
 * Pattern (style/javascript.md Mock convention): the agent-scoped face and the
 * tool execution context are injected doubles (`vi.fn()`); no module
 * interception. Contract: specs/074-dsh-grpc-runtime/contracts/memory-library.md
 * §5.
 */

import { validateArgs } from "@deepseek-ai/dsh-tools";
import type { ToolRunContext } from "@deepseek-ai/dsh-tools";
import { describe, expect, it, vi } from "vitest";

import type { MemoryFace } from "./face.js";
import {
  MEMORY_TOOL_DESCRIPTION,
  MEMORY_TOOL_NAME,
  MEMORY_TOOL_PARAMETERS,
  createMemoryToolDefinition,
  executeMemoryTool,
  resolveMemoryFace,
} from "./tool.js";

/** A tool execution carrying an agent whose scope holds the given face. */
function makeExec(
  face?: Partial<MemoryFace>,
  agentId = "planner",
): ToolRunContext {
  return {
    agent: { id: agentId, ctx: face === undefined ? {} : { memory: face } },
  } as unknown as ToolRunContext;
}

/** The agent-scoped face double. */
function makeFace(applyCall: (args: unknown) => Promise<string>): MemoryFace {
  return { applyCall } as unknown as MemoryFace;
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
  it("registers exactly the `memory` tool with a generic top-10 description", () => {
    const definition = createMemoryToolDefinition();
    expect(definition.name).toBe(MEMORY_TOOL_NAME);
    expect(definition.name).toBe("memory");
    expect(definition.description).toBe(MEMORY_TOOL_DESCRIPTION);
    expect(definition.description.length).toBeGreaterThan(0);
  });

  it("describes the snapshot top-10 recency bound and the full-storage writes (specs/068-wiki-round-pause/spec.md FR-012)", () => {
    expect(MEMORY_TOOL_DESCRIPTION).toContain("10 most recently updated");
    expect(MEMORY_TOOL_DESCRIPTION).toContain("not refreshed by writes");
    expect(MEMORY_TOOL_DESCRIPTION).toContain("full stored memory");
  });

  it("stays generic: no scenario nouns, no third-party terms, no other tools (specs/068-wiki-round-pause/spec.md FR-012)", () => {
    for (const scenarioNoun of ["planner", "hermes", "review", "player", "game"]) {
      expect(MEMORY_TOOL_DESCRIPTION.toLowerCase()).not.toContain(scenarioNoun);
    }
    expect(MEMORY_TOOL_DESCRIPTION).not.toContain("skill");
    expect(MEMORY_TOOL_DESCRIPTION).not.toContain("compression");
    // Only this tool's own function — the cross-tool workflow lives in the
    // capability unit's prompt sections (specs/068-wiki-round-pause/spec.md FR-013).
    expect(MEMORY_TOOL_DESCRIPTION).not.toContain("wiki");
    expect(MEMORY_TOOL_DESCRIPTION).not.toContain("knowledge base");
  });

  it("describes the batch form without promising transactionality (specs/068-wiki-round-pause/spec.md Edge Cases「批量 write 的原子性」)", () => {
    // The batch is described as a shape (one call), not as an atomic
    // transaction: the commit phase has no rollback (spec.md Edge Cases
    // 「批量 write 的原子性」).
    expect(MEMORY_TOOL_DESCRIPTION).toContain("operations applies a batch");
    const lower = MEMORY_TOOL_DESCRIPTION.toLowerCase();
    for (const promise of ["all-or-nothing", "atomic", "transaction"]) {
      expect(lower).not.toContain(promise);
    }
  });

  it("renders a successful canonical value as one text block", () => {
    const definition = createMemoryToolDefinition();
    const content = definition.output.render({}, { result: "memory added" });
    expect(content).toEqual([{ type: "text", text: "memory added" }]);
  });
});

describe("executeMemoryTool (text results, never throws)", () => {
  it("resolves the face through exec.agent.ctx.memory and returns its text", async () => {
    const applyCall = vi.fn(async () => "memory added");
    const exec = makeExec(makeFace(applyCall));

    const result = await executeMemoryTool(
      { action: "add", content: "一条记忆" },
      exec,
    );

    expect(result).toEqual({ result: "memory added" });
    expect(applyCall).toHaveBeenCalledWith({
      action: "add",
      content: "一条记忆",
    });
  });

  it("keeps the argument combination contract ON the face (single XOR batch)", async () => {
    const applyCall = vi.fn(async () => "memory: provide EITHER action/content/old_text (single operation) OR operations (batch).");
    const result = await executeMemoryTool(
      { action: "add", content: "x", operations: [{ action: "add", content: "y" }] },
      makeExec(makeFace(applyCall)),
    );

    expect(result.result).toContain("provide EITHER");
    expect(applyCall).toHaveBeenCalledTimes(1);
  });

  it("an infrastructure failure becomes `memory failed: …` TEXT (no throw)", async () => {
    const applyCall = vi.fn(async () => {
      throw Object.assign(new Error("memory service unavailable"), { code: 14 });
    });

    const result = await executeMemoryTool(
      { action: "add", content: "x" },
      makeExec(makeFace(applyCall)),
    );

    expect(result).toEqual({ result: "memory failed: memory service unavailable" });
  });

  it("a missing face fails loud with the shared wording (no silent success)", async () => {
    const result = await executeMemoryTool(
      { action: "add", content: "x" },
      makeExec(undefined, "planner-7"),
    );

    expect(result.result).toContain("memory failed:");
    expect(result.result).toContain(
      'memory tools require the agent-scoped "memory" service; agent "planner-7" has none',
    );
    expect(result.result).toContain("memory capability mount");
  });

  it("a missing agent caller becomes TEXT (no throw)", async () => {
    const result = await executeMemoryTool(
      { action: "add", content: "x" },
      {} as ToolRunContext,
    );

    expect(result.result).toContain("memory failed:");
    expect(result.result).toContain(
      "memory tool requires an agent caller",
    );
    expect(result.result).toContain("no agent on the tool execution");
  });
});

describe("resolveMemoryFace (fail-loud resolution)", () => {
  it("returns the agent-scoped face when present", () => {
    const face = makeFace(async () => "ok");
    expect(resolveMemoryFace(makeExec(face))).toBe(face as unknown as MemoryFace);
  });

  it("throws the shared missing-face error naming the agent", () => {
    expect(() => resolveMemoryFace(makeExec(undefined, "planner-7"))).toThrow(
      'agent "planner-7" has none',
    );
  });
});
