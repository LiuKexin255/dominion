// Package testplan contains the MEMORY module large-test suite.
//
// memory_test.go validates the planner long-term memory data plane
// end-to-end (specs/039-planner-memory-calibration US2 — FR-006..FR-012,
// FR-018/FR-020):
//
//   - TestMemoryServiceHttpCrudAndPagination — the MemoryService storage API
//     through the gateway's public HTTP entry: Create/Update/Delete/List
//     (memory_id-based resources, resource pattern
//     templates/{template}/sessions/{session}/memories/{memory}, FR-012),
//     AIP-158 pagination (page_size/page_token/next_page_token), and the
//     AIP-193 error codes (ALREADY_EXISTS on duplicate memory_id, NOT_FOUND
//     on missing update/delete, INVALID_ARGUMENT on a bad memory_id). The
//     service uses its own mongo database `game_memory` (FR-006,
//     style/mongo.md) — pinned at the unit level by the repository tests
//     (projects/game/memory/runtime/mongo/repository_test.go — DI seam) and
//     by the service wiring (memory/cmd/main.go); the large test verifies
//     the durable behaviour (entries survive across requests/sessions)
//     through the public entry, never touching mongo directly. Cross-process
//     restart durability is covered by the repository/handler unit tests +
//     the persistence assertions here (a large-test restart of the memory
//     service is not schedulable in the shared deployment topology).
//   - TestPlannerMemoryToolFlow — the game-driven planner flow: the review
//     node's `memory` tool (hermes-style single tool — action/content/
//     old_text/operations, NO memory_id/target, FR-008) converts agent-side
//     into MemoryService RPCs (batch add → CreateMemory with an
//     agent-generated id; replace → ListMemories + old_text substring
//     location), persists the entries (verified via ListMemories through the
//     gateway), and returns error TEXT feedback for the 0-hit / multi-hit
//     old_text cases (with the current entries, no memory_id — FR-011 pure
//     content). The player partition never sees the memory tool (FR-009).
//
// Organised by MODULE per style/large_test.md (not by spec/scenario id);
// it reuses the shared helpers in helpers_test.go (setupTeamSession /
// playTeamGameUntilWait / the memory HTTP helpers). Trace context: every
// test sets and prints a trace_id (traceContext) and propagates it into the
// SUT requests/WS dials (style/large_test.md §测试用例).
package testplan

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
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

