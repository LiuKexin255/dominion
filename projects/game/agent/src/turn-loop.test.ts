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
 * Feature 043 (T007, US2): a `NodeTimeoutError` stall takes the NON-abort
 * error terminal (`finishError` — the existing catch classifies it via
 * `controller.signal.aborted === false`,
 * specs/043-llm-stream-stall-recovery/contracts/stall-recovery-contract.md
 * §3.2/§3.3, so turn-loop.ts is unchanged): the queued buffer is RETAINED
 * (specs/043-llm-stream-stall-recovery/spec.md FR-006) and auto-drained as
 * the next turn's input (FR-007).
 *
 * Feature 043 (T013, SC-006): regression guard — the stall feature MUST NOT
 * change the existing abort semantics
 * (specs/043-llm-stream-stall-recovery/spec.md SC-006/FR-012): user abort
 * AND connection-drop abort still clear the buffer via `finishAbort`
 * (specs/030-queued-chat-input/spec.md FR-011), contrasted against the
 * stall path which retains it (specs/043-llm-stream-stall-recovery/spec.md
 * FR-008 — the two terminals stay distinct).
 *
 * Mock strategy (`style/javascript.md` §Mock): the `runner` and the
 * `emit` sink are constructor-injected dependencies — tests pass plain
 * fakes/stubs (no `vi.mock` module interception; see
 * [vitest — Mocking Modules Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)).
 */

import { describe, expect, it } from "vitest";

import { NodeTimeoutError } from "@langchain/langgraph";

import type { ContentBlock, TurnContent } from "./llm.js";
import type { TeamFrame } from "../game_types/projects/game/TeamFrame.js";
import { TurnLoop } from "./turn-loop.js";
import type { TurnBlock, TurnRunner } from "./turn-loop.js";

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

/**
 * Fake team-graph runner that simulates an LLM stream stall on its FIRST
 * invocation only: it yields one block ("partial"), then blocks on a `gate`
 * (abort-aware, like {@link makeEchoRunner}) before throwing a REAL LangGraph
 * `NodeTimeoutError` — the error the graph's `idleTimeout` raises when no
 * events arrive for the configured period
 * (specs/043-llm-stream-stall-recovery/contracts/stall-recovery-contract.md
 * §1.3). Later invocations
 * (the turns after recovery) echo normally so the test can observe the
 * retained buffer being drained into a subsequent turn.
 */
function makeStallRunner(
  agent: string,
  opts: {
    gate: Gate;
    recordCalls?: TurnContent[];
  },
): TurnRunner {
  const { gate, recordCalls } = opts;
  const timeoutError = new NodeTimeoutError({
    node: agent,
    kind: "idle",
    idleTimeout: 30000,
    elapsed: 30001,
  });
  let stallOnce = true;
  return async function* stallRunner(
    content: TurnContent,
    signal?: AbortSignal,
  ): AsyncIterable<TurnBlock> {
    recordCalls?.push(content);
    if (!stallOnce) {
      yield {
        agent,
        block: { type: "text", text: `reply:${extractText(content)}` },
      };
      return;
    }
    stallOnce = false;
    yield { agent, block: { type: "text", text: "partial" } };
    // Race the release gate against an abort so abort() unblocks the await
    // (same pattern as makeEchoRunner).
    const abort = new Promise<never>((_, reject) => {
      if (signal?.aborted) reject(new Error("aborted"));
      signal?.addEventListener("abort", () => reject(new Error("aborted")), {
        once: true,
      });
    });
    await Promise.race([gate.promise, abort]).catch(() => {
      // Swallow: the signal.aborted check below gates the throw.
    });
    if (signal?.aborted) return;
    throw timeoutError;
  };
}

/** Recording emit sink: collects every emitted TeamFrame. */
function makeRecordingEmit(): {
  emit: (f: TeamFrame) => void;
  frames: TeamFrame[];
} {
  const frames: TeamFrame[] = [];
  const emit = (f: TeamFrame): void => {
    frames.push(f);
  };
  return { emit, frames };
}

