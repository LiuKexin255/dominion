/**
 * TeamOrchestrator — the saolei team loop's execution layer over the flow
 * layer (flow.ts): materialization, activation landing, the serialized pump
 * that consumes `start(activity)` directives, member-domain delivery, error
 * freezing, and cancel/dispose. The contracts are
 * specs/076-agent-v2-optimize/contracts/flow-orchestration.md（分层与指令 /
 * 投递 / outcome 与冻结 / 错误契约 / 操作路径 / 宿主联动）and
 * specs/076-agent-v2-optimize/data-model.md §2/§5; the design semantics are
 * survey/saolei-member-switch-flow.md §5/§6/§8 (rulings 12–22). The baseline
 * behavior contracts it keeps are
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §2 (drive timing and
 * input: at most one member driven, no synthesized drive input),
 * specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md
 * §2 (the failure origin binary), specs/068-wiki-round-pause/contracts/
 * saolei-loop-round-stop.md (the natural stop), and
 * specs/061-team-queue-steer/contracts/orchestrator-input.md §1 (mid-turn
 * delivery, landed flush).
 *
 * The orchestrator holds no workflow decision: every transition is the flow
 * layer's edge judgment over the facts. It owns only execution concerns —
 * activation landing (presented through {@link OrchestratorSnapshot}),
 * driving (delegated to the member adapters), inbox-landed collection for
 * cancel, and the failure facts the host presents. There is no orchestration
 * FIFO and no queue gate: a user message either steers into the active
 * member's inbox or opens a turn immediately, both inside the member domain
 * (contracts/flow-orchestration.md §3).
 *
 * The host constructs one instance per team
 * (projects/game/agent_v2/src/session.ts): members are created once through
 * `ctx.agents.create`, registered through `ctx.team.register`, and every
 * collaborator (preset compose, agent registry, team service, desktop
 * bridge, game-runtime construction) enters through {@link TeamOrchestratorDeps}
 * so unit tests inject doubles instead of intercepting modules
 * (style/javascript.md Mock convention).
 */

import type { Context } from "@deepseek-ai/cordis";
import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import type { PromptSection } from "@deepseek-ai/dsh-system-prompt";
import type {
  AgentHandle,
  CreateAgentOptions,
} from "@deepseek-ai/dsh-agent";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import { agentMemberSource } from "@dominion/dsh-team";
import type {
  TeamEventStream,
  TeamHandle,
  TeamMemberSource,
  TeamRegistration,
} from "@dominion/dsh-team";
import type { DesktopBridgeService } from "@dominion/dsh-desktop-bridge";
import type {
  GameEventRecord,
  GameRuntime,
  SaoleiFoundation,
} from "@dominion/dsh-saolei";

import { AgentMember } from "./agent-member.js";
import {
  ANNOUNCER_MEMBER_ROLE,
  ANNOUNCER_MEMBER_SUMMARY,
  AnnouncerMember,
} from "./announcer.js";
import { createSaoleiFlow } from "./flow.js";
import type { Flow, SaoleiActivityId, SaoleiMemberId } from "./flow.js";
import type {
  FailureDetail,
  LoopErrorCode,
  Member,
  MemberExecution,
  MemberId,
  Outcome,
} from "./member.js";

/**
 * The member roles the orchestration presents — the snapshot `member`/
 * `active`/`activation` values and the roster roles. The announce-only
 * announcer is a presented member (roster + activation presentation) without
 * an agent runtime (specs/076-agent-v2-optimize/contracts/announcer-member.md
 * §3).
 */
export type TeamRole = "player" | "planner" | "announcer";

/**
 * The agent-backed member roles: the members that own an `AgentHandle`/`Agent`
 * (specs/076-agent-v2-optimize/contracts/announcer-member.md §3 item 5).
 */
export type AgentTeamRole = "player" | "planner";

/**
 * Which side of the workflow the orchestrator is working with: `planning` =
 * the initial planner activation; `playing` = a player turn or the resting
 * player; `reviewing` = the game-end planner review
 * (specs/076-agent-v2-optimize/data-model.md §5).
 */
export type OrchestrationPhase = "planning" | "playing" | "reviewing";

/** Default provider route; the host composition's GLM Responses adapter. */
export const TEAM_PROVIDER = "glm-responses";

/**
 * Failure context handed to the injected orchestration logger: the team
 * session, the phase and member of the failed step (`member` is null when
 * the decision step itself failed), the loop failure code
 * (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §5), and the
 * normalized error message.
 */
