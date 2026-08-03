package chatstream

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/desktop/internal/applog"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// recvTimeout is the upper bound for any channel assertion. A real success
// arrives within microseconds; this only fails the test on an actual hang.
const recvTimeout = 2 * time.Second

// newTestStream builds a zero-value ChatStream with a fixed token and a
// fresh logger. nextID is 0 (empty); Append assigns 1-based IDs by
// incrementing before assignment.
func newTestStream(sessionID string) *ChatStream {
	return &ChatStream{
		sessionID: sessionID,
		token:     "test-token",
		logger:    applog.NewLogger(),
	}
}

// testFrame constructs a minimal content TeamFrame carrying one TextPart
// so Append / SeedFromHistory have a concrete payload without forcing each
// test to repeat the proto oneof boilerplate.
func testFrame(id int64) *game.TeamFrame {
	return &game.TeamFrame{
		SessionId:  "test-session",
		TemplateId: "saolei",
		FrameId:    fmt.Sprintf("frame-%d", id),
		Role:       game.MessageRole_MESSAGE_ROLE_AGENT,
		Payload: &game.TeamFrame_MessageParts{
			MessageParts: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: fmt.Sprintf("msg-%d", id)}}},
				},
			},
		},
	}
}

// testMessages builds count history Messages used as seedFn output. Each
// message carries one TextPart so SeedFromHistory normalization is
// observable. CreateTime is left nil to keep the common case stdlib-only.
func testMessages(count int) []*game.Message {
	msgs := make([]*game.Message, count)
	for i := 0; i < count; i++ {
		msgs[i] = &game.Message{
			MessageId: fmt.Sprintf("msg-%d", i+1),
			Role:      game.MessageRole_MESSAGE_ROLE_USER,
			Content: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: fmt.Sprintf("history-%d", i+1)}}},
				},
			},
		}
	}
	return msgs
}

// recvOrFatal receives from ch or fails the test on timeout.
func recvOrFatal(t *testing.T, ch <-chan ChatEvent) ChatEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(recvTimeout):
		t.Fatal("timed out waiting for event on channel")
		return ChatEvent{}
	}
}

// assertClosed fails the test unless ch is closed within the timeout.
func assertClosed(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(recvTimeout):
		t.Errorf("%s: done channel not closed within timeout", label)
	}
}

// assertNotClosed fails the test if ch is closed within a short window.
func assertNotClosed(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
		t.Errorf("%s: done channel closed unexpectedly", label)
	case <-time.After(20 * time.Millisecond):
	}
}

// eventIDs extracts IDs for assertion error messages. Returns nil for an
// empty slice (per the repo's empty-slice convention).
func eventIDs(events []*ChatEvent) []int64 {
	if len(events) == 0 {
		return nil
	}
	ids := make([]int64, len(events))
	for i, ev := range events {
		ids[i] = ev.ID
	}
	return ids
}

