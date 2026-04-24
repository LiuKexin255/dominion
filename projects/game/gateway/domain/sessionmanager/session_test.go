package sessionmanager

import (
	"errors"
	"sync"
	"testing"

	"dominion/projects/game/gateway/domain"
)

func TestManager_GetOrCreateRuntime(t *testing.T) {
	// given
	m := NewManager("gw-0")

	t.Run("creates new runtime", func(t *testing.T) {
		// when
		rt := m.GetOrCreateRuntime("session-1")

		// then
		if rt == nil {
			t.Fatal("expected non-nil runtime")
		}
		if rt.SessionID != "session-1" {
			t.Fatalf("SessionID = %q, want %q", rt.SessionID, "session-1")
		}
		if rt.GatewayID != "gw-0" {
			t.Fatalf("GatewayID = %q, want %q", rt.GatewayID, "gw-0")
		}
		if rt.AgentConn != nil {
			t.Fatal("AgentConn should be nil for new runtime")
		}
		if rt.InflightOp != nil {
			t.Fatal("InflightOp should be nil for new runtime")
		}
	})

	t.Run("returns existing runtime on second call", func(t *testing.T) {
		// given - already created above
		first := m.GetOrCreateRuntime("session-1")

		// when
		second := m.GetOrCreateRuntime("session-1")

		// then - same pointer, gatewayID unchanged
		if second != first {
			t.Fatal("expected same runtime instance")
		}
		if second.GatewayID != "gw-0" {
			t.Fatalf("GatewayID = %q, want original %q", second.GatewayID, "gw-0")
		}
	})

	t.Run("GetRuntime returns nil for unknown session", func(t *testing.T) {
		// when
		rt := m.GetRuntime("nonexistent")

		// then
		if rt != nil {
			t.Fatal("expected nil for unknown session")
		}
	})

	t.Run("GetRuntime returns existing session", func(t *testing.T) {
		// given
		created := m.GetOrCreateRuntime("session-2")

		// when
		got := m.GetRuntime("session-2")

		// then
		if got != created {
			t.Fatal("expected same runtime instance")
		}
	})
}

func TestManager_RegisterAgent(t *testing.T) {
	// given
	m := NewManager("gw-0")
	m.GetOrCreateRuntime("session-1")

	conn := &domain.AgentConnection{ConnID: "agent-conn-1"}

	t.Run("first registration succeeds", func(t *testing.T) {
		// when
		err := m.RegisterAgent("session-1", conn)

		// then
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rt := m.GetRuntime("session-1")
		if rt.AgentConn == nil {
			t.Fatal("AgentConn should not be nil after registration")
		}
		if rt.AgentConn.ConnID != "agent-conn-1" {
			t.Fatalf("ConnID = %q, want %q", rt.AgentConn.ConnID, "agent-conn-1")
		}
	})

	t.Run("second registration returns ErrAgentAlreadyConnected", func(t *testing.T) {
		// when
		conn2 := &domain.AgentConnection{ConnID: "agent-conn-2"}
		err := m.RegisterAgent("session-1", conn2)

		// then
		if !errors.Is(err, domain.ErrAgentAlreadyConnected) {
			t.Fatalf("error = %v, want ErrAgentAlreadyConnected", err)
		}
	})

	t.Run("unknown session returns ErrSessionNotFound", func(t *testing.T) {
		// when
		err := m.RegisterAgent("nonexistent", conn)

		// then
		if !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("error = %v, want ErrSessionNotFound", err)
		}
	})
}

