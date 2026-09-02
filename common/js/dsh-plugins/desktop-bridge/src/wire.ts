/**
 * Minimal local declarations of the desktop flow wire types (package
 * `projects.game`, file `projects/game/game.proto`).
 *
 * saolei-plugins.md §1 sanctions exactly this shape for the desktop-bridge
 * package: no runtime deps — "grpc handler 类型经 agent_v2 生成类型对齐或
 * 本地最小接口声明". The declarations mirror the proto-loader-gen-types
 * output (`projects/game/agent_v2:agent_v2_types`): camelCase fields,
 * optional/nullable members, enums as string-union types (the runtime loads
 * the proto with `enums: String`, so frames carry enum NAMES), and oneofs as
 * a `kind` discriminator alongside the nullable members. The generated types
 * remain the source of truth for field numbers/semantics; change the proto
 * there and mirror here.
 */

/** ToolResultStatus enum values as they appear on the wire (`enums: String`). */
export type ToolResultStatus =
  | "TOOL_RESULT_STATUS_UNSPECIFIED"
  | "TOOL_RESULT_STATUS_SUCCEEDED"
  | "TOOL_RESULT_STATUS_FAILED";

/** StatusSignalStatus enum values as they appear on the wire. */
export type StatusSignalStatus =
  | "STATUS_SIGNAL_STATUS_UNSPECIFIED"
  | "STATUS_SIGNAL_STATUS_ACTIVE"
  | "STATUS_SIGNAL_STATUS_IDLE";

/** Screenshot captured by the desktop alongside a tool result. `data` is a
 * base64-encoded PNG string (raw bytes from the wire are encoded before the
 * result reaches consumers). */
export interface OperationScreenshot {
  data: string;
  widthPx: number;
  heightPx: number;
}

/** Outcome of a dispatch — the fields of FlowResultPart the bridge carries. */
export interface OperationResult {
  status: ToolResultStatus;
  message: string;
  screenshot?: OperationScreenshot;
}

/** ImagePart (game.proto) — `data` is raw bytes over gRPC or a base64 string
 * over protojson. */
export interface WireImagePart {
  encoding?: string | null;
  data?: Uint8Array | string | null;
  widthPx?: number | null;
  heightPx?: number | null;
  scaleFactor?: number | string | null;
  windowTitle?: string | null;
}

export interface WireFlowResultPart {
  toolId?: string | null;
  status?: ToolResultStatus | null;
  message?: string | null;
  screenshot?: WireImagePart | null;
}

export interface WireStatusSignal {
  status?: StatusSignalStatus | null;
}

export interface WireWaitSignal {
  reason?: string | null;
}

export interface WireWarnSignal {
  message?: string | null;
  code?: string | null;
}

export interface WireMouseMovePart {
  toolId?: string | null;
  xPx?: number | null;
  yPx?: number | null;
  method?: string | null;
}

export interface WireMouseClickPart {
  toolId?: string | null;
  click?: string | null;
  method?: string | null;
}

export interface WireKeyboardPressPart {
  toolId?: string | null;
  key?: string | null;
}

export interface WireMouseMoveAndClickPart {
  toolId?: string | null;
  xPx?: number | null;
  yPx?: number | null;
  click?: string | null;
  method?: string | null;
}

/** FlowPart (game.proto) — control-only block; the oneof members this bridge
 * routes. */
export interface WireFlowPart {
  mouseMove?: WireMouseMovePart | null;
  mouseClick?: WireMouseClickPart | null;
  keyboardPress?: WireKeyboardPressPart | null;
  mouseMoveAndClick?: WireMouseMoveAndClickPart | null;
  wait?: WireWaitSignal | null;
  warn?: WireWarnSignal | null;
  status?: WireStatusSignal | null;
  flowResult?: WireFlowResultPart | null;
  kind?:
    | "mouseMove"
    | "mouseClick"
    | "keyboardPress"
    | "mouseMoveAndClick"
    | "wait"
    | "warn"
    | "status"
    | "flowResult";
}

export interface WireFlowParts {
  parts?: WireFlowPart[] | null;
}

/** Inbound UserFrame subset: identity (gateway-injected) plus the flowParts
 * payload. messageParts frames are not produced by the v2 desktop. */
export interface BridgeUserFrame {
  sessionId?: string | null;
  templateId?: string | null;
  flowParts?: WireFlowParts | null;
}

/** Outbound TeamFrame subset the bridge writes: the flowParts payload with a
 * full envelope (session/template/frame id/timestamp — v1 FR-013 semantics). */
export interface BridgeTeamFrame {
  sessionId?: string | null;
  templateId?: string | null;
  frameId?: string | null;
  createTime?: { seconds: number; nanos: number } | null;
  flowParts?: WireFlowParts | null;
}

/**
 * The one bidirectional stream face the bridge consumes. Structural
 * superset of grpc-js `ServerDuplexStream<UserFrame__Output, TeamFrame>`:
 * the generated `Connect` handler receives that stream and satisfies this
 * interface (EventEmitter `on`, Duplex `write`/`end`).
 */
export interface BidiStream {
  on(event: "data", listener: (frame: BridgeUserFrame) => void): unknown;
  on(
    event: "error" | "end" | "close" | "cancelled",
    listener: (err?: unknown) => void,
  ): unknown;
  write(frame: BridgeTeamFrame): unknown;
  end(): unknown;
}

/** Handler face returned by the service for host gRPC registration: the
 * generated `DesktopBridgeServiceHandlers` (agent_v2.proto) declares the same
 * single bidi `Connect` method. */
export interface DesktopBridgeServiceHandlers {
  Connect(call: BidiStream): void;
}
