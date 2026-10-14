/**
 * flow — the team loop's flow layer: the F-Moore activity graph (the single
 * authored structure), the facts its pure edge predicates read, and the
 * minimal runtime that turns a wake (an activity outcome, or a
 * delivery-started turn settling) into the next `start(activity)`. The
 * contract is specs/076-agent-v2-optimize/contracts/flow-orchestration.md
 * §1/§4; the structure is specs/076-agent-v2-optimize/data-model.md §2; the
 * design semantics are survey/saolei-member-switch-flow.md §4/§5 (rulings
 * 5–14, 20–21).
 *
 * The flow layer is a pure state object: no resources, no subscriptions, no
 * lifecycle — a refresh drops it with its reference and keeps no flow facts
 * (rollings 22). It never touches execution details: a member is a `member`
 * binding id, and the abstraction the loop drives is the member interface
 * (member.ts), not any concrete implementation.
 *
 * Two wakes are distinct. A *drive* wake reports the outcome of a
 * flow-issued `start`; `settled` runs the activity's `onSettle` and judges
 * edges, `resting` rests, `failed`/`aborted` freeze. An *external* wake
 * (`null`) reports a delivery-started turn settling: it never runs
 * `onSettle` (delivery is invisible to the flow — contracts/flow-orchestration.md
 * §3) but it re-judges edges, and while frozen it is the recovery drive whose
 * completion restarts the current activity. A frozen restart that finds no
 * executable content falls back to edge judgment rather than stalling.
 */

import type { GameEventRecord } from "@dominion/dsh-saolei";

import type { FailureDetail, LoopErrorCode, Outcome } from "./member.js";

/**
 * The facts an edge predicate reads — never queued input
 * (specs/076-agent-v2-optimize/data-model.md §2.2). `terminal` is the game
 * runtime's LATEST terminal record, resolved live on every read so that a
 * guard always judges the newest record ("跳局不补发",
 * specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §3).
 * `reviewed` and `statsSentFor` are record-identity guards, not flags.
 */
export interface Facts {
  readonly terminal: GameEventRecord | null;
  reviewed: GameEventRecord | null;
  rounds: number;
  statsSentFor: GameEventRecord | null;
}

/**
 * One activity: a flow graph node (BPMN Task / XState state / SFN Task
 * counterpart) carrying the executing member binding, optional fact-writing
 * hooks, and the pure outgoing-edge predicate.
 */
export interface Activity<Id extends string = string, M extends string = string> {
  /** Activity identity — one per graph node. */
  readonly id: Id;
  /** The executing member whose activation the activity carries. */
  readonly member: M;
  /**
   * Whether the activity has no execution (`rest`): the activation lands but
   * no member is driven —静止 is an activity semantics, not a member
   * capability (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §1).
   */
  readonly driverless?: boolean;
  /**
   * Edge-target effect, applied when the flow enters this activity (the edge
   * table's "start 目标活动的效果" column): `rest` clears the completed-round
   * counter here (specs/068-wiki-round-pause/contracts/saolei-loop-round-stop.md §2).
   */
  readonly onEnter?: (facts: Facts) => void;
  /** Flow-fact write on success (settled only) — flow-layer state, no command. */
  readonly onSettle?: (facts: Facts) => void;
  /**
   * Edge predicate: pure, read-only over the facts; the target activity, or
   * null to stay on the current activity (a continuation drive).
   */
  readonly next: (facts: Facts) => Id | null;
}

/**
 * The flow layer's single output vocabulary: `start(activity)`. There is no
 * separate "switch" command — switching members is starting an activity bound
 * to a different member (specs/076-agent-v2-optimize/contracts/
 * flow-orchestration.md §1). `rest`/`frozen` are the two non-command
 * decisions: the pump rests (no scheduled step) or the flow is frozen.
 */
export type FlowDecision<Id extends string = string> =
  | { readonly kind: "start"; readonly activity: Id }
  | { readonly kind: "rest" }
  | { readonly kind: "frozen" };

/** The frozen flow's last failure fact (failed only; `aborted` writes none). */
export interface FrozenError {
  readonly code: LoopErrorCode;
  readonly detail: FailureDetail;
}

/**
 * The minimal flow runtime: position (current activity) + last outcome +
 * facts + frozen error — the whole of the flow layer's state
 * (specs/076-agent-v2-optimize/data-model.md §2.2). It is a pure state object
 * with no lifecycle.
 */
export class Flow<Id extends string = string, M extends string = string> {
  private currentPosition: Id;
  private currentOutcome: Outcome | null = null;
  private currentFrozenError: FrozenError | null = null;
  /** True while the start just issued was the restart of a frozen activity. */
  private retrying = false;

  constructor(
    private readonly activities: Readonly<Record<Id, Activity<Id, M>>>,
    private readonly currentFacts: Facts,
    initial: Id,
  ) {
    this.currentPosition = initial;
  }

  /** The current activity (the flow position). */
  get position(): Id {
    return this.currentPosition;
  }

  /** The member the current activity's activation belongs to. */
  get activeMemberId(): M {
    return this.activities[this.currentPosition].member;
  }

  /** The activity definition at the current position. */
  get activity(): Activity<Id, M> {
    return this.activities[this.currentPosition];
  }

  /** The read-only flow facts (edge predicates read these; hooks write them). */
  get facts(): Facts {
    return this.currentFacts;
  }

  /** The last activity outcome (the freeze discriminant). */
  get lastOutcome(): Outcome | null {
    return this.currentOutcome;
  }