export interface OrchestrationFailureContext {
  readonly session: string;
  readonly phase: OrchestrationPhase;
  readonly member: TeamRole | null;
  /** Loop failure code (specs/076-agent-v2-optimize/data-model.md §3.3). */
  readonly code: LoopErrorCode;
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
  announcer: ANNOUNCER_MEMBER_SUMMARY,
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
 * Planner memory scope key (template, preset): the business template and the
 * materialization source planner preset id. The orchestration derives it and
 * passes it EXPLICITLY to `memoryMount.build(scope)` — the capability library
 * has no scenario knowledge and never self-derives a scope
 * (specs/074-dsh-grpc-runtime/spec.md FR-019). The type is structurally
 * compatible with the memory library's `MemoryScope`, so no cross-package
 * compile dependency is needed.
 */
export interface PlannerMemoryScope {
  readonly template: string;
  readonly preset: string;
}

/**
 * The static capability unit a provider exposes under the `saoleiTools`
 * provide face (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §3.1):
 * the provider's tool definitions and prompt sections, registered into the
 * member scope together. Declared structurally so the orchestration has no
 * compile dependency on the provider package.
 */
export interface SaoleiToolsUnit {
  readonly tools: readonly ToolDefinition[];
  readonly sections: readonly PromptSection[];
}

/** One agent-scoped face registration: the isolate label and the face value. */
export interface MemoryFaceRegistration {
  readonly key: string;
  readonly value: unknown;
}

/**
 * The atomic memory capability unit a `memoryMount.build(scope)` produces
 * (specs/074-dsh-grpc-runtime/contracts/memory-library.md §1): faces, tools,
 * sections and the fail-loud prefetch are mounted together.
 */
export interface MemoryCapabilities {
  readonly faces: readonly MemoryFaceRegistration[];
  readonly tools: readonly ToolDefinition[];
  readonly sections: readonly PromptSection[];
  /** Fail-loud prefetch: awaited in the member setup, rejects on failure. */
  readonly ready: () => Promise<void>;
}

/**
 * The per-member factory a provider exposes under the `memoryMount` provide
 * face. `build` receives the scope the orchestration derived; the provider
 * holds the deployment connection and the domain library.
 */
export interface MemoryMount {
  build(scope: PlannerMemoryScope): MemoryCapabilities;
}

/** Narrow structural view of `ctx.agents` (registry creation face). */
export interface AgentCreationSeam {
  create(options: CreateAgentOptions): Promise<AgentHandle>;
}

/** Narrow structural view of `ctx.team` (the orchestration's consumption face). */
export interface TeamSeam {
  register(registration: TeamRegistration): TeamHandle;
  drain(member: TeamMemberSource): UserMessage[];
}

/** Collaborators and host-injected seams of one {@link TeamOrchestrator}. */
export interface TeamOrchestratorDeps {
  /** Resolve + compose seam (production: `ctx.presetAuthoring.compose`). */
  readonly compose: ComposePreset;
  /** Agent registry seam; defaults to `ctx.agents`. */
  readonly agents?: AgentCreationSeam;
  /** Team service seam; defaults to `ctx.team`. */
  readonly team?: TeamSeam;
  /** Desktop bridge backing the game-runtime construction; defaults to
   * `ctx.desktopBridge`. */
  readonly desktopBridge?: DesktopBridgeService;
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
  /**
   * Provider route this member's model belongs to; empty/undefined falls back
   * to the team-level provider (specs/063-llm-reliability-opencode-go/
   * contracts/model-selection.md §3).
   */
  readonly provider?: string;
  /** Bare model id; empty/undefined leaves the provider/adapter default in control. */
  readonly model?: string;
}

/** The materialization input of one team (host: UpdateTeam). */
export interface TeamMaterializeOptions {
  /** Team session resource name (`templates/{template}/sessions/{session}`). */
  readonly session: string;
  /**
   * The planner memory scope key halves: the business template and the
   * materialization source planner preset id
   * (`templates/{template}/presets/{preset}/memories/{memory}`). The memory
   * capability library builds the parent resource from these halves, so they
   * stay bare ids rather than full resource names
   * (specs/071-memory-preset-scope/contracts/dsh-plugins.md §1/§4). The
   * orchestration passes this scope explicitly to `memoryMount.build(scope)`
   * (specs/074-dsh-grpc-runtime/spec.md FR-019).
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

/** One accepted user message: steered when a member turn is in flight. */
export interface SubmitResult {
  /** True when the active member's turn is in flight and the message steers. */
  readonly queued: boolean;
  /** 1-based inbox position; present only when `queued`. */
  readonly position?: number;
  /** The stable identity of the created user message (host stream bookkeeping). */
  readonly messageId: string;
}

/**
 * The user-message entry result: `dropped` are the messages that were
 * unclaimed in the member inbox when the cancel landed. They are not driven
 * (FR-108: 未消费输入落地为历史且不触发新驱动); the host lands them as
 * history.
 */
export interface CancelResult {
  readonly dropped: readonly UserMessage[];
}

/**
 * Where a failed orchestration step originated: the driven member's turn
 * (a band-visible LLM failure observed through `agent/error`) or the
 * orchestration layer itself (a thrown step). The session layer maps the
 * origin onto the stream settlement (clean end vs INTERNAL) —
 * specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §2.
 */
export type OrchestratorFailureOrigin = "member-turn" | "orchestration";

/** The last failed orchestration step, exposed through {@link OrchestratorSnapshot}. */
export interface OrchestratorFailure {
  /** Normalized error message of the failed step. */
  readonly message: string;
  /** The member whose drive failed; null when the decision step failed. */
  readonly member: TeamRole | null;
  /** The workflow phase at failure time. */
  readonly phase: OrchestrationPhase;
  /** Loop failure code (specs/076-agent-v2-optimize/data-model.md §3.3). */
  readonly code: LoopErrorCode;
  /**
   * Failure origin: the host session layer's stream-settlement discriminant —
   * a failed member turn ends the streams cleanly, an orchestration-layer
   * failure closes them with an INTERNAL error
   * (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §2).
   */
  readonly origin: OrchestratorFailureOrigin;
  /** The engine code (when any) plus the message, transported for presentation. */
  readonly detail: FailureDetail;
}

/** Read-only orchestration view for the host (status/queue/failure presentation). */
export interface OrchestratorSnapshot {
  readonly materialized: boolean;
  readonly phase: OrchestrationPhase | null;
  /** The member whose turn is in flight, or null. */
  readonly active: TeamRole | null;
  /**
   * The member the current activity's activation belongs to. Materialization
   * rests in the initial `plan` activity (planner); cancel does not change it
   * (specs/060-agent-v2-team-optimize/contracts/team-api.md §1).
   */
  readonly activation: TeamRole;
  /** Whether the flow is frozen (a failed or aborted activity). */
  readonly paused: boolean;
  /**
   * The active member's delivered-but-unconsumed input: the steered-in-flight
   * count plus the inbox pending state
   * (specs/061-team-queue-steer/research.md R4; landed is not counted, R5).
   */
  readonly queued: number;
  /** True while the last drive failed (auto-continuation suspended). */
  readonly failed: boolean;
  /** The last drive failure; cleared by the next successful drive. */
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

/**
 * One registered member: the flow member, its execution seam, and — for
 * agent-backed members only — the agent handle and team source. The
 * announce-only announcer carries neither (specs/076-agent-v2-optimize/
 * contracts/announcer-member.md §3 item 3).
 */
interface MemberBinding {
  readonly id: MemberId;
  readonly member: Member;
  readonly execution: MemberExecution;
  readonly handle?: AgentHandle;
  readonly source?: TeamMemberSource;
}

/** The member ids in presentation order (the busy-member scan). */
const MEMBER_IDS: readonly MemberId[] = ["player", "planner", "announcer"];

/** The workflow phase one activity maps onto (data-model.md §5). */
export function phaseOfActivity(position: SaoleiActivityId): OrchestrationPhase {
  switch (position) {
    case "plan":
      return "planning";
    case "review":
      return "reviewing";
    case "play":
    case "rest":
    case "announce":
      return "playing";
  }
}

/**
 * The per-team execution layer. See the file header for the contract
 * references; member ownership (create → dispose) also lives here, so the
 * host's refresh/destroy path is a single {@link dispose}.
 */
export class TeamOrchestrator {
  private members: Readonly<Record<MemberId, MemberBinding>> | null = null;
  private teamRegistration: TeamHandle | null = null;
  /**
   * The team's game runtime: built at materialization through the foundation
   * face and owned by this orchestrator (lifecycle = the materialization
   * lifetime; specs/075-saolei-game-foundation/contracts/team-loop-assembly.md
   * §2). The player member only receives it as an agent-scoped handle.
   */
  private game: GameRuntime | null = null;
  /** The announce-only announcer member; non-null exactly while materialized. */
  private announcerMember: AnnouncerMember | null = null;
  /** The flow layer's state object; null when unmaterialized. */
  private flow: Flow<SaoleiActivityId, SaoleiMemberId> | null = null;
  /** The materialized team's session resource name (logging context). */
  private session: string | null = null;
  /** Cancel-landed messages awaiting the next turn start's flush (061 R5). */
  private readonly landed: UserMessage[] = [];
  /** The last drive failure; cleared by the next successful drive. */
  private lastError: OrchestratorFailure | null = null;
  /** The serialized pump run; null when no pump is scheduled. */
  private pump: Promise<void> | null = null;
  /** A delivery-started turn's outcome is awaited at the pump's next iteration. */
  private externalPending = false;
  /** The outcome of the last flow-issued drive, fed to the decision entry. */
  private pendingOutcome: Outcome | null = null;
  private disposed = false;

