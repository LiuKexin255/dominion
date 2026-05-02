package app

// WindowRef is a lightweight reference to a bound window for status snapshots.
type WindowRef struct {
	HWND  uintptr `json:"hwnd"`
	Title string  `json:"title"`
}

// WindowRect represents a window's bounding rectangle.
type WindowRect struct {
	Left   int32 `json:"left"`
	Top    int32 `json:"top"`
	Right  int32 `json:"right"`
	Bottom int32 `json:"bottom"`
}

// WindowDetail extends WindowRef with additional window metadata.
type WindowDetail struct {
	WindowRef
	ClassName string     `json:"className"`
	ProcessID int32      `json:"processId"`
	Rect      WindowRect `json:"rect"`
}

// AgentStatus is a point-in-time snapshot of the agent state sent to the frontend.
type AgentStatus struct {
	State              string        `json:"state"`
	SessionID          string        `json:"sessionId"`
	BoundWindow        *WindowDetail `json:"boundWindow"`
	MediaSegCount      int64         `json:"mediaSegCount"`
	LastError          string        `json:"lastError"`
	FFmpegRunning      bool          `json:"ffmpegRunning"`
	HelperRunning      bool          `json:"helperRunning"`
	ConnectedAt        string        `json:"connectedAt"`
	SessionName        string        `json:"sessionName"`
	SessionType        string        `json:"sessionType"`
	GatewayID          string        `json:"gatewayId"`
	StreamingStartedAt string        `json:"streamingStartedAt"`

	// SessionServiceState reflects the reachability of the session service API.
	// Values: "unknown", "ok", "error".
	SessionServiceState string `json:"sessionServiceState"`
	// SessionServiceError contains the last error from the session service, if any.
	SessionServiceError string `json:"sessionServiceError"`
}
