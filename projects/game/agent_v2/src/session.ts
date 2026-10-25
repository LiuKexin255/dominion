/**
 * session.ts — game session ↔ dsh team mapping for agent_v2.
 *
 * `TeamSessions` owns the materialization registry over the team
 * orchestrator (`@dominion/dsh-saolei-loop`): one {@link TeamOrchestrator}
 * per materialized session, the entry's unified member runtimes (the two LLM
 * members plus the announce-only announcer), the session-lifetime team
 * history projections, and the active team streams.
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
 * The member projections consume the team's unified member event stream — a
 * single subscription per entry, dispatched by envelope role
 * (specs/077-announcer-member-unification/contracts/member-event-stream.md
 * §5): the announcer member's announcements (game-over stats) become
 * {@link TeamHistory.appendAnnouncement} merge entries plus their
 * `team_message` frames, and the agent members' events feed their collectors.
 * The subscription detaches with the team entry
 * (specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §4).
 *
 * The Context injected at construction plus the optional {@link
 * TeamSessionsDeps} are the dependency seams: unit tests pass doubles
 * instead of intercepting modules (style/javascript.md Mock convention).
 */

import { error, info } from "@dominion/common-js-logs";
import type { AgentHandle } from "@deepseek-ai/dsh-agent";
import type { TeamMemberEvent } from "@dominion/dsh-team";
import {
  ANNOUNCER_MEMBER_ROLE,
  OrchestratorStateError,
  TeamOrchestrator,
} from "@dominion/dsh-saolei-loop";
import type {
  AgentCreationSeam,
  ComposePreset,
  OrchestratorFailure,
  OrchestratorLogger,
  TeamSeam,
} from "@dominion/dsh-saolei-loop";
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import type { PresetAuthoringService } from "@dominion/dsh-preset-authoring";
import type { ChatEvent } from "../agent_v2_types/projects/game/ChatEvent.js";
import type { DshContext } from "./dsh.js";
import { MemberCollector, TeamHistory, isAgentMemberRole } from "./history.js";
import type {
  AgentMemberRole,
  MemberViewEntry,
  TeamMergeEntry,
  TurnStream,
} from "./history.js";
import { loopErrorText } from "./orchestration.js";
import { SCENES, sceneNotWiredMessage, wiredScene } from "./scenes.js";
import { readMemberSystemPrompt } from "./system-prompt.js";

export type { MemberRole, MemberViewEntry, TeamMergeEntry, TurnStream } from "./history.js";
export { LOOP_ERROR_TEXT, loopErrorText } from "./orchestration.js";

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
  /** Member role (scene vocabulary; saolei: "player" / "planner" / "announcer"). */
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
  /** Orchestration failure reporter; defaults to the repo logger face. */
  readonly logger?: OrchestratorLogger;
  /** Provider route for both members; defaults to {@link PROVIDER}. */
  readonly provider?: string;
}

/**
 * One agent-backed member's runtime: the orchestrator-owned handle plus the
 * session-lifetime collector feeding the shared history projections.
 */
interface AgentMemberRuntime {
  readonly kind: "agent";
  readonly role: AgentMemberRole;
  readonly preset: string;
  readonly model: string;
  readonly handle: AgentHandle;
  readonly collector: MemberCollector;
}

/**
 * The announce-only member's runtime: a registered member carrying no agent
 * runtime (no handle, no collector) — hence no member view
 * (specs/077-announcer-member-unification/contracts/host-member-parity.md §1,
 * specs/077-announcer-member-unification/data-model.md §3.1).
 */
interface AnnounceOnlyMemberRuntime {
  readonly kind: "announce-only";
  readonly role: typeof ANNOUNCER_MEMBER_ROLE;
}

/**
 * A materialized member of the team entry. The `kind` discriminant is the
 * member-category source: the roster, the member read, the member view and
 * teardown all derive from it rather than test the role string
 * (specs/077-announcer-member-unification/contracts/host-member-parity.md §2).
 */
type MemberRuntime = AgentMemberRuntime | AnnounceOnlyMemberRuntime;

