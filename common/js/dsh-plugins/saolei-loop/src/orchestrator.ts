/**
 * TeamOrchestrator — the saolei team loop's orchestration state machine:
 * materialization, alternating activation (at most one member is driven),
 * queue-first digestion, structural continuation, game-end review triggers,
 * and cancel/resume. The behavior contract is
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §2; the state graph
 * and the player→planner / planner→player switching anchors are
 * specs/059-agent-v2-team-mode/data-model.md §5; the group-chat drive model
 * is survey/deepseek-harness-team-mode.md §4.4/§9.4.
 *
 * The orchestration layer synthesizes no drive input (FR-010): every drive
 * carries only the member's unconsumed team relays (`ctx.team.drain`) plus
 * the queued user messages. Materialization therefore does NOT drive: after
 * creating the members and registering the team the loop rests in the
 * initial planning activation, and the first turn starts from the first
 * user send handled by that activation (planner). The same input rule
 * covers structural continuation, the game-end review, queue digestion, and
 * cancel-resume.
 *
 * The host constructs one instance per team
 * (projects/game/agent_v2/src/session.ts): members are created once through
 * `ctx.agents.create`, registered through `ctx.team.register`, and every
 * collaborator (preset compose, agent registry, team service, desktop
 * bridge, planner memory prefetch, game-runtime mount) enters through
 * {@link TeamOrchestratorDeps} so unit tests inject doubles instead of
 * intercepting modules (style/javascript.md Mock convention).
 *
 * Driving consumes the official agent inbox: a drive batches its message set
 * into ONE turn (`inject` all but the last, `followup` the last — the
 * pre-step claim consumes next-step input plus one queued turn,
 * survey/deepseek-harness-team-mode.md §4.4 note 3), then awaits the
 * member's `agent/status` idle transition. Idle is exactly the switching
 * anchor's quiescence ("turn ended and no further turn is triggered"):
 * `running` covers consecutive queued turns and the turn-close checkpoint,
 * so idle means the whole drain interval settled
 * (https://unpkg.com/@deepseek-ai/dsh-agent@0.1.1-rc.2/README.md).
 *
 * gameEnded is read from the player's agent-scoped game runtime after each
 * player turn (`peekGameEvent`, common/js/dsh-plugins/saolei-loop/src/game/
 * runtime.ts): the runtime keeps the LATEST terminal record across a
 * restart-init, and the record is consumed only after its review drive
 * settles, so a failed attempt retries with the held relays instead of
 * losing the review.
 *
 * A pump step that throws (a fail-loud relay read, an alternation-invariant
 * violation, a rejected followup/inject) suspends auto-continuation and is
 * surfaced through the injected logger and the snapshot's
 * `failed`/`lastError` fields — never silently stalled.
 */

import type { Context } from "@deepseek-ai/cordis";
import type {
  Agent,
  AgentHandle,
  CreateAgentOptions,
} from "@deepseek-ai/dsh-agent";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import type { TeamHandle, TeamRegistration } from "@dominion/dsh-team";
import type { DesktopBridgeService } from "@dominion/dsh-desktop-bridge";

import { createAgentGameRuntime } from "./game/runtime.js";
import type { GameEventRecord } from "./game/runtime.js";

/** The two team roles; the orchestrator activates them in alternation. */
export type TeamRole = "player" | "planner";

/**
 * Which side of the workflow the orchestrator is working with:
 * `planning` = the initial planner activation (its first turn starts from
 * the first user send); `playing` = a player turn (or the player waiting for
 * input); `reviewing` = the game-end planner review. data-model.md §5.
 */
export type OrchestrationPhase = "planning" | "playing" | "reviewing";

/** Default provider route; the host composition's GLM Responses adapter. */
export const TEAM_PROVIDER = "glm-responses";

/**
 * Failure context handed to the injected orchestration logger: the team
 * session, the phase and member of the failed step (`member` is null when
 * the decision step itself failed), and the normalized error message.
 */
export interface OrchestrationFailureContext {
  readonly session: string;
  readonly phase: OrchestrationPhase;
  readonly member: TeamRole | null;
  readonly error: string;
}

/**
 * Failure reporter seam. The host injects its own logger (the deployment's
 * OTel-backed `@dominion/common-js-logs` face); the default console fallback
 * keeps a failed orchestration step observable even without one.
 */
