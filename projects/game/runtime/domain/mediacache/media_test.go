package mediacache

import (
	"sync"
	"testing"
	"time"

	"dominion/projects/game/runtime/domain"
)

// newTestInit creates a minimal InitSegmentRef for tests that only need
// a valid init to exist (stream/init ID are the only relevant fields).
func newTestInit(streamID, initID string) *domain.InitSegmentRef {
	return &domain.InitSegmentRef{
		StreamID: streamID,
		InitID:   initID,
		Codec:    "avc1.64001f",
		MimeType: "video/mp4",
		Data:     []byte("test-init-data"),
	}
}

func TestStoreInitSegment(t *testing.T) {
	c := NewCache()

	// given: no init segment stored
	_, ok := c.GetInitSegment()
	if ok {
		t.Fatal("expected no init segment in empty cache")
	}

	// when: storing an init segment
	data := []byte("ftypisom")
	err := c.StoreInitSegment(&domain.InitSegmentRef{
		StreamID: "stream-1",
		InitID:   "init-1",
		Codec:    "avc1.64001f",
		MimeType: "video/mp4",
		Data:     data,
	})
	if err != nil {
		t.Fatalf("StoreInitSegment returned error: %v", err)
	}

	// then: init segment is retrievable with correct fields
	init, ok := c.GetInitSegment()
	if !ok {
		t.Fatal("expected init segment to be present")
	}
	if init.StreamID != "stream-1" {
		t.Fatalf("StreamID = %q, want %q", init.StreamID, "stream-1")
	}
	if init.InitID != "init-1" {
		t.Fatalf("InitID = %q, want %q", init.InitID, "init-1")
	}
	if init.Codec != "avc1.64001f" {
		t.Fatalf("Codec = %q, want %q", init.Codec, "avc1.64001f")
	}
	if init.MimeType != "video/mp4" {
		t.Fatalf("MimeType = %q, want %q", init.MimeType, "video/mp4")
	}
	if string(init.Data) != string(data) {
		t.Fatalf("Data = %v, want %v", init.Data, data)
	}

	// and: GetActiveInitID returns correct ID
	if got := c.GetActiveInitID(); got != "init-1" {
		t.Fatalf("GetActiveInitID() = %q, want %q", got, "init-1")
	}
}

func TestAddSegment(t *testing.T) {
	c := NewCache()
	base := time.Now()

	// given: an init segment is stored
	if err := c.StoreInitSegment(newTestInit("stream-1", "init-1")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}

	// when: adding a single segment with matching stream/init
	seg := &domain.SegmentRef{
		StreamID:     "stream-1",
		InitID:       "init-1",
		Sequence:     1,
		Data:         []byte("moof-mdat-1"),
		RandomAccess: true,
		MediaTime:    base,
	}
	err := c.AddSegment(seg)
	if err != nil {
		t.Fatalf("AddSegment returned error: %v", err)
	}

	// then: GetSegmentsFromLastRandomAccess returns the segment
	segs := c.GetSegmentsFromLastRandomAccess()
	if len(segs) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segs))
	}
	if segs[0].StreamID != "stream-1" {
		t.Fatalf("StreamID = %q, want %q", segs[0].StreamID, "stream-1")
	}
	if segs[0].InitID != "init-1" {
		t.Fatalf("InitID = %q, want %q", segs[0].InitID, "init-1")
	}
	if segs[0].Sequence != 1 {
		t.Fatalf("Sequence = %d, want %d", segs[0].Sequence, 1)
	}
	if !segs[0].RandomAccess {
		t.Fatalf("RandomAccess = false, want true")
	}
}

