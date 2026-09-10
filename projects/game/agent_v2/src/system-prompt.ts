/**
 * system-prompt.ts — the member instance system-prompt read face behind
 * GetTeamMember's output-only `system_prompt` field (FR-016).
 *
 * The reader runs the SAME assembly path the official agent loop runs for a
 * model request: `assembleContextFor(agent)` builds the agent-scoped assembly
 * context, `agent.ctx.systemPrompt.assemble()` resolves the registered
 * sections (global + agent scope), and the official `renderPrompt()`
 * interpolates variables, drops empty sections and joins the rest. Nothing is
 * re-composed here, so persona (preset), team roster (team plugin), tool
 * guidance (saolei/memory rows) and the planner memory snapshot always come
 * from the sections that own them
 * (specs/059-agent-v2-team-mode/data-model.md §2 SystemPrompt 所有权表;
 * specs/059-agent-v2-team-mode/contracts/web-views.md §5 "服务端从装配面取，
 * 非另行拼装").
 *
 * Assembly pipeline references:
 * - the loop's pre-step assembles with `assembleContextFor(agent, signal)`
 *   and its step renders with `renderPrompt`
 *   (https://unpkg.com/@deepseek-ai/dsh-agent-loop@0.1.1-rc.2/lib/index.js);
 * - `assembleContextFor` couples the agent subject with its scope so
 *   agent-scoped sections cannot be omitted
 *   (https://unpkg.com/@deepseek-ai/dsh-agent@0.1.1-rc.2/lib/types/dispatch.d.ts);
 * - `SystemPrompt.assemble` / `renderPrompt` semantics
 *   (https://unpkg.com/@deepseek-ai/dsh-system-prompt@0.1.1-rc.2/lib/types/index.d.ts).
 */

import { assembleContextFor } from "@deepseek-ai/dsh-agent";
import type { Agent } from "@deepseek-ai/dsh-agent";
import { renderPrompt } from "@deepseek-ai/dsh-system-prompt";

/**
 * Read one member agent's complete effective system prompt.
 *
 * The read is a fresh assembly at call time (no cached snapshot), so after a
 * team refresh the GetTeamMember projection follows the new member instance's
 * handle naturally (specs/059-agent-v2-team-mode/contracts/team-api.md §1).
 * A failing section provider or assembly waterfall rejects — the caller maps
 * it to INTERNAL with the cause chain (contracts/team-api.md §6), never to a
 * silent empty prompt.
 */
export async function readMemberSystemPrompt(agent: Agent): Promise<string> {
  const assembly = await agent.ctx.systemPrompt.assemble(assembleContextFor(agent));
  return renderPrompt(assembly);
}
