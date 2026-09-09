/**
 * cordis plugin entry for the saolei team loop: the orchestration layer of
 * the saolei team (驱动权唯一归属——游戏阶段机、成员驱动时机、物化编排).
 * The plugin is the composition row `saolei-loop`; its `apply` carries no
 * service of its own yet — the orchestration state machine lands with the
 * team model (specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §2),
 * while agent DRIVING is owned by the official `dsh-agent-loop` row
 * (specs/059-agent-v2-team-mode/research.md R1/R7: the previous self-built
 * AgentFactory/driver was removed with the loop pivot, and GameRuntime
 * registration moved from the factory's prepare phase into the caller's
 * agent-creation setup hook — {@link createAgentGameRuntime} is the
 * production builder, called from
 * projects/game/agent_v2/src/session.ts materialization).
 *
 * Per-agent game state is NOT a host-level service: the GameRuntime
 * registers as the agent scope's `saoleiGame` service on the agent context
 * (cordis Service contract — unregistered automatically when the owning
 * agent scope unloads, saolei-plugins.md §2.1).
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

export const name = "saolei-loop";

/** Host-row plugin: no service requirements. */
export const inject: string[] = [];

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

export function apply(_ctx: Context): void {}