func TestRingBufferEviction(t *testing.T) {
	base := time.Now()

	tests := []struct {
		name           string
		segments       []*domain.SegmentRef
		wantCount      int
		wantFirstSegID string
	}{
		{
			name: "segments within 3s window are kept",
			segments: []*domain.SegmentRef{
				{StreamID: "s1", InitID: "i1", Sequence: 1, MediaTime: base, RandomAccess: true},
				{StreamID: "s1", InitID: "i1", Sequence: 2, MediaTime: base.Add(1 * time.Second)},
				{StreamID: "s1", InitID: "i1", Sequence: 3, MediaTime: base.Add(2 * time.Second)},
			},
			wantCount:      3,
			wantFirstSegID: "s1",
		},
		{
			name: "segments older than 3s from newest are evicted",
			segments: []*domain.SegmentRef{
				{StreamID: "s1", InitID: "i1", Sequence: 1, MediaTime: base, RandomAccess: true},
				{StreamID: "s1", InitID: "i1", Sequence: 2, MediaTime: base.Add(1 * time.Second)},
				{StreamID: "s1", InitID: "i1", Sequence: 3, MediaTime: base.Add(2 * time.Second)},
				{StreamID: "s1", InitID: "i1", Sequence: 4, MediaTime: base.Add(4 * time.Second)},
				{StreamID: "s1", InitID: "i1", Sequence: 5, MediaTime: base.Add(5 * time.Second)},
			},
			wantCount:      3,
			wantFirstSegID: "s1",
		},
		{
			name: "all segments evicted when gap exceeds 3s",
			segments: []*domain.SegmentRef{
				{StreamID: "s1", InitID: "i1", Sequence: 1, MediaTime: base, RandomAccess: true},
				{StreamID: "s1", InitID: "i1", Sequence: 2, MediaTime: base.Add(5 * time.Second)},
				{StreamID: "s1", InitID: "i1", Sequence: 3, MediaTime: base.Add(10 * time.Second)},
			},
			wantCount:      1,
			wantFirstSegID: "s1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCache()
			// given: an init segment is stored
			if err := c.StoreInitSegment(newTestInit("s1", "i1")); err != nil {
				t.Fatalf("StoreInitSegment: %v", err)
			}
			// when: adding segments
			for _, s := range tt.segments {
				if err := c.AddSegment(s); err != nil {
					t.Fatalf("AddSegment error: %v", err)
				}
			}
			// then: eviction maintains correct window (check internal segments directly)
			c.mu.RLock()
			segs := c.segments
			c.mu.RUnlock()
			if len(segs) != tt.wantCount {
				t.Fatalf("len(segments) = %d, want %d", len(segs), tt.wantCount)
			}
			if tt.wantFirstSegID != "" && segs[0].StreamID != tt.wantFirstSegID {
				t.Fatalf("first StreamID = %q, want %q", segs[0].StreamID, tt.wantFirstSegID)
			}
		})
	}
}

func TestGetSegmentsFromLastRandomAccess(t *testing.T) {
	base := time.Now()

	tests := []struct {
		name     string
		segments []*domain.SegmentRef
		wantIDs  []string
	}{
		{
			name: "returns from last random access to end",
			segments: []*domain.SegmentRef{
				{StreamID: "s1", InitID: "i1", Sequence: 1, MediaTime: base, RandomAccess: true},
				{StreamID: "s1", InitID: "i1", Sequence: 2, MediaTime: base.Add(1 * time.Second), RandomAccess: false},
				{StreamID: "s1", InitID: "i1", Sequence: 3, MediaTime: base.Add(2 * time.Second), RandomAccess: true},
				{StreamID: "s1", InitID: "i1", Sequence: 4, MediaTime: base.Add(3 * time.Second), RandomAccess: false},
			},
			wantIDs: []string{"s1", "s1"},
		},
		{
			name: "no random access returns empty",
			segments: []*domain.SegmentRef{
				{StreamID: "s1", InitID: "i1", Sequence: 1, MediaTime: base, RandomAccess: false},
				{StreamID: "s1", InitID: "i1", Sequence: 2, MediaTime: base.Add(1 * time.Second), RandomAccess: false},
			},
			wantIDs: nil,
		},
		{
			name: "single random access returns that one",
			segments: []*domain.SegmentRef{
				{StreamID: "s1", InitID: "i1", Sequence: 1, MediaTime: base, RandomAccess: true},
			},
			wantIDs: []string{"s1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCache()
			// given: an init segment is stored
			if err := c.StoreInitSegment(newTestInit("s1", "i1")); err != nil {
				t.Fatalf("StoreInitSegment: %v", err)
			}
			// when: adding segments and querying
			for _, s := range tt.segments {
				if err := c.AddSegment(s); err != nil {
					t.Fatalf("AddSegment error: %v", err)
				}
			}
			segs := c.GetSegmentsFromLastRandomAccess()
			// then: correct segments returned
			if len(segs) != len(tt.wantIDs) {
				t.Fatalf("len = %d, want %d", len(segs), len(tt.wantIDs))
			}
			for i, want := range tt.wantIDs {
				if segs[i].StreamID != want {
					t.Fatalf("segs[%d].StreamID = %q, want %q", i, segs[i].StreamID, want)
				}
			}
		})
	}
}

