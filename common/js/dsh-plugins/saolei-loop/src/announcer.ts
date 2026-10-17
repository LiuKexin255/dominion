/**
 * AnnouncerMember — the team's announce-only member: it carries the game-over
 * summary into the group chat. Its contract is
 * specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §3 plus
 * the member-message-source interface of
 * specs/065-agent-v2-team-refine/contracts/team-member-source.md §1/§2; the
 * role name and the member's roster presence are
 * specs/076-agent-v2-optimize/contracts/announcer-member.md §1.
 *
 * The output log is a process-memory, structurally session-event-compatible
 * `assistant/message` list: team derivation/rendering/consumption treats it
 * exactly like an agent member's log, so one announcement is one speech unit
 * without team knowing any "system broadcast" concept. The log lives for the
 * team materialization's lifetime (a refresh disposes it — the same
 * short-term memory semantics agent logs have,
 * specs/065-agent-v2-team-refine/research.md D9). The constructed `turn`/`step`
 * and empty `stream` are fixed placeholders: the derive/render path reads only
 * the message id/content and the event time/seq.
 */

import { SessionSeq } from "@deepseek-ai/dsh-session";
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import { createAssistantMessage } from "@deepseek-ai/dsh-llm";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import type { TeamMemberSource } from "@dominion/dsh-team";
import { gameStatsText } from "@dominion/dsh-saolei";
import type { GameEventRecord } from "@dominion/dsh-saolei";

import type {
  Member,
  MemberExecution,
  MemberId,
  Outcome,
} from "./member.js";

/** The announcer member's wire role — roster registration and merge sender label (specs/076-agent-v2-optimize/contracts/announcer-member.md §1). */
export const ANNOUNCER_MEMBER_ROLE = "announcer";

/** Roster summary of the announcer member (the team section's announcer line). */
export const ANNOUNCER_MEMBER_SUMMARY = "终局播报对局结果与操作统计";

/**
 * The announce member's collaborators: the latest terminal record read. The
 * execution layer owns the game runtime, so it passes the record provider in
 * rather than the member reaching into the game foundation.
 */
export interface AnnouncerMemberDeps {
  /** Read the game runtime's LATEST terminal record, or null while playing. */
  readonly peekTerminal?: () => GameEventRecord | null;
}

/**
 * Synthetic model provenance of an announcement. `createAssistantMessage`
 * requires the source shape (an assistant message always records a routed
 * model), but these values are opaque stand-ins: nothing routes on them, no
 * consumer renders them, and their spelling carries no scene or role meaning
 * (the member's role identifier is {@link ANNOUNCER_MEMBER_ROLE}). They only
 * keep the announcement structurally compatible with the shared
 * session-event vocabulary.
 */
const ANNOUNCE_PROVIDER = "saolei";
const ANNOUNCE_MODEL = "system";

/**
 * The announce-only member's message log and source face. `announce(text)`
 * appends one `assistant/message` event (unique MessageId, single text block)
 * and synchronously notifies subscribers; `source` exposes that log to the
 * team with the announce-only capability bits (`consumes: false`, no
 * `sectionTarget`).
 *
 * As a flow member (specs/076-agent-v2-optimize/contracts/flow-orchestration.md
 * §2.2) its `drive()` renders and announces the latest terminal record
 * synchronously and reports `settled`; the record-identity guard
 * ({@link statsSentFor}, the announce activity's counterpart of the flow
 * facts' `reviewed`) announces each record once. `deliver` carries no meaning
 * for it (`consumes = false`).
 */
export class AnnouncerMember implements Member, MemberExecution {
  /** The team-facing message source (`id = ${session}/announcer`). */
  readonly source: TeamMemberSource;

  /** The loop-layer member identity. */
  readonly id: MemberId = "announcer";
  /** Announce-only: it never consumes delivered user messages. */
  readonly consumes = false;

  /** The immutable log snapshots; replaced (not mutated) on every announce. */
  private log: readonly SessionEvent[] = [];
  private readonly listeners = new Set<(event: SessionEvent) => void>();
  private readonly peekTerminal: () => GameEventRecord | null;
  /** The terminal record already announced — record identity, not a flag. */
  private statsSentFor: GameEventRecord | null = null;
  private seq = 0;
  private lastTime = 0;

  constructor(session: string, deps: AnnouncerMemberDeps = {}) {
    const member = this;
    this.peekTerminal = deps.peekTerminal ?? (() => null);
    this.source = {
      id: `${session}/announcer`,
      get events() {
        return member.log;
      },
      subscribe(onEvent) {
        member.listeners.add(onEvent);
        return () => {
          member.listeners.delete(onEvent);
        };
      },
      consumes: false,
    };
  }

  /**
   * Run the announce activity's execution: render the latest terminal record
   * and speak it. The record-identity guard makes a re-driven announce a
   * no-op; a rendering/announce failure is an implementation error and reports
   * `failed{INTERNAL}` (specs/076-agent-v2-optimize/contracts/
   * flow-orchestration.md §2.2).
   */
  async drive(): Promise<Outcome> {
    const event = this.peekTerminal();
    if (event === null || event === this.statsSentFor) {
      return { kind: "settled" };
    }
    try {
      this.announce(gameStatsText(event));
      this.statsSentFor = event;
      return { kind: "settled" };
    } catch (err) {
      return {
        kind: "failed",
        code: "INTERNAL",
        detail: {
          message: err instanceof Error ? err.message : String(err),
        },
      };
    }
  }

  /** No-op: an announce-only member receives no input. */
  deliver(_message: UserMessage): void {}

  get busy(): boolean {
    return false;
  }

  get pendingInput(): number {
    return 0;
  }

  whenSettled(): Promise<Outcome> {
    return Promise.resolve({ kind: "resting" });
  }

  /** No-op: the announce execution is synchronous and never interrupted. */
  abort(): void {}

  /** No-op: the announce member owns no subscription. */
  dispose(): void {}

  /**
   * Append one announcement to the log and notify subscribers synchronously.
   * `seq` increases per announcement; `time` strictly increases and is
   * stamped at least one millisecond ahead of the wall clock, so the
   * announcement sorts after every already-logged event of the other members
   * (`compareUnits` orders by time, with the per-log seq only as an
   * intra-log tie-breaker — seq is not comparable across logs). The
   * announcement thus stays at the tail of the planner's review input set
   * even when the player's terminal unit landed in the same millisecond
   * (specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §3
   * item 2). Empty text fails loud: a blank speech has no derivable unit and
   * must never enter the log.
   */
  announce(text: string): void {
    if (text === "") {
      throw new Error("announcer: announcement text must not be empty");
    }
    this.seq += 1;
    this.lastTime = Math.max(this.lastTime + 1, Date.now() + 1);
    const message = createAssistantMessage({
      content: [{ type: "text", text }],
      source: { provider: ANNOUNCE_PROVIDER, model: ANNOUNCE_MODEL },
    });
    const event: SessionEvent<"assistant/message"> = {
      type: "assistant/message",
      seq: SessionSeq(this.seq),
      time: this.lastTime,
      // The assistant/message envelope requires the exact timed model stream
      // and a surface op; an announcement has no model stream, so it records
      // an empty one and appends like any assistant message. The derive/render
      // path reads only the message id/content and the event time/seq.
      data: { turn: 1, step: 1, message, stream: [] },
      surfaceOp: "append",
    };
    this.log = [...this.log, event];
    for (const listener of [...this.listeners]) {
      listener(event);
    }
  }
}
