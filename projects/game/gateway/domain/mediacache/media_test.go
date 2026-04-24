package mediacache

import (
	"sync"
	"testing"
	"time"

	"dominion/projects/game/gateway/domain"
)

func TestStoreInitSegment(t *testing.T) {
	c := NewCache()

	// given: no init segment stored
	_, ok := c.GetInitSegment()
	if ok {
		t.Fatal("expected no init segment in empty cache")
	}

	// when: storing an init segment
	data := []byte("ftypisom")
	err := c.StoreInitSegment("video/mp4", data)
	if err != nil {
		t.Fatalf("StoreInitSegment returned error: %v", err)
	}

	// then: init segment is retrievable with correct fields
	init, ok := c.GetInitSegment()
	if !ok {
		t.Fatal("expected init segment to be present")
	}
	if init.MimeType != "video/mp4" {
		t.Fatalf("MimeType = %q, want %q", init.MimeType, "video/mp4")
	}
	if string(init.Data) != string(data) {
		t.Fatalf("Data = %v, want %v", init.Data, data)
	}
}

func TestAddSegment(t *testing.T) {
	c := NewCache()
	base := time.Now()

	// when: adding a single segment
	seg := &domain.SegmentRef{
		SegmentID: "seg-1",
		Data:      []byte("moof-mdat-1"),
		KeyFrame:  true,
		MediaTime: base,
	}
	err := c.AddSegment(seg)
	if err != nil {
		t.Fatalf("AddSegment returned error: %v", err)
	}

	// then: GetSegmentsFromLastKeyframe returns the segment
	segs := c.GetSegmentsFromLastKeyframe()
	if len(segs) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segs))
	}
	if segs[0].SegmentID != "seg-1" {
		t.Fatalf("SegmentID = %q, want %q", segs[0].SegmentID, "seg-1")
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
				{SegmentID: "seg-1", MediaTime: base, KeyFrame: true},
				{SegmentID: "seg-2", MediaTime: base.Add(1 * time.Second)},
				{SegmentID: "seg-3", MediaTime: base.Add(2 * time.Second)},
			},
			wantCount:      3,
			wantFirstSegID: "seg-1",
		},
		{
			name: "segments older than 3s from newest are evicted",
			segments: []*domain.SegmentRef{
				{SegmentID: "seg-1", MediaTime: base, KeyFrame: true},
				{SegmentID: "seg-2", MediaTime: base.Add(1 * time.Second)},
				{SegmentID: "seg-3", MediaTime: base.Add(2 * time.Second)},
				{SegmentID: "seg-4", MediaTime: base.Add(4 * time.Second)},
				{SegmentID: "seg-5", MediaTime: base.Add(5 * time.Second)},
			},
			wantCount:      3,
			wantFirstSegID: "seg-3",
		},
		{
			name: "all segments evicted when gap exceeds 3s",
			segments: []*domain.SegmentRef{
				{SegmentID: "seg-1", MediaTime: base, KeyFrame: true},
				{SegmentID: "seg-2", MediaTime: base.Add(5 * time.Second)},
				{SegmentID: "seg-3", MediaTime: base.Add(10 * time.Second)},
			},
			wantCount:      1,
			wantFirstSegID: "seg-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCache()
			for _, s := range tt.segments {
				if err := c.AddSegment(s); err != nil {
					t.Fatalf("AddSegment error: %v", err)
				}
			}
			segs := c.GetSegmentsFromLastKeyframe()
			if len(segs) != tt.wantCount {
				t.Fatalf("len(segments) = %d, want %d", len(segs), tt.wantCount)
			}
			if tt.wantFirstSegID != "" && segs[0].SegmentID != tt.wantFirstSegID {
				t.Fatalf("first SegmentID = %q, want %q", segs[0].SegmentID, tt.wantFirstSegID)
			}
		})
	}
}

