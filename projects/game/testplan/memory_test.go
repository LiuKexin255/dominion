// Package testplan contains the MEMORY module large-test suite.
//
// memory_test.go validates the MemoryService storage API end-to-end through
// the gateway's public HTTP entry (TestMemoryServiceHttpCrudAndPagination):
// Create/Update/Delete/List (memory_id-based resources, resource pattern
// templates/{template}/sessions/{session}/memories/{memory}), AIP-158
// pagination (page_size/page_token/next_page_token), and the AIP-193 error
// codes (ALREADY_EXISTS on duplicate memory_id, NOT_FOUND on missing
// update/delete, INVALID_ARGUMENT on a bad memory_id). The service uses its
// own mongo database `game_memory` (FR-006, style/mongo.md) — pinned at the
// unit level by the repository tests
// (projects/game/memory/runtime/mongo/repository_test.go — DI seam) and by
// the service wiring (memory/cmd/main.go); the large test verifies the
// durable behaviour (entries survive across requests/sessions) through the
// public entry, never touching mongo directly.
//
// Organised by MODULE per style/large_test.md (not by spec/scenario id);
// it reuses the shared HTTP helpers in helpers_test.go. Trace context: the
// test sets and prints a trace_id (traceContext) and propagates it into the
// SUT requests (style/large_test.md §测试用例).
package testplan

import (
	"fmt"
	"net/http"
	"testing"

	"dominion/common/gopkg/testtool"
)

// TestMemoryServiceHttpCrudAndPagination verifies the MemoryService storage
// API + AIP-158 pagination through the gateway public entry
// (specs/039-planner-memory-calibration contracts/memory-service-contract.md
// §2/§6): create/get/list/update/delete of memory_id-based resources, the
// unique-identity ALREADY_EXISTS rejection, NOT_FOUND on missing
// update/delete, INVALID_ARGUMENT on a memory_id outside [a-z0-9_-], and
// cursor pagination (page_size/page_token/next_page_token — the repository
// sorts by memory_id ascending and pages with limit=pageSize+1).
func TestMemoryServiceHttpCrudAndPagination(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a session as the memory parent (the memory resource hangs off
	// templates/{template}/sessions/{session}/memories — FR-012).
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)

	// when: create a memory via POST /memories?memory_id=... with the
	// embedded {content} body (AIP-133).
	created := createMemory(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "mem-1", "player 常犯边角误标")

	// then: the resource carries the full AIP-122 name, the memory_id
	// segment and the content (contract §1).
	wantName := fmt.Sprintf("templates/%s/sessions/%s/memories/mem-1", saoleiTemplateID, sessionID)
	if created.GetName() != wantName {
		t.Errorf("created Name = %q, want %q", created.GetName(), wantName)
	}
	if created.GetMemoryId() != "mem-1" {
		t.Errorf("created memory_id = %q, want mem-1", created.GetMemoryId())
	}
	if created.GetContent() != "player 常犯边角误标" {
		t.Errorf("created content = %q, want the posted content", created.GetContent())
	}
	if created.GetCreateTime() == nil || created.GetUpdateTime() == nil {
		t.Error("created create_time/update_time are nil — the server must manage the timestamps (OUTPUT_ONLY)")
	}

	// then: a duplicate memory_id is rejected with 409 ALREADY_EXISTS
	// (AIP-133/FR-008 conflict rejection).
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPost,
		fmt.Sprintf("%s%stemplates/%s/sessions/%s/memories?memory_id=mem-1",
			sutHostURL, pathPrefix, saoleiTemplateID, sessionID), sutEnvName,
		[]byte(`{"content":"duplicate"}`))
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate CreateMemory status=%d, want 409 ALREADY_EXISTS, body=%s", resp.StatusCode, respBody)
	}

	// given: four more entries so the session holds 5 (pagination fixture).
	for i := 2; i <= 5; i++ {
		createMemory(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID,
			fmt.Sprintf("mem-%d", i), fmt.Sprintf("entry %d", i))
	}

	// when: walk the list with page_size=2 (AIP-158 cursor pagination).
	var pages [][]string
	pageToken := ""
	for {
		page := listMemories(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 2, pageToken)
		var contents []string
		for _, m := range page.GetMemories() {
			contents = append(contents, m.GetContent())
		}
		pages = append(pages, contents)
		pageToken = page.GetNextPageToken()
		if pageToken == "" {
			break
		}
	}

	// then: 3 pages of 2/2/1 (limit+1 cursor), all 5 entries surfaced once,
	// sorted by memory_id ascending (repository sort).
	if len(pages) != 3 {
		t.Fatalf("pagination produced %d pages, want 3 (page_size=2 over 5 entries)", len(pages))
	}
	if len(pages[0]) != 2 || len(pages[1]) != 2 || len(pages[2]) != 1 {
		t.Errorf("page sizes = %d/%d/%d, want 2/2/1", len(pages[0]), len(pages[1]), len(pages[2]))
	}
	wantFirstPage := []string{"player 常犯边角误标", "entry 2"}
	if pages[0][0] != wantFirstPage[0] || pages[0][1] != wantFirstPage[1] {
		t.Errorf("page 1 contents = %q, want %q (sorted by memory_id ascending)", pages[0], wantFirstPage)
	}
	allContents := append(append([]string{}, pages[0]...), pages[1]...)
	allContents = append(allContents, pages[2]...)
	if len(allContents) != 5 {
		t.Fatalf("pagination returned %d entries total, want 5", len(allContents))
	}

	// when: update mem-1's content via PATCH + update_mask (AIP-134).
	status, body := updateMemory(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "mem-1", "player 常犯节奏过快", "content")
	if status != http.StatusOK {
		t.Fatalf("UpdateMemory status=%d, body=%s", status, body)
	}

	// then: the list reflects the update (the same memory_id entry now
	// carries the new content — the unique identity was preserved).
	page := listMemories(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 100, "")
	if got := page.GetMemories()[0].GetContent(); got != "player 常犯节奏过快" {
		t.Errorf("updated content = %q, want the patched content", got)
	}

	// then: update/delete of a MISSING memory returns 404 NOT_FOUND
	// (AIP-134/135 — no create-or-update).
	if status, _ := updateMemory(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "mem-nope", "x", "content"); status != http.StatusNotFound {
		t.Errorf("UpdateMemory(missing) status=%d, want 404 NOT_FOUND", status)
	}
	if status := deleteMemory(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "mem-nope"); status != http.StatusNotFound {
		t.Errorf("DeleteMemory(missing) status=%d, want 404 NOT_FOUND", status)
	}

	// when: delete mem-1, then list again.
	if status := deleteMemory(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "mem-1"); status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("DeleteMemory status=%d, want 200 or 204", status)
	}
	page = listMemories(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 100, "")
	if got := len(page.GetMemories()); got != 4 {
		t.Errorf("ListMemories after delete = %d entries, want 4", got)
	}

	// then: an invalid memory_id (outside [a-z0-9_-]) is rejected with 400
	// INVALID_ARGUMENT (AIP-193 — contract §6 charset validation).
	resp, respBody = doHTTPTrace(t, ctx, http.MethodPost,
		fmt.Sprintf("%s%stemplates/%s/sessions/%s/memories?memory_id=Bad%%20ID",
			sutHostURL, pathPrefix, saoleiTemplateID, sessionID), sutEnvName,
		[]byte(`{"content":"x"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("CreateMemory(bad memory_id) status=%d, want 400 INVALID_ARGUMENT, body=%s", resp.StatusCode, respBody)
	}
}
