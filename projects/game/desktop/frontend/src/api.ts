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
  createTime: string
}

// ─── Enums ──────────────────────────────────────────────────────────────────

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

// ─── FlowPart Model (control blocks) ────────────────────────────────────────
//
// FlowPart is the control-only content category (specs/023-saolei-mcp-refine/
// contracts/content-model-contract.md §1..§6; spec 023 C3): mouse/keyboard
// operations plus wait/warn/status/queue signals, carried by the flow
// channel's flowParts frame payload. protojson flattens each oneof so exactly
// one variant field is set (the field name is the discriminator).

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
// the flow channel (specs/030-queued-chat-input/contracts/
// queue-channel-contract.md §2). The proto field `queued_count`
// (lower_snake_case per [AIP-140](https://google.aip.dev/140)) arrives as
// `queuedCount` in protojson camelCase.
export interface QueueSignal {
  queuedCount?: number
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
  queue?: QueueSignal
}

export interface FlowParts {
  parts?: FlowPart[]
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

// ─── Session List ───────────────────────────────────────────────────────────

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

// ─── Wails bindings ─────────────────────────────────────────────────────────
// Signatures mirror the Go *App methods (projects/game/desktop/app.go).
// Template-scoped methods take the Template path segment (e.g. "saolei").

interface WailsApp {
  GetConfig(): Promise<Config>
  SetConfig(cfg: Config): Promise<void>
  ListSessions(template: string, pageSize: number, pageToken: string): Promise<ListSessionsResponse>
  ListWindows(): Promise<WindowRef[]>
  SetSelectedWindow(hwnd: number): Promise<void>
  GetSelectedWindow(): Promise<number>
  CaptureScreenshot(): Promise<CapturedImage>
  Connect(template: string, sessionID: string): Promise<string>
  CloseAgent(): Promise<void>

  // Debug control plane — desktop debug mode. The Go bound methods are
  // SetDebugMode/ConfirmToolResult
  // (specs/022-desktop-debug-mode/contracts/debug-control-plane.md §1).
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

export async function listSessions(template: string, pageSize: number, pageToken: string): Promise<ListSessionsResponse> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.ListSessions(template, pageSize, pageToken)
}

// listWindows enumerates the windows offered by the session window picker.
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

export async function getSelectedWindow(): Promise<number> {
  const a = app()
  if (!a) throw new Error('Wails runtime not available')
  return a.GetSelectedWindow()
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

// ─── Debug Control Plane Wrappers ──────────────────────────────────────────
// Desktop debug-mode toggle + held-tool-result confirm. Contract:
// specs/022-desktop-debug-mode/contracts/debug-control-plane.md §1.
// The held payload is extended with an operation descriptor
// (specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md §2) and the
// Confirm control lives in a session-top drawer; the method/event names are
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

// `game:debug:result-held` payload (023-extended). `operation` carries the
// request content for the drawer (contracts/debug-drawer-contract.md §2).
export interface DebugResultHeldPayload {
  toolId: string
  operation: {
    kind: string
    summary: string
    details: Record<string, unknown>
  }
}

// `game:debug:result-released` payload.
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
