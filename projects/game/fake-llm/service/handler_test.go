package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
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

// TestServeHTTP_StreamingStallAfterFirstChunk verifies the stall
// simulation (specs/043-llm-stream-stall-recovery): a request whose user
// text matches the stall-mid-reasoning keyword receives the opening
// role+reasoning delta and then NO further data while the connection
// stays alive. The request context's cancellation — what the agent's
// idle-timeout abort triggers — is the only way the stream unblocks.
func TestServeHTTP_StreamingStallAfterFirstChunk(t *testing.T) {
	// given: real embedded samples (the stall template is keyword-gated
	// and excluded from the random fallback pool) + a cancellable
	// request context so the test can abort the stall like the agent
	// does.
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		srv.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"x","stream":true,"messages":[{"role":"user","content":"please stall now"}]}`))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	// when: the request is served against a REAL transport (httptest
	// server) so the "no further data" half of the stall is observable —
	// a Recorder would buffer everything and hide the pause.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// then (1): the FIRST frame is the role+reasoning delta of the
	// stall template (the partial thinking the desktop sees).
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read first SSE line: %v", err)
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Fatalf("first SSE line %q, want prefix \"data: \"", line)
	}
	first := decodeChunk(t, strings.TrimPrefix(strings.TrimSpace(line), "data: "))
	if got := first.Choices[0].Delta.ReasoningContent; got != "The user asked me to simulate a stream stall. I will send this reasoning chunk and then stop sending data while keeping the connection alive." {
		t.Errorf("frame1 delta.reasoning_content = %q, want the stall template's reasoning", got)
	}
	// The second line of the first SSE event is the blank separator; it
	// must arrive (the first event is complete and flushed).
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatalf("read SSE separator after first frame: %v", err)
	}

	// then (2): NO further data arrives within the probe window — the
	// connection is alive (the server has no WriteTimeout) but the
	// stream is stalled, exactly the failure mode the agent's idle
	// timeout detects.
	moreData := make(chan struct{})
	go func() {
		_, _ = reader.ReadString('\n')
		close(moreData)
	}()
	select {
	case <-moreData:
		t.Fatal("received data after the first chunk — the stall did not pause the stream")
	case <-time.After(300 * time.Millisecond):
		// Expected: no data while stalled.
	}

	// then (3): cancelling the request context (what LangGraph's
	// NodeTimeoutError abort does) unblocks the handler; the connection
	// closes and the read returns an error instead of hanging forever.
	cancel()
	// Wait for the then(2) probe reader to finish first: it is still
	// blocked on the same bufio.Reader, and two goroutines calling
	// ReadString on one bufio.Reader concurrently is a data race. The
	// cancellation closes the connection, so the probe read returns an
	// error and the goroutine exits.
	select {
	case <-moreData:
	case <-time.After(5 * time.Second):
		t.Fatal("the stalled stream did not unblock after the request context was cancelled")
	}
	unblocked := make(chan struct{})
	go func() {
		for {
			if _, err := reader.ReadString('\n'); err != nil {
				break
			}
		}
		close(unblocked)
	}()
	select {
	case <-unblocked:
		// Expected: the handler returned once the context was cancelled.
	case <-time.After(5 * time.Second):
		t.Fatal("the stalled stream did not unblock after the request context was cancelled")
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
	validPNG := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVQI12NgAAIABQABNjN9GQAAAABJRf5ErkJggg=="
	body := `{"stream":false,"messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"` + validPNG + `"}},` +
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
	// the configured responses (the random fallback).
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	got := resp.Choices[0].Message.Content
	validTexts := map[string]bool{
		"Goodbye! Have a great day!":       true,
		"Hello! How can I help you today?": true,
		"Sure, let's chat!":                true,
	}
	if !validTexts[got] {
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

// TestServeHTTP_NonStreamingChunkedReasoning verifies the FR-006
// non-streaming path for a chunked-reasoning template
// (specs/046-fake-llm-think-chunking/data-model.md §8): chunking and
// intervals are streaming-only, so a non-streaming request returns the
// reasoning chunks concatenated into a single reasoning_content value
// with no delays.
func TestServeHTTP_NonStreamingChunkedReasoning(t *testing.T) {
	// given: an in-memory store with one chunked-reasoning template.
	store := newStoreFromMap(t, fstest.MapFS{
		"testdata/chunked.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"name: chunked-answer",
				"keywords:",
				"  - chunked question",
				"reasoning_chunks:",
				"  - \"Step one. \"",
				"  - \"Step two.\"",
				"text: Done.",
				"",
			}, "\n")),
		},
	})
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

	// when: a non-streaming request matching the chunked template.
	body := `{"stream":false,"messages":[{"role":"user","content":"answer the chunked question"}]}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: reasoning_content is the exact concatenation of the chunks.
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	if got := resp.Choices[0].Message.ReasoningContent; got != "Step one. Step two." {
		t.Errorf("reasoning_content = %q, want the concatenated chunks \"Step one. Step two.\"", got)
	}
	if got := resp.Choices[0].Message.Content; got != "Done." {
		t.Errorf("content = %q, want the configured text", got)
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

// lockedBuffer is a mutex-guarded bytes.Buffer used to capture slog
// output that a server goroutine writes while the test goroutine reads
// it. bytes.Buffer is not safe for concurrent use, and in the real-
// transport streaming tests the log write happens on the server
// goroutine after the frame flush the client has already observed, so
// there is no happens-before edge — both sides must hold the lock.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

// newLockedBuffer returns an empty lockedBuffer.
func newLockedBuffer() *lockedBuffer {
	return new(lockedBuffer)
}

// Write implements io.Writer for the slog TextHandler.
func (lb *lockedBuffer) Write(p []byte) (int, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.b.Write(p)
}

// String returns the captured output.
func (lb *lockedBuffer) String() string {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.b.String()
}

// TestServeHTTP_ToolResultTextResponse verifies the tools dispatch
// branch: a request whose last message role is "tool" matches against
// the tools config by tool_name and returns a plain text response with
// finish_reason "stop".
func TestServeHTTP_ToolResultTextResponse(t *testing.T) {
	// given: the real embedded store (which includes the tool-config
	// files operation_tools.yaml / saolei_tools.yaml / planner_tools.yaml)
	// and a tool-role request for the "mouse_move" tool. Feature 015
	// split the single mouse tool into mouse_move / mouse_click.
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
	body := `{"stream":false,"messages":[` +
		`{"role":"user","content":"take a screenshot"},` +
		`{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"mouse_move","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"call_1","name":"mouse_move","content":"screenshot captured at 1920x1080"}` +
		`]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: matched mouse-move-success-text (no substring constraint).
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	c := resp.Choices[0]
	if c.FinishReason == nil || *c.FinishReason != "stop" {
		t.Fatalf("finish_reason = %v, want \"stop\"", c.FinishReason)
	}
	if c.Message.Content != "I see the screen now." {
		t.Errorf("content = %q, want \"I see the screen now.\"", c.Message.Content)
	}
	if len(c.Message.ToolCalls) != 0 {
		t.Errorf("tool_calls len = %d, want 0 for text response", len(c.Message.ToolCalls))
	}
}

// TestServeHTTP_ToolResultToolCallResponse verifies the tool_call
// response format: when a tool config's respond_with.tool_call is set
// (and match_result_contains matches), the response carries tool_calls
// with the correct function name/arguments and finish_reason
// "tool_calls".
func TestServeHTTP_ToolResultToolCallResponse(t *testing.T) {
	// given: tool result for "mouse_move" whose text contains "button",
	// matching mouse-move-followup-click which responds with a mouse_click
	// tool_call (feature 015 split: a move can chain into a click).
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
	body := `{"stream":false,"messages":[` +
		`{"role":"user","content":"click the button"},` +
		`{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"mouse_move","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"call_1","name":"mouse_move","content":"found a button at 50,60"}` +
		`]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: tool_calls present with the correct shape.
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	c := resp.Choices[0]
	if c.FinishReason == nil || *c.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %v, want \"tool_calls\"", c.FinishReason)
	}
	if c.Message.Role != "assistant" {
		t.Errorf("role = %q, want assistant", c.Message.Role)
	}
	if len(c.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(c.Message.ToolCalls))
	}
	tc := c.Message.ToolCalls[0]
	if !strings.HasPrefix(tc.ID, "call_") {
		t.Errorf("tool_call.id = %q, want prefix \"call_\"", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("tool_call.type = %q, want \"function\"", tc.Type)
	}
	if tc.Function.Name != "mouse_click" {
		t.Errorf("tool_call.function.name = %q, want \"mouse_click\"", tc.Function.Name)
	}
	// Arguments must be a JSON string (not a JSON object), per the
	// OpenAI schema. After the US2 split a click carries only click_type.
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("tool_call.function.arguments is not valid JSON: %q (err: %v)", tc.Function.Arguments, err)
	}
	if args["click_type"] != "LEFT_CLICK" {
		t.Errorf("tool_call.function.arguments.click_type = %v, want LEFT_CLICK", args["click_type"])
	}
}

// TestServeHTTP_ToolResultStreamingToolCall verifies the streaming
// tool_call response: two SSE chunks (role+tool_calls delta, empty
// delta with finish_reason "tool_calls") followed by [DONE].
func TestServeHTTP_ToolResultStreamingToolCall(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
	body := `{"stream":true,"messages":[` +
		`{"role":"user","content":"click"},` +
		`{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"mouse_move","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"call_1","name":"mouse_move","content":"found a button at 50,60"}` +
		`]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: 3 SSE frames (2 chunks + DONE).
	frames := scanSSEFrames(t, rec.Body)
	if n := len(frames); n != 3 {
		t.Fatalf("got %d SSE frames, want 3 (2 chunks + DONE):\n%s", n, rec.Body.String())
	}
	if frames[2] != "[DONE]" {
		t.Fatalf("last frame = %q, want [DONE]", frames[2])
	}

	// Frame 1: role assistant + tool_calls. The mouse-move-followup-click
	// config chains a move result into a mouse_click tool_call.
	chunk1 := decodeChunk(t, frames[0])
	if got := chunk1.Choices[0].Delta.Role; got != "assistant" {
		t.Errorf("frame1 delta.role = %q, want assistant", got)
	}
	if len(chunk1.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("frame1 tool_calls len = %d, want 1", len(chunk1.Choices[0].Delta.ToolCalls))
	}
	tc := chunk1.Choices[0].Delta.ToolCalls[0]
	if tc.Function.Name != "mouse_click" {
		t.Errorf("frame1 tool_call.function.name = %q, want mouse_click", tc.Function.Name)
	}

	// Frame 2: empty delta + finish_reason "tool_calls".
	chunk2 := decodeChunk(t, frames[1])
	if got := chunk2.Choices[0].FinishReason; got == nil || *got != "tool_calls" {
		t.Errorf("frame2 finish_reason = %v, want \"tool_calls\"", got)
	}
}

// TestServeHTTP_ToolResultUnmatchedRandom verifies that a tool result
// whose tool_name matches no config falls through to the random
// fallback (matched=false), returning one of the configured tool
// responses without erroring.
func TestServeHTTP_ToolResultUnmatchedRandom(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(99, 0)))
	body := `{"stream":false,"messages":[` +
		`{"role":"user","content":"use unknown tool"},` +
		`{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"nonexistent","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"call_1","name":"nonexistent","content":"result"}` +
		`]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: 200 with a valid response (random fallback from tools).
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
}

// TestServeHTTP_ToolResultExtractToolNameViaCallID verifies that when
// the tool message's `name` field is absent, the handler falls back to
// extracting the tool_name from the preceding assistant message's
// tool_calls by matching tool_call_id.
func TestServeHTTP_ToolResultExtractToolNameViaCallID(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
	// given: tool message WITHOUT a name field, but with tool_call_id
	// that matches the preceding assistant's tool_call. Feature 015
	// renamed the mouse tool to mouse_move / mouse_click.
	body := `{"stream":false,"messages":[` +
		`{"role":"user","content":"screenshot"},` +
		`{"role":"assistant","tool_calls":[{"id":"call_abc","type":"function","function":{"name":"mouse_move","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"call_abc","content":"screenshot taken"}` +
		`]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: tool_name resolved via call_id lookup → mouse-move-success-text.
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	if resp.Choices[0].Message.Content != "I see the screen now." {
		t.Errorf("content = %q, want \"I see the screen now.\" (mouse_move matched via call_id lookup)", resp.Choices[0].Message.Content)
	}
}

// TestServeHTTP_ImageURLTextExtraction verifies that image_url content
// blocks are parsed correctly: text parts are extracted for keyword
// matching while the image data URL is never searched for keywords.
func TestServeHTTP_ImageURLTextExtraction(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

	// given: array-form content with a valid base64 image_url part whose
	// decoded bytes spell "hellohello..." (which must NOT trigger the
	// greeting keyword match — only the text part "bye" drives keyword
	// matching, so the farewell response is expected).
	imgB64 := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("hello", 50)))
	body := `{"stream":false,"messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,` + imgB64 + `"}},` +
		`{"type":"text","text":"bye now"}` +
		`]}]}`

	// when
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: farewell matched (text "bye"), NOT greeting (image data
	// contains "hello" but must be ignored).
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	if resp.Choices[0].Message.Content != "Goodbye! Have a great day!" {
		t.Errorf("content = %q, want farewell text (image data must not drive keyword match)", resp.Choices[0].Message.Content)
	}
}

// TestExtractToolName covers the tool-name resolution logic directly,
// independent of the HTTP layer: name-field first, then tool_call_id
// lookup, then empty.
func TestExtractToolName(t *testing.T) {
	tests := []struct {
		name     string
		messages []*messageParam
		want     string
	}{
		{
			name: "name field on tool message",
			messages: []*messageParam{
				{Role: "assistant", ToolCalls: []*toolCallParam{{ID: "c1", Function: toolCallParamFunction{Name: "wrong"}}}},
				{Role: "tool", ToolCallID: "c1", Name: "mouse"},
			},
			want: "mouse",
		},
		{
			name: "fallback to call_id lookup when name absent",
			messages: []*messageParam{
				{Role: "assistant", ToolCalls: []*toolCallParam{{ID: "c1", Function: toolCallParamFunction{Name: "keyboard"}}}},
				{Role: "tool", ToolCallID: "c1"},
			},
			want: "keyboard",
		},
		{
			name: "call_id not found returns empty",
			messages: []*messageParam{
				{Role: "assistant", ToolCalls: []*toolCallParam{{ID: "c2", Function: toolCallParamFunction{Name: "keyboard"}}}},
				{Role: "tool", ToolCallID: "c1"},
			},
			want: "",
		},
		{
			name: "no tool_call_id and no name returns empty",
			messages: []*messageParam{
				{Role: "tool"},
			},
			want: "",
		},
		{
			name:     "empty messages returns empty",
			messages: nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractToolName(tt.messages)
			if got != tt.want {
				t.Fatalf("extractToolName = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLastMessageRole covers the role extraction helper directly.
func TestLastMessageRole(t *testing.T) {
	tests := []struct {
		name     string
		messages []*messageParam
		want     string
	}{
		{name: "empty returns empty", messages: nil, want: ""},
		{name: "last user role", messages: []*messageParam{{Role: "system"}, {Role: "user"}}, want: "user"},
		{name: "last tool role", messages: []*messageParam{{Role: "user"}, {Role: "tool"}}, want: "tool"},
		{name: "assistant role preserved", messages: []*messageParam{{Role: "assistant"}}, want: "assistant"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lastMessageRole(tt.messages)
			if got != tt.want {
				t.Fatalf("lastMessageRole = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateImageContent(t *testing.T) {
	validPNG := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVQI12NgAAIABQABNjN9GQAAAABJRf5ErkJggg=="

	tests := []struct {
		name     string
		messages []*messageParam
		wantErr  bool
	}{
		{
			name:     "no image parts returns nil",
			messages: []*messageParam{{Role: "user", Content: rawJSON(`"hello"`)}},
			wantErr:  false,
		},
		{
			name: "valid base64 PNG returns nil",
			messages: []*messageParam{{
				Role:    "user",
				Content: rawJSON(`[{"type":"image_url","image_url":{"url":"` + validPNG + `"}},{"type":"text","text":"hi"}]`),
			}},
			wantErr: false,
		},
		{
			name: "garbage base64 returns error",
			messages: []*messageParam{{
				Role:    "user",
				Content: rawJSON(`[{"type":"image_url","image_url":{"url":"data:image/png;base64,137,80,78,71"}},{"type":"text","text":"hi"}]`),
			}},
			wantErr: true,
		},
		{
			name: "non-data URL returns error",
			messages: []*messageParam{{
				Role:    "user",
				Content: rawJSON(`[{"type":"image_url","image_url":{"url":"https://example.com/img.png"}},{"type":"text","text":"hi"}]`),
			}},
			wantErr: true,
		},
		{
			name: "wrong MIME returns error",
			messages: []*messageParam{{
				Role:    "user",
				Content: rawJSON(`[{"type":"image_url","image_url":{"url":"data:text/plain;base64,aGVsbG8="}},{"type":"text","text":"hi"}]`),
			}},
			wantErr: true,
		},
		{
			name: "missing ;base64 marker returns error",
			messages: []*messageParam{{
				Role:    "user",
				Content: rawJSON(`[{"type":"image_url","image_url":{"url":"data:image/png,iVBORw0KGgo="}},{"type":"text","text":"hi"}]`),
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateImageContent(tt.messages)
			if tt.wantErr && err == nil {
				t.Fatalf("validateImageContent returned nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateImageContent returned %v, want nil", err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "Param Incorrect") {
				t.Fatalf("error = %q, want to contain 'Param Incorrect'", err.Error())
			}
		})
	}
}

// newStoreFromMap builds a *MessageStore from an in-memory fstest.MapFS so
// tests can exercise custom Message/ToolConfig combinations (e.g. a Message
// carrying a tool_call) without touching the embedded testdata that the
// pinned TestNewMessageStore_LoadsEmbeddedSamples guards.
func newStoreFromMap(t *testing.T, files fstest.MapFS) *MessageStore {
	t.Helper()
	msgs, tools, err := LoadFromFS(files, "testdata")
	if err != nil {
		t.Fatalf("LoadFromFS unexpected error: %v", err)
	}
	return &MessageStore{messages: msgs, tools: tools}
}

// TestServeHTTP_UserMessageToolCall verifies the dispatch fix: a user turn
// (last message role "user") whose text matches a Message carrying a
// tool_call MUST produce a tool_calls response with finish_reason
// "tool_calls" — not the text path. This is what lets large tests drive the
// model→tool_call→execution chain from a plain user turn. The same store also
// carries a text-only Message, asserted separately to prove backward
// compatibility (nil ToolCall keeps the original behaviour).
func TestServeHTTP_UserMessageToolCall(t *testing.T) {
	// given: an in-memory store with one tool_call Message and one
	// text-only Message, so both dispatch paths are coverable from a
	// user turn.
	store := newStoreFromMap(t, fstest.MapFS{
		"testdata/tool_trigger.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"name: tool-trigger",
				"keywords:",
				"  - start game",
				"tool_call:",
				"  name: saolei_init",
				"  arguments: {}",
				"",
			}, "\n")),
		},
		"testdata/plain_text.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"name: plain-text",
				"keywords:",
				"  - chat",
				"text: Sure, let's chat!",
				"",
			}, "\n")),
		},
	})
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

	// when: a USER turn whose text matches the tool_call Message keyword.
	body := `{"stream":false,"messages":[{"role":"user","content":"please start game now"}]}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: tool_calls response shape.
	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	c := resp.Choices[0]
	if c.FinishReason == nil || *c.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %v, want \"tool_calls\"", c.FinishReason)
	}
	if len(c.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(c.Message.ToolCalls))
	}
	tc := c.Message.ToolCalls[0]
	if tc.Function.Name != "saolei_init" {
		t.Errorf("tool_call.function.name = %q, want saolei_init", tc.Function.Name)
	}
	// saolei_init takes no arguments (spec 023 C11 / FR-019); the dispatch
	// fix must still emit an empty (valid-JSON) arguments object.
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("tool_call arguments not valid JSON: %q (%v)", tc.Function.Arguments, err)
	}
	if len(args) != 0 {
		t.Errorf("tool_call arguments = %v, want empty object (saolei_init is argument-free per spec 023 C11)", args)
	}
}

// TestServeHTTP_UserMessageToolCallStreaming verifies the streaming shape of
// a user-turn tool_call response: two SSE chunks (role+tool_calls delta,
// then empty delta with finish_reason "tool_calls") followed by [DONE].
func TestServeHTTP_UserMessageToolCallStreaming(t *testing.T) {
	store := newStoreFromMap(t, fstest.MapFS{
		"testdata/tool_trigger.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"name: tool-trigger",
				"keywords:",
				"  - start game",
				"tool_call:",
				"  name: saolei_init",
				"  arguments: {}",
				"",
			}, "\n")),
		},
	})
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

	body := `{"stream":true,"messages":[{"role":"user","content":"start game"}]}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	frames := scanSSEFrames(t, rec.Body)
	if n := len(frames); n != 3 {
		t.Fatalf("got %d SSE frames, want 3 (2 chunks + DONE):\n%s", n, rec.Body.String())
	}
	if frames[2] != "[DONE]" {
		t.Fatalf("last frame = %q, want [DONE]", frames[2])
	}
	chunk1 := decodeChunk(t, frames[0])
	if got := chunk1.Choices[0].Delta.Role; got != "assistant" {
		t.Errorf("frame1 delta.role = %q, want assistant", got)
	}
	if len(chunk1.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("frame1 tool_calls len = %d, want 1", len(chunk1.Choices[0].Delta.ToolCalls))
	}
	if chunk1.Choices[0].Delta.ToolCalls[0].Function.Name != "saolei_init" {
		t.Errorf("frame1 tool_call.function.name = %q, want saolei_init",
			chunk1.Choices[0].Delta.ToolCalls[0].Function.Name)
	}
	chunk2 := decodeChunk(t, frames[1])
	if got := chunk2.Choices[0].FinishReason; got == nil || *got != "tool_calls" {
		t.Errorf("frame2 finish_reason = %v, want \"tool_calls\"", got)
	}
}

// TestServeHTTP_TextOnlyMessageUnchangedByToolCallField is a backward-
// compatibility guard: a text-only Message (nil ToolCall) matched from a
// user turn still produces a plain text response with finish_reason "stop",
// exactly as before the ToolCall field was added.
func TestServeHTTP_TextOnlyMessageUnchangedByToolCallField(t *testing.T) {
	store := newStoreFromMap(t, fstest.MapFS{
		"testdata/plain_text.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"name: plain-text",
				"keywords:",
				"  - chat",
				"reasoning: thinking here",
				"text: Sure, let's chat!",
				"",
			}, "\n")),
		},
	})
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

	body := `{"stream":false,"messages":[{"role":"user","content":"let's chat"}]}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	var resp completionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rec.Body.String())
	}
	c := resp.Choices[0]
	if c.FinishReason == nil || *c.FinishReason != "stop" {
		t.Fatalf("finish_reason = %v, want \"stop\"", c.FinishReason)
	}
	if len(c.Message.ToolCalls) != 0 {
		t.Errorf("tool_calls len = %d, want 0 for text-only Message", len(c.Message.ToolCalls))
	}
	if c.Message.Content != "Sure, let's chat!" {
		t.Errorf("content = %q, want the configured text", c.Message.Content)
	}
}

// TestServeHTTP_ChunkedReasoning verifies the streaming shape of a
// chunked-reasoning template (specs/046-fake-llm-think-chunking —
// specs/046-fake-llm-think-chunking/quickstart.md Scenario 1,
// FR-001/FR-004/FR-005): one reasoning_content
// delta per declared chunk, in order, with role only on the first
// delta; the content delta follows all reasoning deltas; finish +
// [DONE] terminate; concatenating the deltas reconstructs the full
// reasoning text exactly.
func TestServeHTTP_ChunkedReasoning(t *testing.T) {
	// given: an in-memory store with a chunked template (no delays, so
	// chunks stream back-to-back).
	store := newStoreFromMap(t, fstest.MapFS{
		"testdata/chunked.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"name: chunked-answer",
				"keywords:",
				"  - chunked question",
				"reasoning_chunks:",
				"  - \"Step one. \"",
				"  - \"Step two. \"",
				"  - \"Step three.\"",
				"text: Done.",
				"",
			}, "\n")),
		},
	})
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

	// when
	body := `{"stream":true,"messages":[{"role":"user","content":"answer the chunked question"}]}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	// then: 3 reasoning frames + content + finish + DONE.
	frames := scanSSEFrames(t, rec.Body)
	if n := len(frames); n != 6 {
		t.Fatalf("got %d SSE frames, want 6 (3 reasoning chunks + content + finish + DONE):\n%s", n, rec.Body.String())
	}
	if frames[5] != "[DONE]" {
		t.Fatalf("last frame = %q, want [DONE]", frames[5])
	}

	// Frame 0: role assistant + first chunk.
	chunk0 := decodeChunk(t, frames[0])
	if got := chunk0.Choices[0].Delta.Role; got != "assistant" {
		t.Errorf("frame0 delta.role = %q, want assistant (role only on the first delta)", got)
	}
	if got := chunk0.Choices[0].Delta.ReasoningContent; got != "Step one. " {
		t.Errorf("frame0 delta.reasoning_content = %q, want chunks[0]", got)
	}

	// Frames 1..2: remaining chunks, no role.
	wantChunks := []string{"Step two. ", "Step three."}
	for i, want := range wantChunks {
		ch := decodeChunk(t, frames[i+1])
		if got := ch.Choices[0].Delta.Role; got != "" {
			t.Errorf("frame%d delta.role = %q, want empty (role only on first delta)", i+1, got)
		}
		if got := ch.Choices[0].Delta.ReasoningContent; got != want {
			t.Errorf("frame%d delta.reasoning_content = %q, want %q", i+1, got, want)
		}
	}

	// Frame 3: content delta (FR-005: text follows all reasoning deltas).
	content := decodeChunk(t, frames[3])
	if got := content.Choices[0].Delta.Content; got != "Done." {
		t.Errorf("frame3 delta.content = %q, want the configured text", got)
	}

	// Frame 4: empty delta + finish_reason "stop".
	finish := decodeChunk(t, frames[4])
	if got := finish.Choices[0].FinishReason; got == nil || *got != "stop" {
		t.Errorf("frame4 finish_reason = %v, want \"stop\"", got)
	}

	// FR-004: concatenating the streamed deltas reconstructs the full
	// reasoning text exactly.
	var joined []string
	for _, f := range frames[:3] {
		joined = append(joined, decodeChunk(t, f).Choices[0].Delta.ReasoningContent)
	}
	if got := strings.Join(joined, ""); got != "Step one. Step two. Step three." {
		t.Errorf("concatenated reasoning = %q, want the full template reasoning", got)
	}
}

// TestServeHTTP_ChunkedReasoningDelays verifies the FR-002/FR-003
// interval mechanism through a real transport (specs/046-fake-llm-think-
// chunking — specs/046-fake-llm-think-chunking/quickstart.md
// Scenario 2): the wall-clock gap between
// consecutive reasoning frames matches each configured delay (per-gap
// independence included), and cancelling the request context mid-gap
// unblocks the handler promptly (context-aware sleep,
// specs/046-fake-llm-think-chunking/research.md D2).
func TestServeHTTP_ChunkedReasoningDelays(t *testing.T) {
	t.Run("gaps match the configured per-gap delays", func(t *testing.T) {
		// given: three chunks with two DIFFERENT gaps, so per-gap
		// independence (FR-003) is observable.
		store := newStoreFromMap(t, fstest.MapFS{
			"testdata/chunked_delays.yaml": &fstest.MapFile{
				Data: []byte(strings.Join([]string{
					"name: chunked-delays",
					"keywords:",
					"  - chunked delay question",
					"reasoning_chunks:",
					"  - \"c0\"",
					"  - \"c1\"",
					"  - \"c2\"",
					"chunk_delays:",
					"  - \"50ms\"",
					"  - \"80ms\"",
					"text: Done.",
					"",
				}, "\n")),
			},
		})
		handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
		srv := httptest.NewServer(handler)
		defer srv.Close()

		// when: stream through a real server, recording the arrival time
		// of each frame.
		body := `{"stream":true,"messages":[{"role":"user","content":"chunked delay question"}]}`
		resp, err := http.DefaultClient.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)

		var arrivals []time.Time
		var payloads []string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read SSE line: %v", err)
			}
			if line == "data: [DONE]\n" {
				break
			}
			if !strings.HasPrefix(line, "data: ") {
				t.Fatalf("unexpected SSE line %q", line)
			}
			arrivals = append(arrivals, time.Now())
			payloads = append(payloads, strings.TrimPrefix(strings.TrimSpace(line), "data: "))
			// consume the blank separator line.
			if _, err := reader.ReadString('\n'); err != nil {
				t.Fatalf("read SSE separator: %v", err)
			}
		}

		// then: 5 data frames (3 reasoning + content + finish).
		if n := len(payloads); n != 5 {
			t.Fatalf("got %d data frames, want 5 (3 reasoning + content + finish)", n)
		}
		reasonings := []string{}
		for _, p := range payloads[:3] {
			reasonings = append(reasonings, decodeChunk(t, p).Choices[0].Delta.ReasoningContent)
		}
		if strings.Join(reasonings, "") != "c0c1c2" {
			t.Fatalf("reasoning deltas = %v, want [c0 c1 c2] in order", reasonings)
		}

		// gap0 (before chunk1) ≈ 50ms, gap1 (before chunk2) ≈ 80ms;
		// generous tolerances keep the assertion robust under CI load
		// while still catching a missing/ignored sleep.
		gap0 := arrivals[1].Sub(arrivals[0])
		if gap0 < 35*time.Millisecond || gap0 > 400*time.Millisecond {
			t.Errorf("gap before chunk1 = %v, want ≈ 50ms", gap0)
		}
		gap1 := arrivals[2].Sub(arrivals[1])
		if gap1 < 55*time.Millisecond || gap1 > 400*time.Millisecond {
			t.Errorf("gap before chunk2 = %v, want ≈ 80ms", gap1)
		}
		// the content delta must follow immediately (no delay configured).
		contentGap := arrivals[3].Sub(arrivals[2])
		if contentGap > 400*time.Millisecond {
			t.Errorf("gap before content delta = %v, want ≈ 0 (no delay configured)", contentGap)
		}
	})

	t.Run("caller cancel mid-gap unblocks", func(t *testing.T) {
		// given: a long 5s gap so the handler is still sleeping when the
		// caller aborts; the gap would outlive the test otherwise.
		store := newStoreFromMap(t, fstest.MapFS{
			"testdata/chunked_cancel.yaml": &fstest.MapFile{
				Data: []byte(strings.Join([]string{
					"name: chunked-cancel",
					"keywords:",
					"  - chunked cancel question",
					"reasoning_chunks:",
					"  - \"c0\"",
					"  - \"c1\"",
					"chunk_delays:",
					"  - \"5s\"",
					"",
				}, "\n")),
			},
		})
		handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
		srv := httptest.NewServer(handler)
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			srv.URL+"/v1/chat/completions",
			strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"chunked cancel question"}]}`))
		if err != nil {
			t.Fatalf("NewRequestWithContext: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)

		// when: consume the first chunk, then cancel while the handler
		// sleeps inside the 5s gap.
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read first SSE line: %v", err)
		}
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read SSE separator: %v", err)
		}
		time.Sleep(100 * time.Millisecond) // let the handler enter the gap sleep
		cancel()

		// then: the read (and the handler) returns promptly instead of
		// sleeping the full 5s.
		unblocked := make(chan struct{})
		go func() {
			for {
				if _, err := reader.ReadString('\n'); err != nil {
					break
				}
			}
			close(unblocked)
		}()
		select {
		case <-unblocked:
			// Expected: context cancellation aborted the gap sleep.
		case <-time.After(2 * time.Second):
			t.Fatal("handler did not unblock after context cancel mid-gap")
		}
	})
}

