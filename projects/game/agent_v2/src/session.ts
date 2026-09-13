/**
 * session.ts — game session ↔ dsh team mapping for agent_v2.
 *
 * `TeamSessions` owns the materialization registry over the team
 * orchestrator (`@dominion/dsh-saolei-loop`): one {@link TeamOrchestrator}
 * per materialized session, two member runtimes (player + planner), the
 * session-lifetime team history projections, and the active team streams.
 *
 * Materialization semantics (specs/059-agent-v2-team-mode/contracts/
 * team-api.md §2): UpdateTeam materializes or refreshes the session's team
 * singleton; validation is fail-fast (both presets exist and match their
 * member role, a non-empty composite `provider/model-id` selector resolves in
 * the selected provider's catalog) and runs BEFORE any
 * teardown; a refresh terminates the in-flight member turn, voids the queued
 * messages, clears both members' short-term memory (a fresh orchestrator +
 * history), and rebuilds — with create_time preserved. Any materialization
 * failure (including a member setup error such as the planner memory
 * prefetch) rolls the created members back inside the orchestrator, so no
 * half-materialized team can ever be observed (GetTeam NOT_FOUND afterwards)
 * and the caller can retry.
 *
 * Send has no lazy creation (FAILED_PRECONDITION while unmaterialized) and is
 * a pure subscription over the orchestrator: the user message is handed to
 * {@link TeamOrchestrator.submit}, the stream is attached to the session's
 * active set, and the stream ends when the team reaches its static point
 * (specs/059-agent-v2-team-mode/contracts/team-api.md §3.1). A client
 * disconnect only detaches the stream — it MUST NOT stop the orchestration;
 * cancelling the team happens only through {@link TeamSessions.cancel}.
 *
 * The Context injected at construction plus the optional {@link
 * TeamSessionsDeps} are the dependency seams: unit tests pass doubles
 * instead of intercepting modules (style/javascript.md Mock convention).
 */

import { error, info } from "@dominion/common-js-logs";
import type { Agent, AgentHandle } from "@deepseek-ai/dsh-agent";
import {
  OrchestratorStateError,
  TeamOrchestrator,
} from "@dominion/dsh-saolei-loop";
import type {
  AgentCreationSeam,
  ComposePreset,
  LoadPlannerMemory,
  MountPlayerRuntime,
  OrchestratorFailure,
  OrchestratorLogger,
  TeamSeam,
} from "@dominion/dsh-saolei-loop";
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import type { PresetAuthoringService } from "@dominion/dsh-preset-authoring";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";
import type { DshContext } from "./dsh.js";
import { MemberCollector, TeamHistory } from "./history.js";
import type { MemberRole, MemberViewEntry, TeamMergeEntry, TurnStream } from "./history.js";
import { readMemberSystemPrompt } from "./system-prompt.js";

export type { MemberRole, MemberViewEntry, TeamMergeEntry, TurnStream } from "./history.js";

/**
 * Adapter route registered by @dominion/dsh-llm-glm (contracts/glm-llm-plugin.md §2).
 */
export const PROVIDER = "glm-responses";

/**
 * Default model selector — the composite `${provider}/${model-id}` form
 * (specs/063-llm-reliability-opencode-go/contracts/model-selection.md §1).
 * `GLM_MODEL` keeps its bare-id semantics (the GLM provider route is
 * implied), so the deployment env contract is unchanged.
 */
export const DEFAULT_MODEL = `${PROVIDER}/${process.env.GLM_MODEL || "glm-5.3"}`;

/** The team goal rendered into the shared team section (scene-agnostic plugin input). */
export const TEAM_GOAL =
  "协作完成多局扫雷游戏：player 执行操作、planner 复盘与制定策略，共同提高胜率。";

/**
 * Stable AIP error codes the gRPC layer maps onto statuses
 * (specs/059-agent-v2-team-mode/contracts/team-api.md §6).
 */
export type TeamSessionErrorCode = "INVALID_ARGUMENT" | "NOT_FOUND" | "FAILED_PRECONDITION";

/** A request-level team error carrying its stable AIP code. */
export class TeamSessionError extends Error {
  readonly code: TeamSessionErrorCode;

  constructor(code: TeamSessionErrorCode, message: string) {
    super(message);
    this.name = "TeamSessionError";
    this.code = code;
  }
}

