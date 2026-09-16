// Package testplan contains the MEMORY module large-test suite.
//
// memory_test.go validates the MemoryService storage API end-to-end through
// the gateway's public HTTP entry: Create/Update/Delete/List (memory_id-based
// resources, resource pattern
// templates/{template}/sessions/{session}/memories/{memory}), AIP-158
// pagination (page_size/page_token/next_page_token), the AIP-193 error codes
// (ALREADY_EXISTS on duplicate memory_id, NOT_FOUND on missing
// update/delete, INVALID_ARGUMENT on a bad memory_id), and the AIP-132
// order_by face ("update_time desc" total order with a composite cursor —
// specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1/§4).
// The service uses its own mongo database `game_memory` (FR-006,
// style/mongo.md) — pinned at the unit level by the repository tests
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
	"net/url"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
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
		page := listMemories(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 2, pageToken, "")
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
	page := listMemories(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 100, "", "")
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
	page = listMemories(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 100, "", "")
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

// TestMemoryServiceHttpListOrderBy verifies the AIP-132 order_by face of
// ListMemories through the gateway public entry
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1/§4):
// "update_time desc" floats a patched entry to the front, every page is a
// total order (update_time descending, ties by memory_id ascending), the
// composite cursor continues that order across pages, and unsupported
// order_by values are rejected with 400 INVALID_ARGUMENT. The default
// memory_id ascending pagination stays pinned by
// TestMemoryServiceHttpCrudAndPagination.
func TestMemoryServiceHttpListOrderBy(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a session with five entries created in memory_id order.
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	for i := 1; i <= 5; i++ {
		createMemory(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID,
			fmt.Sprintf("mem-%d", i), fmt.Sprintf("entry %d", i))
	}

	// when: update mem-2 after a short pause, so its update_time is strictly
	// later than every create's (BSON dates carry millisecond precision).
	time.Sleep(10 * time.Millisecond)
	status, body := updateMemory(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "mem-2", "updated entry 2", "content")
	if status != http.StatusOK {
		t.Fatalf("UpdateMemory status=%d, body=%s", status, body)
	}

	// when: list with order_by=update_time desc.
	ordered := listMemories(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 100, "", "update_time desc")

	// then: every entry, the patched one first, and the page in the
	// update_time desc / memory_id asc total order.
	if got := len(ordered.GetMemories()); got != 5 {
		t.Fatalf("ListMemories(order_by=update_time desc) returned %d entries, want 5", got)
	}
	if got := ordered.GetMemories()[0].GetMemoryId(); got != "mem-2" {
		t.Errorf("ordered first entry = %q, want mem-2 (the patched entry)", got)
	}
	assertMemoryRecencyOrder(t, ordered.GetMemories())

	// when: walk the same listing with page_size=2.
	var paged []*game.Memory
	pageToken := ""
	pages := 0
	for {
		page := listMemories(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 2, pageToken, "update_time desc")
		paged = append(paged, page.GetMemories()...)
		pages++
		pageToken = page.GetNextPageToken()
		if pageToken == "" {
			break
		}
	}

	// then: 3 pages (2/2/1), each entry once, and the composite cursor
	// reproduces the single page's order exactly.
	if pages != 3 {
		t.Fatalf("ordered pagination produced %d pages, want 3 (page_size=2 over 5 entries)", pages)
	}
	if len(paged) != 5 {
		t.Fatalf("ordered pagination returned %d entries in total, want 5", len(paged))
	}
	for i, m := range paged {
		if want := ordered.GetMemories()[i].GetMemoryId(); m.GetMemoryId() != want {
			t.Errorf("ordered pagination entry %d = %q, want %q (order preserved across pages)", i, m.GetMemoryId(), want)
		}
	}

	// then: unsupported order_by values are rejected with 400 INVALID_ARGUMENT
	// (AIP-193).
	for _, orderBy := range []string{"foo", "update_time", "update_time asc"} {
		resp, respBody := doHTTPTrace(t, ctx, http.MethodGet,
			fmt.Sprintf("%s%stemplates/%s/sessions/%s/memories?order_by=%s",
				sutHostURL, pathPrefix, saoleiTemplateID, sessionID, url.QueryEscape(orderBy)),
			sutEnvName, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("ListMemories(order_by=%q) status=%d, want 400 INVALID_ARGUMENT, body=%s", orderBy, resp.StatusCode, respBody)
		}
	}
}

// assertMemoryRecencyOrder asserts the ListMemories "update_time desc" total
// order (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md
// §1): update_time is non-increasing across the page, and entries that share
// an update_time ascend on memory_id.
func assertMemoryRecencyOrder(t *testing.T, memories []*game.Memory) {
	t.Helper()
	for i := 1; i < len(memories); i++ {
		prev, cur := memories[i-1], memories[i]
		prevTime := prev.GetUpdateTime().AsTime()
		curTime := cur.GetUpdateTime().AsTime()
		if prevTime.Before(curTime) {
			t.Fatalf("entry %d (%s, update_time %v) is before entry %d (%s, update_time %v), want update_time descending",
				i-1, prev.GetMemoryId(), prevTime, i, cur.GetMemoryId(), curTime)
		}
		if prevTime.Equal(curTime) && prev.GetMemoryId() >= cur.GetMemoryId() {
			t.Fatalf("tied entries at update_time %v are out of memory_id order: %q before %q, want ascending",
				prevTime, prev.GetMemoryId(), cur.GetMemoryId())
		}
	}
}