// TestChatStream_Append verifies that Append assigns monotonic 1-based IDs
// (events[i].ID == i+1), that every event is reachable via the Subscribe
// snapshot, and that a live subscriber receives every appended event.
func TestChatStream_Append(t *testing.T) {
	tests := []struct {
		name string
		n    int
	}{
		{name: "single event", n: 1},
		{name: "three events", n: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: a fresh stream with one subscriber
			stream := newTestStream("append")
			sub, _ := stream.Subscribe(0)
			if sub == nil {
				t.Fatal("Subscribe returned nil subscriber")
			}

			// when: appending tt.n frames
			for i := 0; i < tt.n; i++ {
				stream.Append(testFrame(int64(i)))
			}

			// then: LastID == n and events[i].ID == i+1 (1-based, monotonic)
			if got := stream.LastID(); got != int64(tt.n) {
				t.Fatalf("LastID = %d, want %d", got, tt.n)
			}
			stream.mu.Lock()
			if len(stream.events) != tt.n {
				stream.mu.Unlock()
				t.Fatalf("events length = %d, want %d", len(stream.events), tt.n)
			}
			for i, ev := range stream.events {
				if ev.ID != int64(i+1) {
					stream.mu.Unlock()
					t.Errorf("events[%d].ID = %d, want %d", i, ev.ID, i+1)
				}
			}
			stream.mu.Unlock()

			// then: every event is reachable via the Subscribe(0) snapshot
			_, snap := stream.Subscribe(0)
			if len(snap) != tt.n {
				t.Fatalf("snapshot length = %d, want %d (ids=%v)", len(snap), tt.n, eventIDs(snap))
			}
			for i, ev := range snap {
				if ev.ID != int64(i+1) {
					t.Errorf("snap[%d].ID = %d, want %d", i, ev.ID, i+1)
				}
			}

			// then: the first subscriber received every appended event in order
			for i := 0; i < tt.n; i++ {
				ev := recvOrFatal(t, sub.events)
				if ev.ID != int64(i+1) {
					t.Errorf("live event[%d].ID = %d, want %d", i, ev.ID, i+1)
				}
			}
		})
	}
}

// TestChatStream_Backpressure verifies F3: a subscriber whose channel is
// full (no drainer) is evicted in place, while a healthy subscriber still
// observes every event across its snapshot + live channel.
func TestChatStream_Backpressure(t *testing.T) {
	// given: subscriber A joins first and is never drained (will stall)
	stream := newTestStream("backpressure")
	subA, _ := stream.Subscribe(0)
	if subA == nil {
		t.Fatal("subA nil")
	}

	// when: appending exactly subscriberBuffer events fills A's channel
	// (cap = subscriberBuffer, none drained)
	for i := 0; i < subscriberBuffer; i++ {
		stream.Append(testFrame(int64(i)))
	}

	// given: subscriber B joins late — its snapshot captures the full
	// backlog and its channel is empty and ready for live events
	subB, snapB := stream.Subscribe(0)
	if subB == nil {
		t.Fatal("subB nil")
	}
	if len(snapB) != subscriberBuffer {
		t.Fatalf("subB snapshot length = %d, want %d", len(snapB), subscriberBuffer)
	}

	// when: appending enough overflow events to reach 70 total. The first
	// overflow evicts A; B keeps receiving on its drained channel.
	const total = 70
	overflow := total - subscriberBuffer
	for i := 0; i < overflow; i++ {
		stream.Append(testFrame(int64(subscriberBuffer + i)))
	}

	// then: A is evicted (done closed) — F3 backpressure
	assertClosed(t, subA.done, "subA")

	// then: B observed all 70 events — `subscriberBuffer` via snapshot and
	// the overflow tail via its live channel, no gaps.
	bSeen := make(map[int64]bool, total)
	for _, ev := range snapB {
		bSeen[ev.ID] = true
	}
	for i := 0; i < overflow; i++ {
		ev := recvOrFatal(t, subB.events)
		bSeen[ev.ID] = true
	}
	if len(bSeen) != total {
		t.Errorf("subB observed %d distinct IDs, want %d", len(bSeen), total)
	}
	for id := int64(1); id <= total; id++ {
		if !bSeen[id] {
			t.Errorf("subB missing event ID %d", id)
		}
	}
}

