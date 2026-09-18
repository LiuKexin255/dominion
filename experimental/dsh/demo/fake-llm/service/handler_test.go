package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestServeHTTP_Streaming verifies the SSE wire shape end-to-end through
// a REAL httptest.Server transport (per the projects/game/fake-llm
// handler_test.go sample, so flusher/transport interactions are
// exercised): four "data:" frames — role delta with explicit empty
// content, content delta carrying the template text, finish delta with
// stop + usage, then the [DONE] terminator — per
// specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §3.
func TestServeHTTP_Streaming(t *testing.T) {
	// given: the embedded store (greeting matches "hello") served over
	// a real HTTP transport.
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	srv := httptest.NewServer(NewChatHandler(store))
	defer srv.Close()

	// when
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"deepseek-v4-flash","stream":true,"messages":[{"role":"user","content":"hello there"}]}`))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	// then: streaming status + headers.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	frames := scanSSEFrames(t, bytes.NewReader(raw))
	if n := len(frames); n != 4 {
		t.Fatalf("got %d SSE frames, want 4 (3 chunks + DONE). Raw body:\n%s", n, string(raw))
	}
	if frames[3] != "[DONE]" {
		t.Fatalf("last frame = %q, want [DONE] (invariant 4)", frames[3])
	}

	// Frame 1: role assistant + explicit empty content, no finish
	// (invariant 1 — the exact "content":"" serialisation is asserted
	// in raw JSON below).
	chunk1 := decodeChunk(t, frames[0])
	d1 := chunk1.Choices[0].Delta
	if d1.Role != "assistant" {
		t.Errorf("frame1 delta.role = %q, want assistant", d1.Role)
	}
	if d1.Content == nil || *d1.Content != "" {
		t.Errorf("frame1 delta.content = %v, want explicit empty string", d1.Content)
	}
	if !strings.Contains(frames[0], `"content":""`) {
		t.Errorf("frame1 raw JSON missing \"content\":\"\" — got %s", frames[0])
	}
	if chunk1.Choices[0].FinishReason != nil {
		t.Errorf("frame1 finish_reason = %v, want null", chunk1.Choices[0].FinishReason)
	}

	// Frame 2: content delta only — its text is the full template text
	// (invariant 2; the demo emits one segment, no injected delay).
	chunk2 := decodeChunk(t, frames[1])
	d2 := chunk2.Choices[0].Delta
	if d2.Role != "" {
		t.Errorf("frame2 delta.role = %q, want empty (role only on first delta)", d2.Role)
	}
	if d2.Content == nil || *d2.Content != "Hello! How can I help you today?" {
		t.Errorf("frame2 delta.content = %v, want the greeting text", d2.Content)
	}
	if chunk2.Choices[0].FinishReason != nil {
		t.Errorf("frame2 finish_reason = %v, want null", chunk2.Choices[0].FinishReason)
	}

	// Frame 3: empty delta {} + finish_reason "stop" + usage
	// (invariant 3).
	chunk3 := decodeChunk(t, frames[2])
	d3 := chunk3.Choices[0].Delta
	if d3.Role != "" || d3.Content != nil {
		t.Errorf("frame3 delta = %+v, want empty object {}", d3)
	}
	if got := chunk3.Choices[0].FinishReason; got == nil || *got != "stop" {
		t.Errorf("frame3 finish_reason = %v, want \"stop\"", got)
	}
	if chunk3.Usage == nil || *chunk3.Usage != (usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}) {
		t.Errorf("frame3 usage = %+v, want the fixed 1/1/2 usage", chunk3.Usage)
	}

	// All three chunks share the same id/object/model/created so the
	// adapter treats them as one completion.
	for i, f := range frames[:3] {
		ch := decodeChunk(t, f)
		if !strings.HasPrefix(ch.ID, "chatcmpl-") {
			t.Errorf("frame%d id = %q, want prefix \"chatcmpl-\"", i, ch.ID)
		}
		if ch.Object != "chat.completion.chunk" {
			t.Errorf("frame%d object = %q, want chat.completion.chunk", i, ch.Object)
		}
		if ch.Model != FakeModel {
			t.Errorf("frame%d model = %q, want %q", i, ch.Model, FakeModel)
		}
		if ch.Created != chunk1.Created {
			t.Errorf("frame%d created = %d, want the shared %d", i, ch.Created, chunk1.Created)
		}
		if ch.ID != chunk1.ID {
			t.Errorf("frame%d id = %q, want the shared %q", i, ch.ID, chunk1.ID)
		}
	}
}

// TestServeHTTP_NonStreaming verifies the single-JSON response shape
// (specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §4):
// choices[0].message.content = the template text, finish_reason "stop",
// usage attached.
func TestServeHTTP_NonStreaming(t *testing.T) {
	// given
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store)
	body := `{"model":"anything","stream":false,"messages":[{"role":"user","content":"hello"}]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	if resp.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", resp.Object)
	}
	if resp.Model != FakeModel {
		t.Errorf("model = %q, want %q", resp.Model, FakeModel)
	}
	if !strings.HasPrefix(resp.ID, "chatcmpl-") {
		t.Errorf("id = %q, want prefix \"chatcmpl-\"", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	c := resp.Choices[0]
	if c.FinishReason == nil || *c.FinishReason != "stop" {
		t.Fatalf("finish_reason = %v, want \"stop\"", c.FinishReason)
	}
	if c.Message == nil {
		t.Fatal("message is nil")
	}
	if c.Message.Role != "assistant" {
		t.Errorf("role = %q, want assistant", c.Message.Role)
	}
	if c.Message.Content == nil || *c.Message.Content != "Hello! How can I help you today?" {
		t.Errorf("content = %v, want the greeting text", c.Message.Content)
	}
	if resp.Usage == nil || *resp.Usage != (usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}) {
		t.Errorf("usage = %+v, want the fixed 1/1/2 usage", resp.Usage)
	}
}

