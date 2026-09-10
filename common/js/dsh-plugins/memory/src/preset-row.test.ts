/**
 * Preset-row tests — the row the planner pool's template preset mounts:
 * exactly one `memory` tool and exactly one function-form snapshot section
 * (order 200+), no guidance section. The section's text closure reads the
 * host service's cache through `ctx.plannerMemory`, keyed by the assembly's
 * `context.scope`.
 *
 * Pattern (style/javascript.md Mock convention): the mounting context and
 * the host service are injected doubles (`vi.fn()`); no module interception.
 * Contract: specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §3 item 2.
 */

import type { Context } from "@deepseek-ai/cordis";
import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import { describe, expect, it, vi } from "vitest";

import { apply, inject, name } from "./preset-row.js";
import { MEMORY_SNAPSHOT_SECTION_NAME, MEMORY_SNAPSHOT_SECTION_ORDER } from "./snapshot.js";

interface SectionRecord {
  readonly name: string;
  readonly order: number;
  readonly text: string | ((context: { scope?: object }) => string);
}

/** A mounting context capturing the registered tool and prompt sections. */
function makeMount() {
  const tools: ToolDefinition[] = [];
  const sections: SectionRecord[] = [];
  const firstScope = { agent: "planner" };
  const snapshot = vi.fn((scope: object | undefined) =>
    scope === firstScope ? "长期记忆：\n开局先点中心更高效" : undefined,
  );
  const applyCall = vi.fn(async () => "memory added");
  const ctx = {
    tools: {
      register: vi.fn((definition: ToolDefinition) => {
        tools.push(definition);
        return vi.fn();
      }),
    },
    systemPrompt: {
      section: vi.fn((section: SectionRecord) => {
        sections.push(section);
        return vi.fn();
      }),
    },
    plannerMemory: { snapshot, applyCall },
  } as unknown as Context;
  return { ctx, tools, sections, snapshot, applyCall, firstScope };
}

describe("memory preset row apply", () => {
  it("declares the host memory service and the dsh core registries as injects", () => {
    expect(name).toBe("memory-row");
    expect([...inject].sort()).toEqual(["plannerMemory", "systemPrompt", "tools"]);
  });

  it("registers exactly one tool: the `memory` tool", () => {
    const { ctx, tools } = makeMount();

    apply(ctx);

    expect(tools).toHaveLength(1);
    expect(tools[0].name).toBe("memory");
  });

  it("binds the host service into the tool from the row context (not agent.ctx)", async () => {
    const { ctx, tools, applyCall } = makeMount();
    apply(ctx);

    // The calling agent's ctx deliberately carries no plannerMemory: the row
    // ctx is the composition point that declares the dependency, and the
    // agent scope's isolate boundary rejects the agent-ctx property walk
    // (the T023 regression).
    const exec = {
      agent: { id: "planner", ctx: {} },
    } as unknown as Parameters<NonNullable<ToolDefinition["execute"]>>[1];
    const result = (await tools[0].execute({ action: "add", content: "一条记忆" }, exec)) as {
      result: string;
    };

    expect(result).toEqual({ result: "memory added" });
    expect(applyCall).toHaveBeenCalledWith(exec.agent, {
      action: "add",
      content: "一条记忆",
    });
  });

  it("registers exactly one section: the function-form snapshot (order 200, no guidance)", () => {
    const { ctx, sections } = makeMount();

    apply(ctx);

    expect(sections).toHaveLength(1);
    expect(sections[0].name).toBe(MEMORY_SNAPSHOT_SECTION_NAME);
    expect(sections[0].name).toBe("memory:snapshot");
    expect(sections[0].order).toBe(MEMORY_SNAPSHOT_SECTION_ORDER);
    expect(sections[0].order).toBeGreaterThanOrEqual(200);
    expect(typeof sections[0].text).toBe("function");
    expect(sections.some((s) => s.name.includes("guidance"))).toBe(false);
  });

  it("the section reads the bound snapshot for the assembly scope", () => {
    const { ctx, sections, snapshot, firstScope } = makeMount();
    apply(ctx);
    const text = sections[0].text as (context: { scope?: object }) => string;

    expect(text({ scope: firstScope })).toBe("长期记忆：\n开局先点中心更高效");
    expect(snapshot).toHaveBeenCalledWith(firstScope);
  });

  it("an unbound or scope-less assembly renders the empty string (no section)", () => {
    const { ctx, sections } = makeMount();
    apply(ctx);
    const text = sections[0].text as (context: { scope?: object }) => string;

    expect(text({ scope: { other: "agent" } })).toBe("");
    expect(text({})).toBe("");
    expect(text({ scope: undefined })).toBe("");
  });
});
