import { describe, expect, it } from "vitest";
import {
  SessionSeq,
  TOOL_NOT_STARTED,
  TOOL_OUTCOME_UNKNOWN,
  ToolCallRecovery,
} from "@deepseek-ai/dsh-session";
import type { SessionEvent } from "@deepseek-ai/dsh-session";

/**
 * Repository-level coverage of the 0.2.0 failed-step tool-result recovery
 * (specs/073-dsh-0.2-upgrade/spec.md FR-007; semantics per the upstream note
 * https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/.agents/notes/implemented/bug-fix/2026-09-19-failed-step-tool-results.md).
 * The loop's step driver builds its conservative recovery results with the
 * same exported `ToolCallRecovery` before it closes a failed step, so driving
 * that component over a committed failure prefix pins the two recovery levels,
 * the assistant call order, the preservation of already-committed results, and
 * the balanced call/result history that keeps later requests valid.
 */

const CALL_STARTED = "call-started";
const CALL_NOT_STARTED = "call-not-started";
const CALL_COMMITTED = "call-committed";

/** One committed session event shaped like the live loop's log. */
function makeEvent(
  seq: number,
  type: SessionEvent["type"],
  data: unknown,
  extra: { surfaceOp?: "append"; sourceEventSeqs?: number[] } = {},
): SessionEvent {
  return {
    seq: SessionSeq(seq),
    time: 1_000 + seq,
    type,
    data,
    ...(extra.surfaceOp === undefined ? {} : { surfaceOp: extra.surfaceOp }),
    ...(extra.sourceEventSeqs === undefined
      ? {}
      : { sourceEventSeqs: extra.sourceEventSeqs.map((value) => SessionSeq(value)) }),
  } as unknown as SessionEvent;
}

/**
 * The committed prefix of one step whose tool scheduling failed before every
 * result was recorded: `CALL_STARTED` has its `tool/call` record but no result,
 * `CALL_NOT_STARTED` has only the assistant request, and `CALL_COMMITTED`
 * already has its result.
 */
function failedStepPrefix(): SessionEvent[] {
  return [
    makeEvent(0, "turn/start", { turn: 1 }),
    makeEvent(1, "step/start", { turn: 1, step: 1 }),
    makeEvent(
      2,
      "assistant/message",
      {
        turn: 1,
        step: 1,
        message: {
          id: "assistant-1",
          role: "assistant",
          content: [
            { type: "tool-call", id: CALL_STARTED, name: "read_file", arguments: "{}" },
            { type: "tool-call", id: CALL_NOT_STARTED, name: "write_file", arguments: "{}" },
            { type: "tool-call", id: CALL_COMMITTED, name: "echo", arguments: "{}" },
          ],
        },
        stream: [],
      },
      { surfaceOp: "append" },
    ),
    makeEvent(3, "tool/call", {
      turn: 1,
      step: 1,
      callId: CALL_STARTED,
      name: "read_file",
      arguments: "{}",
    }),
    makeEvent(
      4,
      "tool/result",
      {
        turn: 1,
        step: 1,
        message: {
          id: "tool-result-committed",
          role: "tool",
          source: { kind: "tool", callId: CALL_COMMITTED },
          toolCallId: CALL_COMMITTED,
          content: [{ type: "text", text: "echoed" }],
        },
      },
      { surfaceOp: "append" },
    ),
  ];
}

/** Every assistant tool-call id left without a matching tool/result. */
function unansweredCalls(events: readonly SessionEvent[]): string[] {
  const answered = new Set<string>();
  for (const event of events) {
    if (event.type === "tool/result") answered.add(String(event.data.message.toolCallId));
  }
  const unanswered: string[] = [];
  for (const event of events) {
    if (event.type !== "assistant/message") continue;
    for (const block of event.data.message.content) {
      if (block.type === "tool-call" && !answered.has(String(block.id))) {
        unanswered.push(String(block.id));
      }
    }
  }
  return unanswered;
}

/** The model-facing text of one recovery result. */
function resultText(event: SessionEvent<"tool/result"> | undefined): string {
  const block = event?.data.message.content[0];
  return block?.type === "text" ? block.text : "";
}

describe("failed-step tool-result recovery", () => {
  it("records the two conservative levels in assistant order and preserves committed results", () => {
    const recovery = new ToolCallRecovery();
    const prefix = failedStepPrefix();
    for (const event of prefix) recovery.observe(event);

    const results = recovery.results();

    // The started-but-unrecorded call is unknown (it may have had side
    // effects); the requested-but-never-started call is retryable — both in
    // assistant call order.
    expect(results.map((event) => String(event.data.message.toolCallId))).toEqual([
      CALL_STARTED,
      CALL_NOT_STARTED,
    ]);
    expect(results.map((event) => event.data.error?.code)).toEqual([
      TOOL_OUTCOME_UNKNOWN,
      TOOL_NOT_STARTED,
    ]);
    expect(results.map((event) => event.data.error?.name)).toEqual([
      "ToolOutcomeUnknownError",
      "ToolNotStartedError",
    ]);
    // Conservative error results: never success, cancellation, or absence.
    expect(results.every((event) => event.data.message.isError === true)).toBe(true);
    expect(results.every((event) => event.data.message.role === "tool")).toBe(true);
    expect(results.every((event) => event.surfaceOp === "append")).toBe(true);
    expect(results.every((event) => event.data.turn === 1 && event.data.step === 1)).toBe(true);
    // The recovery appends after the committed prefix and reuses its timestamp.
    expect(results.map((event) => Number(event.seq))).toEqual([5, 6]);
    expect(results.every((event) => event.time === 1_004)).toBe(true);
    // Cause-specific retry guidance: the unknown outcome warns against a blind
    // retry, while the not-started call is safe to retry.
    expect(resultText(results[0])).toContain("Do not retry blindly");
    expect(resultText(results[1])).toContain("Retry it if it is still needed");
    // Only the recorded call cites its call event as the source.
    expect(results[0]?.sourceEventSeqs?.map(Number)).toEqual([3]);
    expect(results[1]?.sourceEventSeqs).toBeUndefined();

    // The already-committed result is preserved, not re-recovered or rewritten.
    expect(results).toHaveLength(2);
    expect(results.some((event) => String(event.data.message.toolCallId) === CALL_COMMITTED)).toBe(
      false,
    );
    expect(prefix[4]?.data).toMatchObject({
      message: { toolCallId: CALL_COMMITTED, content: [{ type: "text", text: "echoed" }] },
    });
  });

  it("clears the pending calls once the recovery is committed, leaving a continuable history", () => {
    const recovery = new ToolCallRecovery();
    const prefix = failedStepPrefix();
    for (const event of prefix) recovery.observe(event);
    expect(unansweredCalls(prefix)).toEqual([CALL_STARTED, CALL_NOT_STARTED]);

    const recovered = recovery.results();
    // The live driver's session/event subscription observes what it appended.
    for (const event of recovered) recovery.observe(event);
    expect(recovery.results()).toEqual([]);

    // Every assistant request now has a matching result, so later provider
    // requests stay valid and the session can continue.
    expect(unansweredCalls([...prefix, ...recovered])).toEqual([]);

    // Closing the failed step leaves no stale recovery state behind.
    recovery.observe(makeEvent(7, "step/end", { turn: 1, step: 1 }));
    expect(recovery.results()).toEqual([]);
  });
});
