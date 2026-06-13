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
  name: string
  sessionId: string
  createTime: string
}

// ─── Enums ──────────────────────────────────────────────────────────────────

export enum AgentMouseButton {
  LEFT = 1,
  RIGHT = 2,
}

export enum AgentMouseClickType {
  SINGLE = 1,
  DOUBLE = 2,
}

export enum FrameSender {
  UNSPECIFIED = 0,
  USER = 1,
  AGENT = 2,
  SYSTEM = 3,
}

// ─── Frame Types ───────────────────────────────────────────────────────────

export interface AgentStatusFrame {
  status: string
}

export interface AgentEchoFrame {
  data: string // base64-encoded bytes
}

export interface AgentAckFrame {
  ackFrameId: string
  message: string
}

export interface AgentTextFrame {
  content: string
}

export interface AgentThinkingFrame {
  content: string
}

export interface AgentWarnFrame {
  message: string
  code: string
}

export interface AgentMouseOperation {
  button: AgentMouseButton
  clickType: AgentMouseClickType
  xPx: number
  yPx: number
}

export interface AgentKeyboardOperation {
  keyCodes: string
}

export interface AgentOperationFrame {
  operationId: string
  screenshotId: string
  sequence: number
  mouse?: AgentMouseOperation
  keyboard?: AgentKeyboardOperation
}

export interface AgentProfile {
  name: string
  agentProfileName: string
  model: string
  systemPrompt: string
  skillNames: string[]
  mcpNames: string[]
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

export interface AgentScreenshotFrame {
  captureId: string
  encoding: string
  data: string // base64-encoded bytes
  widthPx: number
  heightPx: number
  scaleFactor: number
  windowTitle: string
  captureTime: string
}

export interface AgentFrame {
  sessionId: string
  frameId: string
  createTime: string
  invokeId?: string
  sequence?: number
  status?: AgentStatusFrame
  echo?: AgentEchoFrame
  screenshot?: AgentScreenshotFrame
  ack?: AgentAckFrame
  text?: AgentTextFrame
  thinking?: AgentThinkingFrame
  operation?: AgentOperationFrame
  warn?: AgentWarnFrame
  sender: FrameSender
}

export interface ListSessionsResponse {
  sessions: Session[]
  nextPageToken: string
}

// ─── Operation Execution ─────────────────────────────────────────────────────

export interface OperationResultView {
  operationId: string
  sequence: number
  status: number
  message: string
}

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
  enabled?: boolean
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
  CreateAgent(sessionID: string): Promise<Agent>
  CreateAgentWithProfile(sessionID: string, profileName: string): Promise<Agent>
  GetAgent(sessionID: string): Promise<Agent>
  DeleteAgent(sessionID: string): Promise<void>
  /** @deprecated Use chat-based interfaces instead. */
  ListWindows(): Promise<WindowRef[]>
  /** @deprecated Use chat-based interfaces instead. */
  BindWindow(hwnd: number): Promise<void>
  /** @deprecated Use chat-based interfaces instead. */
  CaptureScreenshot(): Promise<CapturedImage>
  /** @deprecated Use chat-based interfaces instead. */
  SendScreenshot(hwnd: number): Promise<AgentAckFrame>
  ConnectAgent(sessionID: string): Promise<void>
  CloseAgent(): Promise<void>
  SendAgentFrame(frame: AgentFrame): Promise<AgentFrame>
  SendAgentText(sessionId: string, text: string): Promise<AgentFrame>
  /** @deprecated Use chat-based interfaces instead. */
  ExecuteOperation(
    operationID: string, screenshotID: string, sequence: number,
    button: number, clickType: number, xPx: number, yPx: number,
    isMouse: boolean, keyCodes: string,
    windowLeft: number, windowTop: number
  ): Promise<OperationResultView>
  /** @deprecated Use chat-based interfaces instead. */
  SendNextScreenshot(): Promise<void>

  // Prompt Service
  CreateAgentProfile(req: CreateAgentProfileRequest): Promise<AgentProfile>
  GetAgentProfile(agentProfileName: string): Promise<AgentProfile>
  ListAgentProfiles(pageSize: number, pageToken: string): Promise<ListAgentProfilesResponse>
  DeleteAgentProfile(agentProfileName: string): Promise<void>
  CreateSkill(req: CreateSkillRequest): Promise<Skill>
  GetSkill(skillName: string): Promise<Skill>
  ListSkills(pageSize: number, pageToken: string): Promise<ListSkillsResponse>
  DeleteSkill(skillName: string): Promise<void>
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

export async function createAgent(sessionID: string): Promise<Agent> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CreateAgent(sessionID)
}

export async function createAgentWithProfile(sessionID: string, profileName: string): Promise<Agent> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CreateAgentWithProfile(sessionID, profileName)
}

export async function getAgent(sessionID: string): Promise<Agent> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetAgent(sessionID)
}

export async function deleteAgent(sessionID: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.DeleteAgent(sessionID)
}

/** @deprecated Use chat-based interfaces instead. */
export async function listWindows(): Promise<WindowRef[]> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListWindows()
}

/** @deprecated Use chat-based interfaces instead. */
export async function bindWindow(hwnd: number): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.BindWindow(hwnd)
}

/** @deprecated Use chat-based interfaces instead. */
export async function captureScreenshot(): Promise<CapturedImage> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CaptureScreenshot()
}

/** @deprecated Use chat-based interfaces instead. */
export async function sendScreenshot(hwnd: number): Promise<AgentAckFrame> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SendScreenshot(hwnd)
}

export async function connectAgent(sessionID: string): Promise<void> {
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

export async function sendAgentText(sessionId: string, text: string): Promise<AgentFrame> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SendAgentText(sessionId, text)
}

/** @deprecated Use chat-based interfaces instead. */
export async function executeOperation(
  operationID: string, screenshotID: string, sequence: number,
  button: number, clickType: number, xPx: number, yPx: number,
  isMouse: boolean, keyCodes: string,
  windowLeft: number, windowTop: number
): Promise<OperationResultView> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ExecuteOperation(operationID, screenshotID, sequence, button, clickType, xPx, yPx, isMouse, keyCodes, windowLeft, windowTop)
}

/** @deprecated Use chat-based interfaces instead. */
export async function sendNextScreenshot(): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SendNextScreenshot()
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
