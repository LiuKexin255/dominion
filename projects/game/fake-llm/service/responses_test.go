package service

import (
	"bufio"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// responsesTestStore builds an in-memory store with the agent_v2 template
// family so every test asserts against known strings.
func responsesTestStore(t *testing.T) *MessageStore {
	t.Helper()
	store := newStoreFromMap(t, fstest.MapFS{
		"testdata/agent_v2.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"messages:",
				"  - name: agent-v2-greet",
				"    keywords:",
				"      - agent-v2-think",
				"    reasoning_chunks:",
				"      - \"Analyzing the request.\"",
				"      - \"Drafting a reply.\"",
				"    text: \"Hello, I help with game sessions.\"",
				"    responses_only: true",
				"  - name: agent-v2-followup",
				"    keywords:",
				"      - agent-v2-think",
				"    history_keywords:",
				"      - \"game sessions\"",
				"    text: \"As I said, I help with game sessions.\"",
				"    responses_only: true",
				"  - name: agent-v2-plain",
				"    keywords:",
				"      - agent-v2-plain",
				"    text: \"Plain answer.\"",
				"    responses_only: true",
				"  - name: agent-v2-fail",
				"    keywords:",
				"      - agent-v2-fail",
				"    failure:",
				"      code: \"glm_test_failure\"",
				"      message: \"Injected failure\"",
				"    responses_only: true",
				"  - name: agent-v2-fail-mid",
				"    keywords:",
				"      - agent-v2-midfail",
				"    reasoning_chunks:",
				"      - \"Thinking before the break.\"",
				"    text: \"Partial answer.\"",
				"    failure:",
				"      code: \"glm_test_failure\"",
				"      message: \"Injected failure after content\"",
				"    responses_only: true",
				"",
			}, "\n")),
		},
	})
	return store
}

// scanResponsesEvents extracts every SSE event as a name/data pair, in
// order, failing the test on a malformed stream.
func scanResponsesEvents(t *testing.T, r io.Reader) [][2]string {
	t.Helper()
	var events [][2]string
	var name, data string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && name != "":
			events = append(events, [2]string{name, data})
			name, data = "", ""
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning responses SSE stream: %v", err)
	}
	if name != "" {
		t.Fatalf("unterminated SSE event %q at end of stream", name)
	}
	return events
}

// responsesEvent decodes one scanned event's data payload as generic JSON.
func responsesEvent(t *testing.T, data string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("decode responses event data %q: %v", data, err)
	}
	return payload
}

func postResponses(t *testing.T, handler *ResponsesHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body)))
	return rec
}