  constructor(
    private readonly ctx: Context,
    private readonly deps: TeamOrchestratorDeps,
  ) {}

  /**
   * Materialize the team: create both members (compose mount + player game
   * runtime + planner memory prefetch), build the member adapters and the
   * flow, and register with `ctx.team`. The orchestrator then rests in the
   * initial `plan` activity — it does NOT drive: the first turn starts from
   * the first user send (FR-010: the loop synthesizes no drive message). Any
   * failure — compose, a member setup, or the team registration — rolls every
   * created member back and leaves the orchestrator unmaterialized, so the
   * caller can retry (no half-materialized team; contracts/dsh-plugins.md §2).
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

    const created: MemberBinding[] = [];
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

      // The announce-only announcer member: it carries the terminal game
      // summary into the group chat through the same member-message-source
      // closure as the two agent members (specs/065-agent-v2-team-refine/
      // contracts/game-stats-broadcast.md §3 item 1).
      const announcer = new AnnouncerMember(options.session, {
        peekTerminal: () => this.peekTerminal(),
      });
      const registration: TeamRegistration = {
        goal: options.goal,
        context: options.context,
        members: [
          {
            source: player.source as TeamMemberSource,
            role: "player",
            summary: options.summaries?.player ?? DEFAULT_MEMBER_SUMMARIES.player,
          },
          {
            source: planner.source as TeamMemberSource,
            role: "planner",
            summary: options.summaries?.planner ?? DEFAULT_MEMBER_SUMMARIES.planner,
          },
          {
            source: announcer.source,
            role: ANNOUNCER_MEMBER_ROLE,
            summary: DEFAULT_MEMBER_SUMMARIES.announcer,
          },
        ],
      };
      this.teamRegistration = this.team().register(registration);
      this.announcerMember = announcer;
      this.members = {
        player,
        planner,
        announcer: {
          id: "announcer",
          member: announcer,
          execution: announcer,
        },
      };
      this.flow = createSaoleiFlow(() => this.peekTerminal());
      this.session = options.session;
      this.landed.length = 0;
      this.lastError = null;
    } catch (err) {
      // Rollback drops the game runtime built during this attempt (its
      // lifecycle is the materialization,
      // specs/075-saolei-game-foundation/contracts/team-loop-assembly.md §2).
      this.game = null;
      this.announcerMember = null;
      this.flow = null;
      this.members = null;
      await this.rollback(created);
      throw err;
    }
  }

  /**
   * Accept one user message (FR-011): delivered to the current activity's
   * member. While that member's turn is in flight the message steers into its
   * inbox (consumed at the next step boundary); while quiescent it assembles
   * and opens a turn. Delivery never passes through a flow decision.
   */
  submit(text: string): SubmitResult {
    if (this.disposed) {
      throw new OrchestratorStateError("DISPOSED", "orchestrator is disposed");
    }
    const members = this.members;
    if (members === null || this.flow === null) {
      throw new OrchestratorStateError(
        "UNMATERIALIZED",
        "team is not materialized; materialize before sending",
      );
    }
    const message = createUserMessage({
      content: [{ type: "text", text }],
      source: { kind: "user" },
    });
    const binding = members[this.flow.activeMemberId];
    const queued = binding.execution.busy;
    // Known boundary: delivery reaches the activation member only. A frozen
    // flow whose activity member is announce-only (`consumes = false`) cannot
    // be resumed by user delivery — `deliver` is a no-op — because its
    // recovery drive is the delivery itself
    // (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §3/§4). The
    // trigger face is an announce-activity INTERNAL failure, i.e.
    // `gameStatsText` throwing on a malformed record — vanishingly rare.
    // Evolution slot: fall back to delivering to the previous consuming member
    // when the activation member does not consume.
    try {
      binding.member.deliver(message);
    } catch (err) {
      // A delivery assembly failure joins the freeze channel (the caller was
      // not at fault; only the state guards throw synchronously) —
      // specs/076-agent-v2-optimize/contracts/flow-orchestration.md §5/§8.2.
      const failure = this.orchestrationFailure(err);
      this.recordFailure(null, failure, "orchestration");
      this.flow.decide(failureOutcome(failure));
      // No position: 0 is not a valid 1-based inbox position and the delivery
      // never reached the inbox (contracts/flow-orchestration.md §5).
      return {
        queued,
        messageId: String(message.id),
      };
    }
    if (queued) {
      return {
        queued: true,
        position: binding.execution.pendingInput,
        messageId: String(message.id),
      };
    }
    this.externalPending = true;
    this.requestPump();
    return {
      queued: false,
      messageId: String(message.id),
    };
  }

