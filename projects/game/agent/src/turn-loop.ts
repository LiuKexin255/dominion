/**
 * turn-loop.ts — queue + single-flight turn loop (spec 030 semantics).
 *
 * The loop owns the single-flight + FIFO-queue semantics of spec 030
 * (specs/030-queued-chat-input/contracts/turn-loop-contract.md) for the TEAM
 * architecture (specs/031-team-template-mode/research.md D10): one user input
 * → one team turn = one graph invoke, driven through an injected
 * {@link TurnRunner} instead of the former single-agent `AgentAdapter`
 * (`specs/031-team-template-mode/tasks.md` T017 — the runner decouples the
 * loop from the adapter path that Phase 5 removed). The team graph's
 * `gameEnded` is handled INSIDE a turn by the conditional edge, so a turn
 * never requires external continuation and single-flight is preserved.
 *
 * One instance is owned per session by `SessionTeam`. Conversation continuity
 * across auto-continued turns is provided by the team graph's outer
 * `MemorySaver` checkpointer: the runner re-invokes `streamEvents` on the
 * SAME `thread_id` (= session id, FR-013), so each turn picks up where the
 * last one left off
 * ([LangGraph — Add memory](https://docs.langchain.com/oss/javascript/langgraph/add-memory)).
 *
 * The "queue" is a transient FIFO of pending `TurnContent`s awaiting the next
 * turn invocation; no custom persistence. An in-flight invocation does NOT
 * absorb externally-buffered messages — the graph processes one super-step
 * from the checkpoint it loaded, so buffering outside the running graph
 * guarantees the in-flight turn is never disturbed
 * (specs/030-queued-chat-input/spec.md FR-002;
 * [LangGraph — time-travel / forking](https://docs.langchain.com/oss/javascript/langgraph/use-time-travel)).
 * `interrupt()` is intentionally NOT used: it pauses for REQUIRED input,
 * whereas queued input is OPTIONAL and must never pause the agent
 * (specs/030-queued-chat-input/research.md D2;
 * [LangGraph — interrupts](https://docs.langchain.com/oss/javascript/langgraph/interrupts)).
 *
 * Data model / state transitions: specs/030-queued-chat-input/data-model.md
 *
 * Drain path: on turn completion, ALL pending queued messages are merged into
 * ONE aggregated `HumanMessage` (multi content blocks, FIFO) and run as a
 * single next turn on the same `thread_id` (specs/030-queued-chat-input/research.md
 * D3; specs/030-queued-chat-input/spec.md FR-004/FR-005). `combineAll` performs the merge + buffer clear.
 *
 * `QueueSignal` emission: the loop pushes a `QueueSignal` FlowPart over the
 * flow channel on every per-session queue-depth change (submit⇒+1/new depth;
 * drain-to-next-turn⇒0; abort⇒0), per
 * `specs/030-queued-chat-input/contracts/queue-channel-contract.md` §2. The
 * idle terminal emits no extra signal (depth is already 0 there), and a
 * non-abort error retains the buffer so its depth is unchanged (no signal).
 *
 * Frames carry the team `agent` field (specs/031-team-template-mode D12):
 * display blocks carry the producing agent's name; control signals
 * (`wait`/`warn`/`QueueSignal`) carry the session's primary agent
 * (`agentName`, the accepts-user-input agent — "player" for saolei).
 */

import { randomUUID } from "node:crypto";

import { warn } from "@dominion/common-js-logs";

import type { TeamFrame } from "../game_types/projects/game/TeamFrame";
import type { ImagePart } from "../game_types/projects/game/ImagePart";
import type { ToolResultPart } from "../game_types/projects/game/ToolResultPart";
import type {
  ContentBlock,
  TurnContent,
  TurnContentPart,
} from "./llm";
import { toParts } from "./llm";

/**
 * Oneof case names for `TeamFrame.payload` (`projects/game/game.proto`).
 * proto-loader only populates the `payload` discriminator during
 * (de)serialization; outbound raw frame objects built here must carry it
 * explicitly so the frame is self-describing (same convention as `handler.ts`).
 */
const PAYLOAD_ONEOF_KEYS = ["messageParts", "flowParts"] as const;