// TestChatStream_Close verifies F5 close-safety in three shapes: Append to
// a closed stream is a logged no-op; Registry.Append for an absent session
// returns false; Registry.Close is idempotent.
func TestChatStream_Close(t *testing.T) {
	t.Run("Append to closed stream is no-op and warns", func(t *testing.T) {
		// given: a registry-managed stream that has been closed
		logger := applog.NewLogger()
		reg := NewRegistry(logger)
		stream, err := reg.Open("closed", func() ([]*game.Message, error) {
			return nil, nil
		})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		before := stream.LastID()
		reg.Close("closed")

		// when: appending directly to the now-closed stream
		// then: no panic, LastID unchanged
		stream.Append(testFrame(1))
		if got := stream.LastID(); got != before {
			t.Errorf("LastID after closed Append = %d, want %d", got, before)
		}

		// then: the warn was logged at error level
		found := false
		for _, e := range logger.Entries() {
			if e.Source == "backend" && e.Level == "error" &&
				contains(e.Message, "Append to closed stream") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a warn log for closed-stream Append; entries=%v", logger.Entries())
		}
	})

	t.Run("Registry.Append absent session returns false", func(t *testing.T) {
		// given: an empty registry
		reg := NewRegistry(applog.NewLogger())

		// when/then: appending to a never-opened session returns false, no panic
		if got := reg.Append("nope", testFrame(1)); got {
			t.Errorf("Append to absent session = true, want false")
		}
	})

	t.Run("Registry.Close is idempotent", func(t *testing.T) {
		// given: an open session
		reg := NewRegistry(applog.NewLogger())
		if _, err := reg.Open("once", func() ([]*game.Message, error) {
			return nil, nil
		}); err != nil {
			t.Fatalf("Open: %v", err)
		}

		// when: closing twice — neither call panics
		reg.Close("once")
		reg.Close("once")

		// then: registry no longer holds the session
		if got := reg.Get("once"); got != nil {
			t.Errorf("Get after double Close = %v, want nil", got)
		}
	})
}

// contains reports whether substr appears within s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

// indexOf returns the byte index of substr in s, or -1.
func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// TestChatStream_SubscribeAtomcity verifies F4: under concurrent Append +
// Subscribe, the snapshot captures events strictly before the live channel
// — no event is in both, none is dropped, and order is preserved. Must be
// clean under `go test -race`.
func TestChatStream_SubscribeAtomcity(t *testing.T) {
	// given: a stream pre-seeded with preN events so the snapshot always
	// captures a contiguous prefix, then postN concurrent appends race
	// against Subscribe. postN is under subscriberBuffer (64) so the
	// channel cannot overflow regardless of drain timing — the test
	// exercises the F4 atomicity invariant, not backpressure.
	stream := &ChatStream{sessionID: "atomic", token: "t", logger: applog.NewLogger()}
	const preN = 100
	const postN = 50
	const total = preN + postN

	for i := 0; i < preN; i++ {
		stream.Append(testFrame(int64(i)))
	}

	// when: postN appends and a Subscribe run concurrently behind a
	// shared start barrier so they genuinely race
	var appenderDone sync.WaitGroup
	start := make(chan struct{})
	appenderDone.Add(1)
	go func() {
		defer appenderDone.Done()
		<-start
		for i := 0; i < postN; i++ {
			stream.Append(testFrame(int64(preN + i)))
		}
	}()

	type subResult struct {
		sub  *subscriber
		snap []*ChatEvent
	}
	resCh := make(chan subResult, 1)
	go func() {
		<-start
		sub, snap := stream.Subscribe(0)
		resCh <- subResult{sub, snap}
	}()

	close(start)
	res := <-resCh
	if res.sub == nil {
		t.Fatal("Subscribe returned nil under concurrency")
	}
	sub, snap := res.sub, res.snap

	// wait for appends to finish, then drain whatever the channel
	// buffered (postN ≤ subscriberBuffer so no eviction is possible)
	appenderDone.Wait()
	snapSet := make(map[int64]bool, len(snap))
	for _, ev := range snap {
		snapSet[ev.ID] = true
	}
	liveSet := make(map[int64]bool, total)
	for {
		select {
		case ev := <-sub.events:
			liveSet[ev.ID] = true
		default:
			goto verify
		}
	}

verify:
	// then: LastID reflects all appends
	if got := stream.LastID(); got != total {
		t.Errorf("LastID = %d, want %d", got, total)
	}

	// then: snapshot and live are disjoint (snapshot captures events
	// strictly before live — the F4 atomicity guarantee)
	for id := range snapSet {
		if liveSet[id] {
			t.Errorf("event ID %d appears in BOTH snapshot and live channel", id)
		}
	}

	// then: union covers 1..total with no gaps
	for id := int64(1); id <= total; id++ {
		if !snapSet[id] && !liveSet[id] {
			t.Errorf("event ID %d missing from snapshot and live", id)
		}
	}

	// then: snapshot IDs are strictly increasing (order preserved)
	var prevSnapID int64
	for _, ev := range snap {
		if ev.ID <= prevSnapID && prevSnapID != 0 {
			t.Errorf("snapshot order broken at ID %d (prev %d)", ev.ID, prevSnapID)
		}
		prevSnapID = ev.ID
	}
}

