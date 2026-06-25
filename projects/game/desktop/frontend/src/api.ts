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

export interface MessageEntry {
  name: string
  messageId: string
  sender: string
  type: string
  content: string
  imageData?: string
  createTime?: string
}

// ─── Enums ──────────────────────────────────────────────────────────────────

export enum AgentMouseAction {
  UNSPECIFIED = 0,
  LEFT_CLICK = 1,
  LEFT_DOUBLE_CLICK = 2,
  RIGHT_CLICK = 3,
  RIGHT_DOUBLE_CLICK = 4,
  LEFT_RIGHT_PRESS = 5,
  MOVE = 6,
}

export enum AgentOperationResultStatus {
  UNSPECIFIED = 0,
  SUCCEEDED = 1,
  FAILED = 2,
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
  action: AgentMouseAction
  xPx: number
  yPx: number
}

export interface AgentKeyboardOperation {
  keyCodes: string
}

export interface AgentOperationFrame {
  operationId: string
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

export interface AgentImageFrame {
  encoding: string
  data: string // base64-encoded bytes
  widthPx: number
  heightPx: number
  scaleFactor: number
  windowTitle: string
}

export interface AgentWaitFrame {
  reason?: string
}

export interface AgentOperationResultFrame {
  operationId: string
  status: number
  message: string
  screenshot?: {
    data?: string
    widthPx?: number
    heightPx?: number
    encoding?: string
    scaleFactor?: number
    windowTitle?: string
  }
}

export interface AgentUserTurnFrame {
  text?: string
  image?: AgentImageFrame
}

export interface AgentFrame {
  sessionId: string
  frameId: string
  createTime: string
  invokeId?: string
  sequence?: number
  status?: AgentStatusFrame
  echo?: AgentEchoFrame
  ack?: AgentAckFrame
  text?: AgentTextFrame
  thinking?: AgentThinkingFrame
  operation?: AgentOperationFrame
  warn?: AgentWarnFrame
  wait?: AgentWaitFrame
  operationResult?: AgentOperationResultFrame
  userTurn?: AgentUserTurnFrame
  sender: FrameSender
  agentProfileName?: string
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
  profile: AgentProfile
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
  ConnectAgent(sessionID: string): Promise<void>
  CloseAgent(): Promise<void>
  SendAgentFrame(frame: AgentFrame): Promise<AgentFrame>
  SendUserTurn(sessionID: string, text: string, screenshotData: string, screenshotWidth: number, screenshotHeight: number, agentProfileName: string): Promise<void>
  ListMessages(sessionID: string): Promise<MessageEntry[]>

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

export async function listMessages(sessionId: string): Promise<MessageEntry[]> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListMessages(sessionId)
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
