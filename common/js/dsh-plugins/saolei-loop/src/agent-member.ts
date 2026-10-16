/**
 * AgentMember — the LLM agent member adapter: the single implementation the
 * player and planner members share. It owns everything about driving an agent
 * member that used to sit in the orchestrator: input construction (team
 * drain + landed flush), the `inject(*)+followup(last)` pre-step batch, the
 * `agent.whenIdle()` completion wait, the failure marker read (the
 * `agent/error` boundary, always before idle), the engine-code → loop-code
 * mapping, the delivery split (steer / assembly), the in-flight broadcast
 * delivery face ({@link AgentMember.deliverInFlight}), the turn-start inbox
 * hygiene for a failed turn's unclaimed fast-path broadcasts (the sweep that
 * precedes every batch injection), and the held-input replay after a failed or
 * aborted drive. The contracts are
 * specs/076-agent-v2-optimize/contracts/flow-orchestration.md §2.1/§3/§5 and
 * specs/076-agent-v2-optimize/data-model.md §3; the base APIs are
 * @deepseek-ai/dsh-agent 0.2.0-rc.2's `steer`/`whenIdle`/`inject`/`followup`
 * (specs/076-agent-v2-optimize/research.md D3).
 *
 * The `held` input is the review's execution-integrity mechanism: a flow
 * drive drains the member's relays, which the team marks consumed, so a
 * failed or aborted drive must hold the set to replay it when the flow
 * restarts the current activity. The replay is invisible to the flow layer
 * (the restart is a plain start of the current activity).
 */

import type { Agent } from "@deepseek-ai/dsh-agent";
import { isHarnessError } from "@deepseek-ai/dsh-llm";
import type { UserMessage } from "@deepseek-ai/dsh-llm";

import type {
  FailureDetail,
  LoopErrorCode,
  Member,
  MemberExecution,
  MemberId,
  Outcome,
} from "./member.js";

/**
 * The engine codes that classify as a transient failure after the retry
 * waterfall is exhausted: a time-shifted retry may succeed
 * (specs/076-agent-v2-optimize/data-model.md §3.2).
 */
const TRANSIENT_ENGINE_CODES = new Set([
  "RATE_LIMIT",
  "SERVER",
  "TIMEOUT",
  "TRANSPORT",
  "EMPTY_RESPONSE",
]);

/** Engine codes for an exhausted quota or balance: fails until replenished. */
const QUOTA_ENGINE_CODES = new Set(["QUOTA", "ACCOUNT_QUOTA"]);

/** Engine codes for an unusable credential: fails until the user fixes it. */
const CREDENTIAL_ENGINE_CODES = new Set([
  "INVALID_CREDENTIAL",
  "MISSING_CREDENTIAL",
  "AUTH",
]);

/**
 * Engine codes for a configuration/routing error (no adapter, a bad request
 * or endpoint): fails until the configuration is fixed and the team
 * re-materialized. `INVALID_REQUEST`/`UNSUPPORTED_CONTENT`/`UNSUPPORTED` are
 * beyond the mapping table's literal rows
 * (specs/076-agent-v2-optimize/data-model.md §3.2) but classify here by the
 * table's guiding principle — the code is its own recovery path: their user
 * action is to correct the configuration/request parameters, the same
 * recovery class CONFIG binds (HTTP_*, the endpoint-error form of it, is the
 * equivalent the contract names explicitly in
 * specs/076-agent-v2-optimize/contracts/flow-orchestration.md §2.1).
 */
const CONFIG_ENGINE_CODES = new Set([
  "NO_ADAPTER",
  "INVALID_REQUEST",
  "UNSUPPORTED_CONTENT",
  "UNSUPPORTED",
]);

/** Engine codes for input beyond the executable range: fails on the same input. */
const INPUT_TOO_LARGE_ENGINE_CODES = new Set([
  "CONTEXT_WINDOW_EXCEEDED",
  "IMAGE_OFFLOAD_REQUIRED",
]);

/**
 * Map one engine failure code onto the loop contract code
 * (specs/076-agent-v2-optimize/data-model.md §3.2): known recovery classes
 * first, HTTP status codes are configuration/endpoint errors, and anything
 * else — including a non-harness error — is an implementation failure.
 */
