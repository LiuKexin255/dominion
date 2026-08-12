/**
 * operation-bridge.ts — Session-scoped bridge between LangChain tools and the
 * desktop WebSocket bidi stream.
 *
 * OperationBridge is owned by SessionAgent and survives stream reconnects.
 * The Connect handler registers a sink callback on stream open and unregisters
 * it on stream end/error.  LangChain tools (e.g. mouse) call dispatch() to
 * send a FlowPart operation to the desktop and await the matching FlowResultPart.
 *
 * The bridge never holds a reference to a specific stream instance — only a
 * write callback — so a new stream can re-register its sink without recreating
 * the bridge or losing track of in-flight dispatches.
 *
 * Content-model contract (specs/023-saolei-mcp-refine/contracts/content-model-contract.md)
 * + decoupling (research.md D10 / contracts/tool-dispatch-contract.md §1): a
 * tool request is a FlowPart (a mouse/keyboard operation) wrapped in a
 * FlowParts TeamFrame (payload "flowParts"). The desktop replies with a
 * flowParts UserFrame whose FlowResultPart.tool_id matches the bridge-minted
 * operation-channel id (spec 025 FR-023/FR-025 — the operation result travels
 * on the control channel, not as a display tool_result MessagePart). The
 * conversation channel (tool_call/tool_result MessageParts) is fully DECOUPLED
 * — it groups by the LangChain `tool_call.id` independently; the operation
 * channel uses a bridge-minted id that has no relation to any conversation
 * tool_call.id (revised FR-008 / research.md D10).
 *
 * Envelope contract (FR-013): dispatch builds the outbound TeamFrame via
 * `buildTeamFrame` with the session's session_id/template_id, so the
 * operation frame carries the full envelope (session_id/template_id/frame_id/
 * create_time) like every other TeamFrame — the former dispatch that set only
 * the payload was the FR-013 defect
 * (specs/035-proto-contract-refine/contracts/frame-split.md §3.3).
 */

import { randomUUID } from "node:crypto";

import { info, warn } from "@dominion/common-js-logs";

import type { TeamFrame } from "../game_types/projects/game/TeamFrame";
import type { FlowPart } from "../game_types/projects/game/FlowPart";
import type { FlowResultPart } from "../game_types/projects/game/FlowResultPart";
import type { ImagePart } from "../game_types/projects/game/ImagePart";
import type { MouseMovePart } from "../game_types/projects/game/MouseMovePart";
import type { MouseClickPart } from "../game_types/projects/game/MouseClickPart";
import type { KeyboardPressPart } from "../game_types/projects/game/KeyboardPressPart";
import type { MouseMoveAndClickPart } from "../game_types/projects/game/MouseMoveAndClickPart";
import type { ToolResultStatus } from "../game_types/projects/game/ToolResultStatus";

import { buildTeamFrame } from "./turn-loop";
import { TOOL_HEARTBEAT_INTERVAL_MS } from "./llm";

// Maximum wait time (ms) for a tool result before timing out. Raised from 5 s
// to 20 min as a safety-net backstop: the desktop's 15-min auto-continue
// (specs/022-desktop-debug-mode spec.md FR-013) always fires first under debug
// usage, so under normal operation this longer timeout stays dormant
// (specs/022-desktop-debug-mode spec.md FR-014, research.md D6).
const DISPATCH_TIMEOUT_MS = 1_200_000;

/** String literal values for ToolResultStatus (proto enum). */
const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "TOOL_RESULT_STATUS_FAILED";
const STATUS_UNSPECIFIED = "TOOL_RESULT_STATUS_UNSPECIFIED";

/**
 * Sink callback registered by the Connect handler.  Receives a full TeamFrame
 * whose payload is "flowParts". The handler writes it to the stream as-is.
 */
export type OperationSink = (frame: TeamFrame) => void;

/**
 * Opaque identity of a registered sink, returned by registerSink and passed
 * to unregisterSink for compare-and-delete ownership: a stale close from a
 * superseded stream must not clobber a fresh registration
 * (specs/021-agent-session-resync/research.md D3;
 * specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §1).
 * The handle IS the sink reference, so `===` compares identity without extra
 * token bookkeeping.
 */
export type SinkHandle = OperationSink;

/**
 * Screenshot captured by the desktop alongside a tool result.
 * `data` is a base64-encoded PNG string (raw bytes from the wire are encoded
 * by handleResult before reaching consumers).
 */
export interface OperationScreenshot {
  data: string;
  widthPx: number;
  heightPx: number;
}

/** Outcome of a dispatch — mirrors the relevant fields of FlowResultPart. */
export interface OperationResult {
  status: ToolResultStatus;
  message: string;
  screenshot?: OperationScreenshot;
}

interface PendingDispatch {
  resolve: (result: OperationResult) => void;
  timer: ReturnType<typeof setTimeout>;
  cleanup?: () => void;
}