// TestServeHTTP_ChunkedReasoningStallAfter verifies the positionable
// permanent stall (specs/046-fake-llm-think-chunking —
// specs/046-fake-llm-think-chunking/quickstart.md Scenario 3,
// FR-008/FR-009): a stall_after:K template emits exactly reasoning
// chunks 0..K and then no further data while the connection stays
// alive; cancelling the request context (the agent's idle-timeout
// abort) is the only unblock path.
func TestServeHTTP_ChunkedReasoningStallAfter(t *testing.T) {
	// given: a chunked template with stall_after:1 — chunks 0 and 1
	// must arrive, chunk 2 / content / finish must never.
	store := newStoreFromMap(t, fstest.MapFS{
		"testdata/chunked_stall_after.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"name: chunked-stall-after",
				"keywords:",
				"  - chunked stall after question",
				"reasoning_chunks:",
				"  - \"c0\"",
				"  - \"c1\"",
				"  - \"c2\"",
				"stall_after: 1",
				"text: Never arrives.",
				"",
			}, "\n")),
		},
	})
	handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		srv.URL+"/v1/chat/completions",
		strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"chunked stall after question"}]}`))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	// when: served against a REAL transport so the "no further data"
	// half of the stall is observable (a Recorder buffers everything).
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)

	// then (1): exactly chunks 0 and 1 arrive, in order — role only on
	// chunk 0 (the streaming-sequence contract §2).
	wantReasoning := []string{"c0", "c1"}
	for i, want := range wantReasoning {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE line for chunk %d: %v", i, err)
		}
		if !strings.HasPrefix(line, "data: ") {
			t.Fatalf("SSE line %q, want prefix \"data: \"", line)
		}
		ch := decodeChunk(t, strings.TrimPrefix(strings.TrimSpace(line), "data: "))
		if got := ch.Choices[0].Delta.ReasoningContent; got != want {
			t.Errorf("chunk %d reasoning_content = %q, want %q", i, got, want)
		}
		wantRole := ""
		if i == 0 {
			wantRole = "assistant"
		}
		if got := ch.Choices[0].Delta.Role; got != wantRole {
			t.Errorf("chunk %d delta.role = %q, want %q", i, got, wantRole)
		}
		// the blank separator completing the SSE event.
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read SSE separator after chunk %d: %v", i, err)
		}
	}

	// then (2): NO further frame arrives within the probe window — the
	// connection is alive but the stream is stalled after chunk 1.
	moreData := make(chan struct{})
	go func() {
		_, _ = reader.ReadString('\n')
		close(moreData)
	}()
	select {
	case <-moreData:
		t.Fatal("received data after chunk 1 — the stall did not fire at stall_after:1")
	case <-time.After(300 * time.Millisecond):
		// Expected: no data while stalled.
	}

	// then (3): cancelling the request context unblocks the handler
	// (FR-009); the connection closes and the read returns instead of
	// hanging forever.
	cancel()
	select {
	case <-moreData:
	case <-time.After(5 * time.Second):
		t.Fatal("the stalled stream did not unblock after the request context was cancelled")
	}
	unblocked := make(chan struct{})
	go func() {
		for {
			if _, err := reader.ReadString('\n'); err != nil {
				break
			}
		}
		close(unblocked)
	}()
	select {
	case <-unblocked:
		// Expected: the handler returned once the context was cancelled.
	case <-time.After(5 * time.Second):
		t.Fatal("the stalled stream did not unblock after the request context was cancelled")
	}
}

// TestServeHTTP_ChunkedReasoningLogs verifies the FR-018/SC-007
// observability contract (specs/046-fake-llm-think-chunking —
// specs/046-fake-llm-think-chunking/quickstart.md Scenario 8): every
// streamed reasoning chunk emits a
// structured slog entry carrying chunk_index / role_kind / delay_ms,
// and the frame after which the stream stalls carries the EFFECTIVE
// stall_after index — 0 for the legacy stall:true shorthand and the
// configured position for a stall_after:K template.
func TestServeHTTP_ChunkedReasoningLogs(t *testing.T) {
	t.Run("one entry per reasoning chunk with delay_ms", func(t *testing.T) {
		// given: a chunked+delay template (no stall) so all reasoning
		// chunks stream; slog is redirected into a buffer.
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
		defer slog.SetDefault(prev)

		store := newStoreFromMap(t, fstest.MapFS{
			"testdata/chunked_logs.yaml": &fstest.MapFile{
				Data: []byte(strings.Join([]string{
					"name: chunked-logs",
					"keywords:",
					"  - chunked log question",
					"reasoning_chunks:",
					"  - \"c0\"",
					"  - \"c1\"",
					"  - \"c2\"",
					"chunk_delays:",
					"  - \"50ms\"",
					"  - \"50ms\"",
					"text: Done.",
					"",
				}, "\n")),
			},
		})
		handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))

		// when
		body := `{"stream":true,"messages":[{"role":"user","content":"chunked log question"}]}`
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

		// then: one entry per reasoning chunk with the required fields.
		logged := buf.String()
		for _, want := range []string{
			"stream chunk emitted",
			"role_kind=reasoning chunk_index=0 delay_ms=0",
			"role_kind=reasoning chunk_index=1 delay_ms=50",
			"role_kind=reasoning chunk_index=2 delay_ms=50",
		} {
			if !strings.Contains(logged, want) {
				t.Errorf("slog output missing %q, got:\n%s", want, logged)
			}
		}
	})

	t.Run("legacy stall frame carries stall_after", func(t *testing.T) {
		// given: a chunked+delay+legacy-stall template (stall after the
		// first chunk, FR-010) so only chunk 0 streams before the block.
		// The capture buffer is mutex-guarded: the server goroutine
		// writes the log AFTER the frame flush (flush → logStreamFrame →
		// block), so the client can observe the frame before the log
		// entry exists and bytes.Buffer is not safe for concurrent
		// read/write.
		buf := newLockedBuffer()
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
		defer slog.SetDefault(prev)

		store := newStoreFromMap(t, fstest.MapFS{
			"testdata/chunked_stall_logs.yaml": &fstest.MapFile{
				Data: []byte(strings.Join([]string{
					"name: chunked-stall-logs",
					"keywords:",
					"  - chunked stall log question",
					"reasoning_chunks:",
					"  - \"c0\"",
					"  - \"c1\"",
					"chunk_delays:",
					"  - \"50ms\"",
					"stall: true",
					"",
				}, "\n")),
			},
		})
		handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
		srv := httptest.NewServer(handler)
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			srv.URL+"/v1/chat/completions",
			strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"chunked stall log question"}]}`))
		if err != nil {
			t.Fatalf("NewRequestWithContext: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()

		// when: consume chunk 0 (the only frame before the stall), then
		// cancel to unblock the handler.
		reader := bufio.NewReader(resp.Body)
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read first SSE line: %v", err)
		}
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read SSE separator: %v", err)
		}
		cancel()
		go func() {
			for {
				if _, err := reader.ReadString('\n'); err != nil {
					return
				}
			}
		}()

		// then: the stalled frame's entry carries stall_after=0. Poll
		// until the server goroutine has written the entry (it is
		// emitted after the flush the client already observed), with a
		// deadline so a genuinely missing entry fails loudly.
		want := "role_kind=reasoning chunk_index=0 delay_ms=0 stall_after=0"
		deadline := time.Now().Add(2 * time.Second)
		for {
			if strings.Contains(buf.String(), want) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("slog output missing the stall frame entry %q, got:\n%s", want, buf.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
	})

	t.Run("positioned stall frame carries the effective index", func(t *testing.T) {
		// given: a chunked template with stall_after:1 (no delays) so
		// chunks 0 and 1 stream back-to-back and the block fires after
		// chunk 1. The capture buffer is mutex-guarded for the same
		// reason as in the legacy-stall subtest: the server goroutine
		// writes the log after the flush the client has observed.
		buf := newLockedBuffer()
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
		defer slog.SetDefault(prev)

		store := newStoreFromMap(t, fstest.MapFS{
			"testdata/chunked_stall_after_logs.yaml": &fstest.MapFile{
				Data: []byte(strings.Join([]string{
					"name: chunked-stall-after-logs",
					"keywords:",
					"  - chunked stall after log question",
					"reasoning_chunks:",
					"  - \"c0\"",
					"  - \"c1\"",
					"  - \"c2\"",
					"stall_after: 1",
					"",
				}, "\n")),
			},
		})
		handler := NewChatHandler(store, rand.New(rand.NewPCG(1, 0)))
		srv := httptest.NewServer(handler)
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			srv.URL+"/v1/chat/completions",
			strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"chunked stall after log question"}]}`))
		if err != nil {
			t.Fatalf("NewRequestWithContext: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()

		// when: consume chunks 0 and 1 (the frames before the stall),
		// then cancel to unblock the handler after the stall fires.
		reader := bufio.NewReader(resp.Body)
		for i := 0; i < 2; i++ {
			if _, err := reader.ReadString('\n'); err != nil {
				t.Fatalf("read SSE line for chunk %d: %v", i, err)
			}
			if _, err := reader.ReadString('\n'); err != nil {
				t.Fatalf("read SSE separator after chunk %d: %v", i, err)
			}
		}
		cancel()
		go func() {
			for {
				if _, err := reader.ReadString('\n'); err != nil {
					return
				}
			}
		}()

		// then: the stalled frame's entry carries the EFFECTIVE index
		// (stall_after=1 on chunk_index=1, FR-018), and no stall_after=0
		// entry exists anywhere — a regression to the legacy hardcoded 0
		// would fail the positive assertion and a leak of the field onto
		// non-stall frames would fail the negative one.
		want := "role_kind=reasoning chunk_index=1 delay_ms=0 stall_after=1"
		deadline := time.Now().Add(2 * time.Second)
		for {
			if strings.Contains(buf.String(), want) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("slog output missing the stall frame entry %q, got:\n%s", want, buf.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
		if strings.Contains(buf.String(), "stall_after=0") {
			t.Fatalf("slog output carries a hardcoded stall_after=0 entry, got:\n%s", buf.String())
		}
	})
}

// TestTextStreamChunks_NonChunked_Unchanged pins the FR-007 backward
// compatibility (specs/046-fake-llm-think-chunking/contracts/
// streaming-sequence.md §3-§4): for a non-chunked spec, buildTextChunks
// emits byte-identical frames to the pre-change textStreamChunks —
// including the unconditional content delta for a text-less template.
// It also covers the chunked reasoning-only frame count (the chunked
// path MAY omit the content delta, §4).
func TestTextStreamChunks_NonChunked_Unchanged(t *testing.T) {
	// given: the pre-change builder, preserved verbatim as the golden
	// reference for byte-equality.
	legacy := func(respID string, now int64, reasoning, text string) []*completionResponse {
		return []*completionResponse{
			{
				ID: respID, Object: "chat.completion.chunk",
				Created: now, Model: FakeModel,
				Choices: []*choice{{
					Index: 0,
					Delta: &assistantMessage{
						Role:             "assistant",
						ReasoningContent: reasoning,
					},
					FinishReason: nil,
				}},
			},
			{
				ID: respID, Object: "chat.completion.chunk",
				Created: now, Model: FakeModel,
				Choices: []*choice{{
					Index: 0,
					Delta: &assistantMessage{
						Content: text,
					},
					FinishReason: nil,
				}},
			},
			{
				ID: respID, Object: "chat.completion.chunk",
				Created: now, Model: FakeModel,
				Choices: []*choice{{
					Index:        0,
					Delta:        new(assistantMessage),
					FinishReason: strPtr(finishStop),
				}},
			},
		}
	}
	marshalFrames := func(frames []*completionResponse) []byte {
		t.Helper()
		var b bytes.Buffer
		for _, f := range frames {
			data, err := json.Marshal(f)
			if err != nil {
				t.Fatalf("marshal frame: %v", err)
			}
			b.Write(data)
		}
		return b.Bytes()
	}

	tests := []struct {
		name string
		spec responseSpec
	}{
		{
			name: "legacy template with reasoning and text",
			spec: responseSpec{Reasoning: []string{"The user is greeting me."}, Text: "Hello!"},
		},
		{
			name: "legacy text-less template keeps the empty content delta",
			spec: responseSpec{Reasoning: []string{"only thinking"}},
		},
		{
			name: "legacy template without reasoning (role-only first delta)",
			spec: responseSpec{Text: "text only"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasoning := ""
			if len(tt.spec.Reasoning) > 0 {
				reasoning = tt.spec.Reasoning[0]
			}
			want := marshalFrames(legacy("fake-id", 123, reasoning, tt.spec.Text))

			// when
			got := marshalFrames(buildTextChunks("fake-id", 123, tt.spec))

			// then: byte-identical with the pre-change builder.
			if !bytes.Equal(got, want) {
				t.Fatalf("non-chunked bytes differ from pre-change output:\ngot:  %s\nwant: %s", got, want)
			}
		})
	}

	// The chunked reasoning-only path omits the content delta (§4): 2
	// reasoning frames + finish = 3 frames total, no content frame.
	got := buildTextChunks("fake-id", 123, responseSpec{Reasoning: []string{"c0", "c1"}})
	if n := len(got); n != 3 {
		t.Fatalf("chunked reasoning-only frames = %d, want 3 (2 reasoning + finish, content delta omitted)", n)
	}
	for i, f := range got {
		if i < len(got)-1 && f.Choices[0].FinishReason != nil {
			t.Fatalf("finish frame at index %d, want it last", i)
		}
		if i < len(got)-1 && f.Choices[0].Delta.Content != "" {
			t.Fatalf("unexpected content delta at index %d on the reasoning-only path", i)
		}
	}
}
