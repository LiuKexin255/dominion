// Package chatstream implements the per-session SSE event log and fan-out
// for the desktop chat push channel. A ChatStream is an append-only log of
// AgentFrames with stable monotonic IDs; a Registry maps session IDs to
// ChatStreams and provides single-flight open semantics.
package chatstream

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"dominion/projects/game"
	"dominion/projects/game/desktop/internal/applog"
)

// subscriberBuffer is the capacity of each subscriber's event channel.
// A subscriber that falls behind by more than this many unread events is
// evicted (F3 backpressure).
const subscriberBuffer = 64

// ChatEvent is one entry in the per-session event log. ID is the stable
// monotonic 1-based sequence number assigned at append time; Frame is the
// agent frame payload.
type ChatEvent struct {
	ID    int64
	Frame *game.AgentFrame
}

// subscriber represents a connected SSE client. events delivers live
// ChatEvents; done is closed to signal disconnect (slow-consumer
// eviction, token rotation, or stream close). closeOnce ensures done is
// closed at most once across concurrent evict/close paths.
type subscriber struct {
	events      chan ChatEvent
	done        chan struct{}
	closeOnce   sync.Once
	lastEventID int64
	stream      *ChatStream
}

// SubscribeStatus is the result of SubscribeAuthorized.
type SubscribeStatus int

const (
	// SubscribeOK means the subscription was created successfully.
	SubscribeOK SubscribeStatus = iota
	// SubscribeStaleToken means the supplied token no longer matches the
	// stream's current token (rotated since the handoff was issued).
	SubscribeStaleToken
	// SubscribeClosed means the stream has been closed and cannot accept
	// new subscribers.
	SubscribeClosed
)

// ChatStream is the per-session SSE event log with fan-out to
// subscribers. nextID is the highest assigned event ID (0 when empty);
// events are appended with ID = nextID+1. All methods are safe for
// concurrent use.
type ChatStream struct {
	sessionID   string
	events      []*ChatEvent
	nextID      int64
	subscribers []*subscriber
	token       string
	closed      bool
	mu          sync.Mutex
	logger      *applog.Logger
}

// Registry is the thread-safe session-to-ChatStream map. Open uses a
// single-flight pattern (R3f) so concurrent opens for the same session
// share a single creation and seed.
type Registry struct {
	streams  map[string]*ChatStream
	mu       sync.Mutex
	logger   *applog.Logger
	inflight map[string]chan struct{}
}

// NewRegistry returns a new Registry. logger must not be nil.
func NewRegistry(logger *applog.Logger) *Registry {
	return &Registry{
		streams:  make(map[string]*ChatStream),
		inflight: make(map[string]chan struct{}),
		logger:   logger,
	}
}

// Append records frame as the next event in the log and fans it out to
// every subscriber. Slow subscribers whose event channel is full are
// evicted in place (F3 backpressure). Append to a closed stream is a
// logged no-op (F5 close-safety): it never panics and never enqueues.
func (s *ChatStream) Append(frame *game.AgentFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		s.logger.Error("backend", "warn: chatstream Append to closed stream", map[string]any{"session_id": s.sessionID})
		return
	}
	s.nextID++
	ev := ChatEvent{ID: s.nextID, Frame: frame}
	s.events = append(s.events, &ev)

	// Fan out non-blocking (F3): a full subscriber channel evicts the
	// subscriber under the same lock, so the snapshot a concurrent
	// Subscribe observes is always consistent with the live fan-out.
	kept := s.subscribers[:0]
	for _, sub := range s.subscribers {
		select {
		case sub.events <- ev:
			kept = append(kept, sub)
		default:
			sub.evictLocked()
			s.logger.Error("backend", "warn: chatstream subscriber evicted (slow consumer)", map[string]any{"session_id": s.sessionID})
		}
	}
	for i := len(kept); i < len(s.subscribers); i++ {
		s.subscribers[i] = nil
	}
	s.subscribers = kept
}

// Token returns the stream's current subscription token.
func (s *ChatStream) Token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

