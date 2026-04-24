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
	MimeType string
	Segment  []byte
}

// MediaSegmentPayload carries one fMP4 media segment.
type MediaSegmentPayload struct {
	SegmentID string
	Segment   []byte
	KeyFrame  bool
}

// ControlRequestPayload requests one complete mouse action from web to agent.
type ControlRequestPayload struct {
	RequestID     string
	Kind          OperationKind
	X, Y          int32
	DurationMs    int32
	FlashSnapshot bool
}

// ControlAckPayload acknowledges receipt of a control request by the agent.
type ControlAckPayload struct {
	RequestID string
}

// ControlResultPayload reports the outcome of a control operation.
type ControlResultPayload struct {
	RequestID string
	Success   bool
	Error     string
	TimedOut  bool
}

// ErrorPayload carries an application-level error.
type ErrorPayload struct {
	Code    string
	Message string
}

// Implement MessagePayload interface for each payload type.
func (HelloPayload) isMessagePayload()          {}
func (PingPayload) isMessagePayload()           {}
func (PongPayload) isMessagePayload()           {}
func (MediaInitPayload) isMessagePayload()      {}
func (MediaSegmentPayload) isMessagePayload()   {}
func (ControlRequestPayload) isMessagePayload() {}
func (ControlAckPayload) isMessagePayload()     {}
func (ControlResultPayload) isMessagePayload()  {}
func (ErrorPayload) isMessagePayload()          {}

// RoutedMessage represents a message to be routed to a specific connection
// (domain equivalent of RoutedEnvelope).
//
// TargetConnID determines the routing behavior:
//   - empty string: broadcast to all web connections
//   - non-empty: deliver to the specific web connection
type RoutedMessage struct {
	// TargetConnID is the destination connection ID. Empty means broadcast.
	TargetConnID string
	// Message is the domain message to send on the target connection.
	Message *Message
}
