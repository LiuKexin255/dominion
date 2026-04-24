// Package mediacache provides an in-memory cache for fMP4 media segments and
// JPEG snapshots within a single session runtime.
package mediacache

import (
	"sync"
	"time"

	"dominion/projects/game/gateway/domain"
)

// SnapshotFreshThreshold is the maximum age of a cached snapshot before it
// is considered stale and must be re-extracted.
const SnapshotFreshThreshold = 1 * time.Second

// SegmentWindow is the duration of the ring buffer window. Segments older
// than this from the newest segment are evicted.
const SegmentWindow = 3 * time.Second

// Cache implements domain.MediaCache using an in-memory ring buffer for
// segments and an optional cached JPEG snapshot.
type Cache struct {
	mu          sync.RWMutex
	initSegment *domain.InitSegmentRef
	segments    []*domain.SegmentRef
	snapshot    *domain.SnapshotRef
}

// NewCache creates a new Cache ready for use.
func NewCache() *Cache {
	return new(Cache)
}

// StoreInitSegment stores the fMP4 initialization segment, replacing any
// previously stored init segment.
func (c *Cache) StoreInitSegment(mimeType string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.initSegment = &domain.InitSegmentRef{
		MimeType: mimeType,
		Data:     data,
	}
	return nil
}

// AddSegment appends a media segment to the ring buffer and evicts segments
// whose MediaTime is more than SegmentWindow older than the newest segment.
func (c *Cache) AddSegment(seg *domain.SegmentRef) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.segments = append(c.segments, seg)
	c.evict()
	return nil
}

// GetInitSegment returns the stored initialization segment.
func (c *Cache) GetInitSegment() (*domain.InitSegmentRef, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.initSegment == nil {
		return nil, false
	}
	return c.initSegment, true
}

// GetSegmentsFromLastKeyframe returns all segments starting from the most
// recent key frame boundary. If no key frame is found, it returns all
// segments.
func (c *Cache) GetSegmentsFromLastKeyframe() []*domain.SegmentRef {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.segments) == 0 {
		return nil
	}

	idx := -1
	for i := len(c.segments) - 1; i >= 0; i-- {
		if c.segments[i].KeyFrame {
			idx = i
			break
		}
	}

	if idx < 0 {
		return c.segments
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

// RefreshSnapshot extracts a new JPEG snapshot from the latest key frame
// segment, caches it, and returns it with Cached=false.
func (c *Cache) RefreshSnapshot() (*domain.SnapshotRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var seg *domain.SegmentRef
	for i := len(c.segments) - 1; i >= 0; i-- {
		if c.segments[i].KeyFrame {
			seg = c.segments[i]
			break
		}
	}

	if seg == nil {
		return nil, errNoKeyFrame
	}

	jpeg, err := ExtractJPEGFromSegment(c.initSegmentBytes(), seg)
	if err != nil {
		jpeg = ExtractJPEGFallback()
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
	if c.initSegment == nil {
		return nil
	}
	return c.initSegment.Data
}
