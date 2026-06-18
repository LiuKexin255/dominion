package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestServeHTTP_NonStreaming verifies the non-streaming JSON shape:
// one chat.completion object whose single choice carries an assistant
// message with BOTH reasoning_content and content, and finish_reason
// equal to "stop". A keyword-matched greeting is used as the input so
// the assertion pins the exact configured strings.
func TestServeHTTP_NonStreaming(t *testing.T) {
	// given: real embedded samples + a request whose user text matches
	// the greeting message's "hello" keyword.
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
	body := `{"model":"anything","stream":false,"messages":[{"role":"user","content":"hello there"}]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: 200 + application/json + chat.completion object shape.
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
	if !strings.HasPrefix(resp.ID, "fake-") {
		t.Errorf("id = %q, want prefix \"fake-\"", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	c := resp.Choices[0]
	if c.FinishReason == nil || *c.FinishReason != "stop" {
		t.Fatalf("finish_reason = %v, want \"stop\"", c.FinishReason)
	}
	if c.Message == nil {
		t.Fatalf("message is nil")
	}
	if c.Message.ReasoningContent != "The user is greeting me, I should respond warmly." {
		t.Errorf("reasoning_content = %q, want greeting reasoning", c.Message.ReasoningContent)
	}
	if c.Message.Content != "Hello! How can I help you today?" {
		t.Errorf("content = %q, want greeting text", c.Message.Content)
	}
	if c.Message.Role != "assistant" {
		t.Errorf("role = %q, want assistant", c.Message.Role)
	}
}

// TestServeHTTP_Streaming verifies the SSE wire shape: three
// "data: {...}\n\n" frames followed by a "data: [DONE]" terminator,
// in the contractually required order (role+reasoning delta, then
// content delta, then empty delta with finish_reason:"stop").
func TestServeHTTP_Streaming(t *testing.T) {
	// given: keyword match against greeting to keep the asserted
	// strings deterministic.
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
	body := `{"model":"x","stream":true,"messages":[{"role":"user","content":"hello"}]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: streaming headers
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
	if conn := rec.Header().Get("Connection"); conn != "keep-alive" {
		t.Fatalf("Connection = %q, want keep-alive", conn)
	}

	frames := scanSSEFrames(t, rec.Body)
	if n := len(frames); n != 4 {
		t.Fatalf("got %d SSE frames, want 4 (3 chunks + DONE):\n%s", n, rec.Body.String())
	}
	if frames[3] != "[DONE]" {
		t.Fatalf("last frame = %q, want [DONE]", frames[3])
	}

	// Frame 1: role assistant + reasoning_content, no content, no finish.
	chunk1 := decodeChunk(t, frames[0])
	if got := chunk1.Choices[0].Delta.Role; got != "assistant" {
		t.Errorf("frame1 delta.role = %q, want assistant (required by @langchain/openai)", got)
	}
	if got := chunk1.Choices[0].Delta.ReasoningContent; got != "The user is greeting me, I should respond warmly." {
		t.Errorf("frame1 delta.reasoning_content = %q, want greeting reasoning", got)
	}
	if got := chunk1.Choices[0].Delta.Content; got != "" {
		t.Errorf("frame1 delta.content = %q, want empty", got)
	}
	if chunk1.Choices[0].FinishReason != nil {
		t.Errorf("frame1 finish_reason = %v, want nil", chunk1.Choices[0].FinishReason)
	}

	// Frame 2: content delta (no role, no reasoning).
	chunk2 := decodeChunk(t, frames[1])
	if got := chunk2.Choices[0].Delta.Role; got != "" {
		t.Errorf("frame2 delta.role = %q, want empty (role only on first delta)", got)
	}
	if got := chunk2.Choices[0].Delta.Content; got != "Hello! How can I help you today?" {
		t.Errorf("frame2 delta.content = %q, want greeting text", got)
	}
	if chunk2.Choices[0].FinishReason != nil {
		t.Errorf("frame2 finish_reason = %v, want nil", chunk2.Choices[0].FinishReason)
	}

	// Frame 3: empty delta + finish_reason:"stop".
	chunk3 := decodeChunk(t, frames[2])
	if got := chunk3.Choices[0].Delta.Role; got != "" {
		t.Errorf("frame3 delta.role = %q, want empty", got)
	}
	if got := chunk3.Choices[0].Delta.Content; got != "" {
		t.Errorf("frame3 delta.content = %q, want empty", got)
	}
	if got := chunk3.Choices[0].Delta.ReasoningContent; got != "" {
		t.Errorf("frame3 delta.reasoning_content = %q, want empty", got)
	}
	if got := chunk3.Choices[0].FinishReason; got == nil || *got != "stop" {
		t.Errorf("frame3 finish_reason = %v, want \"stop\"", got)
	}

	// All three chunks must share the same id/object/model and a stable
	// created timestamp so the client treats them as one completion.
	for i, f := range frames[:3] {
		ch := decodeChunk(t, f)
		if !strings.HasPrefix(ch.ID, "fake-") {
			t.Errorf("frame%d id = %q, want prefix \"fake-\"", i, ch.ID)
		}
		if ch.Object != "chat.completion.chunk" {
			t.Errorf("frame%d object = %q, want chat.completion.chunk", i, ch.Object)
		}
		if ch.Model != FakeModel {
			t.Errorf("frame%d model = %q, want %q", i, ch.Model, FakeModel)
		}
	}
}

// TestServeHTTP_ArrayContentDecode covers the OpenAI multimodal form
// where message.content is an array of typed parts. Only the {type:
// "text"} entries should contribute to the user text used by Match.
// We drive a keyword match through the array form to prove the text
// is decoded end-to-end.
func TestServeHTTP_ArrayContentDecode(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

	// given: array-form content with a non-text part (which must be
	// ignored) interleaved with the text carrying the "bye" keyword.
	body := `{"stream":false,"messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"data:..."}},` +
		`{"type":"text","text":"time to say"}` +
		`]},{"role":"user","content":[{"type":"text","text":"bye now"}]}]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: the LAST user message wins and its text decoded from array
	// form hits the farewell keyword, so the response must carry the
	// farewell reasoning + text.
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	if got := resp.Choices[0].Message.ReasoningContent; got != "The user is saying goodbye." {
		t.Errorf("reasoning_content = %q, want farewell reasoning", got)
	}
	if got := resp.Choices[0].Message.Content; got != "Goodbye! Have a great day!" {
		t.Errorf("content = %q, want farewell text", got)
	}
}

// TestServeHTTP_NoMatchRandom verifies that a user prompt with no
// keyword match still returns 200 with a well-formed response. The
// body is the random fallback's configured Text rather than a hard
// error, so downstream callers always see a valid completion.
func TestServeHTTP_NoMatchRandom(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(99, 0)))

	// given: a prompt that matches no shipped keyword.
	body := `{"stream":false,"messages":[{"role":"user","content":"xyzzy-no-such-keyword"}]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: 200, application/json, and a message whose text is one of
	// the two configured responses (the random fallback).
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	got := resp.Choices[0].Message.Content
	if got != "Goodbye! Have a great day!" && got != "Hello! How can I help you today?" {
		t.Fatalf("random fallback content = %q, want one of the configured texts", got)
	}
}

// TestServeHTTP_MethodAndAuth covers two MUST-NOT-FAIL guards: the
// handler rejects non-POST methods with 405, and accepts any bearer
// token (including a bogus one) without validation.
func TestServeHTTP_MethodAndAuth(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

	t.Run("GET method rejected with 405", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("any bearer token accepted", func(t *testing.T) {
		body := `{"stream":false,"messages":[{"role":"user","content":"hello"}]}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer totally-bogus-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (auth must not be validated)", rec.Code)
		}
	})
}

// TestLastUserText covers the role+content extraction directly,
// independent of the HTTP layer: case-insensitive role match, LAST-user
// precedence, and the string/array content forms.
func TestLastUserText(t *testing.T) {
	tests := []struct {
		name     string
		messages []*messageParam
		want     string
	}{
		{
			name: "single user string content",
			messages: []*messageParam{
				{Role: "system", Content: rawJSON(`"sys"`)},
				{Role: "user", Content: rawJSON(`"hello"`)},
			},
			want: "hello",
		},
		{
			name: "last user wins when multiple present",
			messages: []*messageParam{
				{Role: "user", Content: rawJSON(`"first"`)},
				{Role: "assistant", Content: rawJSON(`"ack"`)},
				{Role: "user", Content: rawJSON(`"second"`)},
			},
			want: "second",
		},
		{
			name: "role case-insensitive (User)",
			messages: []*messageParam{
				{Role: "User", Content: rawJSON(`"cased"`)},
			},
			want: "cased",
		},
		{
			name: "array-form text parts joined with space",
			messages: []*messageParam{
				{Role: "user", Content: rawJSON(`[{"type":"text","text":"a"},{"type":"image_url"},{"type":"text","text":"b"}]`)},
			},
			want: "a b",
		},
		{
			name: "no user message returns empty",
			messages: []*messageParam{
				{Role: "system", Content: rawJSON(`"sys"`)},
				{Role: "assistant", Content: rawJSON(`"ack"`)},
			},
			want: "",
		},
		{
			name:     "no messages at all returns empty",
			messages: nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lastUserText(tt.messages)
			if got != tt.want {
				t.Fatalf("lastUserText = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestServeHTTP_RealServerSmoke runs the handler through a real
// httptest.Server and a real *http.Client so the streaming path is
// exercised end-to-end through Go's HTTP transport, not just a
// ResponseRecorder. This catches flusher/transport interaction
// regressions that httptest.NewRecorder cannot.
func TestServeHTTP_RealServerSmoke(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	body := `{"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// Read every byte; the server only closes the body after [DONE].
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	frames := scanSSEFrames(t, bytes.NewReader(raw))
	if n := len(frames); n != 4 {
		t.Fatalf("real-server SSE frame count = %d, want 4. Raw body:\n%s", n, string(raw))
	}
	if frames[3] != "[DONE]" {
		t.Fatalf("real-server last frame = %q, want [DONE]", frames[3])
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
			t.Fatalf("unexpected SSE line %q (want \"data: ...\" )", line)
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

// rawJSON is a tiny helper that returns a json.RawMessage literal from
// a string, failing the test if the input is not valid JSON. It keeps
// messageParam construction in the test readable.
func rawJSON(s string) json.RawMessage {
	if !json.Valid([]byte(s)) {
		panic("rawJSON: invalid JSON: " + s)
	}
	return json.RawMessage(s)
}
