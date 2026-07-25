package chatstream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/desktop/internal/applog"
)

// sseReadTimeout is the upper bound for any SSE read assertion. Real events
// arrive within microseconds (flushed per event); this only fails the test on
// an actual hang.
const sseReadTimeout = 3 * time.Second

// sseEvent is one parsed SSE event: its type, optional id line, and data
// payload. HasID distinguishes chunk fragments that intentionally omit the
// id: line (R8/F9).
type sseEvent struct {
	EventType string
	ID        string
	Data      string
	HasID     bool
}

// newTestServer builds a Server via NewServer (so httpServer is initialized
// for the real-listener tests) backed by a fresh Registry and logger. The
// handler-only tests drive serveChatStream via httptest and ignore the
// listener entirely.
func newTestServer() (*Server, *Registry, *applog.Logger) {
	logger := applog.NewLogger()
	reg := NewRegistry(logger)
	return NewServer(reg, logger), reg, logger
}

// newTestServerHTTP wraps serveChatStream in an httptest.Server. The returned
// client connects with session+token query params.
func newTestServerHTTP(t *testing.T) (*Server, *Registry, *applog.Logger, *httptest.Server) {
	t.Helper()
	srv, reg, logger := newTestServer()
	ts := httptest.NewServer(http.HandlerFunc(srv.serveChatStream))
	t.Cleanup(ts.Close)
	return srv, reg, logger, ts
}

// streamURL builds the SSE endpoint URL with the given session and token.
func streamURL(ts *httptest.Server, session, token string) string {
	return fmt.Sprintf("%s%s?session=%s&token=%s", ts.URL, chatStreamPath, session, token)
}

// connectSSE issues a GET to the SSE endpoint with the given session/token
// and optional Last-Event-ID header. The caller owns resp.Body and must close it.
func connectSSE(t *testing.T, ts *httptest.Server, session, token, lastEventID string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, streamURL(ts, session, token), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

// readSSEEvents reads up to n parsed SSE events from r, or fails the test on
// timeout/EOF-before-n. Snapshot events are flushed immediately, so n events
// arrive well within the timeout for a backlog-only stream. The reader
// goroutine exits when r errors (the caller closes resp.Body after collecting
// n), so no goroutine leaks past the test.
func readSSEEvents(t *testing.T, r io.Reader, n int) []sseEvent {
	t.Helper()
	eventsCh := make(chan sseEvent, n)
	errCh := make(chan error, 1)
	go func() {
		br := bufio.NewReader(r)
		var cur sseEvent
		hasContent := false
		for {
			line, err := br.ReadString('\n')
			trimmed := strings.TrimRight(line, "\n")
			trimmed = strings.TrimRight(trimmed, "\r")
			switch {
			case trimmed == "":
				if hasContent {
					eventsCh <- cur
					cur = sseEvent{}
					hasContent = false
				}
			case strings.HasPrefix(trimmed, "event: "):
				cur.EventType = strings.TrimPrefix(trimmed, "event: ")
				hasContent = true
			case strings.HasPrefix(trimmed, "id: "):
				cur.ID = strings.TrimPrefix(trimmed, "id: ")
				cur.HasID = true
				hasContent = true
			case strings.HasPrefix(trimmed, "data: "):
				cur.Data = strings.TrimPrefix(trimmed, "data: ")
				hasContent = true
			}
			if err != nil {
				errCh <- err
				return
			}
		}
	}()
	got := make([]sseEvent, 0, n)
	deadline := time.After(sseReadTimeout)
	for len(got) < n {
		select {
		case ev := <-eventsCh:
			got = append(got, ev)
		case err := <-errCh:
			if len(got) < n {
				t.Fatalf("readSSEEvents: stream ended with %d/%d events: %v", len(got), n, err)
			}
			return got
		case <-deadline:
			t.Fatalf("readSSEEvents timed out with %d/%d events", len(got), n)
		}
	}
	return got
}

// readFirstLine reads the first line from r in a goroutine, failing the test
// if nothing arrives within the deadline (i.e. the response was buffered
// instead of flushed immediately).
func readFirstLine(t *testing.T, r io.Reader, deadline time.Duration) string {
	t.Helper()
	ch := make(chan string, 1)
	go func() {
		br := bufio.NewReader(r)
		line, _ := br.ReadString('\n')
		ch <- line
	}()
	select {
	case line := <-ch:
		return line
	case <-time.After(deadline):
		t.Fatalf("readFirstLine: no data within %v (response not flushed)", deadline)
		return ""
	}
}

// bigTextFrame builds a content AgentFrame whose serialized JSON exceeds
// maxFragmentBytes (48 KiB), forcing ChunkPayload to fragment it.
func bigTextFrame(id int64) *game.AgentFrame {
	big := strings.Repeat("x", 60*1024)
	return &game.AgentFrame{
		SessionId: "big",
		FrameId:   fmt.Sprintf("frame-%d", id),
		Sender:    game.FrameSender_FRAME_SENDER_AGENT,
		Payload: &game.AgentFrame_MessageParts{
			MessageParts: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: big}}},
				},
			},
		},
	}
}