func TestGetSegmentsFromLastKeyframe(t *testing.T) {
	base := time.Now()

	tests := []struct {
		name     string
		segments []*domain.SegmentRef
		wantIDs  []string
	}{
		{
			name: "returns from last keyframe to end",
			segments: []*domain.SegmentRef{
				{SegmentID: "kf-1", MediaTime: base, KeyFrame: true},
				{SegmentID: "p-1", MediaTime: base.Add(1 * time.Second), KeyFrame: false},
				{SegmentID: "kf-2", MediaTime: base.Add(2 * time.Second), KeyFrame: true},
				{SegmentID: "p-2", MediaTime: base.Add(3 * time.Second), KeyFrame: false},
			},
			wantIDs: []string{"kf-2", "p-2"},
		},
		{
			name: "no keyframe returns all segments",
			segments: []*domain.SegmentRef{
				{SegmentID: "p-1", MediaTime: base, KeyFrame: false},
				{SegmentID: "p-2", MediaTime: base.Add(1 * time.Second), KeyFrame: false},
			},
			wantIDs: []string{"p-1", "p-2"},
		},
		{
			name: "single keyframe returns that one",
			segments: []*domain.SegmentRef{
				{SegmentID: "kf-1", MediaTime: base, KeyFrame: true},
			},
			wantIDs: []string{"kf-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCache()
			for _, s := range tt.segments {
				if err := c.AddSegment(s); err != nil {
					t.Fatalf("AddSegment error: %v", err)
				}
			}
			segs := c.GetSegmentsFromLastKeyframe()
			if len(segs) != len(tt.wantIDs) {
				t.Fatalf("len = %d, want %d", len(segs), len(tt.wantIDs))
			}
			for i, want := range tt.wantIDs {
				if segs[i].SegmentID != want {
					t.Fatalf("segs[%d].SegmentID = %q, want %q", i, segs[i].SegmentID, want)
				}
			}
		})
	}
}

func TestGetLatestSnapshot_CacheHitWithinThreshold(t *testing.T) {
	c := NewCache()
	now := time.Now()

	jpeg := ExtractJPEGFallback()
	c.snapshot = &domain.SnapshotRef{
		Data:        jpeg,
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
	if string(snap.Data) != string(jpeg) {
		t.Fatal("snapshot data mismatch")
	}
}

func TestGetLatestSnapshot_CacheMissAfterThreshold(t *testing.T) {
	c := NewCache()

	c.snapshot = &domain.SnapshotRef{
		Data:        ExtractJPEGFallback(),
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

func TestRefreshSnapshot_NoKeyFrame(t *testing.T) {
	c := NewCache()

	// when: no segments at all
	_, err := c.RefreshSnapshot()

	// then: error about no key frame
	if err == nil {
		t.Fatal("expected error when no key frame available")
	}
}

func TestRefreshSnapshot_WithKeyFrameSegment(t *testing.T) {
	c := NewCache()

	// when: key frame segment with data that will fail fMP4 parsing
	seg := &domain.SegmentRef{
		SegmentID: "kf-1",
		Data:      []byte("not-mp4-data"),
		KeyFrame:  true,
		MediaTime: time.Now(),
	}
	if err := c.AddSegment(seg); err != nil {
		t.Fatalf("AddSegment error: %v", err)
	}

	// then: RefreshSnapshot returns a fallback snapshot because data is not valid fMP4
	snap, err := c.RefreshSnapshot()
	if err != nil {
		t.Fatalf("RefreshSnapshot error: %v", err)
	}
	if snap == nil {
		t.Fatal("expected fallback snapshot for invalid fMP4 data")
	}
	if snap.MimeType != "image/jpeg" {
		t.Fatalf("snapshot MimeType = %q, want image/jpeg", snap.MimeType)
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := NewCache()
	base := time.Now()
	_ = c.StoreInitSegment("video/mp4", []byte("init"))

	var wg sync.WaitGroup
	const writers = 10
	const readers = 10

	// when: concurrent writers add segments
	for i := range writers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			seg := &domain.SegmentRef{
				SegmentID: "seg-concurrent",
				Data:      ExtractJPEGFallback(),
				KeyFrame:  idx%3 == 0,
				MediaTime: base.Add(time.Duration(idx) * 500 * time.Millisecond),
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

func TestExtractJPEGFallback(t *testing.T) {
	// when: calling ExtractJPEGFallback
	jpeg := ExtractJPEGFallback()

	// then: returns valid JPEG data (starts with SOI, ends with EOI)
	if len(jpeg) == 0 {
		t.Fatal("expected non-empty JPEG data")
	}
	if jpeg[0] != 0xFF || jpeg[1] != 0xD8 {
		t.Fatalf("JPEG does not start with SOI marker: %02X %02X", jpeg[0], jpeg[1])
	}
	if jpeg[len(jpeg)-2] != 0xFF || jpeg[len(jpeg)-1] != 0xD9 {
		t.Fatalf("JPEG does not end with EOI marker: %02X %02X", jpeg[len(jpeg)-2], jpeg[len(jpeg)-1])
	}
}