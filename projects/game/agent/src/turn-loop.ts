/**
 * turn-loop.ts — LangGraph-native queue + single-flight turn loop.
 *
 * Replaces the per-frame `acquireMutex → generateTurn → releaseMutex` path
 * (`projects/game/agent/src/handler.ts`). One instance is owned per session by
 * `SessionAgent`. Conversation continuity across auto-continued turns is
 * provided natively by the existing `MemorySaver` checkpointer: the loop
 * re-invokes `AgentAdapter.generateTurn` (= LangGraph `streamEvents(v3)`) on
 * the SAME `thread_id`, so each turn picks up where the last one left off
 * ([LangGraph — Add memory](https://docs.langchain.com/oss/javascript/langgraph/add-memory);
 * [Use functional API — multi-turn](https://docs.langchain.com/oss/javascript/langgraph/use-functional-api)).
 *
 * The "queue" is a transient FIFO of pending `TurnContent`s awaiting the next
 * `streamEvents` invocation; no custom persistence. An in-flight `streamEvents`
 * invocation does NOT absorb externally-buffered messages — LangGraph processes
 * one super-step from the checkpoint it loaded, so buffering outside the
 * running graph guarantees the in-flight turn is never disturbed (FR-002;
 * [LangGraph — time-travel / forking](https://docs.langchain.com/oss/javascript/langgraph/use-time-travel)).
 * `interrupt()` is intentionally NOT used: it pauses for REQUIRED input,
 * whereas queued input is OPTIONAL and must never pause the agent
 * (`specs/030-queued-chat-input/research.md` D2;
 * [LangGraph — interrupts](https://docs.langchain.com/oss/javascript/langgraph/interrupts)).
 *
 * Contract: `specs/030-queued-chat-input/contracts/turn-loop-contract.md`
 * Data model / state transitions: `specs/030-queued-chat-input/data-model.md`
 *
 * Phase 2 scope (T002): single-message drain path — on turn completion the
 * NEXT queued message becomes the next turn (one turn per drain). Phase 4
 * (T009) generalizes `combineAll` to merge ALL pending into one aggregated
 * `HumanMessage`. The `combineAll` abstraction is retained here so Phase 4
 * only swaps its body.
 */

import { randomUUID } from "node:crypto";

import { warn } from "@dominion/common-js-logs";

import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import type { ImagePart } from "../game_types/projects/game/ImagePart";
import type { ToolResultPart } from "../game_types/projects/game/ToolResultPart";
import type { AgentAdapter, ContentBlock, TurnContent } from "./llm";

/**
 * FrameSender enum string literals (proto). Defined locally (mirroring
 * `handler.ts`) so this module has no runtime dependency on the generated
 * game_types modules, which are not resolvable from the test runfiles tree;
 * all game_types references here are type-only.
 */
const FrameSender = {
  FRAME_SENDER_UNSPECIFIED: "FRAME_SENDER_UNSPECIFIED",
  FRAME_SENDER_USER: "FRAME_SENDER_USER",
  FRAME_SENDER_AGENT: "FRAME_SENDER_AGENT",
  FRAME_SENDER_SYSTEM: "FRAME_SENDER_SYSTEM",
} as const;

/**
 * Oneof case names for `AgentFrame.payload` (`projects/game/game.proto`).
 * proto-loader only populates the `payload` discriminator during
 * (de)serialization; outbound raw frame objects built here must carry it
 * explicitly so the frame is self-describing (same convention as `handler.ts`).
 */
const PAYLOAD_ONEOF_KEYS = ["messageParts", "flowParts"] as const;

/**
 * Sink the handler registered on the active bidi stream. The loop pushes
 * fully-formed `AgentFrame`s (display blocks, `wait`, `warn`, and — from
 * Phase 5 / T010 — `QueueSignal`) through it. It is injected (not module-
 * intercepted) so tests pass a plain recording array
 * (`style/javascript.md` §Mock — dependency injection over `vi.mock`;
 * [vitest — Mocking Modules Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)).
 */
export type TurnLoopEmit = (frame: AgentFrame) => void;

