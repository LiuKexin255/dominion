/**
 * turn-loop.test.ts — Unit tests for the queue + single-flight turn loop
 * (`turn-loop.ts`).
 *
 * Covers the Phase 2 (T002) contract cases:
 *  - submit-while-idle starts the loop.
 *  - submit-while-running buffers and does NOT disturb the in-flight turn.
 *  - empty buffer ⇒ exactly one terminal `wait`.
 *  - abort clears the buffer + emits `wait` (FR-011).
 *  - non-abort turn error retains the buffer + emits `warn` (FR-015).
 *
 * Phase 4 (T009): multiple queued messages combine into ONE aggregated turn,
 * FIFO.
 *
 * Phase 5 (T010): `QueueSignal` depth-change emission assertions — submit⇒new
 * depth, drain-to-next-turn⇒0, abort⇒0, idle⇒no extra signal, non-abort
 * error retains buffer⇒no signal
 * (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2).
 *
 * Phase 5 Batch 2 (specs/031-team-template-mode T017): the loop's turn
 * dependency is the injected {@link TurnRunner} (a `(content, signal) =>
 * AsyncIterable<TurnBlock>` team-graph runner — research.md D10), replacing
 * the former `AgentAdapter` provider; frames carry the team `agent` field
 * (D12).
 *
 * Mock strategy (`style/javascript.md` §Mock): the `runner` and the
 * `emit` sink are constructor-injected dependencies — tests pass plain
 * fakes/stubs (no `vi.mock` module interception; see
 * [vitest — Mocking Modules Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)).
 */

import { describe, expect, it } from "vitest";

import type { ContentBlock, TurnContent } from "./llm";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import { TurnLoop } from "./turn-loop";
import type { TurnBlock, TurnRunner } from "./turn-loop";

// ---------------------------------------------------------------------------
// Test fakes / helpers
// ---------------------------------------------------------------------------

/**
 * Extract the concatenated text of a `TurnContent` (flat `text` OR the FIFO
 * `parts` array produced by `combineAll`). Test fakes echo this so they remain
 * agnostic to the single-message vs aggregated shape.
 */
function extractText(content: TurnContent): string {
  if (content.parts) {
    return content.parts.map((p) => p.text ?? "").join("");
  }
  return content.text ?? "";
}

/** A releasable gate so a fake turn can simulate an in-flight (blocking) turn. */
interface Gate {
  promise: Promise<void>;
  resolve: () => void;
}

