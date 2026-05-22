package sessionclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dominion/common/gopkg/otel/tracecontext"
	session "dominion/projects/game/session"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestClientMethods(t *testing.T) {
	wantSession := &session.Session{
		Name:                "sessions/abc123",
		Type:                session.SessionType_SESSION_TYPE_SAOLEI,
		Status:              session.SessionStatus_SESSION_STATUS_ACTIVE,
		OwnerRuntimeId:      "runtime-1",
		ReconnectGeneration: 2,
	}

	tests := []struct {
		name        string
		method      string
		path        string
		statusCode  int
		response    proto.Message
		wantBody    string
		call        func(context.Context, *Client) (any, error)
		want        any
		wantErrText string
	}{
		{
			name:       "list sessions success",
			method:     http.MethodGet,
			path:       "/v1/sessions",
			statusCode: http.StatusOK,
			response: &session.ListSessionsResponse{
				Sessions: []*session.Session{wantSession},
			},
			call: func(ctx context.Context, c *Client) (any, error) {
				return c.ListSessions(ctx)
			},
			want: []*session.Session{wantSession},
		},
		{
			name:       "create session success",
			method:     http.MethodPost,
			path:       "/v1/sessions",
			statusCode: http.StatusOK,
			response: &session.CreateSessionResponse{
				Session: wantSession,
			},
			wantBody: `"type"`,
			call: func(ctx context.Context, c *Client) (any, error) {
				return c.CreateSession(ctx, "SESSION_TYPE_SAOLEI")
			},
			want: wantSession,
		},
		{
			name:       "reconnect session success",
			method:     http.MethodPost,
			path:       "/v1/sessions/abc123:reconnect",
			statusCode: http.StatusOK,
			response: &session.ReconnectSessionResponse{
				Session: wantSession,
			},
			call: func(ctx context.Context, c *Client) (any, error) {
				return c.ReconnectSession(ctx, "sessions/abc123")
			},
			want: wantSession,
		},
		{
			name:       "delete session success",
			method:     http.MethodDelete,
			path:       "/v1/sessions/abc123",
			statusCode: http.StatusOK,
			call: func(ctx context.Context, c *Client) (any, error) {
				return nil, c.DeleteSession(ctx, "sessions/abc123")
			},
		},
		{
			name:        "server error",
			method:      http.MethodGet,
			path:        "/v1/sessions",
			statusCode:  http.StatusInternalServerError,
			response:    nil,
			call:        func(ctx context.Context, c *Client) (any, error) { return c.ListSessions(ctx) },
			wantErrText: "HTTP 500: temporary failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.method {
					t.Fatalf("Method = %q, want %q", r.Method, tt.method)
				}
				if r.URL.Path != tt.path {
					t.Fatalf("Path = %q, want %q", r.URL.Path, tt.path)
				}
				if tt.wantBody != "" {
					body := make([]byte, r.ContentLength)
					if _, err := r.Body.Read(body); err != nil && err.Error() != "EOF" {
						t.Fatalf("Read request body unexpected error: %v", err)
					}
					if !strings.Contains(string(body), tt.wantBody) {
						t.Fatalf("Body = %s, want containing %s", string(body), tt.wantBody)
					}
				}
				w.WriteHeader(tt.statusCode)
				if tt.response == nil {
					if tt.wantErrText != "" {
						_, _ = w.Write([]byte("temporary failure"))
					}
					return
				}
				data, err := protojson.Marshal(tt.response)
				if err != nil {
					t.Fatalf("Marshal response unexpected error: %v", err)
				}
				_, _ = w.Write(data)
			}))
			defer server.Close()

			client := &Client{baseURL: server.URL, httpClient: server.Client()}

			// when
			got, err := tt.call(context.Background(), client)

			// then
			if tt.wantErrText != "" {
				if err == nil {
					t.Fatalf("call expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("call unexpected error: %v", err)
			}
			if tt.want != nil {
				assertEqual(t, got, tt.want)
			}
		})
	}
}

func TestClient_ListSessionsEmptyReturnsNil(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("Method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v1/sessions" {
			t.Fatalf("Path = %q, want /v1/sessions", r.URL.Path)
		}
		data, err := protojson.Marshal(&session.ListSessionsResponse{Sessions: []*session.Session{}})
		if err != nil {
			t.Fatalf("Marshal response unexpected error: %v", err)
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()

	client := &Client{baseURL: server.URL, httpClient: server.Client()}

	// when
	got, err := client.ListSessions(context.Background())

	// then
	if err != nil {
		t.Fatalf("ListSessions unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("ListSessions = %v, want nil", got)
	}
}

func TestNewClient(t *testing.T) {
	// given
	httpClient := new(http.Client)

	// when
	client := NewClient(httpClient)

	// then
	if client.baseURL != defaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", client.baseURL, defaultBaseURL)
	}
	if client.httpClient != httpClient {
		t.Fatalf("httpClient = %+v, want %+v", client.httpClient, httpClient)
	}
}

func TestNewClient_DefaultHTTPClient(t *testing.T) {
	// when
	client := NewClient(nil)

	// then
	transport, ok := client.httpClient.Transport.(*tracecontext.HTTPTransport)
	if !ok {
		t.Fatalf("httpClient.Transport = %T, want *tracecontext.HTTPTransport", client.httpClient.Transport)
	}
	_ = transport
}

func TestClient_GetSnapshot(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
		wantSnapID string
	}{
		{name: "success", statusCode: http.StatusOK, body: `{"snapshotId":"snap-1","mimeType":"image/png","image":"aW1n"}`, wantSnapID: "snap-1"},
		{name: "gateway error", statusCode: http.StatusBadGateway, body: `bad gateway`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatalf("Method = %q, want %q", r.Method, http.MethodGet)
				}
				if r.URL.Path != "/v1/sessions/s-1/game/snapshot" {
					t.Fatalf("Path = %q, want /v1/sessions/s-1/game/snapshot", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			gatewayHost := strings.TrimPrefix(server.URL, "https://")
			client := &Client{baseURL: server.URL, httpClient: server.Client()}

			// when
			got, err := client.GetSnapshot(context.Background(), gatewayHost, "sessions/s-1")

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("GetSnapshot expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("GetSnapshot unexpected error: %v", err)
			}
			if !tt.wantErr && got.GetSnapshotId() != tt.wantSnapID {
				t.Fatalf("SnapshotId = %q, want %q", got.GetSnapshotId(), tt.wantSnapID)
			}
		})
	}
}

func TestClient_DoRequestError(t *testing.T) {
	// given
	client := &Client{
		baseURL:    "http://127.0.0.1:1",
		httpClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("network down") })},
	}

	// when
	_, err := client.ListSessions(context.Background())

	// then
	if err == nil {
		t.Fatalf("ListSessions expected error")
	}
	if !strings.Contains(err.Error(), "do request") {
		t.Fatalf("error = %q, want do request", err.Error())
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func assertEqual(t *testing.T, got, want any) {
	t.Helper()
	switch g := got.(type) {
	case []*session.Session:
		w := want.([]*session.Session)
		if len(g) != len(w) {
			t.Fatalf("len(result) = %d, want %d", len(g), len(w))
		}
		for i := range g {
			if !proto.Equal(g[i], w[i]) {
				t.Fatalf("result[%d] = %v, want %v", i, g[i], w[i])
			}
		}
	case proto.Message:
		if !proto.Equal(g, want.(proto.Message)) {
			t.Fatalf("result = %v, want %v", got, want)
		}
	default:
		t.Fatalf("unexpected type %T", got)
	}
}
