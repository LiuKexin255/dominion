package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"dominion/projects/game/agent/domain"
)

func TestCreate(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()

	// when
	_, err := rt.Create(ctx, "session1")

	// then
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}

	s, err := rt.Status(ctx, "session1")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.SessionId != "session1" {
		t.Errorf("Status().SessionId = %q, want %q", s.SessionId, "session1")
	}
	if s.Status != "initialized" {
		t.Errorf("Status().Status = %q, want %q", s.Status, "initialized")
	}
}

func TestStatusInitialized(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()
	_, _ = rt.Create(ctx, "session1")

	// when
	s, err := rt.Status(ctx, "session1")

	// then
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.Status != "initialized" {
		t.Errorf("Status().Status = %q, want %q", s.Status, "initialized")
	}
	if s.CreateTime.IsZero() {
		t.Error("Status().CreateTime is zero, want non-zero")
	}
}

func TestStatusUnknown(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()

	// when
	s, err := rt.Status(ctx, "nonexistent")

	// then
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.Status != "unknown" {
		t.Errorf("Status().Status = %q, want %q", s.Status, "unknown")
	}
	if s.SessionId != "nonexistent" {
		t.Errorf("Status().SessionId = %q, want %q", s.SessionId, "nonexistent")
	}
	if !s.CreateTime.IsZero() {
		t.Error("Status().CreateTime should be zero for unknown session")
	}
}

func TestInitMultipleSessions(t *testing.T) {
	// given
	tests := []struct {
		name       string
		sessionID  string
		wantStatus string
	}{
		{name: "session s1", sessionID: "s1", wantStatus: "initialized"},
		{name: "session s2", sessionID: "s2", wantStatus: "initialized"},
	}
	rt := NewSimpleRuntime()
	ctx := context.Background()
	_, _ = rt.Create(ctx, "s1")
	_, _ = rt.Create(ctx, "s2")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			s, err := rt.Status(ctx, tt.sessionID)

			// then
			if err != nil {
				t.Fatalf("Status() unexpected error: %v", err)
			}
			if s.SessionId != tt.sessionID {
				t.Errorf("SessionId = %q, want %q", s.SessionId, tt.sessionID)
			}
			if s.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", s.Status, tt.wantStatus)
			}
		})
	}
}

func TestConcurrentInit(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)

	// when — concurrent Create calls using different session IDs
	for i := 0; i < n; i++ {
		go func(id int) {
			defer wg.Done()
			sessionID := "session-concurrent"
			_, err := rt.Create(ctx, sessionID)
			if err != nil {
				t.Errorf("Init() unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// then — status must be "initialized" after concurrent creates
	s, err := rt.Status(ctx, "session-concurrent")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.Status != "initialized" {
		t.Errorf("Status = %q, want %q", s.Status, "initialized")
	}
}

func TestDelete(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()
	_, _ = rt.Create(ctx, "session1")

	// when
	err := rt.Delete(ctx, "session1")

	// then
	if err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
	s, err := rt.Status(ctx, "session1")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.Status != "unknown" {
		t.Errorf("Status = %q, want %q after delete", s.Status, "unknown")
	}
}

func TestDeleteIdempotent(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
	}{
		{name: "non-existent session", sessionID: "nonexistent"},
		{name: "empty session id", sessionID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rt := NewSimpleRuntime()
			ctx := context.Background()

			// when
			err := rt.Delete(ctx, tt.sessionID)

			// then
			if err != nil {
				t.Fatalf("Delete() unexpected error for %q: %v", tt.sessionID, err)
			}
		})
	}
}

func TestReceiveScreenshot_Valid(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()
	input := &domain.ScreenshotInput{
		SessionId:   "session1",
		CaptureId:   "capture-001",
		Encoding:    "PNG",
		Data:        []byte{0x89, 0x50, 0x4E, 0x47},
		WidthPx:     1920,
		HeightPx:    1080,
		ScaleFactor: 1.0,
		WindowTitle: "Test Window",
		CaptureTime: time.Now(),
	}

	// when
	receipt, err := rt.ReceiveScreenshot(ctx, input)

	// then
	if err != nil {
		t.Fatalf("ReceiveScreenshot() unexpected error: %v", err)
	}
	if receipt.AckFrameId != "capture-001" {
		t.Errorf("AckFrameId = %q, want %q", receipt.AckFrameId, "capture-001")
	}
	if receipt.Message != "screenshot received" {
		t.Errorf("Message = %q, want %q", receipt.Message, "screenshot received")
	}
}

func TestReceiveScreenshot_InvalidEncoding(t *testing.T) {
	// given
	tests := []struct {
		name     string
		encoding string
	}{
		{name: "JPEG encoding", encoding: "JPEG"},
		{name: "empty encoding", encoding: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := NewSimpleRuntime()
			ctx := context.Background()
			input := &domain.ScreenshotInput{
				SessionId: "session1",
				CaptureId: "capture-002",
				Encoding:  tt.encoding,
				Data:      []byte{0x01},
				WidthPx:   100,
				HeightPx:  100,
			}

			// when
			_, err := rt.ReceiveScreenshot(ctx, input)

			// then
			if err == nil {
				t.Fatalf("ReceiveScreenshot() encoding=%q expected error", tt.encoding)
			}
		})
	}
}

func TestReceiveScreenshot_EmptyData(t *testing.T) {
	// given
	tests := []struct {
		name string
		data []byte
	}{
		{name: "nil data", data: nil},
		{name: "empty data", data: []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := NewSimpleRuntime()
			ctx := context.Background()
			input := &domain.ScreenshotInput{
				SessionId: "session1",
				CaptureId: "capture-003",
				Encoding:  "PNG",
				Data:      tt.data,
				WidthPx:   100,
				HeightPx:  100,
			}

			// when
			_, err := rt.ReceiveScreenshot(ctx, input)

			// then
			if err == nil {
				t.Fatalf("ReceiveScreenshot() data=%v expected error", tt.data)
			}
		})
	}
}

func TestReceiveScreenshot_ZeroDimensions(t *testing.T) {
	// given
	tests := []struct {
		name     string
		widthPx  int32
		heightPx int32
	}{
		{name: "zero width", widthPx: 0, heightPx: 100},
		{name: "zero height", widthPx: 100, heightPx: 0},
		{name: "both zero", widthPx: 0, heightPx: 0},
		{name: "negative width", widthPx: -1, heightPx: 100},
		{name: "negative height", widthPx: 100, heightPx: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := NewSimpleRuntime()
			ctx := context.Background()
			input := &domain.ScreenshotInput{
				SessionId: "session1",
				CaptureId: "capture-004",
				Encoding:  "PNG",
				Data:      []byte{0x01},
				WidthPx:   tt.widthPx,
				HeightPx:  tt.heightPx,
			}

			// when
			_, err := rt.ReceiveScreenshot(ctx, input)

			// then
			if err == nil {
				t.Fatalf("ReceiveScreenshot() width=%d height=%d expected error", tt.widthPx, tt.heightPx)
			}
		})
	}
}
