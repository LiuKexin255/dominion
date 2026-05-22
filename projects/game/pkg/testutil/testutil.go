// Package testutil provides minimal test helpers for game system testing.
//
// It offers reusable patterns extracted from game testplans:
//   - Session CRUD via the public gateway REST endpoint
//   - WebSocket dialing to the game connect endpoint
//   - Token parsing for routing extracts
//
// This package intentionally excludes business test semantics and deploy
// config reading; those belong to higher-level testplan packages.
package testutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"dominion/projects/game/pkg/token"
	session "dominion/projects/game/session"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	sessionPath       = "/v1/sessions"
	httpClientTimeout = 10 * time.Second
)

var (
	httpClient      = &http.Client{Timeout: httpClientTimeout}
	jsonMarshaler   = protojson.MarshalOptions{}
	jsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
	// dummySigner is a token signer used solely for ParseRoutingClaims,
	// which only decodes and unmarshals the payload without verification.
	dummySigner = token.NewHMACSigner("", 0)
)

// NewTestSessionID generates a unique session ID for testing prefixed with
// "test-".
func NewTestSessionID() string {
	return fmt.Sprintf("test-%d", time.Now().UnixNano())
}

// CreateSession creates a new session via the public gateway endpoint and
// returns the created Session.
func CreateSession(ctx context.Context, endpoint string, sessionType string, sessionID string) (*session.Session, error) {
	reqBody, err := jsonMarshaler.Marshal(&session.CreateSessionRequest{
		Type:      parseSessionType(sessionType),
		SessionId: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal create request: %w", err)
	}

	resp, err := doRequest(ctx, http.MethodPost, endpoint+sessionPath, reqBody)
	if err != nil {
		return nil, err
	}

	var result session.CreateSessionResponse
	if err := decodeResponse(resp, &result); err != nil {
		return nil, err
	}
	if result.Session == nil {
		return nil, fmt.Errorf("create response missing session")
	}
	return result.Session, nil
}

// DeleteSession deletes a session via the public gateway endpoint.
func DeleteSession(ctx context.Context, endpoint string, sessionName string) error {
	resp, err := doRequest(ctx, http.MethodDelete, endpoint+"/v1/"+sessionName, nil)
	if err != nil {
		return err
	}
	return decodeResponse(resp, nil)
}

// ReconnectSession reconnects a session via the public gateway endpoint and
// returns the updated Session.
func ReconnectSession(ctx context.Context, endpoint string, sessionName string) (*session.Session, error) {
	resp, err := doRequest(ctx, http.MethodPost, endpoint+"/v1/"+sessionName+":reconnect", nil)
	if err != nil {
		return nil, err
	}

	var result session.ReconnectSessionResponse
	if err := decodeResponse(resp, &result); err != nil {
		return nil, err
	}
	if result.Session == nil {
		return nil, fmt.Errorf("reconnect response missing session")
	}
	return result.Session, nil
}

// DialWebSocket dials a WebSocket connection to the game connect endpoint.
// The url should be a full WebSocket URL including the token query parameter,
// for example:
//
//	ws://gateway.test/v1/sessions/session-1/game/connect?token=...
func DialWebSocket(ctx context.Context, url string) (*websocket.Conn, *http.Response, error) {
	return websocket.Dial(ctx, url, nil)
}

// ParseSessionToken extracts the owner_runtime_id from a session connection
// token. It decodes the token payload without cryptographic verification,
// suitable for test assertions.
func ParseSessionToken(tokenStr string) (ownerRuntimeID string, err error) {
	claims, err := dummySigner.ParseRoutingClaims(tokenStr)
	if err != nil {
		return "", fmt.Errorf("parse session token: %w", err)
	}
	if claims.OwnerRuntimeID == "" {
		return "", fmt.Errorf("token missing owner_runtime_id")
	}
	return claims.OwnerRuntimeID, nil
}

// doRequest creates and executes an HTTP request.
func doRequest(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	return resp, nil
}

// decodeResponse reads the response body, checks the status, and optionally
// unmarshals the JSON body into the given proto message.
func decodeResponse(resp *http.Response, msg proto.Message) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	if msg != nil {
		if err := jsonUnmarshaler.Unmarshal(body, msg); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
	}
	return nil
}

// parseSessionType converts a session type string to the proto enum value.
// It accepts both full enum names (e.g. "SESSION_TYPE_SAOLEI") and returns
// SESSION_TYPE_UNSPECIFIED for unknown values.
func parseSessionType(s string) session.SessionType {
	if v, ok := session.SessionType_value[s]; ok {
		return session.SessionType(v)
	}
	return session.SessionType_SESSION_TYPE_UNSPECIFIED
}
