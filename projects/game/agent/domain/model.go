// Package domain provides the domain models and interfaces for the agent service.
package domain

import (
	"errors"
	"time"
)

// ScreenshotInput carries the raw screenshot data sent by a client.
type ScreenshotInput struct {
	// SessionId identifies the agent session this screenshot belongs to.
	SessionId string
	// CaptureId is a client-assigned identifier used to correlate the
	// screenshot with its acknowledgment.
	CaptureId string
	// Encoding is the image encoding format (currently only "PNG").
	Encoding string
	// Data contains the raw image bytes.
	Data []byte
	// WidthPx is the image width in pixels.
	WidthPx int32
	// HeightPx is the image height in pixels.
	HeightPx int32
	// ScaleFactor is the display scale factor of the captured region.
	ScaleFactor float64
	// WindowTitle is the title of the window at capture time.
	WindowTitle string
	// CaptureTime is the timestamp when the screenshot was taken on the client.
	CaptureTime time.Time
}

// ScreenshotReceipt is returned after successfully receiving a screenshot.
type ScreenshotReceipt struct {
	// AckFrameId echoes the CaptureId from the input so the sender can
	// correlate the acknowledgment with the original capture request.
	AckFrameId string
	// Message is a human-readable confirmation.
	Message string
}

// Status represents the current state of an agent session.
type Status struct {
	// SessionId is the unique identifier of the session.
	SessionId string
	// Status is the current status string (e.g. "initialized", "unknown").
	Status string
	// ProfileName is the agent profile name used to create this agent.
	ProfileName string
	// CreateTime is the timestamp when this status was recorded.
	CreateTime time.Time
}

// ErrNotFound is returned when an agent session is not found.
var ErrNotFound = errors.New("agent not found")

// InvokeState represents the state of an invoke cycle.
type InvokeState int

const (
	InvokeStateIdle      InvokeState = iota
	InvokeStateInvoking              = iota
	InvokeStateCompleted             = iota
	InvokeStateFailed                = iota
)

// InvokeContext holds the state of an active invoke.
type InvokeContext struct {
	SessionID   string
	InvokeID    string
	Sequence    int64
	State       InvokeState
	CreateTime  time.Time
	ProfileName string
	Skills      []string
	MCPNames    []string
}

// InvokeRuntimeConfig holds the configuration for creating an agent runtime.
type InvokeRuntimeConfig struct {
	// ProfileName is the agent profile name (business identifier).
	ProfileName string
	// Model is the model name to use.
	Model string
	// SystemPrompt is the system prompt for the agent.
	SystemPrompt string
	// Skills are the skills configured for this agent.
	Skills []SkillConfig
	// MCPNames are the names of MCP servers accessible to this agent.
	MCPNames []string
}

// SkillConfig holds the configuration for a single skill.
type SkillConfig struct {
	// SkillName is the business identifier for this skill.
	SkillName string
	// Content is the skill content (text).
	Content string
}

// OperationInput represents a desktop operation to execute.
type OperationInput struct {
	OperationID  string
	ScreenshotID string
	// Mouse operation fields
	Button    int32 // 1=LEFT, 2=RIGHT
	ClickType int32 // 1=SINGLE, 2=DOUBLE
	XPx       int32
	YPx       int32
	// Keyboard operation fields
	KeyCodes string
	IsMouse  bool // true=mouse, false=keyboard
}

// FrameType identifies the kind of frame payload.
type FrameType int

const (
	FrameTypeText      FrameType = iota + 1
	FrameTypeThinking            = iota + 1
	FrameTypeOperation           = iota + 1
	FrameTypeWarn                = iota + 1
)

// Frame represents a single output frame from the agent.
type Frame struct {
	Type    FrameType
	Content string // used for text, thinking

	// Operation-specific fields (only set when Type == FrameTypeOperation)
	OperationID  string
	ScreenshotID string
	// Sequence carries the envelope sequence value (promoted to AgentFrame.Sequence by handler). This is NOT a payload-level sequence.
	Sequence  int64
	Button    int32
	ClickType int32
	XPx       int32
	YPx       int32
	IsMouse   bool
	KeyCodes  string

	// Warn-specific fields (only set when Type == FrameTypeWarn)
	WarnMessage string
	WarnCode    string
}