  /** The frozen failure fact; null unless the last outcome failed. */
  get frozenError(): FrozenError | null {
    return this.currentFrozenError;
  }

  /** Whether the flow is frozen (last outcome failed or aborted). */
  get paused(): boolean {
    const kind = this.currentOutcome?.kind;
    return kind === "failed" || kind === "aborted";
  }

  /**
   * The decision entry: consume one wake and return the next directive.
   * `wake === null` is an external wake (a delivery-started turn settled);
   * otherwise it reports the outcome of the flow's own last `start`.
   */
  decide(wake: Outcome | null): FlowDecision<Id> {
    const wasRetry = this.retrying;
    this.retrying = false;
    if (wake !== null) {
      this.currentOutcome = wake;
      switch (wake.kind) {
        case "settled":
          this.currentFrozenError = null;
          this.activities[this.currentPosition].onSettle?.(this.currentFacts);
          break;
        case "failed":
          this.currentFrozenError = { code: wake.code, detail: wake.detail };
          break;
        case "aborted":
          this.currentFrozenError = null;
          break;
        case "resting":
          break;
      }
    }

    if (this.paused) {
      if (wake === null) {
        // The recovery drive (delivery) settled: restart the current activity.
        // A held-input replay happens inside the member adapter, invisible
        // here (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §4).
        this.retrying = true;
        return { kind: "start", activity: this.currentPosition };
      }
      return { kind: "frozen" };
    }
    if (wake !== null && wake.kind === "resting" && !wasRetry) {
      // No executable content (an empty drain, or the rest activity itself):
      // the pump rests and the next wake re-judges.
      return { kind: "rest" };
    }
    // Edge judgment: a settled drive, an external wake, or a restart that
    // found nothing to execute.
    return this.judge();
  }

  /**
   * Freeze the flow on an aborted outcome without a drive — the quiescent
   * cancel path (there is no in-flight activity whose outcome would freeze
   * it). The command entry then restarts the current activity on recovery.
   */
  freezeAborted(): void {
    this.currentOutcome = { kind: "aborted" };
    this.currentFrozenError = null;
  }

  /** The pure edge evaluation: next target (or the current activity). */
  private judge(): FlowDecision<Id> {
    const current = this.activities[this.currentPosition];
    const target = current.next(this.currentFacts) ?? this.currentPosition;
    this.currentPosition = target;
    this.currentOutcome = null;
    this.activities[target].onEnter?.(this.currentFacts);
    return { kind: "start", activity: target };
  }
}

/** The completed rounds after which the loop rests (saolei's natural stop). */
export const ROUND_STOP_INTERVAL = 5;

/** The saolei graph's activity ids (one per F-Moore node). */
export type SaoleiActivityId = "plan" | "play" | "announce" | "review" | "rest";

/** The saolei graph's member bindings. */
export type SaoleiMemberId = "player" | "planner" | "announcer";

/** The unreviewed-terminal-record guard shared by the play and rest edges. */
function hasUnreviewedTerminal(facts: Facts): boolean {
  return facts.terminal !== null && facts.terminal !== facts.reviewed;
}

/**
 * The saolei F-Moore graph: 5 activities, 8 edges
 * (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §1 边表).
 * `Counting` in the 068 natural-stop state graph is not an activity — it is
 * the review activity's `onSettle` — and `Stopped` is `rest`
 * (specs/068-wiki-round-pause/data-model.md §8).
 */
export const SAOLEI_ACTIVITIES: Readonly<
  Record<SaoleiActivityId, Activity<SaoleiActivityId, SaoleiMemberId>>
> = {
  plan: {
    id: "plan",
    member: "planner",
    next: () => "play",
  },
  play: {
    id: "play",
    member: "player",
    next: (facts) => (hasUnreviewedTerminal(facts) ? "announce" : "play"),
  },
  announce: {
    id: "announce",
    member: "announcer",
    onSettle: (facts) => {
      facts.statsSentFor = facts.terminal;
    },
    next: () => "review",
  },
  review: {
    id: "review",
    member: "planner",
    onSettle: (facts) => {
      facts.reviewed = facts.terminal;
      facts.rounds += 1;
    },
    next: (facts) => (facts.rounds < ROUND_STOP_INTERVAL ? "play" : "rest"),
  },
  rest: {
    id: "rest",
    member: "player",
    driverless: true,
    onEnter: (facts) => {
      facts.rounds = 0;
    },
    next: (facts) => (hasUnreviewedTerminal(facts) ? "announce" : "play"),
  },
};

/**
 * The saolei facts: the mutable flow facts plus a live terminal-record read
 * (the game runtime's LATEST record), so a guard is always evaluated against
 * the newest record.
 */
export class SaoleiFacts implements Facts {
  reviewed: GameEventRecord | null = null;
  rounds = 0;
  statsSentFor: GameEventRecord | null = null;

  /** @param readTerminal - Read the runtime's LATEST terminal record. */
  constructor(private readonly readTerminal: () => GameEventRecord | null) {}

  get terminal(): GameEventRecord | null {
    return this.readTerminal();
  }
}

/** Build one saolei flow resting at `plan` (the materialization's initial state). */
export function createSaoleiFlow(
  readTerminal: () => GameEventRecord | null,
): Flow<SaoleiActivityId, SaoleiMemberId> {
  return new Flow(SAOLEI_ACTIVITIES, new SaoleiFacts(readTerminal), "plan");
}