// TestChatStream_SubscribeAuthorized verifies C2 atomic token+subscribe:
// valid token returns OK with a non-nil subscriber and snapshot; a stale
// token is rejected atomically; a closed stream returns SubscribeClosed.
func TestChatStream_SubscribeAuthorized(t *testing.T) {
	tests := []struct {
		name        string
		stale       bool // true → use the pre-rotation (stale) token
		closeStream bool // true → close the stream before subscribing
		wantStatus  SubscribeStatus
		wantNilSub  bool
		wantSnapLen int // valid case observes the 2 seeded events
	}{
		{name: "valid token", stale: false, closeStream: false, wantStatus: SubscribeOK, wantNilSub: false, wantSnapLen: 2},
		{name: "stale token", stale: true, closeStream: false, wantStatus: SubscribeStaleToken, wantNilSub: true, wantSnapLen: 0},
		{name: "closed stream", stale: false, closeStream: true, wantStatus: SubscribeClosed, wantNilSub: true, wantSnapLen: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: a stream with two seeded events and a freshly rotated token
			stream := newTestStream("authz")
			stream.Append(testFrame(1))
			stream.Append(testFrame(2))
			old := stream.Token()
			stream.RotateToken()
			current := stream.Token()

			token := current
			if tt.stale {
				token = old
			}
			if tt.closeStream {
				stream.mu.Lock()
				stream.closed = true
				stream.mu.Unlock()
			}

			// when: subscribing with the prepared token
			sub, snap, status := stream.SubscribeAuthorized(token, 0)

			// then: status, subscriber nil-ness, and snapshot size match
			if status != tt.wantStatus {
				t.Errorf("status = %v, want %v", status, tt.wantStatus)
			}
			if (sub == nil) != tt.wantNilSub {
				t.Errorf("subscriber nil = %v, want %v", sub == nil, tt.wantNilSub)
			}
			if len(snap) != tt.wantSnapLen {
				t.Errorf("snapshot length = %d, want %d", len(snap), tt.wantSnapLen)
			}
		})
	}
}

// TestChatStream_SubscriberClose verifies R3: subscriber.Close() removes
// the subscriber from the stream's list so subsequent appends do not fan
// out to it, and concurrent Close + Append-driven eviction never panic
// (sync.Once guards the done-channel close). Must be clean under -race.
func TestChatStream_SubscriberClose(t *testing.T) {
	t.Run("Close removes subscriber from fan-out list", func(t *testing.T) {
		// given: a stream with two subscribers
		stream := newTestStream("subclose")
		subA, _ := stream.Subscribe(0)
		subB, _ := stream.Subscribe(0)
		if subA == nil || subB == nil {
			t.Fatal("Subscribe returned nil")
		}

		// when: A closes itself
		subA.Close()

		// then: A's done is closed, A is no longer in the list
		assertClosed(t, subA.done, "subA")
		stream.mu.Lock()
		for _, s := range stream.subscribers {
			if s == subA {
				stream.mu.Unlock()
				t.Errorf("subA still present in subscribers list after Close")
			}
		}
		stream.mu.Unlock()

		// then: a subsequent append fans out only to B
		stream.Append(testFrame(1))
		recvOrFatal(t, subB.events)
		select {
		case ev, ok := <-subA.events:
			if ok {
				t.Errorf("subA received event %d after Close", ev.ID)
			}
		case <-time.After(20 * time.Millisecond):
			// expected: A receives nothing
		}
	})

	t.Run("concurrent Close and eviction do not panic", func(t *testing.T) {
		// given: a stream with one subscriber
		stream := newTestStream("subclose-race")
		sub, _ := stream.Subscribe(0)
		if sub == nil {
			t.Fatal("Subscribe returned nil")
		}

		// when: handler Close races against Append-driven overflow eviction
		// (both paths call closeOnce.Do(close(done)))
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			sub.Close()
		}()
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < subscriberBuffer+10; i++ {
				stream.Append(testFrame(int64(i)))
			}
		}()
		close(start)
		wg.Wait()

		// then: no panic occurred (reaching here is the assertion) and the
		// done channel is closed exactly once
		assertClosed(t, sub.done, "sub")
	})
}

