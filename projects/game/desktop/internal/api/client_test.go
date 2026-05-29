package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestClient_CreateSession
// ---------------------------------------------------------------------------

func TestClient_CreateSession(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			sessionID:  "test-session",
			statusCode: http.StatusOK,
			respBody:   `{"name":"sessions/test-session","session_id":"test-session","create_time":"2024-01-01T00:00:00Z"}`,
			wantErr:    false,
		},
		{
			name:       "server error",
			sessionID:  "bad",
			statusCode: http.StatusInternalServerError,
			respBody:   "internal error",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: mock server that validates request and returns canned response
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/api/v1/sessions" {
					t.Errorf("expected /api/v1/sessions, got %s", r.URL.Path)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
				}

				body, _ := io.ReadAll(r.Body)
				var payload map[string]string
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to parse request body: %v", err)
				}
				if payload["session_id"] != tt.sessionID {
					t.Errorf("expected session_id %q in body, got %q", tt.sessionID, payload["session_id"])
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when: call CreateSession
			session, err := client.CreateSession(tt.sessionID)

			// then: verify result
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "create session") {
					t.Errorf("error should contain 'create session', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if session == nil {
				t.Fatal("expected session, got nil")
			}
			if session.SessionID != tt.sessionID {
				t.Errorf("expected session_id %q, got %q", tt.sessionID, session.SessionID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_GetSession
// ---------------------------------------------------------------------------

func TestClient_GetSession(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			sessionID:  "test123",
			statusCode: http.StatusOK,
			respBody:   `{"name":"sessions/test123","session_id":"test123","create_time":"2024-01-01T00:00:00Z"}`,
			wantErr:    false,
		},
		{
			name:       "not found",
			sessionID:  "missing",
			statusCode: http.StatusNotFound,
			respBody:   `{"error":"not found"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				wantPath := "/api/v1/sessions/" + tt.sessionID
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			session, err := client.GetSession(tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if session.SessionID != tt.sessionID {
				t.Errorf("expected session_id %q, got %q", tt.sessionID, session.SessionID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_DeleteSession
// ---------------------------------------------------------------------------

func TestClient_DeleteSession(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			sessionID:  "del-me",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "server error",
			sessionID:  "del-fail",
			statusCode: http.StatusInternalServerError,
			respBody:   "internal error",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("expected DELETE, got %s", r.Method)
				}
				wantPath := "/api/v1/sessions/" + tt.sessionID
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			err := client.DeleteSession(tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_CreateAgent
// ---------------------------------------------------------------------------

func TestClient_CreateAgent(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			sessionID:  "sess-1",
			statusCode: http.StatusOK,
			respBody:   `{"name":"sessions/sess-1/agent","session_id":"sess-1","owner_index":0,"owner":"user","create_time":"2024-01-01T00:00:00Z"}`,
			wantErr:    false,
		},
		{
			name:       "server error",
			sessionID:  "sess-bad",
			statusCode: http.StatusInternalServerError,
			respBody:   "failed",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				wantPath := "/api/v1/sessions/" + tt.sessionID + "/agent"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}

				body, _ := io.ReadAll(r.Body)
				var payload map[string]interface{}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to parse request body: %v", err)
				}
				if _, ok := payload["agent"]; !ok {
					t.Errorf("expected body to contain 'agent' key, got %v", payload)
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			agent, err := client.CreateAgent(tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if agent == nil {
				t.Fatal("expected agent, got nil")
			}
			if agent.SessionID != tt.sessionID {
				t.Errorf("expected session_id %q, got %q", tt.sessionID, agent.SessionID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_GetAgent
// ---------------------------------------------------------------------------

func TestClient_GetAgent(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			sessionID:  "sess-1",
			statusCode: http.StatusOK,
			respBody:   `{"name":"sessions/sess-1/agent","session_id":"sess-1","owner_index":0,"owner":"user","create_time":"2024-01-01T00:00:00Z"}`,
			wantErr:    false,
		},
		{
			name:       "not found",
			sessionID:  "no-agent",
			statusCode: http.StatusNotFound,
			respBody:   "not found",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				wantPath := "/api/v1/sessions/" + tt.sessionID + "/agent"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			agent, err := client.GetAgent(tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if agent == nil {
				t.Fatal("expected agent, got nil")
			}
			if agent.SessionID != tt.sessionID {
				t.Errorf("expected session_id %q, got %q", tt.sessionID, agent.SessionID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_DeleteAgent
// ---------------------------------------------------------------------------

func TestClient_DeleteAgent(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			sessionID:  "sess-1",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "server error",
			sessionID:  "sess-bad",
			statusCode: http.StatusInternalServerError,
			respBody:   "failed",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("expected DELETE, got %s", r.Method)
				}
				wantPath := "/api/v1/sessions/" + tt.sessionID + "/agent"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			err := client.DeleteAgent(tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_URLTrailingSlash
// ---------------------------------------------------------------------------

func TestClient_URLTrailingSlash(t *testing.T) {
	// given: config with trailing slash in GatewayURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "//") {
			t.Errorf("URL path contains double slash: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name":"sessions/ts","session_id":"ts","create_time":"2024-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	client := NewClient(Config{GatewayURL: srv.URL + "/"})

	// when: make a request
	session, err := client.GetSession("ts")

	// then: no double slash, request succeeds
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.SessionID != "ts" {
		t.Errorf("expected session_id %q, got %q", "ts", session.SessionID)
	}
}

// ---------------------------------------------------------------------------
// TestClient_EnvHeader
// ---------------------------------------------------------------------------

func TestClient_EnvHeader(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		wantEnv string
	}{
		{
			name:    "env header set",
			env:     "production",
			wantEnv: "production",
		},
		{
			name:    "env header empty",
			env:     "",
			wantEnv: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.Header.Get("env")
				if got != tt.wantEnv {
					t.Errorf("expected env header %q, got %q", tt.wantEnv, got)
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"name":"sessions/e","session_id":"e","create_time":"2024-01-01T00:00:00Z"}`))
			}))
			defer srv.Close()

			client := NewClient(Config{
				GatewayURL: srv.URL,
				Env:        tt.env,
			})

			// when
			_, err := client.GetSession("e")

			// then
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