func TestGetLatestSnapshot_CacheHitWithinThreshold(t *testing.T) {
	c := NewCache()
	now := time.Now()

	snapData := []byte("cached-jpeg-data")
	c.snapshot = &domain.SnapshotRef{
		Data:        snapData,
		MimeType:    "image/jpeg",
		CaptureTime: now,
		Cached:      false,
	}

	// when: snapshot is fresh
	snap, ok := c.GetLatestSnapshot()

	// then: returns cached snapshot with Cached=true
	if !ok {
		t.Fatal("expected snapshot to be found")
	}
	if !snap.Cached {
		t.Fatal("expected Cached=true for cache hit")
	}
	if snap.MimeType != "image/jpeg" {
		t.Fatalf("MimeType = %q, want %q", snap.MimeType, "image/jpeg")
	}
	if string(snap.Data) != string(snapData) {
		t.Fatal("snapshot data mismatch")
	}
}

func TestGetLatestSnapshot_CacheMissAfterThreshold(t *testing.T) {
	c := NewCache()

	c.snapshot = &domain.SnapshotRef{
		Data:        []byte("stale-jpeg-data"),
		MimeType:    "image/jpeg",
		CaptureTime: time.Now().Add(-2 * time.Second),
		Cached:      false,
	}

	// when: snapshot is stale (older than 1s)
	_, ok := c.GetLatestSnapshot()

	// then: returns false
	if ok {
		t.Fatal("expected no snapshot for stale entry")
	}
}

func TestGetLatestSnapshot_EmptyCache(t *testing.T) {
	c := NewCache()

	_, ok := c.GetLatestSnapshot()
	if ok {
		t.Fatal("expected no snapshot in empty cache")
	}
}

func TestRefreshSnapshot_NoRandomAccess(t *testing.T) {
	c := NewCache()

	// when: no segments at all
	_, err := c.RefreshSnapshot()

	// then: error about no random access segment
	if err == nil {
		t.Fatal("expected error when no random access segment available")
	}
}

func TestRefreshSnapshot_WithRandomAccessSegment_InvalidData(t *testing.T) {
	c := NewCache()

	// given: an init segment is stored
	if err := c.StoreInitSegment(newTestInit("s1", "i1")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}

	// when: random access segment with data that will fail fMP4 parsing
	seg := &domain.SegmentRef{
		StreamID:     "s1",
		InitID:       "i1",
		Sequence:     1,
		Data:         []byte("not-mp4-data"),
		RandomAccess: true,
		MediaTime:    time.Now(),
	}
	if err := c.AddSegment(seg); err != nil {
		t.Fatalf("AddSegment error: %v", err)
	}

	// then: RefreshSnapshot returns error because data is not valid fMP4
	_, err := c.RefreshSnapshot()
	if err == nil {
		t.Fatal("expected error for invalid fMP4 data in random access segment")
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := NewCache()
	base := time.Now()
	_ = c.StoreInitSegment(&domain.InitSegmentRef{
		StreamID: "s1",
		InitID:   "i1",
		Codec:    "avc1.64001f",
		MimeType: "video/mp4",
		Data:     []byte("init"),
	})

	var wg sync.WaitGroup
	const writers = 10
	const readers = 10

	// when: concurrent writers add segments
	for i := range writers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			seg := &domain.SegmentRef{
				StreamID:     "s1",
				InitID:       "i1",
				Sequence:     uint64(idx),
				Data:         []byte("concurrent-seg-data"),
				RandomAccess: idx%3 == 0,
				MediaTime:    base.Add(time.Duration(idx) * 500 * time.Millisecond),
			}
			_ = c.AddSegment(seg)
		}(i)
	}

	// and: concurrent readers get snapshots and init segments
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.GetLatestSnapshot()
			_, _ = c.GetInitSegment()
		}()
	}

	wg.Wait()

	// then: no panics or data races (verified by race detector)
	init, ok := c.GetInitSegment()
	if !ok {
		t.Fatal("init segment lost during concurrent access")
	}
	if init.MimeType != "video/mp4" {
		t.Fatalf("MimeType = %q, want %q", init.MimeType, "video/mp4")
	}
}

