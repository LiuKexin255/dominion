/**
 * Memory tool-row tests — the row the planner pool's template preset mounts:
 * the `memory` tool plus the three wiki tools (`wiki_search`/`wiki_get`/
 * `wiki_write`), the two guidance sections (`memory:guidance` order 110,
 * `wiki:guidance` order 120) and the function-form snapshot section (order
 * 200). The section's text closure reads the host service's cache through
 * `ctx.plannerMemory`, keyed by the assembly's `context.scope`; the wiki
 * tools and the wiki guidance section inject no entry content into the
 * system prompt.
 *
 * Pattern (style/javascript.md Mock convention): the mounting context and
 * the host service are injected doubles (`vi.fn()`); no module interception.
 * Contract: specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §2;
 * specs/068-wiki-round-pause/contracts/dsh-plugins.md §2-3.
 */

import type { Context } from "@deepseek-ai/cordis";
import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import { describe, expect, it, vi } from "vitest";

import {
  MEMORY_SNAPSHOT_SECTION_NAME,
  MEMORY_SNAPSHOT_SECTION_ORDER,
} from "@dominion/dsh-memory-service";
import {
  MEMORY_GUIDANCE,
  MEMORY_GUIDANCE_SECTION_NAME,
  MEMORY_GUIDANCE_SECTION_ORDER,
  WIKI_GUIDANCE,
  WIKI_GUIDANCE_SECTION_NAME,
  WIKI_GUIDANCE_SECTION_ORDER,
} from "./guidance.js";
import { apply, inject, name } from "./index.js";

interface SectionRecord {
  readonly name: string;
  readonly order: number;
  readonly text: string | ((context: { scope?: object }) => string);
}

/** A mounting context capturing the registered tools and prompt sections. */
function makeMount() {
  const tools: ToolDefinition[] = [];
  const sections: SectionRecord[] = [];
  const firstScope = { agent: "planner" };
  const snapshot = vi.fn((scope: object | undefined) =>
    scope === firstScope ? "长期记忆：\n开局先点中心更高效" : undefined,
  );
  const applyCall = vi.fn(async () => "memory added");
  const wikiSearch = vi.fn(async () => "wiki: 1 title(s) matched 'x':\n- 标题");
  const wikiGet = vi.fn(async () => "wiki: 1 entry(ies) retrieved.");
  const wikiWrite = vi.fn(async () => "wiki: 1 entry(ies) applied");
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
    plannerMemory: { snapshot, applyCall, wikiSearch, wikiGet, wikiWrite },
  } as unknown as Context;
  return {
    ctx,
    tools,
    sections,
    snapshot,
    applyCall,
    wikiSearch,
    wikiGet,
    wikiWrite,
    firstScope,
  };
}

function toolNamed(tools: readonly ToolDefinition[], toolName: string): ToolDefinition {
  const found = tools.find((tool) => tool.name === toolName);
  if (found === undefined) {
    throw new Error(`tool ${toolName} was not registered`);
  }
  return found;
}

function sectionNamed(
  sections: readonly SectionRecord[],
  sectionName: string,
): SectionRecord {
  const found = sections.find((section) => section.name === sectionName);
  if (found === undefined) {
    throw new Error(`section ${sectionName} was not registered`);
  }
  return found;
}

