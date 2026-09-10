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