// TestResponsesHandler_StreamThinkText verifies the full think+text event
// order of fake-responses-wire.md §2: created → output_item.added
// (reasoning, index 0) → reasoning deltas → output_item.added (message,
// index 1) → text delta → output_item.done (message, full text) →
// completed (usage last), with deterministic usage constants.
func TestResponsesHandler_StreamThinkText(t *testing.T) {
	// given: the agent_v2 store and a turn-1 request matching the greet
	// template.
	handler := NewResponsesHandler(responsesTestStore(t), rand.New(rand.NewPCG(1, 0)))
	body := `{"model":"glm-5.2","stream":true,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"please agent-v2-think"}]}]}`

	// when
	rec := postResponses(t, handler, body)

	// then: 200 + text/event-stream, and the exact event sequence.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	events := scanResponsesEvents(t, rec.Body)
	wantNames := []string{
		"response.created",
		"response.output_item.added",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.delta",
		"response.output_item.added",
		"response.output_text.delta",
		"response.output_item.done",
		"response.completed",
	}
	if len(events) != len(wantNames) {
		t.Fatalf("got %d events, want %d: %v", len(events), len(wantNames), events)
	}
	for i, want := range wantNames {
		if events[i][0] != want {
			t.Fatalf("event %d = %q, want %q", i, events[i][0], want)
		}
	}

	// Reasoning item on output_index 0, message item on output_index 1:
	// think and text never share an output item (§2 invariant 2).
	added0 := responsesEvent(t, events[1][1])
	if got := added0["output_index"]; got != float64(0) {
		t.Errorf("reasoning output_index = %v, want 0", got)
	}
	if item, ok := added0["item"].(map[string]any); !ok || item["type"] != "reasoning" {
		t.Errorf("event1 item = %v, want a reasoning item", added0["item"])
	}
	delta0 := responsesEvent(t, events[2][1])
	if got := delta0["delta"]; got != "Analyzing the request." {
		t.Errorf("first reasoning delta = %v, want the configured chunk", got)
	}
	added1 := responsesEvent(t, events[4][1])
	if got := added1["output_index"]; got != float64(1) {
		t.Errorf("message output_index = %v, want 1", got)
	}
	textDelta := responsesEvent(t, events[5][1])
	if got := textDelta["delta"]; got != "Hello, I help with game sessions." {
		t.Errorf("text delta = %v, want the configured text", got)
	}
	done := responsesEvent(t, events[6][1])
	item, ok := done["item"].(map[string]any)
	if !ok || item["type"] != "message" {
		t.Fatalf("done item = %v, want a message item", done["item"])
	}
	content, ok := item["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("done item content = %v, want one output_text part", item["content"])
	}
	part := content[0].(map[string]any)
	if part["text"] != "Hello, I help with game sessions." {
		t.Errorf("done item text = %v, want the full text", part["text"])
	}

	// completed is final and carries the deterministic usage derived from
	// the template lengths (§2 invariant 3).
	completed := responsesEvent(t, events[7][1])
	resp, ok := completed["response"].(map[string]any)
	if !ok || resp["status"] != "completed" {
		t.Fatalf("completed response = %v, want status completed", completed["response"])
	}
	usage, ok := resp["usage"].(map[string]any)
	if !ok {
		t.Fatalf("completed usage = %v, want a usage object", resp["usage"])
	}
	// "Analyzing the request." + "Drafting a reply." form the think
	// length; output/input tokens are the think+text length.
	wantReasoning := len([]rune("Analyzing the request.")) + len([]rune("Drafting a reply."))
	wantText := len([]rune("Hello, I help with game sessions."))
	if usage["input_tokens"] != float64(wantReasoning+wantText) {
		t.Errorf("input_tokens = %v, want %d", usage["input_tokens"], wantReasoning+wantText)
	}
	if usage["output_tokens"] != float64(wantReasoning+wantText) {
		t.Errorf("output_tokens = %v, want %d", usage["output_tokens"], wantReasoning+wantText)
	}
	details, ok := usage["output_tokens_details"].(map[string]any)
	if !ok {
		t.Fatalf("output_tokens_details = %v, want an object", usage["output_tokens_details"])
	}
	if details["reasoning_tokens"] != float64(wantReasoning) {
		t.Errorf("reasoning_tokens = %v, want %d", details["reasoning_tokens"], wantReasoning)
	}
}

// TestResponsesHandler_StreamPlainText verifies US2 scenario 2's fake
// side: a pure text template produces ZERO reasoning events and the
// message item sits at output_index 0.
func TestResponsesHandler_StreamPlainText(t *testing.T) {
	// given: a request matching the plain template.
	handler := NewResponsesHandler(responsesTestStore(t), rand.New(rand.NewPCG(1, 0)))
	body := `{"model":"glm-5.2","stream":true,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"agent-v2-plain please"}]}]}`

	// when
	rec := postResponses(t, handler, body)

	// then: no reasoning event anywhere; message at output_index 0.
	events := scanResponsesEvents(t, rec.Body)
	var names []string
	for _, e := range events {
		names = append(names, e[0])
		if strings.Contains(e[0], "reasoning") {
			t.Errorf("unexpected reasoning event %q in a pure-text stream", e[0])
		}
	}
	want := []string{
		"response.created",
		"response.output_item.added",
		"response.output_text.delta",
		"response.output_item.done",
		"response.completed",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", names, want)
	}
	added := responsesEvent(t, events[1][1])
	if got := added["output_index"]; got != float64(0) {
		t.Errorf("message output_index = %v, want 0 (no think before it)", got)
	}
}

// TestResponsesHandler_MultiTurnHistoryKeywords verifies the multi-turn
// condition path (US1-2): the same keyword resolves to greet on turn 1
// and to the history-keyword-gated followup template on turn 2.
func TestResponsesHandler_MultiTurnHistoryKeywords(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantText     string
		wantThinkish bool
	}{
		{
			name:         "turn 1 hits the greet template",
			input:        `[{"type":"message","role":"user","content":[{"type":"input_text","text":"agent-v2-think"}]}]`,
			wantText:     "Hello, I help with game sessions.",
			wantThinkish: true,
		},
		{
			name: "turn 2 with greet reply in history hits the followup template",
			input: `[` +
				`{"type":"message","role":"user","content":[{"type":"input_text","text":"agent-v2-think"}]},` +
				`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello, I help with game sessions."}]},` +
				`{"type":"message","role":"user","content":[{"type":"input_text","text":"agent-v2-think again"}]}]`,
			wantText:     "As I said, I help with game sessions.",
			wantThinkish: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler := NewResponsesHandler(responsesTestStore(t), rand.New(rand.NewPCG(1, 0)))
			body := `{"model":"glm-5.2","stream":true,"input":` + tt.input + `}`

			// when
			rec := postResponses(t, handler, body)

			// then: the text delta and done item carry the expected text;
			// the followup template declares no think, so no reasoning
			// events may appear.
			events := scanResponsesEvents(t, rec.Body)
			var text string
			for _, e := range events {
				if e[0] == "response.output_text.delta" {
					text = responsesEvent(t, e[1])["delta"].(string)
				}
				if !tt.wantThinkish && strings.Contains(e[0], "reasoning") {
					t.Errorf("unexpected reasoning event %q for the followup template", e[0])
				}
			}
			if text != tt.wantText {
				t.Fatalf("text delta = %q, want %q", text, tt.wantText)
			}
		})
	}
}

