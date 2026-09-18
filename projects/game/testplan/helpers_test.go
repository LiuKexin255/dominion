// Package testplan contains shared types and helpers used across the game
// integration test files.
package testplan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"

	"google.golang.org/protobuf/encoding/protojson"
)

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	headerEnv  = "env"
	pathPrefix = "/api/v1/"
	// wsReadTimeout bounds every WS/turn wait in the agent_v2 suites: a case
	// that gives up on a frame or a streamed turn must do so well inside the
	// binary's size timeout, while staying long enough to absorb the
	// slow-template windows the queued-turn cases deliberately wait out.
	wsReadTimeout = 75 * time.Second
)

// saoleiTemplateID is the saolei template path segment — the only known
// template (gameconst.SaoleiTemplate). Every resource hangs off
// "templates/saolei/...".
var saoleiTemplateID = gameconst.SaoleiTemplate.TemplateID

// ─── JSON-response types (mirroring proto messages) ─────────────────────────

// sessionResponse mirrors the Session proto message returned via gRPC-gateway
// with protojson camelCase field names. template and session_id are carried by
// the name path segments (AIP-124; specs/035-proto-contract-refine/
// data-model.md §1.1), so the JSON carries only name + createTime.
type sessionResponse struct {
	Name       string `json:"name"`
	CreateTime string `json:"createTime"`
}

// listSessionsResponse mirrors the ListSessionsResponse proto message.
type listSessionsResponse struct {
	Sessions      []sessionResponse `json:"sessions"`
	NextPageToken string            `json:"nextPageToken"`
}

// ─── General Helpers ────────────────────────────────────────────────────────

// traceContext returns a context carrying a W3C trace context for the test
// (style/large_test.md §测试用例 — set and print trace_id for log/trace
// correlation). It continues the TRACEPARENT injected by `guitar run` into
// the test process env (tools/test/guitar/pkg/run/run.go) when present, else
// starts a fresh trace; the trace_id is printed so an operator can correlate
// the test's HTTP/WS traffic in signoz.
func traceContext(t *testing.T) context.Context {
	t.Helper()
	ctx := tracecontext.FromEnv(context.Background())
	t.Logf("trace_id: %s", tracecontext.ID(ctx))
	return ctx
}

// doHTTPTrace is doHTTP with W3C traceparent propagation: the request runs
// on ctx (see traceContext) and the client transport injects the traceparent
// header from it, so the SUT's spans join the test's trace.
func doHTTPTrace(t *testing.T, ctx context.Context, method, rawurl, envName string, body []byte) (*http.Response, []byte) {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawurl, bodyReader)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext %s %s: %v", method, rawurl, err)
	}
	req.Header.Set(headerEnv, envName)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Transport: tracecontext.NewHTTPTransport(http.DefaultTransport)}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawurl, err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response %s %s: %v", method, rawurl, err)
	}

	return resp, respBody
}

// uniqueSuffix returns a short timestamp-based suffix to make resource names
// unique across test runs.
func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%10000000)
}

// doHTTP executes an HTTP request and returns the response and body.
// Calls t.Fatal on connection or read errors.
func doHTTP(t *testing.T, method, rawurl, envName string, body []byte) (*http.Response, []byte) {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, rawurl, bodyReader)
	if err != nil {
		t.Fatalf("http.NewRequest %s %s: %v", method, rawurl, err)
	}
	req.Header.Set(headerEnv, envName)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawurl, err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response %s %s: %v", method, rawurl, err)
	}

	return resp, respBody
}

// buildWSURL constructs a WebSocket URL from the HTTP endpoint by replacing
// the scheme with ws or wss.
func buildWSURL(sutHostURL, path string) string {
	u, err := url.Parse(sutHostURL)
	if err != nil {
		panic(fmt.Sprintf("parse sutHostURL %q: %v", sutHostURL, err))
	}
	host := u.Host
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, path)
}

// ─── Session Helpers (JSON-based) ───────────────────────────────────────────

// createSession sends a POST request with an empty CreateSessionRequest
// body ({}) to /api/v1/templates/{template}/sessions (AIP-133 — the parent
// template lives in the URI path) and returns the server-generated session
// ID together with the raw response body. The session ID is parsed from the
// response's name path segment (Session.session_id was removed,
// specs/035-proto-contract-refine/data-model.md §1.1). Calls t.Fatal on
// non-200 responses.
func createSession(t *testing.T, sutHostURL, sutEnvName, template string) (string, []byte) {
	t.Helper()

	reqBody := []byte("{}")
	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions", sutHostURL, pathPrefix, template)

	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, reqBody)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createSession status=%d, body=%s", resp.StatusCode, respBody)
	}

	sess := new(sessionResponse)
	if err := json.Unmarshal(respBody, sess); err != nil {
		t.Fatalf("json.Unmarshal createSession response: %v", err)
	}
	name, err := game.ParseSessionName(sess.Name)
	if err != nil {
		t.Fatalf("parse createSession response name %q: %v", sess.Name, err)
	}
	if name.SessionID == "" {
		t.Fatal("createSession: server returned empty session id in name")
	}
	return name.SessionID, respBody
}