// --------------------------------------------------------------------------
// v2 stream-aware lifecycle tests
// --------------------------------------------------------------------------

func TestStreamLifecycle_NewStreamClearsOld(t *testing.T) {
	c := NewCache()
	base := time.Now()

	// given: stream-1 with init-1, segments, and snapshot
	if err := c.StoreInitSegment(newTestInit("stream-1", "init-1")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 1,
		Data: []byte("seg-1"), RandomAccess: true, MediaTime: base,
	})
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 2,
		Data: []byte("seg-2"), RandomAccess: false, MediaTime: base.Add(100 * time.Millisecond),
	})
	c.snapshot = &domain.SnapshotRef{
		Data: []byte("old-snap"), MimeType: "image/jpeg", CaptureTime: base,
	}

	// when: storing a new init with different stream_id
	err := c.StoreInitSegment(newTestInit("stream-2", "init-2"))
	if err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}

	// then: old segments are cleared
	segs := c.GetSegmentsFromLastRandomAccess()
	if len(segs) != 0 {
		t.Fatalf("expected 0 segments after stream change, got %d", len(segs))
	}

	// then: old snapshot is cleared
	if c.snapshot != nil {
		t.Fatal("expected snapshot to be nil after stream change")
	}

	// then: new init is active
	init, ok := c.GetInitSegment()
	if !ok {
		t.Fatal("expected init segment to be present")
	}
	if init.StreamID != "stream-2" {
		t.Fatalf("StreamID = %q, want %q", init.StreamID, "stream-2")
	}
	if init.InitID != "init-2" {
		t.Fatalf("InitID = %q, want %q", init.InitID, "init-2")
	}

	// then: GetActiveInitID returns new init
	if got := c.GetActiveInitID(); got != "init-2" {
		t.Fatalf("GetActiveInitID() = %q, want %q", got, "init-2")
	}
}

func TestStreamLifecycle_NewInitClearsDependent(t *testing.T) {
	c := NewCache()
	base := time.Now()

	// given: stream-1 with init-1, segments, and snapshot
	if err := c.StoreInitSegment(newTestInit("stream-1", "init-1")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 1,
		Data: []byte("seg-1"), RandomAccess: true, MediaTime: base,
	})
	c.snapshot = &domain.SnapshotRef{
		Data: []byte("old-snap"), MimeType: "image/jpeg", CaptureTime: base,
	}

	// when: storing a new init with same stream but different init_id
	err := c.StoreInitSegment(newTestInit("stream-1", "init-2"))
	if err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}

	// then: old segments (referencing init-1) are cleared
	segs := c.GetSegmentsFromLastRandomAccess()
	if len(segs) != 0 {
		t.Fatalf("expected 0 segments after init change, got %d", len(segs))
	}

	// then: snapshot is cleared
	if c.snapshot != nil {
		t.Fatal("expected snapshot to be nil after init change")
	}

	// then: new init is active
	init, ok := c.GetInitSegment()
	if !ok {
		t.Fatal("expected init segment to be present")
	}
	if init.InitID != "init-2" {
		t.Fatalf("InitID = %q, want %q", init.InitID, "init-2")
	}
}

