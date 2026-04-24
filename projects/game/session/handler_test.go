package session

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"dominion/projects/game/pkg/token"
	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/runtime/gateway"
	"dominion/projects/game/session/runtime/storage"
	"dominion/projects/game/session/service"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testSecret   = "test-secret-key-for-handler-tests"
	testTokenTTL = 10 * time.Minute
)

// sessionName returns the resource name in "sessions/{id}" format.
func sessionName(id string) string {
	return "sessions/" + id
}

// newTestHandler creates a handler with a FakeStore, real HMACSigner, and StaticRegistry
// using the provided gateway IDs.
func newTestHandler(gatewayIDs ...string) (*Handler, *storage.FakeStore) {
	repo := storage.NewFakeStore()
	issuer := token.NewHMACSigner(testSecret, testTokenTTL)
	assignments := make([]*gateway.Assignment, len(gatewayIDs))
	for i, id := range gatewayIDs {
		assignments[i] = &gateway.Assignment{
			GatewayID:  id,
			Index:      i,
			PublicHost: fmt.Sprintf("gateway-%d-game.liukexin.com", i),
		}
	}
	registry := gateway.NewStaticRegistry(assignments)
	svc := service.NewSessionService(repo, issuer, registry)
	return NewHandler(svc), repo
}

// seedSession creates a domain session in Active state with a gateway assigned,
// persists it, and returns the resource name.
func seedSession(t *testing.T, repo domain.Repository, sessionID, gatewayID string) string {
	t.Helper()

	session, err := domain.NewSession(domain.TypeSaolei, sessionID)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	session.SetGatewayID(gatewayID)
	if err := session.MarkActive(); err != nil {
		t.Fatalf("MarkActive() error = %v", err)
	}
	if err := repo.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	return sessionName(sessionID)
}

// seedDisconnectedSession creates a session that is active then disconnected.
func seedDisconnectedSession(t *testing.T, repo domain.Repository, sessionID, gatewayID string) string {
	t.Helper()

	session, err := domain.NewSession(domain.TypeSaolei, sessionID)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	session.SetGatewayID(gatewayID)
	if err := session.MarkActive(); err != nil {
		t.Fatalf("MarkActive() error = %v", err)
	}
	if err := session.MarkDisconnected(); err != nil {
		t.Fatalf("MarkDisconnected() error = %v", err)
	}
	if err := repo.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	return sessionName(sessionID)
}

func TestHandler_GetSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		request        *GetSessionRequest
		wantCode       codes.Code
		wantNamePrefix string
		wantType       SessionType
		wantGatewayID  string
	}{
		{
			name:           "given a created session, when GetSession called, returns proto Session with matching fields",
			request:        &GetSessionRequest{Name: sessionName("session-1")},
			wantCode:       codes.OK,
			wantNamePrefix: "sessions/",
			wantType:       SessionType_SESSION_TYPE_SAOLEI,
			wantGatewayID:  "gw-0",
		},
		{
			name:     "given no session, when GetSession called, returns NotFound gRPC error",
			request:  &GetSessionRequest{Name: sessionName("nonexistent")},
			wantCode: codes.NotFound,
		},
		{
			name:     "given empty name, when GetSession called, returns InvalidArgument",
			request:  &GetSessionRequest{Name: ""},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name without sessions prefix, when GetSession called, returns InvalidArgument",
			request:  &GetSessionRequest{Name: "invalid-name"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name with empty ID, when GetSession called, returns InvalidArgument",
			request:  &GetSessionRequest{Name: "sessions/"},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler, repo := newTestHandler("gw-0", "gw-1")
			if tt.wantCode == codes.OK {
				seedSession(t, repo, "session-1", "gw-0")
			}

			// when
			got, err := handler.GetSession(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			if !strings.HasPrefix(got.GetName(), tt.wantNamePrefix) {
				t.Fatalf("GetSession() name = %q, want prefix %q", got.GetName(), tt.wantNamePrefix)
			}
			if got.GetType() != tt.wantType {
				t.Fatalf("GetSession() type = %v, want %v", got.GetType(), tt.wantType)
			}
			if got.GetGatewayId() != tt.wantGatewayID {
				t.Fatalf("GetSession() gateway_id = %q, want %q", got.GetGatewayId(), tt.wantGatewayID)
			}
			if got.GetCreateTime() == nil {
				t.Fatal("GetSession() create_time is nil, want non-nil")
			}
			if got.GetUpdateTime() == nil {
				t.Fatal("GetSession() update_time is nil, want non-nil")
			}
		})
	}
}

