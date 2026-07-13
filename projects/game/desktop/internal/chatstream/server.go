package chatstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"

	"dominion/projects/game/desktop/internal/applog"
)

const (
	// retryMS is the SSE client reconnect interval hint (milliseconds).
	// Chromium's EventSource default (3s on Chrome, ~4s on Edge) is too
	// aggressive; 2s balances recovery latency against CPU when auth fails
	// repeatedly. Emitted once per connection as "retry: 2000".
	retryMS = 2000

	// listenAddr is the loopback-only bind host (FR-008: never expose the
	// push channel to remote hosts — the token is in the query string and
	// must not traverse untrusted networks).
	listenAddr = "127.0.0.1"

	// chatStreamPath is the SSE endpoint path. Query params session+token
	// authenticate the connection (C2).
	chatStreamPath = "/api/v1/chat/stream"
)

// Server is the chat stream SSE server running on a loopback-only listener.
// It is safe for concurrent use. Start captures the listener synchronously
// so Endpoint returns the real URL immediately after Start returns (F10).
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	registry   *Registry
	logger     *applog.Logger
	// mu guards listener (set in Start, read in Endpoint, cleared in Stop).
	mu sync.Mutex
}

// NewServer creates a Server wired to the given Registry and logger. It does
// not start listening. The HTTP server's ErrorLog is silenced (R7: the
// default server logger echoes the request line, which contains the token in
// the query string — that must never reach logs).
func NewServer(reg *Registry, logger *applog.Logger) *Server {
	s := &Server{
		registry: reg,
		logger:   logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(chatStreamPath, s.serveChatStream)
	s.httpServer = &http.Server{
		Handler:  mux,
		ErrorLog: log.New(io.Discard, "", 0),
	}
	return s
}

// Start begins listening on 127.0.0.1:0 (OS-assigned ephemeral port) and
// serves the SSE handler in a background goroutine. The listener is captured
// synchronously before Serve runs, so Endpoint returns the real URL
// immediately on return (F10). Returns any listen error; on success Serve
// runs until Stop.
func (s *Server) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", listenAddr+":0")
	if err != nil {
		return fmt.Errorf("chatstream: listen: %w", err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	go s.httpServer.Serve(ln)
	return nil
}

// Endpoint returns the full HTTP URL of the SSE endpoint
// (http://127.0.0.1:<port>/api/v1/chat/stream), or the empty string before
// Start has been called (or after Stop). Safe to call at any time.
func (s *Server) Endpoint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return "http://" + s.listener.Addr().String() + chatStreamPath
}

// Stop forcefully tears the server down. It calls http.Server.Close (F6:
// NOT a graceful drain — SSE connections are long-lived and waiting for
// them to finish would block indefinitely) and then closes the listener.
// Safe to call even if Start was never called or already stopped.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	ln := s.listener
	s.listener = nil
	s.mu.Unlock()
	// httpServer.Close forcibly closes active connections and the listener
	// it is serving on. The explicit ln.Close covers the listener either
	// way (a duplicate close is benign) and keeps the teardown explicit.
	err := s.httpServer.Close()
	if ln != nil {
		_ = ln.Close()
	}
	return err
}

