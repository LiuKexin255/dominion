/**
 * scene-saolei.ts — the saolei scene registration of agent_v2.
 *
 * Wraps the saolei-loop `TeamOrchestrator` in the scene-neutral orchestration
 * seam and keeps every saolei-specific derivation on the scene side: the
 * shared team goal, the planner memory scope, the announce-only announcer
 * projection and its materialization invariant, and the workflow `phase` log
 * field. The host consumes only the seam
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §5).
 *
 * The scene table (`scenes.ts`) binds its own entry through the `spec`
 * argument of the factory; this module never imports the scene table, so the
 * runtime dependency edge stays one-way
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §1/§7).
 */

import type { AgentHandle } from "@deepseek-ai/dsh-agent";
import type { TeamEventStream } from "@dominion/dsh-team";
import { OrchestratorStateError, TeamOrchestrator } from "@dominion/dsh-saolei-loop";
import type {
  AgentTeamRole,
  AnnouncerMember,
  CancelResult,
  OrchestratorLogger,
  OrchestratorSnapshot,
  SubmitResult,
  TeamMaterializeOptions,
  TeamMemberOptions,
} from "@dominion/dsh-saolei-loop";

import type { DshContext } from "./dsh.js";
import { SceneStateError } from "./orchestration.js";
import type {
  SceneLogger,
  SceneMaterializeInput,
  SceneMember,
  SceneMemberConfig,
  SceneOrchestration,
  SceneRegistrationDeps,
  SceneSnapshot,
} from "./orchestration.js";

/**
 * The saolei scene data the adapter binds — the subset of the scene table
 * entry the adapter consumes. Declared structurally so the scene registration
 * module stays independent of `scenes.ts`
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §1).
 */
export interface SaoleiSceneData {
  /** Goal text of the shared team section, forwarded verbatim. */
  readonly teamGoal: string;
}

/**
 * The team-level orchestration face the adapter consumes. `TeamOrchestrator`
 * satisfies it structurally; unit tests inject a double instead of
 * intercepting modules (style/javascript.md Mock convention).
 */
export interface SaoleiTeamOrchestration {
  materialize(options: TeamMaterializeOptions): Promise<void>;
  member(role: AgentTeamRole): AgentHandle | undefined;
  teamEvents(): TeamEventStream;
  snapshot(): OrchestratorSnapshot;
  submit(text: string): SubmitResult;
  cancel(): CancelResult;
  whenQuiescent(): Promise<void>;
  dispose(): Promise<void>;
  /** The announce-only member; undefined exactly while unmaterialized. */
  readonly announcer: AnnouncerMember | undefined;
}

/**
 * The materialized member projection, in presentation order: the two
 * agent-backed members then the announce-only announcer
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §3;
 * specs/077-announcer-member-unification/contracts/host-member-parity.md §1).
 */
const SAOLEI_MEMBERS: readonly SceneMember[] = [
  { role: "player", kind: "agent" },
  { role: "planner", kind: "agent" },
  { role: "announcer", kind: "announce-only" },
];

/** The agent-backed saolei roles (`member` read surface). */
function isAgentRole(role: string): role is AgentTeamRole {
  return role === "player" || role === "planner";
}

/**
 * Wrap the host failure reporter so the saolei `phase` field stays in the
 * scene-local log context and never enters the generic seam
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §5).
 */
function saoleiLogger(logger: SceneLogger): OrchestratorLogger {
  return {
    error(message, context) {
      logger.error(message, {
        session: context.session,
        phase: context.phase,
        member: context.member ?? "",
        code: context.code,
        error: context.error,
      });
    },
  };
}

/** Translate a package state error into the seam's state error by `code`. */
function mapStateError(err: unknown): unknown {
  return err instanceof OrchestratorStateError
    ? new SceneStateError(err.code, err.message)
    : err;
}

/** The member configuration of one role (host roster validation guarantees presence). */
function memberConfig(
  input: SceneMaterializeInput,
  role: string,
): SceneMemberConfig {
  const member = input.members.find((candidate) => candidate.role === role);
  if (member === undefined) {
    throw new Error(
      `saolei orchestration requires the "${role}" member configuration`,
    );
  }
  return member;
}

/** Forward one member's preset id and split model route. */
function memberOptions(config: SceneMemberConfig): TeamMemberOptions {
  return {
    preset: config.preset,
    provider: config.provider,
    model: config.model,
  };
}

/**
 * The saolei adapter over {@link SaoleiTeamOrchestration}: the scene-neutral
 * seam backed by the team loop. `materialize` owns the scene derivation, the
 * announcer invariant and the member projection; every other member is a
 * pass-through with state-error translation and the `phase` field dropped
 * from the snapshot (specs/080-host-main-sub-model/contracts/
 * scene-registry.md §2/§5).
 */
export class SaoleiSceneOrchestration implements SceneOrchestration {
  constructor(
    private readonly orchestrator: SaoleiTeamOrchestration,
    private readonly spec: SaoleiSceneData,
  ) {}