function makeGate(): Gate {
  let resolve!: () => void;
  const promise = new Promise<void>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

/**
 * Fake team-graph turn runner that echoes the turn's text as a `reply:<text>`
 * block tagged with the given `agent`. If a `gate` is supplied it awaits it
 * before yielding (simulating an in-flight turn). If `throwAfterGate` is set
 * it rejects instead of yielding (simulating a non-abort turn error). The
 * gate wait is abort-aware so an `abort()` during the wait unblocks the
 * generator (mirroring LangGraph's signal handling).
 *
 * The echoed text is the concatenated text of the `TurnContent` (flat OR
 * aggregated `parts`) via {@link extractText}, so the fake is agnostic to the
 * single-message vs combined-turn shape.
 *
 * Pass `recordCalls` to capture each runner input for combine-shape
 * assertions (US3 multi-message tests).
 */
function makeEchoRunner(
  agent: string,
  opts: {
    gate?: Gate;
    throwAfterGate?: string;
    recordCalls?: TurnContent[];
  } = {},
): TurnRunner {
  const { gate, throwAfterGate, recordCalls } = opts;
  return async function* echoRunner(
    content: TurnContent,
    signal?: AbortSignal,
  ): AsyncIterable<TurnBlock> {
    recordCalls?.push(content);
    if (gate) {
      // Race the gate against an abort so abort() unblocks the await.
      const abort = new Promise<never>((_, reject) => {
        if (signal?.aborted) reject(new Error("aborted"));
        signal?.addEventListener("abort", () => reject(new Error("aborted")), {
          once: true,
        });
      });
      await Promise.race([gate.promise, abort]).catch(() => {
        // Swallow: the signal.aborted check below gates yielding.
      });
      if (signal?.aborted) return;
    }
    if (throwAfterGate) {
      throw new Error(throwAfterGate);
    }
    yield { agent, block: { type: "text", text: `reply:${extractText(content)}` } };
  };
}

/** Recording emit sink: collects every emitted AgentFrame. */
function makeRecordingEmit(): {
  emit: (f: AgentFrame) => void;
  frames: AgentFrame[];
} {
  const frames: AgentFrame[] = [];
  const emit = (f: AgentFrame): void => {
    frames.push(f);
  };
  return { emit, frames };
}

/** Extract terminal `wait` frames from the recorded emission. */
function waitFrames(frames: AgentFrame[]): AgentFrame[] {
  return frames.filter((f) => {
    const fr = f as Record<string, unknown>;
    return (
      fr.payload === "flowParts" &&
      (fr.flowParts as { parts: Record<string, unknown>[] } | undefined)?.parts?.some(
        (p) => "wait" in p,
      ) === true
    );
  });
}

/** Extract `warn` frames from the recorded emission. */
function warnFrames(frames: AgentFrame[]): AgentFrame[] {
  return frames.filter((f) => {
    const fr = f as Record<string, unknown>;
    return (
      fr.payload === "flowParts" &&
      (fr.flowParts as { parts: Record<string, unknown>[] } | undefined)?.parts?.some(
        (p) => "warn" in p,
      ) === true
    );
  });
}

/**
 * Extract the sequence of `QueueSignal.queued_count` values emitted, in order
 * (Phase 5 / T010 — specs/030-queued-chat-input/contracts/queue-channel-contract.md
 * §2). Used to assert the depth-change emission rules: submit⇒+1/new depth,
 * drain-to-next-turn⇒0, abort⇒0.
 */
function queueSignalDepths(frames: AgentFrame[]): number[] {
  const depths: number[] = [];
  for (const f of frames) {
    const fr = f as Record<string, unknown>;
    if (fr.payload !== "flowParts") continue;
    const parts = (fr.flowParts as { parts: Record<string, unknown>[] } | undefined)
      ?.parts ?? [];
    for (const p of parts) {
      const queue = (p as { queue?: { queuedCount?: number } }).queue;
      if (queue && typeof queue.queuedCount === "number") {
        depths.push(queue.queuedCount);
      }
    }
  }
  return depths;
}

/** Extract the agent-emitted text block contents, in order. */
function textContents(frames: AgentFrame[]): string[] {
  const out: string[] = [];
  for (const f of frames) {
    const fr = f as Record<string, unknown>;
    if (fr.payload !== "messageParts") continue;
    const parts = (fr.messageParts as { parts: Record<string, unknown>[] } | undefined)
      ?.parts ?? [];
    for (const p of parts) {
      const content = (p as { text?: { content?: string } }).text?.content;
      if (content) out.push(content);
    }
  }
  return out;
}

function flush(ms = 0): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

const SID = "sid-loop";
const TID = "saolei";
const AGENT = "player";

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("TurnLoop", () => {
  it("submit while idle starts the loop and emits blocks + a terminal wait", async () => {
    const adapter = makeEchoRunner("player");
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      TID,
      adapter,
      emit,
      AGENT,
    );

    expect(loop.isRunning()).toBe(false);

    loop.submit({ text: "hello" });
    // Loop started synchronously (running flips immediately).
    expect(loop.isRunning()).toBe(true);

    await flush();

    expect(loop.isRunning()).toBe(false);
    expect(textContents(frames)).toEqual(["reply:hello"]);
    expect(waitFrames(frames)).toHaveLength(1);
    // Every outbound frame carries the REQUIRED template_id alongside
    // session_id (api-contract.md §3.6): the gateway/desktop inject both, the
    // agent passes its session-scoped template through on outbound frames.
    expect(frames.length).toBeGreaterThan(0);
    expect(frames.every((f) => f.sessionId === SID && f.templateId === TID)).toBe(true);
    // Phase 5 (T010): an IDLE submit starts a turn (depth stays 0), so NO
    // QueueSignal is emitted
    // (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2:
    // only submit-while-RUNNING grows the buffer and emits a signal).
    expect(queueSignalDepths(frames)).toEqual([]);
  });

  it("submit while running buffers and does not disturb the in-flight turn", async () => {
    const gate = makeGate();
    const adapter = makeEchoRunner("player", { gate });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      TID,
      adapter,
      emit,
      AGENT,
    );

    // Turn 1 starts and blocks in generateTurn on the gate.
    loop.submit({ text: "msg-1" });
    await flush();
    expect(loop.isRunning()).toBe(true);

    // While turn 1 is in flight, a second submission is buffered (FR-002: it
    // MUST NOT alter the in-flight turn).
    loop.submit({ text: "msg-2" });
    expect(loop.queueDepth()).toBe(1);
    expect(loop.isRunning()).toBe(true);

    // Release turn 1: it completes with its OWN reply (undisturbed), then the
    // queued msg-2 becomes the next turn on the same thread_id.
    gate.resolve();
    await flush(20);

    // Turn 1's output is "reply:msg-1" (not affected by the queued msg-2);
    // msg-2 then became the next turn → "reply:msg-2"; single terminal wait.
    expect(textContents(frames)).toEqual(["reply:msg-1", "reply:msg-2"]);
    expect(waitFrames(frames)).toHaveLength(1);
    expect(loop.queueDepth()).toBe(0);
    expect(loop.isRunning()).toBe(false);
    // Phase 5 (T010): QueueSignal depth-change sequence is [1] (submit while
    // running grows the buffer to 1) then [0] (drain into the next turn
    // clears the buffer)
    // (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2).
    expect(queueSignalDepths(frames)).toEqual([1, 0]);
  });

  it("emits exactly one terminal wait when the buffer is empty", async () => {
    const adapter = makeEchoRunner("player");
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      TID,
      adapter,
      emit,
      AGENT,
    );

    loop.submit({ text: "only" });
    await flush();

    // No queued messages ⇒ exactly one wait at the very end (FR-006: never an
    // empty turn, never a missing wait).
    expect(waitFrames(frames)).toHaveLength(1);
    expect(textContents(frames)).toEqual(["reply:only"]);
  });

  it("abort clears the buffer and emits wait (FR-011)", async () => {
    const gate = makeGate();
    const adapter = makeEchoRunner("player", { gate });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      TID,
      adapter,
      emit,
      AGENT,
    );

    // Turn 1 in flight (blocked on gate); queue a second message.
    loop.submit({ text: "msg-1" });
    await flush();
    loop.submit({ text: "msg-2" });
    expect(loop.queueDepth()).toBe(1);
    expect(loop.isRunning()).toBe(true);

    // Abort: discards the queue (FR-011) and returns the desktop to ready.
    loop.abort();

    await flush(20);

    expect(loop.queueDepth()).toBe(0);
    expect(loop.isRunning()).toBe(false);
    expect(waitFrames(frames)).toHaveLength(1);
    // The queued msg-2 was discarded — no turn ran for it.
    expect(textContents(frames)).toEqual([]);
    // Phase 5 (T010): QueueSignal [1] on submit, then [0] when abort clears
    // the buffer (the depth-0 signal precedes the terminal `wait`)
    // (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2).
    expect(queueSignalDepths(frames)).toEqual([1, 0]);
  });

  it("non-abort turn error retains the buffer and emits warn (FR-015)", async () => {
    const gate = makeGate();
    const adapter = makeEchoRunner("player", { gate, throwAfterGate: "boom" });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      TID,
      adapter,
      emit,
      AGENT,
    );

    // Turn 1 in flight; queue a second message before the error fires.
    loop.submit({ text: "msg-1" });
    await flush();
    loop.submit({ text: "msg-2" });
    expect(loop.queueDepth()).toBe(1);

    // Release the gate → generateTurn throws a non-abort error.
    gate.resolve();
    await flush(20);

    // FR-015: buffer RETAINED (not dropped), warn surfaced, loop terminates
    // to idle with a wait.
    expect(loop.queueDepth()).toBe(1);
    expect(loop.isRunning()).toBe(false);
    expect(warnFrames(frames)).toHaveLength(1);
    expect(waitFrames(frames)).toHaveLength(1);
    // No turn output (the turn errored before yielding any block).
    expect(textContents(frames)).toEqual([]);
    // Phase 5 (T010): QueueSignal [1] on submit; NO signal on the non-abort
    // error — the buffer is RETAINED (FR-015) so the depth is unchanged, and
    // the contract only emits on depth change
    // (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2).
    expect(queueSignalDepths(frames)).toEqual([1]);
  });

  it("abort when idle is a no-op", async () => {
    const adapter = makeEchoRunner("player");
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      TID,
      adapter,
      emit,
      AGENT,
    );

    // IDLE abort must not emit anything or flip state.
    loop.abort();
    expect(loop.isRunning()).toBe(false);
    expect(frames).toHaveLength(0);
  });

  // -------------------------------------------------------------------------
  // US3 (Phase 4 / T009): multiple queued messages combine into ONE aggregated
  // turn, FIFO (specs/030-queued-chat-input/research.md D3; quickstart.md
  // Scenario 3; turn-loop-contract.md loop body).
  // -------------------------------------------------------------------------

  it("combines ALL pending queued messages into one aggregated turn (FIFO)", async () => {
    // Scenario 3: submit A (initial), then B, then C while A is in flight. On
    // A's completion the buffer [B, C] is merged into ONE aggregated
    // HumanMessage whose parts are [B, C] in submission order — exactly ONE
    // next turn runs (not one per message).
    const gate = makeGate();
    const calls: TurnContent[] = [];
    const adapter = makeEchoRunner("player", { gate, recordCalls: calls });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      TID,
      adapter,
      emit,
      AGENT,
    );

    loop.submit({ text: "A" });
    await flush();
    expect(loop.isRunning()).toBe(true);

    loop.submit({ text: "B" });
    loop.submit({ text: "C" });
    expect(loop.queueDepth()).toBe(2);

    gate.resolve();
    await flush(20);

    // Exactly two generateTurn invocations: turn 1 = A, turn 2 = aggregated
    // [B, C]. NOT three turns (one-per-message was the Phase 2 behaviour).
    expect(calls).toHaveLength(2);
    expect(extractText(calls[0])).toBe("A");
    // The aggregated turn carries B and C as ordered parts (FR-004/FR-005).
    expect(calls[1].parts).toEqual([{ text: "B" }, { text: "C" }]);

    // Turn 2's single reply concatenates B+C (one LLM-facing turn).
    expect(textContents(frames)).toEqual(["reply:A", "reply:BC"]);
    // Buffer fully drained; single terminal wait.
    expect(loop.queueDepth()).toBe(0);
    expect(loop.isRunning()).toBe(false);
    expect(waitFrames(frames)).toHaveLength(1);
    // Phase 5 (T010): QueueSignal goes 1→2 (B then C submitted while running),
    // then 0 when the combined turn drains the buffer
    // (specs/030-queued-chat-input/quickstart.md Scenario 3).
    expect(queueSignalDepths(frames)).toEqual([1, 2, 0]);
  });

  it("combines queued messages preserving screenshots in FIFO order", async () => {
    // An image-bearing queued message MUST survive the combine intact: its
    // text and screenshot become distinct parts of the aggregated turn in
    // submission order (research.md D3 — no loss of text or screenshots).
    const gate = makeGate();
    const calls: TurnContent[] = [];
    const adapter = makeEchoRunner("player", { gate, recordCalls: calls });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      TID,
      adapter,
      emit,
      AGENT,
    );

    loop.submit({ text: "first" });
    await flush();
    loop.submit({ text: "look", imageData: "img", imageMimeType: "image/png" });
    loop.submit({ text: "then act" });
    expect(loop.queueDepth()).toBe(2);

    gate.resolve();
    await flush(20);

    // One aggregated turn from the two buffered messages; the screenshot
    // part sits between the two text parts in FIFO order.
    expect(calls).toHaveLength(2);
    expect(calls[1].parts).toEqual([
      {
        text: "look",
        image: { data: "img", mimeType: "image/png" },
      },
      { text: "then act" },
    ]);
    expect(textContents(frames)).toEqual([
      "reply:first",
      "reply:lookthen act",
    ]);
    expect(loop.queueDepth()).toBe(0);
    expect(waitFrames(frames)).toHaveLength(1);
  });

  it("a single queued message still drains as exactly one turn (N=1 combine)", async () => {
    // Backward-compat boundary: with only ONE buffered message the combine
    // yields a single-part aggregated turn — behaviour identical to the
    // Phase 2 single-message drain, just expressed via `parts`.
    const gate = makeGate();
    const calls: TurnContent[] = [];
    const adapter = makeEchoRunner("player", { gate, recordCalls: calls });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      TID,
      adapter,
      emit,
      AGENT,
    );

    loop.submit({ text: "turn-1" });
    await flush();
    loop.submit({ text: "turn-2" });
    expect(loop.queueDepth()).toBe(1);

    gate.resolve();
    await flush(20);

    expect(calls).toHaveLength(2);
    expect(calls[1].parts).toEqual([{ text: "turn-2" }]);
    expect(textContents(frames)).toEqual(["reply:turn-1", "reply:turn-2"]);
    expect(loop.queueDepth()).toBe(0);
    expect(waitFrames(frames)).toHaveLength(1);
  });

  // -------------------------------------------------------------------------
  // Phase 5 (T010): QueueSignal depth-change emission rules
  // (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2).
  // submit⇒+1/new depth; drain-to-next-turn⇒0; abort⇒0; idle⇒no extra signal;
  // non-abort error retains buffer⇒no signal.
  // -------------------------------------------------------------------------

  it("emits QueueSignal(new depth) on each submit while running", async () => {
    // Submit A (initial turn, blocks), then B, C, D while running. The depth
    // signal sequence MUST be [1, 2, 3] — one signal per submit, each carrying
    // the buffer length AFTER the push (queue-channel-contract.md §2).
    const gate = makeGate();
    const adapter = makeEchoRunner("player", { gate });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(SID, TID, adapter, emit, AGENT);

    loop.submit({ text: "A" });
    await flush();
    expect(loop.isRunning()).toBe(true);

    loop.submit({ text: "B" });
    loop.submit({ text: "C" });
    loop.submit({ text: "D" });
    expect(loop.queueDepth()).toBe(3);

    // Each submit-while-running emitted its post-push depth, in order, BEFORE
    // the drain (no drain signal yet because the gate is still held).
    expect(queueSignalDepths(frames)).toEqual([1, 2, 3]);

    gate.resolve();
    await flush(20);

    // Drain into the single combined turn emits the final depth-0 signal.
    expect(queueSignalDepths(frames)).toEqual([1, 2, 3, 0]);
    expect(waitFrames(frames)).toHaveLength(1);
  });

  it("emits no extra QueueSignal at idle (depth is already 0)", async () => {
    // Contract §2: "Loop reaches idle (emits wait) — depth is already 0; no
    // extra signal required." A single turn with no queue drains straight to
    // idle, so NO QueueSignal is emitted at all.
    const adapter = makeEchoRunner("player");
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(SID, TID, adapter, emit, AGENT);

    loop.submit({ text: "solo" });
    await flush();

    expect(waitFrames(frames)).toHaveLength(1);
    expect(queueSignalDepths(frames)).toEqual([]);
  });

  it("emits QueueSignal(0) before wait on abort (FR-011)", async () => {
    // Contract §2: "Abort clears the buffer → QueueSignal(0) then wait." The
    // depth-0 signal MUST precede the terminal wait so the desktop drops the
    // pending indicator before returning to ready.
    const gate = makeGate();
    const adapter = makeEchoRunner("player", { gate });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(SID, TID, adapter, emit, AGENT);

    loop.submit({ text: "in-flight" });
    await flush();
    loop.submit({ text: "queued" });
    expect(queueSignalDepths(frames)).toEqual([1]);

    loop.abort();
    await flush(20);

    // The abort terminal emitted QueueSignal(0) BEFORE the wait frame. Assert
    // ordering by finding the wait frame's index vs the trailing depth-0.
    const depths = queueSignalDepths(frames);
    expect(depths).toEqual([1, 0]);
    const waitIdx = frames.findIndex((f) => {
      const fr = f as Record<string, unknown>;
      return (
        fr.payload === "flowParts" &&
        (fr.flowParts as { parts: Record<string, unknown>[] } | undefined)?.parts?.some(
          (p) => "wait" in p,
        ) === true
      );
    });
    const queueZeroIdx = frames.findIndex((f) => {
      const fr = f as Record<string, unknown>;
      if (fr.payload !== "flowParts") return false;
      return (fr.flowParts as { parts: Record<string, unknown>[] }).parts.some(
        (p) => (p as { queue?: { queuedCount?: number } }).queue?.queuedCount === 0,
      );
    });
    expect(queueZeroIdx).toBeGreaterThanOrEqual(0);
    expect(waitIdx).toBeGreaterThan(queueZeroIdx);
  });
});