// TestResponsesHandler_FailureInjection verifies the failure template
// (§2 invariant 4): created → response.failed with the configured
// code/message, and no think/text events at all.
func TestResponsesHandler_FailureInjection(t *testing.T) {
	// given: a request matching the failure template.
	handler := NewResponsesHandler(responsesTestStore(t), rand.New(rand.NewPCG(1, 0)))
	body := `{"model":"glm-5.2","stream":true,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"agent-v2-fail"}]}]}`

	// when
	rec := postResponses(t, handler, body)

	// then
	events := scanResponsesEvents(t, rec.Body)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (created + failed): %v", len(events), events)
	}
	if events[0][0] != "response.created" || events[1][0] != "response.failed" {
		t.Fatalf("events = [%q, %q], want [created, failed]", events[0][0], events[1][0])
	}
	failed := responsesEvent(t, events[1][1])
	resp, ok := failed["response"].(map[string]any)
	if !ok || resp["status"] != "failed" {
		t.Fatalf("failed response = %v, want status failed", failed["response"])
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("failed error = %v, want an error object", resp["error"])
	}
	if errObj["code"] != "glm_test_failure" || errObj["message"] != "Injected failure" {
		t.Errorf("error = %v, want the configured code/message", errObj)
	}
}

// TestResponsesHandler_FailureAfterPartialContent verifies the
// content-carrying failure template (agent_v2.yaml agent-v2-fail-mid): the
// think and text events stream first, and response.failed replaces the
// terminal completed — the "partial content then provider failure" wire
// whose produced prefix the agent solidifies as the interrupted history
// entry (agent-api-changes.md §6).
func TestResponsesHandler_FailureAfterPartialContent(t *testing.T) {
	// given: a request matching the partial-content failure template.
	handler := NewResponsesHandler(responsesTestStore(t), rand.New(rand.NewPCG(1, 0)))
	body := `{"model":"glm-5.2","stream":true,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"agent-v2-midfail"}]}]}`

	// when
	rec := postResponses(t, handler, body)

	// then: created → reasoning delta → message added → text delta →
	// message done → failed (no completed, no usage).
	events := scanResponsesEvents(t, rec.Body)
	wantNames := []string{
		"response.created",
		"response.output_item.added",
		"response.reasoning_summary_text.delta",
		"response.output_item.added",
		"response.output_text.delta",
		"response.output_item.done",
		"response.failed",
	}
	if len(events) != len(wantNames) {
		t.Fatalf("got %d events, want %d: %v", len(events), len(wantNames), events)
	}
	for i, want := range wantNames {
		if events[i][0] != want {
			t.Fatalf("event[%d] = %q, want %q", i, events[i][0], want)
		}
	}
	if delta := responsesEvent(t, events[2][1])["delta"]; delta != "Thinking before the break." {
		t.Errorf("reasoning delta = %v, want the template's think piece", delta)
	}
	if delta := responsesEvent(t, events[4][1])["delta"]; delta != "Partial answer." {
		t.Errorf("text delta = %v, want the template's text", delta)
	}
	failed := responsesEvent(t, events[len(events)-1][1])
	resp, ok := failed["response"].(map[string]any)
	if !ok || resp["status"] != "failed" {
		t.Fatalf("failed response = %v, want status failed", failed["response"])
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("failed error = %v, want an error object", resp["error"])
	}
	if errObj["code"] != "glm_test_failure" || errObj["message"] != "Injected failure after content" {
		t.Errorf("error = %v, want the configured code/message", errObj)
	}
}

