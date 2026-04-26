package testplan

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	session "dominion/projects/game/session"

	"google.golang.org/protobuf/encoding/protojson"
)

const (
	headerEnv         = "env"
	sessionPath       = "/v1/sessions"
	httpClientTimeout = 10 * time.Second
)

var (
	httpClient      = &http.Client{Timeout: httpClientTimeout}
	jsonMarshaler   = protojson.MarshalOptions{}
	jsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
	testSessionType = session.SessionType_SESSION_TYPE_SAOLEI
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