  /**
   * Cancel (FR-108, idempotent): terminate the in-flight member turn, freeze
   * the flow position (facts unconsumed), land the unclaimed inbox as history,
   * and wait for delivery to resume. A frozen flow is a no-op; the cancel
   * produces no failure presentation (caller intent is not a fault).
   */
  cancel(): CancelResult {
    if (this.disposed) {
      return { dropped: [] };
    }
    const members = this.members;
    const flow = this.flow;
    if (members === null || flow === null) {
      throw new OrchestratorStateError(
        "UNMATERIALIZED",
        "team is not materialized; there is nothing to cancel",
      );
    }
    if (flow.paused) {
      return { dropped: [] };
    }
    const binding = members[flow.activeMemberId];
    const dropped = this.landUnclaimed(binding);
    binding.execution.abort();
    const agent = binding.handle?.agent;
    if (binding.execution.busy && agent !== undefined) {
      agent.cancel({ kind: "user" });
    } else {
      // Quiescent cancel: freeze the flow position directly (there is no
      // in-flight drive whose aborted outcome would freeze it).
      flow.freezeAborted();
    }
    return { dropped };
  }

  /**
   * Terminate the in-flight turn, void the landed set, and release every
   * member. The orchestrator owns the member handles it created, so the host's
   * refresh/destroy path is exactly this call (idempotent, allSettled release).
   */
  async dispose(): Promise<void> {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    this.landed.length = 0;
    const members = this.members;
    const agentBindings = members === null ? [] : [members.player, members.planner];
    const inFlight = agentBindings.find((binding) => binding.execution.busy);
    if (inFlight !== undefined) {
      inFlight.execution.abort();
      inFlight.handle?.agent.cancel({ kind: "disposed" });
    }
    await this.whenQuiescent();

    const registration = this.teamRegistration;
    this.teamRegistration = null;
    registration?.dispose();

    this.members = null;
    this.game = null;
    this.announcerMember = null;
    this.flow = null;
    this.session = null;
    this.lastError = null;

    for (const binding of agentBindings) {
      binding.execution.dispose();
    }
    const results = await Promise.allSettled(
      agentBindings.map(
        (binding) => binding.handle?.dispose() ?? Promise.resolve(),
      ),
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
      await pump.catch(() => undefined);
      pump = this.pump;
    }
  }