// TestResponsesHandler_NonStreaming verifies the stream:false shape: one
// JSON response object with the reasoning (when present) and message
// output items plus the derived usage.
func TestResponsesHandler_NonStreaming(t *testing.T) {
	// given
	handler := NewResponsesHandler(responsesTestStore(t), rand.New(rand.NewPCG(1, 0)))
	body := `{"model":"glm-5.2","stream":false,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"agent-v2-think"}]}]}`

	// when
	rec := postResponses(t, handler, body)

	// then
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	if resp["status"] != "completed" || resp["object"] != "response" {
		t.Errorf("status/object = %v/%v, want completed/response", resp["status"], resp["object"])
	}
	output, ok := resp["output"].([]any)
	if !ok || len(output) != 2 {
		t.Fatalf("output = %v, want two items (reasoning + message)", resp["output"])
	}
	if output[0].(map[string]any)["type"] != "reasoning" {
		t.Errorf("output[0] = %v, want the reasoning item", output[0])
	}
	if output[1].(map[string]any)["type"] != "message" {
		t.Errorf("output[1] = %v, want the message item", output[1])
	}
}

// TestResponsesHandler_BadRequests covers the 400/405 guards and the
// input-tolerance rules (fake-responses-wire.md §1).
func TestResponsesHandler_BadRequests(t *testing.T) {
	handler := NewResponsesHandler(responsesTestStore(t), rand.New(rand.NewPCG(1, 0)))

	tests := []struct {
		name   string
		method string
		body   string
		want   int
	}{
		{
			name:   "GET is rejected with 405",
			method: http.MethodGet,
			body:   `{}`,
			want:   http.StatusMethodNotAllowed,
		},
		{
			name:   "malformed JSON yields 400",
			method: http.MethodPost,
			body:   `{not json`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "missing model yields 400",
			method: http.MethodPost,
			body:   `{"stream":true,"input":[]}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "missing input yields 400",
			method: http.MethodPost,
			body:   `{"model":"glm-5.2","stream":true}`,
			want:   http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(tt.method, "/v1/responses", strings.NewReader(tt.body)))

			// then
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}

	t.Run("unknown input item types are ignored, empty input falls back", func(t *testing.T) {
		// given: only a reasoning item (dropped) — the request is legal and
		// falls through to the deterministic fallback with an empty pool
		// message list.
		body := `{"model":"glm-5.2","stream":false,"input":[` +
			`{"type":"reasoning","content":[]},{"type":"message","role":"assistant","content":[]}]}`
		// when
		rec := postResponses(t, handler, body)
		// then: 200 (fallback), not an error.
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
}

// TestResponsesHandler_DeterministicFallback verifies the no-match path
// returns the same template text for the same request across calls (§5
// determinism anchor) and never picks the failure template.
func TestResponsesHandler_DeterministicFallback(t *testing.T) {
	// given: a request matching no keyword.
	handler := NewResponsesHandler(responsesTestStore(t), rand.New(rand.NewPCG(1, 0)))
	body := `{"model":"glm-5.2","stream":false,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"xyzzy-no-such-keyword"}]}]}`

	// when: the same request twice.
	var texts []string
	for range 2 {
		rec := postResponses(t, handler, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp["status"] == "failed" {
			t.Fatal("fallback must never pick the failure template")
		}
		output, ok := resp["output"].([]any)
		if !ok || len(output) == 0 {
			t.Fatalf("output = %v, want at least one item", resp["output"])
		}
		item, ok := output[len(output)-1].(map[string]any)
		if !ok || item["type"] != "message" {
			t.Fatalf("last output item = %v, want a message item", output[len(output)-1])
		}
		content, ok := item["content"].([]any)
		if !ok || len(content) != 1 {
			t.Fatalf("message content = %v, want one output_text part", item["content"])
		}
		texts = append(texts, content[0].(map[string]any)["text"].(string))
	}

	// then: identical text across calls (determinism pinned on the payload,
	// not just the status).
	if texts[0] == "" || texts[0] != texts[1] {
		t.Fatalf("fallback texts = %v, want the same non-empty text twice", texts)
	}
}

// TestResponsesHandler_LongDelayWiring verifies the controllable-delay
// projection: the wall-clock gap between consecutive reasoning deltas
// matches the configured chunk_delays entry (a real transport, so the
// sleep actually separates the frames).
func TestResponsesHandler_LongDelayWiring(t *testing.T) {
	// given: an ad-hoc store with a short 80ms delay (the shipped 3s
	// template shares this code path but is too slow for a unit test).
	store := newStoreFromMap(t, fstest.MapFS{
		"testdata/slow.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"name: agent-v2-test-slow",
				"keywords:",
				"  - agent-v2-test-slow",
				"reasoning_chunks:",
				"  - \"one\"",
				"  - \"two\"",
				"chunk_delays:",
				"  - \"80ms\"",
				"text: done",
				"",
			}, "\n")),
		},
	})
	srv := httptest.NewServer(NewResponsesHandler(store, rand.New(rand.NewPCG(1, 0))))
	defer srv.Close()

	// when: stream through the real server, recording arrival times.
	resp, err := http.DefaultClient.Post(srv.URL+"/v1/responses", "application/json",
		strings.NewReader(`{"model":"m","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"agent-v2-test-slow"}]}]}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var arrivals []time.Time
	var names []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ") {
			names = append(names, strings.TrimPrefix(line, "event: "))
			arrivals = append(arrivals, time.Now())
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}

	// then: the gap before the second reasoning delta is ≥ the configured
	// 80ms; generous upper bound keeps it robust under CI load.
	if len(names) < 4 {
		t.Fatalf("got %d events, want ≥ 4: %v", len(names), names)
	}
	gap := arrivals[3].Sub(arrivals[2])
	if gap < 60*time.Millisecond || gap > 2*time.Second {
		t.Errorf("gap between reasoning deltas = %v, want ≈ 80ms", gap)
	}
}