// TestServeHTTP_HeadersTolerated covers the header obligations of
// specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §2: the dsh
// llm-deepseek adapter always sends a bearer authorization, the
// accept: text/event-stream header, the x-deepseek-harness-* identity
// headers and an attribution User-Agent — fake-llm must ignore them all
// (no validation, no rejection).
func TestServeHTTP_HeadersTolerated(t *testing.T) {
	// given
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store)

	tests := []struct {
		name   string
		header http.Header
	}{
		{
			name: "dummy bearer accepted",
			header: http.Header{
				"Authorization": []string{"Bearer dummy-fake-key"},
			},
		},
		{
			name: "dsh harness headers accepted",
			header: http.Header{
				"Authorization":                 []string{"Bearer dummy-fake-key"},
				"Accept":                        []string{"text/event-stream"},
				"X-Deepseek-Harness-User-Id":    []string{"anonymous-1"},
				"X-Deepseek-Harness-Session-Id": []string{"conv-1"},
				"X-Deepseek-Harness-Compact":    []string{"1"},
				"User-Agent":                    []string{"x-deepseek-ai-app/dsh-demo"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
				strings.NewReader(`{"stream":false,"messages":[{"role":"user","content":"hello"}]}`))
			for k, vs := range tt.header {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// then: the request is served normally — the headers never
			// influence the outcome.
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (headers must be tolerated)", rec.Code)
			}
			var resp completionResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
			}
			if resp.Choices[0].Message.Content == nil || *resp.Choices[0].Message.Content != "Hello! How can I help you today?" {
				t.Fatalf("content = %v, want the greeting text", resp.Choices[0].Message.Content)
			}
		})
	}
}