func TestGetSegmentsFromLastRandomAccess_OnlyMatchingStreamInit(t *testing.T) {
	c := NewCache()
	base := time.Now()

	// given: stream-1 init-1 active, with matching segments
	if err := c.StoreInitSegment(newTestInit("stream-1", "init-1")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 1,
		Data: []byte("seg-1"), RandomAccess: true, MediaTime: base,
	})
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 2,
		Data: []byte("seg-2"), RandomAccess: false, MediaTime: base.Add(100 * time.Millisecond),
	})

	// when: querying segments
	segs := c.GetSegmentsFromLastRandomAccess()

	// then: only matching stream/init segments are returned
	if len(segs) != 2 {
		t.Fatalf("len = %d, want 2", len(segs))
	}
	for _, s := range segs {
		if s.StreamID != "stream-1" {
			t.Fatalf("segment has StreamID = %q, want %q", s.StreamID, "stream-1")
		}
		if s.InitID != "init-1" {
			t.Fatalf("segment has InitID = %q, want %q", s.InitID, "init-1")
		}
	}
}

func TestGetSegmentsFromLastRandomAccess_NoRandomAccessReturnsEmpty(t *testing.T) {
	c := NewCache()
	base := time.Now()

	// given: init stored, only non-random-access segments
	if err := c.StoreInitSegment(newTestInit("stream-1", "init-1")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 1,
		Data: []byte("seg-1"), RandomAccess: false, MediaTime: base,
	})
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 2,
		Data: []byte("seg-2"), RandomAccess: false, MediaTime: base.Add(100 * time.Millisecond),
	})

	// when: querying segments
	segs := c.GetSegmentsFromLastRandomAccess()

	// then: returns nil (no valid catch-up start point)
	if segs != nil {
		t.Fatalf("expected nil when no random-access segment, got %d segments", len(segs))
	}
}

func TestGetSegmentsFromLastRandomAccess_RandomAccessAsStart(t *testing.T) {
	c := NewCache()
	base := time.Now()

	// given: three segments, first is random-access, rest are not
	if err := c.StoreInitSegment(newTestInit("stream-1", "init-1")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 1,
		Data: []byte("seg-1"), RandomAccess: true, MediaTime: base,
	})
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 2,
		Data: []byte("seg-2"), RandomAccess: false, MediaTime: base.Add(100 * time.Millisecond),
	})
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 3,
		Data: []byte("seg-3"), RandomAccess: false, MediaTime: base.Add(200 * time.Millisecond),
	})

	// when: querying segments
	segs := c.GetSegmentsFromLastRandomAccess()

	// then: returns from the random-access segment to the end
	if len(segs) != 3 {
		t.Fatalf("len = %d, want 3", len(segs))
	}
	if segs[0].Sequence != 1 {
		t.Fatalf("first segment Sequence = %d, want 1", segs[0].Sequence)
	}
	if segs[2].Sequence != 3 {
		t.Fatalf("last segment Sequence = %d, want 3", segs[2].Sequence)
	}
}

func TestGetActiveInitID(t *testing.T) {
	tests := []struct {
		name    string
		inits   []*domain.InitSegmentRef
		wantIDs []string
	}{
		{
			name:    "no init returns empty string",
			inits:   nil,
			wantIDs: []string{""},
		},
		{
			name: "single init returns its ID",
			inits: []*domain.InitSegmentRef{
				{StreamID: "s1", InitID: "init-1", Codec: "avc1.64001f", MimeType: "video/mp4", Data: []byte("d")},
			},
			wantIDs: []string{"init-1"},
		},
		{
			name: "replaced init returns new ID",
			inits: []*domain.InitSegmentRef{
				{StreamID: "s1", InitID: "init-1", Codec: "avc1.64001f", MimeType: "video/mp4", Data: []byte("d1")},
				{StreamID: "s1", InitID: "init-2", Codec: "avc1.64001f", MimeType: "video/mp4", Data: []byte("d2")},
			},
			wantIDs: []string{"init-1", "init-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCache()
			for i, init := range tt.inits {
				if err := c.StoreInitSegment(init); err != nil {
					t.Fatalf("StoreInitSegment[%d]: %v", i, err)
				}
				if got := c.GetActiveInitID(); got != tt.wantIDs[i] {
					t.Fatalf("GetActiveInitID() after store[%d] = %q, want %q", i, got, tt.wantIDs[i])
				}
			}
			// verify empty cache returns ""
			if len(tt.inits) == 0 {
				if got := c.GetActiveInitID(); got != "" {
					t.Fatalf("GetActiveInitID() on empty cache = %q, want %q", got, "")
				}
			}
		})
	}
}

