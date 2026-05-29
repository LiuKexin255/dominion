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
  ownerIndex: number
  owner: string
  createTime: string
}

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
  status?: AgentStatusFrame
  echo?: AgentEchoFrame
  screenshot?: AgentScreenshotFrame
  ack?: AgentAckFrame
}

export interface ListSessionsResponse {
  sessions: Session[]
  nextPageToken: string
}

export interface AgentStatus {
  sessionId: string
  status: string
  createTime: string
}

export interface WindowRef {
  handle: number
  title: string
  processID: number
  clientWidthPx: number
  clientHeightPx: number
  scaleFactor: number
}

export interface CapturedImage {
  data: number[]
  widthPx: number
  heightPx: number
  encoding: string
}

interface WailsApp {
  GetConfig(): Promise<Config>
  SetConfig(cfg: Config): Promise<void>
  CreateSession(): Promise<Session>
  ListSessions(pageSize: number, pageToken: string): Promise<ListSessionsResponse>
  GetSession(sessionID: string): Promise<Session>
  DeleteSession(sessionID: string): Promise<void>
  CreateAgent(sessionID: string): Promise<Agent>
  GetAgent(sessionID: string): Promise<Agent>
  DeleteAgent(sessionID: string): Promise<void>
  ListWindows(): Promise<WindowRef[]>
  BindWindow(hwnd: number): Promise<void>
  CaptureScreenshot(): Promise<CapturedImage>
  SendScreenshot(hwnd: number): Promise<AgentAckFrame>
  GetAgentStatus(sessionID: string): Promise<AgentStatus>
  ConnectAgent(sessionID: string): Promise<void>
  SendAgentFrame(frame: AgentFrame): Promise<AgentFrame>
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

export async function sendScreenshot(hwnd: number): Promise<AgentAckFrame> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SendScreenshot(hwnd)
}

export async function getAgentStatus(sessionID: string): Promise<AgentStatus> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetAgentStatus(sessionID)
}

export async function connectAgent(sessionID: string): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ConnectAgent(sessionID)
}

export async function sendAgentFrame(frame: AgentFrame): Promise<AgentFrame> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.SendAgentFrame(frame)
}
