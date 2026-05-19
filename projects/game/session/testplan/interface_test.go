package testplan

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	session "dominion/projects/game/session"

	"google.golang.org/protobuf/encoding/protojson"
)

const (
	headerEnv              = "env"
	sessionPath            = "/v1/sessions"
	minReconnectGeneration = int64(1)
	httpClientTimeout      = 10 * time.Second
)

var (
	httpClient      = &http.Client{Timeout: httpClientTimeout}
	jsonMarshaler   = protojson.MarshalOptions{}
	jsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
	testSessionType = session.SessionType_SESSION_TYPE_SAOLEI
	testInvalidType = session.SessionType_SESSION_TYPE_UNSPECIFIED
)

func uniqueSessionID() string {
	return fmt.Sprintf("session-%d", time.Now().UnixNano())
}

func doRequest(t *testing.T, method, url string, body io.Reader) *http.Response {
	t.Helper()

	sutEnvName := testtool.MustEnv()

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("http.NewRequest(%s, %s) unexpected error: %v", method, url, err)
	}
	req.Header.Set(headerEnv, sutEnvName)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("http.Do(%s, %s) unexpected error: %v", method, url, err)
	}

	return resp
}

func decodeSession(t *testing.T, resp *http.Response) *session.Session {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() unexpected error: %v", err)
	}

	got := new(session.Session)
	if err := jsonUnmarshaler.Unmarshal(body, got); err != nil {
		t.Fatalf("protojson.Unmarshal(Session) unexpected error: %v, body=%s", err, string(body))
	}

	return got
}

func decodeCreateSessionResponse(t *testing.T, resp *http.Response) *session.CreateSessionResponse {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() unexpected error: %v", err)
	}

	got := new(session.CreateSessionResponse)
	if err := jsonUnmarshaler.Unmarshal(body, got); err != nil {
		t.Fatalf("protojson.Unmarshal(CreateSessionResponse) unexpected error: %v, body=%s", err, string(body))
	}

	return got
}

func decodeReconnectSessionResponse(t *testing.T, resp *http.Response) *session.ReconnectSessionResponse {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() unexpected error: %v", err)
	}

	got := new(session.ReconnectSessionResponse)
	if err := jsonUnmarshaler.Unmarshal(body, got); err != nil {
		t.Fatalf("protojson.Unmarshal(ReconnectSessionResponse) unexpected error: %v, body=%s", err, string(body))
	}

	return got
}

func decodeListSessionsResponse(t *testing.T, resp *http.Response) *session.ListSessionsResponse {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() unexpected error: %v", err)
	}

	got := new(session.ListSessionsResponse)
	if err := jsonUnmarshaler.Unmarshal(body, got); err != nil {
		t.Fatalf("protojson.Unmarshal(ListSessionsResponse) unexpected error: %v, body=%s", err, string(body))
	}

	return got
}

