/**
 * Sts2Orchestrator — the sts2 minimal orchestration's assembly layer: one main
 * agent materialized per session, driven by the official agent API through
 * {@link MainAgentDriver}, presented as a single-member orchestration seam.
 *
 * The behaviour contract is
 * specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md §2 (public
 * surface), §3 (materialization/release), §5 (envelope) and §6 (snapshot); the
 * seam it satisfies structurally is the host's `SceneOrchestration`
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §2). This package
 * never imports the host: every seam type below is a narrow structural view
 * whose shape the host's `SceneOrchestration` accepts, and the state error
 * carries the same stable `code` strings so the host maps it without class
 * identity (specs/080-host-main-sub-model/contracts/scene-registry.md §2).
 *
 * Materialization follows the saolei-loop precedent
 * (common/js/dsh-plugins/saolei/loop/src/orchestrator.ts): compose the preset,
 * create the agent with the composed declaration and the split model route,
 * let the preset mount its scope inside the creation setup, then subscribe the
 * member event envelope. One creation attempt owns the whole transaction, so
 * any failure releases what it created and leaves the orchestrator
 * unmaterialized for a retry (contract §3, no half-materialized session).
 *
 * The single-agent shape removes the team layer entirely: no team registration
 * (`ctx.team`), no announcer, no memory mount, no hooks, no game runtime, no
 * desktop flow, no workflow phase (contract §3 "不做"; spec FR-010).
 */

import type { Context } from "@deepseek-ai/cordis";
import type { AgentHandle, CreateAgentOptions } from "@deepseek-ai/dsh-agent";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import type { TeamEventStream } from "@dominion/dsh-team";

import { MainAgentDriver } from "./drive.js";
import type { MainDriveErrorCode, MainDriveFailure } from "./drive.js";
import { MainAgentEventStream, MAIN_MEMBER_ROLE } from "./events.js";

/** Default provider route; the host passes the deployment route explicitly. */
const DEFAULT_PROVIDER = "glm-responses";

/**
 * The preset-compose seam result: structurally the preset-authoring
 * `ComposeResult` (common/js/dsh-plugins/preset-authoring/src/index.ts), so the
 * host passes `ctx.presetAuthoring.compose` without this package depending on
 * the authoring plugin.
 */
export interface ComposedPreset {
  /** Resolved preset id snapshot for the creation meta (`meta.agentPreset`). */
  readonly agentPreset: string;
  /** Scope mount hook invoked by the agent creation setup. */
  setup(agentCtx: Context): Promise<void> | void;
}

/** Resolve + compose one preset (production: `presetAuthoring.compose`). */
export type ComposePreset = (preset: string) => Promise<ComposedPreset>;

/** Narrow structural view of `ctx.agents` (registry creation face). */
export interface AgentCreationSeam {
  create(options: CreateAgentOptions): Promise<AgentHandle>;
}

/**
 * The failure facts handed to the injected reporter: the session, the member
 * that failed, the stable loop code and the normalized message. The sts2
 * orchestration has one member and no workflow phase, so the context carries
 * neither a role set nor a phase.
 */
export type Sts2OrchestrationFailureContext = {
  readonly session: string;
  readonly member: string | null;
  readonly code: MainDriveErrorCode;
  readonly error: string;
};

/**
 * Failure reporter seam. The host injects its own logger (the deployment's
 * OTel-backed `@dominion/common-js-logs` face); the default console fallback
 * keeps a failed step observable even without one.
 */
export interface OrchestratorLogger {
  error(message: string, context: Sts2OrchestrationFailureContext): void;
}

/** Default failure reporter (hosts override through {@link Sts2OrchestratorDeps.logger}). */
const DEFAULT_ORCHESTRATOR_LOGGER: OrchestratorLogger = {
  error(message, context) {
    console.error(message, context);
  },
};

/** Collaborators and host-injected seams of one {@link Sts2Orchestrator}. */
export interface Sts2OrchestratorDeps {
  /** Resolve + compose seam (production: `ctx.presetAuthoring.compose`). */
  readonly compose: ComposePreset;
  /** Agent registry seam; defaults to `ctx.agents`. */
  readonly agents?: AgentCreationSeam;
  /** Failure reporter; defaults to a console-backed reporter. */
  readonly logger?: OrchestratorLogger;
  /** Provider route; defaults to {@link DEFAULT_PROVIDER}. */
  readonly provider?: string;
}

