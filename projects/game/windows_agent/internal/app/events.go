package app

// LogEntry is a structured log entry sent to the frontend.
type LogEntry struct {
	Timestamp string            `json:"timestamp"`
	Level     string            `json:"level"`
	Module    string            `json:"module"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields"`
}

// Wails event names for frontend communication.
const (
	EventStatusChanged = "status:changed"
	EventWindowList    = "window:list"
	EventMediaInit     = "media:init"
	EventMediaSegment  = "media:segment"
	EventErrorOccurred = "error:occurred"
	EventLogEntry      = "log:entry"
)

// emitEvent sends a named event with optional data to the Wails frontend.
func (a *App) emitEvent(name string, data interface{}) {
	if a.ctx == nil || a.emitFunc == nil {
		return
	}
	a.emitFunc(a.ctx, name, data)
}

// emitStatusChanged emits the status:changed event with the current status snapshot.
func (a *App) emitStatusChanged() {
	a.mu.RLock()
	status := a.status
	a.mu.RUnlock()
	a.emitEvent(EventStatusChanged, status)
}

// EmitLog emits a diagnostic log entry to the Wails frontend.
func (a *App) EmitLog(level, message string) {
	a.emitEvent(EventLogEntry, map[string]string{"level": level, "message": message})
}

// EmitStructuredLog emits a structured log entry to the Wails frontend.
func (a *App) EmitStructuredLog(entry LogEntry) {
	a.emitEvent(EventLogEntry, entry)
}
