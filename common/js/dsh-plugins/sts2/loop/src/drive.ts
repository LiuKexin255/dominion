/**
 * MainAgentDriver — the sts2 minimal orchestration's single-agent drive
 * primitives over the dsh-agent 0.2.0-rc.2 base API
 * (`inject`/`followup`/`steer`/`whenIdle`/`cancel`). The behaviour contract is
 * specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md §4; the state
 * machine it implements is specs/080-host-main-sub-model/data-model.md §4
 * (Quiescent → MainTurn → Quiescent, with the Frozen branch for cancel and
 * failure). The primitive shape is the single-agent simplification of the
 * saolei-loop precedent
 * (common/js/dsh-plugins/saolei/loop/src/agent-member.ts): no team broadcast
 * drain, no member switching, no workflow phase, no self-healing pump.
 *
 * The driver owns only what one agent turn needs: the pre-step batch assembly
 * (`inject` all but the last, `followup` the last), the in-flight steering
 * count, the failure read (`agent/error` always reaches the boundary before
 * idle), the engine-code classification, and the freeze/held-input recovery
 * chain. Materialization, session ownership, snapshots and disposal are the
 * orchestrator's (`orchestrator.ts`), so this module takes an already-created
 * agent and never talks to the composition.
 *
 * The driver builds no messages: the caller hands it `UserMessage` values
 * (`createUserMessage` stays at the orchestration edge, as in
 * common/js/dsh-plugins/saolei/loop/src/orchestrator.ts), which keeps this
 * module free of runtime imports beyond the agent's own methods.
 */

import type { Agent, AgentCancelCause } from "@deepseek-ai/dsh-agent";
import type { UserMessage } from "@deepseek-ai/dsh-llm";

/**
 * The loop-layer failure classification of one main-agent turn: the saolei
 * `LoopErrorCode` six-code set
 * (common/js/dsh-plugins/saolei/loop/src/member.ts, contract §4 "失败").
 */
export type MainDriveErrorCode =
  | "TRANSIENT"
  | "QUOTA"
  | "CREDENTIAL"
  | "CONFIG"
  | "INPUT_TOO_LARGE"
  | "INTERNAL";

/**
 * Engine codes that classify as a transient failure after the retry waterfall
 * is exhausted: a time-shifted retry may succeed
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
 * Engine codes for a configuration/routing error: fails until the
 * configuration is fixed and the agent re-materialized. HTTP status codes are
 * the endpoint-error form of the same class
 * (specs/076-agent-v2-optimize/data-model.md §3.2).
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
 * (specs/076-agent-v2-optimize/data-model.md §3.2), identical to the saolei
 * `mapEngineCode` classification set: known recovery classes first, HTTP
 * status codes are configuration/endpoint errors, and anything else —
 * including an absent code — is an implementation failure.
 */
export function mapEngineCode(engineCode: string | undefined): MainDriveErrorCode {
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
  if (CONFIG_ENGINE_CODES.has(engineCode) || engineCode.startsWith("HTTP_")) {
    return "CONFIG";
  }
  if (INPUT_TOO_LARGE_ENGINE_CODES.has(engineCode)) {
    return "INPUT_TOO_LARGE";
  }
  return "INTERNAL";
}

/** The diagnostic half of a failed turn, transported for presentation/logging. */
export interface MainDriveFailureDetail {
  /** The engine's stable code, when the failure surfaced as one. */
  readonly engineCode?: string;
  readonly message: string;
}

/** The last failed main-agent turn, exposed through the orchestrator snapshot. */
export interface MainDriveFailure {
  readonly message: string;
  readonly code: MainDriveErrorCode;
  readonly detail: MainDriveFailureDetail;
}

/**
 * The terminal state of one submitted turn (contract §4):
 * `settled` completed (`turn_end{COMPLETED}`); `resting` was awaited while no
 * turn was in flight; `aborted` was interrupted by `cancel`
 * (`turn_end{CANCELED}`, not a fault); `failed` reports the classified engine
 * failure (`turn_end` error).
 */
export type MainDriveOutcome =
  | { readonly kind: "settled" }
  | { readonly kind: "resting" }
  | { readonly kind: "aborted" }
  | {
      readonly kind: "failed";
      readonly code: MainDriveErrorCode;
      readonly detail: MainDriveFailureDetail;
    };

/** The failure facts one turn reports (written by the `agent/error` boundary). */
interface TurnFailure {
  readonly engineCode?: string;
  readonly message: string;
}

/**
 * Narrow the `agent/error` payload to its stable code without taking a runtime
 * dependency on `@deepseek-ai/dsh-llm`'s `isHarnessError`: the boundary emits
 * a `HarnessError` whose `code` is a non-empty string, so reading the code
 * structurally is equivalent there. A payload without a code classifies
 * INTERNAL (see {@link mapEngineCode}), so the driver never invents a
 * recovery class for an unclassified error.
 */