  /** The live member handle (the system-prompt read surface of specs/059-agent-v2-team-mode/tasks.md T032), or undefined. */
  member(role: AgentTeamRole): AgentHandle | undefined {
    return this.members?.[role].handle;
  }

  /**
   * The team-scoped unified member event stream (a zero-shape pass-through:
   * the orchestration returns the team plugin's own stream object, so a
   * consumer never depends on a per-scenario event shape and a future loop
   * passes its stream through the same one-line read face). Contract:
   * specs/077-announcer-member-unification/contracts/member-event-stream.md §4.
   * Before materialization and after disposal the existing state-error
   * semantics apply.
   */
  teamEvents(): TeamEventStream {
    if (this.disposed) {
      throw new OrchestratorStateError("DISPOSED", "orchestrator is disposed");
    }
    const registration = this.teamRegistration;
    if (registration === null) {
      throw new OrchestratorStateError(
        "UNMATERIALIZED",
        "team is not materialized",
      );
    }
    return registration.events();
  }

  /**
   * The announce-only announcer member (its log/source face) while the team is
   * materialized; undefined before materialization and after dispose. The host
   * projects its announcements into the merge history through this read
   * surface (specs/065-agent-v2-team-refine/contracts/
   * game-stats-broadcast.md §3 item 6 / §4; the roster presence is
   * specs/076-agent-v2-optimize/contracts/announcer-member.md §3).
   */
  get announcer(): AnnouncerMember | undefined {
    return this.announcerMember ?? undefined;
  }

  /** The team's live state snapshot (status/queue/failure presentation). */
  snapshot(): OrchestratorSnapshot {
    const flow = this.flow;
    const busy = this.busyMember();
    return {
      materialized: this.members !== null,
      phase: flow === null ? null : phaseOfActivity(flow.position),
      active: busy,
      activation: flow?.activeMemberId ?? "planner",
      paused: flow?.paused ?? false,
      queued:
        busy === null ? 0 : (this.members?.[busy].execution.pendingInput ?? 0),
      failed: this.lastError !== null,
      lastError: this.lastError,
    };
  }