// responsesSaoleiStore builds the store carrying BOTH the game chain
// (agent_v2_saolei.yaml) and the v1 chat fixture chain (saolei_tools.yaml +
// its messages file) so every test asserts the real shared-store matching —
// the agent_v2 rules must interoperate with the v1 configs without stealing
// their matches (template-comment 互不干扰契约).
func responsesSaoleiStore(t *testing.T) *MessageStore {
	t.Helper()
	store := newStoreFromMap(t, fstest.MapFS{
		"testdata/agent_v2_saolei.yaml": &fstest.MapFile{
			Data: embeddedTestdata(t, "agent_v2_saolei.yaml"),
		},
		"testdata/agent_v2_saolei_tools.yaml": &fstest.MapFile{
			Data: embeddedTestdata(t, "agent_v2_saolei_tools.yaml"),
		},
		"testdata/saolei_tools.yaml": &fstest.MapFile{
			Data: embeddedTestdata(t, "saolei_tools.yaml"),
		},
		"testdata/saolei.yaml": &fstest.MapFile{
			Data: embeddedTestdata(t, "saolei.yaml"),
		},
	})
	return store
}

// embeddedTestdata reads a template file from the binary's embedded store —
// the fixture source of truth (style/golang.md: the test body must carry all
// information; binary fixtures stay in testdata).
func embeddedTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := embeddedFiles.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("embedded testdata %s: %v", name, err)
	}
	return data
}

// saoleiResponsesHandler wires the saolei store with a fixed-seed RNG so a
// no-match fallback is still deterministic.
func saoleiResponsesHandler(t *testing.T) *ResponsesHandler {
	t.Helper()
	return NewResponsesHandler(responsesSaoleiStore(t), rand.New(rand.NewPCG(1, 0)))
}

// toolInput builds the Responses input array of one mid-chain request: the
// original user turn, the replayed model call, and the tool result output.
// arguments follows the real wire shape (serialize.ts): a JSON STRING, not an
// object.
func toolInput(callID, toolName, arguments, output string) string {
	return `{"model":"m","stream":true,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"开始一局扫雷"}]},` +
		`{"type":"function_call","call_id":"` + callID + `","name":"` + toolName + `","arguments":` + strconvQuote(arguments) + `},` +
		`{"type":"function_call_output","call_id":"` + callID + `","output":` + strconvQuote(output) + `}]}`
}

