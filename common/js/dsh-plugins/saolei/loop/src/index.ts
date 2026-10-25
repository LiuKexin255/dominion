/**
 * cordis plugin entry for the saolei team loop: the orchestration layer of
 * the saolei team (flow 流程控制 + 执行层 + 成员适配器、物化编排) and the
 * composition row `saolei-loop`.
 *
 * `apply` is the dependency-assembly step of the team-layer assembly
 * (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §2/§4): its
 * `config.plugins` is the FLAT capability-dependency list (no role grouping,
 * no whitelist, no validation), each row is mounted as a child plugin by its
 * bare package name (`Loader`-style dynamic import, resolved by the Node ESM
 * host closure) with the row's config, and the two required provide faces the
 * orchestration consumes (`saoleiTools`, `memoryMount`) are read back and fail
 * loud when absent. The hooks row is the one OPTIONAL capability
 * (specs/078-saolei-js-hooks/spec.md FR-015): a composition without it keeps
 * the pre-hooks behavior, so only a LISTED hooks row is checked — listed but
 * unprovided fails the load like the two required faces
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §5). The
 * orchestrator itself is constructed per team by
 * the host (projects/game/agent_v2/src/session.ts), so per-team state and its
 * `ctx.team`/`ctx.agents` consumption never become composition singletons.
 * Agent DRIVING is split between the official `dsh-agent-loop` row (turn/step
 * execution) and this plugin's member adapters (completion judgement,
 * delivery split, held-input replay); the workflow decision lives in the
 * package's pure flow layer (flow.ts) and the orchestrator only consumes its
 * `start(activity)` directives
 * (specs/076-agent-v2-optimize/contracts/flow-orchestration.md).
 *
 * The game domain is an upstream service of this plugin: `inject: ["saolei"]`
 * declares the composition's top-level game-foundation row as a load-time
 * requirement, and the orchestrator consumes `ctx.saolei` — the game-runtime
 * factory it constructs and owns plus the gameplay section it registers per
 * member (specs/075-saolei-game-foundation/contracts/team-loop-assembly.md
 * §1/§2/§3).
 *
 * The orchestration contracts:
 * specs/076-agent-v2-optimize/contracts/flow-orchestration.md;
 * specs/076-agent-v2-optimize/data-model.md §2/§5;
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §2.
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
  phaseOfActivity,
} from "./orchestrator.js";
export type { LoopErrorCode, FailureDetail } from "./member.js";
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
  SaoleiHooksMount,
  SaoleiHooksUnit,
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

/**
 * The optional hooks capability row
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §5): the only
 * `config.plugins` entry a composition may omit, because it is the only
 * capability the team works without. Its presence is what makes a missing
 * `saoleiHooks` provide face a load-time error.
 */
const SAOLEI_HOOKS_PLUGIN = "@dominion/dsh-saolei-hooks";

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
 * composition load), then read back the capability provide faces: the two
 * required ones (`saoleiTools`, `memoryMount`) always, and `saoleiHooks` only
 * when its row is listed — the hooks capability is optional, so an absent row
 * leaves the pre-hooks behavior intact while a listed row that provided
 * nothing fails the load
 * (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §2/§4;
 * specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §5).
 */
export async function apply(
  ctx: Context,
  config: SaoleiLoopConfig,
  deps: SaoleiLoopPluginDeps = {},
): Promise<void> {
  const importPlugin =
    deps.importPlugin ?? ((name: string): Promise<Plugin> => import(name));
  let hooksRowListed = false;
  for (const row of config.plugins) {
    if (row.name === SAOLEI_HOOKS_PLUGIN) {
      hooksRowListed = true;
    }
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
  if (hooksRowListed && ctx.get("saoleiHooks") === undefined) {
    throw new Error(
      'saolei-loop: the "saoleiHooks" capability is not mounted; the ' +
        "saolei-hooks provider is listed in the saolei-loop row's " +
        "config.plugins but provides no face",
    );
  }
}