/**
 * Convert an ImagePart screenshot into the resolved OperationScreenshot shape.
 * Raw bytes (`Uint8Array`/`Buffer`) arriving over the bidi stream are
 * base64-encoded here; an already-string `data` (e.g. protojson) is passed
 * through unchanged.  Missing numeric dimensions coalesce to 0 so the
 * resolved shape stays non-optional per the interface contract.
 */
function toOperationScreenshot(source: ImagePart): OperationScreenshot {
  const { data, widthPx, heightPx } = source;
  const encoded =
    typeof data === "string" ? data : Buffer.from(data ?? "").toString("base64");
  return {
    data: encoded,
    widthPx: widthPx ?? 0,
    heightPx: heightPx ?? 0,
  };
}

export class OperationBridge {
  private readonly sessionId: string;
  private readonly templateId: string;
  private sink: OperationSink | null = null;
  private readonly pending = new Map<string, PendingDispatch>();

  /**
   * @param sessionId The session id (bare segment) stamped on dispatched
   *   TeamFrame envelopes (FR-013).
   * @param templateId The session's template id (bare segment) stamped on
   *   dispatched TeamFrame envelopes (FR-013).
   */
  constructor(sessionId = "", templateId = "") {
    this.sessionId = sessionId;
    this.templateId = templateId;
  }

  /**
   * Register the stream write callback.  Called by the Connect handler when a
   * new bidi stream opens.  Replaces any previously registered sink and
   * returns a handle identifying this installation; the caller stores the
   * handle per session and passes it to unregisterSink so a stale close from
   * a superseded stream is a no-op (compare-and-delete).
   */
  registerSink(writeFn: OperationSink): SinkHandle {
    this.sink = writeFn;
    info("operation bridge sink registered");
    return writeFn;
  }

  /**
   * Clear the registered sink, but only when `handle` identifies the
   * currently-registered sink (compare-and-delete).  A stale close from a
   * stream whose sink was already superseded is a no-op, so it cannot clobber
   * a fresh registration (specs/021-agent-session-resync/research.md D3;
   * specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §1).
   * Omitting `handle` is likewise a no-op: `this.sink` is never undefined
   * (null or a function), so it never equals undefined.  In-flight dispatches
   * on a closing stream still resolve FAILED "aborted" via the per-turn
   * AbortController; the 20-min timeout remains the fallback for dispatches
   * without a signal.
   */
  unregisterSink(handle?: SinkHandle): void {
    if (this.sink === handle) {
      this.sink = null;
      info("operation bridge sink unregistered", {
        pendingCount: this.pending.size,
      });
    }
  }