func TestHandler_CreateSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		request        *CreateSessionRequest
		gatewayIDs     []string
		wantCode       codes.Code
		wantType       SessionType
		wantGatewayID  string
		wantSessionID  string
		wantIDNonEmpty bool
	}{
		{
			name: "given valid type SAOLEI, when CreateSession called, returns session and agent_connect_url",
			request: &CreateSessionRequest{
				Type: SessionType_SESSION_TYPE_SAOLEI,
			},
			gatewayIDs:     []string{"gw-0"},
			wantCode:       codes.OK,
			wantType:       SessionType_SESSION_TYPE_SAOLEI,
			wantGatewayID:  "gw-0",
			wantIDNonEmpty: true,
		},
		{
			name: "given valid type with session_id, when CreateSession called, returns session with provided ID",
			request: &CreateSessionRequest{
				Type:      SessionType_SESSION_TYPE_SAOLEI,
				SessionId: "my-custom-id",
			},
			gatewayIDs:    []string{"gw-1"},
			wantCode:      codes.OK,
			wantType:      SessionType_SESSION_TYPE_SAOLEI,
			wantGatewayID: "gw-1",
			wantSessionID: "my-custom-id",
		},
		{
			name: "given UNSPECIFIED type, when CreateSession called, returns InvalidArgument",
			request: &CreateSessionRequest{
				Type: SessionType_SESSION_TYPE_UNSPECIFIED,
			},
			gatewayIDs: []string{"gw-0"},
			wantCode:   codes.InvalidArgument,
		},
		{
			name: "given no gateway available, when CreateSession called, returns Internal",
			request: &CreateSessionRequest{
				Type: SessionType_SESSION_TYPE_SAOLEI,
			},
			gatewayIDs: nil,
			wantCode:   codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			gwIDs := tt.gatewayIDs
			if gwIDs == nil {
				gwIDs = []string{}
			}
			handler, _ := newTestHandler(gwIDs...)

			// when
			got, err := handler.CreateSession(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			session := got.GetSession()
			if session == nil {
				t.Fatal("CreateSession() session is nil")
			}
			if session.GetType() != tt.wantType {
				t.Fatalf("CreateSession() type = %v, want %v", session.GetType(), tt.wantType)
			}
			if session.GetGatewayId() != tt.wantGatewayID {
				t.Fatalf("CreateSession() gateway_id = %q, want %q", session.GetGatewayId(), tt.wantGatewayID)
			}
			if tt.wantSessionID != "" {
				wantName := sessionName(tt.wantSessionID)
				if session.GetName() != wantName {
					t.Fatalf("CreateSession() name = %q, want %q", session.GetName(), wantName)
				}
			}
			if tt.wantIDNonEmpty {
				if session.GetName() == "" {
					t.Fatal("CreateSession() name is empty, want auto-generated")
				}
				if !strings.HasPrefix(session.GetName(), "sessions/") {
					t.Fatalf("CreateSession() name = %q, want 'sessions/' prefix", session.GetName())
				}
			}
			connectURL := got.GetAgentConnectUrl()
			if connectURL == "" {
				t.Fatal("CreateSession() agent_connect_url is empty")
			}
			parsedURL, err := url.Parse(connectURL)
			if err != nil {
				t.Fatalf("CreateSession() agent_connect_url parse error = %v", err)
			}
			if parsedURL.Scheme != "wss" {
				t.Fatalf("CreateSession() agent_connect_url scheme = %q, want wss", parsedURL.Scheme)
			}
			if parsedURL.Host == "" {
				t.Fatal("CreateSession() agent_connect_url host is empty")
			}
			if !strings.HasPrefix(parsedURL.Host, "gateway-") || !strings.HasSuffix(parsedURL.Host, "-game.liukexin.com") {
				t.Fatalf("CreateSession() agent_connect_url host = %q, want pattern gateway-{index}-game.liukexin.com", parsedURL.Host)
			}
			sessionID := strings.TrimPrefix(session.GetName(), "sessions/")
			wantPath := "/v1/sessions/" + sessionID + "/game/connect"
			if parsedURL.Path != wantPath {
				t.Fatalf("CreateSession() agent_connect_url path = %q, want %q", parsedURL.Path, wantPath)
			}
			token := parsedURL.Query().Get("token")
			if token == "" {
				t.Fatalf("CreateSession() agent_connect_url token is empty")
			}
		})
	}
}

func TestHandler_DeleteSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		seedName string
		request  *DeleteSessionRequest
		wantCode codes.Code
	}{
		{
			name:     "given existing session, when DeleteSession called, returns Empty",
			seedName: sessionName("session-1"),
			request:  &DeleteSessionRequest{Name: sessionName("session-1")},
			wantCode: codes.OK,
		},
		{
			name:     "given non-existent session, when DeleteSession called, returns NotFound",
			request:  &DeleteSessionRequest{Name: sessionName("missing")},
			wantCode: codes.NotFound,
		},
		{
			name:     "given empty name, when DeleteSession called, returns InvalidArgument",
			request:  &DeleteSessionRequest{Name: ""},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name without sessions prefix, when DeleteSession called, returns InvalidArgument",
			request:  &DeleteSessionRequest{Name: "invalid-name"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name with empty ID, when DeleteSession called, returns InvalidArgument",
			request:  &DeleteSessionRequest{Name: "sessions/"},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler, repo := newTestHandler("gw-0")
			if tt.seedName != "" {
				seedSession(t, repo, "session-1", "gw-0")
			}

			// when
			got, err := handler.DeleteSession(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}
			if got == nil {
				t.Fatal("DeleteSession() response is nil, want empty proto")
			}
		})
	}
}