export interface OrchestratorLogger {
  error(message: string, context: OrchestrationFailureContext): void;
}

/** Default roster summaries (one-line third-person duty index, R2 boundary). */
export const DEFAULT_MEMBER_SUMMARIES: Readonly<Record<TeamRole, string>> = {
  player: "执行扫雷操作，独占桌面控制",
  planner: "复盘对局与制定策略，不操作",
};

/**
 * Default failure reporter (hosts override through
 * {@link TeamOrchestratorDeps.logger}): a console line beats a silent stall.
 */
const DEFAULT_ORCHESTRATOR_LOGGER: OrchestratorLogger = {
  error(message, context) {
    console.error(message, context);
  },
};

/**
 * The preset-compose seam result: structurally the preset-authoring
 * `ComposeResult` (common/js/dsh-plugins/preset-authoring/src/index.ts), so
 * the host passes `ctx.presetAuthoring.compose` without this package taking
 * a dependency on the authoring plugin.
 */
export interface ComposedPreset {
  /** Resolved preset id snapshot for the creation meta (`meta.agentPreset`). */
  readonly agentPreset: string;
  /** Roster mount hook invoked inside the member's `agents.create` setup. */
  setup(agentCtx: Context): Promise<void> | void;
}

/** Resolve + compose one preset (production: `presetAuthoring.compose`). */
export type ComposePreset = (preset: string) => Promise<ComposedPreset>;

/**
 * The player-side terminal-event view (production:
 * `createAgentGameRuntime`'s `SaoleiGame`); the review trigger reads
 * `peekGameEvent()` after each player turn.
 */
export interface GameEventSource {
  peekGameEvent(): GameEventRecord | null;
}

/** Player-only game-runtime mount, run inside the member's setup. */
export type MountPlayerRuntime = (
  agent: Agent,
  sessionName: string,
) => GameEventSource | undefined;

/** The planner memory snapshot prefetch scope key (template, session). */
export interface PlannerMemoryScope {
  readonly template: string;
  readonly session: string;
}

/**
 * Planner memory prefetch seam — mandatory. The orchestration layer has no
 * compile or runtime dependency on the memory plugin: the host injects the
 * real `ctx.plannerMemory.load` (common/js/dsh-plugins/memory/src/) and a
 * rejection fails the whole materialization rollback — fail-loud
 * (contracts/dsh-plugins.md §2/§3).
 */
export type LoadPlannerMemory = (
  agentCtx: Context,
  scope: PlannerMemoryScope,
) => Promise<void>;

/** Narrow structural view of `ctx.agents` (registry creation face). */
export interface AgentCreationSeam {
  create(options: CreateAgentOptions): Promise<AgentHandle>;
}

/** Narrow structural view of `ctx.team` (the orchestration's consumption face). */
export interface TeamSeam {
  register(registration: TeamRegistration): TeamHandle;
  drain(member: AgentHandle): UserMessage[];
}

/** Collaborators and host-injected seams of one {@link TeamOrchestrator}. */
export interface TeamOrchestratorDeps {
  /** Resolve + compose seam (production: `ctx.presetAuthoring.compose`). */
  readonly compose: ComposePreset;
  /** Agent registry seam; defaults to `ctx.agents`. */
  readonly agents?: AgentCreationSeam;
  /** Team service seam; defaults to `ctx.team`. */
  readonly team?: TeamSeam;
  /** Desktop bridge backing the default player-runtime mount; defaults to `ctx.desktopBridge`. */
  readonly desktopBridge?: DesktopBridgeService;
  /** Player game-runtime mount; defaults to `createAgentGameRuntime(agent, desktopBridge)`. */
  readonly mountPlayerRuntime?: MountPlayerRuntime;
  /**
   * Planner memory prefetch — REQUIRED. The host binds the real
   * `ctx.plannerMemory.load` (the memory plugin's host-row service face),
   * so a planner member is never materialized without its snapshot
   * prefetch; the seam stays injectable for unit tests
   * (specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §2/§3).
   */
  readonly loadPlannerMemory: LoadPlannerMemory;
  /** Failure reporter; defaults to a console-backed reporter. */
  readonly logger?: OrchestratorLogger;
  /** Provider route; defaults to {@link TEAM_PROVIDER}. */
  readonly provider?: string;
  /** Session `cwd` meta; defaults to `process.cwd()`. */
  readonly cwd?: string;
}

