// Wails binding wrappers — all backend calls go through window.go.main.App
// In production, Wails v2 injects window.go.main.App with bound methods

// Extend Window with Wails runtime types
declare global {
  interface Window {
    go?: {
      main?: {
        App?: WailsApp
      }
    }
    runtime?: {
      EventsOn: (event: string, callback: (...args: unknown[]) => void) => void
    }
  }
}

// ─── Template (local constant control plane, FR-024) ────────────────────────
//
// The desktop holds the Template list as LOCAL constants and MUST NOT fetch a
// template-list API (spec 031-team-template-mode spec.md FR-024). The values
// are the Template resource path segments, matching the proto Template
// resource / gameconst constants (`game.TemplateName{TemplateID: "saolei"}`,
// specs/031-team-template-mode/contracts/api-contract.md §3.1). Current known
// templates: saolei only.

export const TEMPLATE_SAOLEI = 'saolei'

/** Local template constants — the top-level control plane (FR-024). */
export const TEMPLATES: string[] = [TEMPLATE_SAOLEI]

// ─── Core types ─────────────────────────────────────────────────────────────

export interface Config {
  gateway_url: string
  env: string
}

export interface Session {
  name: string
  sessionId: string
  template: string
  createTime: string
}

// Team is the execution subject of a session: a per-template set of agents
// (replaces the old single-agent `Agent`; spec 031-team-template-mode
// data-model.md §1.3). Returned by getTeam/createTeam.
export interface Team {
  name: string
  sessionId: string
  agents: TeamAgent[]
  createTime?: string
}

// TeamAgent is one agent inside the team, with its user-input acceptance flag
// (FR-031). saolei: player=true (accepts input), planner=false (observe-only).
export interface TeamAgent {
  name: string
  acceptsUserInput: boolean
}

// ─── Enums ──────────────────────────────────────────────────────────────────

export enum FrameSender {
  UNSPECIFIED = 0,
  USER = 1,
  AGENT = 2,
  SYSTEM = 3,
}

export enum ImageEncoding {
  UNSPECIFIED = 0,
  PNG = 1,
}

// MouseClickAction lists click types only. MOVE is expressed by MouseMovePart,
// so a click part cannot carry a move action. Mirrors proto MouseClickAction.
export enum MouseClickAction {
  UNSPECIFIED = 0,
  LEFT_CLICK = 1,
  LEFT_DOUBLE_CLICK = 2,
  RIGHT_CLICK = 3,
  RIGHT_DOUBLE_CLICK = 4,
  LEFT_RIGHT_PRESS = 5,
}

// Outcome of an executed tool operation. Mirrors proto ToolResultStatus.
export enum ToolResultStatus {
  UNSPECIFIED = 0,
  SUCCEEDED = 1,
  FAILED = 2,
}

// ─── Content Part Model (spec 023 content-model split) ─────────────────────
//
// The content model is split into two disjoint categories
// (specs/023-saolei-mcp-refine/contracts/content-model-contract.md §1..§6;
// spec 023 C3 / FR-001..FR-004):
//
//   - MessagePart (display only): text / thinking / image / tool_call /
//     tool_result. Carried by AgentFrame.messageParts (live) and Message.content
//     (history) — identical shape so live and history render identically.
//   - FlowPart (control only — never rendered in the conversation): mouse/
//     keyboard operations + wait/warn/status signals. Carried by
//     AgentFrame.flowParts. protojson flattens each oneof so exactly one
//     variant field is set (the field name is the discriminator).

export interface TextPart {
  content: string
}

export interface ThinkingPart {
  content: string
}

// ImagePart arrives as protojson: encoding is the enum name string
// (e.g. "IMAGE_ENCODING_PNG") and data is base64-encoded bytes.
export interface ImagePart {
  encoding?: ImageEncoding | string
  data: string
  widthPx?: number
  heightPx?: number
  scaleFactor?: number
  windowTitle?: string
}

// ToolCallPart carries the model's tool invocation as display content
// (spec 023 FR-002). tool_id links the call to its tool_result MessagePart
// within the conversation channel for bubble grouping (spec 023 C6/C13).
// The operation channel uses an independent bridge-minted id
// (contracts/tool-dispatch-contract.md §1; research.md D10).
export interface ToolCallPart {
  toolId?: string
  name?: string
  argsJson?: string
}