export function mapEngineCode(engineCode: string | undefined): LoopErrorCode {
  if (engineCode === undefined) {
    return "INTERNAL";
  }
  if (TRANSIENT_ENGINE_CODES.has(engineCode)) {
    return "TRANSIENT";
  }
  if (QUOTA_ENGINE_CODES.has(engineCode)) {
    return "QUOTA";
  }
  if (CREDENTIAL_ENGINE_CODES.has(engineCode)) {
    return "CREDENTIAL";
  }
  if (
    CONFIG_ENGINE_CODES.has(engineCode) ||
    engineCode.startsWith("HTTP_")
  ) {
    return "CONFIG";
  }
  if (INPUT_TOO_LARGE_ENGINE_CODES.has(engineCode)) {
    return "INPUT_TOO_LARGE";
  }
  return "INTERNAL";
}

/** The failure facts one agent turn reports (written by the error boundary). */
interface TurnFailure {
  readonly engineCode?: string;
  readonly message: string;
}

/** Normalize an `agent/error` payload into a {@link TurnFailure}. */
function turnFailureOf(error: unknown): TurnFailure {
  if (isHarnessError(error)) {
    return { engineCode: error.code, message: error.message };
  }
  return { message: error instanceof Error ? error.message : String(error) };
}

/** The collaborators one {@link AgentMember} drives. */
export interface AgentMemberDeps {
  /** The execution-layer member identity (role). */
  readonly id: MemberId;
  /** The agent this member drives. */
  readonly agent: Agent;
  /** The member's unconsumed team broadcasts (team-internal pull read). */
  readonly drain: () => readonly UserMessage[];
  /**
   * Take the cancel-landed user messages, if any: the source-independent
   * flush that precedes every turn start and steer
   * (specs/061-team-queue-steer/contracts/orchestrator-input.md §1).
   */
  readonly flushLanded: () => readonly UserMessage[];
}

/**
 * The LLM agent member: implements {@link Member} for the flow layer and
 * {@link MemberExecution} for the execution layer's bookkeeping.
 */
export class AgentMember implements Member, MemberExecution {
  readonly id: MemberId;
  readonly consumes = true;

  private readonly agent: Agent;
  private readonly drainSource: () => readonly UserMessage[];
  private readonly takeLanded: () => readonly UserMessage[];
  /**
   * The active turn's promise: a flow drive (awaited by the drive call) or a
   * quiescent delivery's turn (awaited by the execution layer through
   * {@link whenSettled}). Null while quiescent.
   */
  private activeTurn: Promise<Outcome> | null = null;
  /**
   * The input set of the last failed or aborted flow drive, replayed when the
   * flow restarts the current activity and a fresh drain yields nothing.
   */
  private held: readonly UserMessage[] | null = null;
  /**
   * The messages steered into the in-flight turn and not yet consumed. The
   * count is the snapshot's `queued` fact. The 061 consumption closure — a
   * per-event removal keyed by messageId
   * (specs/061-team-queue-steer/contracts/orchestrator-input.md §2) — was
   * replaced by a wholesale clear when the turn settles, so during a turn the
   * count can briefly over-count messages the driver already consumed. That
   * is presentation-only: the web chip lifecycle is driven by
   * `member_view{sender:"user"}` frames, never by this count
   * (specs/061-team-queue-steer/contracts/web-queue-ui.md §1).
   */
  private steered: UserMessage[] = [];
  private lastTurnFailure: TurnFailure | null = null;
  /** Set by {@link abort} while a turn is in flight; read once idle settles. */
  private abortRequested = false;
  private readonly offError: () => void;

  constructor(deps: AgentMemberDeps) {
    this.id = deps.id;
    this.agent = deps.agent;
    this.drainSource = deps.drain;
    this.takeLanded = deps.flushLanded;
    // The turn's failure boundary: always reached before idle, so reading it
    // after the idle await reports exactly this turn's failure
    // (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §1).
    this.offError = this.agent.ctx.on("agent/error", (payload) => {
      if (payload.agent !== this.agent) {
        return;
      }
      this.lastTurnFailure = turnFailureOf(payload.error);
    });
  }

  get busy(): boolean {
    return this.activeTurn !== null;
  }

  get pendingInput(): number {
    return this.steered.length;
  }