func TestAddSegment_RejectsNonMatchingInit(t *testing.T) {
	c := NewCache()
	base := time.Now()

	// given: stream-1 init-1 is active
	if err := c.StoreInitSegment(newTestInit("stream-1", "init-1")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}

	tests := []struct {
		name    string
		seg     *domain.SegmentRef
		wantErr bool
	}{
		{
			name: "rejects different init_id",
			seg: &domain.SegmentRef{
				StreamID: "stream-1", InitID: "init-2", Sequence: 1,
				Data: []byte("seg"), RandomAccess: true, MediaTime: base,
			},
			wantErr: true,
		},
		{
			name: "rejects different stream_id",
			seg: &domain.SegmentRef{
				StreamID: "stream-2", InitID: "init-1", Sequence: 1,
				Data: []byte("seg"), RandomAccess: true, MediaTime: base,
			},
			wantErr: true,
		},
		{
			name: "rejects both mismatched",
			seg: &domain.SegmentRef{
				StreamID: "stream-2", InitID: "init-2", Sequence: 1,
				Data: []byte("seg"), RandomAccess: true, MediaTime: base,
			},
			wantErr: true,
		},
		{
			name: "accepts matching stream and init",
			seg: &domain.SegmentRef{
				StreamID: "stream-1", InitID: "init-1", Sequence: 1,
				Data: []byte("seg"), RandomAccess: true, MediaTime: base,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.AddSegment(tt.seg)
			if tt.wantErr && err == nil {
				t.Fatal("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNonRandomAccessNotCatchUpStart(t *testing.T) {
	c := NewCache()
	base := time.Now()

	// given: non-random-access segment first, then random-access
	if err := c.StoreInitSegment(newTestInit("stream-1", "init-1")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 1,
		Data: []byte("seg-1"), RandomAccess: false, MediaTime: base,
	})
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 2,
		Data: []byte("seg-2"), RandomAccess: true, MediaTime: base.Add(100 * time.Millisecond),
	})

	// when: querying segments
	segs := c.GetSegmentsFromLastRandomAccess()

	// then: non-random-access segment is NOT included (it's before the catch-up start)
	if len(segs) != 1 {
		t.Fatalf("len = %d, want 1 (only latest random-access segment)", len(segs))
	}
	if segs[0].Sequence != 2 {
		t.Fatalf("segment Sequence = %d, want 2 (random-access)", segs[0].Sequence)
	}
	if !segs[0].RandomAccess {
		t.Fatal("expected returned segment to be RandomAccess=true")
	}
}

func TestInitSwitchOldSegmentsNotInCatchUp(t *testing.T) {
	c := NewCache()
	base := time.Now()

	// given: init-1 with segments
	if err := c.StoreInitSegment(newTestInit("stream-1", "init-1")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-1", Sequence: 1,
		Data: []byte("old-seg-1"), RandomAccess: true, MediaTime: base,
	})

	// when: switching to init-2 (clears old segments), adding new segments
	if err := c.StoreInitSegment(newTestInit("stream-1", "init-2")); err != nil {
		t.Fatalf("StoreInitSegment: %v", err)
	}
	_ = c.AddSegment(&domain.SegmentRef{
		StreamID: "stream-1", InitID: "init-2", Sequence: 1,
		Data: []byte("new-seg-1"), RandomAccess: true, MediaTime: base.Add(200 * time.Millisecond),
	})

	// then: catch-up only includes init-2 segments
	segs := c.GetSegmentsFromLastRandomAccess()
	if len(segs) != 1 {
		t.Fatalf("len = %d, want 1 (only new init segments)", len(segs))
	}
	if segs[0].InitID != "init-2" {
		t.Fatalf("InitID = %q, want %q", segs[0].InitID, "init-2")
	}
	if segs[0].Sequence != 1 {
		t.Fatalf("Sequence = %d, want 1", segs[0].Sequence)
	}
}