export interface ToolResultPart {
  toolId?: string
  // status arrives as the proto enum name string
  // (e.g. "TOOL_RESULT_STATUS_SUCCEEDED") under protojson.
  status?: ToolResultStatus | string
  message?: string
  screenshot?: ImagePart
}

// MessagePart is one display-only content block. Exactly one variant field is
// set; use messagePartKind() to read the active variant.
export interface MessagePart {
  text?: TextPart
  thinking?: ThinkingPart
  image?: ImagePart
  toolCall?: ToolCallPart
  toolResult?: ToolResultPart
}

export interface MessageParts {
  parts?: MessagePart[]
}

// FlowPart operation messages (unchanged fields from spec 018; moved into
// FlowPart.kind per spec 023).
export interface MouseMovePart {
  toolId?: string
  xPx: number
  yPx: number
}

export interface MouseClickPart {
  toolId?: string
  click?: MouseClickAction | string
}

export interface KeyboardPressPart {
  toolId?: string
  key?: string
}

export interface MouseMoveAndClickPart {
  toolId?: string
  xPx: number
  yPx: number
  click?: MouseClickAction | string
  method?: string
}

// FlowPart is one control-only block. Exactly one variant field is set; use
// flowPartKind() to read the active variant. The `queue` variant
// (specs/030-queued-chat-input/spec.md FR-008) carries the per-session queue
// depth pushed by the backend; see
// QueueSignal below and specs/030-queued-chat-input/contracts/queue-channel-contract.md §2.
export interface FlowPart {
  mouseMove?: MouseMovePart
  mouseClick?: MouseClickPart
  keyboardPress?: KeyboardPressPart
  mouseMoveAndClick?: MouseMoveAndClickPart
  wait?: WaitSignal
  warn?: WarnSignal
  status?: StatusSignal
  queue?: QueueSignal
}

export interface FlowParts {
  parts?: FlowPart[]
}

// Active variant of a MessagePart, or undefined for an empty/unknown part.
export type MessagePartKind = 'text' | 'thinking' | 'image' | 'toolCall' | 'toolResult'

export function messagePartKind(part: MessagePart): MessagePartKind | undefined {
  if (part.text) return 'text'
  if (part.thinking) return 'thinking'
  if (part.image) return 'image'
  if (part.toolCall) return 'toolCall'
  if (part.toolResult) return 'toolResult'
  return undefined
}

// Active variant of a FlowPart, or undefined for an empty/unknown part.
export type FlowPartKind = 'mouseMove' | 'mouseClick' | 'keyboardPress' | 'mouseMoveAndClick' | 'wait' | 'warn' | 'status' | 'queue'

export function flowPartKind(part: FlowPart): FlowPartKind | undefined {
  if (part.mouseMove) return 'mouseMove'
  if (part.mouseClick) return 'mouseClick'
  if (part.keyboardPress) return 'keyboardPress'
  if (part.mouseMoveAndClick) return 'mouseMoveAndClick'
  if (part.wait) return 'wait'
  if (part.warn) return 'warn'
  if (part.status) return 'status'
  if (part.queue) return 'queue'
  return undefined
}

// The three render states a resolved tool-result bubble resolves to. This is
// the single source of truth for tool-result status classification
// (specs/024-tool-render-coord-fix/research.md D3;
// specs/024-tool-render-coord-fix/data-model.md §1): a neutral status covers
// both an explicit TOOL_RESULT_STATUS_UNSPECIFIED and an absent status field —
// protojson omits zero-value enum fields without field presence
// (https://protobuf.dev/programming-guides/json/#presence) — and saolei/MCP
// tool results carry UNSPECIFIED (specs/023-saolei-mcp-refine C15/D12), so a
// neutral result MUST map to 'neutral', never 'failed'.
export type ToolResultStatusClass = 'succeeded' | 'failed' | 'neutral'