// TestServeHTTP_MalformedBody verifies the 400 error paths: a malformed
// JSON body, and a messages array carrying a null entry (which decodes
// to a nil element and must surface as the contract's error object —
// not a panic/reset) both return 400 with an OpenAI-style error object
// (specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §5).
func TestServeHTTP_MalformedBody(t *testing.T) {
	// given
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "malformed json returns 400",
			body: `{"model":`,
		},
		{
			name: "null messages element returns 400 instead of panicking",
			body: `{"stream":false,"messages":[null,{"role":"user","content":"hello"}]}`,
		},
		{
			name: "lone null messages element returns 400",
			body: `{"stream":true,"messages":[null]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tt.body)))

			// then
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			var resp errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal error response: %v\nbody: %s", err, rec.Body.String())
			}
			if resp.Error.Type != "invalid_request_error" {
				t.Errorf("error.type = %q, want invalid_request_error", resp.Error.Type)
			}
			if resp.Error.Message == "" {
				t.Error("error.message is empty, want a descriptive message")
			}
		})
	}
}

// Test_normalizeMessages covers the wire-to-matcher conversion
// directly, independent of the HTTP layer: field pass-through, content
// decoding, and the null-element rejection (a nil *messageParam yields
// the 400-worthy error naming the offending index).
func Test_normalizeMessages(t *testing.T) {
	tests := []struct {
		name    string
		params  []*messageParam
		want    []*chatMessage
		wantErr string
	}{
		{
			name:   "nil params yield no messages",
			params: nil,
			want:   nil,
		},
		{
			name: "role and decoded content passed through",
			params: []*messageParam{
				{Role: "user", Content: json.RawMessage(`"hello"`)},
				{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"Hi."},{"type":"text","text":"There."}]`)},
			},
			want: []*chatMessage{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "Hi. There."},
			},
		},
		{
			name:    "null element rejected with its index",
			params:  []*messageParam{{Role: "user", Content: json.RawMessage(`"a"`)}, nil},
			wantErr: "messages[1] is null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := normalizeMessages(tt.params)

			// then
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("normalizeMessages expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("normalizeMessages error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeMessages unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("normalizeMessages len = %d, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if *got[i] != *tt.want[i] {
					t.Fatalf("normalizeMessages[%d] = %+v, want %+v", i, *got[i], *tt.want[i])
				}
			}
		})
	}
}

// TestServeHTTP_MethodNotAllowed verifies non-POST methods are rejected
// with 405.
func TestServeHTTP_MethodNotAllowed(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestServeHTTP_FallbackAndDeterminism covers the US1 acceptance
// anchors over the embedded store: an unmatched message still returns
// 200 with the farewell fallback text, and repeating the SAME request —
// matched or not — always yields the identical reply (US1-2).
func TestServeHTTP_FallbackAndDeterminism(t *testing.T) {
	// given
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store)

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "keyword match returns the greeting text",
			body: `{"stream":false,"messages":[{"role":"user","content":"hello world"}]}`,
			want: "Hello! How can I help you today?",
		},
		{
			name: "no match returns the farewell fallback text",
			body: `{"stream":false,"messages":[{"role":"user","content":"xyzzy-no-such-keyword"}]}`,
			want: "I'm sorry, I didn't catch that.",
		},
		{
			name: "empty messages array returns the fallback text",
			body: `{"stream":false,"messages":[]}`,
			want: "I'm sorry, I didn't catch that.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: the same request is served twice.
			var replies []string
			for range 2 {
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tt.body)))
				// then (per call)
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", rec.Code)
				}
				var resp completionResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
				}
				if resp.Choices[0].Message.Content == nil || *resp.Choices[0].Message.Content != tt.want {
					t.Fatalf("content = %v, want %q", resp.Choices[0].Message.Content, tt.want)
				}
				replies = append(replies, *resp.Choices[0].Message.Content)
			}
			// then (determinism): equal requests yield equal replies.
			if replies[0] != replies[1] {
				t.Fatalf("repeated request drifted: %q then %q, want identical replies", replies[0], replies[1])
			}
		})
	}
}