/** One member's configuration and runtime state (View projection). */
export interface TeamMemberView {
  /** Member resource name (server-constructed from the role). */
  readonly name: string;
  /** Member role (scene vocabulary; saolei: "player" / "planner"). */
  readonly role: string;
  /** Full preset resource name the member materialized from. */
  readonly preset: string;
  /** Effective model selector, in composite `${provider}/${model-id}` form. */
  readonly model: string;
  /**
   * The member instance's complete effective system prompt (persona + team
   * section + tool guidance + the planner memory snapshot). Filled only by
   * GetTeamMember from the instance's live assembly surface; empty in the
   * Team member snapshots (GetTeam/UpdateTeam).
   */
  readonly systemPrompt: string;
}

/** The team singleton projection served by GetTeam/UpdateTeam. */
export interface TeamView {
  readonly name: string;
  /**
   * The current active member (FR-008 single merged value): the in-flight
   * driving member while a member turn runs, otherwise the member owning the
   * next input (activation). Always a role on a materialized team (the
   * initial activation is "planner"); cancel/pause does not change it
   * (specs/060-agent-v2-team-optimize/contracts/team-api.md §1).
   */
  readonly activeMember: string;
  readonly members: readonly TeamMemberView[];
  readonly createTime: Date;
  readonly updateTime: Date;
}

/**
 * One caller-supplied member configuration — the scene-agnostic
 * materialization primitive (proto `TeamMember` input side).
 */
export interface TeamMemberOptions {
  /** Member role (scene vocabulary; saolei: "player" / "planner"). */
  readonly role: string;
  /** Full preset resource name. */
  readonly preset: string;
  /**
   * Composite model selector (`provider/model-id`, e.g.
   * `glm-responses/glm-5.3`); empty/undefined = the process default
   * (specs/063-llm-reliability-opencode-go/contracts/model-selection.md §3).
   */
  readonly model?: string;
}

/** The materialize/refresh request: the caller-supplied member list. */
export interface MaterializeTeamOptions {
  readonly members: readonly TeamMemberOptions[];
}

/**
 * The collaborator seams of {@link TeamSessions}. Production resolves them
 * from the composed context; unit tests inject doubles.
 */
export interface TeamSessionsDeps {
  /** Preset compose/lookup face; defaults to `ctx.presetAuthoring`. */
  readonly authoring?: Pick<PresetAuthoringService, "compose" | "get">;
  /** Deployment model catalog; defaults to the composition's `ctx.llm`. */
  readonly listModels?: (provider: string) => Promise<ReadonlyArray<{ id: string }>>;
  /** Agent registry creation seam; defaults to `ctx.agents`. */
  readonly agents?: AgentCreationSeam;
  /** Team service seam; defaults to `ctx.team`. */
  readonly team?: TeamSeam;
  /** Player game-runtime mount; defaults to the desktop-bridge builder. */
  readonly mountPlayerRuntime?: MountPlayerRuntime;
  /**
   * Planner memory prefetch override (unit tests); production binds the real
   * `ctx.plannerMemory.load` host-row service face (T021).
   */
  readonly loadPlannerMemory?: LoadPlannerMemory;
  /** Orchestration failure reporter; defaults to the repo logger face. */
  readonly logger?: OrchestratorLogger;
  /** Provider route for both members; defaults to {@link PROVIDER}. */
  readonly provider?: string;
}

interface MemberRuntime {
  readonly role: MemberRole;
  readonly preset: string;
  readonly model: string;
  readonly handle: AgentHandle;
  readonly collector: MemberCollector;
}

interface TeamEntry {
  readonly sessionName: string;
  readonly template: string;
  readonly orchestrator: TeamOrchestrator;
  readonly history: TeamHistory;
  readonly members: Record<MemberRole, MemberRuntime>;
  readonly streams: Set<TurnStream>;
  readonly createTime: Date;
  updateTime: Date;
  /** The armed quiescence watcher; null when none is pending. */
  quiescenceWatch: Promise<void> | null;
  disposed: boolean;
}

const SESSION_RESOURCE = /^templates\/([^/]+)\/sessions\/([^/]+)$/;
const PRESET_RESOURCE = /^templates\/([^/]+)\/presets\/([^/]+)$/;

/**
 * Read attempts for one GetTeamMember call: each attempt samples a live entry
 * generation and re-samples after a refresh tears it down mid-read. Three
 * strikes is comfortably beyond the realistic (user-initiated) double-Apply
 * window; a team still churning afterwards surfaces NOT_FOUND (retryable).
 */
const MEMBER_READ_ATTEMPTS = 3;

function parseSessionResource(name: string): { template: string; session: string } | undefined {
  const match = SESSION_RESOURCE.exec(name);
  if (match === null) {
    return undefined;
  }
  return { template: match[1] as string, session: match[2] as string };
}

function parsePresetResource(name: string): { template: string; preset: string } | undefined {
  const match = PRESET_RESOURCE.exec(name);
  if (match === null) {
    return undefined;
  }
  return { template: match[1] as string, preset: match[2] as string };
}