/**
 * One caller-supplied member configuration (the host validates and splits the
 * composite model selector before handing the input over).
 */
export interface Sts2MemberConfig {
  /** Member role; the sts2 scene roster has exactly `main`. */
  readonly role: string;
  /** Preset id the member materializes from. */
  readonly preset: string;
  /** Provider route this member's model belongs to; empty = the default route. */
  readonly provider?: string;
  /** Bare model id; empty = the provider/adapter default. */
  readonly model?: string;
}

/** The materialization input of one session (host: UpdateTeam). */
export interface Sts2MaterializeInput {
  /** Session resource name (`templates/{template}/sessions/{session}`). */
  readonly session: string;
  /** Business template id the session belongs to (unused by the single-agent flow). */
  readonly template: string;
  /** The configured members; the sts2 scene supplies exactly the `main` member. */
  readonly members: readonly Sts2MemberConfig[];
}

/** One materialized member's projection: the sts2 scene has only an agent member. */
export interface Sts2Member {
  /** Member role. */
  readonly role: string;
  /** The main member is agent-backed. */
  readonly kind: "agent";
}

/**
 * The diagnostic half of a failure report: the engine's stable code (when the
 * failure surfaced as one) plus its message.
 */
export interface Sts2FailureDetail {
  readonly engineCode?: string;
  readonly message: string;
}

/** The last failed main-agent turn, exposed through {@link Sts2Snapshot}. */
export interface Sts2Failure {
  /** Normalized error message of the failed turn. */
  readonly message: string;
  /** The member whose turn failed; the sts2 orchestration has one member. */
  readonly member: string | null;
  /** Loop failure code ({@link MainDriveErrorCode}). */
  readonly code: MainDriveErrorCode;
  /** Failure origin: a single agent has no orchestration-layer failure source. */
  readonly origin: "member-turn" | "orchestration";
  /** The engine code (when any) plus the message, transported for presentation. */
  readonly detail: Sts2FailureDetail;
}

/**
 * Read-only orchestration view for the host (queue pre-read, quiescence
 * watcher, TeamView projection). It deliberately has no workflow-position
 * field (contract §6).
 */
export interface Sts2Snapshot {
  readonly materialized: boolean;
  /** The member whose turn is in flight, or null. */
  readonly active: string | null;
  /** The single member's activation; always `main`. */
  readonly activation: string;
  /** Whether the driver is frozen (a cancel or a failed turn). */
  readonly paused: boolean;
  /** The in-flight steering count. */
  readonly queued: number;
  /** True while the last turn failed (no automatic continuation). */
  readonly failed: boolean;
  /** The last failed turn; cleared by the next successful turn. */
  readonly lastError: Sts2Failure | null;
}

/** Stable state-error codes the host maps onto `FAILED_PRECONDITION`. */
export type Sts2StateErrorCode =
  | "UNMATERIALIZED"
  | "ALREADY_MATERIALIZED"
  | "DISPOSED";

/** A caller-visible state error, same-coded as the host's `SceneStateError`. */
export class Sts2StateError extends Error {
  readonly code: Sts2StateErrorCode;

  constructor(code: Sts2StateErrorCode, message: string) {
    super(message);
    this.name = "Sts2StateError";
    this.code = code;
  }
}

/**
 * The single-member orchestration over one main agent. The orchestrator owns
 * the agent handle it created, the driver and the event stream, so the host's
 * refresh/destroy path is exactly {@link dispose}.
 */
export class Sts2Orchestrator {
  private handle: AgentHandle | null = null;
  private driver: MainAgentDriver | null = null;
  private events: MainAgentEventStream | null = null;
  private session: string | null = null;
  private disposed = false;

  constructor(
    private readonly ctx: Context,
    private readonly deps: Sts2OrchestratorDeps,
  ) {}