  /** Create one member with the full setup chain (compose → role extras). */
  private async createMember(
    role: AgentTeamRole,
    member: TeamMemberOptions,
    options: TeamMaterializeOptions,
    sessionId: string,
  ): Promise<MemberBinding> {
    const composed = await this.deps.compose(member.preset);
    const foundation = this.saoleiFoundation();
    const handle = await this.agents().create({
      sessionId: sessionId as CreateAgentOptions["sessionId"],
      meta: {
        cwd: this.deps.cwd ?? process.cwd(),
        agentPreset: composed.agentPreset,
      },
      agentOptions: {
        // Per-member route: the member's own provider wins, then the team
        // dependency default (host override), then the shipped route. The
        // model stays the bare id — the composite selector lives on the
        // selection surface only (specs/063-llm-reliability-opencode-go/
        // contracts/model-selection.md §3).
        provider: member.provider ?? this.deps.provider ?? TEAM_PROVIDER,
        ...(member.model === undefined || member.model === ""
          ? {}
          : { model: member.model }),
      },
      setup: async (agentCtx) => {
        await composed.setup(agentCtx);
        if (role === "player") {
          // The player consumes only the saolei static unit — the game runtime
          // dispatches through the desktop-bridge connection registered for
          // the GAME session resource name; the member's dsh session id is
          // namespaced (`{session}/player`) and is not the bridge key
          // (specs/059-agent-v2-team-mode/data-model.md §3). The orchestrator
          // builds the runtime here (materialization-time desktop-bridge
          // resolution) and holds it; the member scope receives only the
          // handle (specs/075-saolei-game-foundation/contracts/
          // team-loop-assembly.md §2/§3).
          this.registerSaoleiTools(agentCtx);
          agentCtx.systemPrompt.section(foundation.gameSection);
          this.game = foundation.createGameRuntime(
            options.session,
            this.desktopBridge(),
          );
          agentCtx.isolate("saoleiGame").provide("saoleiGame", this.game);
          return;
        }
        // The planner registers the gameplay section too; the static
        // registration precedes the fail-loud memory prefetch so a rejected
        // prefetch remains the setup's last rollback boundary
        // (specs/075-saolei-game-foundation/research.md D3).
        agentCtx.systemPrompt.section(foundation.gameSection);
        await this.registerMemory(agentCtx, options.memoryScope);
      },
    });

    const source = agentMemberSource(handle);
    const adapter = new AgentMember({
      id: role,
      agent: handle.agent,
      drain: () => this.team().drain(source),
      flushLanded: () => this.takeLanded(),
    });
    return {
      id: role,
      member: adapter,
      execution: adapter,
      handle,
      source,
    };
  }

  /** Read and clear the cancel-landed messages (the assembly-point flush). */
  private takeLanded(): UserMessage[] {
    return this.landed.splice(0);
  }

  /**
   * Collect the active member's unclaimed inbox into the landed set
   * (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §6 cancel):
   * messages delivered but not consumed by a turn. The durable cancel that
   * follows clears the inbox itself.
   */
  private landUnclaimed(binding: MemberBinding): UserMessage[] {
    const agent = binding.handle?.agent;
    const inbox = agent?.inbox as
      | { readonly nextStep?: readonly UserMessage[]; readonly nextTurn?: readonly UserMessage[] }
      | undefined;
    if (inbox === undefined) {
      return [];
    }
    const pending = [...(inbox.nextStep ?? []), ...(inbox.nextTurn ?? [])];
    this.landed.push(...pending);
    return pending;
  }

  private peekTerminal(): GameEventRecord | null {
    return this.game?.peekGameEvent() ?? null;
  }

  /** The agent-creation seam (host override, else the composition's registry). */
  private agents(): AgentCreationSeam {
    return this.deps.agents ?? this.ctx.agents;
  }

  /** The team-service seam (host override, else `ctx.team`). */
  private team(): TeamSeam {
    return this.deps.team ?? this.ctx.team;
  }

  /**
   * The game foundation face (`ctx.saolei`), provided by the top-level
   * `saolei` composition row. The plugin's `inject: ["saolei"]` already makes
   * the face a load-time requirement; the undefined guard narrows the type
   * for direct construction (unit tests) and would fail loud rather than
   * materialize a team without a game runtime
   * (specs/075-saolei-game-foundation/contracts/team-loop-assembly.md §1/§2).
   */
  private saoleiFoundation(): SaoleiFoundation {
    const foundation = this.ctx.saolei;
    if (foundation === undefined) {
      throw new Error(
        'saolei-loop orchestrator: the "saolei" game foundation is not ' +
          "composed; the top-level saolei row must provide it",
      );
    }
    return foundation;
  }

  /**
   * The desktop-bridge dispatch service backing the game runtime, resolved at
   * materialization time: the host dep first, else the composition's
   * `ctx.desktopBridge`. Missing = fail-loud (the runtime cannot dispatch).
   */
  private desktopBridge(): DesktopBridgeService {
    const desktopBridge = this.deps.desktopBridge ?? this.ctx.get("desktopBridge");
    if (desktopBridge === undefined) {
      throw new Error(
        "saolei-loop orchestrator: player game runtime needs the desktop-bridge service",
      );
    }
    return desktopBridge;
  }