function engineCodeOf(error: unknown): string | undefined {
  if (typeof error !== "object" || error === null || !("code" in error)) {
    return undefined;
  }
  const code = (error as { readonly code?: unknown }).code;
  return typeof code === "string" && code !== "" ? code : undefined;
}

/** Normalize an `agent/error` payload into a {@link TurnFailure}. */
function turnFailureOf(error: unknown): TurnFailure {
  const engineCode = engineCodeOf(error);
  const message = error instanceof Error ? error.message : String(error);
  return engineCode === undefined ? { message } : { engineCode, message };
}

/** The collaborators one {@link MainAgentDriver} drives. */
export interface MainAgentDriverDeps {
  /** The already-created main agent (one per materialization). */
  readonly agent: Agent;
}

/**
 * The single-agent driver: one main agent, one turn at a time.
 *
 * Turn assembly (contract §4 "静止时 submit"): the whole batch is one pre-step
 * claim — `inject` every message but the last, `followup` the last — with the
 * idle await registered first so a driver wake cannot race the await. The
 * held-input replay after a freeze delivers the same shape.
 *
 * In-flight submit (contract §4 "在途时 submit"): `steer` joins the nearest
 * step boundary and increments {@link pendingInput}; the official loop claims
 * it inside the running turn, so a normal turn end leaves nothing to replay.
 *
 * Cancel (contract §4 "cancel"): terminate the in-flight turn, keep the
 * in-flight input set as the held set (the agent's cancel clears the inbox, so
 * the driver is the input's only remaining carrier) and freeze; the next
 * delivery replays the held set plus the new message (data-model §4
 * Frozen → MainTurn).
 *
 * Failure (contract §4 "失败"): the turn ends through the `agent/error`
 * boundary, the code maps onto the six-code set, `failed`/`lastError` latch
 * and the driver freezes — no automatic retry or continuation. The next
 * delivery replays the held set and clears the failure fact on success.
 */
export class MainAgentDriver {
  private readonly agent: Agent;
  /** The active turn; null while quiescent or frozen. */
  private activeTurn: Promise<MainDriveOutcome> | null = null;
  /**
   * Messages accepted while a turn was in flight, not yet claimed by it. The
   * count is the snapshot's `queued` fact; at settle the official loop has
   * claimed the consumed ones, and the unclaimed ones move to {@link held}
   * on an abort/failure.
   */
  private steered: UserMessage[] = [];
  /**
   * The input set retained across a freeze: delivered inputs the agent's inbox
   * no longer carries. Replayed by the next delivery
   * (data-model §4 Frozen → MainTurn).
   */
  private held: UserMessage[] = [];
  /**
   * The steering set captured by {@link cancel} before the agent's cancel
   * clears its inbox; consumed by the aborting turn's settle. Null outside a
   * cancelled turn.
   */
  private abortHeld: UserMessage[] | null = null;
  private lastTurnFailure: TurnFailure | null = null;
  /** Set by {@link cancel} while a turn is in flight; read once idle settles. */
  private abortRequested = false;
  private frozen = false;
  private lastTurnError: MainDriveFailure | null = null;
  private readonly offError: () => void;