// classifyToolResultStatus maps a ToolResultPart.status (protojson enum-name
// string or numeric enum form) to one of the three render states. Accepts both
// forms because protojson emits the enum name by default but may emit the
// integer when the "emit enums as integers" option is set
// (https://protobuf.dev/programming-guides/json/#json-options). undefined/null/
// ''/0/"TOOL_RESULT_STATUS_UNSPECIFIED" all classify as 'neutral' so an absent
// status (the protojson default-value omission) never reads as failure
// (specs/024-tool-render-coord-fix/data-model.md §5).
export function classifyToolResultStatus(
  status: ToolResultStatus | string | undefined | null,
): ToolResultStatusClass {
  if (status == null) return 'neutral'
  if (typeof status === 'number') {
    if (status === ToolResultStatus.SUCCEEDED) return 'succeeded'
    if (status === ToolResultStatus.FAILED) return 'failed'
    return 'neutral'
  }
  switch (status) {
    case 'TOOL_RESULT_STATUS_SUCCEEDED':
      return 'succeeded'
    case 'TOOL_RESULT_STATUS_FAILED':
      return 'failed'
    default:
      return 'neutral'
  }
}

// ─── Control Signals (FlowPart kinds; never persisted to history) ──────────
// WaitSignal / WarnSignal / StatusSignal carry turn-control signals. Per the
// content-model split (spec 023 C3 / FR-003) they are FlowPart kinds, carried
// by AgentFrame.flowParts and never rendered as conversation entries.

export interface WaitSignal {
  reason?: string
}

export interface WarnSignal {
  message?: string
  code?: string
}

export type StatusSignalStatus =
  | 'STATUS_SIGNAL_STATUS_UNSPECIFIED'
  | 'STATUS_SIGNAL_STATUS_ACTIVE'
  | 'STATUS_SIGNAL_STATUS_IDLE'

export interface StatusSignal {
  status?: StatusSignalStatus
}

// QueueSignal carries the per-session queue depth pushed by the backend over
// the flow channel whenever the depth changes (event-driven, not polled). The
// desktop renders pending messages and transitions them to normal on consume
// (specs/030-queued-chat-input/spec.md FR-008/FR-009). The proto field
// `queued_count`
// (lower_snake_case per [AIP-140](https://google.aip.dev/140)) arrives as
// `queuedCount` in protojson camelCase. See
// specs/030-queued-chat-input/contracts/queue-channel-contract.md §2.
export interface QueueSignal {
  queuedCount?: number
}

// ─── TeamProfile (replaces AgentProfile; typed oneof spec, D1) ─────────────
//
// Documentation-type interface: describes the shape of the spec.saolei oneof
// variant (FR-027 — only the player/planner model choices; tools/MCP are
// template-fixed, FR-028). TeamProfile flattens playerModel/plannerModel into
// top-level fields (mirroring the Go TeamProfileView — desktop/view_model.go),
// so this interface is retained purely as typed documentation of the variant
// shape and is not referenced at runtime.
export interface SaoleiProfile {
  playerModel: string
  plannerModel: string
}

export interface TeamProfile {
  name: string
  profileName: string
  template: string
  // spec.saolei → SaoleiProfile (flattened): the Wails view model lifts the
  // oneof variant fields to the TeamProfile top level (desktop/view_model.go
  // TeamProfileView); absent when the variant is unset.
  playerModel?: string
  plannerModel?: string
  createTime?: string
  updateTime?: string
}

export interface CreateTeamProfileRequest {
  profileName: string
  playerModel?: string
  plannerModel?: string
}

export interface ListTeamProfilesResponse {
  teamProfiles: TeamProfile[]
  nextPageToken: string
}

// ─── Frame & Message Envelopes ─────────────────────────────────────────────

// AgentFrame is the transport unit exchanged over WebSocket / gRPC streams.
// A frame carries exactly one payload (protojson flattens the oneof): a batch
// of display blocks (messageParts) OR a batch of control blocks (flowParts).
// `agent` (D12) replaces the former agentProfileName: it names the team agent
// the frame belongs to (FR-023), and is the dimension frames are routed into
// per-agent tabs by (FR-025).
export interface AgentFrame {
  sessionId?: string
  frameId?: string
  createTime?: string
  sender?: FrameSender | string
  agent?: string
  messageParts?: MessageParts
  flowParts?: FlowParts
}

// Message is one normalized conversation entry reconstructed from checkpoint
// state (history), partitioned per team agent (FR-005). Its content is a
// MessageParts (display blocks only) — the identical shape a live AgentFrame's
// messageParts payload carries, so history and live view render identically
// (spec 023 FR-009). Control blocks (FlowParts) never appear here.
export interface Message {
  name?: string
  messageId?: string
  sender?: FrameSender | string
  agent?: string
  createTime?: string
  content?: MessageParts
}