/** One member's materialization input. */
export interface TeamMemberOptions {
  /** Preset reference handed to the compose seam. */
  readonly preset: string;
  /** Model id; empty/undefined leaves the provider/adapter default in control. */
  readonly model?: string;
}

/** The materialization input of one team (host: UpdateTeam). */
export interface TeamMaterializeOptions {
  /** Team session resource name (`templates/{template}/sessions/{session}`). */
  readonly session: string;
  /**
   * The planner memory prefetch scope key halves: the business template and
   * the session ID (`templates/{template}/sessions/{session}/memories/{memory}`).
   * This is NOT the full session resource name — the memory plugin builds the
   * parent resource from these halves (the memory service resource model),
   * so passing `session` here doubles the prefix (T023 wiring, specs/059-agent-v2-team-mode/tasks.md).
   */
  readonly memoryScope: PlannerMemoryScope;
  /** Team goal rendered into the team section. */
  readonly goal: string;
  /** Generalized correlation key relayed onto broadcasts (team never interprets it). */
  readonly context?: string;
  readonly player: TeamMemberOptions;
  readonly planner: TeamMemberOptions;
  /** Member dsh session id override; defaults to `${session}/player`. */
  readonly playerSessionId?: string;
  /** Member dsh session id override; defaults to `${session}/planner`. */
  readonly plannerSessionId?: string;
  /** Roster summary overrides (default {@link DEFAULT_MEMBER_SUMMARIES}). */
  readonly summaries?: {
    readonly player?: string;
    readonly planner?: string;
  };
}

/** One accepted user message: queued when a member turn is in flight. */
export interface SubmitResult {
  /** True when the current member's turn is in flight and the message waits. */
  readonly queued: boolean;
  /** 1-based FIFO position; present only when `queued`. */
  readonly position?: number;
  /** The stable identity of the created user message (host stream bookkeeping). */
  readonly messageId: string;
}

/**
 * The user-message entry result: `dropped` are the messages that were waiting
 * in the FIFO when the cancel landed. They are not driven (FR-017: 排队消息
 * 落地为历史且不触发新驱动); the host lands them as history.
 */
export interface CancelResult {
  readonly dropped: readonly UserMessage[];
}

/** The last failed orchestration step, exposed through {@link OrchestratorSnapshot}. */
export interface OrchestratorFailure {
  /** Normalized error message of the failed step. */
  readonly message: string;
  /** The member whose drive failed; null when the decision step failed. */
  readonly member: TeamRole | null;
  /** The workflow phase at failure time. */
  readonly phase: OrchestrationPhase;
}

/** Read-only orchestration view for the host (status/queue/failure presentation). */
export interface OrchestratorSnapshot {
  readonly materialized: boolean;
  readonly phase: OrchestrationPhase | null;
  /** The member whose turn is in flight, or null. */
  readonly active: TeamRole | null;
  /**
   * The member the next structural/queued input belongs to (activation): the
   * current member while a turn is in flight, and the next input's owner
   * while quiescent. Materialization rests in the initial planning
   * activation (planner); cancel/pause does not change it. The host's team
   * view merges it with {@link active} into one active-member value
   * (specs/060-agent-v2-team-optimize/contracts/team-api.md §1).
   */
  readonly activation: TeamRole;
  /** Auto-continuation suspended by a cancel until the next user message. */
  readonly paused: boolean;
  readonly queued: number;
  /** True while the last pump step failed (auto-continuation suspended). */
  readonly failed: boolean;
  /** The last pump failure; cleared by the next successful drive. */
  readonly lastError: OrchestratorFailure | null;
}

/** Stable state-error codes the host maps onto AIP statuses. */
export type OrchestratorStateErrorCode =
  | "UNMATERIALIZED"
  | "ALREADY_MATERIALIZED"
  | "DISPOSED";

/** A caller-visible orchestration state error. */
export class OrchestratorStateError extends Error {
  readonly code: OrchestratorStateErrorCode;

  constructor(code: OrchestratorStateErrorCode, message: string) {
    super(message);
    this.name = "OrchestratorStateError";
    this.code = code;
  }
}

interface IdleWaiter {
  readonly resolve: () => void;
}