  /**
   * Register the saolei static unit into the player member's scope: the
   * three tool definitions then the sections, one indivisible consumer
   * call (specs/074-dsh-grpc-runtime/spec.md FR-015/FR-017). The unit is read
   * from the `saoleiTools` provide face the saolei-loop apply mounted.
   */
  private registerSaoleiTools(agentCtx: Context): void {
    const unit = this.saoleiTools();
    for (const definition of unit.tools) {
      agentCtx.tools.register(definition);
    }
    for (const section of unit.sections) {
      agentCtx.systemPrompt.section(section);
    }
  }

  /**
   * Register the planner member's memory capability unit, then await its
   * fail-loud prefetch: faces → tools → sections → ready
   * (specs/074-dsh-grpc-runtime/spec.md FR-016; contracts/team-assembly.md
   * §4). A rejection from `ready()` propagates out of the member setup, so
   * the whole materialization rolls back — no half-assembled member
   * (contracts/team-assembly.md §6).
   */
  private async registerMemory(
    agentCtx: Context,
    scope: PlannerMemoryScope,
  ): Promise<void> {
    const unit = this.memoryMountProvider().build(scope);
    for (const face of unit.faces) {
      // The agent-scoped registration: an isolate child of the agent context
      // shares the agent fiber, so `exec.agent.ctx.<key>` resolves the face
      // from the tool path (the saolei `saoleiGame` pattern;
      // contracts/memory-library.md §5).
      agentCtx.isolate(face.key).provide(face.key, face.value);
    }
    for (const definition of unit.tools) {
      agentCtx.tools.register(definition);
    }
    for (const section of unit.sections) {
      agentCtx.systemPrompt.section(section);
    }
    await unit.ready();
  }

  /**
   * Read the `saoleiTools` provide face. Absent = the saolei-loop apply
   * mounted no saolei unit provider (a `config.plugins` omission) →
   * fail-loud; the orchestration never materializes a player without its
   * tools (contracts/team-assembly.md §4/§6).
   */
  private saoleiTools(): SaoleiToolsUnit {
    const unit = this.ctx.get("saoleiTools") as SaoleiToolsUnit | undefined;
    if (unit === undefined) {
      throw new Error(
        'saolei-loop orchestrator: the "saoleiTools" capability is not ' +
          "mounted; the saolei-loop row's config.plugins must list the " +
          "saolei unit provider",
      );
    }
    return unit;
  }

  /**
   * Read the `memoryMount` provide face. Absent = the saolei-loop apply
   * mounted no memory provider (a `config.plugins` omission) → fail-loud; a
   * planner is never materialized without its memory capability
   * (contracts/team-assembly.md §4/§6).
   */
  private memoryMountProvider(): MemoryMount {
    const mount = this.ctx.get("memoryMount") as MemoryMount | undefined;
    if (mount === undefined) {
      throw new Error(
        'saolei-loop orchestrator: the "memoryMount" capability is not ' +
          "mounted; the saolei-loop row's config.plugins must list the " +
          "memory provider",
      );
    }
    return mount;
  }

