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
  session_id: string
  create_time: string
}

export interface Agent {
  name: string
  session_id: string
  owner_index: number
  owner: string
  create_time: string
}

export interface AgentFrame {
  session_id: string
  type: string
  payload: string
}

export interface LogEntry {
  time: string
  level: string
  source: string
  message: string
  fields?: Record<string, string>
}

export interface CheckResult {
  success: boolean
  steps: string[]
  error?: string
}

interface WailsApp {
  GetConfig(): Promise<Config>
  SetConfig(cfg: Config): Promise<void>
  CreateSession(sessionID: string): Promise<Session>
  GetSession(sessionID: string): Promise<Session>
  DeleteSession(sessionID: string): Promise<void>
  CreateAgent(sessionID: string): Promise<Agent>
  GetAgent(sessionID: string): Promise<Agent>
  DeleteAgent(sessionID: string): Promise<void>
  ConnectAgent(sessionID: string): Promise<void>
  SendAgentFrame(frame: AgentFrame): Promise<AgentFrame>
  CloseAgent(): Promise<void>
  RunConnectivityCheck(sessionID: string): Promise<CheckResult>
  Logs(): Promise<LogEntry[]>
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

export async function createSession(sessionID: string): Promise<Session> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CreateSession(sessionID)
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

export async function closeAgent(): Promise<void> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.CloseAgent()
}

export async function runConnectivityCheck(sessionID: string): Promise<CheckResult> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.RunConnectivityCheck(sessionID)
}

export async function logs(): Promise<LogEntry[]> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.Logs()
}