// strconvQuote JSON-escapes s as a JSON string literal.
func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestResponsesHandler_ToolCallChainWon verifies the agent_v2 won-scenario
// chain (fake-desktop default scenario): the keyword fires saolei_init, the
// won-board init result re-triggers saolei_operate, the game_won reject text
// terminates with the summary, and the progressive 16×16 chain drives a
// click+flag batch to its own terminator.
func TestResponsesHandler_ToolCallChainWon(t *testing.T) {
	handler := saoleiResponsesHandler(t)

	t.Run("user keyword triggers saolei_init", func(t *testing.T) {
		// The instructions carry the materialized preset's persona anchor
		// line — the saolei fixtures declare it as a system_keywords
		// condition (T008), mirroring the post-pivot agent_v2 wire.
		request := `{"model":"m","stream":true,"instructions":"你是扫雷 player。","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"帮我开始一局扫雷"}]}]}`
		rec := postResponses(t, handler, request)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		events := scanResponsesEvents(t, rec.Body)
		if events[0][0] != "response.created" {
			t.Fatalf("first event = %s, want response.created", events[0][0])
		}
		if events[1][0] != "response.output_item.added" {
			t.Fatalf("second event = %s, want output_item.added", events[1][0])
		}
		added := responsesEvent(t, events[1][1])
		item := added["item"].(map[string]any)
		if item["type"] != "function_call" || item["name"] != "saolei_init" {
			t.Fatalf("added item = %v, want function_call saolei_init", item)
		}
		callID, _ := item["call_id"].(string)
		if !strings.HasPrefix(callID, "call_fake_") || callID == "call_fake_" {
			t.Fatalf("call_id = %q, want the request-derived call_fake_<hash>", callID)
		}
		// Deterministic for the same request (the fake-wire invariant) …
		again := postResponses(t, handler, request)
		againItem := responsesEvent(t, scanResponsesEvents(t, again.Body)[1][1])["item"].(map[string]any)
		if againItem["call_id"] != callID {
			t.Errorf("repeated request call_id = %v, want the deterministic %q", againItem["call_id"], callID)
		}
		// … and distinct across a chain's steps: the team broadcast
		// reference model anchors a member's tool units on the call id, so a
		// later tool call sharing this id would render/consume as the same
		// unit (specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §1).
		other := postResponses(t, handler, toolInput("call_1", "saolei_init", "{}", "new game started\ngame status: won\n\nboard size 9*9"))
		otherItem := responsesEvent(t, scanResponsesEvents(t, other.Body)[1][1])["item"].(map[string]any)
		if otherItem["call_id"] == callID {
			t.Errorf("chain follow-up call_id = %v, want a distinct id from %q", otherItem["call_id"], callID)
		}
		// The terminal event carries the same call assembled (finish maps
		// to tool-calls on the adapter side).
		last := events[len(events)-1]
		if last[0] != "response.completed" {
			t.Fatalf("last event = %s, want response.completed", last[0])
		}
	})

	t.Run("init result continues with the operate batch", func(t *testing.T) {
		initResult := "new game started\ngame status: won\ngame status: won board follows\nboard size 9*9"
		rec := postResponses(t, handler, toolInput("call_1", "saolei_init", "{}", initResult))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		events := scanResponsesEvents(t, rec.Body)
		done := events[len(events)-2]
		if done[0] != "response.output_item.done" {
			t.Fatalf("event before completed = %s, want output_item.done", done[0])
		}
		item := responsesEvent(t, done[1])["item"].(map[string]any)
		if item["name"] != "saolei_operate" {
			t.Fatalf("follow-up call = %v, want saolei_operate", item)
		}
		args, ok := item["arguments"].(string)
		if !ok || !strings.Contains(args, `"operations"`) {
			t.Fatalf("arguments = %v, want the batch operations JSON", item["arguments"])
		}
	})

	t.Run("won operate result terminates with the summary text", func(t *testing.T) {
		operateResult := "saolei_operate → stopped at click(0,0) (game_won)\ngame status: won\n\nboard size 9*9"
		rec := postResponses(t, handler, toolInput("call_2", "saolei_operate", "{}", operateResult))
		events := scanResponsesEvents(t, rec.Body)
		var text string
		for _, ev := range events {
			if ev[0] == "response.output_text.delta" {
				text = responsesEvent(t, ev[1])["delta"].(string)
			}
		}
		if !strings.Contains(text, "游戏获胜") {
			t.Fatalf("final text = %q, want the won summary", text)
		}
	})

	t.Run("progressive init result drives a click+flag batch", func(t *testing.T) {
		initResult := "new game started\ngame status: playing\n\nboard size 16*16"
		rec := postResponses(t, handler, toolInput("call_3", "saolei_init", "{}", initResult))
		events := scanResponsesEvents(t, rec.Body)
		done := events[len(events)-2]
		item := responsesEvent(t, done[1])["item"].(map[string]any)
		args, ok := item["arguments"].(string)
		if !ok {
			t.Fatalf("arguments = %v, want the batch JSON", item["arguments"])
		}
		// The agent_v2 progressive batch carries one click AND one flag —
		// the v1 follow-up would be two clicks (no flag).
		if !strings.Contains(args, `"click"`) || !strings.Contains(args, `"flag"`) {
			t.Fatalf("arguments = %q, want the click+flag progressive batch", args)
		}
	})

	t.Run("progressive operate result terminates with the board summary", func(t *testing.T) {
		operateResult := "saolei_operate → executed 2 ops\ngame status: playing\n\nboard size 16*16"
		rec := postResponses(t, handler, toolInput("call_4", "saolei_operate", "{}", operateResult))
		events := scanResponsesEvents(t, rec.Body)
		var text string
		for _, ev := range events {
			if ev[0] == "response.output_text.delta" {
				text = responsesEvent(t, ev[1])["delta"].(string)
			}
		}
		if !strings.Contains(text, "棋盘已刷新") {
			t.Fatalf("final text = %q, want the progressive terminator", text)
		}
	})
}