// TestServeHTTP_ArrayContentDecode covers the OpenAI multimodal form
// where message.content is an array of typed parts: only the
// {type:"text"} entries contribute to the matching text, non-text parts
// are ignored.
func TestServeHTTP_ArrayContentDecode(t *testing.T) {
	// given: array-form content where the text part carries the "hello"
	// keyword and an image_url part sits in front of it.
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store)
	body := `{"stream":false,"messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}},` +
		`{"type":"text","text":"well hello there"}` +
		`]}]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: the decoded text parts hit the greeting keyword.
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	if resp.Choices[0].Message.Content == nil || *resp.Choices[0].Message.Content != "Hello! How can I help you today?" {
		t.Fatalf("content = %v, want the greeting text", resp.Choices[0].Message.Content)
	}
}

// Test_decodeContent covers the content decoder directly: string form,
// array-of-parts form, and the degraded cases.
func Test_decodeContent(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "string form returned as-is", raw: `"hello"`, want: "hello"},
		{name: "empty raw message", raw: ``, want: ""},
		{
			name: "array form joins text parts with space",
			raw:  `[{"type":"text","text":"a"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}},{"type":"text","text":"b"}]`,
			want: "a b",
		},
		{name: "malformed json array degrades to empty", raw: `[{"type":"text","text":`, want: ""},
		{name: "non-string non-array form degrades to empty", raw: `123`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeContent(json.RawMessage(tt.raw))
			if got != tt.want {
				t.Fatalf("decodeContent(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestHandleHealth verifies the liveness probe returns 200 "ok".
func TestHandleHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	HandleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want ok", rec.Body.String())
	}
}

// Test_generateResponseID pins the id convention: chatcmpl- prefix and
// uniqueness across calls (so the adapter never collapses two turns).
func Test_generateResponseID(t *testing.T) {
	first := generateResponseID()
	second := generateResponseID()
	if !strings.HasPrefix(first, "chatcmpl-") {
		t.Fatalf("id = %q, want prefix \"chatcmpl-\"", first)
	}
	if first == second {
		t.Fatalf("ids not unique: %q twice", first)
	}
}

// scanSSEFrames extracts the payload of every "data: ...\n\n" frame in
// r in order. It fails the test on a malformed stream rather than
// returning a partial result, so a missing terminator surfaces loudly.
func scanSSEFrames(t *testing.T, r io.Reader) []string {
	t.Helper()
	var frames []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			t.Fatalf("unexpected SSE line %q (want \"data: ...\")", line)
		}
		frames = append(frames, strings.TrimPrefix(line, "data: "))
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning SSE stream: %v", err)
	}
	return frames
}

// decodeChunk unmarshals one SSE data payload into a completionResponse
// (the same struct is reused for both full completions and chunks
// because their top-level shape matches).
func decodeChunk(t *testing.T, payload string) completionResponse {
	t.Helper()
	var ch completionResponse
	if err := json.Unmarshal([]byte(payload), &ch); err != nil {
		t.Fatalf("decode SSE payload %q: %v", payload, err)
	}
	return ch
}

// nonFlusher wraps an http.ResponseWriter while hiding the Flush
// method, producing a writer WITHOUT http.Flusher capability — the
// shape serveStreaming must reject up front (a ResponseRecorder alone
// always implements Flusher, so the wrapper is what makes the missing
// capability testable).
type nonFlusher struct {
	http.ResponseWriter
}

// Test_serveStreaming_NonFlusherWriter pins the capability-check
// ordering: when the writer cannot flush, the 500 error must be the
// committed response (status not yet sent, so http.Error still governs)
// and no SSE frame is emitted.
func Test_serveStreaming_NonFlusherWriter(t *testing.T) {
	// given: a recorder stripped of its Flusher capability.
	rec := httptest.NewRecorder()

	// when
	serveStreaming(nonFlusher{rec}, "some text", "chatcmpl-x")

	// then: the 500 is the actual committed status (had WriteHeader(200)
	// preceded the check, the status would be locked at 200 and this
	// assertion would fail with a superfluous-header warning), and the
	// body carries no partial SSE stream.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "data: ") {
		t.Fatalf("body contains SSE frames, want none:\n%s", rec.Body.String())
	}
}
