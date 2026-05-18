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
	StoreInitSegment(ref *InitSegmentRef) error
	// AddSegment appends a media segment to the cache.
	AddSegment(seg *SegmentRef) error
	// GetInitSegment returns the stored initialization segment.
	GetInitSegment() (*InitSegmentRef, bool)
	// GetActiveInitID returns the init segment ID of the currently active stream.
	GetActiveInitID() string
	// GetSegmentsFromLastRandomAccess returns all segments starting from the most
	// recent random access point. Returns nil if no random access point is found.
	GetSegmentsFromLastRandomAccess() []*SegmentRef
	// GetLatestSnapshot returns the most recently cached snapshot.
	GetLatestSnapshot() (*SnapshotRef, bool)
	// RefreshSnapshot decodes a new snapshot from the latest random-access segment.
	RefreshSnapshot() (*SnapshotRef, error)
}

// SegmentRef holds a single fMP4 media segment.
type SegmentRef struct {
	// StreamID identifies the media stream this segment belongs to.
	StreamID string
	// InitID identifies the init segment this segment references.
	InitID string
	// Sequence is the monotonically increasing sequence number within the stream.
	Sequence uint64
	// Data contains the raw fMP4 segment bytes.
	Data []byte
	// RandomAccess indicates whether this segment starts from a random access point.
	RandomAccess bool
	// DurationMS is the duration of this segment in milliseconds.
	DurationMS int32
	// Discontinuity indicates a gap or discontinuity in the segment timeline.
	Discontinuity bool
	// MediaTime is the timestamp when the segment was received from the agent.
	MediaTime time.Time
}

// InitSegmentRef holds the fMP4 initialization segment.
type InitSegmentRef struct {
	// StreamID identifies the media stream this init segment belongs to.
	StreamID string
	// InitID identifies this init segment uniquely.
	InitID string
	// Codec is the codec string (e.g. "avc1.64001f").
	Codec string
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