interface TeamEntry {
  readonly sessionName: string;
  readonly template: string;
  readonly orchestrator: TeamOrchestrator;
  readonly history: TeamHistory;
  /** Every registered member (player/planner/announcer), in roster order. */
  readonly members: readonly MemberRuntime[];
  /** Detaches the entry's single unified-stream subscription (entry teardown). */
  readonly streamOff: () => void;
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
    const inFlight = agentMemberInFlight(entry);
    if (inFlight !== null) {
      // Known window: the snapshot can report the member active before its
      // collector observed the running transition, in which case the mark is
      // consumed by the first idle that finds an active turn (see
      // MemberCollector.markOutcome).
      inFlight.collector.markOutcome({ status: "CANCELED" });
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
   * unmaterialized or when the member id is outside the scene's roster (plus
   * the announce-only member).
   * The output-only `system_prompt` is read off the member instance's live
   * assembly surface ({@link readMemberSystemPrompt}) — the same prompt the
   * official loop feeds to the model, never a separate re-composition (FR-016,
   * specs/059-agent-v2-team-mode/contracts/web-views.md §5). The announcer is
   * announce-only: it has no agent/handle and no assembled prompt, so it is
   * served as the same empty-form roster entry GetTeam carries
   * (specs/076-agent-v2-optimize/contracts/announcer-member.md §3 item 3).
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
    const parsedSession = parseSessionResource(session);
    if (parsedSession === undefined) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `session must be a game session resource name ("templates/{template}/sessions/{session}"), got "${session}"`,
      );
    }
    const template = parsedSession.template;
    if (!isMemberRole(member)) {
      throw new TeamSessionError(
        "NOT_FOUND",
        `team member "${member}" does not exist; the roster is ${memberRoster(template)}`,
      );
    }
    const entry = this.requireEntry(session, "NOT_FOUND");
    const runtime = findMember(entry, member);
    if (runtime === undefined) {
      throw new TeamSessionError(
        "NOT_FOUND",
        `team member "${member}" is not materialized`,
      );
    }
    // The announce-only member carries no agent, so there is no assembly to
    // read and no generation to retry: it is served synchronously as the same
    // empty-form roster entry GetTeam carries
    // (specs/076-agent-v2-optimize/contracts/announcer-member.md §3 item 3).
    if (runtime.kind === "announce-only") {
      return memberStateView(entry, runtime);
    }
    for (let attempt = 0; attempt < MEMBER_READ_ATTEMPTS; attempt += 1) {
      // Re-sample the live generation: a concurrent refresh swaps the entry
      // between attempts.
      const generation = this.requireEntry(session, "NOT_FOUND");
      const agentMember = findAgentMember(generation, member);
      if (agentMember === undefined) {
        throw new TeamSessionError(
          "NOT_FOUND",
          `team member "${member}" is not materialized`,
        );
      }
      // The live handle comes off the orchestration's member read surface
      // (TeamOrchestrator.member) — the instance the orchestrator created and
      // drives, so a refresh is followed naturally.
      const handle = generation.orchestrator.member(agentMember.role);
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
        if (this.liveEntry(session) === generation) {
          return { ...memberStateView(generation, agentMember), systemPrompt };
        }
      } catch (err) {
        // An assembly failure of the still-live generation is a genuine
        // INTERNAL; a failure from a superseded instance is part of the
        // teardown and re-samples the current generation below.
        if (this.liveEntry(session) === generation) {
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
    const runtime = findMember(entry, member);
    // Only agent members own a view: an announce-only member (and any unknown
    // role) has no member-view projection
    // (specs/077-announcer-member-unification/contracts/host-member-parity.md §2).
    if (runtime === undefined || runtime.kind !== "agent") {
      throw new TeamSessionError(
        "NOT_FOUND",
        `team member "${member}" has no message view; the member-view roster is ${sceneRoster(entry.template).join("/")}`,
      );
    }
    return entry.history.listMemberMessages(runtime.role);
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
    // Scene wiring gate — BEFORE any validation: an unwired scene rejects
    // every roster shape (empty members included) with the same error, so no
    // validation, orchestration, or storage path is entered; the RPC entry
    // point applies the same gate before its structure checks
    // (specs/079-sts2-template-scaffold/contracts/scene-dispatch.md §3 item 1).
    const scene = wiredScene(parsedSession.template);
    if (scene === undefined) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        sceneNotWiredMessage(parsedSession.template),
      );
    }
    // Layer 1 — structure (scene-agnostic): members non-empty, every member
    // role non-empty, preset a valid resource name under the session
    // template, model optional (data-model.md §4).
    this.validateMemberStructure(options.members, parsedSession.template);
    // Layer 2 — scene roster: the template's scene table fixes the member
    // count and role set (projects/game/agent_v2/src/scenes.ts;
    // specs/079-sts2-template-scaffold/data-model.md §2).
    this.validateSceneRoster(options.members, parsedSession.template);
    const [playerRole, plannerRole] = orchestratedRoles(scene.roster);
    const player = this.memberFor(options.members, playerRole, parsedSession.template);
    const planner = this.memberFor(options.members, plannerRole, parsedSession.template);
    // The effective member-model values stay composite (`provider/model-id`);
    // the orchestrator receives the split (provider, model) route below
    // (specs/063-llm-reliability-opencode-go/contracts/model-selection.md §3).
    const playerModel = player.model || DEFAULT_MODEL;
    const plannerModel = planner.model || DEFAULT_MODEL;

    // Fail-fast validation BEFORE any teardown (data-model.md §4): preset
    // existence + role equality, then each selector's provider catalog. The
    // model validators return the parsed route materialization forwards.
    await this.validateMemberPreset(player.preset, playerRole);
    await this.validateMemberPreset(planner.preset, plannerRole);
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
        // materialization source planner preset id: the planner's long-term
        // memory is mounted on the preset it was materialized from, so the
        // scope stays a bare pair of ids rather than a full resource name
        // (specs/071-memory-preset-scope/contracts/dsh-plugins.md §4).
        memoryScope: { template: parsedSession.template, preset: this.presetId(planner.preset) },
        goal: scene.teamGoal,
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

    const playerHandle = orchestrator.member(playerRole);
    const plannerHandle = orchestrator.member(plannerRole);
    if (playerHandle === undefined || plannerHandle === undefined) {
      await orchestrator.dispose().catch(() => undefined);
      throw new Error("team materialization invariant violated: members are missing");
    }
    if (orchestrator.announcer === undefined) {
      await orchestrator.dispose().catch(() => undefined);
      throw new Error("team materialization invariant violated: the announcer member is missing");
    }
    const members: readonly MemberRuntime[] = [
      // Agent runtimes keep the composite selector for the projections; the
      // orchestrator already received the split route above
      // (contracts/model-selection.md §3).
      this.createMemberRuntime(orchestrator, playerHandle, playerRole, player.preset, playerModel, session, history, broadcast),
      this.createMemberRuntime(orchestrator, plannerHandle, plannerRole, planner.preset, plannerModel, session, history, broadcast),
      // The announcer is a registered member with no agent runtime: it is held
      // as its announce-only kind rather than a parallel field/roster special
      // case (specs/077-announcer-member-unification/contracts/host-member-parity.md §1).
      { kind: "announce-only", role: ANNOUNCER_MEMBER_ROLE },
    ];
    // The single member-event consumption path (spec FR-002): subscribe once to
    // the orchestrator's zero-shape pass-through of the team's unified stream
    // and dispatch by envelope role — an agent member's envelopes go to its
    // collector, the announce-only member's log productions to the merge
    // history (specs/077-announcer-member-unification/contracts/
    // member-event-stream.md §5).
    const streamOff = orchestrator.teamEvents().subscribe((envelope) => {
      const member = members.find((candidate) => candidate.role === envelope.role);
      if (member === undefined) {
        return;
      }
      if (member.kind === "announce-only") {
        projectAnnouncement(history, member.role, envelope);
        return;
      }
      member.collector.applyEvent(envelope);
    });
    const entry: TeamEntry = {
      sessionName: session,
      template: parsedSession.template,
      orchestrator,
      history,
      members,
      streamOff,
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
    role: AgentMemberRole,
    preset: string,
    model: string,
    session: string,
    history: TeamHistory,
    broadcast: (event: ChatEvent) => void,
  ): AgentMemberRuntime {
    // The orchestrator owns creation and teardown; this guard documents the
    // ownership invariant (the handle must belong to this orchestrator).
    if (orchestrator.member(role) !== handle) {
      throw new Error(`team materialization invariant violated: ${role} handle mismatch`);
    }
    const collector = new MemberCollector(
      String(handle.agent.id),
      role,
      session,
      history,
      broadcast,
    );
    return { kind: "agent", role, preset, model, handle, collector };
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
   * orchestrator (its rollback/ownership contract), then detach the entry's
   * single stream subscription and end every active stream — the old
   * lifecycle's subscriptions die with it.
   *
   * The stream detach follows the orchestrator release, not precedes it: the
   * ABORTED terminal frame is itself a member event of that stream, so the
   * collector must still be subscribed while dispose converges. No old
   * lifecycle production can reach the old projection afterwards — the team
   * registration's member subscriptions drop inside dispose (the announcer's
   * included), and the entry is no longer live.
   */
  private async teardownEntry(entry: TeamEntry): Promise<void> {
    entry.disposed = true;
    const inFlight = agentMemberInFlight(entry);
    if (inFlight !== null) {
      inFlight.collector.markOutcome({ status: "ABORTED" });
    }
    await entry.orchestrator.dispose();
    entry.streamOff();
    // Release every member from the unified list: agent members drop their
    // turn bookkeeping, the announce-only member has none to release
    // (specs/077-announcer-member-unification/contracts/host-member-parity.md §2).
    for (const member of entry.members) {
      if (member.kind === "agent") {
        member.collector.dispose();
      }
    }
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

  /**
   * End every active stream at a team static point (natural quiescence,
   * cancel, or a member-turn failure whose turn_end ERROR frame is already
   * on the stream).
   */
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
   * Terminate every active stream at an orchestration-layer failure
   * (`lastError.origin === "orchestration"`): the failure is surfaced as a
   * stream error (the gRPC adapter maps it to INTERNAL —
   * specs/059-agent-v2-team-mode/contracts/team-api.md §6) and as a
   * repo-logger line, never as a clean EOF that would disguise the stall.
   * A member-turn failure settles through {@link endStreams} instead: its
   * turn_end ERROR frame is already on the stream
   * (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §2;
   * design: specs/063-llm-reliability-opencode-go/design-session-stream-settlement.md §1).
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
          stream.fail({
            code: "INTERNAL",
            message: `${failure.message} — ${loopErrorText(failure.code)}`,
          });
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
   * and no pending digestible input — contracts/team-api.md §3.1). The
   * snapshot fields carry the new semantics: `paused` is the flow's frozen
   * state (a failed or aborted activity), and `queued` is the active member's
   * delivered-but-unconsumed input — the steered-in-flight count plus the
   * member inbox pending state (specs/061-team-queue-steer/research.md R4).
   * New activity started while the watcher settles re-arms through the
   * snapshot re-check; a Send always arms a watcher, so an already-ended
   * epoch cannot swallow a later stream.
   *
   * A frozen orchestrator has no scheduled pump — `whenQuiescent()` resolves
   * immediately — so waiting for active/queued alone would spin the microtask
   * queue. The freeze also carries the terminal state, settled by the failure
   * origin: an orchestration-layer failure closes the streams with an error
   * ({@link failStreams}), while a member-turn failure — like a cancel — ends
   * them cleanly ({@link endStreams}), the turn_end ERROR frame having already
   * been sunk by the member history collector before this watcher's microtask
   * (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §2;
   * design: specs/063-llm-reliability-opencode-go/design-session-stream-settlement.md §1/§3).
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
      if (snapshot.lastError?.origin === "orchestration") {
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
   * Layer 2 (scene roster): the template's scene table supplies the fixed
   * member count and role set, enforced here because the proto is a
   * scene-agnostic primitive
   * (specs/079-sts2-template-scaffold/data-model.md §2).
   */
  private validateSceneRoster(members: readonly TeamMemberOptions[], template: string): void {
    const roster = SCENES[template].roster;
    if (members.length !== roster.length) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `${template} team materialization requires exactly ${roster.length} members (${roster.join(" and ")}), got ${members.length}`,
      );
    }
    const roles = members.map((member) => member.role);
    if (!roster.every((role) => roles.includes(role))) {
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `${template} team member roles must be exactly {${roster.map((role) => `"${role}"`).join(", ")}}, got [${roles.join(", ")}]`,
      );
    }
  }

  /** The scene member entry for a validated roster role. */
  private memberFor(
    members: readonly TeamMemberOptions[],
    role: AgentMemberRole,
    template: string,
  ): TeamMemberOptions {
    const found = members.find((member) => member.role === role);
    if (found === undefined) {
      // validateSceneRoster already guarantees presence; this is a
      // programming-error backstop.
      throw new TeamSessionError(
        "INVALID_ARGUMENT",
        `${template} team member "${role}" is missing`,
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
  private async validateMemberPreset(presetName: string, role: AgentMemberRole): Promise<void> {
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
 * Whether a wire string names a member of the scene table's roster union (plus
 * the announce-only member): the member-name pre-check is scene-agnostic, and
 * the entry's own roster decides membership afterwards.
 */
function isMemberRole(value: string): boolean {
  return (
    Object.values(SCENES).some((scene) => scene.roster.includes(value)) ||
    value === ANNOUNCER_MEMBER_ROLE
  );
}

/** The scene table's member roster; a template without a scene has none. */
function sceneRoster(template: string): readonly string[] {
  return SCENES[template]?.roster ?? [];
}

/** The roster as rendered in member-name error text: roster + the announce-only member. */
function memberRoster(template: string): string {
  return [...sceneRoster(template), ANNOUNCER_MEMBER_ROLE].join("/");
}

/**
 * The scene's roster in orchestration order: the saolei-loop orchestrator's
 * two agent member slots (its materialize input keys `player`/`planner`), fed
 * from the scene table (projects/game/agent_v2/src/scenes.ts
 * `SceneSpec.roster`). The roster is validated against the request before
 * this point, and the guard below rejects a roster that names anything other
 * than the orchestrated agent roles
 * (specs/079-sts2-template-scaffold/data-model.md §2).
 */
function orchestratedRoles(
  roster: readonly string[],
): readonly [AgentMemberRole, AgentMemberRole] {
  const [first, second] = roster;
  if (
    first === undefined ||
    second === undefined ||
    !isAgentMemberRole(first) ||
    !isAgentMemberRole(second)
  ) {
    throw new TeamSessionError(
      "INVALID_ARGUMENT",
      `the scene roster must name the two orchestrated agent member roles, got [${roster.join(", ")}]`,
    );
  }
  return [first, second];
}

/** The registered member runtime for a role, or undefined when outside the roster. */
function findMember(entry: TeamEntry, role: string): MemberRuntime | undefined {
  return entry.members.find((member) => member.role === role);
}

/** The agent member runtime for a role; an announce-only or unknown role yields undefined. */
function findAgentMember(entry: TeamEntry, role: string): AgentMemberRuntime | undefined {
  const member = findMember(entry, role);
  return member !== undefined && member.kind === "agent" ? member : undefined;
}

/**
 * The agent member whose turn is in flight, or null when the team is
 * quiescent or an announce-only member holds the activation. The
 * announce-only member never runs a turn, so its reported activation has no
 * member turn to settle — the member kind, not the role string, decides
 * (specs/077-announcer-member-unification/contracts/host-member-parity.md §2).
 */
function agentMemberInFlight(entry: TeamEntry): AgentMemberRuntime | null {
  const active = entry.orchestrator.snapshot().active;
  return active === null ? null : (findAgentMember(entry, active) ?? null);
}

/**
 * One member's configured state: the Team snapshot member projection
 * (GetTeam/UpdateTeam). `system_prompt` stays empty — it is served only by
 * GetTeamMember ({@link TeamSessions.getTeamMember}; the proto field is
 * OUTPUT_ONLY and the assembly read is a separate, on-demand face). The
 * announce-only member is the same projection with the empty preset/model
 * form (specs/077-announcer-member-unification/contracts/host-member-parity.md §2).
 */
function memberStateView(entry: TeamEntry, member: MemberRuntime): TeamMemberView {
  return {
    name: `${entry.sessionName}/team/members/${member.role}`,
    role: member.role,
    // The configured full resource name; mirrored from the materialization
    // request so the view is stable even if the store record changes.
    preset: member.kind === "agent" ? member.preset : "",
    model: member.kind === "agent" ? member.model : "",
    systemPrompt: "",
  };
}

/**
 * Project one announce-only member event into the merge history: the member's
 * log-channel `assistant/message` productions become announcement entries. Its
 * speech text is the text blocks in order — the same projection the team's
 * broadcast renderer applies
 * (common/js/dsh-plugins/team/src/broadcast.ts messageBody). Other channels and
 * event types carry no announcement (specs/077-announcer-member-unification/
 * spec.md FR-002).
 */
function projectAnnouncement(history: TeamHistory, role: string, envelope: TeamMemberEvent): void {
  if (envelope.channel !== "log" || envelope.event.type !== "assistant/message") {
    return;
  }
  const text = envelope.event.data.message.content
    .map((block) => (block.type === "text" ? block.text : ""))
    .join("\n\n");
  if (text !== "") {
    history.appendAnnouncement(role, text);
  }
}

function toTeamView(entry: TeamEntry): TeamView {
  // The single merged active-member value (contracts/team-api.md §1): the
  // in-flight driving member wins while a turn runs; at rest the next input's
  // owner (activation) is served. A materialized entry always has a role. The
  // roster is derived from the entry's unified member collection
  // (specs/076-agent-v2-optimize/contracts/announcer-member.md §3), so a
  // member's roster form follows its kind — the announce-only member carries
  // no preset/model.
  const snapshot = entry.orchestrator.snapshot();
  return {
    name: `${entry.sessionName}/team`,
    activeMember: snapshot.active ?? snapshot.activation,
    members: entry.members.map((member) => memberStateView(entry, member)),
    createTime: entry.createTime,
    updateTime: entry.updateTime,
  };
}