  async drive(input?: readonly UserMessage[]): Promise<Outcome> {
    const messages = input ?? this.defaultInput();
    if (messages.length === 0) {
      return { kind: "resting" };
    }
    return this.startTurn(messages, true);
  }

  deliver(message: UserMessage): void {
    if (this.activeTurn !== null) {
      for (const landed of this.takeLanded()) {
        this.agent.inject(landed);
      }
      this.agent.steer(message);
      this.steered.push(message);
      return;
    }
    // Quiescent: assemble the member's unconsumed broadcasts, the cancel-landed
    // flush, then the message (the drive input's landed position,
    // specs/061-team-queue-steer/contracts/orchestrator-input.md §1).
    const messages = [...this.drainSource(), ...this.takeLanded(), message];
    void this.startTurn(messages, false);
  }

  whenSettled(): Promise<Outcome> {
    return this.activeTurn ?? Promise.resolve({ kind: "resting" });
  }

  /**
   * The in-flight broadcast fast path — the one policy every LLM member shares
   * (specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md
   * §2.1). While a turn is in flight the broadcast joins the next-step inbox
   * without waking the driver: `inject` "adds model-facing context without
   * waking the driver, so it lands in the next admitted step"
   * (https://unpkg.com/@deepseek-ai/dsh-agent@0.2.0-rc.2/README.md), which
   * neither opens nor extends a turn and leaves the completion judgement
   * untouched. While quiescent the member is not a receiver at all — the
   * pending baseline delivers the broadcast through the next drive's assembly.
   * An injection failure is reported and swallowed: the announce path that
   * drove this call must never break on it (contract §2.1/§5).
   */
  deliverInFlight(message: UserMessage): void {
    if (!this.busy) {
      return;
    }
    try {
      this.agent.inject(message);
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      this.warn(
        `saolei-loop agent member "${this.id}": in-flight broadcast delivery failed; ` +
          `the pending baseline still holds it: ${reason}`,
      );
    }
  }

  abort(): void {
    if (this.activeTurn !== null) {
      this.abortRequested = true;
    }
  }

  /** Unsubscribe the failure observation (the handle already unwinds the scope). */
  dispose(): void {
    this.offError();
  }

  /** Report a fast-path failure without disturbing the announce path (contract §2.1). */
  private warn(message: string): void {
    this.agent.ctx.logger?.warn?.(message);
  }

  /**
   * Consume the turn's failure marker: it describes this turn only, so the
   * next drive (the retained member after a failure) starts from a clean slate.
   */
  private consumeTurnFailure(): TurnFailure | null {
    const failure = this.lastTurnFailure;
    this.lastTurnFailure = null;
    return failure;
  }

  /**
   * The default input: the member's unconsumed relays plus the cancel-landed
   * flush; when both are empty and a previous drive left a held set, the held
   * set is replayed (the flow's restart of a failed/aborted activity).
   *
   * fresh-before-held ordering rests on an unreachability invariant: while the
   * flow is frozen the only drive source is the frozen member's own recovery
   * delivery, and that member's relays were already consumed by the drive that
   * failed, so a non-empty fresh drain coinciding with a held set cannot occur
   * (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §4). Held must
   * win if that invariant ever breaks (e.g. members driven in parallel): a
   * replay that silently dropped the held set would lose the activity's input
   * while the flow believed it restarted.
   */
  private defaultInput(): readonly UserMessage[] {
    const fresh = [...this.drainSource(), ...this.takeLanded()];
    if (fresh.length > 0) {
      return fresh;
    }
    const held = this.held;
    this.held = null;
    return held ?? [];
  }

