/**
 * cordis plugin entry for the saolei team loop: the orchestration layer of
 * the saolei team (驱动权唯一归属——游戏阶段机、成员驱动时机与 buffer 消费
 * 策略、物化编排). The plugin is the composition row `saolei-loop`; its
 * `apply` carries no host-level service of its own — the host constructs one
 * {@link TeamOrchestrator} per team
 * (projects/game/agent_v2/src/session.ts), so per-team state and its
 * `ctx.team`/`ctx.agents` consumption never become composition singletons.
 * Agent DRIVING itself is owned by the official `dsh-agent-loop` row
 * (specs/059-agent-v2-team-mode/research.md R1/R7: the previous self-built
 * AgentFactory/driver was removed with the loop pivot, and GameRuntime
 * registration moved from the factory's prepare phase into the caller's
 * agent-creation setup hook — {@link createAgentGameRuntime} is the
 * production builder, called from the orchestration's player setup).
 *
 * The orchestration state machine and its contracts:
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §2;
 * specs/059-agent-v2-team-mode/data-model.md §5.
 *
 * Per-agent game state is NOT a host-level service: the GameRuntime
 * registers as the agent scope's `saoleiGame` service on the agent context
 * (cordis Service contract — unregistered automatically when the owning
 * agent scope unloads, saolei-plugins.md §2.1).
 *
 * `apply` also registers the `saolei:game` prompt section (game rules + the
 * operations the saolei tools expose; host scope, visible to every member) —
 * the game domain's single owner. Prompt ownership:
 * specs/060-agent-v2-team-optimize/contracts/prompt-sections.md §1.
 */

import type { Context } from "@deepseek-ai/cordis";

import { createAgentGameRuntime, GameRuntimeService } from "./game/runtime.js";
import type {
  CellOperation,
  GameEventRecord,
  GameLogEntry,
  GameRuntime,
  GameRuntimeDeps,
  GameStats,
  OperationType,
  OperateInput,
  SaoleiGame,
  ToolOutcome,
} from "./game/runtime.js";

export {
  createAgentGameRuntime,
  GameRuntimeService,
} from "./game/runtime.js";
export type {
  CellOperation,
  GameEventRecord,
  GameLogEntry,
  GameRuntime,
  GameRuntimeDeps,
  GameStats,
  OperationType,
  OperateInput,
  SaoleiGame,
  ToolOutcome,
} from "./game/runtime.js";

export {
  SAOLEI_MEMBER_ROLE,
  SAOLEI_MEMBER_SUMMARY,
  SaoleiSystemMember,
} from "./announcer.js";
export { gameStatsText } from "./game/text.js";

export {
  DEFAULT_MEMBER_SUMMARIES,
  OrchestratorStateError,
  TEAM_PROVIDER,
  TeamOrchestrator,
} from "./orchestrator.js";
export type {
  AgentCreationSeam,
  CancelResult,
  ComposePreset,
  ComposedPreset,
  GameEventSource,
  LoadPlannerMemory,
  MountPlayerRuntime,
  OrchestrationFailureContext,
  OrchestrationPhase,
  OrchestratorFailure,
  OrchestratorFailureOrigin,
  OrchestratorLogger,
  OrchestratorSnapshot,
  OrchestratorStateErrorCode,
  PlannerMemoryScope,
  SubmitResult,
  TeamMaterializeOptions,
  TeamMemberOptions,
  TeamOrchestratorDeps,
  TeamRole,
  TeamSeam,
} from "./orchestrator.js";

export const name = "saolei-loop";

/** Host-row plugin: registers the game-rules section on the host scope. */
export const inject = ["systemPrompt"];

declare module "@deepseek-ai/cordis" {
  interface Context {
    /**
     * The calling agent's game runtime — agent-scoped only: visible on
     * `agent.ctx` and its derived scopes (undefined on the host/root
     * context), unregistered when the agent scope unloads.
     */
    saoleiGame?: SaoleiGame;
  }
}

export function apply(ctx: Context): void {
  ctx.systemPrompt.section({
    name: "saolei:game",
    order: 50,
    text: SAOLEI_GAME_RULES,
  });
}

/**
 * The `saolei:game` section text: the classic Minesweeper rules plus the
 * operations the saolei tools actually expose. Ownership boundary
 * (specs/060-agent-v2-team-optimize/contracts/prompt-sections.md §1): game
 * facts only — no tool call shapes or result formats (saolei:guidance), no
 * member identity (persona), no team facts (team section). The rules follow
 * the classic (Win98-era Microsoft Minesweeper) form described at
 * https://en.wikipedia.org/wiki/Microsoft_Minesweeper and
 * https://en.wikipedia.org/wiki/Minesweeper_(video_game) ; the operation set
 * is the intersection with the saolei plugin's three tools
 * (`common/js/dsh-plugins/saolei/src/index.ts`). The `saolei_remain` per-cell
 * wording and the global-counter distinction follow
 * specs/064-memory-split-fold-remain/contracts/saolei-plugins.md §3.
 */
export const SAOLEI_GAME_RULES = `## 扫雷玩法与可用操作

对局为经典扫雷（旧版 Windows / Win98 时代形态）：

- 棋盘是一张隐藏的雷区网格；目标是揭示全部非雷格且不踩雷。
- 揭示一个非雷格后：数字 1–8 表示该格八邻格中的雷数；空白（0）表示相邻无雷，并会级联展开相邻的非雷区域。
- 未揭示的格子可以标旗作为推理标记（标旗不改变格内容，可再次操作取消）。
- 对已揭示的数字格，当其相邻旗数满足该数字时，可以 chord（左右同击）一次展开其余未标旗的邻格。
- 踩中雷即本局失败（负局棋盘会展示全部雷位）；全部非雷格揭示即本局获胜。
- 顶部计数器的剩余雷数计数 = 总雷数 − 已标旗数，可以为负（表示标旗过多）；它是全局计数，与 \`saolei_remain\` 的每格视图不同。

可用操作（与 saolei 工具能力一致）：

- 开局/重开一局：\`saolei_init\`（再次调用即重开并重新播种）。
- 格子操作：\`saolei_operate\` 支持 click（揭示）、flag（标旗/取消标旗）、chord（同击），可单发或按序批量执行。
- 只读查询：\`saolei_remain\` 返回每个已揭示数字格周围的剩余未标记雷数（= 数字 − 相邻已标旗数，可为 0 或负），不是旗子数量。`;