interface MemberRuntime {
  readonly role: TeamRole;
  readonly handle: AgentHandle;
  readonly agent: Agent;
  /** Player-only terminal-event view captured at setup time. */
  game: GameEventSource | undefined;
  offStatus: () => void;
  readonly idleWaiters: IdleWaiter[];
}

interface PumpStep {
  readonly member: MemberRuntime;
  readonly messages: readonly UserMessage[];
  /** The terminal record this step reviews; consumed after the drive settles. */
  readonly review?: GameEventRecord;
}

/**
 * A review transition whose drive has not settled. The drained relays are
 * held here so a failed attempt can retry: the team drain already consumed
 * the anchors, so re-reading `ctx.team.drain` would find nothing.
 */
interface PendingReview {
  readonly event: GameEventRecord;
  readonly messages: readonly UserMessage[];
}

/**
 * The per-team orchestration state machine. See the file header for the
 * contract references; member ownership (create → dispose) also lives here,
 * so the host's refresh/destroy path is a single {@link dispose}.
 */
export class TeamOrchestrator {
  private members: Readonly<Record<TeamRole, MemberRuntime>> | null = null;
  private teamRegistration: TeamHandle | null = null;
  /** The materialized team's session resource name (logging context). */
  private session: string | null = null;
  private phase: OrchestrationPhase = "planning";
  /** The member the next structural/queued input belongs to (activation). */
  private current: TeamRole = "planner";
  private readonly queue: UserMessage[] = [];
  /** The member whose turn is in flight; null when quiescent. */
  private drivingMember: MemberRuntime | null = null;
  private paused = false;
  private disposed = false;
  /** The terminal record already handed to a settled planner review. */
  private reviewedGameEvent: GameEventRecord | null = null;
  /** The review transition awaiting its settling drive (retry-held input). */
  private pendingReview: PendingReview | null = null;
  /** The last failed pump step; cleared by the next successful drive. */
  private lastError: OrchestratorFailure | null = null;
  /** The serialized pump run; null when idle. */
  private pump: Promise<void> | null = null;

  constructor(
    private readonly ctx: Context,
    private readonly deps: TeamOrchestratorDeps,
  ) {}

  /**
   * Materialize the team: create both members (compose mount + player game
   * runtime + planner memory prefetch) and register with `ctx.team`. The
   * orchestrator then rests in the initial planning activation — it does NOT
   * drive: the first turn starts from the first user send (FR-010: the loop
   * synthesizes no drive message). Any failure — compose, a member setup, or
   * the team registration — rolls every created member back and leaves the
   * orchestrator unmaterialized, so the caller can retry (no half-materialized
   * team; contracts/dsh-plugins.md §2).
   */
  async materialize(options: TeamMaterializeOptions): Promise<void> {
    if (this.disposed) {
      throw new OrchestratorStateError("DISPOSED", "orchestrator is disposed");
    }
    if (this.members !== null) {
      throw new OrchestratorStateError(
        "ALREADY_MATERIALIZED",
        "team is already materialized",
      );
    }

    const created: MemberRuntime[] = [];
    try {
      const player = await this.createMember(
        "player",
        options.player,
        options,
        options.playerSessionId ?? `${options.session}/player`,
      );
      created.push(player);
      const planner = await this.createMember(
        "planner",
        options.planner,
        options,
        options.plannerSessionId ?? `${options.session}/planner`,
      );
      created.push(planner);

      const registration: TeamRegistration = {
        goal: options.goal,
        context: options.context,
        members: [
          {
            agent: player.handle,
            role: "player",
            summary: options.summaries?.player ?? DEFAULT_MEMBER_SUMMARIES.player,
          },
          {
            agent: planner.handle,
            role: "planner",
            summary: options.summaries?.planner ?? DEFAULT_MEMBER_SUMMARIES.planner,
          },
        ],
      };
      this.teamRegistration = this.team().register(registration);
      this.members = { player, planner };
      this.session = options.session;
      this.phase = "planning";
      this.current = "planner";
      this.pendingReview = null;
      this.reviewedGameEvent = null;
      this.lastError = null;
    } catch (err) {
      await this.rollback(created);
      throw err;
    }
  }