/** A parsed composite model selector: the structured (provider, model) route. */
export interface ParsedModelIdentifier {
  /** Provider route owning the model (e.g. `glm-responses`). */
  readonly provider: string;
  /** Bare model id passed to the adapter (e.g. `glm-5.3`). */
  readonly model: string;
}

/** Composite-form hint shared by every model-selector rejection (FR-014). */
const COMPOSITE_MODEL_HINT =
  'model selectors use the composite form "provider/model-id" (see ListModels)';

/**
 * Split one composite model selector at the FIRST `/` — the service's single
 * parsing point (specs/063-llm-reliability-opencode-go/contracts/
 * model-selection.md §1). A value without `/` or with an empty provider/model
 * segment is INVALID_ARGUMENT: the service never guesses a provider for a
 * legacy bare id (fail-loud migration, contract §5).
 */
export function parseModelIdentifier(selector: string): ParsedModelIdentifier {
  const slash = selector.indexOf("/");
  if (slash <= 0 || slash === selector.length - 1) {
    throw new TeamSessionError(
      "INVALID_ARGUMENT",
      `model "${selector}" is not a valid selector; ${COMPOSITE_MODEL_HINT}`,
    );
  }
  return {
    provider: selector.slice(0, slash),
    model: selector.slice(slash + 1),
  };
}

/**
 * Owns the live team entries, their per-session materialization serialization,
 * the history projections, and the active team streams.
 */
export class TeamSessions {
  private readonly teams = new Map<string, TeamEntry>();
  /** Serializes re-materializations per session (refresh-in-flight rule). */
  private readonly materializations = new Map<string, Promise<unknown>>();

  constructor(
    private readonly ctx: DshContext,
    private readonly deps: TeamSessionsDeps = {},
  ) {}

  /**
   * Accept one user message for a MATERIALIZED team. The message is fixed
   * into the merged sequence at acceptance (enqueue-time, including the
   * queued path), the stream is attached to the session's active stream set,
   * and a `queued` frame is written first on this stream only when the
   * current member's turn is in flight. The returned detach removes exactly
   * this stream — a client disconnect must not touch the orchestration.
   *
   * An unmaterialized session throws FAILED_PRECONDITION before any frame
   * (specs/059-agent-v2-team-mode/contracts/team-api.md §3).
   */
  send(session: string, text: string, stream: TurnStream): () => void {
    const entry = this.requireEntry(session, "FAILED_PRECONDITION");
    // The queued frame is this stream's first frame (contract §3.3), so the
    // queued branch is written before the stream joins the active set. The
    // snapshot is taken synchronously before submit, and submit's queued
    // state derives from the same orchestrator state.
    const before = entry.orchestrator.snapshot();
    const willQueue = before.active !== null;
    if (willQueue) {
      stream.write({ session, turnId: "", queued: { position: before.queued + 1 } });
    }
    const detach = this.attach(entry, stream);
    // Fix the user message into the merged sequence at acceptance (enqueue
    // included) and fan out its team_message frame before the first drive.
    entry.history.appendUser(text);
    try {
      entry.orchestrator.submit(text);
    } catch (err) {
      detach();
      throw this.mapOrchestratorError(err);
    }
    this.watchQuiescence(entry);
    return detach;
  }

  /**
   * Cancel the team (FR-017 / contracts/team-api.md §4): terminate the
   * in-flight member turn with a CANCELED terminal frame, suspend automatic
   * continuation, keep the queued messages as already-fixed history (they
   * entered the merged sequence at Send acceptance), and end the active
   * streams at the resulting static point. Idempotent.
   * Unmaterialized sessions fail like Send (FAILED_PRECONDITION).
   */
  cancel(session: string): void {
    const entry = this.requireEntry(session, "FAILED_PRECONDITION");
    const active = entry.orchestrator.snapshot().active;
    if (active !== null) {
      // Known window: the snapshot can report the member active before its
      // collector observed the running transition, in which case the mark is
      // consumed by the first idle that finds an active turn (see
      // MemberCollector.markOutcome).
      entry.members[active].collector.markOutcome({ status: "CANCELED" });
    }
    entry.orchestrator.cancel();
    this.watchQuiescence(entry);
  }

  /**
   * The session's team singleton projection (GetTeam); NOT_FOUND while
   * unmaterialized (AIP-156: https://google.aip.dev/156).
   */
  getTeam(session: string): TeamView {
    const entry = this.requireEntry(session, "NOT_FOUND");
    return toTeamView(entry);
  }

