// Package testplan contains the WIKI module large-test suite.
//
// wiki_test.go validates the WikiService storage and search API end-to-end
// through the gateway's public HTTP entry: Create/Get/Update/Delete
// (name-addressed resources, resource pattern
// templates/{template}/sessions/{session}/wikis/{wiki}), the scope-unique
// normalized title (ALREADY_EXISTS on a case/full-width variant), List in the
// fixed wiki_id ascending order with AIP-158 cursor pagination, and SearchWikis
// (normalized substring matching with exact > prefix > substring ranking, the
// single-rune regex fallback, and query-bound offset pagination)
// (spec 068 FR-001~005;
// specs/068-wiki-round-pause/contracts/wiki-service.md §2-4).
//
// Organised by MODULE per style/large_test.md (not by spec/scenario id); it
// reuses the shared HTTP helpers in helpers_test.go and the session helper.
// Trace context: the test sets and prints a trace_id (traceContext) and
// propagates it into the SUT requests (style/large_test.md §测试用例).
package testplan

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"

	"google.golang.org/protobuf/encoding/protojson"
)

// TestWikiServiceHttpCrud verifies the WikiService resource lifecycle through
// the gateway public entry: Create (wiki_id + embedded title/content), Get,
// Update via update_mask (title-only and content-only), Delete, the
// scope-unique normalized title conflict (409), NOT_FOUND on a missing
// get/delete, and INVALID_ARGUMENT on a bad wiki_id.
func TestWikiServiceHttpCrud(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a session as the wiki parent
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)

	// when: create a wiki via POST /wikis?wiki_id=... with the embedded body
	created, createdBody := createWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1", "Alpha 策略", "正文一")

	// then: the resource name's {wiki} segment carries the id, and the
	// response has no separate id field (FR-017)
	wantName := fmt.Sprintf("templates/%s/sessions/%s/wikis/wiki-1", saoleiTemplateID, sessionID)
	if created.GetName() != wantName {
		t.Errorf("created Name = %q, want %q", created.GetName(), wantName)
	}
	if created.GetTitle() != "Alpha 策略" || created.GetContent() != "正文一" {
		t.Errorf("created = %+v, want the posted title/content", created)
	}
	if created.GetCreateTime() == nil || created.GetUpdateTime() == nil {
		t.Error("created create_time/update_time are nil — the server must manage the timestamps (OUTPUT_ONLY)")
	}
	if strings.Contains(string(createdBody), "wikiId") {
		t.Errorf("created body = %s, must not carry a wikiId field (FR-017)", createdBody)
	}

	// then: a full-width, upper-case spelling of the existing title is
	// rejected with 409 ALREADY_EXISTS (scope-unique normalized title)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPost,
		fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis?wiki_id=wiki-2",
			sutHostURL, pathPrefix, saoleiTemplateID, sessionID), sutEnvName,
		[]byte(`{"title":"ＡＬＰＨＡ 策略","content":"duplicate"}`))
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate-title CreateWiki status=%d, want 409 ALREADY_EXISTS, body=%s", resp.StatusCode, respBody)
	}

	// when: get the created resource
	got, status := getWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1")

	// then
	if status != http.StatusOK {
		t.Fatalf("GetWiki status=%d, want 200", status)
	}
	if got.GetContent() != "正文一" {
		t.Errorf("GetWiki content = %q, want the created content", got.GetContent())
	}

	// when: update the title only via update_mask
	if status, body := updateWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1", "Beta 策略", "", "title"); status != http.StatusOK {
		t.Fatalf("UpdateWiki(title) status=%d, body=%s", status, body)
	}

	// then: the title changed and the content was preserved
	got, _ = getWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1")
	if got.GetTitle() != "Beta 策略" || got.GetContent() != "正文一" {
		t.Errorf("after title update = %+v, want the new title with preserved content", got)
	}

	// when: update the content only via update_mask
	if status, body := updateWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1", "", "正文二", "content"); status != http.StatusOK {
		t.Fatalf("UpdateWiki(content) status=%d, body=%s", status, body)
	}

	// then: both fields reflect the updates
	got, _ = getWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1")
	if got.GetTitle() != "Beta 策略" || got.GetContent() != "正文二" {
		t.Errorf("after content update = %+v, want both updates applied", got)
	}

	// when: delete, then get and delete again
	if status := deleteWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1"); status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("DeleteWiki status=%d, want 200 or 204", status)
	}
	if _, status := getWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1"); status != http.StatusNotFound {
		t.Errorf("GetWiki after delete status=%d, want 404 NOT_FOUND", status)
	}
	if status := deleteWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1"); status != http.StatusNotFound {
		t.Errorf("DeleteWiki(missing) status=%d, want 404 NOT_FOUND", status)
	}

	// then: an invalid wiki_id (outside [a-z0-9_-]) is rejected with 400
	// INVALID_ARGUMENT
	status, body := createWikiRaw(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "Bad ID", "t", "c")
	if status != http.StatusBadRequest {
		t.Errorf("CreateWiki(bad wiki_id) status=%d, want 400 INVALID_ARGUMENT, body=%s", status, body)
	}
}

