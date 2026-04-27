package app

// WindowRef is a lightweight reference to a bound window for status snapshots.
type WindowRef struct {
	HWND  uintptr `json:"hwnd"`
	Title string  `json:"title"`
}

// AgentStatus is a point-in-time snapshot of the agent state sent to the frontend.
type AgentStatus struct {
	State         string     `json:"state"`
	SessionID     string     `json:"sessionId"`
	BoundWindow   *WindowRef `json:"boundWindow"`
	MediaSegCount int64      `json:"mediaSegCount"`
	LastError     string     `json:"lastError"`
	FFmpegRunning bool       `json:"ffmpegRunning"`
	HelperRunning bool       `json:"helperRunning"`
	ConnectedAt   string     `json:"connectedAt"`
}