// TestPlannerMemoryToolFlow verifies the game-driven planner memory flow
// (specs/039-planner-memory-calibration FR-008/FR-011/FR-018): after a game
// end, the review node's single hermes-style `memory` tool runs the
// deterministic fixture chain — a BATCH add (operations form), a replace
// with a 0-hit old_text, a replace with a multi-hit old_text, then the
// instruct_player review instruction. The test asserts:
//
//  1. The `memory` tool_calls carry hermes-style args only — action/content/
//     old_text/operations, NO memory_id and NO target parameters (FR-008).
//  2. The agent-side conversion persisted the entries: ListMemories (through
//     the gateway public entry) returns the two added contents — the batch
//     add generated internal ids and called CreateMemory (the LLM never
//     supplies ids).
//  3. The old_text 0-hit / multi-hit errors are returned as TEXT tool results
//     (not thrown — 031 C15 neutral status) whose bodies carry the current
//     entries as PURE CONTENT (no memory_id prefixes — FR-011) so the LLM
//     can re-pick a more specific substring (FR-008).
//  4. The player partition carries no memory tool_call (FR-009 — the memory
//     tool is planner-only).
//
// The frozen-snapshot half (FR-010/FR-011: pure-content SystemMessage
// injection, freeze until the compression boundary) is NOT directly
// observable from the WS/HTTP surface — the snapshot is filtered out of the
// channel write-back (contract §3) — and is pinned by the unit tests
// (team/memory-snapshot.test.ts, team/planner.test.ts, team/graph.test.ts)
// plus the fake-llm "system prompt received" INFO logs (operator-side
// signoz; the trace_id printed by traceContext correlates the run).
func TestPlannerMemoryToolFlow(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "mem-flow-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game — the first operate click's terminal reply ends
	// the game and the planner runs the memory review chain.
	frames := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then (1): the memory tool_calls carry the hermes-style parameters —
	// the batch operations form, the replace action with old_text/content —
	// and NEVER memory_id/target (FR-008).
	memoryCalls := 0
	for _, f := range frames {
		for _, p := range frameMessageParts(f).GetParts() {
			tc := p.GetToolCall()
			if tc == nil || tc.GetName() != "memory" {
				continue
			}
			memoryCalls++
			args := tc.GetArgsJson()
			if strings.Contains(args, "memory_id") || strings.Contains(args, "target") {
				t.Errorf("memory tool_call args_json = %q — must NOT carry memory_id/target (FR-008)", args)
			}
			if strings.Contains(args, "operations") {
				// The batch add form (FR-008 — operations array).
				if !strings.Contains(args, expectedPlannerMemoryE1) || !strings.Contains(args, expectedPlannerMemoryE2) {
					t.Errorf("memory batch args_json = %q, want to contain both fixture entries %q / %q",
						args, expectedPlannerMemoryE1, expectedPlannerMemoryE2)
				}
			} else {
				// The replace form: action + old_text + content.
				if !strings.Contains(args, `"action":"replace"`) || !strings.Contains(args, "old_text") {
					t.Errorf("memory replace args_json = %q, want action=replace + old_text (FR-008)", args)
				}
			}
		}
	}
	if memoryCalls != 3 {
		t.Errorf("memory tool_calls = %d, want exactly 3 (batch add + 0-hit replace + multi-hit replace — FR-008)", memoryCalls)
	}

	// then (2): the entries are PERSISTED via the agent's conversion — the
	// batch add generated internal memory_ids and called CreateMemory; the
	// LLM-visible surface never exposed an id (ListMemories contents are
	// pure content — FR-011). The memory_ids are content digests
	// (generateMemoryId), so the ListMemories order is compared as a set.
	contents := listMemoryContents(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	if len(contents) != 2 {
		t.Fatalf("ListMemories = %d entries, want 2 (the batch add persisted both entries — FR-006/FR-008)", len(contents))
	}
	if !slices.Contains(contents, expectedPlannerMemoryE1) || !slices.Contains(contents, expectedPlannerMemoryE2) {
		t.Errorf("ListMemories contents = %q, want both fixture entries %q and %q", contents, expectedPlannerMemoryE1, expectedPlannerMemoryE2)
	}

	// then (3): the 0-hit / multi-hit old_text errors came back as TEXT tool
	// results carrying the current entries as pure content (no memory_id
	// prefixes — FR-011) so the LLM can re-pick a more specific substring
	// (FR-008).
	var zeroHit, multiHit string
	for _, f := range frames {
		for _, p := range frameMessageParts(f).GetParts() {
			tr := p.GetToolResult()
			if tr == nil {
				continue
			}
			msg := tr.GetMessage()
			switch {
			case strings.Contains(msg, "memory: no entry matched"):
				zeroHit = msg
			case strings.Contains(msg, "multiple entries matched"):
				multiHit = msg
			}
		}
	}
	if zeroHit == "" {
		t.Error("no 0-hit old_text error tool_result — the replace with a non-matching substring was not rejected (FR-008)")
	} else {
		if !strings.Contains(zeroHit, expectedPlannerMemoryE1) || !strings.Contains(zeroHit, expectedPlannerMemoryE2) {
			t.Errorf("0-hit error = %q, want the current entries %q / %q for the LLM to re-pick (FR-008)", zeroHit, expectedPlannerMemoryE1, expectedPlannerMemoryE2)
		}
		if strings.Contains(zeroHit, "memory_id") {
			t.Errorf("0-hit error = %q — the current entries must render as pure content, no memory_id prefixes (FR-011)", zeroHit)
		}
	}
	if multiHit == "" {
		t.Error("no multi-hit old_text error tool_result — the replace with a shared substring was not rejected (FR-008)")
	} else {
		if !strings.Contains(multiHit, "Be more specific") {
			t.Errorf("multi-hit error = %q, want the 'be more specific' guidance (FR-008)", multiHit)
		}
		if strings.Contains(multiHit, "memory_id") {
			t.Errorf("multi-hit error = %q — the hit previews must render as pure content, no memory_id prefixes (FR-011)", multiHit)
		}
	}

	// then (4): the player partition carries no memory tool_call (FR-009 —
	// the memory tool is planner-only; the review stays invisible to the
	// player, FR-017).
	playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	for _, m := range playerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if name == "memory" {
				t.Errorf("player partition carries memory tool_call %q — the memory tool is planner-only (FR-009)", name)
			}
		}
	}
}