// TestWikiServiceHttpListOrderAndPagination verifies ListWikis through the
// gateway public entry: the fixed wiki_id ascending order, AIP-158 cursor
// pagination (page_size/page_token/next_page_token), full resources (content
// included), page_size coercion above the maximum, and the negative page_size
// rejection.
func TestWikiServiceHttpListOrderAndPagination(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a session with four wikis created out of wiki_id order
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	for _, id := range []string{"delta", "alpha", "charlie", "bravo"} {
		createWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, id, "title-"+id, "content-"+id)
	}

	// when: first page with page_size=2
	page1 := listWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 2, "")

	// then: wiki_id ascending, full content, next token present
	assertListedWikiTitles(t, page1.GetWikis(), []string{"title-alpha", "title-bravo"})
	for i, wiki := range page1.GetWikis() {
		if wiki.GetContent() == "" {
			t.Errorf("ListWikis()[%d] content is empty — List returns full resources", i)
		}
	}
	if page1.GetNextPageToken() == "" {
		t.Fatal("ListWikis() page 1 next_page_token is empty, want non-empty")
	}

	// when: second page with the token
	page2 := listWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 2, page1.GetNextPageToken())

	// then: the remaining two entries, no further token
	assertListedWikiTitles(t, page2.GetWikis(), []string{"title-charlie", "title-delta"})
	if page2.GetNextPageToken() != "" {
		t.Errorf("ListWikis() page 2 next_page_token = %q, want empty", page2.GetNextPageToken())
	}

	// then: a page_size above the maximum is coerced, not rejected
	all := listWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 500, "")
	if got := len(all.GetWikis()); got != 4 {
		t.Errorf("ListWikis(page_size=500) returned %d entries, want 4 (coerced to the maximum)", got)
	}

	// then: a negative page_size is INVALID_ARGUMENT
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet,
		fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis?page_size=-1",
			sutHostURL, pathPrefix, saoleiTemplateID, sessionID), sutEnvName, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("ListWikis(page_size=-1) status=%d, want 400 INVALID_ARGUMENT, body=%s", resp.StatusCode, respBody)
	}
}

