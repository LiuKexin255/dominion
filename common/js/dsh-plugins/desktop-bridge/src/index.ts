/**
 * cordis plugin entry for the desktop operation bridge: binds desktop
 * flow-control connections to game sessions and dispatches FlowPart
 * operations with result receipts.
 *
 * Class-form Service plugin (official dsh core plugin convention —
 * `@deepseek-ai/dsh-session`/`AgentLoop` default-export their Service class;
 * the Loader unwraps `exports.default`): the constructor registers the
 * `desktopBridge` service on the mounting context, so the composition row
 * `{ id: desktop-bridge, name: '@dominion/dsh-desktop-bridge' }` exposes
 * `ctx.desktopBridge` with no inject (the service is self-contained —
 * contracts/desktop-bridge.md §2; the gRPC handlers are registered by the
 * host server.ts on the single 50051 server).
 * Contract: specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §2,
 * package shape: specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md §1.
 */

import { Service, type Context } from "@deepseek-ai/cordis";

import { DesktopBridge } from "./bridge.js";
import type {
  DesktopBridgeServiceHandlers,
  OperationResult,
  SendFrameCall,
  SendFrameCallback,
  StreamCall,
  WireFlowPart,
} from "./wire.js";

/** The plugin identifier the composition manifest references. */
export const name = "desktop-bridge";

/** The service face mounted as `ctx.desktopBridge`. */
export interface DesktopBridgeService {
  /**
   * Bind a downlink stream as THE connection for the session resource name.
   * Identity arrives in the Stream handler built by {@link handlers};
   * attach owns takeover (the previous connection is ended, its heartbeat
   * stopped) and disconnect cleanup.
   */
  attach(sessionName: string, stream: StreamCall): void;
  /**
   * Handler face for the session downlink (path-bound session identity):
   * takes over the session's previous connection and owns the confirmation
   * frame, heartbeat, and disconnect cleanup.
   */
  Stream(call: StreamCall): void;
  /**
   * Handler face for the uplink single-shot frame submission: a missing
   * session/frame is INVALID_ARGUMENT, no active downlink is
   * FAILED_PRECONDITION, and a delivered receipt resolves its in-flight
   * dispatch.
   */
  SendFrame(call: SendFrameCall, callback: SendFrameCallback): void;
  /**
   * Dispatch one FlowPart operation and await its result receipt. No
   * connection → FAILED "desktop disconnected"; abort → FAILED "aborted";
   * 20-minute timeout backstop → FAILED "operation timed out".
   */
  dispatch(
    sessionName: string,
    part: WireFlowPart,
    signal?: AbortSignal,
  ): Promise<OperationResult>;
  /**
   * Whether the session's bridge connection registry holds a live
   * connection (the GetAgent `desktop_connected` fact source,
   * specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md §4).
   */
  isDesktopConnected(sessionName: string): boolean;
  /** Handler implementation for the host's DesktopBridgeService registration. */
  handlers(): DesktopBridgeServiceHandlers;
}

export type {
  BridgeTeamFrame,
  BridgeUserFrame,
  DesktopBridgeServiceHandlers,
  OperationResult,
  OperationScreenshot,
  SendFrameCall,
  SendFrameCallback,
  StreamCall,
  ToolResultStatus,
  WireFlowPart,
  WireFlowResultPart,
} from "./wire.js";

declare module "@deepseek-ai/cordis" {
  interface Context {
    desktopBridge: DesktopBridgeService;
  }
}

/** Class-form cordis plugin providing `ctx.desktopBridge`. */
export class DesktopBridgePlugin extends Service implements DesktopBridgeService {
  private readonly bridge = new DesktopBridge();

  constructor(ctx: Context) {
    super(ctx, "desktopBridge");
  }

  attach(sessionName: string, stream: StreamCall): void {
    this.bridge.attach(sessionName, stream);
  }

  Stream(call: StreamCall): void {
    this.bridge.Stream(call);
  }

  SendFrame(call: SendFrameCall, callback: SendFrameCallback): void {
    this.bridge.SendFrame(call, callback);
  }

  dispatch(
    sessionName: string,
    part: WireFlowPart,
    signal?: AbortSignal,
  ): Promise<OperationResult> {
    return this.bridge.dispatch(sessionName, part, signal);
  }

  isDesktopConnected(sessionName: string): boolean {
    return this.bridge.isDesktopConnected(sessionName);
  }

  handlers(): DesktopBridgeServiceHandlers {
    return this.bridge.handlers();
  }
}

export default DesktopBridgePlugin;
