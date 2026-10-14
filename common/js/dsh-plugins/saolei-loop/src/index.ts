/**
 * cordis plugin entry for the saolei team loop: the orchestration layer of
 * the saolei team (驱动权唯一归属——游戏阶段机、成员驱动时机与 buffer 消费
 * 策略、物化编排) and the composition row `saolei-loop`.
 *
 * `apply` is the dependency-assembly step of the team-layer assembly
 * (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §2/§4): its
 * `config.plugins` is the FLAT capability-dependency list (no role grouping,
 * no whitelist, no validation), each row is mounted as a child plugin by its
 * bare package name (`Loader`-style dynamic import, resolved by the Node ESM
 * host closure) with the row's config, and the two provide faces the
 * orchestration consumes (`saoleiTools`, `memoryMount`) are read back and
 * fail loud when absent. The orchestrator itself is constructed per team by
 * the host (projects/game/agent_v2/src/session.ts), so per-team state and its
 * `ctx.team`/`ctx.agents` consumption never become composition singletons.
 * Agent DRIVING itself is owned by the official `dsh-agent-loop` row; this
 * plugin holds the orchestration state machine only, no turn/step driving
 * (specs/059-agent-v2-team-mode/research.md R1/R7).
 *
 * The game domain is an upstream service of this plugin: `inject: ["saolei"]`
 * declares the composition's top-level game-foundation row as a load-time
 * requirement, and the orchestrator consumes `ctx.saolei` — the game-runtime
 * factory it constructs and owns plus the gameplay section it registers per
 * member (specs/075-saolei-game-foundation/contracts/team-loop-assembly.md
 * §1/§2/§3).
 *
 * The orchestration state machine and its contracts:
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §2;
 * specs/059-agent-v2-team-mode/data-model.md §5.
 */

import type { Context, Plugin } from "@deepseek-ai/cordis";
import z from "@deepseek-ai/schemastery";

export {
  ANNOUNCER_MEMBER_ROLE,
  ANNOUNCER_MEMBER_SUMMARY,
  AnnouncerMember,
} from "./announcer.js";

export {
  DEFAULT_MEMBER_SUMMARIES,
  OrchestratorStateError,
  TEAM_PROVIDER,
  TeamOrchestrator,
} from "./orchestrator.js";
export type {
  AgentCreationSeam,
  AgentTeamRole,
  CancelResult,
  ComposePreset,
  ComposedPreset,
  MemoryCapabilities,
  MemoryFaceRegistration,
  MemoryMount,
  OrchestrationFailureContext,
  OrchestrationPhase,
  OrchestratorFailure,
  OrchestratorFailureOrigin,
  OrchestratorLogger,
  OrchestratorSnapshot,
  OrchestratorStateErrorCode,
  PlannerMemoryScope,
  SaoleiToolsUnit,
  SubmitResult,
  TeamMaterializeOptions,
  TeamMemberOptions,
  TeamOrchestratorDeps,
  TeamRole,
  TeamSeam,
} from "./orchestrator.js";

export const name = "saolei-loop";

/**
 * The game foundation face this plugin consumes. The composition's top-level
 * `saolei` row provides it, so a missing row fails the composition load at the
 * inject resolution rather than degrading to a team without a game runtime
 * (specs/075-saolei-game-foundation/contracts/team-loop-assembly.md §1).
 */
export const inject: string[] = ["saolei"];

/** One flat capability-dependency row (`config.plugins` item). */
export interface SaoleiLoopPluginRow {
  /** Bare package name resolved by the host ESM closure. */
  readonly name: string;
  /** Plugin config, validated by the mounted plugin's own Config schema. */
  readonly config?: unknown;
}

/** The saolei-loop row config: the flat capability-dependency list. */
export interface SaoleiLoopConfig {
  readonly plugins: readonly SaoleiLoopPluginRow[];
}

// Schemastery's ObjectS input shape is wider than the validated output type;
// the cast mirrors the official adapters' declared `Config: z<Config>` shape
// (common/js/dsh-plugins/llm-glm/src/index.ts precedent).
export const Config: z<SaoleiLoopConfig> = z.object({
  plugins: z
    .array(
      z.object({
        name: z.string().required(),
        config: z.any(),
      }),
    )
    .default([]),
}) as unknown as z<SaoleiLoopConfig>;

/**
 * Injection seams for {@link apply} (DI; tests pass stub plugin importers so
 * the assembly is exercised without real packages, style/javascript.md §测试).
 */
export interface SaoleiLoopPluginDeps {
  /** Bare-name plugin resolver; defaults to the Node ESM dynamic import. */
  readonly importPlugin?: (name: string) => Promise<Plugin>;
}

/**
 * Apply the plugin: mount every `config.plugins` row as a child plugin (the
 * plugin's own Config schema validates the row config; a rejection fails the
 * composition load) and read back both capability provide faces, failing loud
 * when a row is missing (specs/074-dsh-grpc-runtime/contracts/
 * team-assembly.md §2/§4).
 */
export async function apply(
  ctx: Context,
  config: SaoleiLoopConfig,
  deps: SaoleiLoopPluginDeps = {},
): Promise<void> {
  const importPlugin =
    deps.importPlugin ?? ((name: string): Promise<Plugin> => import(name));
  for (const row of config.plugins) {
    const entry = await importPlugin(row.name);
    await ctx.plugin(entry, row.config as never);
  }
  if (ctx.get("saoleiTools") === undefined) {
    throw new Error(
      'saolei-loop: the "saoleiTools" capability is not mounted; add the ' +
        "saolei unit provider to the saolei-loop row's config.plugins",
    );
  }
  if (ctx.get("memoryMount") === undefined) {
    throw new Error(
      'saolei-loop: the "memoryMount" capability is not mounted; add the ' +
        "memory provider to the saolei-loop row's config.plugins",
    );
  }
}