// TestWikiServiceHttpSearch verifies SearchWikis through the gateway public
// entry: normalized substring matching (Chinese and case-insensitive ASCII),
// the exact > prefix > substring ranking, the single-rune fallback, and the
// query-bound offset pagination (a continuation whose query differs is
// rejected with 400).
func TestWikiServiceHttpSearch(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: titles covering exact, prefix, substring and misses
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	for _, seed := range []struct{ id, title string }{
		{"wiki-1", "策略"},
		{"wiki-2", "策略复盘"},
		{"wiki-3", "中期策略复盘"},
		{"wiki-4", "无关条目"},
		{"wiki-5", "ABC 复盘"},
		{"wiki-6", "Ｗｉｄｅ 标题"},
	} {
		createWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, seed.id, seed.title, "content-"+seed.id)
	}

	// when: search with the Chinese substring
	resp := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "策略", 0, "")

	// then: exact > prefix > substring; the miss is excluded
	assertSearchedWikiTitles(t, resp.GetWikis(), []string{"策略", "策略复盘", "中期策略复盘"})
	if resp.GetNextPageToken() != "" {
		t.Errorf("SearchWikis() next_page_token = %q, want empty", resp.GetNextPageToken())
	}

	// when: search with a lower-case ASCII query against a mixed-case title
	lower := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "abc", 0, "")

	// then: matching is case-insensitive
	assertSearchedWikiTitles(t, lower.GetWikis(), []string{"ABC 复盘"})

	// when: a full-width query against a half-width ASCII title, and the
	// half-width/full-width spellings of a query against a full-width title
	fullWidthQuery := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "ＡＢＣ", 0, "")
	halfWidthQuery := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wide", 0, "")
	fullWidthSpelling := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "ＷＩＤＥ", 0, "")

	// then: NFKC normalization makes full-width and half-width equivalent
	assertSearchedWikiTitles(t, fullWidthQuery.GetWikis(), []string{"ABC 复盘"})
	assertSearchedWikiTitles(t, halfWidthQuery.GetWikis(), []string{"Ｗｉｄｅ 标题"})
	assertSearchedWikiTitles(t, fullWidthSpelling.GetWikis(), []string{"Ｗｉｄｅ 标题"})

	// when: search with a single rune (the regex fallback path)
	short := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "策", 0, "")

	// then: the same three titles, prefix matches first
	assertSearchedWikiTitles(t, short.GetWikis(), []string{"策略", "策略复盘", "中期策略复盘"})

	// when: paginate the search with page_size=1
	page1 := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "策略", 1, "")

	// then
	assertSearchedWikiTitles(t, page1.GetWikis(), []string{"策略"})
	if page1.GetNextPageToken() == "" {
		t.Fatal("SearchWikis() page 1 next_page_token is empty, want non-empty")
	}

	// then: a continuation whose query differs from the token's is rejected
	respMismatch, bodyMismatch := doHTTPTrace(t, ctx, http.MethodGet,
		fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis:search?query=%s&page_token=%s",
			sutHostURL, pathPrefix, saoleiTemplateID, sessionID, url.QueryEscape("开局"), url.QueryEscape(page1.GetNextPageToken())),
		sutEnvName, nil)
	if respMismatch.StatusCode != http.StatusBadRequest {
		t.Errorf("query-mismatch continuation status=%d, want 400 INVALID_ARGUMENT, body=%s", respMismatch.StatusCode, bodyMismatch)
	}

	// when: continue with the same query
	page2 := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "策略", 1, page1.GetNextPageToken())

	// then: the next ranked entry and order preserved
	assertSearchedWikiTitles(t, page2.GetWikis(), []string{"策略复盘"})
}

// TestWikiServiceHttpSearchTracksTitleUpdates verifies that an Update changing
// a wiki's title rebuilds the search index end-to-end: the old title stops
// matching, the new one matches, and Get returns the stored content
// (spec 068 FR-002;
// specs/068-wiki-round-pause/contracts/wiki-service.md §2-4).
func TestWikiServiceHttpSearchTracksTitleUpdates(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a wiki whose title is later changed
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	createWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1", "旧版策略", "正文一")

	// when: the old title is searchable
	before := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "旧版策略", 0, "")

	// then
	assertSearchedWikiTitles(t, before.GetWikis(), []string{"旧版策略"})

	// when: update the title via update_mask
	if status, body := updateWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1", "新版策略", "", "title"); status != http.StatusOK {
		t.Fatalf("UpdateWiki(title) status=%d, body=%s", status, body)
	}

	// then: the old title no longer matches (the search index was rebuilt)
	old := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "旧版策略", 0, "")
	if got := len(old.GetWikis()); got != 0 {
		t.Errorf("SearchWikis(old title) returned %d entries, want 0 after the title change", got)
	}

	// then: the new title matches
	updated := searchWikis(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "新版策略", 0, "")
	assertSearchedWikiTitles(t, updated.GetWikis(), []string{"新版策略"})

	// then: Get returns the new title with the preserved content
	got, status := getWiki(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "wiki-1")
	if status != http.StatusOK {
		t.Fatalf("GetWiki status=%d, want 200", status)
	}
	if got.GetTitle() != "新版策略" || got.GetContent() != "正文一" {
		t.Errorf("GetWiki after title update = %+v, want the new title with preserved content", got)
	}
}

