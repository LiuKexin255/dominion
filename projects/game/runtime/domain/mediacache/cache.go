// Package mediacache provides an in-memory cache for fMP4 media segments and
// JPEG snapshots within a single session runtime.
package mediacache

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"dominion/projects/game/runtime/domain"
)

// SnapshotFreshThreshold is the maximum age of a cached snapshot before it
// is considered stale and must be re-extracted.
const SnapshotFreshThreshold = 1 * time.Second

// SegmentWindow is the duration of the ring buffer window. Segments older
// than this from the newest segment are evicted.
const SegmentWindow = 3 * time.Second

var errNoActiveInit = errors.New("no active init segment stored")

// Cache implements domain.MediaCache using an in-memory ring buffer for
// segments and an optional cached JPEG snapshot.
type Cache struct {
	mu         sync.RWMutex
	activeInit *domain.InitSegmentRef // currently active init
	segments   []*domain.SegmentRef   // segments matching active stream+init
	snapshot   *domain.SnapshotRef
}

// NewCache creates a new Cache ready for use.
func NewCache() *Cache {
	return new(Cache)
}

// StoreInitSegment stores the fMP4 initialization segment. If the stream_id
// changes, all old segments, snapshot, and random-access index are cleared.
// If only the init_id changes (same stream), old segments and snapshot are
// cleared. Replaces the active init segment.
func (c *Cache) StoreInitSegment(ref *domain.InitSegmentRef) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.activeInit != nil {
		if ref.StreamID != c.activeInit.StreamID {
			// New stream: clear everything.
			c.segments = nil
			c.snapshot = nil
		} else if ref.InitID != c.activeInit.InitID {
			// Same stream, new init: clear segments and snapshot.
			c.segments = nil
			c.snapshot = nil
		}
	}

	c.activeInit = ref
	return nil
}

// AddSegment appends a media segment to the ring buffer after validating
// that the segment's stream_id and init_id match the active init segment.
// Segments whose MediaTime is more than SegmentWindow older than the newest
// segment are evicted.
func (c *Cache) AddSegment(seg *domain.SegmentRef) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.activeInit == nil {
		return errNoActiveInit
	}
	if seg.StreamID != c.activeInit.StreamID {
		return fmt.Errorf("segment stream_id %q does not match active stream %q: %w", seg.StreamID, c.activeInit.StreamID, domain.ErrStreamMismatch)
	}
	if seg.InitID != c.activeInit.InitID {
		return fmt.Errorf("segment init_id %q does not match active init %q: %w", seg.InitID, c.activeInit.InitID, domain.ErrUnknownInitID)
	}

	c.segments = append(c.segments, seg)
	c.evict()
	return nil
}

// GetInitSegment returns the stored initialization segment.
func (c *Cache) GetInitSegment() (*domain.InitSegmentRef, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.activeInit == nil {
		return nil, false
	}
	return c.activeInit, true
}

// GetActiveInitID returns the init segment ID of the currently active stream.
// Returns empty string if no init segment has been stored.
func (c *Cache) GetActiveInitID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.activeInit == nil {
		return ""
	}
	return c.activeInit.InitID
}

// GetSegmentsFromLastRandomAccess returns all segments starting from the most
// recent random access point. If no random access point is found, it returns
// nil (empty). This ensures late joiners only receive decodable sequences.
func (c *Cache) GetSegmentsFromLastRandomAccess() []*domain.SegmentRef {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.segments) == 0 {
		return nil
	}

	idx := -1
	for i := len(c.segments) - 1; i >= 0; i-- {
		if c.segments[i].RandomAccess {
			idx = i
			break
		}
	}

	if idx < 0 {
		return nil
	}

	return c.segments[idx:]
}

// GetLatestSnapshot returns the cached snapshot if it was captured within
// SnapshotFreshThreshold. The returned SnapshotRef has Cached=true.
func (c *Cache) GetLatestSnapshot() (*domain.SnapshotRef, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.snapshot == nil {
		return nil, false
	}

	elapsed := time.Since(c.snapshot.CaptureTime)
	if elapsed > SnapshotFreshThreshold {
		return nil, false
	}

	return &domain.SnapshotRef{
		Data:        c.snapshot.Data,
		MimeType:    c.snapshot.MimeType,
		CaptureTime: c.snapshot.CaptureTime,
		Cached:      true,
	}, true
}

// RefreshSnapshot extracts a new JPEG snapshot from the latest random access
// segment, caches it, and returns it with Cached=false.
func (c *Cache) RefreshSnapshot() (*domain.SnapshotRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var seg *domain.SegmentRef
	for i := len(c.segments) - 1; i >= 0; i-- {
		if c.segments[i].RandomAccess {
			seg = c.segments[i]
			break
		}
	}

	if seg == nil {
		return nil, errNoRandomAccess
	}

	jpeg, err := ExtractJPEGFromSegment(c.initSegmentBytes(), seg)
	if err != nil {
		return nil, fmt.Errorf("extract JPEG from segment: %w", err)
	}

	snap := &domain.SnapshotRef{
		Data:        jpeg,
		MimeType:    "image/jpeg",
		CaptureTime: time.Now(),
		Cached:      false,
	}

	c.snapshot = snap
	return snap, nil
}

// evict removes segments whose MediaTime is more than SegmentWindow older
// than the newest segment. Caller must hold c.mu.
func (c *Cache) evict() {
	if len(c.segments) < 2 {
		return
	}

	newest := c.segments[len(c.segments)-1]
	cutoff := newest.MediaTime.Add(-SegmentWindow)

	i := 0
	for i < len(c.segments) {
		if c.segments[i].MediaTime.After(cutoff) || c.segments[i].MediaTime.Equal(cutoff) {
			break
		}
		i++
	}

	if i > 0 {
		copy(c.segments, c.segments[i:])
		c.segments = c.segments[:len(c.segments)-i]
	}
}

func (c *Cache) initSegmentBytes() []byte {
	if c.activeInit == nil {
		return nil
	}
	return c.activeInit.Data
}