// TestChatStream_RotateToken verifies R3d: RotateToken generates a new
// non-empty token, disconnects every existing subscriber, and invalidates
// the old token for MatchToken.
func TestChatStream_RotateToken(t *testing.T) {
	t.Run("generates new non-empty token", func(t *testing.T) {
		tests := []struct {
			name     string
			starting string
		}{
			{name: "from fixed seed token", starting: "test-token"},
			{name: "from empty token", starting: ""},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// given: a stream with a known starting token
				stream := &ChatStream{
					sessionID: "rotate-change",
					token:     tt.starting,
					logger:    applog.NewLogger(),
				}

				// when: rotating
				next := stream.RotateToken()

				// then: new token differs, is non-empty, and is current
				if next == tt.starting {
					t.Errorf("token unchanged: %q", next)
				}
				if next == "" {
					t.Errorf("RotateToken returned empty token")
				}
				if got := stream.Token(); got != next {
					t.Errorf("Token() = %q, want %q", got, next)
				}
			})
		}
	})

	t.Run("closes all existing subscriber done channels", func(t *testing.T) {
		// given: a stream with two subscribers
		stream := newTestStream("rotate-subs")
		subA, _ := stream.Subscribe(0)
		subB, _ := stream.Subscribe(0)
		if subA == nil || subB == nil {
			t.Fatal("Subscribe returned nil")
		}

		// when: rotating the token
		stream.RotateToken()

		// then: both subscribers' done channels are closed
		assertClosed(t, subA.done, "subA")
		assertClosed(t, subB.done, "subB")
	})

	t.Run("invalidates old token for MatchToken", func(t *testing.T) {
		// given: a stream with a known token
		stream := newTestStream("rotate-match")
		old := stream.Token()

		// when: rotating
		next := stream.RotateToken()

		// then: the old token no longer matches, the new one does
		if stream.MatchToken(old) {
			t.Errorf("MatchToken(old) = true, want false after rotation")
		}
		if !stream.MatchToken(next) {
			t.Errorf("MatchToken(new) = false, want true")
		}
	})
}

