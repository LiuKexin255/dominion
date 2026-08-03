// Package testplan contains session-level integration tests.
// This file is compiled as its own test binary for the session suite.
package testplan

import (
	"encoding/json"
	"fmt"
	"testing"

	"dominion/common/gopkg/testtool"
)

// TestCreateSession verifies that a session can be created successfully via
// POST /api/v1/templates/{template}/sessions with an empty body (AIP-133 —
// the parent template lives in the URI path, spec 031-team-template-mode
// contracts/api-contract.md §2.1) and that the server returns a name with
// the expected template-scoped format. The session id and template are
// carried by the name path segments only (Session.template/session_id were
// removed, specs/035-proto-contract-refine/data-model.md §1.1).
func TestCreateSession(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: empty CreateSessionRequest

	// when
	sessionID, body := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)

	// then: the session id parsed from the name is non-empty
	if sessionID == "" {
		t.Error("createSession returned empty session id in name")
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
// one page. Sessions are matched by their name (the session id is a name
// path segment, not a JSON field — specs/035-proto-contract-refine/
// data-model.md §1.1).
func TestListSessions(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: create two sessions
	sess1ID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	sess2ID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	want1 := fmt.Sprintf("templates/%s/sessions/%s", saoleiTemplateID, sess1ID)
	want2 := fmt.Sprintf("templates/%s/sessions/%s", saoleiTemplateID, sess2ID)

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
		if s.Name == want1 {
			found1 = true
		}
		if s.Name == want2 {
			found2 = true
		}
	}
	if !found1 {
		t.Errorf("session %q not found in list result", want1)
	}
	if !found2 {
		t.Errorf("session %q not found in list result", want2)
	}
	if got.NextPageToken != "" && len(got.Sessions) < 100 {
		t.Errorf("next_page_token = %q, want empty (got %d sessions)", got.NextPageToken, len(got.Sessions))
	}
}