// LastID returns the highest event ID in the log (0 if empty).
func (s *ChatStream) LastID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextID
}

// Subscribe registers a new subscriber starting after lastEventID. The
// returned snapshot contains every event with ID > lastEventID and is
// delivered directly to the client; it is NOT enqueued into the
// subscriber's channel (F4 atomicity: snapshot capture and subscriber
// registration happen under a single lock). Returns (nil, nil) if the
// stream is closed.
func (s *ChatStream) Subscribe(lastEventID int64) (*subscriber, []*ChatEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil
	}
	return s.subscribeLocked(lastEventID), s.snapshotLocked(lastEventID)
}

// SubscribeAuthorized is Subscribe with an atomic token check (C2): the
// token comparison and subscriber registration occur under a single lock
// so a stale token can never observe a half-registered subscriber.
func (s *ChatStream) SubscribeAuthorized(token string, lastEventID int64) (*subscriber, []*ChatEvent, SubscribeStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, SubscribeClosed
	}
	if s.token != token {
		return nil, nil, SubscribeStaleToken
	}
	return s.subscribeLocked(lastEventID), s.snapshotLocked(lastEventID), SubscribeOK
}

// subscribeLocked registers a new subscriber. Caller must hold s.mu.
func (s *ChatStream) subscribeLocked(lastEventID int64) *subscriber {
	sub := &subscriber{
		events:      make(chan ChatEvent, subscriberBuffer),
		done:        make(chan struct{}),
		lastEventID: lastEventID,
		stream:      s,
	}
	s.subscribers = append(s.subscribers, sub)
	return sub
}

// snapshotLocked returns events with ID > lastEventID in append order.
// Caller must hold s.mu.
func (s *ChatStream) snapshotLocked(lastEventID int64) []*ChatEvent {
	var snap []*ChatEvent
	for _, ev := range s.events {
		if ev.ID > lastEventID {
			snap = append(snap, ev)
		}
	}
	return snap
}

// SeedFromHistory normalizes persisted history Messages into content
// AgentFrames and appends them to the log. Each Message becomes one
// event preserving its original messageId, sender, createTime, and
// content PartBlock; the sessionID is rewritten to this stream's
// session so frames are consistent across history/live boundaries.
func (s *ChatStream) SeedFromHistory(msgs []*game.Message) {
	for _, msg := range msgs {
		frame := &game.AgentFrame{
			SessionId:  s.sessionID,
			FrameId:    msg.GetMessageId(),
			Sender:     msg.GetSender(),
			CreateTime: msg.GetCreateTime(),
			Payload:    &game.AgentFrame_Content{Content: msg.GetContent()},
		}
		s.Append(frame)
	}
}

// RotateToken issues a new 32-byte hex token, disconnects every existing
// subscriber (their done channels are closed via closeOnce), and clears
// the subscriber list. Returns the new token (or the prior token if the
// crypto source failed, which should not happen in practice).
func (s *ChatStream) RotateToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		s.logger.Error("backend", "warn: chatstream rand failed; keeping current token", map[string]any{"session_id": s.sessionID})
	} else {
		s.token = hex.EncodeToString(b)
	}
	for _, sub := range s.subscribers {
		sub.closeOnce.Do(func() { close(sub.done) })
	}
	s.subscribers = nil
	return s.token
}

// MatchToken reports whether t equals the stream's current token.
func (s *ChatStream) MatchToken(t string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token == t
}

// Close disconnects the subscriber and removes it from its stream's
// subscriber list (R3). Safe to call multiple times and safe to call on
// a subscriber already evicted by Append.
func (sub *subscriber) Close() {
	sub.closeOnce.Do(func() { close(sub.done) })
	stream := sub.stream
	stream.mu.Lock()
	kept := stream.subscribers[:0]
	for _, s := range stream.subscribers {
		if s != sub {
			kept = append(kept, s)
		}
	}
	for i := len(kept); i < len(stream.subscribers); i++ {
		stream.subscribers[i] = nil
	}
	stream.subscribers = kept
	stream.mu.Unlock()
}

