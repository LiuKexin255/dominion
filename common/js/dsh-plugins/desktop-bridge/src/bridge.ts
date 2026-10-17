/**
 * The desktop connection registry and dispatch pipeline behind the
 * `ctx.desktopBridge` service.
 *
 * Semantics (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §2,
 * data-model.md §2.6):
 * - A connection object holds only the stream face for writes, its heartbeat
 *   handle, and its own cleanup handle — pending dispatches are keyed per
 *   session at bridge level, so a reconnect (new connection taking over)
 *   never loses in-flight dispatches and a late receipt can still resolve
 *   them.
 * - Attaching a second connection for a session takes over: the previous
 *   stream is ended and its late disconnect is a compare-and-delete no-op
 *   that cannot clobber the fresh registration.
 * - Dispatch mints a fresh UUID tool_id, decoupled from any conversation
 *   tool_call.id (the bridge owns the operation channel's correlation ids).
 * - Failure paths resolve FAILED in-band (never throw): no connection →
 *   "desktop disconnected"; abort → "aborted"; 20-minute timeout backstop →
 *   "operation timed out"; a disconnect settles in-flight dispatches →
 *   "desktop disconnected" (data-model.md §2.6 状态迁移).
 * - Stale receipts (unknown/expired tool_id) are logged and ignored.
 */

import { randomUUID } from "node:crypto";

import { info, warn } from "@dominion/common-js-logs";

import type {
  BridgeTeamFrame,
  BridgeUserFrame,
  DesktopBridgeServiceHandlers,
  OperationResult,
  OperationScreenshot,
  SendFrameCall,
  SendFrameCallback,
  StatusSignalStatus,
  StreamCall,
  ToolResultStatus,
  WireFlowPart,
  WireFlowResultPart,
  WireImagePart,
  WireMouseMoveAndClickPart,
  WireMouseMovePart,
  WireMouseClickPart,
  WireKeyboardPressPart,
} from "./wire.js";

/** Maximum wait (ms) for a result before timing out. Safety-net backstop:
 * the desktop's 15-min confirmation auto-release always fires first under
 * debug usage, so this stays dormant in normal operation (v1
 * operation-bridge.ts DISPATCH_TIMEOUT_MS rationale). */
const DISPATCH_TIMEOUT_MS = 1_200_000;

/** Downlink idle interval between status heartbeat frames. Keeps the
 * long-lived stream alive across intermediate idle timeouts and serves as
 * application-level liveness (~10s, specs/066-desktop-bridge-split/spec.md
 * FR-008). */
const HEARTBEAT_INTERVAL_MS = 10_000;

const STATUS_SUCCEEDED: ToolResultStatus = "TOOL_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED: ToolResultStatus = "TOOL_RESULT_STATUS_FAILED";
const STATUS_UNSPECIFIED: ToolResultStatus = "TOOL_RESULT_STATUS_UNSPECIFIED";

/** Connection-confirmation and heartbeat status value
 * (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.2/§2.3). */
const STATUS_ACTIVE: StatusSignalStatus = "STATUS_SIGNAL_STATUS_ACTIVE";

/** gRPC INVALID_ARGUMENT: a request argument is malformed regardless of the
 * system state (https://github.com/grpc/grpc/blob/master/doc/statuscodes.md). */
const INVALID_ARGUMENT = 3;

/** gRPC FAILED_PRECONDITION: the session has no active downlink stream, so the
 * uplink frame cannot be delivered until the desktop re-establishes it
 * (https://github.com/grpc/grpc/blob/master/doc/statuscodes.md;
 * specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.6). */
const FAILED_PRECONDITION = 9;

/** Operation-channel id stamped onto the dispatched FlowPart. */
type ToolId = string;

interface PendingDispatch {
  resolve: (result: OperationResult) => void;
  timer: ReturnType<typeof setTimeout>;
  cleanup?: () => void;
}

/** One live desktop connection: the stream face plus its heartbeat handle. */
interface Connection {
  stream: StreamCall;
  heartbeat?: ReturnType<typeof setInterval>;
}

