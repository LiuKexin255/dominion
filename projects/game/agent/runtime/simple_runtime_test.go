package runtime

import (
	"context"
	"sync"
	"testing"
)

func TestInit(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()

	// when
	_, err := rt.Init(ctx, "session1")

	// then
	if err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
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
	_, _ = rt.Init(ctx, "session1")

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
	_, _ = rt.Init(ctx, "s1")
	_, _ = rt.Init(ctx, "s2")

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

	// when — concurrent Init calls using different session IDs
	for i := 0; i < n; i++ {
		go func(id int) {
			defer wg.Done()
			sessionID := "session-concurrent"
			_, err := rt.Init(ctx, sessionID)
			if err != nil {
				t.Errorf("Init() unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// then — status must be "initialized" after concurrent inits
	s, err := rt.Status(ctx, "session-concurrent")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.Status != "initialized" {
		t.Errorf("Status = %q, want %q", s.Status, "initialized")
	}
}