/**
 * Sink the handler registered on the active bidi stream. The loop pushes
 * fully-formed `TeamFrame`s (display blocks, `wait`, `warn`, and — from
 * Phase 5 / T010 — `QueueSignal`) through it. It is injected (not module-
 * intercepted) so tests pass a plain recording array
 * (`style/javascript.md` §Mock — dependency injection over `vi.mock`;
 * [vitest — Mocking Modules Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)).
 */
export type TurnLoopEmit = (frame: TeamFrame) => void;

/**
 * One streamed team-turn output: a `ContentBlock` plus the team agent that
 * produced it (the frame's `agent` value, specs/031-team-template-mode D12).
 * The team graph's nodes stream their channel updates per node, so the
 * runner attributes each block to `player`/`planner` (FR-023).
 */
export interface TurnBlock {
  agent: string;
  block: ContentBlock;
}

/**
 * Lazy resolver of a team turn's stream. Replaces the former
 * `AdapterProvider` (`AgentAdapter.generateTurn`): the team architecture
 * decouples the loop from the adapter (specs/031-team-template-mode/research.md
 * D10 — `SessionTeam` provides the team-graph-invoke runner).
 */
export type TurnRunner = (
  content: TurnContent,
  signal?: AbortSignal,
) => AsyncIterable<TurnBlock>;

/**
 * Build an outbound `TeamFrame` envelope, tagging the `payload` oneof case
 * from whichever payload key is present and deriving `role` from it: a
 * messageParts payload is real-time agent display content → AGENT; a flowParts
 * payload is a control signal/operation → UNSPECIFIED (FR-020,
 * specs/035-proto-contract-refine/contracts/frame-split.md §3.2). The envelope
 * always sets session_id/template_id/frame_id/create_time (FR-013,
 * specs/035-proto-contract-refine/contracts/frame-split.md §3.3). `templateId`
 * is the session's template path segment (bare, REQUIRED by the proto contract
 * — specs/031-team-template-mode/contracts/api-contract.md §3.6).
 *
 * `frameId` is an explicit override: the channel-frame emitter passes the
 * source message's id so the live frame and the reloaded ListMessages entry
 * share one dedup anchor (specs/037-saolei-team-optimize/data-model.md §4,
 * research.md D9 — desktop `renderedMessageIds` dedups on `frameId == msg.id`).
 * Defaults to a fresh randomUUID when omitted (the pre-existing behavior).
 *
 * Exported as the canonical TeamFrame builder: `handler.ts` and
 * `operation-bridge.ts` reuse it so every outbound frame carries the full
 * envelope (the former operation-bridge dispatch set only the payload — the
 * FR-013 defect this fixes).
 */
export function buildTeamFrame(
  sessionId: string,
  templateId: string,
  payload: Partial<TeamFrame>,
  frameId?: string,
): TeamFrame {
  const payloadKind = PAYLOAD_ONEOF_KEYS.find((k) => k in payload);
  return {
    sessionId,
    templateId,
    frameId: frameId ?? randomUUID(),
    createTime: timestampNow(),
    role:
      payloadKind === "messageParts"
        ? "MESSAGE_ROLE_AGENT"
        : "MESSAGE_ROLE_UNSPECIFIED",
    ...(payloadKind ? { payload: payloadKind } : {}),
    ...payload,
  };
}

function timestampNow(): { seconds: number; nanos: number } {
  const ms = Date.now();
  return {
    seconds: Math.floor(ms / 1000),
    nanos: (ms % 1000) * 1_000_000,
  };
}

/**
 * Drain the FIFO buffer into the next turn's aggregated input.
 *
 * Merges ALL pending `TurnContent`s into ONE aggregated input — a single
 * `HumanMessage` with multiple content blocks, in FIFO submission order
 * (`specs/030-queued-chat-input/research.md` D3; specs/030-queued-chat-input/spec.md FR-004/FR-005). Each
 * buffered message is normalized to parts via `toParts` and concatenated, so
 * an aggregated turn carries every queued message's text and screenshots
 * without loss. The buffer is cleared on drain
 * (`specs/030-queued-chat-input/contracts/turn-loop-contract.md` loop body;
 * `specs/030-queued-chat-input/data-model.md`).
 *
 * The caller (`runLoop`) emits `QueueSignal(0)` immediately after this clear,
 * per specs/030-queued-chat-input/contracts/queue-channel-contract.md §2.
 */
