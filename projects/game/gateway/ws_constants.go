package gateway

// Protocol-level WebSocket error codes.
const (
	ErrCodeSessionMismatch       = "session_mismatch"
	ErrCodeMissingPayload        = "missing_payload"
	ErrCodeUnsupportedCodec      = "unsupported_codec"
	ErrCodeInitHashMismatch      = "init_hash_mismatch"
	ErrCodeStreamMismatch        = "stream_mismatch"
	ErrCodeUnknownInitID         = "unknown_init_id"
	ErrCodeSequenceNonIncreasing = "sequence_not_increasing"
	ErrCodeRandomAccessMissing   = "random_access_missing"
	ErrCodeSegmentTooLarge       = "segment_too_large"
)