  /** Roll a failed materialization back: drop created members, no residue. */
  private async rollback(created: readonly MemberBinding[]): Promise<void> {
    const registration = this.teamRegistration;
    this.teamRegistration = null;
    registration?.dispose();
    // A teardown failure must not mask the original materialization error and
    // cannot restore a half-built team, so the caller sees the original cause.
    await Promise.allSettled(
      [...created].reverse().map((binding) => {
        binding.execution.dispose();
        return binding.handle?.dispose() ?? Promise.resolve();
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
    void pump.then(
      () => {
        if (this.pump === pump) {
          this.pump = null;
        }
      },
      () => {
        if (this.pump === pump) {
          this.pump = null;
        }
      },
    );
  }

  /**
   * The serialized pump: each iteration consumes exactly one wake — the
   * outcome of the flow's last `start`, or a delivery-started turn's
   * settlement — asks the flow's decision entry, and either runs the returned
   * `start(activity)` to its outcome or rests (resting / frozen). This is the
   * "at most one member driven at a time" invariant's enforcement point.
   */
  private async runPump(): Promise<void> {
    for (;;) {
      if (this.disposed) {
        return;
      }
      const flow = this.flow;
      const members = this.members;
      if (flow === null || members === null) {
        return;
      }

      let wake: Outcome | null;
      if (this.externalPending) {
        this.externalPending = false;
        const binding = members[flow.activeMemberId];
        let outcome: Outcome;
        try {
          outcome = await binding.execution.whenSettled();
        } catch (err) {
          // A technical throw from a delivery-started turn: the same
          // orchestration-at-the-start-site translation as a flow drive
          // (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §5).
          const failure = this.orchestrationFailure(err);
          this.recordFailure(binding.id, failure, "orchestration");
          this.flow?.decide(failureOutcome(failure));
          return;
        }
        if (this.disposed) {
          return;
        }
        this.settleError(outcome);
        if (outcome.kind === "failed") {
          this.recordFailure(binding.id, outcome, "member-turn");
        }
        // A delivery-started turn is invisible to the flow (no onSettle); a
        // settled delivery is an external wake, a failure joins the freeze
        // channel (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §3).
        wake = outcome.kind === "settled" ? null : outcome;
      } else {
        wake = this.pendingOutcome;
        this.pendingOutcome = null;
      }

      const decision = flow.decide(wake);
      if (decision.kind !== "start") {
        return;
      }
      const outcome = await this.execute();
      if (this.disposed) {
        return;
      }
      this.pendingOutcome = outcome;
    }
  }

  /**
   * Run the flow's current activity to its outcome: land the activation (the
   * flow position already fixed it), then drive the bound member — a rest
   * activity has no execution and reports `resting`. A technical throw at the
   * start call site is translated into `failed{INTERNAL}` with origin
   * orchestration (specs/076-agent-v2-optimize/contracts/flow-orchestration.md
   * §5).
   */
  private async execute(): Promise<Outcome> {
    const flow = this.flow;
    const members = this.members;
    if (flow === null || members === null) {
      return { kind: "resting" };
    }
    const activity = flow.activity;
    if (activity.driverless === true) {
      return { kind: "resting" };
    }
    const binding = members[activity.member];
    try {
      const outcome = await binding.member.drive();
      this.settleError(outcome);
      if (outcome.kind === "failed") {
        this.recordFailure(binding.id, outcome, "member-turn");
      }
      return outcome;
    } catch (err) {
      const failure = this.orchestrationFailure(err);
      this.recordFailure(binding.id, failure, "orchestration");
      return failureOutcome(failure);
    }
  }

  /** Clear the failure fact after a successful or aborted drive. */
  private settleError(outcome: Outcome): void {
    if (outcome.kind === "settled" || outcome.kind === "resting" || outcome.kind === "aborted") {
      this.lastError = null;
    }
  }

  /**
   * Record a failed drive: the fact goes to the host-injected logger (console
   * fallback) and into {@link OrchestratorSnapshot} (`failed`/`lastError`) so
   * the host can map an INTERNAL/turn error instead of a silent stall.
   * `origin` records which channel failed — the driven member's turn or the
   * orchestration layer — the host's stream-settlement discriminant
   * (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §2).
   */
  private recordFailure(
    member: MemberId | null,
    failure: { readonly code: LoopErrorCode; readonly detail: FailureDetail },
    origin: OrchestratorFailureOrigin,
  ): void {
    const phase = this.phase();
    const loggerMember: TeamRole | null = member;
    this.lastError = {
      message: failure.detail.message,
      member,
      phase,
      code: failure.code,
      origin,
      detail: failure.detail,
    };
    this.logger().error(
      "saolei-loop orchestration step failed; auto-continuation suspended",
      {
        session: this.session ?? "",
        phase,
        member,
        code: failure.code,
        error: failure.detail.message,
      },
    );
  }

  /** Translate a technical throw into the orchestration-interior failure shape. */
  private orchestrationFailure(err: unknown): {
    readonly code: LoopErrorCode;
    readonly detail: FailureDetail;
  } {
    return {
      code: "INTERNAL",
      detail: { message: err instanceof Error ? err.message : String(err) },
    };
  }

  /** The current workflow phase, derived from the flow position. */
  private phase(): OrchestrationPhase {
    return this.flow === null ? "planning" : phaseOfActivity(this.flow.position);
  }

  /** The host's failure reporter, or the console fallback. */
  private logger(): OrchestratorLogger {
    return this.deps.logger ?? DEFAULT_ORCHESTRATOR_LOGGER;
  }

  /** The busy member id (at most one member is driven at a time). */
  private busyMember(): MemberId | null {
    const members = this.members;
    if (members === null) {
      return null;
    }
    for (const id of MEMBER_IDS) {
      if (members[id].execution.busy) {
        return id;
      }
    }
    return null;
  }
}

/** The outcome shape one orchestration-interior failure feeds the flow. */
function failureOutcome(failure: {
  readonly code: LoopErrorCode;
  readonly detail: FailureDetail;
}): Outcome {
  return { kind: "failed", code: failure.code, detail: failure.detail };
}