  /**
   * Accept one user message (FR-011): queued at the team FIFO when the
   * current member's turn is in flight, otherwise handed to the current
   * activation immediately. A send also lifts a cancel pause (FR-017).
   */
  submit(text: string): SubmitResult {
    if (this.disposed) {
      throw new OrchestratorStateError("DISPOSED", "orchestrator is disposed");
    }
    if (this.members === null) {
      throw new OrchestratorStateError(
        "UNMATERIALIZED",
        "team is not materialized; materialize before sending",
      );
    }
    const message = createUserMessage({
      content: [{ type: "text", text }],
      source: { kind: "user" },
    });
    this.queue.push(message);
    this.paused = false;
    const queued = this.drivingMember !== null;
    const position = queued ? this.queue.length : undefined;
    this.requestPump();
    return {
      queued,
      ...(position === undefined ? {} : { position }),
      messageId: String(message.id),
    };
  }

  /**
   * Cancel (FR-017, idempotent): terminate the in-flight member turn,
   * suspend auto-continuation, and drain the waiting FIFO so those messages
   * land as history without triggering a turn. A later {@link submit}
   * resumes the loop with the current activation.
   */
  cancel(): CancelResult {
    if (this.disposed) {
      return { dropped: [] };
    }
    if (this.members === null) {
      throw new OrchestratorStateError(
        "UNMATERIALIZED",
        "team is not materialized; there is nothing to cancel",
      );
    }
    const dropped = this.queue.splice(0);
    this.paused = true;
    const active = this.drivingMember;
    if (active !== null) {
      active.agent.cancel({ kind: "user" });
    }
    return { dropped };
  }

  /**
   * Terminate the in-flight turn, void the queue, and release every member.
   * The orchestrator owns the member handles it created, so the host's
   * refresh/destroy path is exactly this call (idempotent).
   */
  async dispose(): Promise<void> {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    this.paused = true;
    this.queue.length = 0;
    this.pendingReview = null;
    this.lastError = null;
    const active = this.drivingMember;
    if (active !== null) {
      active.agent.cancel({ kind: "disposed" });
    }
    await this.whenQuiescent();

    const registration = this.teamRegistration;
    this.teamRegistration = null;
    registration?.dispose();

    const members = this.members;
    this.members = null;
    this.session = null;
    if (members === null) {
      return;
    }
    const runtimes = [members.planner, members.player];
    for (const runtime of runtimes) {
      runtime.offStatus();
    }
    const results = await Promise.allSettled(
      runtimes.map((runtime) => runtime.handle.dispose()),
    );
    const failure = results.find(
      (result): result is PromiseRejectedResult => result.status === "rejected",
    );
    if (failure !== undefined) {
      throw failure.reason;
    }
  }

  /** Resolve when the pump has no scheduled step left (all turns settled). */
  async whenQuiescent(): Promise<void> {
    let pump = this.pump;
    while (pump !== null) {
      await pump;
      pump = this.pump;
    }
  }

  /** The live member handle (the system-prompt read surface of specs/059-agent-v2-team-mode/tasks.md T032), or undefined. */
  member(role: TeamRole): AgentHandle | undefined {
    return this.members?.[role].handle;
  }

  /** The team's live state snapshot (status/queue/failure presentation). */
  snapshot(): OrchestratorSnapshot {
    return {
      materialized: this.members !== null,
      phase: this.members === null ? null : this.phase,
      active: this.drivingMember?.role ?? null,
      activation: this.current,
      paused: this.paused,
      queued: this.queue.length,
      failed: this.lastError !== null,
      lastError: this.lastError,
    };
  }

  /** Create one member with the full setup chain (compose → role extras). */
  private async createMember(
    role: TeamRole,
    member: TeamMemberOptions,
    options: TeamMaterializeOptions,
    sessionId: string,
  ): Promise<MemberRuntime> {
    const composed = await this.deps.compose(member.preset);
    let game: GameEventSource | undefined;
    const handle = await this.agents().create({
      sessionId: sessionId as CreateAgentOptions["sessionId"],
      meta: {
        cwd: this.deps.cwd ?? process.cwd(),
        agentPreset: composed.agentPreset,
      },
      agentOptions: {
        provider: this.deps.provider ?? TEAM_PROVIDER,
        ...(member.model === undefined || member.model === ""
          ? {}
          : { model: member.model }),
      },
      setup: async (agentCtx) => {
        await composed.setup(agentCtx);
        if (role === "player") {
          // The game runtime dispatches through the desktop-bridge connection
          // registered for the GAME session resource name; the member's dsh
          // session id is namespaced (`{session}/player`) and is not the
          // bridge key (specs/059-agent-v2-team-mode/data-model.md §3).
          game = this.mountPlayerRuntime()(agentCtx.agent as Agent, options.session);
          return;
        }
        await this.loadPlannerMemory(agentCtx, options.memoryScope);
      },
    });

    const runtime: MemberRuntime = {
      role,
      handle,
      agent: handle.agent,
      game,
      offStatus: () => {},
      idleWaiters: [],
    };
    runtime.offStatus = handle.agent.ctx.on("agent/status", (payload) => {
      if (payload.agent === handle.agent && payload.status === "idle") {
        this.settleIdle(runtime);
      }
    });
    return runtime;
  }

