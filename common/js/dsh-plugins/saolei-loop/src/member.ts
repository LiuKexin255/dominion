/**
 * The team loop's member vocabulary: the execution unit the flow layer is
 * written against, independent of what backs a member (LLM agent, announce
 * only, or a future type). The contract is
 * specs/076-agent-v2-optimize/contracts/flow-orchestration.md §2; the outcome
 * four-state set and the freeze semantics it drives are §4; the error
 * contract code is §5 (design semantics:
 * survey/saolei-member-switch-flow.md §5.2/§7/§8.1).
 *
 * `drive()` is a closed loop — input construction, execution, completion
 * judgement and the terminal outcome all resolve inside one call, so neither
 * the flow layer nor the execution layer observes member-internal signals.
 * `deliver()` carries a user message into the member domain without passing
 * through flow decisions (contracts/flow-orchestration.md §3).
 */

import type { UserMessage } from "@deepseek-ai/dsh-llm";

/**
 * The execution-layer member identities: the flow graph's activity bindings
 * (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §1).
 */
export type MemberId = "player" | "planner" | "announcer";

/**
 * The loop-layer failure classification (contracts/flow-orchestration.md §5):
 * each code binds the recovery dimensions (same-input retry feasibility, the
 * recovering party, the presentation guidance) rather than the failure's
 * origin; the member maps engines codes onto it
 * (specs/076-agent-v2-optimize/data-model.md §3.2).
 */
export type LoopErrorCode =
  | "TRANSIENT"
  | "QUOTA"
  | "CREDENTIAL"
  | "CONFIG"
  | "INPUT_TOO_LARGE"
  | "INTERNAL";

/**
 * The diagnostic half of a failure report: the engine's stable code (when the
 * failure surfaced as one) plus the message. The loop layer transports it for
 * presentation/logging and never interprets it
 * (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §5).
 */
export interface FailureDetail {
  readonly engineCode?: string;
  readonly message: string;
}

/**
 * The four terminal states of one driven activity
 * (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §4):
 * `settled` completed; `resting` had no executable content (not started);
 * `failed` is the member's minimal report after exhausting self-healing;
 * `aborted` was interrupted by caller intent (cancel/dispose, not a fault —
 * it writes no failure fact).
 */
export type Outcome =
  | { readonly kind: "settled" }
  | { readonly kind: "resting" }
  | {
      readonly kind: "failed";
      readonly code: LoopErrorCode;
      readonly detail: FailureDetail;
    }
  | { readonly kind: "aborted" };

/** The loop layer's member interface (contracts/flow-orchestration.md §2). */
export interface Member {
  /** Stable member identity — the flow activity's `member` binding. */
  readonly id: MemberId;
  /**
   * Whether the member consumes delivered messages. Announce-only members set
   * it false: `deliver` carries no meaning for them
   * (contracts/flow-orchestration.md §2).
   */
  readonly consumes: boolean;
  /**
   * Closed loop: input construction (default drain; an explicit `input`
   * overrides it for the held-input retry path) → execution → completion
   * judgement → terminal outcome. Internal failures are translated into a
   * `failed` outcome; a technical throw propagates to the execution layer,
   * which translates it at the start call site
   * (contracts/flow-orchestration.md §5 成员承诺).
   */
  drive(input?: readonly UserMessage[]): Promise<Outcome>;
  /**
   * Deliver one user message into the member domain: while a turn is in
   * flight it steers into the member's next-step inbox (consumed at the next
   * step boundary, or by the turn self-healing at its close); while quiescent
   * it assembles `[drain, landed flush, message]` and opens a turn. Delivery
   * never passes through a flow decision.
   */
  deliver(message: UserMessage): void;
}

/**
 * The execution layer's read/control seam over a member. It is separate from
 * {@link Member} because the flow layer consumes only the closed-loop
 * vocabulary; these reads belong to the loop's own bookkeeping (snapshot
 * derivation, the serialized pump awaiting a delivery-started turn, and the
 * cancel path).
 */
export interface MemberExecution {
  /** Whether a turn this member started is still in flight. */
  readonly busy: boolean;
  /**
   * Number of delivered-but-unconsumed user messages (the snapshot `queued`
   * fact, including the members' inbox pending state —
   * specs/076-agent-v2-optimize/contracts/flow-orchestration.md §7).
   */
  readonly pendingInput: number;
  /** Await the in-flight turn's outcome; `resting` when nothing is in flight. */
  whenSettled(): Promise<Outcome>;
  /** Latch the in-flight turn's interruption so its drive returns `aborted`. */
  abort(): void;
  /** Drop the member's subscriptions (the handle teardown also unwinds them). */
  dispose(): void;
}
