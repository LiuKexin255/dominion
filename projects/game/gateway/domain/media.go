package domain

import (
	"time"
)

// MaxSegmentSize is the maximum allowed size for a single media segment or init
// segment sent over the WebSocket protocol. Both the agent (sender) and the
// gateway (receiver) must respect this limit.
const MaxSegmentSize = 1 << 20 // 1 MiB

// MediaCache defines the interface for caching media segments and snapshots
// for a single session runtime.
type MediaCache interface {
	// StoreInitSegment stores the fMP4 initialization segment.
	StoreInitSegment(mimeType string, data []byte) error
	// AddSegment appends a media segment to the cache.
	AddSegment(seg *SegmentRef) error
	// GetInitSegment returns the stored initialization segment.
	GetInitSegment() (*InitSegmentRef, bool)
	// GetSegmentsFromLastKeyframe returns all segments starting from the most
	// recent key frame boundary, used for late-joining web clients.
	GetSegmentsFromLastKeyframe() []*SegmentRef
	// GetLatestSnapshot returns the most recently cached snapshot.
	GetLatestSnapshot() (*SnapshotRef, bool)
	// RefreshSnapshot decodes a new snapshot from the latest key frame.
	RefreshSnapshot() (*SnapshotRef, error)
}

// SegmentRef holds a single fMP4 media segment.
type SegmentRef struct {
	// SegmentID uniquely identifies the segment within the session.
	SegmentID string
	// Data contains the raw fMP4 segment bytes.
	Data []byte
	// KeyFrame indicates whether this segment starts from a key frame boundary.
	KeyFrame bool
	// MediaTime is the timestamp when the segment was received from the agent.
	MediaTime time.Time
}

// InitSegmentRef holds the fMP4 initialization segment.
type InitSegmentRef struct {
	// MimeType is the MIME type of the media stream (e.g. "video/mp4").
	MimeType string
	// Data contains the raw fMP4 initialization segment bytes.
	Data []byte
}

// SnapshotRef holds a JPEG snapshot decoded from the video stream.
type SnapshotRef struct {
	// Data contains the JPEG-encoded image bytes.
	Data []byte
	// MimeType is the MIME type of the snapshot image.
	MimeType string
	// CaptureTime is when the snapshot was decoded from the video stream.
	CaptureTime time.Time
	// Cached indicates whether the snapshot was served from cache.
	Cached bool
}
