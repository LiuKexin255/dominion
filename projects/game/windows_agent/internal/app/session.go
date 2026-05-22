package app

// Session represents a remote agent session exposed to the Wails frontend.
// JSON field names follow protojson conventions (camelCase, int64 as string).
type Session struct {
	Name                string `json:"name"`
	Type                string `json:"type"`
	Status              string `json:"status"`
	RuntimeID           string `json:"runtimeId"`
	AgentConnectURL     string `json:"agentConnectUrl"`
	CreateTime          string `json:"createTime"`
	UpdateTime          string `json:"updateTime"`
	ReconnectGeneration string `json:"reconnectGeneration"`
	LastError           string `json:"lastError"`
}

// ScreenshotResult represents the result of a screen capture operation.
type ScreenshotResult struct {
	ImageURL    string `json:"imageURL"`
	MimeType    string `json:"mimeType"`
	SnapshotID  string `json:"snapshotID"`
	CaptureTime string `json:"captureTime"`
	SessionName string `json:"sessionName"`
	RuntimeID   string `json:"runtimeID"`
	Error       string `json:"error"`
}
