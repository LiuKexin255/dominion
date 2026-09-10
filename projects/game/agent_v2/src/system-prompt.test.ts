import { describe, expect, it, vi } from "vitest";
import type { Agent } from "@deepseek-ai/dsh-agent";
import type { PromptAssembly } from "@deepseek-ai/dsh-system-prompt";
import { readMemberSystemPrompt } from "./system-prompt.js";

/**
 * Unit tests for the member system-prompt read face behind GetTeamMember
 * (specs/059-agent-v2-team-mode/contracts/web-views.md §5): the read runs the
 * instance's live assembly surface — `assembleContextFor(agent)` + the
 * official `renderPrompt` — never a separate re-composition. The assembly
 * service is a `vi.fn()` double and the injected sections stand in for their
 * owners' contributions (preset persona, team section, tool guidance, planner
 * memory snapshot); no module interception (style/javascript.md Mock
 * convention).
 */

function assembly(
  sections: Array<{ name: string; text: string }>,
  variables: Record<string, string> = {},
): PromptAssembly {
  return { sections, contexts: [], tools: [], variables };
}

function fakeAgent(assemble: (context: unknown) => Promise<PromptAssembly>): Agent {
  return { id: "a1", ctx: { systemPrompt: { assemble } } } as unknown as Agent;
}

describe("readMemberSystemPrompt", () => {
  it("renders every section of the agent-scoped assembly (persona, team, tool guidance)", async () => {
    const assemble = vi.fn(async (_context: unknown) =>
      assembly([
        { name: "deployment:persona", text: "你是扫雷 player，负责操作桌面扫雷窗口完成对局。" },
        {
          name: "team:roster",
          text: "团队目标：协作完成多局扫雷游戏。\n- [planner] 复盘对局与制定策略",
        },
        { name: "tool:saolei", text: "## saolei (Minesweeper tools)\n- saolei_init()" },
      ]),
    );
    const agent = fakeAgent(assemble);

    const prompt = await readMemberSystemPrompt(agent);

    expect(prompt).toContain("你是扫雷 player");
    expect(prompt).toContain("团队目标：协作完成多局扫雷游戏。");
    expect(prompt).toContain("## saolei (Minesweeper tools)");
    expect(prompt).not.toContain("长期记忆");
    // The read IS the loop's assembly call: the coupled agent/scope context
    // from assembleContextFor, assembled once per read.
    expect(assemble).toHaveBeenCalledTimes(1);
    const context = assemble.mock.calls[0]?.[0] as { agent?: Agent; scope?: Agent };
    expect(context.agent).toBe(agent);
    expect(context.scope).toBe(agent);
  });

  it("stays role-differentiated: player guidance vs planner memory snapshot", async () => {
    const player = fakeAgent(async () =>
      assembly([
        { name: "deployment:persona", text: "你是扫雷 player" },
        { name: "tool:saolei", text: "saolei 工具守则：以工具返回的棋盘事实为准" },
      ]),
    );
    const planner = fakeAgent(async () =>
      assembly([
        { name: "deployment:persona", text: "你是扫雷 planner" },
        { name: "memory:snapshot", text: "长期记忆：\n第一局胜率 50%" },
      ]),
    );

    const playerPrompt = await readMemberSystemPrompt(player);
    const plannerPrompt = await readMemberSystemPrompt(planner);

    expect(playerPrompt).toContain("你是扫雷 player");
    expect(playerPrompt).toContain("saolei 工具守则");
    expect(playerPrompt).not.toContain("长期记忆");
    expect(plannerPrompt).toContain("你是扫雷 planner");
    expect(plannerPrompt).toContain("长期记忆：");
    expect(plannerPrompt).not.toContain("saolei 工具守则");
  });

  it("renders through the official renderPrompt (strict interpolation, empty sections dropped)", async () => {
    const assemble = vi.fn(async () =>
      assembly(
        [
          { name: "deployment:persona", text: "你是扫雷 player，运行模型 {{model}}。" },
          // An empty contribution (e.g. an empty memory snapshot) is dropped
          // by the official renderer and must not leave a blank separator.
          { name: "memory:snapshot", text: "" },
        ],
        { model: "glm-5.3" },
      ),
    );
    const agent = fakeAgent(assemble);

    const prompt = await readMemberSystemPrompt(agent);

    expect(prompt).toContain("运行模型 glm-5.3。");
    expect(prompt).not.toContain("{{model}}");
    expect(prompt).not.toContain("\n\n");
    // String concatenation would neither call the assembly service nor apply
    // the official interpolation/empty-drop join semantics.
    expect(assemble).toHaveBeenCalledTimes(1);
  });

  it("propagates an assembly failure instead of returning an empty prompt", async () => {
    const agent = fakeAgent(async () => {
      throw new Error("section provider exploded");
    });

    await expect(readMemberSystemPrompt(agent)).rejects.toThrow("section provider exploded");
  });
});
