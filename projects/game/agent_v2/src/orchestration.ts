/**
 * orchestration.ts — the scene-neutral orchestration seam of agent_v2.
 *
 * The host session path drives one {@link SceneOrchestration} per materialized
 * session: the contract is defined here (host side) and every scene's
 * orchestration satisfies it structurally, so the host depends on the
 * implementation while no implementation imports the host
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §7).
 *
 * The seam carries no scene vocabulary: roles are open strings, the member
 * projection is `{role, kind}`, the snapshot has no workflow-position field,
 * and the shared failure codes are the six stable loop codes. Scene-specific
 * derivation (shared prompt text, per-member memory scope, announce-only
 * invariants) stays inside the scene registration module that constructs the
 * orchestration, never on this seam
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §2/§5).
 *
 * Every injection type below is a narrow structural view, so this module
 * imports no scene plugin package — only the framework packages whose public
 * types the seam names (specs/080-host-main-sub-model/quickstart.md §V3 硬门禁).
 */

import type { AgentHandle, CreateAgentOptions } from "@deepseek-ai/dsh-agent";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import type {
  TeamEventStream,
  TeamHandle,
  TeamMemberSource,
  TeamRegistration,
} from "@dominion/dsh-team";

import type { SceneSpec } from "./scenes.js";

/**
 * The orchestration layer's failure classification (six stable codes). The
 * classification binds the recovery dimensions the host presents; the host
 * owns the user-facing text ({@link LOOP_ERROR_TEXT})
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §2).
 */
export type SceneLoopErrorCode =
  | "TRANSIENT"
  | "QUOTA"
  | "CREDENTIAL"
  | "CONFIG"
  | "INPUT_TOO_LARGE"
  | "INTERNAL";

/**
 * The host's user-action guidance for each loop failure code — the
 * presentation half of the error contract: the codes are the orchestration
 * layer's classification
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §2), and each
 * wording below is the 「呈现指导」 column of
 * survey/saolei-member-switch-flow.md §8.1, adopted as the contract wording
 * by specs/076-agent-v2-optimize/spec.md FR-107. The orchestration layer
 * classifies the code; the host owns the text, which stays byte-stable for
 * the clients that already render it.
 */
export const LOOP_ERROR_TEXT: Readonly<Record<SceneLoopErrorCode, string>> = {
  TRANSIENT: "服务暂不可用，稍后重试",
  QUOTA: "配额耗尽",
  CREDENTIAL: "检查 API 凭证",
  CONFIG: "模型配置错误",
  INPUT_TOO_LARGE: "上下文超限",
  INTERNAL: "内部错误",
};

/** The host's user-action guidance for one loop failure code. */
export function loopErrorText(code: SceneLoopErrorCode): string {
  return LOOP_ERROR_TEXT[code];
}

/**
 * One caller-supplied member configuration — the scene-agnostic
 * materialization primitive (proto team-member input side). `preset` is a
 * preset id; `provider`/`model` are the already-split route (the host
 * validated both layers and parsed the composite selector before handing
 * the input over; specs/063-llm-reliability-opencode-go/contracts/model-selection.md §3).
 */
export interface SceneMemberConfig {
  /** Member role (open scene vocabulary). */
  readonly role: string;
  /** Preset id the member materializes from. */
  readonly preset: string;
  /** Provider route this member's model belongs to; empty = the default route. */
  readonly provider?: string;
  /** Bare model id; empty = the provider/adapter default. */
  readonly model?: string;
}

/** The materialization input of one session (host: UpdateTeam). */
export interface SceneMaterializeInput {
  /** Session resource name (`templates/{template}/sessions/{session}`). */
  readonly session: string;
  /** Business template id the session belongs to. */
  readonly template: string;
  /** Every configured member, in caller order. */
  readonly members: readonly SceneMemberConfig[];
}

/**
 * One materialized member's projection: the role and the member kind. The
 * host derives the member runtime (agent-backed vs announce-only) from the
 * kind, never from the role string
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §2).
 */
export interface SceneMember {
  /** Member role (open scene vocabulary). */
  readonly role: string;
  /** `agent` owns an agent handle; `announce-only` is a registered voice with no runtime. */
  readonly kind: "agent" | "announce-only";
}

/**
 * Which side of the orchestration produced a failure: the driven member's
 * turn, or the orchestration layer itself. The host's stream settlement is
 * the consumer — a failed member turn ends the streams cleanly, an
 * orchestration-layer failure closes them with an internal error
 * (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §2).
 */
export type SceneFailureOrigin = "member-turn" | "orchestration";

/**
 * The diagnostic half of a failure report: the engine's stable code (when the
 * failure surfaced as one) plus its message. The orchestration transports it
 * for presentation/logging and never interprets it
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §2).
 */
export interface SceneFailureDetail {
  readonly engineCode?: string;
  readonly message: string;
}

/** The last failed orchestration step, exposed through {@link SceneSnapshot}. */
export interface SceneFailure {
  /** Normalized error message of the failed step. */
  readonly message: string;
  /** The member whose drive failed; null when no member step failed. */
  readonly member: string | null;
  /** Loop failure code ({@link SceneLoopErrorCode}). */
  readonly code: SceneLoopErrorCode;
  /** Failure origin — the host's stream-settlement discriminant. */
  readonly origin: SceneFailureOrigin;
  /** The engine code (when any) plus the message, transported for presentation. */
  readonly detail: SceneFailureDetail;
}

/**
 * Read-only orchestration view for the host (status/queue/failure
 * presentation). It deliberately has no workflow-position field: scene
 * workflow vocabulary stays inside the scene orchestration
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §2).
 */