func TestManager_UnregisterAgent(t *testing.T) {
	// given
	m := NewManager("gw-0")
	m.GetOrCreateRuntime("session-1")
	m.RegisterAgent("session-1", &domain.AgentConnection{ConnID: "agent-1"})
	m.GetRuntime("session-1").InflightOp = &domain.InflightOperation{
		OperationID: "op-1",
		Kind:        domain.OperationKindMouseClick,
	}

	// when
	err := m.UnregisterAgent("session-1")

	// then
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rt := m.GetRuntime("session-1")
	if rt.AgentConn != nil {
		t.Fatal("AgentConn should be nil after unregister")
	}
	if rt.InflightOp != nil {
		t.Fatal("InflightOp should be nil after unregister")
	}
	if rt.LastError != "agent disconnected" {
		t.Fatalf("LastError = %q, want %q", rt.LastError, "agent disconnected")
	}

	t.Run("unknown session returns ErrSessionNotFound", func(t *testing.T) {
		err := m.UnregisterAgent("nonexistent")
		if !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("error = %v, want ErrSessionNotFound", err)
		}
	})
}

func TestManager_AddRemoveWebConn(t *testing.T) {
	// given
	m := NewManager("gw-0")
	m.GetOrCreateRuntime("session-1")

	conn1 := &domain.WebConnection{ConnID: "web-1"}
	conn2 := &domain.WebConnection{ConnID: "web-2"}
	conn3 := &domain.WebConnection{ConnID: "web-3"}

	t.Run("add multiple connections", func(t *testing.T) {
		// when
		m.AddWebConn("session-1", conn1)
		m.AddWebConn("session-1", conn2)
		m.AddWebConn("session-1", conn3)

		// then
		rt := m.GetRuntime("session-1")
		if len(rt.WebConns) != 3 {
			t.Fatalf("len(WebConns) = %d, want 3", len(rt.WebConns))
		}
	})

	t.Run("remove middle connection", func(t *testing.T) {
		// when
		m.RemoveWebConn("session-1", "web-2")

		// then
		rt := m.GetRuntime("session-1")
		if len(rt.WebConns) != 2 {
			t.Fatalf("len(WebConns) = %d, want 2", len(rt.WebConns))
		}
		hasWeb1 := false
		hasWeb3 := false
		for _, c := range rt.WebConns {
			if c.ConnID == "web-1" {
				hasWeb1 = true
			}
			if c.ConnID == "web-3" {
				hasWeb3 = true
			}
			if c.ConnID == "web-2" {
				t.Fatal("web-2 should have been removed")
			}
		}
		if !hasWeb1 || !hasWeb3 {
			t.Fatal("web-1 and web-3 should still be present")
		}
	})

	t.Run("remove nonexistent connID is no-op", func(t *testing.T) {
		// when
		m.RemoveWebConn("session-1", "nonexistent")

		// then
		rt := m.GetRuntime("session-1")
		if len(rt.WebConns) != 2 {
			t.Fatalf("len(WebConns) = %d, want 2", len(rt.WebConns))
		}
	})

	t.Run("remove from unknown session is no-op", func(t *testing.T) {
		// when - should not panic
		m.RemoveWebConn("nonexistent", "web-1")
	})

	t.Run("add to unknown session returns ErrSessionNotFound", func(t *testing.T) {
		err := m.AddWebConn("nonexistent", conn1)
		if !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("error = %v, want ErrSessionNotFound", err)
		}
	})
}

func TestManager_ConcurrentAccess(t *testing.T) {
	// given
	m := NewManager("gw-0")
	m.GetOrCreateRuntime("session-1")

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	// when - 50 goroutines try to register agent concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conn := &domain.AgentConnection{ConnID: "agent-conn"}
			err := m.RegisterAgent("session-1", conn)
			if err != nil && !errors.Is(err, domain.ErrAgentAlreadyConnected) {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	// then - no unexpected errors
	for err := range errs {
		t.Fatalf("unexpected error during concurrent RegisterAgent: %v", err)
	}

	// when - concurrent AddWebConn and UnregisterAgent
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conn := &domain.WebConnection{ConnID: "web-concurrent"}
			_ = m.AddWebConn("session-1", conn)
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.UnregisterAgent("session-1")
		}()
	}

	wg.Wait()

	// then - no panic or data race (detected by race detector)
	rt := m.GetRuntime("session-1")
	if rt == nil {
		t.Fatal("runtime should still exist after concurrent access")
	}
}