  /** The agent-creation seam (host override, else the composition's registry). */
  private agents(): AgentCreationSeam {
    return this.deps.agents ?? this.ctx.agents;
  }

  /** The team-service seam (host override, else `ctx.team`). */
  private team(): TeamSeam {
    return this.deps.team ?? this.ctx.team;
  }

  /** The player game-runtime mount (host override, else the desktop bridge). */
  private mountPlayerRuntime(): MountPlayerRuntime {
    if (this.deps.mountPlayerRuntime !== undefined) {
      return this.deps.mountPlayerRuntime;
    }
    return (agent, sessionName) => {
      const desktopBridge =
        this.deps.desktopBridge ?? this.ctx.get("desktopBridge");
      if (desktopBridge === undefined) {
        throw new Error(
          "saolei-loop orchestrator: player game runtime needs the desktop-bridge service",
        );
      }
      return createAgentGameRuntime(agent, desktopBridge, sessionName);
    };
  }

  /**
   * Planner memory prefetch (fail-loud): the host-injected
   * `ctx.plannerMemory.load`. A rejection propagates out of the member
   * setup, which rolls the whole materialization back
   * (specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §3 item 3).
   */
  private async loadPlannerMemory(
    agentCtx: Context,
    scope: PlannerMemoryScope,
  ): Promise<void> {
    await this.deps.loadPlannerMemory(agentCtx, scope);
  }

  /** Roll a failed materialization back: drop created members, no residue. */
  private async rollback(created: readonly MemberRuntime[]): Promise<void> {
    const registration = this.teamRegistration;
    this.teamRegistration = null;
    registration?.dispose();
    // A teardown failure must not mask the original materialization error and
    // cannot restore a half-built team, so the caller sees the original cause.
    await Promise.allSettled(
      [...created].reverse().map((runtime) => {
        runtime.offStatus();
        return runtime.handle.dispose();
      }),
    );
  }

  /** Queue a pump run; the loop always re-reads the state after each drive. */
  private requestPump(): void {
    if (this.disposed || this.pump !== null) {
      return;
    }
    const pump = this.runPump();
    this.pump = pump;
    void pump.then(() => {
      if (this.pump === pump) {
        this.pump = null;
      }
    });
  }

  /**
   * The serialized pump: each iteration takes exactly one step (a queued
   * user message or a structural continuation) and awaits its turn's idle
   * before re-evaluating. This is the "at most one member driven at a time"
   * invariant's enforcement point (FR-011). A failing step suspends
   * auto-continuation and is reported through {@link fail} — never silently
   * dropped: the loop has no other owner to notice a stalled team.
   */
  private async runPump(): Promise<void> {
    while (!this.disposed && !this.paused) {
      let member: TeamRole | null = null;
      try {
        const step = this.nextStep();
        if (step === null) {
          return;
        }
        member = step.member.role;
        await this.drive(step.member, step.messages);
        this.lastError = null;
        if (step.review !== undefined) {
          // The review drive settled: only now is the terminal record consumed.
          this.reviewedGameEvent = step.review;
          this.pendingReview = null;
        }
      } catch (err) {
        this.fail(member, err);
        return;
      }
    }
  }