/** Extract terminal `wait` frames from the recorded emission. */
function waitFrames(frames: TeamFrame[]): TeamFrame[] {
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
function warnFrames(frames: TeamFrame[]): TeamFrame[] {
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
function queueSignalDepths(frames: TeamFrame[]): number[] {
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
function textContents(frames: TeamFrame[]): string[] {
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
  // Feature 043 (T007, US2): stall recovery — a `NodeTimeoutError` trigger
  // takes the NON-abort error terminal (`finishError`): queued messages are
  // RETAINED (specs/043-llm-stream-stall-recovery/spec.md FR-006) and
  // auto-drained as the next turn's input (FR-007). The existing catch
  // classifies it correctly
  // (specs/043-llm-stream-stall-recovery/contracts/stall-recovery-contract.md
  // §3.2/§3.3) — turn-loop.ts is unchanged;
  // this test verifies the emergent property for the stall trigger.
  // -------------------------------------------------------------------------

  it("retains the queued buffer through a NodeTimeoutError stall and auto-drains it on the next turn (043 US2: FR-006/FR-007)", async () => {
    const gate = makeGate();
    const calls: TurnContent[] = [];
    const runner = makeStallRunner(AGENT, { gate, recordCalls: calls });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(SID, TID, runner, emit, AGENT);

    // Turn 1 starts and streams one block; the stall is then released via
    // the gate. While the turn is RUNNING a second message is queued
    // (buffer depth 1 → QueueSignal(1), specs/030-queued-chat-input/contracts/queue-channel-contract.md §2).
    loop.submit({ text: "msg-1" });
    await flush();
    expect(loop.isRunning()).toBe(true);
    expect(textContents(frames)).toEqual(["partial"]);

    loop.submit({ text: "msg-2" });
    expect(loop.queueDepth()).toBe(1);
    expect(queueSignalDepths(frames)).toEqual([1]);

    // Release the stall: the runner throws the real LangGraph
    // NodeTimeoutError. It reaches runLoop's catch with
    // controller.signal.aborted === false (the idle timeout fires on
    // LangGraph's internal signal, NOT the TurnLoop's controller —
    // specs/043-llm-stream-stall-recovery/contracts/stall-recovery-contract.md
    // §3.2), so it is classified as a NON-abort error → finishError (§3.3 of
    // the same contract).
    gate.resolve();
    await flush(20);

    // FR-006 + FR-008: warn + wait emitted (the stall is surfaced and the
    // session returns to idle), and the buffer is RETAINED — the user's
    // queued message survives the stall, unlike a user abort which clears it
    // (specs/030-queued-chat-input/spec.md FR-011).
    expect(loop.isRunning()).toBe(false);
    expect(warnFrames(frames)).toHaveLength(1);
    // The warn frame's message is built by turn-loop.ts's `warnFrame()`,
    // which prefixes the error message with "Processing error: ". The
    // underlying text is the NodeTimeoutError message carrying the node, the
    // configured idle window and the elapsed time — the message format comes
    // from LangGraph's NodeTimeoutError class
    // (https://github.com/langchain-ai/langgraphjs/blob/main/libs/langgraph-core/src/errors.ts).
    expect(
      warnFrames(frames)[0]?.flowParts?.parts[0]?.warn?.message,
    ).toContain('Node "player" exceeded its idle timeout of 30000ms');
    expect(waitFrames(frames)).toHaveLength(1);
    expect(loop.queueDepth()).toBe(1);
    // No depth signal on the error terminal — the depth is unchanged
    // (specs/030-queued-chat-input/spec.md FR-015 retains the buffer;
    // queue-channel-contract.md §2 emits only on
    // depth change).
    expect(queueSignalDepths(frames)).toEqual([1]);

    // FR-007: after recovery the user submits a new message; it starts a new
    // turn, and at that turn's completion boundary the RETAINED msg-2 is
    // auto-drained as the next turn's combined input
    // (specs/030-queued-chat-input/spec.md FR-006 drain semantics) — the user
    // does not re-submit it.
    loop.submit({ text: "recovery-msg" });
    await flush(20);

    // Runner inputs: turn 1 = msg-1 (stalled), turn 2 = recovery-msg, turn 3
    // = the retained msg-2 drained from the buffer.
    expect(calls.map(extractText)).toEqual(["msg-1", "recovery-msg", "msg-2"]);
    expect(textContents(frames)).toEqual([
      "partial",
      "reply:recovery-msg",
      "reply:msg-2",
    ]);
    expect(loop.queueDepth()).toBe(0);
    expect(loop.isRunning()).toBe(false);
    // Two terminal waits: one at the stall recovery (finishError → idle —
    // specs/043-llm-stream-stall-recovery/spec.md FR-005, the session
    // returned to ready so the user can interact again) and one at the final
    // idle after the drained turn completed (specs/030-queued-chat-input/spec.md
    // FR-006 — empty queue at turn completion returns to idle).
    expect(waitFrames(frames)).toHaveLength(2);
    // The drain into the next turn emitted the depth-0 signal (§2).
    expect(queueSignalDepths(frames)).toEqual([1, 0]);
  });

  // -------------------------------------------------------------------------
  // Feature 043 (T013, SC-006): the stall feature MUST NOT change the
  // existing abort semantics — user abort and connection-drop abort still
  // clear the buffer via `finishAbort` (specs/030-queued-chat-input/spec.md
  // FR-011), while only the stall-induced termination retains it
  // (specs/043-llm-stream-stall-recovery/spec.md FR-006/FR-008, SC-006;
  // contract §5 scope boundary —
  // specs/043-llm-stream-stall-recovery/contracts/stall-recovery-contract.md).
  // -------------------------------------------------------------------------

  it("SC-006: user abort clears the queued buffer while a stall retains it (043 zero regression — FR-008 terminals stay distinct)", async () => {
    // Contrast test: two loops with the SAME stall runner and the SAME buffer
    // state (msg-1 in flight streaming "partial", msg-2 queued at depth 1).
    // The terminal outcome differs ONLY by trigger — gate release (stall →
    // finishError) vs abort() (→ finishAbort) — exercising the catch's
    // classification (contract §3.1, turn-loop.ts:352-358: aborting /
    // controller.signal.aborted → finishAbort, else finishError).
    const stallGate = makeGate();
    const stallRec = makeRecordingEmit();
    const stallLoop = new TurnLoop(
      SID,
      TID,
      makeStallRunner(AGENT, { gate: stallGate }),
      stallRec.emit,
      AGENT,
    );
    const abortGate = makeGate();
    const abortRec = makeRecordingEmit();
    const abortLoop = new TurnLoop(
      SID,
      TID,
      makeStallRunner(AGENT, { gate: abortGate }),
      abortRec.emit,
      AGENT,
    );

    stallLoop.submit({ text: "msg-1" });
    abortLoop.submit({ text: "msg-1" });
    await flush();
    stallLoop.submit({ text: "msg-2" });
    abortLoop.submit({ text: "msg-2" });
    expect(stallLoop.queueDepth()).toBe(1);
    expect(abortLoop.queueDepth()).toBe(1);

    // Branch 1 — STALL: the runner throws a real NodeTimeoutError →
    // finishError: buffer RETAINED, warn + wait, no depth signal
    // (specs/043-llm-stream-stall-recovery/spec.md FR-006/FR-008).
    stallGate.resolve();
    // Branch 2 — USER ABORT: abort() → finishAbort: buffer CLEARED,
    // QueueSignal(0) then wait, no warn (specs/030-queued-chat-input/spec.md
    // FR-011; specs/043-llm-stream-stall-recovery/spec.md FR-012).
    abortLoop.abort();
    await flush(20);

    // Same starting buffer depth, opposite outcomes — the stall feature did
    // not conflate the two terminals (SC-006).
    expect(stallLoop.queueDepth()).toBe(1);
    expect(abortLoop.queueDepth()).toBe(0);
    expect(warnFrames(stallRec.frames)).toHaveLength(1);
    expect(warnFrames(abortRec.frames)).toHaveLength(0);
    expect(queueSignalDepths(stallRec.frames)).toEqual([1]);
    expect(queueSignalDepths(abortRec.frames)).toEqual([1, 0]);
    expect(waitFrames(stallRec.frames)).toHaveLength(1);
    expect(waitFrames(abortRec.frames)).toHaveLength(1);
    expect(stallLoop.isRunning()).toBe(false);
    expect(abortLoop.isRunning()).toBe(false);
  });

  it("SC-006: connection-drop abort (stream close → abort()) clears the buffer without warn (Feature 026 path)", async () => {
    // Connection-drop abort and user abort share ONE entry point at the
    // TurnLoop level: `abort()` (session-team.ts:673-674 — `turnLoop.abort()`).
    // The bidi stream end/error chain is `abortLoops()` → `team.abort()` →
    // `turnLoop.abort()` (handler.ts:339-342,559,570 — the Feature 026
    // stream-close → abort chain, specs/026-agent-abort-crash-fix/spec.md
    // FR-001). So this test asserts BOTH triggers: the queue is discarded
    // (specs/030-queued-chat-input/spec.md FR-011; the stall feature leaves
    // this unchanged — specs/043-llm-stream-stall-recovery/spec.md FR-012)
    // and the abort terminal emits QueueSignal(0) + `wait` WITHOUT a `warn`
    // (finishAbort, never finishError — specs/026-agent-abort-crash-fix/spec.md
    // FR-003: no error/warn frames on the abort path).
    const gate = makeGate();
    const adapter = makeEchoRunner("player", { gate });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(SID, TID, adapter, emit, AGENT);

    // Turn 1 in flight (blocked on the gate); queue msg-2 before the "drop".
    loop.submit({ text: "msg-1" });
    await flush();
    loop.submit({ text: "msg-2" });
    expect(loop.queueDepth()).toBe(1);

    // The drop: abort() unblocks the runner's abort-aware gate wait, the
    // generator exits, and runLoop's abort check (turn-loop.ts:365) reaches
    // finishAbort — the queue is discarded (FR-011).
    loop.abort();
    await flush(20);

    expect(loop.queueDepth()).toBe(0);
    expect(loop.isRunning()).toBe(false);
    expect(warnFrames(frames)).toHaveLength(0);
    expect(waitFrames(frames)).toHaveLength(1);
    expect(queueSignalDepths(frames)).toEqual([1, 0]);
    // The queued msg-2 was discarded — no turn ran for it.
    expect(textContents(frames)).toEqual([]);
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

  // -------------------------------------------------------------------------
  // Feature 038 (T001): mid-turn `drainQueue()` — called by the player's
  // `queueDrain` `beforeModel` middleware via `configurable.drainQueuedInput`
  // (specs/038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md;
  // specs/038-queue-input-mid-turn/data-model.md §2).
  // -------------------------------------------------------------------------

  it("drainQueue on an empty buffer returns null and emits nothing", () => {
    const adapter = makeEchoRunner("player");
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(SID, TID, adapter, emit, AGENT);

    // Contract: "If the buffer is empty: return null (no-op, no emission)."
    // An IDLE loop has an empty buffer, so drainQueue must be a strict no-op —
    // no combined content, no QueueSignal, no state change.
    expect(loop.drainQueue()).toBeNull();
    expect(loop.queueDepth()).toBe(0);
    expect(loop.isRunning()).toBe(false);
    expect(frames).toHaveLength(0);
  });

  it("drainQueue returns combined content, emits QueueSignal(0), and clears the buffer", async () => {
    const gate = makeGate();
    const adapter = makeEchoRunner("player", { gate });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(SID, TID, adapter, emit, AGENT);

    // Turn 1 in flight (blocked on gate); buffer two messages mid-turn.
    loop.submit({ text: "msg-1" });
    await flush();
    loop.submit({ text: "msg-2" });
    loop.submit({ text: "msg-3" });
    expect(loop.queueDepth()).toBe(2);

    // Contract: merge ALL buffered TurnContents via combineAll (FIFO), clear
    // the buffer, emit QueueSignal(0), and return the combined content — in
    // one synchronous step. The in-flight turn is NOT disturbed: `running`
    // stays true (drainQueue only touches the buffer + emits the signal).
    const drained = loop.drainQueue();
    expect(drained).toEqual({ parts: [{ text: "msg-2" }, { text: "msg-3" }] });
    expect(loop.queueDepth()).toBe(0);
    expect(loop.isRunning()).toBe(true);
    // Signal sequence so far: submit⇒1, submit⇒2, mid-turn drain⇒0
    // (turn-loop-drain-contract emission table: "Mid-turn drainQueue clears
    // the buffer → QueueSignal(0)").
    expect(queueSignalDepths(frames)).toEqual([1, 2, 0]);

    // Tear down: release turn 1 so the loop can terminate.
    gate.resolve();
    await flush(20);
    expect(loop.isRunning()).toBe(false);
  });

  it("after a drainQueue call the turn-end drain sees an empty buffer (no double-drain)", async () => {
    const gate = makeGate();
    const calls: TurnContent[] = [];
    const adapter = makeEchoRunner("player", { gate, recordCalls: calls });
    const { emit, frames } = makeRecordingEmit();
    const loop = new TurnLoop(SID, TID, adapter, emit, AGENT);

    loop.submit({ text: "msg-1" });
    await flush();
    loop.submit({ text: "msg-2" });
    expect(loop.queueDepth()).toBe(1);

    // Mid-turn drain consumes the buffer.
    expect(loop.drainQueue()).toEqual({ parts: [{ text: "msg-2" }] });

    // Turn 1 completes: the runLoop turn-end buffer check
    // (if (this.buffer.length > 0)) sees 0,
    // so the loop goes idle — the drained msg-2 must NOT run again as a
    // second turn (exactly ONE runner call), and no second depth-0 signal is
    // emitted (idle emits no signal; depth is already 0).
    gate.resolve();
    await flush(20);

    expect(calls).toHaveLength(1);
    expect(extractText(calls[0])).toBe("msg-1");
    expect(textContents(frames)).toEqual(["reply:msg-1"]);
    expect(waitFrames(frames)).toHaveLength(1);
    expect(loop.queueDepth()).toBe(0);
    expect(loop.isRunning()).toBe(false);
    expect(queueSignalDepths(frames)).toEqual([1, 0]);
  });
});
