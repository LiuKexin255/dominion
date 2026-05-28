// Package bind provides bidirectional stream binding logic for AgentFrame
// streams. It is used by the proxy service to forward frames between the
// gateway and agent, and by the gateway service to connect WebSocket and gRPC.
package bind

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	game "dominion/projects/game"
)

// AgentFrameStream is the bidirectional stream interface for exchanging
// AgentFrames between two endpoints (e.g. gateway and agent).
type AgentFrameStream interface {
	// Recv receives the next AgentFrame from the stream.
	// Returns io.EOF when the stream is closed by the peer.
	Recv() (*game.AgentFrame, error)
	// Send sends an AgentFrame on the stream.
	Send(*game.AgentFrame) error
}

// Binder binds two AgentFrameStream instances for bidirectional forwarding.
// Frames received on one stream are forwarded to the other until either stream
// closes or an error occurs.
type Binder interface {
	// Bind starts bidirectional forwarding between left and right streams.
	// Blocks until the first goroutine reports an error (or nil on clean close).
	// io.EOF and context.Canceled are treated as clean closes (nil).
	Bind(left AgentFrameStream, right AgentFrameStream) error
}

// bindState tracks the shared state of a Bind operation.
type bindState struct {
	done  atomic.Bool
	once  sync.Once
	errCh chan error
}

// binder implements Binder.
type binder struct{}

// NewBinder creates a new Binder instance.
func NewBinder() Binder {
	return new(binder)
}

// Bind starts four goroutines for bidirectional forwarding:
//   - Recv goroutine (left→channel): reads from left, writes to leftToRight
//   - Recv goroutine (right→channel): reads from right, writes to rightToLeft
//   - Send goroutine (channel→right): reads from leftToRight, sends to right
//   - Send goroutine (channel→left): reads from rightToLeft, sends to left
//
// Blocks until the first goroutine reports an error (or nil on clean close).
// io.EOF and context.Canceled are treated as clean closes.
func (b *binder) Bind(left AgentFrameStream, right AgentFrameStream) error {
	leftToRight := make(chan *game.AgentFrame, 1)
	rightToLeft := make(chan *game.AgentFrame, 1)

	s := &bindState{
		errCh: make(chan error, 1),
	}

	// left recv → leftToRight
	go func() {
		defer close(leftToRight)
		for {
			frame, err := left.Recv()
			if err != nil {
				b.report(s, err)
				return
			}
			if s.done.Load() {
				return
			}
			leftToRight <- frame
		}
	}()

	// right recv → rightToLeft
	go func() {
		defer close(rightToLeft)
		for {
			frame, err := right.Recv()
			if err != nil {
				b.report(s, err)
				return
			}
			if s.done.Load() {
				return
			}
			rightToLeft <- frame
		}
	}()

	// leftToRight → right send
	go func() {
		for frame := range leftToRight {
			if err := right.Send(frame); err != nil {
				b.report(s, err)
				return
			}
		}
	}()

	// rightToLeft → left send
	go func() {
		for frame := range rightToLeft {
			if err := left.Send(frame); err != nil {
				b.report(s, err)
				return
			}
		}
	}()

	return <-s.errCh
}

// report normalizes io.EOF and context.Canceled to nil, then sets done and
// writes the first result to errCh using sync.Once.
func (b *binder) report(s *bindState, err error) {
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		err = nil
	}
	s.once.Do(func() {
		s.done.Store(true)
		s.errCh <- err
	})
}