// TestToolChainEndpointIsolation verifies the endpoint scoping of the tool
// fixtures (ToolConfig.ResponsesOnly / ToolsForEndpoint): the v1
// chat-completions chain and the agent_v2 Responses chain share one store but
// never intercept each other's results — the two agents can emit
// byte-identical board texts, so the tools scope MUST be endpoint-keyed.
func TestToolChainEndpointIsolation(t *testing.T) {
	// ToolsForEndpoint keeps each endpoint's scope disjoint, including the
	// no-match fallback pool each endpoint draws from.
	tools := []*ToolConfig{
		{Name: "v1-only"},
		{Name: "shared-responses", ResponsesOnly: true},
		{Name: "v1-only-b"},
	}
	if got := ToolsForEndpoint(tools, false); len(got) != 2 || got[0].Name != "v1-only" || got[1].Name != "v1-only-b" {
		t.Fatalf("ToolsForEndpoint(chat) = %v, want the two non-responses-only entries", got)
	}
	if got := ToolsForEndpoint(tools, true); len(got) != 1 || got[0].Name != "shared-responses" {
		t.Fatalf("ToolsForEndpoint(responses) = %v, want only the responses-only entry", got)
	}

	// Behavioral direction 1 — the RESPONSES endpoint must not resolve
	// through a v1 rule: a progressive-scenario init result takes the
	// agent_v2 progressive batch (one click AND one flag); the v1
	// follow-up rule would answer two clicks. Both rules are candidates on
	// a shared store, so the flag proves the v1 rule was out of scope.
	handler := saoleiResponsesHandler(t)
	initResult := "new game started\ngame status: playing\n\nboard size 16*16"
	rec := postResponses(t, handler, toolInput("call_1", "saolei_init", "{}", initResult))
	events := scanResponsesEvents(t, rec.Body)
	done := events[len(events)-2]
	item := responsesEvent(t, done[1])["item"].(map[string]any)
	args, ok := item["arguments"].(string)
	if !ok {
		t.Fatalf("arguments = %v, want the batch JSON", item["arguments"])
	}
	if !strings.Contains(args, `"flag"`) {
		t.Fatalf("arguments = %q, want the agent_v2 click+flag batch — the v1 two-click rule leaked into the responses scope", args)
	}

	// Behavioral direction 2 — the CHAT endpoint must not resolve through
	// an agent_v2 rule: the 9×9 won init text matches
	// agent-v2-saolei-init-operate's constraints, but the chat scope is
	// blind to it, so the v1 follow-up (click(3,4), click(5,6)) answers.
	// The v2 rule would answer click(0,0)/click(1,1) — the coordinates
	// prove which rule fired.
	chatHandler := NewChatHandler(responsesSaoleiStore(t), rand.New(rand.NewPCG(1, 0)))
	wonInit := "new game started\ngame status: won\n\nboard size 9*9"
	spec := chatHandler.dispatch([]*messageParam{
		{Role: "user", Content: jsonRaw(`"开始一局扫雷"`)},
		{Role: "assistant", Content: jsonRaw(`""`), ToolCalls: []*toolCallParam{{
			ID: "c1", Type: "function",
			Function: toolCallParamFunction{Name: "saolei_init", Arguments: "{}"},
		}}},
		{Role: "tool", ToolCallID: "c1", Name: "saolei_init", Content: jsonRaw(strconvQuote(wonInit))},
	})
	if spec.ToolCall == nil {
		t.Fatal("chat tools branch produced no tool_call")
	}
	if spec.ToolCall.Name != "saolei_operate" {
		t.Fatalf("chat follow-up = %v, want saolei_operate", spec.ToolCall.Name)
	}
	b, _ := json.Marshal(spec.ToolCall.Arguments)
	if !strings.Contains(string(b), "3") || !strings.Contains(string(b), "5") {
		t.Fatalf("chat arguments = %s, want the v1 batch click(3,4)/click(5,6) — an agent_v2 rule leaked into the chat scope", b)
	}
}

// jsonRaw marshals s as a JSON raw message for messageParam fields.
func jsonRaw(s string) json.RawMessage {
	return json.RawMessage(s)
}

