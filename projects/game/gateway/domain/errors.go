package domain

import "errors"

var (
	// ErrSessionNotFound indicates the requested session runtime does not exist.
	ErrSessionNotFound = errors.New("session not found")
	// ErrAgentAlreadyConnected indicates a second agent attempted to connect.
	ErrAgentAlreadyConnected = errors.New("agent already connected")
	// ErrNoAgent indicates no agent is connected when one is required.
	ErrNoAgent = errors.New("no agent connected")
	// ErrOperationInflight indicates a concurrent control operation was rejected.
	ErrOperationInflight = errors.New("operation already inflight")
	// ErrInvalidMouseAction indicates the mouse action parameters are invalid.
	ErrInvalidMouseAction = errors.New("invalid mouse action")
	// ErrHoldDurationExceeded indicates the requested hold duration exceeds MaxHoldDuration.
	ErrHoldDurationExceeded = errors.New("hold duration exceeds maximum")
	// ErrStreamMismatch indicates the media stream ID does not match.
	ErrStreamMismatch = errors.New("stream mismatch")
	// ErrUnknownInitID indicates the init segment ID is not recognized.
	ErrUnknownInitID = errors.New("unknown init segment ID")
	// ErrSequenceNotIncreasing indicates sequence numbers are not monotonically increasing.
	ErrSequenceNotIncreasing = errors.New("sequence not increasing")
	// ErrRandomAccessMissing indicates a random access point is required but not present.
	ErrRandomAccessMissing = errors.New("random access point missing")
	// ErrInitHashMismatch indicates the init segment hash does not match.
	ErrInitHashMismatch = errors.New("init segment hash mismatch")
	// ErrUnsupportedCodec indicates the codec is not supported.
	ErrUnsupportedCodec = errors.New("unsupported codec")
)
