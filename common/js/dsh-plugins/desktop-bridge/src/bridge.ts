/**
 * The desktop connection registry and dispatch pipeline behind the
 * `ctx.desktopBridge` service.
 *
 * Semantics (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §2,
 * data-model.md §2.6):
 * - A connection object holds only the stream face for writes and its own
 *   cleanup handle — pending dispatches are keyed per session at bridge
 *   level, so a reconnect (new connection taking over) never loses in-flight
 *   dispatches and a late receipt can still resolve them (v1
 *   projects/game/agent/src/operation-bridge.ts:10-12 semantics).
 * - Attaching a second connection for a session takes over: the previous
 *   stream is ended and its late disconnect is a compare-and-delete no-op
 *   that cannot clobber the fresh registration.
 * - Dispatch mints a fresh UUID tool_id, decoupled from any conversation
 *   tool_call.id (v1 operation-bridge.ts:232-233).
 * - Failure paths resolve FAILED in-band (never throw): no connection →
 *   "desktop disconnected"; abort → "aborted"; 20-minute timeout backstop →
 *   "operation timed out"; a disconnect settles in-flight dispatches →
 *   "desktop disconnected" (data-model.md §2.6 状态迁移).
 * - Stale receipts (unknown/expired tool_id) are logged and ignored (v1
 *   handleResult semantics).
 */

import { randomUUID } from "node:crypto";

import { info, warn } from "@dominion/common-js-logs";

import type {
  BidiStream,
  BridgeTeamFrame,
  BridgeUserFrame,
  DesktopBridgeServiceHandlers,
  OperationResult,
  OperationScreenshot,
  ToolResultStatus,
  WireFlowPart,
  WireFlowResultPart,
  WireImagePart,
  WireMouseMoveAndClickPart,
  WireMouseMovePart,
  WireMouseClickPart,
  WireKeyboardPressPart,
  WireStatusSignal,
} from "./wire.js";

/** Maximum wait (ms) for a result before timing out. Safety-net backstop:
 * the desktop's 15-min confirmation auto-release always fires first under
 * debug usage, so this stays dormant in normal operation (v1
 * operation-bridge.ts DISPATCH_TIMEOUT_MS rationale). */
const DISPATCH_TIMEOUT_MS = 1_200_000;

const STATUS_SUCCEEDED: ToolResultStatus = "TOOL_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED: ToolResultStatus = "TOOL_RESULT_STATUS_FAILED";
const STATUS_UNSPECIFIED: ToolResultStatus = "TOOL_RESULT_STATUS_UNSPECIFIED";

/** Operation-channel id stamped onto the dispatched FlowPart. */
type ToolId = string;

interface PendingDispatch {
  resolve: (result: OperationResult) => void;
  timer: ReturnType<typeof setTimeout>;
  cleanup?: () => void;
}

/** One live desktop connection: the stream face plus the disconnect wiring. */
interface Connection {
  stream: BidiStream;
}

/**
 * Convert an ImagePart screenshot into the resolved OperationScreenshot
 * shape. Raw bytes arriving over the bidi stream are base64-encoded here; an
 * already-string `data` (protojson) passes through unchanged. Missing numeric
 * dimensions coalesce to 0 so the resolved shape stays non-optional.
 */
function toOperationScreenshot(source: WireImagePart): OperationScreenshot {
  const { data, widthPx, heightPx } = source;
  const encoded =
    typeof data === "string" ? data : Buffer.from(data ?? "").toString("base64");
  return { data: encoded, widthPx: widthPx ?? 0, heightPx: heightPx ?? 0 };
}

/**
 * Extract the operation oneof member of a FlowPart (v1
 * operation-bridge.ts dispatch extraction).
 */
function operationPart(
  part: WireFlowPart,
): WireMouseMovePart | WireMouseClickPart | WireKeyboardPressPart | WireMouseMoveAndClickPart | undefined {
  return (
    part.mouseMove ??
    part.mouseClick ??
    part.keyboardPress ??
    part.mouseMoveAndClick ??
    undefined
  );
}

/**
 * Derive the session resource name `templates/{template}/sessions/{session}`
 * from a bridge user frame. The gateway injects the bare segments from the
 * connect URL path into the first frame (desktop-bridge.md §1); a full
 * resource-name form in sessionId is tolerated defensively (v1
 * extractSessionId). Returns undefined when the frame carries no session id.
 */
export function deriveSessionName(frame: BridgeUserFrame): string | undefined {
  const raw = frame.sessionId ?? "";
  if (!raw) {
    return undefined;
  }
  const session = /(?:^|\/)sessions\/([^/]+)$/.exec(raw)?.[1] ?? raw;
  const template = frame.templateId ?? "";
  return `templates/${template}/sessions/${session}`;
}

