/**
 * Unit tests for {@link SaoleiSystemMember}
 * (specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §3,
 * contracts/team-member-source.md §1/§2): the announce-only source capability
 * bits, the `assistant/message` log entry shape (unique MessageId, single
 * text block, synthetic model provenance, monotonic seq/time), synchronous
 * subscriber notification with unsubscribe, snapshot log semantics, and the
 * empty-text fail-loud rule.
 */

import type { SessionEvent } from "@deepseek-ai/dsh-session";
import { describe, expect, it, vi } from "vitest";

import {
  SAOLEI_MEMBER_ROLE,
  SAOLEI_MEMBER_SUMMARY,
  SaoleiSystemMember,
} from "./announcer.js";

const SESSION = "templates/saolei/sessions/s1";
const ANNOUNCER_ID = `${SESSION}/saolei`;

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

describe("SaoleiSystemMember.source", () => {
  it("is a TeamMemberSource with the announce-only capability bits", () => {
    const member = new SaoleiSystemMember(SESSION);

    expect(member.source.id).toBe(ANNOUNCER_ID);
    expect(member.source.consumes).toBe(false);
    expect(member.source.sectionTarget).toBeUndefined();
    expect(typeof member.source.subscribe).toBe("function");
    expect(member.source.events).toEqual([]);
  });

  it("exposes the log as snapshots: an announce does not mutate the previously read array", () => {
    const member = new SaoleiSystemMember(SESSION);
    const before = member.source.events;

    member.announce("第一局");

    expect(before).toEqual([]);
    expect(member.source.events).toHaveLength(1);
  });
});

describe("SaoleiSystemMember.announce", () => {
  it("appends one assistant/message event with a unique id, single text block and synthetic provenance", () => {
    const member = new SaoleiSystemMember(SESSION);

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
    expect(first.data.message.source).toMatchObject({
      kind: "model",
      provider: "saolei",
      model: "system",
    });
    // Every announcement mints its own MessageId.
    expect(String(first.data.message.id)).not.toBe(String(second.data.message.id));
  });

  it("keeps seq and time monotonic across synchronous announcements", () => {
    const member = new SaoleiSystemMember(SESSION);

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
    const member = new SaoleiSystemMember(SESSION);
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
    const member = new SaoleiSystemMember(SESSION);
    const listener = vi.fn();
    member.source.subscribe?.(listener);

    expect(() => member.announce("")).toThrow(/must not be empty/);

    expect(member.source.events).toEqual([]);
    expect(listener).not.toHaveBeenCalled();
  });
});

describe("SAOLEI_MEMBER_ROLE / SAOLEI_MEMBER_SUMMARY", () => {
  it("carries the wire role and roster duty line of the saolei system member", () => {
    expect(SAOLEI_MEMBER_ROLE).toBe("saolei");
    expect(SAOLEI_MEMBER_SUMMARY).toBe("扫雷系统，终局播报对局结果与操作统计");
  });
});
