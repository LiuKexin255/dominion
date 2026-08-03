// Package testplan contains session-level integration tests.
// This file is compiled as its own test binary for the session suite.
package testplan

import (
	"encoding/json"
	"testing"

	"dominion/common/gopkg/testtool"
)

// TestCreateSession verifies that a session can be created successfully via
// POST /api/v1/templates/{template}/sessions with an empty body (AIP-133 —
// the parent template lives in the URI path, spec 031-team-template-mode
// contracts/api-contract.md §2.1) and that the server returns a non-empty,
// server-generated sessionId.
func TestCreateSession(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: empty CreateSessionRequest

	// when
	sessionID, body := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)

	// then
	if sessionID == "" {
		t.Error("createSession returned empty sessionId")
	}

	// verify the response body contains the expected template-scoped name
	sess := new(sessionResponse)
	if err := json.Unmarshal(body, sess); err != nil {
		t.Fatalf("json.Unmarshal session response: %v", err)
	}
	wantName := "templates/" + saoleiTemplateID + "/sessions/" + sessionID
	if sess.Name != wantName {
		t.Errorf("session name = %q, want %q", sess.Name, wantName)
	}
}

// TestListSessions verifies that listing sessions of a template returns all
// created sessions and that nextPageToken is empty when all sessions fit in
// one page.
func TestListSessions(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: create two sessions
	sess1ID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	sess2ID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)

	// when: list sessions with large page size to avoid pagination from prior tests
	respBody := listSessions(t, sutHostURL, sutEnvName, saoleiTemplateID, 100)

	// then: both sessions are in the response
	got := new(listSessionsResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("json.Unmarshal listSessions response: %v", err)
	}

	found1 := false
	found2 := false
	for _, s := range got.Sessions {
		if s.SessionID == sess1ID {
			found1 = true
		}
		if s.SessionID == sess2ID {
			found2 = true
		}
	}
	if !found1 {
		t.Errorf("session %q not found in list result", sess1ID)
	}
	if !found2 {
		t.Errorf("session %q not found in list result", sess2ID)
	}
	if got.NextPageToken != "" && len(got.Sessions) < 100 {
		t.Errorf("next_page_token = %q, want empty (got %d sessions)", got.NextPageToken, len(got.Sessions))
	}
}