// listSessions sends a GET request to list sessions of a template with the
// given page size and returns the raw response body. Calls t.Fatal on
// non-200 responses.
func listSessions(t *testing.T, sutHostURL, sutEnvName, template string, pageSize int) []byte {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions?page_size=%d", sutHostURL, pathPrefix, template, pageSize)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET listSessions status=%d, body=%s", resp.StatusCode, respBody)
	}
	return respBody
}

// getSessionWithStatus sends a GET request for a session and returns the HTTP
// status code and response body. Does NOT fatal on non-200 responses.
func getSessionWithStatus(t *testing.T, sutHostURL, sutEnvName, template, sessionID string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s", sutHostURL, pathPrefix, template, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	return resp.StatusCode, respBody
}

// deleteSession sends a DELETE request for a session. Does NOT fatal on
// non-200 responses.
func deleteSession(t *testing.T, sutHostURL, sutEnvName, template, sessionID string) *http.Response {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s", sutHostURL, pathPrefix, template, sessionID)
	resp, _ := doHTTP(t, http.MethodDelete, reqURL, sutEnvName, nil)

	return resp
}

// ─── Memory Helpers (proto-based, via the gateway public entry) ────────────
//
// The MemoryService (spec 039-planner-memory-calibration FR-006) is exposed
// through the gateway's public HTTP entry (gateway/cmd/main.go registers
// RegisterMemoryServiceHandler), so the large tests verify memory
// persistence/pagination through it exactly like the other services — no
// direct mongo access. The resource pattern is
// templates/{template}/sessions/{session}/memories/{memory} (FR-012).

// createMemory sends a POST to /api/v1/templates/{template}/sessions/{session}/
// memories?memory_id={memoryID} with the embedded Memory body {content}
// (AIP-133 — body "memory", memory_id from the query string). Calls t.Fatal
// on non-200 responses. ctx carries the test's W3C trace context
// (traceContext).
func createMemory(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID, memoryID, content string) *game.Memory {
	t.Helper()

	body, err := protojson.Marshal(&game.Memory{Content: content})
	if err != nil {
		t.Fatalf("protojson.Marshal Memory: %v", err)
	}

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/memories?memory_id=%s",
		sutHostURL, pathPrefix, template, sessionID, memoryID)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPost, reqURL, sutEnvName, body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createMemory status=%d, body=%s", resp.StatusCode, respBody)
	}

	created := new(game.Memory)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, created); err != nil {
		t.Fatalf("Unmarshal Memory: %v (raw: %s)", err, string(respBody))
	}
	return created
}

// listMemories sends a GET to list the memories of a session (AIP-132 +
// AIP-158 pagination: page_size/page_token/next_page_token) and returns the
// parsed ListMemoriesResponse. Calls t.Fatal on non-200 responses.
func listMemories(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID string, pageSize int, pageToken string) *game.ListMemoriesResponse {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/memories?page_size=%d",
		sutHostURL, pathPrefix, template, sessionID, pageSize)
	if pageToken != "" {
		reqURL += "&page_token=" + pageToken
	}
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET listMemories status=%d, body=%s", resp.StatusCode, respBody)
	}

	lmr := new(game.ListMemoriesResponse)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, lmr); err != nil {
		t.Fatalf("Unmarshal ListMemoriesResponse: %v (raw: %s)", err, string(respBody))
	}
	return lmr
}

// updateMemory sends a PATCH to /api/v1/templates/{template}/sessions/{session}/
// memories/{memory} with the Memory body {name, content} and update_mask
// (AIP-134 — the only mutable Memory field is content). Returns the HTTP
// status code and response body; does NOT fail on non-200 (used to assert
// the NOT_FOUND contract).
func updateMemory(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID, memoryID, content, updateMask string) (int, []byte) {
	t.Helper()

	patch := &game.Memory{
		Name:    game.MemoryName{TemplateID: template, SessionID: sessionID, MemoryID: memoryID}.String(),
		Content: content,
	}
	body, err := protojson.Marshal(patch)
	if err != nil {
		t.Fatalf("protojson.Marshal patch Memory: %v", err)
	}

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/memories/%s?update_mask=%s",
		sutHostURL, pathPrefix, template, sessionID, memoryID, updateMask)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPatch, reqURL, sutEnvName, body)
	return resp.StatusCode, respBody
}

// deleteMemory sends a DELETE for a Memory resource (AIP-135). Returns the
// HTTP status code; does NOT fail on non-200 (used to assert the NOT_FOUND
// contract).
func deleteMemory(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID, memoryID string) int {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/memories/%s",
		sutHostURL, pathPrefix, template, sessionID, memoryID)
	resp, _ := doHTTPTrace(t, ctx, http.MethodDelete, reqURL, sutEnvName, nil)
	return resp.StatusCode
}
