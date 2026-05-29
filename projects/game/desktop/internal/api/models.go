package api

// Config holds the desktop client configuration.
type Config struct {
	GatewayURL string `json:"gateway_url"`
	Env        string `json:"env,omitempty"`
}

// Session represents a game session from the gateway.
type Session struct {
	Name       string `json:"name"`
	SessionID  string `json:"session_id"`
	CreateTime string `json:"create_time"`
}

// Agent represents a game agent from the gateway.
type Agent struct {
	Name       string `json:"name"`
	SessionID  string `json:"session_id"`
	OwnerIndex int32  `json:"owner_index"`
	Owner      string `json:"owner"`
	CreateTime string `json:"create_time"`
}

// AgentFrame is a WebSocket frame for agent communication.
// Payload is a base64-encoded string (protojson bytes → base64).
type AgentFrame struct {
	SessionID string `json:"session_id"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
}

// LogEntry represents a single log entry for the UI log display.
// Source is either "frontend" or "backend".
type LogEntry struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Source  string         `json:"source"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// CheckResult is the result of a full connectivity check.
type CheckResult struct {
	Success bool     `json:"success"`
	Steps   []string `json:"steps"`
	Error   string   `json:"error,omitempty"`
}
