package sessionclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"dominion/common/gopkg/otel/tracecontext"
	gw "dominion/projects/game/gateway"
	session "dominion/projects/game/session"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const defaultBaseURL = "https://game.liukexin.com"

// Client calls the session service REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a session service REST client.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Transport: tracecontext.NewHTTPTransport(http.DefaultTransport),
		}
	}
	return &Client{
		baseURL:    defaultBaseURL,
		httpClient: httpClient,
	}
}

// ListSessions returns all sessions visible to the caller.
func (c *Client) ListSessions(ctx context.Context) ([]*session.Session, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/sessions", nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	result := new(session.ListSessionsResponse)
	if err := c.do(req, result); err != nil {
		return nil, err
	}
	if len(result.Sessions) == 0 {
		return nil, nil
	}
	return result.Sessions, nil
}

// CreateSession creates a session of the requested type.
func (c *Client) CreateSession(ctx context.Context, sessionType string) (*session.Session, error) {
	body, err := protojson.Marshal(&session.CreateSessionRequest{
		Type: parseSessionType(sessionType),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal create request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	result := new(session.CreateSessionResponse)
	if err := c.do(req, result); err != nil {
		return nil, err
	}
	return result.Session, nil
}

// ReconnectSession reconnects an existing session by resource name.
func (c *Client) ReconnectSession(ctx context.Context, name string) (*session.Session, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/"+name+":reconnect", nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	result := new(session.ReconnectSessionResponse)
	if err := c.do(req, result); err != nil {
		return nil, err
	}
	return result.Session, nil
}

// DeleteSession deletes an existing session by resource name.
func (c *Client) DeleteSession(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/v1/"+name, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	return c.do(req, nil)
}

// GetSnapshot fetches the latest game snapshot from the gateway.
func (c *Client) GetSnapshot(ctx context.Context, gatewayHost, sessionName string) (*gw.GameSnapshot, error) {
	snapshotURL := fmt.Sprintf("https://%s/v1/%s/game/snapshot", gatewayHost, sessionName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snapshotURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	result := new(gw.GameSnapshot)
	if err := c.do(req, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) do(req *http.Request, result any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read error response: %w", err)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	if result == nil {
		return nil
	}

	msg, ok := result.(proto.Message)
	if !ok {
		return fmt.Errorf("unexpected result type %T, expected proto message", result)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	return protojsonUnmarshaler.Unmarshal(body, msg)
}

var protojsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}

// parseSessionType converts a frontend session type string to a proto enum.
// It accepts both enum names (e.g. "SESSION_TYPE_SAOLEI") and short names (e.g. "saolei").
func parseSessionType(s string) session.SessionType {
	// Try exact match first (enum name).
	if v, ok := session.SessionType_value[s]; ok {
		return session.SessionType(v)
	}
	return session.SessionType_SESSION_TYPE_UNSPECIFIED
}