export interface SceneSnapshot {
  readonly materialized: boolean;
  /** The member whose turn is in flight, or null. */
  readonly active: string | null;
  /** The member the current activation belongs to (always a role of the materialized set). */
  readonly activation: string;
  /** Whether the orchestration is frozen (a failed or aborted activity). */
  readonly paused: boolean;
  /** The active member's delivered-but-unconsumed input count. */
  readonly queued: number;
  /** True while the last drive failed (auto-continuation suspended). */
  readonly failed: boolean;
  /** The last drive failure; cleared by the next successful drive. */
  readonly lastError: SceneFailure | null;
}

/**
 * The orchestration contract the host session path consumes
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §2). Host
 * consumption points: materialization (once, after all validation),
 * member-projection derivation, the single unified-event subscription, the
 * stream/quiescence watcher, Send, Cancel, refresh teardown, and shutdown.
 */
export interface SceneOrchestration {
  /**
   * Materialize the session's orchestration from the validated input. Called
   * once per materialization; a failure rolls every created member back and
   * leaves the orchestration unmaterialized (no half-materialized state), so
   * the caller can retry.
   */
  materialize(input: SceneMaterializeInput): Promise<void>;
  /** The materialized member projection, in presentation order; empty while unmaterialized. */
  members(): readonly SceneMember[];
  /** The live agent handle of one member (system-prompt read / ownership check), or undefined. */
  member(role: string): AgentHandle | undefined;
  /**
   * The unified member event stream: subscribed once per materialized session
   * and dispatched by envelope role; the subscription detaches with the entry
   * (specs/080-host-main-sub-model/contracts/scene-registry.md §3).
   */
  teamEvents(): TeamEventStream;
  /** The live orchestration snapshot (queue pre-read, quiescence watcher, view projection). */
  snapshot(): SceneSnapshot;
  /**
   * Accept one user message: in flight the message steers into the active
   * member's inbox, at rest it opens a turn. The host reads the queued-frame
   * decision from {@link snapshot} before the call and does not consume a
   * return value.
   */
  submit(text: string): void;
  /** Cancel the in-flight turn and freeze (idempotent). */
  cancel(): void;
  /** Resolve at the orchestration's quiescent point (no in-flight turn, no pending input). */
  whenQuiescent(): Promise<void>;
  /** Release every member and the orchestration's registrations (idempotent). */
  dispose(): Promise<void>;
}

/**
 * Stable state-error codes the host maps onto `FAILED_PRECONDITION` by code,
 * not by class identity.
 */
export type SceneStateErrorCode = "UNMATERIALIZED" | "ALREADY_MATERIALIZED" | "DISPOSED";

/** A caller-visible orchestration state error. */
export class SceneStateError extends Error {
  readonly code: SceneStateErrorCode;

  constructor(code: SceneStateErrorCode, message: string) {
    super(message);
    this.name = "SceneStateError";
    this.code = code;
  }
}

/**
 * The agent scope a composed preset mounts into — derived from the agent
 * creation setup face so this module names no framework type of its own.
 */
export type SceneAgentScope = Parameters<NonNullable<CreateAgentOptions["setup"]>>[0];

/**
 * The preset-compose seam result: structurally the preset-authoring
 * compose result, so the host passes the authoring service's `compose`
 * without this module depending on the authoring plugin
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §1).
 */
export interface SceneComposedPreset {
  /** Resolved preset id snapshot for the creation meta. */
  readonly agentPreset: string;
  /** Roster mount hook invoked inside the member's creation setup. */
  setup(agentCtx: SceneAgentScope): Promise<void> | void;
}

/** Resolve + compose one preset (production: the preset-authoring `compose` face). */
export type SceneCompose = (preset: string) => Promise<SceneComposedPreset>;

/** Narrow structural view of the agent registry creation face (`ctx.agents`). */
export interface SceneAgentCreationSeam {
  create(options: CreateAgentOptions): Promise<AgentHandle>;
}

/**
 * Narrow structural view of the group-chat service (`ctx.team`), consumed only
 * by scenes that relay.
 */
export interface SceneTeamSeam {
  register(registration: TeamRegistration): TeamHandle;
  drain(member: TeamMemberSource): UserMessage[];
}

/**
 * The scene-neutral failure reporter. The host injects its logger; a scene
 * registration may wrap it with scene-local context that never enters this
 * seam (specs/080-host-main-sub-model/contracts/scene-registry.md §1/§5).
 */
export interface SceneLogger {
  error(message: string, context: Readonly<Record<string, unknown>>): void;
}

/**
 * The host-injected collaborators of one scene registration — only
 * scene-neutral seams. The scene registration takes what it needs; the host
 * does not know which scene consumes which seam
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §1).
 */
export interface SceneRegistrationDeps {
  /** Preset derivation (the authoring service's structured compose face). */
  readonly compose: SceneCompose;
  /** Agent creation seam (production: `ctx.agents`). */
  readonly agents: SceneAgentCreationSeam;
  /** Failure reporter. */
  readonly logger: SceneLogger;
  /** Deployment provider route. */
  readonly provider: string;
  /** Group-chat relay seam; absent for scenes that do not relay. */
  readonly team?: SceneTeamSeam;
}

/**
 * The template-keyed scene registry the host consumes, injectable so tests
 * can compose synthetic scenes. `SceneSpec` is imported as a type only
 * (erased at runtime), so this module adds no runtime edge to the scene table
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §1/§7).
 */
export type SceneRegistry = Readonly<Record<string, SceneSpec>>;
