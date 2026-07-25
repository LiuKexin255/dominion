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

export interface Config {
  gateway_url: string
  env: string
}

export interface Session {
  name: string
  sessionId: string
  createTime: string
}

export interface Agent {
  sessionId: string
  createTime?: string
  agentProfileName: string
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
// (spec 023 FR-002). tool_id links the call to its tool_result MessagePart and
// to the FlowPart operation it dispatches (FR-008).
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
// flowPartKind() to read the active variant.
export interface FlowPart {
  mouseMove?: MouseMovePart
  mouseClick?: MouseClickPart
  keyboardPress?: KeyboardPressPart
  mouseMoveAndClick?: MouseMoveAndClickPart
  wait?: WaitSignal
  warn?: WarnSignal
  status?: StatusSignal
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
export type FlowPartKind = 'mouseMove' | 'mouseClick' | 'keyboardPress' | 'mouseMoveAndClick' | 'wait' | 'warn' | 'status'

export function flowPartKind(part: FlowPart): FlowPartKind | undefined {
  if (part.mouseMove) return 'mouseMove'
  if (part.mouseClick) return 'mouseClick'
  if (part.keyboardPress) return 'keyboardPress'
  if (part.mouseMoveAndClick) return 'mouseMoveAndClick'
  if (part.wait) return 'wait'
  if (part.warn) return 'warn'
  if (part.status) return 'status'
  return undefined
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

export interface AgentProfile {
  name: string
  agentProfileName: string
  model: string
  systemPrompt: string
  skillNames: string[]
  mcpNames: string[]
  toolNames: string[]
  enabled: boolean
  createTime?: string
  updateTime?: string
}

export interface Skill {
  name: string
  skillName: string
  content: string
  enabled: boolean
  createTime?: string
  updateTime?: string
}

// ─── Frame & Message Envelopes ─────────────────────────────────────────────

// AgentFrame is the transport unit exchanged over WebSocket / gRPC streams.
// A frame carries exactly one payload (protojson flattens the oneof): a batch
// of display blocks (messageParts) OR a batch of control blocks (flowParts).
export interface AgentFrame {
  sessionId?: string
  frameId?: string
  createTime?: string
  sender?: FrameSender | string
  agentProfileName?: string
  messageParts?: MessageParts
  flowParts?: FlowParts
}

// Message is one normalized conversation entry reconstructed from checkpoint
// state (history). Its content is a MessageParts (display blocks only) — the
// identical shape a live AgentFrame's messageParts payload carries, so history
// and live view render identically (spec 023 FR-009). Control blocks
// (FlowParts) never appear here.
export interface Message {
  name?: string
  messageId?: string
  sender?: FrameSender | string
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

// ─── Prompt Service Types ──────────────────────────────────────────────────

export interface CreateAgentProfileRequest {
  agentProfileName: string
  model?: string
  systemPrompt?: string
  skillNames?: string[]
  mcpNames?: string[]
  toolNames?: string[]
  enabled?: boolean
}

export interface UpdateAgentProfileRequest {
  agentProfileName: string
  agentProfile: AgentProfile
  updateMask?: string[]
}

export interface ListAgentProfilesResponse {
  agentProfiles: AgentProfile[]
  nextPageToken: string
}

export interface CreateSkillRequest {
  skillName: string
  content?: string
  enabled?: boolean
}

export interface ListSkillsResponse {
  skills: Skill[]
  nextPageToken: string
}

interface WailsApp {
  GetConfig(): Promise<Config>
  SetConfig(cfg: Config): Promise<void>
  CreateSession(): Promise<Session>
  ListSessions(pageSize: number, pageToken: string): Promise<ListSessionsResponse>
  GetSession(sessionID: string): Promise<Session>
  DeleteSession(sessionID: string): Promise<void>
  GetAgent(sessionID: string): Promise<Agent>
  ListWindows(): Promise<WindowRef[]>
  BindWindow(hwnd: number): Promise<void>
  CaptureScreenshot(): Promise<CapturedImage>
  ConnectAgent(sessionID: string): Promise<string>
  CloseAgent(): Promise<void>
  SendAgentFrame(frame: AgentFrame): Promise<AgentFrame>
  SendUserTurn(sessionID: string, text: string, screenshotData: string, screenshotWidth: number, screenshotHeight: number, agentProfileName: string): Promise<void>
  ListMessages(sessionID: string): Promise<Message[]>
  CloseChatStream(sessionID: string): Promise<void>
  OpenChatStream(sessionID: string): Promise<ChatStreamHandoff>

  // Prompt Service
  CreateAgentProfile(req: CreateAgentProfileRequest): Promise<AgentProfile>
  GetAgentProfile(agentProfileName: string): Promise<AgentProfile>
  ListAgentProfiles(pageSize: number, pageToken: string): Promise<ListAgentProfilesResponse>
  UpdateAgentProfile(agentProfileName: string, profile: AgentProfile, updateMaskPaths: string[]): Promise<AgentProfile>
  DeleteAgentProfile(agentProfileName: string): Promise<void>
  CreateSkill(req: CreateSkillRequest): Promise<Skill>
  GetSkill(skillName: string): Promise<Skill>
  ListSkills(pageSize: number, pageToken: string): Promise<ListSkillsResponse>
  DeleteSkill(skillName: string): Promise<void>

  RefreshAgent(sessionID: string): Promise<void>

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

export async function createSession(): Promise<Session> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CreateSession()
}

export async function listSessions(pageSize: number, pageToken: string): Promise<ListSessionsResponse> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListSessions(pageSize, pageToken)
}

export async function getSession(sessionID: string): Promise<Session> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetSession(sessionID)
}

export async function deleteSession(sessionID: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.DeleteSession(sessionID)
}

export async function getAgent(sessionID: string): Promise<Agent> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetAgent(sessionID)
}

/** @deprecated Use chat-based interfaces instead. */
export async function listWindows(): Promise<WindowRef[]> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListWindows()
}

