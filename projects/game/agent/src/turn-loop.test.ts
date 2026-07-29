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
 */
function makeEchoAdapter(opts: {
  gate?: Gate;
  throwAfterGate?: string;
} = {}): AgentAdapter {
  const { gate, throwAfterGate } = opts;
  return {
    async *generateTurn(
      _threadId: string,
      content: TurnContent,
      signal?: AbortSignal,
    ): AsyncIterable<ContentBlock> {
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
      yield { type: "text", text: `reply:${content.text ?? ""}` };
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
});