  /**
   * Materialize the main agent: compose → create (preset declaration + model
   * route + scope setup) → subscribe the member envelope. A failure rolls the
   * created agent back, leaving the orchestrator unmaterialized so the caller
   * can retry (contract §3).
   */
  async materialize(input: Sts2MaterializeInput): Promise<void> {
    if (this.disposed) {
      throw new Sts2StateError("DISPOSED", "orchestrator is disposed");
    }
    if (this.handle !== null) {
      throw new Sts2StateError(
        "ALREADY_MATERIALIZED",
        "the main agent is already materialized",
      );
    }
    const member = mainMember(input);
    let created: AgentHandle | null = null;
    let stream: MainAgentEventStream | null = null;
    try {
      const composed = await this.deps.compose(member.preset);
      created = await this.agents().create({
        sessionId: `${input.session}/${MAIN_MEMBER_ROLE}` as CreateAgentOptions["sessionId"],
        meta: {
          cwd: process.cwd(),
          agentPreset: composed.agentPreset,
        },
        agentOptions: {
          // The member's own route wins, then the deployment default; the model
          // stays the bare id (the composite selector lives on the selection
          // surface only, specs/063-llm-reliability-opencode-go/contracts/
          // model-selection.md §3).
          provider: member.provider ?? this.deps.provider ?? DEFAULT_PROVIDER,
          ...(member.model === undefined || member.model === ""
            ? {}
            : { model: member.model }),
        },
        setup: async (agentCtx) => {
          await composed.setup(agentCtx);
        },
      });
      stream = new MainAgentEventStream(created);
      const driver = new MainAgentDriver({ agent: created.agent });
      this.handle = created;
      this.events = stream;
      this.driver = driver;
      this.session = input.session;
    } catch (err) {
      // A teardown failure must not mask the original materialization error,
      // and the agent is not reachable through the seam before commit.
      stream?.dispose();
      await created?.dispose().catch(() => undefined);
      this.logger().error(
        "sts2-loop materialization failed; rolled back to unmaterialized",
        {
          session: input.session,
          member: MAIN_MEMBER_ROLE,
          code: "INTERNAL",
          error: messageOf(err),
        },
      );
      throw err;
    }
  }

  /** The single member projection while materialized; empty otherwise. */
  members(): readonly Sts2Member[] {
    return this.handle === null
      ? []
      : [{ role: MAIN_MEMBER_ROLE, kind: "agent" }];
  }

  /** The live main agent handle, or undefined while unmaterialized. */
  member(role: string): AgentHandle | undefined {
    return role === MAIN_MEMBER_ROLE ? (this.handle ?? undefined) : undefined;
  }

  /** The main member's unified event stream (contract §5). */
  teamEvents(): TeamEventStream {
    if (this.disposed) {
      throw new Sts2StateError("DISPOSED", "orchestrator is disposed");
    }
    const events = this.events;
    if (events === null) {
      throw new Sts2StateError(
        "UNMATERIALIZED",
        "the main agent is not materialized",
      );
    }
    return events;
  }

  /** The live orchestration snapshot (contract §6). */
  snapshot(): Sts2Snapshot {
    const driver = this.driver;
    if (driver === null) {
      return {
        materialized: false,
        active: null,
        activation: MAIN_MEMBER_ROLE,
        paused: false,
        queued: 0,
        failed: false,
        lastError: null,
      };
    }
    return {
      materialized: true,
      active: driver.busy ? MAIN_MEMBER_ROLE : null,
      activation: MAIN_MEMBER_ROLE,
      paused: driver.paused,
      queued: driver.pendingInput,
      failed: driver.failed,
      lastError: failureOf(driver.lastError),
    };
  }

  /**
   * Accept one user message: at rest it opens a turn, in flight it steers into
   * the agent inbox (contract §4). The host reads the queued decision from
   * {@link snapshot} and consumes no return value.
   */
  submit(text: string): void {
    if (this.disposed) {
      throw new Sts2StateError("DISPOSED", "orchestrator is disposed");
    }
    const driver = this.driver;
    if (driver === null) {
      throw new Sts2StateError(
        "UNMATERIALIZED",
        "the main agent is not materialized; materialize before sending",
      );
    }
    const message = createUserMessage({
      content: [{ type: "text", text }],
      source: { kind: "user" },
    });
    const opensTurn = !driver.busy;
    driver.submit(message);
    if (opensTurn) {
      this.observeTurn(driver, this.session ?? "");
    }
  }

