/**
 * SaoleiSystemMember — the saolei team's non-agent system member: an
 * announce-only {@link TeamMemberSource} that carries the game-over summary
 * into the group chat. Its contract is
 * specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §3 plus
 * the member-message-source interface of
 * specs/065-agent-v2-team-refine/contracts/team-member-source.md §1/§2.
 *
 * The output log is a process-memory, structurally session-event-compatible
 * `assistant/message` list: team derivation/rendering/consumption treats it
 * exactly like an agent member's log, so one announcement is one speech unit
 * without team knowing any "system broadcast" concept. The log lives for the
 * team materialization's lifetime (a refresh disposes it — the same
 * short-term memory semantics agent logs have,
 * specs/065-agent-v2-team-refine/research.md D9). The constructed `turn`/`step`
 * are fixed placeholders: the derive/render path reads only the message
 * id/content and the event time/seq.
 */

import type { SessionEvent } from "@deepseek-ai/dsh-session";
import { createAssistantMessage } from "@deepseek-ai/dsh-llm";
import type { TeamMemberSource } from "@dominion/dsh-team";

/** The saolei system member's wire role — roster registration and merge sender label (specs/065-agent-v2-team-refine/data-model.md §1.2). */
export const SAOLEI_MEMBER_ROLE = "saolei";

/** Roster summary of the saolei system member (the team section's saolei line). */
export const SAOLEI_MEMBER_SUMMARY = "扫雷系统，终局播报对局结果与操作统计";

/**
 * Synthetic model provenance of an announcement. The source shape is required
 * by `createAssistantMessage` (an assistant message always records a routed
 * model), but the value is never routed or rendered; it only keeps the event
 * structurally compatible with the shared session-event vocabulary.
 */
const ANNOUNCE_PROVIDER = "saolei";
const ANNOUNCE_MODEL = "system";

/**
 * The system member's message log and source face. `announce(text)` appends
 * one `assistant/message` event (unique MessageId, single text block) and
 * synchronously notifies subscribers; `source` exposes that log to the team
 * with the announce-only capability bits (`consumes: false`, no
 * `sectionTarget`).
 */
export class SaoleiSystemMember {
  /** The team-facing message source (`id = ${session}/saolei`). */
  readonly source: TeamMemberSource;

  /** The immutable log snapshots; replaced (not mutated) on every announce. */
  private log: readonly SessionEvent[] = [];
  private readonly listeners = new Set<(event: SessionEvent) => void>();
  private seq = 0;
  private lastTime = 0;

  constructor(session: string) {
    const member = this;
    this.source = {
      id: `${session}/saolei`,
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
      throw new Error("saolei announcer: announcement text must not be empty");
    }
    this.seq += 1;
    this.lastTime = Math.max(this.lastTime + 1, Date.now() + 1);
    const message = createAssistantMessage({
      content: [{ type: "text", text }],
      source: { provider: ANNOUNCE_PROVIDER, model: ANNOUNCE_MODEL },
    });
    const event: SessionEvent<"assistant/message"> = {
      type: "assistant/message",
      seq: this.seq,
      time: this.lastTime,
      data: { turn: 1, step: 1, message },
    };
    this.log = [...this.log, event];
    for (const listener of [...this.listeners]) {
      listener(event);
    }
  }
}
