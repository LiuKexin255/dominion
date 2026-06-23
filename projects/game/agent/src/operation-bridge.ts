/**
 * operation-bridge.ts — Session-scoped bridge between LangChain tools and the
 * desktop WebSocket bidi stream.
 *
 * OperationBridge is owned by SessionAgent and survives stream reconnects.
 * The Connect handler registers a sink callback on stream open and unregisters
 * it on stream end/error.  LangChain tools (e.g. mouse) call dispatch() to
 * send an operation frame to the desktop and await the matching result.
 *
 * The bridge never holds a reference to a specific stream instance — only a
 * write callback — so a new stream can re-register its sink without recreating
 * the bridge or losing track of in-flight dispatches.
 */

import { randomUUID } from "node:crypto";

import { info, warn } from "@dominion/common-js-logs";

import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import type { AgentOperationFrame } from "../game_types/projects/game/AgentOperationFrame";
import type { AgentOperationResultFrame } from "../game_types/projects/game/AgentOperationResultFrame";
import type { AgentOperationResultStatus } from "../game_types/projects/game/AgentOperationResultStatus";

/** Maximum wait time (ms) for an operation result before timing out. */
const DISPATCH_TIMEOUT_MS = 5_000;

/** String literal values for AgentOperationResultStatus (proto enum). */
const STATUS_SUCCEEDED = "AGENT_OPERATION_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "AGENT_OPERATION_RESULT_STATUS_FAILED";
const STATUS_UNSPECIFIED = "AGENT_OPERATION_RESULT_STATUS_UNSPECIFIED";

/**
 * Sink callback registered by the Connect handler.  Receives a full AgentFrame
 * whose payload is "operation".  The handler may augment envelope fields
 * (sessionId, frameId, sequence, sender, createTime) before writing to the
 * stream.
 */
export type OperationSink = (frame: AgentFrame) => void;

/** Outcome of a dispatch — mirrors the relevant fields of AgentOperationResultFrame. */
export interface OperationResult {
  status: AgentOperationResultStatus;
  message: string;
}

interface PendingDispatch {
  resolve: (result: OperationResult) => void;
  timer: ReturnType<typeof setTimeout>;
}

export class OperationBridge {
  private sink: OperationSink | null = null;
  private readonly pending = new Map<string, PendingDispatch>();
  private currentScreenshotId = "";

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
   * or error.  In-flight dispatches are left to time out naturally (5s cap)
   * so the bridge survives reconnect without spurious resolve/reject churn.
   */
  unregisterSink(): void {
    this.sink = null;
    info("operation bridge sink unregistered", {
      pendingCount: this.pending.size,
    });
  }

  /**
   * Dispatch an operation frame to the desktop via the registered sink and
   * await the matching result.
   *
   * Generates a UUID operation_id on the frame, wraps it in an AgentFrame
   * envelope, and writes via the sink.  If no sink is registered the frame is
   * not written but the dispatch still waits up to DISPATCH_TIMEOUT_MS before
   * returning FAILED, giving the caller a consistent timeout contract
   * regardless of sink state.
   *
   * @returns SUCCEEDED/FAILED status from the desktop, or FAILED on timeout,
   *          missing sink, or sink write error.
   */
  async dispatch(frame: AgentOperationFrame): Promise<OperationResult> {
    const operationId = randomUUID();
    frame.operationId = operationId;

    return new Promise<OperationResult>((resolve) => {
      const timer = setTimeout(() => {
        if (this.pending.delete(operationId)) {
          warn("operation dispatch timed out", { operationId });
          resolve({ status: STATUS_FAILED, message: "operation timed out" });
        }
      }, DISPATCH_TIMEOUT_MS);

      this.pending.set(operationId, { resolve, timer });

      const sink = this.sink;
      if (!sink) {
        warn("operation dispatch with no active sink", { operationId });
        return;
      }

      const envelope: AgentFrame = {
        payload: "operation",
        operation: frame,
      };
      try {
        sink(envelope);
      } catch (err) {
        if (this.pending.delete(operationId)) {
          clearTimeout(timer);
          const msg = err instanceof Error ? err.message : "sink write error";
          warn("operation dispatch sink threw", { operationId, error: msg });
          resolve({ status: STATUS_FAILED, message: msg });
        }
      }
    });
  }

  /**
   * Resolve a pending dispatch whose operation_id matches the result frame.
   * Called by the Connect handler when an AgentOperationResultFrame arrives
   * from the desktop.  Unknown or stale operation_ids are logged and ignored.
   */
  handleResult(resultFrame: AgentOperationResultFrame): void {
    const operationId = resultFrame.operationId ?? "";
    if (!operationId) {
      warn("operation result received with no operation_id");
      return;
    }

    const pending = this.pending.get(operationId);
    if (!pending) {
      warn("operation result for unknown operation_id", { operationId });
      return;
    }

    this.pending.delete(operationId);
    clearTimeout(pending.timer);

    const status = resultFrame.status ?? STATUS_UNSPECIFIED;
    const message = resultFrame.message ?? "";
    pending.resolve({ status, message });
  }

  /**
   * Store the current turn's screenshot_id.  Set by the Connect handler when a
   * screenshot frame arrives; read by the mouse tool at dispatch time.
   */
  setCurrentScreenshotId(id: string): void {
    this.currentScreenshotId = id;
  }

  /**
   * Return the most recently stored screenshot_id.  The mouse tool reads this
   * to populate AgentOperationFrame.screenshotId before calling dispatch.
   */
  getCurrentScreenshotId(): string {
    return this.currentScreenshotId;
  }
}
