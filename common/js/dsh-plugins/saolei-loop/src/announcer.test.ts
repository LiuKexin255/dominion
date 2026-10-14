/**
 * Unit tests for {@link AnnouncerMember}
 * (specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §3,
 * contracts/team-member-source.md §1/§2): the announce-only source capability
 * bits, the `assistant/message` log entry shape (unique MessageId, single
 * text block, synthetic model provenance, monotonic seq/time), synchronous
 * subscriber notification with unsubscribe, snapshot log semantics, and the
 * empty-text fail-loud rule.
 */

import type { SessionEvent } from "@deepseek-ai/dsh-session";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import type { GameEventRecord } from "@dominion/dsh-saolei";
import { describe, expect, it, vi } from "vitest";

import {
  ANNOUNCER_MEMBER_ROLE,
  ANNOUNCER_MEMBER_SUMMARY,
  AnnouncerMember,
} from "./announcer.js";

const SESSION = "templates/saolei/sessions/s1";
const ANNOUNCER_ID = `${SESSION}/announcer`;

/** The assistant/message event at `index`, narrowed for field access. */
function announcedEvent(
  events: readonly SessionEvent[],
  index = 0,
): SessionEvent<"assistant/message"> {
  const event = events[index];
  if (event?.type !== "assistant/message") {
    throw new Error(`expected an assistant/message event at index ${index}`);
  }
  return event;
}

describe("AnnouncerMember.source", () => {
  it("is a TeamMemberSource with the announce-only capability bits", () => {
    const member = new AnnouncerMember(SESSION);

    expect(member.source.id).toBe(ANNOUNCER_ID);
    expect(member.source.consumes).toBe(false);
    expect(member.source.sectionTarget).toBeUndefined();
    expect(typeof member.source.subscribe).toBe("function");
    expect(member.source.events).toEqual([]);
  });

  it("exposes the log as snapshots: an announce does not mutate the previously read array", () => {
    const member = new AnnouncerMember(SESSION);
    const before = member.source.events;

    member.announce("第一局");

    expect(before).toEqual([]);
    expect(member.source.events).toHaveLength(1);
  });
});

describe("AnnouncerMember.announce", () => {
  it("appends one assistant/message event with a unique id, single text block and synthetic provenance", () => {
    const member = new AnnouncerMember(SESSION);

    member.announce("本局游戏结束：胜利。");
    member.announce("本局游戏结束：失败。");

    const events = member.source.events;
    expect(events).toHaveLength(2);
    const first = announcedEvent(events, 0);
    const second = announcedEvent(events, 1);
    expect(first.data.turn).toBe(1);
    expect(first.data.step).toBe(1);
    expect(first.data.message.role).toBe("assistant");
    expect(first.data.message.content).toEqual([
      { type: "text", text: "本局游戏结束：胜利。" },
    ]);
    // The provenance values are opaque structural stand-ins, never routed or
    // rendered (specs/076-agent-v2-optimize/contracts/announcer-member.md §1).
    expect(first.data.message.source).toMatchObject({
      kind: "model",
      provider: "saolei",
      model: "system",
    });
    // Every announcement mints its own MessageId.
    expect(String(first.data.message.id)).not.toBe(String(second.data.message.id));
  });

  it("keeps seq and time monotonic across synchronous announcements", () => {
    const member = new AnnouncerMember(SESSION);

    const before = Date.now();
    member.announce("第一局");
    member.announce("第二局");

    const first = announcedEvent(member.source.events, 0);
    const second = announcedEvent(member.source.events, 1);
    expect(first.seq).toBe(1);
    expect(second.seq).toBe(2);
    expect(second.time > first.time).toBe(true);
    // The stamp is at least one millisecond ahead of the wall clock, so it
    // cannot tie with an event already logged by another member.
    expect(first.time).toBeGreaterThanOrEqual(before + 1);
  });

  it("notifies subscribers synchronously and stops after unsubscribe", () => {
    const member = new AnnouncerMember(SESSION);
    const listener = vi.fn();
    const unsubscribe = member.source.subscribe?.(listener);

    member.announce("本局游戏结束：胜利。");
    // Synchronous delivery: the listener has seen the event by the time
    // announce returns.
    expect(listener).toHaveBeenCalledTimes(1);
    expect(announcedEvent([listener.mock.calls[0]?.[0] as SessionEvent]).seq).toBe(1);

    unsubscribe?.();
    member.announce("本局游戏结束：失败。");
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("fails loud on empty text without appending or notifying", () => {
    const member = new AnnouncerMember(SESSION);
    const listener = vi.fn();
    member.source.subscribe?.(listener);

    expect(() => member.announce("")).toThrow(/must not be empty/);

    expect(member.source.events).toEqual([]);
    expect(listener).not.toHaveBeenCalled();
  });
});

describe("AnnouncerMember.drive (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §2.2)", () => {
  const record: GameEventRecord = {
    status: "won",
    stats: {
      operationCount: 5,
      operationsByType: { click: 4, flag: 1, chord: 0 },
      correctFlags: 3,
      avgOpsPerMine: 1.67,
    },
    endedAt: 1_000,
  };
  const statsText =
    "本局游戏结束：胜利。\n本局共执行 5 个操作：click 4 次、flag 1 次、chord 0 次。";

  it("announces the latest terminal record once and settles", async () => {
    const member = new AnnouncerMember(SESSION, { peekTerminal: () => record });

    expect(await member.drive()).toEqual({ kind: "settled" });
    expect(announcedText(member)).toBe(statsText);
    expect(member.consumes).toBe(false);
    expect(member.busy).toBe(false);
    expect(member.pendingInput).toBe(0);

    // The record-identity guard makes a re-driven announce a no-op.
    expect(await member.drive()).toEqual({ kind: "settled" });
    expect(member.source.events).toHaveLength(1);
  });

  it("settles without announcing while no terminal record exists", async () => {
    const member = new AnnouncerMember(SESSION, { peekTerminal: () => null });

    expect(await member.drive()).toEqual({ kind: "settled" });
    expect(member.source.events).toEqual([]);
  });

  it("reports failed{INTERNAL} for a render/announce error", async () => {
    const broken = { status: "won", endedAt: 0 } as unknown as GameEventRecord;
    const member = new AnnouncerMember(SESSION, { peekTerminal: () => broken });

    const outcome = await member.drive();
    expect(outcome).toMatchObject({ kind: "failed", code: "INTERNAL" });
  });

  it("has no delivery semantics and reports resting when awaited", async () => {
    const member = new AnnouncerMember(SESSION, { peekTerminal: () => null });

    expect(() => member.deliver(announcementMessage())).not.toThrow();
    expect(await member.whenSettled()).toEqual({ kind: "resting" });
    expect(() => member.abort()).not.toThrow();
    expect(() => member.dispose()).not.toThrow();
  });

  function announcedText(member: AnnouncerMember): string {
    return member.source.events
      .map((event) =>
        event.type === "assistant/message"
          ? event.data.message.content
              .map((block) => (block.type === "text" ? block.text : ""))
              .join("")
          : "",
      )
      .join("");
  }

  function announcementMessage() {
    return createUserMessage({
      content: [{ type: "text", text: "ignore" }],
      source: { kind: "user" },
    });
  }
});

describe("ANNOUNCER_MEMBER_ROLE / ANNOUNCER_MEMBER_SUMMARY", () => {
  it("carries the wire role and roster duty line of the announcer member", () => {
    expect(ANNOUNCER_MEMBER_ROLE).toBe("announcer");
    expect(ANNOUNCER_MEMBER_SUMMARY).toBe("终局播报对局结果与操作统计");
  });
});