  /**
   * Dispatch a FlowPart operation (MouseMovePart, MouseClickPart,
   * KeyboardPressPart, or MouseMoveAndClickPart) to the desktop via the
   * registered sink and await the matching FlowResultPart.
   *
   * The bridge ALWAYS mints a fresh UUID as the operation-channel id and stamps
   * it onto the FlowPart's `tool_id`; this id is for operation-channel
   * correlation only (dispatch↔`handleResult` via the pending map). It is NOT
   * related to the conversation-channel `tool_call.id` — the two channels are
   * decoupled (research.md D10 / contracts/tool-dispatch-contract.md §1: the
   * conversation groups tool_call↔tool_result by the LangChain `tool_call.id`
   * independently; the operation channel uses the bridge-minted id). The tool
   * caller therefore passes NO toolId to dispatch.
   *
   * The operation is wrapped in a FlowParts TeamFrame (built via
   * `buildTeamFrame` with the session's session_id/template_id — full
   * envelope per FR-013) and written via the sink. When no sink is registered
   * (desktop disconnected), resolves FAILED immediately rather than throwing.
   * When the optional `signal` is already aborted, or aborts while the
   * dispatch is pending, resolves FAILED "aborted" immediately — abort is a
   * third race participant alongside the 20-min timeout and handleResult,
   * whichever fires first wins.
   *
   * While the dispatch is pending, the optional `heartbeat` (the saolei tools
   * pass LangGraph's `config.heartbeat`, installed by `wrapConfig`) is called
   * immediately and then every `TOOL_HEARTBEAT_INTERVAL_MS`. LangGraph's
   * `idleTimeout` watchdog refreshes `lastProgress` unconditionally on
   * heartbeat, so a long tool wait (up to DISPATCH_TIMEOUT_MS) never trips a
   * false `NodeTimeoutError` (specs/043-llm-stream-stall-recovery/
   * research.md R7, contracts/stall-recovery-contract.md §1.2). The interval
   * is cleared on resolve/abort/timeout/sink-error — no leaked timers.
   *
   * @param part      - FlowPart operation to dispatch.
   * @param signal    - Optional AbortSignal; when aborted, resolves FAILED.
   * @param heartbeat - Optional idle-timer refresher (see above); when
   *                    omitted (non-saolei tools, tests) the dispatch
   *                    degrades to today's behavior.
   * @returns SUCCEEDED/FAILED status from the desktop, or FAILED on timeout,
   *          abort, non-operation FlowPart, no-sink, or sink write error.
   */
  async dispatch(
    part: FlowPart,
    signal?: AbortSignal,
    heartbeat?: () => void,
  ): Promise<OperationResult> {
    const toolPart:
      | MouseMovePart
      | MouseClickPart
      | KeyboardPressPart
      | MouseMoveAndClickPart
      | undefined =
      part.mouseMove ??
      part.mouseClick ??
      part.keyboardPress ??
      part.mouseMoveAndClick ??
      undefined;
    if (!toolPart) {
      warn("dispatch received a non-operation FlowPart");
      return { status: STATUS_FAILED, message: "invalid tool part" };
    }

    const sink = this.sink;
    if (!sink) {
      return {
        status: STATUS_FAILED,
        message: "desktop disconnected",
      };
    }

    if (signal?.aborted) {
      return { status: STATUS_FAILED, message: "aborted" };
    }

    const resolvedToolId = randomUUID();
    toolPart.toolId = resolvedToolId;

    return new Promise<OperationResult>((resolve) => {
      // 043 R7 (specs/043-llm-stream-stall-recovery/research.md R7 /
      // specs/043-llm-stream-stall-recovery/contracts/stall-recovery-contract.md
      // §1.2): while the dispatch awaits the desktop, no LangChain callback
      // events fire, so a bare idleTimeout would trip mid-tool. `heartbeat`
      // refreshes LangGraph's idle timer unconditionally: call it immediately
      // (the first setInterval tick is TOOL_HEARTBEAT_INTERVAL_MS away), then
      // every interval until the dispatch exits.
      let heartbeatTimer: ReturnType<typeof setInterval> | undefined;
      if (heartbeat) {
        heartbeat();
        heartbeatTimer = setInterval(heartbeat, TOOL_HEARTBEAT_INTERVAL_MS);
      }
      const clearHeartbeat = () => {
        if (heartbeatTimer !== undefined) {
          clearInterval(heartbeatTimer);
          heartbeatTimer = undefined;
        }
      };

      const timer = setTimeout(() => {
        if (this.pending.delete(resolvedToolId)) {
          signal?.removeEventListener("abort", onAbort);
          clearHeartbeat();
          warn("operation dispatch timed out", { toolId: resolvedToolId });
          resolve({ status: STATUS_FAILED, message: "operation timed out" });
        }
      }, DISPATCH_TIMEOUT_MS);

      const onAbort = () => {
        if (this.pending.delete(resolvedToolId)) {
          clearTimeout(timer);
          clearHeartbeat();
          resolve({ status: STATUS_FAILED, message: "aborted" });
        }
      };
      if (signal) {
        signal.addEventListener("abort", onAbort, { once: true });
      }

      this.pending.set(resolvedToolId, {
        resolve,
        timer,
        cleanup: () => {
          clearHeartbeat();
          signal?.removeEventListener("abort", onAbort);
        },
      });

      const envelope: TeamFrame = buildTeamFrame(this.sessionId, this.templateId, {
        flowParts: { parts: [part] },
      });
      try {
        sink(envelope);
      } catch (err) {
        if (this.pending.delete(resolvedToolId)) {
          clearTimeout(timer);
          clearHeartbeat();
          signal?.removeEventListener("abort", onAbort);
          const msg = err instanceof Error ? err.message : "sink write error";
          warn("operation dispatch sink threw", { toolId: resolvedToolId, error: msg });
          resolve({ status: STATUS_FAILED, message: msg });
        }
      }
    });
  }

  /**
   * Resolve a pending dispatch whose tool_id matches the result part.
   * Called by the Connect handler when a FlowResultPart arrives from the
   * desktop on the control channel (a flowParts frame whose kind is
   * flow_result — spec 025 FR-023/FR-025). Unknown or stale tool_ids are
   * logged and ignored.
   */
  handleResult(result: FlowResultPart): void {
    const toolId = result.toolId ?? "";
    if (!toolId) {
      warn("tool result received with no tool_id");
      return;
    }

    const pending = this.pending.get(toolId);
    if (!pending) {
      warn("tool result for unknown tool_id", { toolId });
      return;
    }

    this.pending.delete(toolId);
    clearTimeout(pending.timer);
    pending.cleanup?.();

    const status = result.status ?? STATUS_UNSPECIFIED;
    const message = result.message ?? "";
    const resolved: OperationResult = { status, message };
    if (result.screenshot) {
      resolved.screenshot = toOperationScreenshot(result.screenshot);
    }
    pending.resolve(resolved);
  }
}
