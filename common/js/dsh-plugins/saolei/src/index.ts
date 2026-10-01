/**
 * cordis plugin entry for the saolei game foundation
 * (`@dominion/dsh-saolei`, specs/075-saolei-game-foundation/contracts/
 * saolei-foundation.md): the composition's top-level domain foundation row.
 * `apply` provides the foundation service face `ctx.provide("saolei", {
 * createGameRuntime, gameSection })` — the pure domain game-runtime factory
 * (the orchestration constructs and owns the instance;
 * specs/075-saolei-game-foundation/research.md D10) plus the `saolei:game`
 * gameplay section the orchestration registers per member
 * (specs/060-agent-v2-team-optimize/contracts/prompt-sections.md §1).
 *
 * The package owns the saolei game DOMAIN only: the runtime implementation
 * (`src/game/`), the gameplay section content and the game type/value export
 * face. Model-facing tools and the `saolei:guidance` tool-usage section belong
 * to the separate tool plugin; the gameplay section is registered by the
 * orchestration (this plugin registers nothing on the composition scope).
 * `saoleiGame` is an agent-scoped handle registered on `agent.ctx` by the
 * orchestration's player setup, so the Context augmentation below is the
 * type-face the tools resolve `exec.agent.ctx.saoleiGame` against
 * (specs/075-saolei-game-foundation/contracts/team-loop-assembly.md §2/§3).
 */

import type { PromptSection } from "@deepseek-ai/dsh-system-prompt";
import type { Context } from "@deepseek-ai/cordis";

import { createGameRuntime } from "./game/runtime.js";
import type { SaoleiGame } from "./game/runtime.js";

export const name = "saolei";

/** Pure provider: the foundation is exposed at plugin load, nothing is required. */
export const inject: string[] = [];

/** The provide name of the game foundation face (contracts/saolei-foundation.md §1). */
export const SAOLEI_FOUNDATION_SERVICE = "saolei";

/**
 * The game foundation face: the game domain's service boundary for the
 * orchestration layer (contracts/saolei-foundation.md §2). Only the two
 * runtime values the orchestration consumes travel in the face; game types
 * and pure functions (`gameStatsText`, the entity types) are library exports.
 */
export interface SaoleiFoundation {
  /** Pure domain game-runtime factory — the orchestration constructs and
   * owns the instance (research.md D10). */
  readonly createGameRuntime: typeof createGameRuntime;
  /** The gameplay section spec the orchestration registers for player and
   * planner. */
  readonly gameSection: PromptSection;
}

// ── game domain single export face (contracts/saolei-foundation.md §3) ──────
// The pure factory, the terminal stats text and every game type are exported
// from this package alone; the orchestration and the tool plugin consume them
// here (spec M8). SAOLEI_GAME_RULES / SAOLEI_GAME_SECTION stay in this file.
export { createGameRuntime };
export { gameStatsText } from "./game/text.js";
export type {
  CellOperation,
  GameEventRecord,
  GameLogEntry,
  GameRuntime,
  GameRuntimeDeps,
  GameStats,
  OperateInput,
  OperationType,
  SaoleiGame,
  ToolOutcome,
} from "./game/runtime.js";

declare module "@deepseek-ai/cordis" {
  interface Context {
    /** Composition-level game foundation face (the top-level saolei row
     * provides it; resolvable from any agent-derived scope). */
    saolei?: SaoleiFoundation;
    /** The player member's agent-scoped game handle: the orchestration
     * registers the runtime it holds on `agent.ctx` for tool resolution. */
    saoleiGame?: SaoleiGame;
  }
}

/**
 * Apply the plugin: provide the game foundation face through
 * `ctx.provide("saolei", ...)` (the game-runtime factory plus the
 * `saolei:game` section). Nothing is registered on the composition scope —
 * the orchestration registers the section per member itself
 * (specs/075-saolei-game-foundation/contracts/team-loop-assembly.md §3).
 */
export function apply(ctx: Context): void {
  ctx.provide(SAOLEI_FOUNDATION_SERVICE, {
    createGameRuntime,
    gameSection: SAOLEI_GAME_SECTION,
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
 * is the intersection with the three tools the tool plugin provides. The
 * `saolei_remain` per-cell wording and the global-counter distinction follow
 * specs/064-memory-split-fold-remain/contracts/saolei-plugins.md §3.
 * Section name/order/text are the frozen saolei contract values
 * (specs/075-saolei-game-foundation/contracts/saolei-foundation.md §2).
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

/**
 * The `saolei:game` section spec carried by the foundation face: name, order
 * and text are the frozen saolei contract values
 * (specs/075-saolei-game-foundation/contracts/saolei-foundation.md §2). The
 * orchestration registers it for both player and planner
 * (specs/075-saolei-game-foundation/contracts/team-loop-assembly.md §3).
 */
export const SAOLEI_GAME_SECTION: PromptSection = {
  name: "saolei:game",
  order: 50,
  text: SAOLEI_GAME_RULES,
};
