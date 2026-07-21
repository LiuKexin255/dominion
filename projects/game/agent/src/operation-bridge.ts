/**
 * operation-bridge.ts — Session-scoped bridge between LangChain tools and the
 * desktop WebSocket bidi stream.
 *
 * OperationBridge is owned by SessionAgent and survives stream reconnects.
 * The Connect handler registers a sink callback on stream open and unregisters
 * it on stream end/error.  LangChain tools (e.g. mouse) call dispatch() to
 * send a tool Part to the desktop and await the matching ToolResultPart.
 *
 * The bridge never holds a reference to a specific stream instance — only a
 * write callback — so a new stream can re-register its sink without recreating
 * the bridge or losing track of in-flight dispatches.
 *
 * Part-model contract: a tool request is a Part (MouseMovePart or
 * MouseClickPart) wrapped in a PartBlock carried by a content AgentFrame. The
 * desktop replies with a content frame whose ToolResultPart.tool_id matches the
 * request. tool_id is the single correlation key (no invoke_id/sequence).
 */

import { randomUUID } from "node:crypto";

import { info, warn } from "@dominion/common-js-logs";

import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import type { Part } from "../game_types/projects/game/Part";
import type { ImagePart } from "../game_types/projects/game/ImagePart";
import type { MouseMovePart } from "../game_types/projects/game/MouseMovePart";
import type { MouseClickPart } from "../game_types/projects/game/MouseClickPart";
import type { KeyboardPressPart } from "../game_types/projects/game/KeyboardPressPart";
import type { MouseMoveAndClickPart } from "../game_types/projects/game/MouseMoveAndClickPart";
import type { ToolResultPart } from "../game_types/projects/game/ToolResultPart";
import type { ToolResultStatus } from "../game_types/projects/game/ToolResultStatus";

/** Maximum wait time (ms) for a tool result before timing out. */
const DISPATCH_TIMEOUT_MS = 5_000;

/** String literal values for ToolResultStatus (proto enum). */
const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "TOOL_RESULT_STATUS_FAILED";
const STATUS_UNSPECIFIED = "TOOL_RESULT_STATUS_UNSPECIFIED";

/**
 * Sink callback registered by the Connect handler.  Receives a full AgentFrame
 * whose payload is "content".  The handler may augment envelope fields
 * (sessionId, frameId, sender, createTime) before writing to the stream.
 */
export type OperationSink = (frame: AgentFrame) => void;

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

/** Outcome of a dispatch — mirrors the relevant fields of ToolResultPart. */
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
  private sink: OperationSink | null = null;
  private readonly pending = new Map<string, PendingDispatch>();

  /**
   * Register the stream write callback.  Called by the Connect handler when a
   * new bidi stream opens.  Replaces any previously registered sink.
   */
  registerSink(writeFn: OperationSink): void {
    this.sink = writeFn;
    info("operation bridge sink registered");
  }

  /**
   * Clear the registered sink.  Called by the Connect handler on stream end
   * or error.  In-flight dispatches whose per-turn AbortController fires
   * resolve immediately with FAILED "aborted"; the 5s timeout remains as a
   * fallback for dispatches without a signal.
   */
  unregisterSink(): void {
    this.sink = null;
    info("operation bridge sink unregistered", {
      pendingCount: this.pending.size,
    });
  }

  /**
   * Dispatch a tool Part (MouseMovePart, MouseClickPart, KeyboardPressPart,
   * or MouseMoveAndClickPart) to the desktop via the registered sink and
   * await the matching ToolResultPart.
   *
   * Generates a UUID tool_id on the inner part, wraps the Part in a PartBlock
   * carried by a content AgentFrame, and writes via the sink.  When no sink
   * is registered (desktop disconnected), resolves FAILED immediately rather
   * than throwing.  When the optional `signal` is already aborted, or aborts
   * while the dispatch is pending, resolves FAILED "aborted" immediately —
   * abort is a third race participant alongside the 5s timeout and
   * handleResult, whichever fires first wins.
   *
   * Spec 018-saolei-mcp FR-004a/b: `KeyboardPressPart` (F2 new game) and
   * `MouseMoveAndClickPart` (window-message cell operations) are stamped with
   * `tool_id` here exactly like the existing mouse parts; the desktop replies
   * with the same correlation key.
   *
   * @param part   - Tool Part to dispatch.
   * @param signal - Optional AbortSignal; when aborted, resolves FAILED.
   * @returns SUCCEEDED/FAILED status from the desktop, or FAILED on timeout,
   *          abort, non-tool Part, no-sink, or sink write error.
   */
  async dispatch(
    part: Part,
    signal?: AbortSignal,
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
      warn("dispatch received a non-tool Part");
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

    const toolId = randomUUID();
    toolPart.toolId = toolId;

    return new Promise<OperationResult>((resolve) => {
      const timer = setTimeout(() => {
        if (this.pending.delete(toolId)) {
          signal?.removeEventListener("abort", onAbort);
          warn("operation dispatch timed out", { toolId });
          resolve({ status: STATUS_FAILED, message: "operation timed out" });
        }
      }, DISPATCH_TIMEOUT_MS);

      const onAbort = () => {
        if (this.pending.delete(toolId)) {
          clearTimeout(timer);
          resolve({ status: STATUS_FAILED, message: "aborted" });
        }
      };
      if (signal) {
        signal.addEventListener("abort", onAbort, { once: true });
      }

      this.pending.set(toolId, {
        resolve,
        timer,
        cleanup: signal
          ? () => signal.removeEventListener("abort", onAbort)
          : undefined,
      });

      const envelope: AgentFrame = {
        payload: "content",
        content: { parts: [part] },
      };
      try {
        sink(envelope);
      } catch (err) {
        if (this.pending.delete(toolId)) {
          clearTimeout(timer);
          signal?.removeEventListener("abort", onAbort);
          const msg = err instanceof Error ? err.message : "sink write error";
          warn("operation dispatch sink threw", { toolId, error: msg });
          resolve({ status: STATUS_FAILED, message: msg });
        }
      }
    });
  }

  /**
   * Resolve a pending dispatch whose tool_id matches the result part.
   * Called by the Connect handler when a ToolResultPart arrives from the
   * desktop.  Unknown or stale tool_ids are logged and ignored.
   */
  handleResult(result: ToolResultPart): void {
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