/** Build an outbound flowParts TeamFrame with the full envelope (v1 FR-013:
 * frame_id/create_time always set; flowParts payload ⇒ role unspecified). */
function buildFlowFrame(
  sessionName: string,
  parts: WireFlowPart[],
): BridgeTeamFrame {
  const template = /^templates\/([^/]+)\//.exec(sessionName)?.[1] ?? "";
  const now = new Date();
  const epochMs = now.getTime();
  return {
    sessionId: sessionName,
    templateId: template,
    frameId: randomUUID(),
    createTime: {
      seconds: Math.floor(epochMs / 1000),
      nanos: (epochMs % 1000) * 1_000_000,
    },
    flowParts: { parts },
  };
}

/**
 * The bridge core: session-keyed connection registry + dispatch pipeline.
 * The cordis plugin (src/index.ts) delegates to one instance; tests drive it
 * directly with injected stream doubles (style/javascript.md Mock convention).
 */
export class DesktopBridge {
  /** Live connections keyed by the session resource name. */
  private readonly connections = new Map<string, Connection>();
  /** In-flight dispatches keyed by session, then by the minted tool_id. */
  private readonly pending = new Map<string, Map<ToolId, PendingDispatch>>();

  /**
   * Bind a bidi stream as THE connection for `sessionName`. A previously
   * registered connection is taken over: its stream is ended and its late
   * disconnect is a no-op against the fresh registration. Stream disconnect
   * afterwards clears the registration and settles that session's in-flight
   * dispatches FAILED "desktop disconnected".
   *
   * The caller (the Connect handler below) derives `sessionName` from the
   * gateway-injected first frame; `attach` owns everything after identity.
   */
  attach(sessionName: string, stream: BidiStream): void {
    const previous = this.connections.get(sessionName);
    if (previous !== undefined) {
      this.connections.delete(sessionName);
      info("desktop connection taken over", { session: sessionName });
      try {
        previous.stream.end();
      } catch (err) {
        warn("desktop connection takeover end failed", {
          session: sessionName,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    }
    const connection: Connection = { stream };
    this.connections.set(sessionName, connection);
    stream.on("data", (frame) => {
      this.routeFrame(sessionName, frame);
    });
    const onDisconnect = (reason: unknown) => {
      this.disconnect(sessionName, connection, reason);
    };
    stream.on("end", onDisconnect);
    stream.on("error", onDisconnect);
    stream.on("close", onDisconnect);
    stream.on("cancelled", onDisconnect);
    info("desktop connection attached", { session: sessionName });
  }

  /**
   * Dispatch a FlowPart operation to the session's desktop connection and
   * await the matching FlowResultPart. Minting a fresh UUID tool_id is
   * unconditional — the id correlates dispatch↔result only and is unrelated
   * to any conversation tool_call.id. All failure paths resolve FAILED
   * in-band; nothing throws.
   */
  async dispatch(
    sessionName: string,
    part: WireFlowPart,
    signal?: AbortSignal,
  ): Promise<OperationResult> {
    const toolPart = operationPart(part);
    if (!toolPart) {
      warn("dispatch received a non-operation FlowPart");
      return { status: STATUS_FAILED, message: "invalid tool part" };
    }
    const connection = this.connections.get(sessionName);
    if (!connection) {
      return { status: STATUS_FAILED, message: "desktop disconnected" };
    }
    if (signal?.aborted) {
      return { status: STATUS_FAILED, message: "aborted" };
    }

    const toolId = randomUUID();
    toolPart.toolId = toolId;
    const sessionPending = this.pendingOf(sessionName);

    return new Promise<OperationResult>((resolve) => {
      const timer = setTimeout(() => {
        if (sessionPending.delete(toolId)) {
          signal?.removeEventListener("abort", onAbort);
          warn("desktop dispatch timed out", { session: sessionName, toolId });
          resolve({ status: STATUS_FAILED, message: "operation timed out" });
        }
      }, DISPATCH_TIMEOUT_MS);

      const onAbort = () => {
        if (sessionPending.delete(toolId)) {
          clearTimeout(timer);
          resolve({ status: STATUS_FAILED, message: "aborted" });
        }
      };
      if (signal) {
        signal.addEventListener("abort", onAbort, { once: true });
      }

      sessionPending.set(toolId, {
        resolve,
        timer,
        cleanup: signal
          ? () => signal.removeEventListener("abort", onAbort)
          : undefined,
      });

      const frame = buildFlowFrame(sessionName, [part]);
      try {
        connection.stream.write(frame);
      } catch (err) {
        if (sessionPending.delete(toolId)) {
          clearTimeout(timer);
          signal?.removeEventListener("abort", onAbort);
          const message = err instanceof Error ? err.message : "stream write error";
          warn("desktop dispatch write failed", {
            session: sessionName,
            toolId,
            error: message,
          });
          resolve({ status: STATUS_FAILED, message });
        }
      }
    });
  }

  /**
   * Resolve a pending dispatch whose tool_id matches the result part.
   * Unknown or stale tool_ids are logged and ignored.
   */
  handleResult(sessionName: string, result: WireFlowResultPart): void {
    const toolId = result.toolId ?? "";
    if (!toolId) {
      warn("desktop tool result received with no tool_id", { session: sessionName });
      return;
    }
    const pending = this.pendingOf(sessionName).get(toolId);
    if (!pending) {
      warn("desktop tool result for unknown tool_id", {
        session: sessionName,
        toolId,
      });
      return;
    }

    this.pendingOf(sessionName).delete(toolId);
    clearTimeout(pending.timer);
    pending.cleanup?.();

    const resolved: OperationResult = {
      status: result.status ?? STATUS_UNSPECIFIED,
      message: result.message ?? "",
    };
    if (result.screenshot) {
      resolved.screenshot = toOperationScreenshot(result.screenshot);
    }
    pending.resolve(resolved);
  }

  /**
   * The gRPC handler face for host registration
   * (server.ts adds it as the DesktopBridgeService implementation). The
   * first frame carries the gateway-injected identity: the handler derives
   * the session resource name, attaches, and routes that frame (the attach
   * listener registers during the same emit, so the first frame is routed
   * here exactly once). Later frames route through the attach listener.
   */
  handlers(): DesktopBridgeServiceHandlers {
    return {
      Connect: (call) => {
        let bound = false;
        call.on("data", (frame) => {
          if (bound) {
            return;
          }
          const sessionName = deriveSessionName(frame);
          if (sessionName === undefined) {
            warn("connect frame with no session id");
            return;
          }
          bound = true;
          this.attach(sessionName, call);
          this.routeFrame(sessionName, frame);
        });
      },
    };
  }

  /** Route one inbound frame for a bound session (receipts + signals). */
  private routeFrame(sessionName: string, frame: BridgeUserFrame): void {
    if (!frame.flowParts) {
      // messageParts frames are not produced by the v2 desktop
      // (desktop-bridge.md §1: 消息帧在 v2 面不产生).
      return;
    }
    const parts = frame.flowParts?.parts ?? [];
    for (const part of parts) {
      if (part.flowResult) {
        this.handleResult(sessionName, part.flowResult);
        continue;
      }
      const status: WireStatusSignal | undefined = part.status ?? undefined;
      if (status) {
        this.respondProbe(sessionName, status);
        continue;
      }
      if (part.wait) {
        info("wait signal received from desktop", { session: sessionName });
      } else if (part.warn) {
        warn("warn signal received from desktop", {
          session: sessionName,
          message: part.warn.message ?? "",
        });
      }
    }
  }

  /** Answer the desktop's connectivity probe by echoing the status enum name
   * (desktop-bridge.md §1 probe-response row). */
  private respondProbe(sessionName: string, status: WireStatusSignal): void {
    const connection = this.connections.get(sessionName);
    if (!connection) {
      return;
    }
    const echo: WireFlowPart = { status: { status: status.status ?? "STATUS_SIGNAL_STATUS_UNSPECIFIED" } };
    try {
      connection.stream.write(buildFlowFrame(sessionName, [echo]));
    } catch (err) {
      warn("desktop probe response write failed", {
        session: sessionName,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }

  /**
   * Clear the registration for a closing stream, but only when the closing
   * connection is still the live registration (compare-and-delete): a stale
   * close from a superseded stream must not clear a fresh registration nor
   * settle its in-flight dispatches.
   */
  private disconnect(
    sessionName: string,
    connection: Connection,
    reason: unknown,
  ): void {
    if (this.connections.get(sessionName) !== connection) {
      return;
    }
    this.connections.delete(sessionName);
    info("desktop connection closed", {
      session: sessionName,
      reason: reason instanceof Error ? reason.message : String(reason ?? ""),
    });
    const sessionPending = this.pendingOf(sessionName);
    for (const [toolId, pending] of [...sessionPending]) {
      sessionPending.delete(toolId);
      clearTimeout(pending.timer);
      pending.cleanup?.();
      pending.resolve({
        status: STATUS_FAILED,
        message: "desktop disconnected",
      });
    }
  }

  private pendingOf(sessionName: string): Map<ToolId, PendingDispatch> {
    let sessionPending = this.pending.get(sessionName);
    if (sessionPending === undefined) {
      sessionPending = new Map();
      this.pending.set(sessionName, sessionPending);
    }
    return sessionPending;
  }
}

export { STATUS_SUCCEEDED, STATUS_FAILED, STATUS_UNSPECIFIED };