describe("memory preset row apply", () => {
  it("declares the host memory service and the dsh core registries as injects", () => {
    expect(name).toBe("memory-row");
    expect([...inject].sort()).toEqual(["plannerMemory", "systemPrompt", "tools"]);
  });

  it("registers the memory tool plus the three wiki tools", () => {
    const { ctx, tools } = makeMount();

    apply(ctx);

    expect(tools.map((tool) => tool.name).sort()).toEqual([
      "memory",
      "wiki_get",
      "wiki_search",
      "wiki_write",
    ]);
  });

  it("binds the host service into the memory tool from the row context (not agent.ctx)", async () => {
    const { ctx, tools, applyCall } = makeMount();
    apply(ctx);

    // The calling agent's ctx deliberately carries no plannerMemory: the row
    // ctx is the composition point that declares the dependency, and the
    // agent scope's isolate boundary rejects the agent-ctx property walk
    // (the T023 regression).
    const exec = {
      agent: { id: "planner", ctx: {} },
    } as unknown as Parameters<NonNullable<ToolDefinition["execute"]>>[1];
    const result = (await toolNamed(tools, "memory").execute(
      { action: "add", content: "一条记忆" },
      exec,
    )) as { result: string };

    expect(result).toEqual({ result: "memory added" });
    expect(applyCall).toHaveBeenCalledWith(exec.agent, {
      action: "add",
      content: "一条记忆",
    });
  });

  it("binds the host service into the wiki tools from the row context", async () => {
    const { ctx, tools, wikiSearch, wikiGet, wikiWrite } = makeMount();
    apply(ctx);
    const exec = {
      agent: { id: "planner", ctx: {} },
    } as unknown as Parameters<NonNullable<ToolDefinition["execute"]>>[1];

    expect(await toolNamed(tools, "wiki_search").execute({ query: "开局" }, exec)).toEqual(
      { result: "wiki: 1 title(s) matched 'x':\n- 标题" },
    );
    expect(wikiSearch).toHaveBeenCalledWith(exec.agent, "开局");

    expect(
      await toolNamed(tools, "wiki_get").execute({ titles: ["标题"] }, exec),
    ).toEqual({ result: "wiki: 1 entry(ies) retrieved." });
    expect(wikiGet).toHaveBeenCalledWith(exec.agent, ["标题"]);

    expect(
      await toolNamed(tools, "wiki_write").execute(
        { title: "标题", content: "正文" },
        exec,
      ),
    ).toEqual({ result: "wiki: 1 entry(ies) applied" });
    expect(wikiWrite).toHaveBeenCalledWith(exec.agent, {
      title: "标题",
      content: "正文",
      entries: undefined,
    });
  });

  it("an unbound wiki call renders the failure as text (no throw)", async () => {
    const { ctx, tools, wikiSearch } = makeMount();
    wikiSearch.mockRejectedValue(new Error("no loaded memory scope"));
    apply(ctx);
    const exec = {
      agent: { id: "planner", ctx: {} },
    } as unknown as Parameters<NonNullable<ToolDefinition["execute"]>>[1];

    const result = await toolNamed(tools, "wiki_search").execute({ query: "x" }, exec);

    expect(result).toEqual({ result: "wiki failed: no loaded memory scope" });
  });

  it("registers the two guidance sections and the snapshot section in order", () => {
    const { ctx, sections } = makeMount();

    apply(ctx);

    expect(sections.map((section) => section.name)).toEqual([
      MEMORY_GUIDANCE_SECTION_NAME,
      WIKI_GUIDANCE_SECTION_NAME,
      MEMORY_SNAPSHOT_SECTION_NAME,
    ]);
    expect(sections.map((section) => section.order)).toEqual([
      MEMORY_GUIDANCE_SECTION_ORDER,
      WIKI_GUIDANCE_SECTION_ORDER,
      MEMORY_SNAPSHOT_SECTION_ORDER,
    ]);
    expect(sections[0].order).toBe(110);
    expect(sections[1].order).toBe(120);
    expect(sections[2].order).toBe(200);
    // Both guidance sections carry static text; only the snapshot section is
    // a closure over the host service.
    expect(sections[0].text).toBe(MEMORY_GUIDANCE);
    expect(sections[1].text).toBe(WIKI_GUIDANCE);
    expect(typeof sections[2].text).toBe("function");
    // Wiki entries inject no content into the system prompt: `wiki:guidance`
    // is static workflow text, never a snapshot closure
    // (specs/068-wiki-round-pause/spec.md Edge Cases「wiki 不注入快照」).
    expect(typeof sectionNamed(sections, "wiki:guidance").text).toBe("string");
  });

  it("the snapshot section reads the bound snapshot for the assembly scope", () => {
    const { ctx, sections, snapshot, firstScope } = makeMount();
    apply(ctx);
    const text = sectionNamed(sections, MEMORY_SNAPSHOT_SECTION_NAME).text as (
      context: { scope?: object },
    ) => string;

    expect(text({ scope: firstScope })).toBe("长期记忆：\n开局先点中心更高效");
    expect(snapshot).toHaveBeenCalledWith(firstScope);
  });

  it("an unbound or scope-less assembly renders the empty string (no section)", () => {
    const { ctx, sections } = makeMount();
    apply(ctx);
    const text = sectionNamed(sections, MEMORY_SNAPSHOT_SECTION_NAME).text as (
      context: { scope?: object },
    ) => string;

    expect(text({ scope: { other: "agent" } })).toBe("");
    expect(text({})).toBe("");
    expect(text({ scope: undefined })).toBe("");
  });
});