// TestResponsesHandler_ToolOutputNotLastKeepsKeywordMatching verifies the
// dispatch rule: a function_call_output that is NOT the last input item does
// not route into the tools branch — the request keeps matching by keywords.
func TestResponsesHandler_ToolOutputNotLastKeepsKeywordMatching(t *testing.T) {
	handler := saoleiResponsesHandler(t)
	body := `{"model":"m","stream":true,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"开始一局扫雷"}]},` +
		`{"type":"function_call","call_id":"c1","name":"saolei_init","arguments":"{}"},` +
		`{"type":"function_call_output","call_id":"c1","output":"stale result"},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"called init"}]}]}`
	rec := postResponses(t, handler, body)
	events := scanResponsesEvents(t, rec.Body)
	// The keyword match fires saolei_init again (tools branch would need a
	// trailing function_call_output).
	var sawFunctionCall bool
	for _, ev := range events {
		if ev[0] == "response.output_item.added" {
			item := responsesEvent(t, ev[1])["item"].(map[string]any)
			sawFunctionCall = item["type"] == "function_call"
		}
	}
	if !sawFunctionCall {
		t.Fatal("expected the keyword path to emit the saolei_init function_call")
	}
}

// TestResponsesHandler_ToolCallNonStreaming verifies the stream:false shape
// of a tool-call response: output carries exactly the function_call item.
func TestResponsesHandler_ToolCallNonStreaming(t *testing.T) {
	handler := saoleiResponsesHandler(t)
	rec := postResponses(t, handler, `{"model":"m","stream":false,"instructions":"你是扫雷 player。","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"开始一局扫雷"}]}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	output := payload["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("output items = %d, want only the function_call", len(output))
	}
	item := output[0].(map[string]any)
	if item["type"] != "function_call" || item["name"] != "saolei_init" {
		t.Fatalf("item = %v, want function_call saolei_init", item)
	}
}

// TestMatchResponsesSystemKeywords pins the system-prompt condition
// (specs/059-agent-v2-team-mode/tasks.md T008): every declared system
// keyword must hit the lowered `instructions` text — the persona anchor
// carrier after the preset-roster pivot — while an undeclared set stays
// vacuous and a miss defers to the next candidate.
func TestMatchResponsesSystemKeywords(t *testing.T) {
	anchored := &Message{
		Name:           "b-anchored",
		Keywords:       []string{"trigger"},
		SystemKeywords: []string{"你是扫雷 player"},
		Text:           "anchored reply",
	}
	plain := &Message{
		Name: "a-plain",
		Text: "plain reply",
	}
	templates := []*Message{anchored, plain}
	userOnly := []responsesMessage{{Role: "user", Text: "trigger please"}}
	instructions := "你是扫雷 player。冷静、精确。"

	tests := []struct {
		name        string
		templates   []*Message
		messages    []responsesMessage
		instruction string
		want        string
	}{
		{
			name:        "anchor hit selects the system-conditioned template",
			templates:   templates,
			messages:    userOnly,
			instruction: instructions,
			want:        "anchored reply",
		},
		{
			name:        "anchor miss defers to the unconditioned candidate",
			templates:   templates,
			messages:    userOnly,
			instruction: "unrelated system prompt",
			want:        "plain reply",
		},
		{
			name:        "no declared system keywords stays vacuous",
			templates:   []*Message{plain},
			messages:    userOnly,
			instruction: "",
			want:        "plain reply",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchResponses(tt.templates, tt.messages, strings.ToLower(tt.instruction))
			if got.Text != tt.want {
				t.Fatalf("matchResponses() = %q, want %q", got.Text, tt.want)
			}
		})
	}
}

// TestMatchResponsesTeamSnapshotPriority pins the T023 fixture ordering: the
// snapshot entry and the opening entry declare the same condition count
// (keywords + system) and the snapshot entry shares the startup keyword
// (扫雷), so the alphabetical tie-break must hand a snapshot-loaded planner
// turn to team-planner-memory-snapshot; without the snapshot section in the
// instructions the same message must fall to team-planner-opening. This is
// the unit-level counterpart of the team memory large test's refresh phase.
func TestMatchResponsesTeamSnapshotPriority(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	messages := []responsesMessage{{Role: "user", Text: "请开始扫雷"}}

	snapshot := matchResponses(
		store.Messages(),
		messages,
		strings.ToLower("你是扫雷 planner。\n长期记忆：\n本局复盘观察：中心区域开局稳定，边角标记需谨慎。"),
	)
	if snapshot.Name != "team-planner-memory-snapshot" {
		t.Fatalf("snapshot-loaded planner turn matched %q, want team-planner-memory-snapshot", snapshot.Name)
	}

	opening := matchResponses(store.Messages(), messages, strings.ToLower("你是扫雷 planner。"))
	if opening.Name != "team-planner-opening" {
		t.Fatalf("snapshot-less planner turn matched %q, want team-planner-opening", opening.Name)
	}
}