/**
 * Convert an ImagePart screenshot into the resolved OperationScreenshot
 * shape. Raw bytes arriving over the uplink are base64-encoded here; an
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
   * Bind a stream as THE downlink connection for `sessionName`. A previously
   * registered connection is taken over: its stream is ended (and its
   * heartbeat stopped) and its late disconnect is a no-op against the fresh
   * registration. The fresh connection immediately writes one status
   * confirmation frame and then a status heartbeat every
   * {@link HEARTBEAT_INTERVAL_MS}; a disconnect of the current stream clears
   * the registration, stops the heartbeat, and settles that session's
   * in-flight dispatches FAILED "desktop disconnected"
   * (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2,
   * data-model.md §1.1/§1.3).
   *
   * The Stream handler below derives `sessionName` from the path-bound
   * request; `attach` owns everything after identity. The stream carries no
   * inbound frames — the desktop's uplink is the separate SendFrame call — so
   * the connection registers only the outbound write face and its lifecycle
   * listeners.
   */
  attach(sessionName: string, stream: StreamCall): void {
    const previous = this.connections.get(sessionName);
    if (previous !== undefined) {
      this.connections.delete(sessionName);
      if (previous.heartbeat !== undefined) {
        clearInterval(previous.heartbeat);
      }
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
    const onDisconnect = (reason: unknown) => {
      this.disconnect(sessionName, connection, reason);
    };
    stream.on("error", onDisconnect);
    stream.on("close", onDisconnect);
    stream.on("cancelled", onDisconnect);
    this.writeStatus(sessionName, connection);
    connection.heartbeat = setInterval(() => {
      this.writeStatus(sessionName, connection);
    }, HEARTBEAT_INTERVAL_MS);
    info("desktop connection attached", { session: sessionName });
  }

  /**
   * The gRPC handler face for the session downlink: the request's
   * path-injected session resource name binds the connection and owns
   * everything after identity (confirmation frame, heartbeat, disconnect
   * cleanup). A call with no session ends with zero frames: a server-streaming
   * handler that simply returns sends no status and leaves the RPC pending
   * until the client cancels, so the handler closes the call explicitly
   * (specs/066-desktop-bridge-split/research.md D12.2).
   */
  Stream(call: StreamCall): void {
    const sessionName = call.request.session;
    if (!sessionName) {
      warn("stream call with no session id");
      call.end();
      return;
    }
    this.attach(sessionName, call);
  }

  /**
   * The gRPC handler face for the uplink single-shot frame submission: the
   * request's path-injected session addresses an already-active downlink;
   * a missing session or frame is a request-level INVALID_ARGUMENT, and a
   * session with no active connection is a FAILED_PRECONDITION the desktop
   * retries after re-establishing the stream. A delivered frame is routed
   * through the same part dispatch as the in-band path, and the synchronous
   * reply carries no information — correlation rides on `flow_result.tool_id`
   * (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.6/§2.7/§2.9).
   */
  SendFrame(call: SendFrameCall, callback: SendFrameCallback): void {
    const sessionName = call.request.session;
    if (!sessionName) {
      callback({ code: INVALID_ARGUMENT, message: "session must not be empty" });
      return;
    }
    const frame = call.request.frame;
    if (!frame || !frame.flowParts) {
      callback({
        code: INVALID_ARGUMENT,
        message: "frame.flow_parts must be present",
      });
      return;
    }
    if (!this.connections.has(sessionName)) {
      callback({ code: FAILED_PRECONDITION, message: "desktop stream not active" });
      return;
    }
    this.routeFrame(sessionName, frame);
    callback(null, {});
  }

  /**
   * Whether the session's connection registry currently holds a live
   * connection — the `desktop_connected` fact source read directly off the
   * registry (specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md
   * §4). Takeover-window semantics are eventually consistent by
   * construction: the superseded stream's late disconnect is a
   * compare-and-delete no-op, so it can never clear the fresh registration.
   */
  isDesktopConnected(sessionName: string): boolean {
    return this.connections.has(sessionName);
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
   * (server.ts adds it as the DesktopBridgeService implementation). `Stream`
   * binds the session from the path-bound request; `SendFrame` forwards one
   * uplink frame into the same part dispatch.
   */
  handlers(): DesktopBridgeServiceHandlers {
    return {
      Stream: (call) => {
        this.Stream(call);
      },
      SendFrame: (call, callback) => {
        this.SendFrame(call, callback);
      },
    };
  }

  /** Write one ACTIVE status frame on the connection, tolerating a stream
   * that is already closed (the disconnect listener owns cleanup). */
  private writeStatus(sessionName: string, connection: Connection): void {
    const status: WireFlowPart = { status: { status: STATUS_ACTIVE } };
    try {
      connection.stream.write(buildFlowFrame(sessionName, [status]));
    } catch (err) {
      warn("desktop status write failed", {
        session: sessionName,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }

  /**
   * Route one uplink frame delivered by the SendFrame handler for a bound
   * session: receipts resolve in-flight dispatches, wait/warn parts keep
   * their existing log behavior, and every other part is logged and ignored
   * (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.9).
   */
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
      if (part.wait) {
        info("wait signal received from desktop", { session: sessionName });
        continue;
      }
      if (part.warn) {
        warn("warn signal received from desktop", {
          session: sessionName,
          message: part.warn.message ?? "",
        });
        continue;
      }
      warn("unsupported desktop frame part ignored", { session: sessionName });
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
    if (connection.heartbeat !== undefined) {
      clearInterval(connection.heartbeat);
    }
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