// TestServer_Method_405 verifies F16: a non-GET request gets 405 with a
// non-event-stream content type.
func TestServer_Method_405(t *testing.T) {
	_, reg, _, ts := newTestServerHTTP(t)
	stream, err := reg.Open("s1", func() ([]*game.Message, error) { return nil, nil })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, streamURL(ts, "s1", stream.Token()), nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
}

// TestServer_MissingParams_401 verifies §3.3: omitting session or token
// yields 401 Unauthorized (text/plain, never text/event-stream) so
// EventSource treats the connection as failed and reconnects (FR-009).
// Auth is checked before the stream lookup, so no registry access occurs.
func TestServer_MissingParams_401(t *testing.T) {
	_, _, _, ts := newTestServerHTTP(t)

	tests := []struct {
		name string
		url  string
	}{
		{name: "no params", url: ts.URL + chatStreamPath},
		{name: "session only", url: ts.URL + chatStreamPath + "?session=s1"},
		{name: "token only", url: ts.URL + chatStreamPath + "?token=t1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := ts.Client().Get(tt.url)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
				t.Errorf("Content-Type = %q, want text/plain", ct)
			}
		})
	}
}

// TestServer_InvalidSession_401 verifies that a session with no stream is
// rejected with 401.
func TestServer_InvalidSession_401(t *testing.T) {
	_, _, _, ts := newTestServerHTTP(t)

	resp, err := ts.Client().Get(streamURL(ts, "unknown", "any-token"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
}

// TestServer_StaleToken_401 verifies C2: after RotateToken, the previous
// token no longer authenticates (401).
func TestServer_StaleToken_401(t *testing.T) {
	_, reg, _, ts := newTestServerHTTP(t)
	stream, err := reg.Open("s1", func() ([]*game.Message, error) { return nil, nil })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	stale := stream.Token()
	stream.RotateToken()

	resp, err := ts.Client().Get(streamURL(ts, "s1", stale))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestServer_ClosedStream_503 verifies the SubscribeClosed branch: a stream
// that is in the registry but marked closed yields 503. (Registry.Close
// removes the stream from the map, so Get returns nil → 401; the 503 path is
// the close-vs-Get race window, exercised here by setting closed directly.)
func TestServer_ClosedStream_503(t *testing.T) {
	_, reg, _, ts := newTestServerHTTP(t)
	stream, err := reg.Open("s1", func() ([]*game.Message, error) { return nil, nil })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	token := stream.Token()
	// Mark closed but leave the stream in the registry so Get still resolves
	// it and SubscribeAuthorized returns SubscribeClosed.
	stream.mu.Lock()
	stream.closed = true
	stream.mu.Unlock()

	resp, err := ts.Client().Get(streamURL(ts, "s1", token))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestServer_ValidSSE_StreamsEvents verifies the happy path: a valid
// connection emits SSE headers, the retry line, the backlog snapshot, AND
// live events appended after the connection opened. Each "chat" event
// carries a monotonic id line.
func TestServer_ValidSSE_StreamsEvents(t *testing.T) {
	_, reg, _, ts := newTestServerHTTP(t)
	stream, err := reg.Open("s1", func() ([]*game.Message, error) {
		return testMessages(2), nil
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	resp := connectSSE(t, ts, "s1", stream.Token(), "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if ao := resp.Header.Get("Access-Control-Allow-Origin"); ao != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", ao)
	}
	if conn := resp.Header.Get("Connection"); conn != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", conn)
	}

	// Read the 2 snapshot events plus 1 live event in a SINGLE reader pass.
	// readSSEEvents spawns one bufio-backed goroutine; calling it twice on
	// the same body would let the first goroutine's internal buffer steal the
	// live event's bytes, deadlocking the second read at the timeout. Append
	// the live frame from a goroutine so it lands after the snapshot flush
	// but is consumed by that same reader.
	go func() {
		time.Sleep(50 * time.Millisecond) // let the snapshot flush first
		stream.Append(testFrame(99))
	}()

	events := readSSEEvents(t, resp.Body, 3)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	// events[0..1]: snapshot ids 1,2; events[2]: live id 3 — same wire shape.
	for i, ev := range events {
		if ev.EventType != "chat" {
			t.Errorf("events[%d].EventType = %q, want chat", i, ev.EventType)
		}
		if !ev.HasID {
			t.Errorf("events[%d] missing id line", i)
		}
		wantID := fmt.Sprintf("%d", i+1) // ids 1, 2, 3
		if ev.ID != wantID {
			t.Errorf("events[%d].ID = %q, want %s", i, ev.ID, wantID)
		}
		if !strings.Contains(ev.Data, `"content"`) {
			t.Errorf("events[%d].Data = %q, want JSON containing content", i, ev.Data)
		}
	}
}

// TestServer_LastEventID_Replay verifies FR-003a reconnect replay: with
// Last-Event-ID: 1, only events with id > 1 are emitted.
func TestServer_LastEventID_Replay(t *testing.T) {
	_, reg, _, ts := newTestServerHTTP(t)
	stream, err := reg.Open("s1", func() ([]*game.Message, error) {
		return testMessages(3), nil
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	resp := connectSSE(t, ts, "s1", stream.Token(), "1")
	defer resp.Body.Close()

	events := readSSEEvents(t, resp.Body, 2)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	for i, ev := range events {
		wantID := i + 2 // ids 2 and 3
		if ev.ID != fmt.Sprintf("%d", wantID) {
			t.Errorf("events[%d].ID = %q, want %d", i, ev.ID, wantID)
		}
	}
}

// TestServer_Flush_Immediate verifies the response starts streaming before
// the first event is appended — i.e. the retry line is flushed immediately
// rather than the whole response being buffered.
func TestServer_Flush_Immediate(t *testing.T) {
	_, reg, _, ts := newTestServerHTTP(t)
	stream, err := reg.Open("s1", func() ([]*game.Message, error) {
		return testMessages(1), nil
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	resp := connectSSE(t, ts, "s1", stream.Token(), "")
	defer resp.Body.Close()

	// The retry line must arrive promptly. If the server buffered the whole
	// response, nothing would arrive until the connection closed.
	first := readFirstLine(t, resp.Body, time.Second)
	if trimmed := strings.TrimSpace(first); !strings.HasPrefix(trimmed, "retry:") {
		t.Errorf("first line = %q, want a retry: line (immediate flush)", first)
	}
}

// TestServer_NoIDOnNonFinalChunks verifies R8/F9: a chunked event emits
// multiple "chunk" SSE events; only the final fragment carries an id: line,
// earlier fragments omit it so Last-Event-ID resume is per-logical-event.
func TestServer_NoIDOnNonFinalChunks(t *testing.T) {
	_, reg, _, ts := newTestServerHTTP(t)
	// Seed a single oversized frame directly so we know it fragments.
	stream, err := reg.Open("big", func() ([]*game.Message, error) { return nil, nil })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	stream.Append(bigTextFrame(1))

	// Compute the expected fragment count for the assertion.
	pieces := ChunkPayload(SerializeFrame(bigTextFrame(1)))
	if len(pieces) < 2 {
		t.Fatalf("bigTextFrame did not fragment: %d pieces", len(pieces))
	}
	want := len(pieces)

	resp := connectSSE(t, ts, "big", stream.Token(), "")
	defer resp.Body.Close()

	events := readSSEEvents(t, resp.Body, want)
	if len(events) != want {
		t.Fatalf("got %d events, want %d", len(events), want)
	}
	for i, ev := range events {
		if ev.EventType != "chunk" {
			t.Errorf("events[%d].EventType = %q, want chunk", i, ev.EventType)
		}
		if !strings.Contains(ev.Data, `"groupId"`) || !strings.Contains(ev.Data, `"index"`) {
			t.Errorf("events[%d].Data = %q, want chunk envelope", i, ev.Data)
		}
		isFinal := i == want-1
		if ev.HasID != isFinal {
			t.Errorf("events[%d] (index=%d) HasID = %v, want %v", i, i, ev.HasID, isFinal)
		}
		if isFinal && ev.ID != "1" {
			t.Errorf("final event ID = %q, want 1", ev.ID)
		}
	}
}

// TestServer_ContextCancel_Disconnects verifies the handler exits cleanly
// when the client cancels mid-stream: after cancellation, the body read
// returns an error (connection torn down) within a short window.
func TestServer_ContextCancel_Disconnects(t *testing.T) {
	_, reg, _, ts := newTestServerHTTP(t)
	stream, err := reg.Open("s1", func() ([]*game.Message, error) {
		return testMessages(1), nil
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		streamURL(ts, "s1", stream.Token()), nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	// Drain the backlog so we are parked in the live loop.
	_ = readSSEEvents(t, resp.Body, 1)

	// Cancel the client context. The handler's r.Context() fires Done and it
	// returns; the transport tears the connection down.
	cancel()
	buf := make([]byte, 16)
	if _, err := resp.Body.Read(buf); err == nil {
		t.Errorf("expected read error after cancel, got nil")
	}
	resp.Body.Close()
}

// TestServer_StartStop_LoopbackAndEndpoint verifies Start binds loopback,
// Endpoint returns the URL synchronously, and Stop closes without hanging.
// This is the only test exercising the real net listener (the rest use
// httptest with the handler directly).
func TestServer_StartStop_LoopbackAndEndpoint(t *testing.T) {
	srv, reg, _ := newTestServer()
	stream, err := reg.Open("s1", func() ([]*game.Message, error) { return nil, nil })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Endpoint is empty before Start.
	if got := srv.Endpoint(); got != "" {
		t.Errorf("Endpoint before Start = %q, want empty", got)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	ep := srv.Endpoint()
	if ep == "" {
		t.Fatal("Endpoint empty after Start")
	}
	if !strings.HasPrefix(ep, "http://127.0.0.1:") {
		t.Errorf("Endpoint = %q, want loopback http://127.0.0.1:<port>...", ep)
	}
	if !strings.HasSuffix(ep, chatStreamPath) {
		t.Errorf("Endpoint = %q, want suffix %s", ep, chatStreamPath)
	}

	// A real GET against the live server with a valid token streams the retry
	// line immediately, proving the bound listener serves the handler.
	resp, err := http.Get(ep + "?session=s1&token=" + stream.Token())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	first := readFirstLine(t, resp.Body, time.Second)
	if trimmed := strings.TrimSpace(first); !strings.HasPrefix(trimmed, "retry:") {
		t.Errorf("first line = %q, want retry: line", first)
	}
}

// TestServer_ConcurrentServe_StopNoHang guards F6 under -race: serving with
// an active in-flight request, Stop must return promptly (forceful Close,
// not a graceful drain that would block on long-lived SSE connections) and
// not deadlock the registry.
func TestServer_ConcurrentServe_StopNoHang(t *testing.T) {
	srv, reg, _ := newTestServer()
	stream, err := reg.Open("s1", func() ([]*game.Message, error) {
		return testMessages(1), nil
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Hold one open connection while we Stop.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, _ := http.Get(srv.Endpoint() + "?session=s1&token=" + stream.Token())
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()

	// Give the connection a moment to establish, then Stop must not block.
	time.Sleep(50 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		_ = srv.Stop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(sseReadTimeout):
		t.Fatal("Stop hung with an active SSE connection (F6 regression)")
	}
	wg.Wait()
}

// TestServer_TokenNeverLogged verifies R7/F18: the session token must never
// appear in any log entry. The handler logs only r.URL.Path and r.RemoteAddr
// — never r.URL, r.URL.RawQuery, r.URL.Query(), or any formatted URL. Every
// request path that produces a log line is exercised (unknown session, stale
// token), plus a valid stream, then every recorded Entry is scanned for the
// secret token substring.
func TestServer_TokenNeverLogged(t *testing.T) {
	_, reg, logger, ts := newTestServerHTTP(t)
	stream, err := reg.Open("s1", func() ([]*game.Message, error) {
		return testMessages(1), nil
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	current := stream.Token()
	stale := current
	stream.RotateToken()
	current = stream.Token() // new active token; stale is now invalid

	// Hit every log-producing path: unknown session (warn), stale token
	// (warn), and a valid connection (no warn expected, but covered). The
	// bodies are closed without draining — a valid SSE stream never EOFs,
	// so io.Copy would block forever. Closing cancels r.Context() and the
	// handler exits its live loop; logging happens synchronously during
	// request processing, before the live loop.
	for _, tt := range []struct {
		name, session, token string
	}{
		{name: "unknown session", session: "nope", token: current},
		{name: "stale token", session: "s1", token: stale},
		{name: "valid", session: "s1", token: current},
	} {
		resp, err := ts.Client().Get(streamURL(ts, tt.session, tt.token))
		if err != nil {
			t.Fatalf("%s: Get: %v", tt.name, err)
		}
		resp.Body.Close()
	}

	// Then: neither the active nor the stale token appears in any log field.
	for _, entry := range logger.Entries() {
		if strings.Contains(entry.Message, current) || strings.Contains(entry.Message, stale) {
			t.Errorf("token leaked into log message: %q", entry.Message)
		}
		for k, v := range entry.Fields {
			if s, ok := v.(string); ok {
				if s == current || s == stale || strings.Contains(s, current) || strings.Contains(s, stale) {
					t.Errorf("token leaked into log field %q: %q", k, s)
				}
			}
		}
	}
}

// TestServer_StaleToken_DoesNotRegisterSubscriber verifies C2 atomicity from
// the handler's perspective: a failed (stale-token) subscribe attempt must
// not leave a lingering subscriber in the stream's fan-out list. The
// subscriber is either fully registered or not at all; a stale token never
// produces a half-registered entry that later receives fan-out or leaks.
func TestServer_StaleToken_DoesNotRegisterSubscriber(t *testing.T) {
	_, reg, _, ts := newTestServerHTTP(t)
	stream, err := reg.Open("s1", func() ([]*game.Message, error) {
		return testMessages(1), nil
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	stale := stream.Token()
	stream.RotateToken()

	resp, err := ts.Client().Get(streamURL(ts, "s1", stale))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// The stream was rotated, so the subscriber list was already cleared by
	// RotateToken. The handler's SubscribeAuthorized(stale) must NOT have
	// re-added a subscriber. Assert the fan-out list is still empty.
	stream.mu.Lock()
	n := len(stream.subscribers)
	stream.mu.Unlock()
	if n != 0 {
		t.Errorf("stale-token subscribe left %d lingering subscriber(s); want 0", n)
	}
}