function combineAll(buffer: TurnContent[]): TurnContent {
  if (buffer.length === 0) {
    // Callers guard on `buffer.length > 0` before invoking; this is defensive.
    throw new Error("combineAll: buffer unexpectedly empty");
  }
  const parts: TurnContentPart[] = [];
  for (const content of buffer) {
    parts.push(...toParts(content));
  }
  buffer.length = 0;
  return { parts };
}

/**
 * The LangGraph-native queue + single-flight loop.
 *
 * State machine (`specs/030-queued-chat-input/data-model.md`):
 * - IDLE → submit → RUNNING (start loop).
 * - RUNNING → submit → RUNNING (buffer.push; FIFO).
 * - RUNNING → turn done, buffer non-empty → RUNNING (merge ALL pending into
 *   one aggregated HumanMessage via combineAll; next turn, same thread_id).
 * - RUNNING → turn done, buffer empty → IDLE (emit `wait`).
 * - RUNNING → abort → IDLE (clear buffer; specs/030-queued-chat-input/spec.md
 *   FR-011; emit `wait`).
 * - RUNNING → non-abort error → IDLE (emit `warn`; RETAIN buffer;
 *   specs/030-queued-chat-input/spec.md FR-015; emit `wait`).
 *
 * Guarantees (mapped to specs/030-queued-chat-input/spec.md FRs):
 * - specs/030-queued-chat-input/spec.md FR-002: `submit` while RUNNING only
 *   touches the buffer; the in-flight `generateTurn` is never disturbed.
 * - specs/030-queued-chat-input/spec.md FR-004: FIFO buffer.
 * - specs/030-queued-chat-input/spec.md FR-006: `wait` emitted iff buffer
 *   empty at a turn boundary; never an empty turn.
 * - specs/030-queued-chat-input/spec.md FR-011: `abort` clears the buffer +
 *   emits `wait`.
 * - specs/030-queued-chat-input/spec.md FR-015: a non-abort turn error
 *   retains the buffer.
 */
export class TurnLoop {
  private readonly sessionId: string;
  /** The session's template id (bare path segment, e.g. "saolei") — stamped
   * on every outbound frame alongside sessionId (REQUIRED, api-contract.md §3.6). */
  private readonly templateId: string;
  private readonly runner: TurnRunner;
  private readonly emit: TurnLoopEmit;
  /** The session's primary agent — stamped on control frames (`agent`). */
  private readonly agentName: string;

  private buffer: TurnContent[] = [];
  private running = false;
  private aborting = false;
  private controller: AbortController | null = null;

  constructor(
    sessionId: string,
    templateId: string,
    runner: TurnRunner,
    emit: TurnLoopEmit,
    agentName: string,
  ) {
    this.sessionId = sessionId;
    this.templateId = templateId;
    this.runner = runner;
    this.emit = emit;
    this.agentName = agentName;
  }

  /**
   * Non-blocking. IDLE ⇒ start the loop with `content` (IDLE→RUNNING).
   * RUNNING ⇒ append `content` to the FIFO buffer and push a `QueueSignal`
   * carrying the new buffer depth
   * (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2:
   * "submit while a turn is RUNNING (buffer grows) → QueueSignal(new depth)").
   * Never disturbs the in-flight turn
   * (specs/030-queued-chat-input/spec.md FR-002). Returns immediately in both
   * cases.
   *
   * The IDLE branch emits NO QueueSignal: depth stays 0 there (the message
   * starts a turn rather than being buffered), so per the contract no
   * depth-change signal is required.
   */
  submit(content: TurnContent): void {
    if (!this.running) {
      this.running = true;
      // Fire-and-forget: the loop owns its own lifecycle and swallows turn
      // errors internally (see runLoop catch). Never awaited by the caller.
      void this.runLoop(content);
      return;
    }
    this.buffer.push(content);
    this.emit(this.queueSignalFrame(this.buffer.length));
  }

  /** True iff a turn is in flight or the loop is draining queued work. */
  isRunning(): boolean {
    return this.running;
  }