func TestHandler_ReconnectSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		seedSessionID  string
		seedGatewayID  string
		seedDisconnect bool
		request        *ReconnectSessionRequest
		gatewayIDs     []string
		wantCode       codes.Code
		wantGatewayID  string
	}{
		{
			name:           "given existing session, when ReconnectSession called, returns new URL and session",
			seedSessionID:  "session-1",
			seedGatewayID:  "gw-0",
			seedDisconnect: true,
			request:        &ReconnectSessionRequest{Name: sessionName("session-1")},
			gatewayIDs:     []string{"gw-0", "gw-1"},
			wantCode:       codes.OK,
			wantGatewayID:  "gw-1",
		},
		{
			name:       "given non-existent session, when ReconnectSession called, returns NotFound",
			request:    &ReconnectSessionRequest{Name: sessionName("missing")},
			gatewayIDs: []string{"gw-0"},
			wantCode:   codes.NotFound,
		},
		{
			name:       "given empty name, when ReconnectSession called, returns InvalidArgument",
			request:    &ReconnectSessionRequest{Name: ""},
			gatewayIDs: []string{"gw-0"},
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "given name without sessions prefix, when ReconnectSession called, returns InvalidArgument",
			request:    &ReconnectSessionRequest{Name: "invalid-name"},
			gatewayIDs: []string{"gw-0"},
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "given name with empty ID, when ReconnectSession called, returns InvalidArgument",
			request:    &ReconnectSessionRequest{Name: "sessions/"},
			gatewayIDs: []string{"gw-0"},
			wantCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler, repo := newTestHandler(tt.gatewayIDs...)
			if tt.seedSessionID != "" {
				if tt.seedDisconnect {
					seedDisconnectedSession(t, repo, tt.seedSessionID, tt.seedGatewayID)
				} else {
					seedSession(t, repo, tt.seedSessionID, tt.seedGatewayID)
				}
			}

			// when
			got, err := handler.ReconnectSession(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			session := got.GetSession()
			if session == nil {
				t.Fatal("ReconnectSession() session is nil")
			}
			if session.GetGatewayId() != tt.wantGatewayID {
				t.Fatalf("ReconnectSession() gateway_id = %q, want %q", session.GetGatewayId(), tt.wantGatewayID)
			}
			if session.GetStatus() != SessionStatus_SESSION_STATUS_ACTIVE {
				t.Fatalf("ReconnectSession() status = %v, want ACTIVE", session.GetStatus())
			}
			if session.GetReconnectGeneration() != 1 {
				t.Fatalf("ReconnectSession() reconnect_generation = %d, want 1", session.GetReconnectGeneration())
			}
			connectURL := got.GetAgentConnectUrl()
			if connectURL == "" {
				t.Fatal("ReconnectSession() agent_connect_url is empty")
			}
			parsedURL, err := url.Parse(connectURL)
			if err != nil {
				t.Fatalf("ReconnectSession() agent_connect_url parse error = %v", err)
			}
			if parsedURL.Scheme != "wss" {
				t.Fatalf("ReconnectSession() agent_connect_url scheme = %q, want wss", parsedURL.Scheme)
			}
			if parsedURL.Host == "" {
				t.Fatal("ReconnectSession() agent_connect_url host is empty")
			}
			if !strings.HasPrefix(parsedURL.Host, "gateway-") || !strings.HasSuffix(parsedURL.Host, "-game.liukexin.com") {
				t.Fatalf("ReconnectSession() agent_connect_url host = %q, want pattern gateway-{index}-game.liukexin.com", parsedURL.Host)
			}
			sessionID := strings.TrimPrefix(session.GetName(), "sessions/")
			wantPath := "/v1/sessions/" + sessionID + "/game/connect"
			if parsedURL.Path != wantPath {
				t.Fatalf("ReconnectSession() agent_connect_url path = %q, want %q", parsedURL.Path, wantPath)
			}
			token := parsedURL.Query().Get("token")
			if token == "" {
				t.Fatalf("ReconnectSession() agent_connect_url token is empty")
			}
		})
	}
}

// assertStatusCode checks that the error matches the expected gRPC status code.
func assertStatusCode(t *testing.T, err error, want codes.Code) {
	t.Helper()

	if want == codes.OK {
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		return
	}

	if err == nil {
		t.Fatalf("error = nil, want code %v", want)
	}
	if status.Code(err) != want {
		t.Fatalf("status.Code() = %v, want %v", status.Code(err), want)
	}
}
