/**
 * turn-loop.test.ts — Unit tests for the LangGraph-native queue + single-flight
 * loop (`turn-loop.ts`).
 *
 * Covers the Phase 2 (T002) contract cases:
 *  - submit-while-idle starts the loop.
 *  - submit-while-running buffers and does NOT disturb the in-flight turn.
 *  - empty buffer ⇒ exactly one terminal `wait`.
 *  - abort clears the buffer + emits `wait` (FR-011).
 *  - non-abort turn error retains the buffer + emits `warn` (FR-015).
 *
 * Mock strategy (`style/javascript.md` §Mock): the `adapterProvider` and the
 * `emit` sink are constructor-injected dependencies — tests pass plain
 * fakes/stubs (no `vi.mock` module interception; see
 * [vitest — Mocking Modules Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)).
 */

import { describe, expect, it } from "vitest";

import type { AgentAdapter, ContentBlock, TurnContent } from "./llm";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import { TurnLoop } from "./turn-loop";

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
 * Fake adapter that echoes the turn's text as a `reply:<text>` block. If a
 * `gate` is supplied it awaits it before yielding (simulating an in-flight
 * turn). If `throwAfterGate` is set it rejects instead of yielding (simulating
 * a non-abort turn error). The gate wait is abort-aware so an `abort()` during
 * the wait unblocks the generator (mirroring LangGraph's signal handling).
 *
 * The echoed text is the concatenated text of the `TurnContent` (flat OR
 * aggregated `parts`) via {@link extractText}, so the fake is agnostic to the
 * single-message vs combined-turn shape.
 *
 * Pass `recordCalls` to capture each `generateTurn` content for combine-shape
 * assertions (US3 multi-message tests).
 */
function makeEchoAdapter(opts: {
  gate?: Gate;
  throwAfterGate?: string;
  recordCalls?: TurnContent[];
} = {}): AgentAdapter {
  const { gate, throwAfterGate, recordCalls } = opts;
  return {
    async *generateTurn(
      _threadId: string,
      content: TurnContent,
      signal?: AbortSignal,
    ): AsyncIterable<ContentBlock> {
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
      yield { type: "text", text: `reply:${extractText(content)}` };
    },
    async getState() {
      return null;
    },
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
const PROFILE = "p-loop";

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("TurnLoop", () => {
  it("submit while idle starts the loop and emits blocks + a terminal wait", async () => {
    const adapter = makeEchoAdapter();
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      async () => adapter,
      emit,
      PROFILE,
    );

    expect(loop.isRunning()).toBe(false);

    loop.submit({ text: "hello" });
    // Loop started synchronously (running flips immediately).
    expect(loop.isRunning()).toBe(true);

    await flush();

    expect(loop.isRunning()).toBe(false);
    expect(textContents(frames)).toEqual(["reply:hello"]);
    expect(waitFrames(frames)).toHaveLength(1);
  });

  it("submit while running buffers and does not disturb the in-flight turn", async () => {
    const gate = makeGate();
    const adapter = makeEchoAdapter({ gate });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      async () => adapter,
      emit,
      PROFILE,
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
  });

  it("emits exactly one terminal wait when the buffer is empty", async () => {
    const adapter = makeEchoAdapter();
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      async () => adapter,
      emit,
      PROFILE,
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
    const adapter = makeEchoAdapter({ gate });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      async () => adapter,
      emit,
      PROFILE,
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
  });

  it("non-abort turn error retains the buffer and emits warn (FR-015)", async () => {
    const gate = makeGate();
    const adapter = makeEchoAdapter({ gate, throwAfterGate: "boom" });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      async () => adapter,
      emit,
      PROFILE,
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
  });

  it("abort when idle is a no-op", async () => {
    const adapter = makeEchoAdapter();
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      async () => adapter,
      emit,
      PROFILE,
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
    const adapter = makeEchoAdapter({ gate, recordCalls: calls });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      async () => adapter,
      emit,
      PROFILE,
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
  });

  it("combines queued messages preserving screenshots in FIFO order", async () => {
    // An image-bearing queued message MUST survive the combine intact: its
    // text and screenshot become distinct parts of the aggregated turn in
    // submission order (research.md D3 — no loss of text or screenshots).
    const gate = makeGate();
    const calls: TurnContent[] = [];
    const adapter = makeEchoAdapter({ gate, recordCalls: calls });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      async () => adapter,
      emit,
      PROFILE,
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
    const adapter = makeEchoAdapter({ gate, recordCalls: calls });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(
      SID,
      async () => adapter,
      emit,
      PROFILE,
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
});