  /**
   * Cancel the in-flight turn and freeze (idempotent; contract §4). A frozen
   * driver holds its unconsumed input for the next delivery.
   */
  cancel(): void {
    if (this.disposed) {
      return;
    }
    const driver = this.driver;
    if (driver === null) {
      throw new Sts2StateError(
        "UNMATERIALIZED",
        "the main agent is not materialized; there is nothing to cancel",
      );
    }
    driver.cancel();
  }

  /** Resolve at the driver's quiescent point (no in-flight turn). */
  async whenQuiescent(): Promise<void> {
    const driver = this.driver;
    if (driver === null) {
      return;
    }
    await driver.whenSettled().then(
      () => undefined,
      () => undefined,
    );
  }

  /**
   * Terminate the in-flight turn, then release the event subscription, the
   * driver listener and the agent (idempotent; contract §3).
   */
  async dispose(): Promise<void> {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    const driver = this.driver;
    const events = this.events;
    const handle = this.handle;
    this.driver = null;
    this.events = null;
    this.handle = null;
    this.session = null;
    if (driver !== null) {
      if (driver.busy) {
        // Teardown is not a user action: the durable turn/end cause says
        // `disposed` (the saolei-loop dispose precedent).
        driver.cancel({ kind: "disposed" });
      }
      await driver.whenSettled().then(
        () => undefined,
        () => undefined,
      );
      driver.dispose();
    }
    events?.dispose();
    if (handle !== null) {
      await handle.dispose();
    }
  }

  /** Report the turn's failure once the turn that this submit opened settles. */
  private observeTurn(driver: MainAgentDriver, session: string): void {
    void driver.whenSettled().then(
      (outcome) => {
        if (outcome.kind !== "failed") {
          return;
        }
        this.logger().error(
          "sts2-loop main agent turn failed; the driver is frozen until the next delivery",
          {
            session,
            member: MAIN_MEMBER_ROLE,
            code: outcome.code,
            error: outcome.detail.message,
          },
        );
      },
      (err: unknown) => {
        this.logger().error(
          "sts2-loop main agent turn failed with a technical error",
          {
            session,
            member: MAIN_MEMBER_ROLE,
            code: "INTERNAL",
            error: messageOf(err),
          },
        );
      },
    );
  }

  /** The agent-creation seam (host override, else the composition's registry). */
  private agents(): AgentCreationSeam {
    return this.deps.agents ?? this.ctx.agents;
  }

  /** The host's failure reporter, or the console fallback. */
  private logger(): OrchestratorLogger {
    return this.deps.logger ?? DEFAULT_ORCHESTRATOR_LOGGER;
  }
}

/**
 * The single member of the materialization input: the host validated the roster
 * shape, so anything else is a programming error at the orchestration edge and
 * fails loud instead of materializing a partial session (contract §3).
 */
function mainMember(input: Sts2MaterializeInput): Sts2MemberConfig {
  if (input.members.length !== 1) {
    throw new Error(
      `sts2-loop materialization requires exactly one member; got ${input.members.length}`,
    );
  }
  const member = input.members[0];
  if (member.role !== MAIN_MEMBER_ROLE) {
    throw new Error(
      `sts2-loop materialization requires the "${MAIN_MEMBER_ROLE}" member; got "${member.role}"`,
    );
  }
  if (member.preset === "") {
    throw new Error(
      `sts2-loop materialization requires a preset id for the "${MAIN_MEMBER_ROLE}" member`,
    );
  }
  return member;
}

/** Project the driver's failure onto the seam's failure shape (contract §6). */
function failureOf(failure: MainDriveFailure | null): Sts2Failure | null {
  if (failure === null) {
    return null;
  }
  return {
    message: failure.message,
    member: MAIN_MEMBER_ROLE,
    code: failure.code,
    origin: "member-turn",
    detail: failure.detail,
  };
}

/** Normalize an unknown thrown value into a message. */
function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