export async function bindWindow(hwnd: number): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.BindWindow(hwnd)
}

export async function captureScreenshot(): Promise<CapturedImage> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CaptureScreenshot()
}

export async function connectAgent(sessionID: string): Promise<string> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ConnectAgent(sessionID)
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

export async function sendUserTurn(
  sessionID: string,
  text: string,
  screenshotData: string,
  screenshotWidth: number,
  screenshotHeight: number,
  agentProfileName: string,
): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SendUserTurn(sessionID, text, screenshotData, screenshotWidth, screenshotHeight, agentProfileName)
}

export async function listMessages(sessionId: string): Promise<Message[]> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListMessages(sessionId)
}

export async function closeChatStream(sessionId: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CloseChatStream(sessionId)
}

export async function openChatStream(sessionId: string): Promise<ChatStreamHandoff> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.OpenChatStream(sessionId)
}

// ─── Prompt Service Wrappers ───────────────────────────────────────────────

export async function createAgentProfile(req: CreateAgentProfileRequest): Promise<AgentProfile> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CreateAgentProfile(req)
}

export async function getAgentProfile(agentProfileName: string): Promise<AgentProfile> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetAgentProfile(agentProfileName)
}

export async function listAgentProfiles(pageSize: number, pageToken: string): Promise<ListAgentProfilesResponse> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListAgentProfiles(pageSize, pageToken)
}

export async function deleteAgentProfile(agentProfileName: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.DeleteAgentProfile(agentProfileName)
}

export async function createSkill(req: CreateSkillRequest): Promise<Skill> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CreateSkill(req)
}

export async function getSkill(skillName: string): Promise<Skill> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetSkill(skillName)
}

export async function listSkills(pageSize: number, pageToken: string): Promise<ListSkillsResponse> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListSkills(pageSize, pageToken)
}

export async function deleteSkill(skillName: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.DeleteSkill(skillName)
}

export async function updateAgentProfile(agentProfileName: string, profile: AgentProfile, updateMaskPaths: string[]): Promise<AgentProfile> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.UpdateAgentProfile(agentProfileName, profile, updateMaskPaths)
}

export async function refreshAgent(sessionID: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.RefreshAgent(sessionID)
}

// ─── Debug Control Plane Wrappers ──────────────────────────────────────────
// Desktop debug-mode toggle + held-tool-result confirm. Contract:
// specs/022-desktop-debug-mode/contracts/debug-control-plane.md §1.

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