  constructor(deps: MainAgentDriverDeps) {
    this.agent = deps.agent;
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

  /** Whether a submitted turn is still in flight. */
  get busy(): boolean {
    return this.activeTurn !== null;
  }

  /** The in-flight steering count (the snapshot's `queued` fact). */
  get pendingInput(): number {
    return this.steered.length;
  }

  /** Whether the driver is frozen after a cancel or a failed turn. */
  get paused(): boolean {
    return this.frozen;
  }

  /** Whether the last turn failed (a successful delivery clears it). */
  get failed(): boolean {
    return this.lastTurnError !== null;
  }

  /** The last failed turn, or null after a successful/aborted settle. */
  get lastError(): MainDriveFailure | null {
    return this.lastTurnError;
  }

  /**
   * Accept one user message (seam `submit`): while a turn is in flight it
   * steers into the agent inbox and counts as pending; otherwise it opens a
   * turn — replaying the held set first when the driver was frozen
   * (contract §4).
   */
  submit(message: UserMessage): void {
    if (this.activeTurn !== null) {
      this.agent.steer(message);
      this.steered.push(message);
      return;
    }
    const replay = this.held;
    this.held = [];
    this.frozen = false;
    void this.startTurn([...replay, message]);
  }

  /**
   * Cancel (contract §4, idempotent): freeze and terminate the in-flight turn.
   * The held set survives — cancel never consumes it — so the next delivery
   * resumes from exactly the unconsumed input. `cause` is the durable
   * `turn/end` cancellation cause: the user-facing seam uses `user`, the
   * host's teardown path passes `disposed` (the saolei-loop precedent).
   */
  cancel(cause: AgentCancelCause = { kind: "user" }): void {
    this.frozen = true;
    if (this.activeTurn === null || this.abortRequested) {
      return;
    }
    this.abortRequested = true;
    // Take custody of the in-flight set BEFORE cancelling: the agent's cancel
    // discards its inbox (no keepInbox), so the driver becomes the input's
    // only remaining carrier. Cancel does not consume the held input.
    this.abortHeld = [...this.steered];
    this.steered = [];
    // The cause is the stable caller intent recorded on `turn/end`
    // (https://unpkg.com/@deepseek-ai/dsh-agent@0.2.0-rc.2/README.md).
    this.agent.cancel(cause);
  }

  /**
   * Await the in-flight turn chain's outcome; `resting` when nothing has been
   * submitted (the orchestrator's `whenQuiescent` uses this).
   */
  whenSettled(): Promise<MainDriveOutcome> {
    return this.activeTurn ?? Promise.resolve({ kind: "resting" });
  }

  /**
   * Drop the failure observation. The agent handle's teardown already unwinds
   * the scope; this only releases the listener this driver registered
   * (contract §3: dispose converges the in-flight turn first).
   */
  dispose(): void {
    this.offError();
  }

  /** Start one turn (invariant: at most one in flight; the driver serializes). */
  private startTurn(messages: readonly UserMessage[]): Promise<MainDriveOutcome> {
    if (this.activeTurn !== null) {
      throw new Error(
        "sts2-loop main driver invariant violated: a turn is already in flight",
      );
    }
    const run = this.runTurn(messages);
    this.activeTurn = run;
    void run.then(
      () => this.clearActive(run),
      () => this.clearActive(run),
    );
    return run;
  }

  private clearActive(run: Promise<MainDriveOutcome>): void {
    if (this.activeTurn === run) {
      this.activeTurn = null;
    }
  }

  private async runTurn(messages: readonly UserMessage[]): Promise<MainDriveOutcome> {
    if (messages.length === 0) {
      return { kind: "resting" };
    }
    // Register the idle await before waking: the followup below may complete
    // synchronously, and a later `whenIdle` would miss that transition
    // (common/js/dsh-plugins/saolei/loop/src/agent-member.ts runTurn).
    const idle = this.agent.whenIdle();
    this.abortRequested = false;
    this.abortHeld = null;
    this.lastTurnFailure = null;
    for (const message of messages.slice(0, -1)) {
      this.agent.inject(message);
    }
    this.agent.followup(messages[messages.length - 1]);
    await idle;

    const failure = this.consumeTurnFailure();
    const aborted = this.abortRequested;
    this.abortRequested = false;
    const inFlight = this.steered;
    this.steered = [];
    if (aborted) {
      // The set captured at cancel plus anything steered while the turn was
      // winding down: the pre-cancel set lost its inbox carrier to the
      // cancel's inbox clear, and a late steer may still sit in the inbox, so
      // it is taken out of there before joining the same held set.
      this.held = [
        ...this.takeUnclaimed(inFlight),
        ...(this.abortHeld ?? []),
        ...this.held,
      ];
      this.abortHeld = null;
      this.lastTurnError = null;
      return { kind: "aborted" };
    }
    if (failure !== null) {
      // A failed (not cancelled) turn leaves its unconsumed steering in the
      // durable inbox. Take custody before freezing: leaving it there and also
      // replaying it on recovery would claim the same input twice.
      this.held = [...this.takeUnclaimed(inFlight), ...this.held];
      const detail: MainDriveFailureDetail = {
        ...(failure.engineCode === undefined
          ? {}
          : { engineCode: failure.engineCode }),
        message: failure.message,
      };
      const code = mapEngineCode(failure.engineCode);
      this.lastTurnError = { message: failure.message, code, detail };
      this.frozen = true;
      return { kind: "failed", code, detail };
    }
    this.lastTurnError = null;
    return { kind: "settled" };
  }

  /**
   * Take the still-pending steering messages out of the agent inbox and
   * return them: a message the failed turn consumed is gone from the inbox and
   * is not part of the held set; one still pending is removed from the durable
   * inbox (an ordinary `Inbox.remove`, the durable-cancellation primitive —
   * https://unpkg.com/@deepseek-ai/dsh-agent@0.2.0-rc.2/README.md) so the
   * recovery delivery can replay it exactly once.
   */
  private takeUnclaimed(steered: readonly UserMessage[]): UserMessage[] {
    const inbox = this.agent.inbox;
    const pending = new Set(
      [...inbox.nextStep, ...inbox.nextTurn].map((message) => String(message.id)),
    );
    const unclaimed: UserMessage[] = [];
    for (const message of steered) {
      if (!pending.has(String(message.id))) {
        continue;
      }
      inbox.remove(message.id);
      unclaimed.push(message);
    }
    return unclaimed;
  }

  /**
   * Consume the turn's failure marker: it describes this turn only, so the
   * next turn starts from a clean slate.
   */
  private consumeTurnFailure(): TurnFailure | null {
    const failure = this.lastTurnFailure;
    this.lastTurnFailure = null;
    return failure;
  }
}
