package domain

// ClientRole represents the role of a WebSocket client.
type ClientRole int

const (
	// ClientRoleUnspecified is the zero value for ClientRole.
	ClientRoleUnspecified ClientRole = 0
	// ClientRoleWindowsAgent represents the agent running on the Windows host.
	ClientRoleWindowsAgent ClientRole = 1
	// ClientRoleWeb represents a web browser client.
	ClientRoleWeb ClientRole = 2
)

// Message represents a WebSocket message (domain equivalent of
// GameWebSocketEnvelope).
type Message struct {
	SessionID string
	MessageID string
	Payload   MessagePayload
}

// MessagePayload is a sum type for different message payloads.
type MessagePayload interface {
	isMessagePayload()
}

// HelloPayload is the first business message sent after a successful WebSocket
// upgrade.
type HelloPayload struct {
	Role ClientRole
}

// PingPayload is a keep-alive ping sent by either side.
type PingPayload struct {
	Nonce string
}

// PongPayload is the response to a PingPayload.
type PongPayload struct {
	Nonce string
}

// MediaInitPayload carries the latest fMP4 initialization segment.
type MediaInitPayload struct {
	StreamID string
	InitID   string
	MimeType string
	Codec    string
	Segment  []byte
}

// MediaSegmentPayload carries one fMP4 media segment.
type MediaSegmentPayload struct {
	StreamID      string
	InitID        string
	Sequence      uint64
	Segment       []byte
	RandomAccess  bool
	DurationMS    int32
	Discontinuity bool
}

// ControlAckPayload acknowledges receipt of a control request by the agent.
type ControlAckPayload struct {
	RequestID string
}

// ControlResultStatus represents the outcome status of a control operation.
type ControlResultStatus int

const (
	// ControlResultStatusSucceeded indicates the operation completed successfully.
	ControlResultStatusSucceeded ControlResultStatus = iota + 1
	// ControlResultStatusFailed indicates the operation failed.
	ControlResultStatusFailed
	// ControlResultStatusTimedOut indicates the operation timed out.
	ControlResultStatusTimedOut
)

// ControlResultPayload reports the outcome of a control operation.
type ControlResultPayload struct {
	OperationID  string
	Status       ControlResultStatus
	ErrorMessage string
}

// ErrorPayload carries an application-level error.
type ErrorPayload struct {
	Code    string
	Message string
}

// Implement MessagePayload interface for each payload type.
func (HelloPayload) isMessagePayload()         {}
func (PingPayload) isMessagePayload()          {}
func (PongPayload) isMessagePayload()          {}
func (MediaInitPayload) isMessagePayload()     {}
func (MediaSegmentPayload) isMessagePayload()  {}
func (ControlAckPayload) isMessagePayload()    {}
func (ControlResultPayload) isMessagePayload() {}
func (ErrorPayload) isMessagePayload()         {}

// RouteTargetKind indicates how a message should be routed.
type RouteTargetKind int

const (
	// RouteTargetAgent routes to the agent connection.
	RouteTargetAgent RouteTargetKind = iota
	// RouteTargetWebBroadcast broadcasts to all web connections.
	RouteTargetWebBroadcast
	// RouteTargetConn routes to a specific connection by TargetConnID.
	RouteTargetConn
)

// RoutedMessage represents a message to be routed based on TargetKind.
type RoutedMessage struct {
	Message      Message
	TargetKind   RouteTargetKind
	TargetConnID string
}
