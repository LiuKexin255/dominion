export interface LogEntry {
  time: string
  level: string
  source: string
  message: string
  fields?: Record<string, string>
}

// Global log entries array — shared with App.svelte via state
// App.svelte manages its own $state array; this is the bridge
let logSink: ((entry: LogEntry) => void) | null = null

export function setLogSink(fn: (entry: LogEntry) => void) {
  logSink = fn
}

export function log(level: string, source: string, message: string, fields?: Record<string, string>) {
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