export interface ChatStreamHandoff {
  endpoint: string
  token: string
  lastEventId: number
}

export interface ListSessionsResponse {
  sessions: Session[]
  nextPageToken: string
}

// ─── Operation Execution ─────────────────────────────────────────────────────

export interface WindowRef {
  handle: number
  title: string
  processID: number
  widthPx: number
  heightPx: number
  scaleFactor: number
}

export interface CapturedImage {
  data: string // base64-encoded PNG bytes (Wails serializes Go []byte as base64 string)
  widthPx: number
  heightPx: number
  encoding: string
}

// ─── Wails bindings (desktop-contract §4) ──────────────────────────────────
// Signatures mirror the Go *App methods (projects/game/desktop/app.go).
// Template-scoped methods take the Template path segment (e.g. "saolei");
// SendUserTurn/ListMessages take the team agent name (D12).

interface WailsApp {
  GetConfig(): Promise<Config>
  SetConfig(cfg: Config): Promise<void>
  CreateSession(template: string): Promise<Session>
  ListSessions(template: string, pageSize: number, pageToken: string): Promise<ListSessionsResponse>
  GetSession(template: string, sessionID: string): Promise<Session>
  DeleteSession(template: string, sessionID: string): Promise<void>
  GetTeam(template: string, sessionID: string): Promise<Team>
  CreateTeam(template: string, sessionID: string, profile: string): Promise<Team>
  ListWindows(): Promise<WindowRef[]>
  SetSelectedWindow(hwnd: number): Promise<void>
  CaptureScreenshot(): Promise<CapturedImage>
  Connect(template: string, sessionID: string): Promise<string>
  CloseAgent(): Promise<void>
  SendAgentFrame(frame: AgentFrame): Promise<AgentFrame>
  SendUserTurn(template: string, sessionID: string, text: string, screenshotData: string, screenshotWidth: number, screenshotHeight: number, agent: string): Promise<void>
  ListMessages(template: string, sessionID: string, agent: string): Promise<Message[]>
  CloseChatStream(sessionID: string): Promise<void>
  OpenChatStream(sessionID: string, agent: string): Promise<ChatStreamHandoff>

  // Prompt Service — TeamProfile CRUD (replaces AgentProfile/Skill).
  ListTeamProfiles(template: string, pageSize: number, pageToken: string): Promise<ListTeamProfilesResponse>
  CreateTeamProfile(template: string, req: CreateTeamProfileRequest): Promise<TeamProfile>
  GetTeamProfile(template: string, profileName: string): Promise<TeamProfile>
  UpdateTeamProfile(template: string, profileName: string, profile: TeamProfile, updateMaskPaths: string[]): Promise<TeamProfile>
  DeleteTeamProfile(template: string, profileName: string): Promise<void>

  RefreshTeam(template: string, sessionID: string): Promise<void>

  // Debug control plane — desktop debug mode. The Go bound methods are added to
  // *App in their story phases (T005 SetDebugMode, T010 ConfirmToolResult); this
  // binding surface is declared ahead of time so the US1/US2 frontend typechecks
  // against it. See specs/022-desktop-debug-mode/contracts/debug-control-plane.md §1.
  SetDebugMode(enabled: boolean): Promise<void>
  ConfirmToolResult(toolID: string): Promise<void>
}

function app(): WailsApp | undefined {
  return window.go?.main?.App
}

export async function getConfig(): Promise<Config> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetConfig()
}

export async function setConfig(cfg: Config): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SetConfig(cfg)
}

export async function createSession(template: string): Promise<Session> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CreateSession(template)
}

export async function listSessions(template: string, pageSize: number, pageToken: string): Promise<ListSessionsResponse> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListSessions(template, pageSize, pageToken)
}

export async function getSession(template: string, sessionID: string): Promise<Session> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetSession(template, sessionID)
}

export async function deleteSession(template: string, sessionID: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.DeleteSession(template, sessionID)
}

export async function getTeam(template: string, sessionID: string): Promise<Team> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetTeam(template, sessionID)
}

// createTeam explicitly creates the per-session singleton Team (AIP-133 —
// the ONLY Team creation point, FR-033). profile is the TeamProfile full
// resource name (templates/{template}/profiles/{profile}); repeated create
// with the same profile is idempotent (api-contract §2.2 idempotency note).
export async function createTeam(template: string, sessionID: string, profile: string): Promise<Team> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CreateTeam(template, sessionID, profile)
}