  /** Current `buffer.length` (used by tests; not required by callers). */
  queueDepth(): number {
    return this.buffer.length;
  }

  /**
   * Abort the in-flight turn (`controller.abort()`) AND clear the buffer
   * (specs/030-queued-chat-input/spec.md FR-011). Transitions RUNNING→IDLE
   * and emits `wait` so the desktop returns to ready. No-op if IDLE.
   *
   * The actual buffer clear + `wait` emission is performed by the loop's
   * abort terminal (`finishAbort`) so there is a single emission owner and no
   * double-`wait` race between this method and the async loop. `aborting`
   * flags the intent; the loop observes it at its next checkpoint (catch,
   * post-turn, or top-of-loop) and finalizes.
   */
  abort(): void {
    if (!this.running) {
      return;
    }
    this.aborting = true;
    this.controller?.abort();
  }

  /**
   * The RUNNING-state loop body. Drives the injected {@link TurnRunner}
   * (one team graph invoke per turn), emits display frames, and on turn
   * completion either drains the next queued message (next turn, same
   * `thread_id` → checkpointer continues) or emits `wait` (idle).
   *
   * Three terminal paths, each setting `running=false`:
   * - `finishAbort`: buffer cleared (specs/030-queued-chat-input/spec.md
   *   FR-011), `wait` emitted.
   * - `finishError`: buffer RETAINED (specs/030-queued-chat-input/spec.md
   *   FR-015), `warn` then `wait` emitted.
   * - `finishIdle`: `wait` emitted (buffer empty at boundary,
   *   specs/030-queued-chat-input/spec.md FR-006).
   */
  private async runLoop(initialContent: TurnContent): Promise<void> {
    let current = initialContent;
    // Top-of-loop abort check covers the drain gap (between turn completion
    // and the next turn start) where `controller.abort()` would target an
    // already-completed controller.
    while (true) {
      if (this.aborting) {
        this.finishAbort();
        return;
      }
      this.controller = new AbortController();
      try {
        for await (const { agent, block } of this.runner(
          current,
          this.controller.signal,
        )) {
          this.emit(this.displayFrame(block, agent));
        }
      } catch (err: unknown) {
        if (this.controller.signal.aborted || this.aborting) {
          this.finishAbort();
        } else {
          this.finishError(err);
        }
        return;
      }
      this.controller = null;

      // Abort race: the turn completed normally but `abort()` arrived in the
      // gap before this checkpoint. Treat as abort
      // (specs/030-queued-chat-input/spec.md FR-011).
      if (this.aborting) {
        this.finishAbort();
        return;
      }

      // Drain: merge ALL pending into ONE aggregated HumanMessage (multi
      // content blocks, FIFO) and run it as the next turn on the same
      // thread_id (specs/030-queued-chat-input/contracts/turn-loop-contract.md
      // loop body; specs/030-queued-chat-input/research.md D3).
      // combineAll clears the buffer on drain; the contract
      // (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2:
      // "Turn completes and buffer drained into the next turn → QueueSignal(0)")
      // requires a depth-0 signal right after the clear, before the next turn
      // starts, so the desktop transitions the pending messages to normal
      // (specs/030-queued-chat-input/spec.md FR-009) and stays `processing`
      // across the auto-continued turn boundary (no `wait` here).
      if (this.buffer.length > 0) {
        current = combineAll(this.buffer);
        this.emit(this.queueSignalFrame(0));
        continue;
      }

      // Idle: buffer empty at the turn boundary
      // (specs/030-queued-chat-input/spec.md FR-006).
      this.finishIdle();
      return;
    }
  }

  /**
   * Abort terminal: clear buffer (specs/030-queued-chat-input/spec.md FR-011),
   * emit `QueueSignal(0)` then `wait`, → IDLE.
   *
   * The depth-0 signal precedes `wait` per
   * specs/030-queued-chat-input/contracts/queue-channel-contract.md §2
   * ("Abort clears the buffer → QueueSignal(0) then wait"), so the desktop
   * drops its pending indicator before the idle `wait` returns it to ready.
   */
  private finishAbort(): void {
    this.buffer = [];
    this.aborting = false;
    this.running = false;
    this.controller = null;
    this.emit(this.queueSignalFrame(0));
    this.emit(this.waitFrame());
  }