  /**
   * Suspend auto-continuation after a failed step and surface it: the failure
   * goes to the host-injected logger (console fallback) and into
   * {@link OrchestratorSnapshot} (`failed`/`lastError`) so the host can map an
   * INTERNAL/turn error instead of a silent stall. The next successful drive
   * clears it.
   */
  private fail(member: TeamRole | null, err: unknown): void {
    const message = err instanceof Error ? err.message : String(err);
    this.lastError = { message, member, phase: this.phase };
    this.paused = true;
    this.logger().error(
      "saolei-loop orchestration step failed; auto-continuation suspended",
      {
        session: this.session ?? "",
        phase: this.phase,
        member,
        error: message,
      },
    );
  }

  /** The host's failure reporter, or the console fallback. */
  private logger(): OrchestratorLogger {
    return this.deps.logger ?? DEFAULT_ORCHESTRATOR_LOGGER;
  }

  /**
   * The state-machine transition decision (data-model.md §5 切换锚点):
   *
   * 1. queued user messages first, consumed by the current activation
   *    together with its unconsumed relays (消化优先于切换); the first user
   *    send after materialization is served here with an empty relay set;
   * 2. an unsettled review transition retries with its held relays;
   * 3. planner quiescent (planning/reviewing) → structurally drive player;
   * 4. player quiescent: an unreviewed gameEnded → start the planner review
   *    (the record is consumed only after the drive settles); otherwise
   *    structurally continue the player with newly relayed broadcasts. An
   *    empty delivery set cannot form a model request, so the pump rests in
   *    the current activation until new input arrives.
   */
  private nextStep(): PumpStep | null {
    if (this.disposed || this.paused) {
      return null;
    }
    const members = this.members;
    if (members === null) {
      return null;
    }

    if (this.queue.length > 0) {
      const relays = this.drain(members[this.current]);
      const message = this.queue.shift() as UserMessage;
      return {
        member: members[this.current],
        messages: [...relays, message],
      };
    }

    if (this.pendingReview !== null) {
      return {
        member: members.planner,
        messages: this.pendingReview.messages,
        review: this.pendingReview.event,
      };
    }

    if (this.phase === "planning" || this.phase === "reviewing") {
      this.phase = "playing";
      this.current = "player";
      const relays = this.drain(members.player);
      return relays.length === 0 ? null : { member: members.player, messages: relays };
    }

    const event = members.player.game?.peekGameEvent() ?? null;
    if (event !== null && event !== this.reviewedGameEvent) {
      const relays = this.drain(members.planner);
      if (relays.length > 0) {
        this.pendingReview = { event, messages: relays };
        this.phase = "reviewing";
        this.current = "planner";
        return { member: members.planner, messages: relays, review: event };
      }
    }
    const relays = this.drain(members.player);
    return relays.length === 0 ? null : { member: members.player, messages: relays };
  }

  /** Consume one member's unconsumed broadcasts (team-internal read path). */
  private drain(member: MemberRuntime): UserMessage[] {
    return this.team().drain(member.handle);
  }

  /**
   * Run one member turn: batch the whole message set into one pre-step claim
   * (`inject` all but the last, `followup` the last) and await the member's
   * idle transition. The alternation guard rejects a second concurrent drive.
   */
  private async drive(
    member: MemberRuntime,
    messages: readonly UserMessage[],
  ): Promise<void> {
    if (messages.length === 0) {
      throw new Error(
        "saolei-loop orchestrator: an empty message set cannot start a turn",
      );
    }
    if (this.drivingMember !== null) {
      throw new Error(
        "saolei-loop orchestrator invariant violated: a member turn is already in flight",
      );
    }
    if (member.agent.status !== "idle") {
      throw new Error(
        `saolei-loop orchestrator invariant violated: ${member.role} is not quiescent`,
      );
    }
    this.drivingMember = member;
    try {
      const idle = this.waitForIdle(member);
      for (const message of messages.slice(0, -1)) {
        member.agent.inject(message);
      }
      member.agent.followup(
        messages[messages.length - 1] as UserMessage,
      );
      await idle;
    } finally {
      this.drivingMember = null;
    }
  }

  /** Register an idle waiter BEFORE the waking followup (no lost transition). */
  private waitForIdle(member: MemberRuntime): Promise<void> {
    return new Promise<void>((resolve) => {
      member.idleWaiters.push({ resolve });
    });
  }

  /** Resolve every waiter on the member's idle transition. */
  private settleIdle(member: MemberRuntime): void {
    const waiters = member.idleWaiters.splice(0);
    for (const waiter of waiters) {
      waiter.resolve();
    }
  }
}