/**
 * Lazy resolver of the bound `AgentAdapter` for a turn. Reuses
 * `SessionAgent.getOrCreateAdapter` so the cached adapter is served and the
 * existing `MemorySaver` checkpointer is forwarded unchanged.
 */
export type AdapterProvider = () => Promise<AgentAdapter>;

/**
 * Build an outbound `AgentFrame` envelope, tagging the `payload` oneof case
 * from whichever payload key is present (same convention as `handler.ts`).
 */
function buildFrame(
  sessionId: string,
  sender: (typeof FrameSender)[keyof typeof FrameSender],
  payload: Partial<AgentFrame>,
): AgentFrame {
  const payloadKind = PAYLOAD_ONEOF_KEYS.find((k) => k in payload);
  return {
    sessionId,
    frameId: randomUUID(),
    sender,
    createTime: timestampNow(),
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
 * Drain the FIFO buffer into the next turn's input.
 *
 * Phase 2 (T002): single-message path — shift the FIRST queued message
 * (FIFO) and run it as the next turn (one turn per drain). The buffer
 * shrinks by one; the loop repeats until empty.
 *
 * Phase 4 (T009) will generalize this to merge ALL pending messages into ONE
 * aggregated `HumanMessage` (multi content blocks, FIFO) and run it as a
 * single next turn (`specs/030-queued-chat-input/research.md` D3; the
 * `llm.ts` `TurnContent` generalization is T008). The function boundary is
 * retained so Phase 4 only swaps this body.
 */
function combineAll(buffer: TurnContent[]): TurnContent {
  const next = buffer.shift();
  // Callers guard on `buffer.length > 0` before invoking; this is defensive.
  if (!next) {
    throw new Error("combineAll: buffer unexpectedly empty");
  }
  return next;
}

/**
 * The LangGraph-native queue + single-flight loop.
 *
 * State machine (`specs/030-queued-chat-input/data-model.md`):
 * - IDLE → submit → RUNNING (start loop).
 * - RUNNING → submit → RUNNING (buffer.push; FIFO).
 * - RUNNING → turn done, buffer non-empty → RUNNING (drain one; next turn,
 *   same thread_id).
 * - RUNNING → turn done, buffer empty → IDLE (emit `wait`).
 * - RUNNING → abort → IDLE (clear buffer FR-011; emit `wait`).
 * - RUNNING → non-abort error → IDLE (emit `warn`; RETAIN buffer FR-015;
 *   emit `wait`).
 *
 * Guarantees (mapped to spec FRs):
 * - FR-002: `submit` while RUNNING only touches the buffer; the in-flight
 *   `generateTurn` is never disturbed.
 * - FR-004: FIFO buffer.
 * - FR-006: `wait` emitted iff buffer empty at a turn boundary; never an
 *   empty turn.
 * - FR-011: `abort` clears the buffer + emits `wait`.
 * - FR-015: a non-abort turn error retains the buffer.
 */
export class TurnLoop {
  private readonly sessionId: string;
  private readonly adapterProvider: AdapterProvider;
  private readonly emit: TurnLoopEmit;
  private readonly profileName: string;

  private buffer: TurnContent[] = [];
  private running = false;
  private aborting = false;
  private controller: AbortController | null = null;

  constructor(
    sessionId: string,
    adapterProvider: AdapterProvider,
    emit: TurnLoopEmit,
    profileName: string,
  ) {
    this.sessionId = sessionId;
    this.adapterProvider = adapterProvider;
    this.emit = emit;
    this.profileName = profileName;
  }

  /**
   * Non-blocking. IDLE ⇒ start the loop with `content` (IDLE→RUNNING).
   * RUNNING ⇒ append `content` to the FIFO buffer (FR-002: never disturbs the
   * in-flight turn). Returns immediately in both cases.
   *
   * `QueueSignal` emission on submit (depth +1) is added in Phase 5 (T010);
   * it is intentionally omitted here because the proto/emit wiring lands with
   * T010 and is out of Phase 2 scope.
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
   * (FR-011). Transitions RUNNING→IDLE and emits `wait` so the desktop
   * returns to ready. No-op if IDLE.
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
   * The RUNNING-state loop body. Drives `generateTurn`, emits display frames,
   * and on turn completion either drains the next queued message (next turn,
   * same `thread_id` → checkpointer continues) or emits `wait` (idle).
   *
   * Three terminal paths, each setting `running=false`:
   * - `finishAbort`: buffer cleared (FR-011), `wait` emitted.
   * - `finishError`: buffer RETAINED (FR-015), `warn` then `wait` emitted.
   * - `finishIdle`: `wait` emitted (buffer empty at boundary, FR-006).
   */
  private async runLoop(initialContent: TurnContent): Promise<void> {
    let current = initialContent;
    // Top-of-loop abort check covers the drain gap (between turn completion
    // and the next `generateTurn` start) where `controller.abort()` would
    // target an already-completed controller.
    while (true) {
      if (this.aborting) {
        this.finishAbort();
        return;
      }
      this.controller = new AbortController();
      try {
        const adapter = await this.adapterProvider();
        for await (const block of adapter.generateTurn(
          this.sessionId,
          current,
          this.controller.signal,
        )) {
          this.emit(this.displayFrame(block));
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
      // gap before this checkpoint. Treat as abort (FR-011).
      if (this.aborting) {
        this.finishAbort();
        return;
      }

      // Drain (Phase 2: one message per drain via combineAll shift).
      if (this.buffer.length > 0) {
        current = combineAll(this.buffer);
        continue;
      }

      // Idle: buffer empty at the turn boundary (FR-006).
      this.finishIdle();
      return;
    }
  }

  /** Abort terminal: clear buffer (FR-011), emit `wait`, → IDLE. */
  private finishAbort(): void {
    this.buffer = [];
    this.aborting = false;
    this.running = false;
    this.controller = null;
    this.emit(this.waitFrame());
  }

  /** Non-abort error terminal: emit `warn`, RETAIN buffer (FR-015), emit `wait`, → IDLE. */
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
  // owns all outbound AgentFrame emission; the handler's emit sink just writes)
  // -----------------------------------------------------------------------

  /**
   * Map a streamed `ContentBlock` to a display `AgentFrame` (messageParts).
   * The block→MessagePart framing matches `handler.ts` exactly so live and
   * loop-emitted output are identical.
   */
  private displayFrame(block: ContentBlock): AgentFrame {
    if (block.type === "reasoning") {
      return buildFrame(this.sessionId, FrameSender.FRAME_SENDER_AGENT, {
        agentProfileName: this.profileName,
        messageParts: {
          parts: [{ thinking: { content: block.reasoning } }],
        },
      });
    }
    if (block.type === "text") {
      return buildFrame(this.sessionId, FrameSender.FRAME_SENDER_AGENT, {
        agentProfileName: this.profileName,
        messageParts: {
          parts: [{ text: { content: block.text } }],
        },
      });
    }
    if (block.type === "tool_call") {
      return buildFrame(this.sessionId, FrameSender.FRAME_SENDER_AGENT, {
        agentProfileName: this.profileName,
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
    return buildFrame(this.sessionId, FrameSender.FRAME_SENDER_AGENT, {
      agentProfileName: this.profileName,
      messageParts: { parts: [{ toolResult: toolResultPart }] },
    });
  }

  /** `wait` FlowPart frame (sender SYSTEM, carries profileName). */
  private waitFrame(): AgentFrame {
    return buildFrame(this.sessionId, FrameSender.FRAME_SENDER_SYSTEM, {
      agentProfileName: this.profileName,
      flowParts: { parts: [{ wait: {} }] },
    });
  }

  /** `warn` FlowPart frame (sender SYSTEM). */
  private warnFrame(message: string): AgentFrame {
    return buildFrame(this.sessionId, FrameSender.FRAME_SENDER_SYSTEM, {
      flowParts: {
        parts: [{ warn: { message: `Processing error: ${message}` } }],
      },
    });
  }
}