// TestWikiServiceHttpValidation verifies the field validation and error
// mapping through the gateway public entry: empty/over-limit title and
// content, an empty search query, and malformed page tokens.
func TestWikiServiceHttpValidation(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a session as the wiki parent
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)

	tests := []struct {
		name    string
		wikiID  string
		title   string
		content string
	}{
		{name: "empty title", wikiID: "wiki-empty-title", title: "", content: "c"},
		{name: "empty content", wikiID: "wiki-empty-content", title: "t", content: ""},
		{name: "title over the rune limit", wikiID: "wiki-long-title", title: strings.Repeat("中", 129), content: "c"},
		{name: "content over the byte limit", wikiID: "wiki-long-content", title: "t", content: strings.Repeat("x", 64*1024+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			status, body := createWikiRaw(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, tt.wikiID, tt.title, tt.content)

			// then
			if status != http.StatusBadRequest {
				t.Errorf("CreateWiki status=%d, want 400 INVALID_ARGUMENT, body=%s", status, body)
			}
		})
	}

	// when: search with an empty query
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet,
		fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis:search",
			sutHostURL, pathPrefix, saoleiTemplateID, sessionID), sutEnvName, nil)

	// then
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("SearchWikis(empty query) status=%d, want 400 INVALID_ARGUMENT, body=%s", resp.StatusCode, respBody)
	}

	// when: malformed page tokens
	for _, target := range []string{
		fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis?page_token=not-a-token", sutHostURL, pathPrefix, saoleiTemplateID, sessionID),
		fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis:search?query=x&page_token=not-a-token", sutHostURL, pathPrefix, saoleiTemplateID, sessionID),
	} {
		// when
		resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, target, sutEnvName, nil)

		// then
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("malformed page_token status=%d, want 400 INVALID_ARGUMENT, url=%s body=%s", resp.StatusCode, target, respBody)
		}
	}
}

// ─── Wiki HTTP helpers ──────────────────────────────────────────────────────

// createWikiRaw posts a Wiki (title+content) to /wikis?wiki_id=... and returns
// the HTTP status and body without failing on an error status, so validation
// cases can assert the error contract.
func createWikiRaw(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID, wikiID, title, content string) (int, []byte) {
	t.Helper()

	body, err := protojson.Marshal(&game.Wiki{Title: title, Content: content})
	if err != nil {
		t.Fatalf("protojson.Marshal Wiki: %v", err)
	}

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis?wiki_id=%s",
		sutHostURL, pathPrefix, template, sessionID, url.QueryEscape(wikiID))
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPost, reqURL, sutEnvName, body)
	return resp.StatusCode, respBody
}

// createWiki posts a Wiki via createWikiRaw and returns the parsed resource
// plus the raw body (the body lets callers assert the FR-017 response shape).
// Calls t.Fatal on non-200 responses.
func createWiki(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID, wikiID, title, content string) (*game.Wiki, []byte) {
	t.Helper()

	status, respBody := createWikiRaw(t, ctx, sutHostURL, sutEnvName, template, sessionID, wikiID, title, content)
	if status != http.StatusOK {
		t.Fatalf("POST createWiki status=%d, body=%s", status, respBody)
	}

	created := new(game.Wiki)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, created); err != nil {
		t.Fatalf("Unmarshal Wiki: %v (raw: %s)", err, string(respBody))
	}
	return created, respBody
}

// getWiki sends a GET for a Wiki and returns the parsed resource together with
// the HTTP status (a non-200 status returns a nil resource).
func getWiki(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID, wikiID string) (*game.Wiki, int) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis/%s",
		sutHostURL, pathPrefix, template, sessionID, wikiID)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode
	}

	got := new(game.Wiki)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, got); err != nil {
		t.Fatalf("Unmarshal Wiki: %v (raw: %s)", err, string(respBody))
	}
	return got, resp.StatusCode
}