  /** Non-abort error terminal: emit `warn`, RETAIN buffer (specs/030-queued-chat-input/spec.md FR-015), emit `wait`, → IDLE. */
  private finishError(err: unknown): void {
    const message = err instanceof Error ? err.message : "Processing error";
    warn("turn loop: turn error, retaining queued buffer", {
      sessionId: this.sessionId,
      error: message,
      retainedDepth: this.buffer.length,
    });
    this.running = false;
    this.controller = null;
    this.emit(this.warnFrame(message));
    this.emit(this.waitFrame());
  }

  /** Idle terminal: buffer empty at a turn boundary; emit `wait`, → IDLE. */
  private finishIdle(): void {
    this.running = false;
    this.emit(this.waitFrame());
  }

  // -----------------------------------------------------------------------
  // Frame builders (unchanged framing — ported from handler.ts so the loop
  // owns all outbound TeamFrame emission; the handler's emit sink just writes)
  // -----------------------------------------------------------------------

  /**
   * Map a streamed `ContentBlock` to a display `TeamFrame` (messageParts).
   * The block→MessagePart framing matches `handler.ts` exactly so live and
   * loop-emitted output are identical. The frame's `agent` field carries the
   * producing team agent's name (specs/031-team-template-mode D12); `role` is
   * AGENT (messageParts payload).
   */
  private displayFrame(block: ContentBlock, agent: string): TeamFrame {
    if (block.type === "reasoning") {
      return buildTeamFrame(this.sessionId, this.templateId, {
        agent,
        messageParts: {
          parts: [{ thinking: { content: block.reasoning } }],
        },
      });
    }
    if (block.type === "text") {
      return buildTeamFrame(this.sessionId, this.templateId, {
        agent,
        messageParts: {
          parts: [{ text: { content: block.text } }],
        },
      });
    }
    if (block.type === "tool_call") {
      return buildTeamFrame(this.sessionId, this.templateId, {
        agent,
        messageParts: {
          parts: [
            {
              toolCall: {
                toolId: block.toolCallId,
                name: block.name,
                argsJson: JSON.stringify(block.args ?? {}),
              },
            },
          ],
        },
      });
    }
    // tool_result
    const toolResultPart: ToolResultPart = {
      toolId: block.toolCallId,
      status: block.status as ToolResultPart["status"],
      message: block.message,
    };
    if (block.screenshot) {
      const screenshot: ImagePart = {
        encoding: "IMAGE_ENCODING_PNG",
        data: block.screenshot.data,
        widthPx: block.screenshot.widthPx,
        heightPx: block.screenshot.heightPx,
      };
      toolResultPart.screenshot = screenshot;
    }
    return buildTeamFrame(this.sessionId, this.templateId, {
      agent,
      messageParts: { parts: [{ toolResult: toolResultPart }] },
    });
  }

  /** `wait` FlowPart frame (control signal, carries the session agent). */
  private waitFrame(): TeamFrame {
    return buildTeamFrame(this.sessionId, this.templateId, {
      agent: this.agentName,
      flowParts: { parts: [{ wait: {} }] },
    });
  }

  /**
   * `QueueSignal` FlowPart frame carrying `depth` as `queued_count`
   * (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2). The
   * proto field `queued_count` (lower_snake_case per
   * [AIP-140 Field names](https://google.aip.dev/140)) is emitted on the JS
   * wire as `queuedCount` (proto-loader camelCase mapping — see the generated
   * `FlowPart.queue`/`QueueSignal.queuedCount` in game_types). Control-only
   * signals match `wait`/`warn`.
   */
  private queueSignalFrame(depth: number): TeamFrame {
    return buildTeamFrame(this.sessionId, this.templateId, {
      flowParts: { parts: [{ queue: { queuedCount: depth } }] },
    });
  }

  /** `warn` FlowPart frame (control signal). */
  private warnFrame(message: string): TeamFrame {
    return buildTeamFrame(this.sessionId, this.templateId, {
      flowParts: {
        parts: [{ warn: { message: `Processing error: ${message}` } }],
      },
    });
  }
}
