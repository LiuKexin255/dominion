package service

import (
	"bufio"
	"encoding/json"
	"io"
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
	handler := NewResponsesHandler(responsesTestStore(t))
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
	handler := NewResponsesHandler(responsesTestStore(t))
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
			handler := NewResponsesHandler(responsesTestStore(t))
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
	handler := NewResponsesHandler(responsesTestStore(t))
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

// TestResponsesHandler_NonStreaming verifies the stream:false shape: one
// JSON response object with the reasoning (when present) and message
// output items plus the derived usage.
func TestResponsesHandler_NonStreaming(t *testing.T) {
	// given
	handler := NewResponsesHandler(responsesTestStore(t))
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
	handler := NewResponsesHandler(responsesTestStore(t))

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
	handler := NewResponsesHandler(responsesTestStore(t))
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
	srv := httptest.NewServer(NewResponsesHandler(store))
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