// evictLocked closes the subscriber's done channel via closeOnce. Caller
// must hold sub.stream.mu and must NOT re-acquire it (the close is
// wait-free and idempotent).
func (sub *subscriber) evictLocked() {
	sub.closeOnce.Do(func() { close(sub.done) })
}

// Open returns the ChatStream for sessionID, creating and seeding it on
// first access. Concurrent Opens for the same session are coalesced via
// a single-flight marker (R3f): exactly one caller runs seedFn, the rest
// wait and receive the same already-seeded stream. On seed failure the
// half-created stream is removed from the registry so the next Open can
// retry from scratch (C4).
func (r *Registry) Open(sessionID string, seedFn func() ([]*game.Message, error)) (*ChatStream, error) {
	// R3f single-flight: wait for any in-flight open, then re-evaluate.
	r.mu.Lock()
	for {
		if ch, ok := r.inflight[sessionID]; ok {
			r.mu.Unlock()
			<-ch
			r.mu.Lock()
			continue
		}
		if stream, ok := r.streams[sessionID]; ok {
			// A stream in r.streams is by construction not yet closed:
			// Registry.Close deletes from the map before setting closed.
			// The stream.mu read below is defensive against future
			// invariants. Lock ordering r.mu -> stream.mu is consistent
			// across this package.
			stream.mu.Lock()
			closed := stream.closed
			stream.mu.Unlock()
			if !closed {
				r.mu.Unlock()
				return stream, nil
			}
		}
		break
	}
	ch := make(chan struct{})
	r.inflight[sessionID] = ch
	r.mu.Unlock()

	// Generate the session token outside the registry lock.
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		r.mu.Lock()
		delete(r.inflight, sessionID)
		r.mu.Unlock()
		close(ch)
		return nil, fmt.Errorf("chatstream: generate token: %w", err)
	}
	stream := &ChatStream{
		sessionID: sessionID,
		token:     hex.EncodeToString(b),
		logger:    r.logger,
	}

	r.mu.Lock()
	r.streams[sessionID] = stream
	r.mu.Unlock()

	msgs, err := seedFn()
	if err != nil {
		// C4: seed failure — remove the half-created stream so the next
		// Open can retry from scratch.
		r.mu.Lock()
		delete(r.streams, sessionID)
		delete(r.inflight, sessionID)
		r.mu.Unlock()
		close(ch)
		return nil, err
	}

	stream.SeedFromHistory(msgs)

	r.mu.Lock()
	delete(r.inflight, sessionID)
	r.mu.Unlock()
	close(ch)
	return stream, nil
}

// Append looks up the stream for sessionID and appends frame to it.
// Returns false if the session has no stream; the underlying
// ChatStream.Append logs and discards the frame if the stream itself is
// closed (F5).
func (r *Registry) Append(sessionID string, frame *game.AgentFrame) bool {
	r.mu.Lock()
	stream, ok := r.streams[sessionID]
	r.mu.Unlock()
	if !ok || stream == nil {
		r.logger.Error("backend", "warn: chatstream Append to absent/closed stream", map[string]any{"session_id": sessionID})
		return false
	}
	stream.Append(frame)
	return true
}

// Get returns the ChatStream for sessionID, or nil if none exists.
func (r *Registry) Get(sessionID string) *ChatStream {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.streams[sessionID]
}

// Close removes the stream for sessionID from the registry and shuts it
// down: the closed flag is set, every subscriber's done channel is
// closed via closeOnce, and the subscriber list is cleared (C5).
// Idempotent: a second Close for the same session is a no-op.
func (r *Registry) Close(sessionID string) {
	r.mu.Lock()
	stream, ok := r.streams[sessionID]
	if ok {
		delete(r.streams, sessionID)
	}
	r.mu.Unlock()
	if stream == nil {
		return
	}
	stream.mu.Lock()
	stream.closed = true
	for _, sub := range stream.subscribers {
		sub.closeOnce.Do(func() { close(sub.done) })
	}
	stream.subscribers = nil
	stream.mu.Unlock()
}