// updateWiki sends a PATCH with the Wiki body {name, title, content} and the
// given update_mask, returning the status and body (it does not fail on a
// non-200 response, so callers can assert the NOT_FOUND contract).
func updateWiki(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID, wikiID, title, content, updateMask string) (int, []byte) {
	t.Helper()

	patch := &game.Wiki{
		Name:    game.WikiName{TemplateID: template, SessionID: sessionID, WikiID: wikiID}.String(),
		Title:   title,
		Content: content,
	}
	body, err := protojson.Marshal(patch)
	if err != nil {
		t.Fatalf("protojson.Marshal patch Wiki: %v", err)
	}

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis/%s?update_mask=%s",
		sutHostURL, pathPrefix, template, sessionID, wikiID, updateMask)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPatch, reqURL, sutEnvName, body)
	return resp.StatusCode, respBody
}

// deleteWiki sends a DELETE for a Wiki resource and returns the HTTP status.
func deleteWiki(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID, wikiID string) int {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis/%s",
		sutHostURL, pathPrefix, template, sessionID, wikiID)
	resp, _ := doHTTPTrace(t, ctx, http.MethodDelete, reqURL, sutEnvName, nil)
	return resp.StatusCode
}

// listWikis sends a GET to list a session's wikis (fixed wiki_id ascending
// order, AIP-158 pagination) and returns the parsed response. Calls t.Fatal on
// non-200 responses.
func listWikis(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID string, pageSize int, pageToken string) *game.ListWikisResponse {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis?page_size=%d",
		sutHostURL, pathPrefix, template, sessionID, pageSize)
	if pageToken != "" {
		reqURL += "&page_token=" + pageToken
	}
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET listWikis status=%d, body=%s", resp.StatusCode, respBody)
	}

	lmr := new(game.ListWikisResponse)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, lmr); err != nil {
		t.Fatalf("Unmarshal ListWikisResponse: %v (raw: %s)", err, string(respBody))
	}
	return lmr
}

// searchWikis sends a GET to the SearchWikis custom method and returns the
// parsed response. A zero pageSize omits the parameter (the server default).
// Calls t.Fatal on non-200 responses.
func searchWikis(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, template, sessionID, query string, pageSize int, pageToken string) *game.SearchWikisResponse {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/wikis:search?query=%s",
		sutHostURL, pathPrefix, template, sessionID, url.QueryEscape(query))
	if pageSize > 0 {
		reqURL += fmt.Sprintf("&page_size=%d", pageSize)
	}
	if pageToken != "" {
		reqURL += "&page_token=" + pageToken
	}
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET searchWikis status=%d, body=%s", resp.StatusCode, respBody)
	}

	swr := new(game.SearchWikisResponse)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, swr); err != nil {
		t.Fatalf("Unmarshal SearchWikisResponse: %v (raw: %s)", err, string(respBody))
	}
	return swr
}

// assertListedWikiTitles asserts the listed resources' titles equal want in
// order.
func assertListedWikiTitles(t *testing.T, wikis []*game.Wiki, want []string) {
	t.Helper()
	if len(wikis) != len(want) {
		t.Fatalf("got %d listed wikis, want %d", len(wikis), len(want))
	}
	for i, title := range want {
		if wikis[i].GetTitle() != title {
			t.Fatalf("listed wiki[%d] title = %q, want %q", i, wikis[i].GetTitle(), title)
		}
	}
}

// assertSearchedWikiTitles asserts the search summaries' titles equal want in
// order.
func assertSearchedWikiTitles(t *testing.T, wikis []*game.WikiSummary, want []string) {
	t.Helper()
	if len(wikis) != len(want) {
		t.Fatalf("got %d search entries, want %d", len(wikis), len(want))
	}
	for i, title := range want {
		if wikis[i].GetTitle() != title {
			t.Fatalf("search entry[%d] title = %q, want %q", i, wikis[i].GetTitle(), title)
		}
	}
}