// TestRegistry_Open verifies R3f single-flight Open (two concurrent opens
// for the same session run seedFn exactly once and yield the same stream)
// and C4 seed-failure cleanup (a failing seedFn removes the stream so a
// retry starts fresh).
func TestRegistry_Open(t *testing.T) {
	t.Run("single-flight: one seedFn, same stream", func(t *testing.T) {
		// given: a registry and a seedFn that blocks until released so both
		// Open callers genuinely race for the single-flight slot
		reg := NewRegistry(applog.NewLogger())
		var seedCalls int32
		releaseCh := make(chan struct{})
		seedFn := func() ([]*game.Message, error) {
			atomic.AddInt32(&seedCalls, 1)
			<-releaseCh
			return testMessages(1), nil
		}

		// when: two concurrent Opens for the same session
		const callers = 2
		type openResult struct {
			stream *ChatStream
			err    error
		}
		resCh := make(chan openResult, callers)
		var wg sync.WaitGroup
		wg.Add(callers)
		for i := 0; i < callers; i++ {
			go func() {
				defer wg.Done()
				s, err := reg.Open("inflight", seedFn)
				resCh <- openResult{s, err}
			}()
		}
		// give both goroutines a chance to enter Open before releasing
		time.Sleep(20 * time.Millisecond)
		close(releaseCh)
		wg.Wait()
		close(resCh)

		// then: exactly one seedFn call ran
		if calls := atomic.LoadInt32(&seedCalls); calls != 1 {
			t.Errorf("seedFn ran %d times, want 1", calls)
		}

		// then: both callers got the same stream, no errors
		var first *ChatStream
		for res := range resCh {
			if res.err != nil {
				t.Errorf("Open returned error: %v", res.err)
				continue
			}
			if first == nil {
				first = res.stream
			} else if res.stream != first {
				t.Errorf("single-flight mismatch: %p vs %p", res.stream, first)
			}
		}
		if first == nil {
			t.Fatal("no successful Open result")
		}
		// then: the shared stream was seeded exactly once
		if got := first.LastID(); got != 1 {
			t.Errorf("seeded LastID = %d, want 1", got)
		}
	})

	t.Run("seed error removes stream (C4)", func(t *testing.T) {
		// given: a registry and a failing seedFn
		reg := NewRegistry(applog.NewLogger())
		seedErr := fmt.Errorf("boom")
		seedFn := func() ([]*game.Message, error) {
			return nil, seedErr
		}

		// when: opening with the failing seedFn
		_, err := reg.Open("fails", seedFn)

		// then: error propagated and the session is absent from the registry
		if err == nil {
			t.Fatal("expected error from failing seedFn, got nil")
		}
		if got := reg.Get("fails"); got != nil {
			t.Errorf("Get after seed failure = %v, want nil", got)
		}

		// then: a subsequent Open with a working seedFn succeeds from scratch
		retry, err := reg.Open("fails", func() ([]*game.Message, error) {
			return testMessages(1), nil
		})
		if err != nil {
			t.Fatalf("retry Open: %v", err)
		}
		if retry.LastID() != 1 {
			t.Errorf("retry LastID = %d, want 1", retry.LastID())
		}
	})
}

// TestRegistry_Close verifies C5: Close closes every subscriber's done
// channel, marks the stream closed (Subscribe then returns nil, Append is
// a no-op), and removes the stream from the registry map.
func TestRegistry_Close(t *testing.T) {
	t.Run("disconnects subscribers and removes from registry", func(t *testing.T) {
		// given: an open session with two subscribers
		reg := NewRegistry(applog.NewLogger())
		stream, err := reg.Open("cleanup", func() ([]*game.Message, error) {
			return nil, nil
		})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		subA, _ := stream.Subscribe(0)
		subB, _ := stream.Subscribe(0)
		if subA == nil || subB == nil {
			t.Fatal("Subscribe returned nil")
		}

		// when: closing the session
		reg.Close("cleanup")

		// then: both subscribers are disconnected and the stream is gone
		assertClosed(t, subA.done, "subA")
		assertClosed(t, subB.done, "subB")
		if got := reg.Get("cleanup"); got != nil {
			t.Errorf("Get after Close = %v, want nil", got)
		}
	})

	t.Run("marks stream closed so Subscribe and Append are no-ops", func(t *testing.T) {
		// given: an open session with a seeded event
		reg := NewRegistry(applog.NewLogger())
		stream, err := reg.Open("closedflag", func() ([]*game.Message, error) {
			return testMessages(1), nil
		})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		before := stream.LastID()

		// when: closing
		reg.Close("closedflag")

		// then: the closed flag is set
		stream.mu.Lock()
		closed := stream.closed
		stream.mu.Unlock()
		if !closed {
			t.Fatalf("stream.closed = false, want true after Registry.Close")
		}

		// then: Subscribe returns nil (no new subscribers admitted)
		if sub, _ := stream.Subscribe(0); sub != nil {
			t.Errorf("Subscribe on closed stream = %v, want nil", sub)
		}

		// then: Append is a no-op (LastID unchanged)
		stream.Append(testFrame(99))
		if got := stream.LastID(); got != before {
			t.Errorf("LastID after closed Append = %d, want %d", got, before)
		}
	})
}

