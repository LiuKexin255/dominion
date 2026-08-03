export interface LogEntry {
  time: string
  level: string
  source: string
  message: string
  fields?: Record<string, unknown>
}

// Global log entries array — shared with App.svelte via state
// App.svelte manages its own $state array; this is the bridge
let logSink: ((entry: LogEntry) => void) | null = null

// Debug-level gating flag (default off, FR-003). When false, logDebug is a
// no-op (no sink push, no console write), so production pays nothing.
// See specs/022-desktop-debug-mode/research.md D5.
let debugEnabled = false

export function setLogSink(fn: (entry: LogEntry) => void) {
  logSink = fn
}

// setDebugEnabled toggles frontend debug-level emission (FR-001/FR-005).
// Driven by the Debug switch in App.svelte.
export function setDebugEnabled(enabled: boolean): void {
  debugEnabled = enabled
}

export function log(level: string, source: string, message: string, fields?: Record<string, unknown>) {
  const entry: LogEntry = {
    time: new Date().toISOString(),
    level,
    source,
    message,
    ...(fields ? { fields } : {})
  }

  // Push to UI log state
  if (logSink) {
    logSink(entry)
  }

  // Also log to browser console in development
  const consoleFn = level === 'error' ? console.error : console.log
  consoleFn(`[${source}] ${message}`, fields || '')
}

// logDebug emits a debug-level entry only while debug mode is enabled. When
// disabled it short-circuits before touching the sink or console, so it is safe
// to call on hot paths (inbound-frame handling, FR-004 frontend).
// See specs/022-desktop-debug-mode/research.md D5.
export function logDebug(source: string, message: string, fields?: Record<string, unknown>): void {
  if (!debugEnabled) return
  log('debug', source, message, fields)
}