/** @deprecated Use chat-based interfaces instead. */
export async function listWindows(): Promise<WindowRef[]> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListWindows()
}

export async function setSelectedWindow(hwnd: number): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SetSelectedWindow(hwnd)
}

export async function captureScreenshot(): Promise<CapturedImage> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CaptureScreenshot()
}

export async function connect(template: string, sessionID: string): Promise<string> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.Connect(template, sessionID)
}

export async function closeAgent(): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CloseAgent()
}

export async function sendAgentFrame(frame: AgentFrame): Promise<AgentFrame> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SendAgentFrame(frame)
}

// sendUserTurn routes the user turn to the named team agent (the agent
// accepting user input — FR-032; saolei: player). agent replaces the former
// agentProfileName (D12).
export async function sendUserTurn(
  template: string,
  sessionID: string,
  text: string,
  screenshotData: string,
  screenshotWidth: number,
  screenshotHeight: number,
  agent: string,
): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SendUserTurn(template, sessionID, text, screenshotData, screenshotWidth, screenshotHeight, agent)
}

// listMessages lists one team agent's message partition (FR-005).
export async function listMessages(template: string, sessionId: string, agent: string): Promise<Message[]> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListMessages(template, sessionId, agent)
}

export async function closeChatStream(sessionId: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CloseChatStream(sessionId)
}

// openChatStream opens the chat push channel seeded with one team agent's
// message partition. The Go chatstream Registry is per-session (single stream,
// RotateToken on every open — desktop/internal/chatstream/stream.go), so the
// frontend opens ONE stream per session (seeded by the first team agent) and
// routes inbound frames by AgentFrame.agent.
export async function openChatStream(sessionId: string, agent: string): Promise<ChatStreamHandoff> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.OpenChatStream(sessionId, agent)
}

// ─── Prompt Service Wrappers ───────────────────────────────────────────────

export async function listTeamProfiles(template: string, pageSize: number, pageToken: string): Promise<ListTeamProfilesResponse> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListTeamProfiles(template, pageSize, pageToken)
}

export async function createTeamProfile(template: string, req: CreateTeamProfileRequest): Promise<TeamProfile> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CreateTeamProfile(template, req)
}

export async function getTeamProfile(template: string, profileName: string): Promise<TeamProfile> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetTeamProfile(template, profileName)
}

export async function updateTeamProfile(template: string, profileName: string, profile: TeamProfile, updateMaskPaths: string[]): Promise<TeamProfile> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.UpdateTeamProfile(template, profileName, profile, updateMaskPaths)
}

export async function deleteTeamProfile(template: string, profileName: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.DeleteTeamProfile(template, profileName)
}

export async function refreshTeam(template: string, sessionID: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.RefreshTeam(template, sessionID)
}

// ─── Debug Control Plane Wrappers ──────────────────────────────────────────
// Desktop debug-mode toggle + held-tool-result confirm. Contract:
// specs/022-desktop-debug-mode/contracts/debug-control-plane.md §1.
// In feature 023 the held payload is extended with an operation descriptor
// (specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md §2) and the
// Confirm control moves to a session-top drawer; the method/event names are
// unchanged.

// A held operation awaiting user confirmation, surfaced in the session-top
// drawer. toolId is the operation-channel id (bridge-minted, NOT the
// conversation tool_call.id — research.md D10/D11). kind/summary/details are
// built by the Go backend from the FlowPart so the drawer needs no proto
// knowledge (contracts/debug-drawer-contract.md §2).
export interface HeldOperation {
  toolId: string
  kind: string
  summary: string
  details: Record<string, unknown>
}

// `game:debug:result-held` payload (023-extended). `toolId` is retained from
// 022 (additive change — contracts/debug-drawer-contract.md §7); `operation`
// carries the request content for the drawer.
export interface DebugResultHeldPayload {
  toolId: string
  operation: {
    kind: string
    summary: string
    details: Record<string, unknown>
  }
}

// `game:debug:result-released` payload (unchanged from 022).
export interface DebugResultReleasedPayload {
  toolId: string
  reason: 'confirmed' | 'timeout' | 'debug-off' | 'shutdown'
}

export async function setDebugMode(enabled: boolean): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SetDebugMode(enabled)
}

export async function confirmToolResult(toolID: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ConfirmToolResult(toolID)
}