// TestSeedFromHistory verifies that SeedFromHistory normalizes persisted
// Messages into content TeamFrames: one event per message, preserving
// messageId (→ FrameId), role, createTime, and content, with the
// sessionID rewritten to the stream's session.
func TestSeedFromHistory(t *testing.T) {
	// given: two history messages with a concrete CreateTime so the
	// timestamp normalization is observable
	stream := newTestStream("seed")
	msgs := []*game.Message{
		{
			MessageId:  "msg-1",
			Role:       game.MessageRole_MESSAGE_ROLE_USER,
			CreateTime: &timestamppb.Timestamp{Seconds: 1000, Nanos: 1},
			Content: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: "history-1"}}},
				},
			},
		},
		{
			MessageId:  "msg-2",
			Role:       game.MessageRole_MESSAGE_ROLE_AGENT,
			CreateTime: &timestamppb.Timestamp{Seconds: 2000, Nanos: 2},
			Content: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: "history-2"}}},
				},
			},
		},
	}

	// when: seeding from history
	stream.SeedFromHistory(msgs)

	// then: two events with monotonic IDs 1,2
	if got := stream.LastID(); got != 2 {
		t.Fatalf("LastID = %d, want 2", got)
	}
	_, snap := stream.Subscribe(0)
	if len(snap) != 2 {
		t.Fatalf("snapshot length = %d, want 2", len(snap))
	}

	for i, ev := range snap {
		wantID := int64(i + 1)
		if ev.ID != wantID {
			t.Errorf("snap[%d].ID = %d, want %d", i, ev.ID, wantID)
		}
		frame := ev.Frame
		if frame.GetFrameId() != msgs[i].GetMessageId() {
			t.Errorf("snap[%d].FrameId = %q, want %q", i, frame.GetFrameId(), msgs[i].GetMessageId())
		}
		if frame.GetRole() != msgs[i].GetRole() {
			t.Errorf("snap[%d].Role = %v, want %v", i, frame.GetRole(), msgs[i].GetRole())
		}
		if frame.GetCreateTime().String() != msgs[i].GetCreateTime().String() {
			t.Errorf("snap[%d].CreateTime = %v, want %v", i, frame.GetCreateTime(), msgs[i].GetCreateTime())
		}
		// sessionID is rewritten to the stream's session
		if frame.GetSessionId() != stream.sessionID {
			t.Errorf("snap[%d].SessionId = %q, want %q", i, frame.GetSessionId(), stream.sessionID)
		}
		// payload is the messageParts content
		if frame.GetMessageParts().String() != msgs[i].GetContent().String() {
			t.Errorf("snap[%d].Content mismatch", i)
		}
	}
}

// TestSeedFromHistory_Empty verifies SeedFromHistory is a no-op on an
// empty message slice (nil-safe per the repo's nil-slice convention).
func TestSeedFromHistory_Empty(t *testing.T) {
	// given: a fresh stream
	stream := newTestStream("seed-empty")

	// when: seeding with nil
	stream.SeedFromHistory(nil)

	// then: no events were appended
	if got := stream.LastID(); got != 0 {
		t.Errorf("LastID after empty seed = %d, want 0", got)
	}
	_, snap := stream.Subscribe(0)
	if snap != nil {
		t.Errorf("snapshot after empty seed = %v, want nil", snap)
	}
}