  /**
   * One fixed member's projection (GetTeamMember); NOT_FOUND while
   * unmaterialized or when the member id is outside the fixed roster. The
   * output-only `system_prompt` is read off the member instance's live
   * assembly surface ({@link readMemberSystemPrompt}) — the same prompt the
   * official loop feeds to the model, never a separate re-composition (FR-016,
   * specs/059-agent-v2-team-mode/contracts/web-views.md §5).
   *
   * A concurrent refresh (UpdateTeam) may tear the entry down between the
   * generation snapshot and the asynchronous assembly: the disposed member
   * scope drops its sections, so an assembly crossing the teardown would
   * render a partial (global-layers-only) prompt — neither the old nor the
   * new instance's content (specs/059-agent-v2-team-mode/contracts/
   * team-api.md §1 刷新语义). The read therefore verifies the entry
   * generation after every await and re-reads the current entry, so a refresh
   * race serves the new instance's content and a failure from a superseded
   * instance is retried rather than surfaced as a live assembly error; a team
   * churning through back-to-back refreshes yields NOT_FOUND once the attempt
   * budget is spent.
   */
  async getTeamMember(session: string, member: string): Promise<TeamMemberView> {
    if (member !== "player" && member !== "planner") {
      throw new TeamSessionError(
        "NOT_FOUND",
        `team member "${member}" does not exist; the roster is player/planner`,
      );
    }
    for (let attempt = 0; attempt < MEMBER_READ_ATTEMPTS; attempt += 1) {
      const entry = this.requireEntry(session, "NOT_FOUND");
      // The live handle comes off the orchestration's member read surface
      // (TeamOrchestrator.member) — the instance the orchestrator created and
      // drives, so a refresh is followed naturally.
      const handle = entry.orchestrator.member(member);
      if (handle === undefined) {
        throw new TeamSessionError(
          "NOT_FOUND",
          `team member "${member}" is not materialized`,
        );
      }
      try {
        const systemPrompt = await readMemberSystemPrompt(handle.agent);
        // Linearization point: only the generation that is still live after
        // the await may be served.
        if (this.liveEntry(session) === entry) {
          return { ...memberStateView(entry, member), systemPrompt };
        }
      } catch (err) {
        // An assembly failure of the still-live generation is a genuine
        // INTERNAL; a failure from a superseded instance is part of the
        // teardown and re-samples the current generation below.
        if (this.liveEntry(session) === entry) {
          throw err;
        }
      }
    }
    throw new TeamSessionError(
      "NOT_FOUND",
      `team for session ${session} is being refreshed; retry the read`,
    );
  }

  /** The merged team sequence snapshot (ListTeamMessages data source). */
  listTeamMessages(session: string): TeamMergeEntry[] {
    const entry = this.requireEntry(session, "NOT_FOUND");
    return entry.history.listTeamMessages();
  }

  /** One member's view snapshot (ListMemberMessages data source). */
  listMemberMessages(session: string, member: string): MemberViewEntry[] {
    const entry = this.requireEntry(session, "NOT_FOUND");
    if (member !== "player" && member !== "planner") {
      throw new TeamSessionError(
        "NOT_FOUND",
        `team member "${member}" does not exist; the roster is player/planner`,
      );
    }
    return entry.history.listMemberMessages(member);
  }

  /**
   * Materialize (or refresh) the session's team singleton — the UpdateTeam
   * semantics (contracts/team-api.md §2): an existing team is torn down
   * first (in-flight member turn terminated with turn_end{ABORTED}, queued
   * messages voided, both members released with all history), then a fresh
   * team is built with the given configuration — even when the configuration
   * is unchanged (refresh folded into Update). Validation of both presets
   * (existence + role match) and the models happened before any teardown;
   * this method cannot produce a half-materialized state. Concurrent
   * materializations of one session serialize; different sessions never
   * block each other.
   */
  async materialize(session: string, options: MaterializeTeamOptions): Promise<TeamView> {
    const previous = this.materializations.get(session) ?? Promise.resolve();
    const job = previous.then(() => this.doMaterialize(session, options));
    this.materializations.set(
      session,
      job.then(
        () => undefined,
        () => undefined,
      ),
    );
    return job;
  }