  async materialize(input: SceneMaterializeInput): Promise<void> {
    const player = memberConfig(input, "player");
    const planner = memberConfig(input, "planner");
    const options: TeamMaterializeOptions = {
      session: input.session,
      // The memory scope halves are the business template and the planner
      // preset the team is materialized from; the planner's long-term memory
      // is mounted on that preset
      // (specs/071-memory-preset-scope/contracts/dsh-plugins.md §4).
      memoryScope: { template: input.template, preset: planner.preset },
      goal: this.spec.teamGoal,
      player: memberOptions(player),
      planner: memberOptions(planner),
    };
    try {
      await this.orchestrator.materialize(options);
    } catch (err) {
      throw mapStateError(err);
    }
    // The announcer is part of the saolei roster but not of the caller's
    // configuration: a materialization without it is a broken team, rolled
    // back whole and never exposed as half-materialized
    // (specs/080-host-main-sub-model/contracts/scene-registry.md §5;
    // specs/076-agent-v2-optimize/contracts/announcer-member.md §3).
    if (this.orchestrator.announcer === undefined) {
      await this.orchestrator.dispose().catch(() => undefined);
      throw new Error(
        "team materialization invariant violated: the announcer member is missing",
      );
    }
  }

  members(): readonly SceneMember[] {
    return this.orchestrator.snapshot().materialized ? SAOLEI_MEMBERS : [];
  }

  /**
   * The scene-local announce-only member read surface: the loop's retained
   * `announcer` accessor, kept off the neutral seam (the host derives
   * announce-only members from {@link members}). Its consumer is the
   * `entrySeam` helper of `projects/game/agent_v2/src/session.test.ts`, which
   * reads `entry.orchestrator.announcer` for the refresh-teardown assertion
   * ("detaches the announcer subscription with the entry teardown"); keeping
   * the scene-local read here lets the neutral `SceneOrchestration` seam carry
   * no announce-only accessor
   * (specs/080-host-main-sub-model/contracts/scene-registry.md §5;
   * specs/077-announcer-member-unification/contracts/host-member-parity.md §1).
   */
  get announcer(): AnnouncerMember | undefined {
    return this.orchestrator.announcer;
  }

  member(role: string): AgentHandle | undefined {
    return isAgentRole(role) ? this.orchestrator.member(role) : undefined;
  }

  teamEvents(): TeamEventStream {
    try {
      return this.orchestrator.teamEvents();
    } catch (err) {
      throw mapStateError(err);
    }
  }

  snapshot(): SceneSnapshot {
    const snapshot = this.orchestrator.snapshot();
    const lastError = snapshot.lastError;
    return {
      materialized: snapshot.materialized,
      active: snapshot.active,
      activation: snapshot.activation,
      paused: snapshot.paused,
      queued: snapshot.queued,
      failed: snapshot.failed,
      // The saolei workflow position is scene vocabulary and stays out of the
      // seam (specs/080-host-main-sub-model/contracts/scene-registry.md §2).
      lastError:
        lastError === null
          ? null
          : {
              message: lastError.message,
              member: lastError.member,
              code: lastError.code,
              origin: lastError.origin,
              detail: lastError.detail,
            },
    };
  }

  submit(text: string): void {
    try {
      this.orchestrator.submit(text);
    } catch (err) {
      throw mapStateError(err);
    }
  }

  cancel(): void {
    try {
      this.orchestrator.cancel();
    } catch (err) {
      throw mapStateError(err);
    }
  }

  async whenQuiescent(): Promise<void> {
    await this.orchestrator.whenQuiescent();
  }

  async dispose(): Promise<void> {
    await this.orchestrator.dispose();
  }
}

/**
 * Wrap an existing team orchestration in the scene-neutral seam. Exported as
 * the unit-test injection point so tests hand in a double instead of
 * constructing a real team orchestration (style/javascript.md Mock
 * convention).
 */
export function adaptSaoleiOrchestration(
  orchestrator: SaoleiTeamOrchestration,
  spec: SaoleiSceneData,
): SceneOrchestration {
  return new SaoleiSceneOrchestration(orchestrator, spec);
}

/**
 * Build the saolei orchestration for one scene table entry: a
 * `TeamOrchestrator` over the host-injected scene-neutral seams, wrapped in
 * the scene-neutral seam (specs/080-host-main-sub-model/contracts/
 * scene-registry.md §5).
 */
export function createSaoleiOrchestration(
  ctx: DshContext,
  deps: SceneRegistrationDeps,
  spec: SaoleiSceneData,
): SceneOrchestration {
  const orchestrator = new TeamOrchestrator(ctx, {
    compose: deps.compose,
    agents: deps.agents,
    logger: saoleiLogger(deps.logger),
    provider: deps.provider,
    ...(deps.team === undefined ? {} : { team: deps.team }),
  });
  return adaptSaoleiOrchestration(orchestrator, spec);
}
