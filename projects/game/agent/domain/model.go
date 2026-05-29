// Package domain provides the domain models and interfaces for the agent service.
package domain

import "time"

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
	// CreateTime is the timestamp when this status was recorded.
	CreateTime time.Time
}