  private async doMaterialize(
    session: string,
    options: MaterializeTeamOptions,
  ): Promise<TeamView> {
    const parsedSession = parseSessionResource(session);
    if (parsedSession === undefined) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `session must be a game session resource name ("templates/{template}/sessions/{session}"), got "${session}"`,
      );
    }
    // Layer 1 — structure (scene-agnostic): members non-empty, every member
    // role non-empty, preset a valid resource name under the session
    // template, model optional (data-model.md §4).
    this.validateMemberStructure(options.members, parsedSession.template);
    // Layer 2 — saolei scene (agent_v2 is the scene host): exactly two
    // members whose roles are exactly {"player", "planner"}.
    this.validateSaoleiMembers(options.members);
    const player = this.memberFor(options.members, "player");
    const planner = this.memberFor(options.members, "planner");
    // The effective member-model values stay composite (`provider/model-id`);
    // the orchestrator receives the split (provider, model) route below
    // (specs/063-llm-reliability-opencode-go/contracts/model-selection.md §3).
    const playerModel = player.model || DEFAULT_MODEL;
    const plannerModel = planner.model || DEFAULT_MODEL;

    // Fail-fast validation BEFORE any teardown (data-model.md §4): preset
    // existence + role equality, then each selector's provider catalog. The
    // model validators return the parsed route materialization forwards.
    await this.validateMemberPreset(player.preset, "player");
    await this.validateMemberPreset(planner.preset, "planner");
    const playerRoute = await this.validateModel(playerModel);
    const plannerRoute = await this.validateModel(plannerModel);

    const existing = this.teams.get(session);
    if (existing !== undefined) {
      this.teams.delete(session);
      await this.teardownEntry(existing);
    }

    // The singleton's create_time survives re-materialization; update_time
    // refreshes on every UpdateTeam (contracts/team-api.md §2).
    const createTime = existing?.createTime ?? new Date();
    const updateTime = new Date();

    const streams = new Set<TurnStream>();
    const broadcast = (event: ChatEvent): void => {
      for (const stream of [...streams]) {
        try {
          stream.write(event);
        } catch (err) {
          // A disconnected peer must not take the orchestration down: drop
          // the stream, keep the team running (contract §3.3).
          streams.delete(stream);
          info("team stream write failed (detached)", {
            session,
            error: err instanceof Error ? err.message : String(err),
          });
        }
      }
    };
    const history = new TeamHistory(session, broadcast);
    const orchestrator = this.createOrchestrator();
    try {
      await orchestrator.materialize({
        session,
        // The memory scope key halves are the business template and the
        // session ID — the full session resource name would double the
        // prefix in the memory service parent (T023 wiring fix).
        memoryScope: { template: parsedSession.template, session: parsedSession.session },
        goal: TEAM_GOAL,
        player: {
          preset: this.presetId(player.preset),
          provider: playerRoute.provider,
          model: playerRoute.model,
        },
        planner: {
          preset: this.presetId(planner.preset),
          provider: plannerRoute.provider,
          model: plannerRoute.model,
        },
      });
    } catch (err) {
      // The orchestrator rolled every created member back; no entry and no
      // history survive, so the caller can retry (no half-materialized team).
      error("team materialization failed", {
        session,
        error: err instanceof Error ? err.message : String(err),
      });
      await orchestrator.dispose().catch(() => undefined);
      throw err;
    }

    const playerHandle = orchestrator.member("player");
    const plannerHandle = orchestrator.member("planner");
    if (playerHandle === undefined || plannerHandle === undefined) {
      await orchestrator.dispose().catch(() => undefined);
      throw new Error("team materialization invariant violated: members are missing");
    }
    const members: Record<MemberRole, MemberRuntime> = {
      // Member runtimes keep the composite selector for the projections; the
      // orchestrator already received the split route above
      // (contracts/model-selection.md §3).
      player: this.createMemberRuntime(orchestrator, playerHandle, "player", player.preset, playerModel, session, history, broadcast),
      planner: this.createMemberRuntime(orchestrator, plannerHandle, "planner", planner.preset, plannerModel, session, history, broadcast),
    };
    const entry: TeamEntry = {
      sessionName: session,
      template: parsedSession.template,
      orchestrator,
      history,
      members,
      streams,
      createTime,
      updateTime,
      quiescenceWatch: null,
      disposed: false,
    };
    this.teams.set(session, entry);
    info("team materialized", {
      session,
      template: parsedSession.template,
      members: options.members.map((member) => `${member.role}:${member.preset}`).join(","),
      playerModel,
      plannerModel,
      replaced: existing !== undefined,
    });
    return toTeamView(entry);
  }

  /**
   * One member's runtime: the orchestrator-owned handle plus the
   * session-lifetime collector feeding the shared history projections and
   * the active stream set.
   */
  private createMemberRuntime(
    orchestrator: TeamOrchestrator,
    handle: AgentHandle,
    role: MemberRole,
    preset: string,
    model: string,
    session: string,
    history: TeamHistory,
    broadcast: (event: ChatEvent) => void,
  ): MemberRuntime {
    // The orchestrator owns creation and teardown; this guard documents the
    // ownership invariant (the handle must belong to this orchestrator).
    if (orchestrator.member(role) !== handle) {
      throw new Error(`team materialization invariant violated: ${role} handle mismatch`);
    }
    const collector = new MemberCollector(
      this.ctx,
      handle.agent as Agent,
      role,
      session,
      history,
      broadcast,
    );
    return { role, preset, model, handle, collector };
  }

  /**
   * Shutdown path only: end every active stream, dispose every team entry,
   * then the composition's root fiber — the graceful order (bootstrap: stop
   * server → dispose teams → dispose fiber → mongo → flush OTel).
   */
  async shutdown(): Promise<void> {
    const entries = [...this.teams.values()];
    this.teams.clear();
    const results = await Promise.allSettled(entries.map((entry) => this.teardownEntry(entry)));
    await this.ctx.fiber.dispose();
    const failure = results.find(
      (result): result is PromiseRejectedResult => result.status === "rejected",
    );
    if (failure) {
      throw failure.reason;
    }
  }

  /** The live entry for a session, or undefined while unmaterialized. */
  private liveEntry(session: string): TeamEntry | undefined {
    const entry = this.teams.get(session);
    if (entry === undefined || entry.disposed) {
      return undefined;
    }
    return entry;
  }

  private requireEntry(session: string, code: TeamSessionErrorCode): TeamEntry {
    const entry = this.liveEntry(session);
    if (entry === undefined) {
      throw new TeamSessionError(
        code,
        `team not materialized for session ${session}; send UpdateTeam first`,
      );
    }
    return entry;
  }

  /**
   * Refresh teardown: settle the in-flight member turn as ABORTED (the
   * orchestrator's dispose cancellation converges to idle, and the collector
   * maps it to the terminal frame), release both members through the
   * orchestrator (its rollback/ownership contract), then end every active
   * stream — the old lifecycle's subscriptions die with it.
   */
  private async teardownEntry(entry: TeamEntry): Promise<void> {
    entry.disposed = true;
    const active = entry.orchestrator.snapshot().active;
    if (active !== null) {
      entry.members[active].collector.markOutcome({ status: "ABORTED" });
    }
    await entry.orchestrator.dispose();
    entry.members.player.collector.dispose();
    entry.members.planner.collector.dispose();
    this.endStreams(entry);
  }

  /** Attach one stream to the session's active set; returns the detach. */
  private attach(entry: TeamEntry, stream: TurnStream): () => void {
    entry.streams.add(stream);
    let detached = false;
    return () => {
      if (detached) {
        return;
      }
      detached = true;
      entry.streams.delete(stream);
    };
  }

  /** End every active stream at a team static point (natural / cancel). */
  private endStreams(entry: TeamEntry): void {
    const streams = [...entry.streams];
    entry.streams.clear();
    for (const stream of streams) {
      try {
        stream.end();
      } catch (err) {
        info("team stream end failed (already closed)", {
          session: entry.sessionName,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    }
  }

  /**
   * Terminate every active stream at an orchestration failure: the failure
   * is surfaced as a stream error (the gRPC adapter maps it to INTERNAL —
   * contracts/team-api.md §6) and as a repo-logger line, never as a clean
   * EOF that would disguise the stall.
   */
  private failStreams(entry: TeamEntry, failure: OrchestratorFailure): void {
    error("team stream closed after an orchestration failure", {
      session: entry.sessionName,
      phase: failure.phase,
      member: failure.member ?? "",
      error: failure.message,
    });
    const streams = [...entry.streams];
    entry.streams.clear();
    for (const stream of streams) {
      try {
        if (stream.fail !== undefined) {
          stream.fail({ code: "INTERNAL", message: failure.message });
        } else {
          stream.end();
        }
      } catch (err) {
        info("team stream error close failed (already closed)", {
          session: entry.sessionName,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    }
  }

  /**
   * Arm the per-entry quiescence watcher: resolve the Send-established team
   * stream lifecycle at the orchestrator's static point (no in-flight turn
   * and no pending digestible input — contracts/team-api.md §3.1). New
   * activity started while the watcher settles re-arms through the
   * snapshot re-check; a Send always arms a watcher, so an already-ended
   * epoch cannot swallow a later stream.
   *
   * A paused orchestrator has no scheduled pump — `whenQuiescent()` resolves
   * immediately — so waiting for active/queued alone would spin the
   * microtask queue. The pause also carries the terminal state: a failed
   * step (surfaced as a stream error) or the Cancel static point.
   */
  private watchQuiescence(entry: TeamEntry): void {
    if (entry.quiescenceWatch !== null || entry.disposed) {
      return;
    }
    const watch = (async () => {
      for (;;) {
        await entry.orchestrator.whenQuiescent();
        const snapshot = entry.orchestrator.snapshot();
        if (snapshot.paused || (snapshot.active === null && snapshot.queued === 0)) {
          break;
        }
      }
      const snapshot = entry.orchestrator.snapshot();
      entry.quiescenceWatch = null;
      if (entry.disposed) {
        return;
      }
      if (snapshot.failed && snapshot.lastError !== null) {
        this.failStreams(entry, snapshot.lastError);
      } else {
        this.endStreams(entry);
      }
    })();
    entry.quiescenceWatch = watch;
  }

  /** The production orchestrator, composed over the ctx plus injected seams. */
  private createOrchestrator(): TeamOrchestrator {
    return new TeamOrchestrator(this.ctx, {
      compose: this.composeSeam(),
      loadPlannerMemory: this.loadPlannerMemoryBinding(),
      logger: this.deps.logger ?? {
        error: (message, context) =>
          error(message, {
            session: context.session,
            phase: context.phase,
            member: context.member ?? "",
            code: context.code,
            error: context.error,
          }),
      },
      ...(this.deps.agents === undefined ? {} : { agents: this.deps.agents }),
      ...(this.deps.team === undefined ? {} : { team: this.deps.team }),
      ...(this.deps.mountPlayerRuntime === undefined
        ? {}
        : { mountPlayerRuntime: this.deps.mountPlayerRuntime }),
      ...(this.deps.provider === undefined ? {} : { provider: this.deps.provider }),
    });
  }

  /** Preset compose seam: host injection first, else `ctx.presetAuthoring`. */
  private composeSeam(): ComposePreset {
    if (this.deps.authoring !== undefined) {
      return (preset) => this.deps.authoring!.compose(preset);
    }
    const authoring = this.ctx.get("presetAuthoring") as
      | Pick<PresetAuthoringService, "compose" | "get">
      | undefined;
    if (authoring === undefined) {
      throw new Error("team sessions: ctx.presetAuthoring is not composed");
    }
    return (preset) => authoring.compose(preset);
  }

  /**
   * Bind the planner memory prefetch (T021): the explicit test override
   * first, else the composed `ctx.plannerMemory` host-row service face
   * (common/js/dsh-plugins/memory/src/index.ts; the `memory` row in
   * cordis.yml provides it). The method is a closure, so the extracted
   * `load` needs no receiver. A missing service is a composition error —
   * fail-loud rather than silently materializing a planner without its
   * memory snapshot (specs/059-agent-v2-team-mode/contracts/dsh-plugins.md
   * §2/§3).
   */
  private loadPlannerMemoryBinding(): LoadPlannerMemory {
    if (this.deps.loadPlannerMemory !== undefined) {
      return this.deps.loadPlannerMemory;
    }
    const memory = this.ctx.get("plannerMemory") as
      | { load?: LoadPlannerMemory }
      | undefined;
    const load = memory?.load;
    if (load === undefined) {
      throw new Error(
        "team sessions: ctx.plannerMemory is not composed; the memory host row is required to materialize a team",
      );
    }
    return load;
  }

  /**
   * Layer 1 (scene-agnostic) structure check: members non-empty, every
   * member role non-empty, preset a valid preset resource name under the
   * session template, model free to be empty (deployment default).
   */
  private validateMemberStructure(
    members: readonly TeamMemberOptions[],
    template: string,
  ): void {
    if (members.length === 0) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        "team.members must not be empty",
      );
    }
    for (const member of members) {
      if (member.role === "") {
        throw new TeamSessionError(
          "INVALID_ARGUMENT",
          "every team member must carry a non-empty role",
        );
      }
      const parsed = parsePresetResource(member.preset);
      if (parsed === undefined) {
        throw new TeamSessionError(
          "INVALID_ARGUMENT",
          `member "${member.role}" preset must be a preset resource name ("templates/{template}/presets/{preset}"), got "${member.preset}"`,
        );
      }
      if (parsed.template !== template) {
        throw new TeamSessionError(
          "INVALID_ARGUMENT",
          `member "${member.role}" preset template ${parsed.template} does not match session template ${template}`,
        );
      }
    }
  }

  /**
   * Layer 2 (saolei scene): exactly two members whose roles are exactly
   * {"player", "planner"} — the scene's fixed roster, enforced here because
   * the proto is a scene-agnostic primitive.
   */
  private validateSaoleiMembers(members: readonly TeamMemberOptions[]): void {
    if (members.length !== 2) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `saolei team materialization requires exactly 2 members (player and planner), got ${members.length}`,
      );
    }
    const roles = members.map((member) => member.role);
    if (!roles.includes("player") || !roles.includes("planner")) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `saolei team member roles must be exactly {"player", "planner"}, got [${roles.join(", ")}]`,
      );
    }
  }

  /** The scene member entry for a validated role. */
  private memberFor(members: readonly TeamMemberOptions[], role: MemberRole): TeamMemberOptions {
    const found = members.find((member) => member.role === role);
    if (found === undefined) {
      // validateSaoleiMembers already guarantees presence; this is a
      // programming-error backstop.
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `saolei team member "${role}" is missing`,
      );
    }
    return found;
  }

  /** The preset id segment of a validated preset resource name. */
  private presetId(presetName: string): string {
    const parsed = parsePresetResource(presetName);
    if (parsed === undefined) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `preset must be a preset resource name ("templates/{template}/presets/{preset}"), got "${presetName}"`,
      );
    }
    return parsed.preset;
  }

  /**
   * Validate one member preset: it exists and its scene role equals the
   * member role (string equality — both are scene vocabulary).
   */
  private async validateMemberPreset(presetName: string, role: MemberRole): Promise<void> {
    const authoring =
      this.deps.authoring ??
      (this.ctx.get("presetAuthoring") as Pick<PresetAuthoringService, "get"> | undefined);
    if (authoring === undefined) {
      throw new Error("team sessions: ctx.presetAuthoring is not composed");
    }
    const presetId = this.presetId(presetName);
    let view: { role?: string };
    try {
      view = await authoring.get(presetId);
    } catch (err) {
      // Only a genuine store miss is the unknown-preset case. A store outage
      // keeps its own error (INTERNAL) and cause chain instead of being
      // rewritten to INVALID_ARGUMENT, matching the preset-api error surface
      // (specs/059-agent-v2-team-mode/contracts/preset-api.md §2 错误语义).
      if (err instanceof PresetAuthoringError && err.code === "NOT_FOUND") {
        throw new TeamSessionError(
          "INVALID_ARGUMENT",
          `unknown preset "${presetId}"; create a preset in the ${role} pool first`,
        );
      }
      throw err;
    }
    if (view.role !== role) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `scene check failed: preset "${presetId}" carries role "${view.role ?? ""}" but member "${role}" requires the matching role`,
      );
    }
  }

  /**
   * Validate one effective model selector against the selected provider's
   * deployment catalog (shared with ListModels) and return the parsed route
   * materialization forwards. The selector is the composite
   * `provider/model-id` form; a legacy bare id or an empty segment is
   * INVALID_ARGUMENT — never a silent provider guess
   * (specs/063-llm-reliability-opencode-go/contracts/model-selection.md §3).
   */
  private async validateModel(selector: string): Promise<ParsedModelIdentifier> {
    const route = parseModelIdentifier(selector);
    const catalog =
      this.deps.listModels ??
      ((provider: string) => this.ctx.llm.listModels(provider));
    const models = await catalog(route.provider);
    if (!models.some((entry) => entry.id === route.model)) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `unknown model "${selector}" for provider "${route.provider}"; ${COMPOSITE_MODEL_HINT}`,
      );
    }
    return route;
  }

  /** Map an orchestration state error onto the request-level error surface. */
  private mapOrchestratorError(err: unknown): unknown {
    if (err instanceof OrchestratorStateError) {
      return new TeamSessionError("FAILED_PRECONDITION", err.message);
    }
    return err;
  }
}

/**
 * One member's configured state: the Team snapshot member projection
 * (GetTeam/UpdateTeam). `system_prompt` stays empty — it is served only by
 * GetTeamMember ({@link TeamSessions.getTeamMember}; the proto field is
 * OUTPUT_ONLY and the assembly read is a separate, on-demand face).
 */
function memberStateView(entry: TeamEntry, role: MemberRole): TeamMemberView {
  const member = entry.members[role];
  return {
    name: `${entry.sessionName}/team/members/${role}`,
    role,
    // The configured full resource name; mirrored from the materialization
    // request so the view is stable even if the store record changes.
    preset: member.preset,
    model: member.model,
    systemPrompt: "",
  };
}

function toTeamView(entry: TeamEntry): TeamView {
  // The single merged active-member value (contracts/team-api.md §1): the
  // in-flight driving member wins while a turn runs; at rest the next input's
  // owner (activation) is served. A materialized entry always has a role.
  const snapshot = entry.orchestrator.snapshot();
  return {
    name: `${entry.sessionName}/team`,
    activeMember: snapshot.active ?? snapshot.activation,
    members: [memberStateView(entry, "player"), memberStateView(entry, "planner")],
    createTime: entry.createTime,
    updateTime: entry.updateTime,
  };
}