func createSession(t *testing.T, sutHostURL string) *session.CreateSessionResponse {
	t.Helper()

	sessionID := uniqueSessionID()
	wantName := "sessions/" + sessionID

	reqBody, err := jsonMarshaler.Marshal(&session.CreateSessionRequest{
		Type:      testSessionType,
		SessionId: sessionID,
	})
	if err != nil {
		t.Fatalf("protojson.Marshal(CreateSessionRequest) unexpected error: %v", err)
	}

	resp := doRequest(t, http.MethodPost, sutHostURL+sessionPath, bytes.NewReader(reqBody))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("CreateSession status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	created := decodeCreateSessionResponse(t, resp)
	if created.GetSession() == nil {
		t.Fatal("CreateSession response missing session")
	}
	if created.GetSession().GetName() != wantName {
		t.Fatalf("CreateSession session.Name = %q, want %q", created.GetSession().GetName(), wantName)
	}

	return created
}

func deleteSessionForCleanup(t *testing.T, sutHostURL, name string) {
	t.Helper()

	resp := doRequest(t, http.MethodDelete, sutHostURL+"/v1/"+name, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("cleanup DeleteSession status = %d, want one of [%d, %d], body=%s", resp.StatusCode, http.StatusOK, http.StatusNotFound, string(body))
	}
}

func requireValidSession(t *testing.T, got *session.Session, wantName string) {
	t.Helper()

	if got == nil {
		t.Fatal("session is nil")
	}
	if got.GetName() != wantName {
		t.Fatalf("session.Name = %q, want %q", got.GetName(), wantName)
	}
	if got.GetType() != testSessionType {
		t.Fatalf("session.Type = %v, want %v", got.GetType(), testSessionType)
	}
	if got.GetGatewayId() == "" {
		t.Fatal("session.GatewayId is empty, want non-empty")
	}
	if got.GetCreateTime() == nil {
		t.Fatal("session.CreateTime is empty, want non-empty")
	}
	if got.GetUpdateTime() == nil {
		t.Fatal("session.UpdateTime is empty, want non-empty")
	}
}

func requireAgentConnectURL(t *testing.T, got string) {
	t.Helper()

	if got == "" {
		t.Fatal("agentConnectUrl is empty, want non-empty")
	}
	if !strings.Contains(got, "token=") {
		t.Fatalf("agentConnectUrl = %q, want token query", got)
	}
}

func TestInterface_CreateSession(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := createSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	requireValidSession(t, created.GetSession(), created.GetSession().GetName())
	requireAgentConnectURL(t, created.GetSession().GetAgentConnectUrl())
	if created.GetSession().GetReconnectGeneration() != 0 {
		t.Fatalf("session.ReconnectGeneration = %d, want 0", created.GetSession().GetReconnectGeneration())
	}
	if created.GetSession().GetStatus() == session.SessionStatus_SESSION_STATUS_UNSPECIFIED {
		t.Fatal("session.Status is unspecified, want non-zero")
	}
}

func TestInterface_GetSession(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := createSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	resp := doRequest(t, http.MethodGet, sutHostURL+"/v1/"+created.GetSession().GetName(), nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GetSession status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	got := decodeSession(t, resp)
	requireValidSession(t, got, created.GetSession().GetName())
	if got.GetReconnectGeneration() != created.GetSession().GetReconnectGeneration() {
		t.Fatalf("session.ReconnectGeneration = %d, want %d", got.GetReconnectGeneration(), created.GetSession().GetReconnectGeneration())
	}
}

func TestInterface_ReconnectSession(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := createSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	reqBody, err := jsonMarshaler.Marshal(&session.ReconnectSessionRequest{
		Name: created.GetSession().GetName(),
	})
	if err != nil {
		t.Fatalf("protojson.Marshal(ReconnectSessionRequest) unexpected error: %v", err)
	}

	resp := doRequest(t, http.MethodPost, sutHostURL+"/v1/"+created.GetSession().GetName()+":reconnect", bytes.NewReader(reqBody))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("ReconnectSession status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	got := decodeReconnectSessionResponse(t, resp)
	requireValidSession(t, got.GetSession(), created.GetSession().GetName())
	requireAgentConnectURL(t, got.GetSession().GetAgentConnectUrl())
	if got.GetSession().GetReconnectGeneration() < created.GetSession().GetReconnectGeneration() {
		t.Fatalf("session.ReconnectGeneration = %d, want >= %d", got.GetSession().GetReconnectGeneration(), created.GetSession().GetReconnectGeneration())
	}
}

func TestInterface_DeleteSession(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := createSession(t, sutHostURL)

	resp := doRequest(t, http.MethodDelete, sutHostURL+"/v1/"+created.GetSession().GetName(), nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("DeleteSession status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	getResp := doRequest(t, http.MethodGet, sutHostURL+"/v1/"+created.GetSession().GetName(), nil)
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(getResp.Body)
		t.Fatalf("GetSession after delete status = %d, want %d, body=%s", getResp.StatusCode, http.StatusNotFound, string(body))
	}
}

func TestInterface_GetSession_NotFound(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	resp := doRequest(t, http.MethodGet, sutHostURL+"/v1/sessions/nonexistent", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GetSession status = %d, want %d, body=%s", resp.StatusCode, http.StatusNotFound, string(body))
	}
}

func TestInterface_CreateSession_InvalidType(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	reqBody, err := jsonMarshaler.Marshal(&session.CreateSessionRequest{
		Type: testInvalidType,
	})
	if err != nil {
		t.Fatalf("protojson.Marshal(CreateSessionRequest) unexpected error: %v", err)
	}

	resp := doRequest(t, http.MethodPost, sutHostURL+sessionPath, bytes.NewReader(reqBody))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("CreateSession invalid type status = %d, want %d, body=%s", resp.StatusCode, http.StatusBadRequest, string(body))
	}
}

func TestInterface_ListSessions(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created1 := createSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created1.GetSession().GetName())

	created2 := createSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created2.GetSession().GetName())

	resp := doRequest(t, http.MethodGet, sutHostURL+sessionPath, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("ListSessions status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	listResp := decodeListSessionsResponse(t, resp)
	if len(listResp.GetSessions()) < 2 {
		t.Fatalf("ListSessions returned %d sessions, want at least 2", len(listResp.GetSessions()))
	}

	for _, s := range listResp.GetSessions() {
		if s.GetAgentConnectUrl() == "" {
			t.Fatalf("session %q has empty AgentConnectUrl, want non-empty", s.GetName())
		}
	}

	name1 := created1.GetSession().GetName()
	name2 := created2.GetSession().GetName()
	hasName := func(list []*session.Session, name string) bool {
		for _, s := range list {
			if s.GetName() == name {
				return true
			}
		}
		return false
	}

	if !hasName(listResp.GetSessions(), name1) {
		t.Fatalf("ListSessions missing session %q", name1)
	}
	if !hasName(listResp.GetSessions(), name2) {
		t.Fatalf("ListSessions missing session %q", name2)
	}

	deleteSessionForCleanup(t, sutHostURL, name1)

	resp2 := doRequest(t, http.MethodGet, sutHostURL+sessionPath, nil)
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("ListSessions after delete status = %d, want %d, body=%s", resp2.StatusCode, http.StatusOK, string(body))
	}

	listResp2 := decodeListSessionsResponse(t, resp2)
	if hasName(listResp2.GetSessions(), name1) {
		t.Fatalf("ListSessions after delete still contains deleted session %q", name1)
	}
	if !hasName(listResp2.GetSessions(), name2) {
		t.Fatalf("ListSessions after delete missing remaining session %q", name2)
	}
}