  /**
   * The turn-start inbox hygiene — the fast path's receiving-side obligation
   * (specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md
   * §2.1). A failed turn (not a cancel: dsh-agent-loop clears pending work only
   * on cancel) leaves its next-step broadcasts in the inbox, and the next turn
   * boundary claims "pending next-step input plus one queued prompt" in one
   * atomic step
   * (https://unpkg.com/@deepseek-ai/dsh-agent-loop@0.2.0-rc.2/README.md) — the
   * residue and the pending baseline's re-delivery of the same anchor would
   * both be granted. For every residue outside the drain's popped window — the
   * fast path's injected copy and the ordinary failure paths — the anchor is
   * unconsumed and the pending list still derives it, so removing the residue
   * here (an ordinary `Inbox.remove`, i.e. a durable cancellation,
   * https://unpkg.com/@deepseek-ai/dsh-agent@0.2.0-rc.2/README.md) keeps the
   * pending list the anchor's only claimable location. A residue inside that
   * window (`drained`, `common/js/dsh-plugins/team/src/team.ts`) comes from a
   * batch that failed technically after its assembly had popped the anchor but
   * before the turn claimed it: there the sweep loses the anchor with no
   * pending re-derivation — an INTERNAL-class reliability boundary owned by
   * the developer, outside this mechanism's guarantee
   * (specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md
   * §2.1; specs/076-agent-v2-optimize/contracts/flow-orchestration.md §5).
   * User messages are left untouched: their turn-boundary claim is the 061
   * semantics
   * (specs/061-team-queue-steer/contracts/orchestrator-input.md §1 obligation 2).
   */
  private sweepBroadcastResidue(): void {
    const inbox = this.agent.inbox;
    const residue = [...inbox.nextStep, ...inbox.nextTurn].filter(
      (message) => message.source.kind === "team-broadcast",
    );
    for (const message of residue) {
      inbox.remove(message.id);
    }
  }

  /**
   * Start one turn: register the idle await before waking (no lost
   * transition), declare the whole batch as one pre-step claim (inject all
   * but the last, followup the last), and record the held set so a failure or
   * abort can replay it.
   *
   * The first two of the contract's three fail-loud guards
   * (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §2.1): a
   * second concurrent turn and a non-quiescent target are invariant
   * violations, not recoverable conditions. The pump serializes drives and
   * delivery steers instead of starting while in flight, so a violation means
   * a caller bypassed the loop; the throw is translated at the start call
   * site into failed{INTERNAL} with origin orchestration
   * (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §5). The
   * input-non-empty guard is structural rather than a throw: `drive` rests on
   * an empty set and `deliver` always assembles at least the delivered
   * message, so no empty batch can reach here.
   */
  private startTurn(
    messages: readonly UserMessage[],
    hold: boolean,
  ): Promise<Outcome> {
    if (this.activeTurn !== null) {
      throw new Error(
        "saolei-loop agent member invariant violated: a member turn is already in flight",
      );
    }
    if (this.agent.status !== "idle") {
      throw new Error(
        `saolei-loop agent member invariant violated: ${this.id} is not quiescent`,
      );
    }
    this.sweepBroadcastResidue();
    const run = this.runTurn(messages, hold);
    this.activeTurn = run;
    void run.then(
      () => {
        if (this.activeTurn === run) {
          this.activeTurn = null;
        }
      },
      () => {
        if (this.activeTurn === run) {
          this.activeTurn = null;
        }
      },
    );
    return run;
  }

  private async runTurn(
    messages: readonly UserMessage[],
    hold: boolean,
  ): Promise<Outcome> {
    const idle = this.agent.whenIdle();
    this.abortRequested = false;
    this.lastTurnFailure = null;
    // Only a flow drive owns the held set: a delivery-started turn must not
    // disturb a pending replay (the recovery chain's delivery drive runs
    // before the flow's activity restart).
    if (hold) {
      this.held = messages;
    }
    for (const message of messages.slice(0, -1)) {
      this.agent.inject(message);
    }
    this.agent.followup(messages[messages.length - 1] as UserMessage);
    await idle;

    const failure = this.consumeTurnFailure();
    const aborted = this.abortRequested;
    this.abortRequested = false;
    // The turn settled: unclaimed steering cannot survive it. The sweep is
    // wholesale by design (the 061 per-user/message-event removal was
    // superseded, see the `steered` field) — see there for the presentation
    // boundary.
    this.steered = [];
    if (aborted) {
      return { kind: "aborted" };
    }
    if (failure !== null) {
      const detail: FailureDetail = {
        ...(failure.engineCode === undefined
          ? {}
          : { engineCode: failure.engineCode }),
        message: failure.message,
      };
      return {
        kind: "failed",
        code: mapEngineCode(failure.engineCode),
        detail,
      };
    }
    if (hold) {
      this.held = null;
    }
    return { kind: "settled" };
  }
}