// serveChatStream is the GET /api/v1/chat/stream SSE handler. It authenticates
// via the session+token query params (C2 atomic token check), replays the
// backlog as of Last-Event-ID, then streams live events. Auth failures use
// text/plain (never text/event-stream) so EventSource treats them as a failed
// connection and reconnects (FR-009).
func (s *Server) serveChatStream(w http.ResponseWriter, r *http.Request) {
	// F16: only GET is supported; EventSource uses GET but be explicit so
	// other verbs get a definitive 405 rather than falling through.
	if r.Method != http.MethodGet {
		writePlain(w, http.StatusMethodNotAllowed, "method not allowed\n")
		return
	}

	// Query params: both session and token are required. §3.3: a missing
	// session or token is treated as unauthorized (401) so EventSource
	// treats the connection as failed and reconnects (FR-009), identical
	// to a stale-token rejection. R7: never log any URL component that may
	// carry the token — only r.URL.Path is safe.
	session := r.URL.Query().Get("session")
	token := r.URL.Query().Get("token")
	if session == "" || token == "" {
		writePlain(w, http.StatusUnauthorized, "unauthorized\n")
		return
	}

	stream := s.registry.Get(session)
	if stream == nil {
		s.logger.Error("backend", "warn: chatstream unknown session",
			map[string]any{"path": r.URL.Path, "remote": r.RemoteAddr})
		writePlain(w, http.StatusUnauthorized, "unauthorized\n")
		return
	}

	// Last-Event-ID is sent by the browser on reconnect only; absent on a
	// fresh connection. Parse defensively: a malformed value is treated as
	// 0 (replay everything), never an error.
	var lastEventID int64
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id >= 0 {
			lastEventID = id
		}
	}

	// C2 atomic subscribe: the token comparison and subscriber registration
	// happen under a single lock in the stream, so a stale token can never
	// observe a half-registered subscriber.
	sub, snapshot, status := stream.SubscribeAuthorized(token, lastEventID)
	switch status {
	case SubscribeStaleToken:
		s.logger.Error("backend", "warn: chatstream stale token",
			map[string]any{"path": r.URL.Path, "remote": r.RemoteAddr})
		writePlain(w, http.StatusUnauthorized, "unauthorized\n")
		return
	case SubscribeClosed:
		writePlain(w, http.StatusServiceUnavailable, "stream closed\n")
		return
	}
	defer sub.Close()

	// SSE response headers. Set before WriteHeader (implicit on first Write).
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("X-Accel-Buffering", "no")

	// net/http's ResponseWriter implements http.Flusher; assert so a future
	// wrapper that drops it fails loudly rather than buffering the response.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writePlain(w, http.StatusInternalServerError, "streaming unsupported\n")
		return
	}

	// Emit the reconnect interval hint once, then flush so the client
	// applies it immediately and the response is visibly streaming before
	// the first event.
	if _, err := io.WriteString(w, "retry: "+strconv.Itoa(retryMS)+"\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ctx := r.Context()

	// Replay the backlog captured atomically with subscription. Between
	// events, bail out if the client disconnected or the stream was torn
	// down (token rotation / close) so we never block a dead connection.
	for _, ev := range snapshot {
		if contextDone(ctx, sub) {
			return
		}
		if !emitEvent(w, flusher, ev) {
			return
		}
	}

	// Live fan-out loop. Each appended event arrives on sub.events; write
	// and flush it, then loop. Exit on client disconnect (ctx), stream
	// teardown (sub.done), or write error (client gone).
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.done:
			return
		case ev := <-sub.events:
			if !emitEvent(w, flusher, &ev) {
				return
			}
		}
	}
}

// emitEvent serializes and writes one logical ChatEvent to the SSE stream,
// flushing after. Small frames become a single "chat" event; large frames
// (serialized JSON > maxFragmentBytes) are split into a chunk group where
// only the final fragment carries an id: line (R8/F9: per-logical-event
// resume). Returns false if a write failed (client disconnected) so the
// caller can stop the loop.
func emitEvent(w http.ResponseWriter, flusher http.Flusher, ev *ChatEvent) bool {
	jsonBytes := SerializeFrame(ev.Frame)
	if len(jsonBytes) == 0 {
		// A well-formed AgentFrame never fails protojson; skip if it does.
		return true
	}

	pieces := ChunkPayload(jsonBytes)
	if pieces == nil {
		id := ev.ID
		if err := writeSSEEvent(w, "chat", &id, string(jsonBytes)); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for i, piece := range pieces {
		pieceJSON, err := json.Marshal(piece)
		if err != nil {
			// ChunkPiece always marshals; skip the fragment defensively.
			return true
		}
		var idPtr *int64
		if i == len(pieces)-1 {
			id := ev.ID
			idPtr = &id
		}
		if err := writeSSEEvent(w, "chunk", idPtr, string(pieceJSON)); err != nil {
			return false
		}
		flusher.Flush()
	}
	return true
}

// writeSSEEvent writes one SSE event to w with an optional id line and the
// provided data payload (already serialized to a string). id == nil omits
// the id: line — used for non-final chunk fragments so Last-Event-ID resume
// stays per-logical-event (R8/F9). The caller is responsible for flushing.
// Returns the first write error (typically a client disconnect).
func writeSSEEvent(w io.Writer, eventType string, id *int64, data string) error {
	if _, err := io.WriteString(w, "event: "+eventType+"\n"); err != nil {
		return err
	}
	if id != nil {
		if _, err := io.WriteString(w, "id: "+strconv.FormatInt(*id, 10)+"\n"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "data: "); err != nil {
		return err
	}
	if _, err := io.WriteString(w, data); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n\n"); err != nil {
		return err
	}
	return nil
}

// writePlain writes a short text/plain error body with the given status.
// Used for every non-SSE response (auth failure, bad method, missing params)
// so EventSource never sees text/event-stream on failure and treats the
// connection as failed (FR-009 auto-reconnect).
func writePlain(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	io.WriteString(w, body)
}

// contextDone reports whether the client disconnected (ctx) or the stream
// was torn down (sub.done) since the last event. Polled between backlog
// events so a disconnect mid-replay exits promptly.
func contextDone(ctx context.Context, sub *subscriber) bool {
	select {
	case <-ctx.Done():
		return true
	case <-sub.done:
		return true
	default:
		return false
	}
}
