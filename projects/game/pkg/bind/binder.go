// Package bind provides bidirectional stream binding logic for the
// direction-split frame streams. It is used by the proxy service to forward
// frames between the gateway and agent, and by the gateway service to connect
// WebSocket and gRPC.
package bind

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	game "dominion/projects/game"
)

// UserFrameStream is the client-facing stream interface (Bind's left side:
// the gateway WebSocket adapter or the proxy's TeamService_ConnectServer).
// Its inbound direction is UserFrame (Recv from the client) and its outbound
// direction is TeamFrame (Send to the client)
// (specs/035-proto-contract-refine/contracts/frame-split.md §6.1).
type UserFrameStream interface {
	// Recv receives the next UserFrame from the client.
	// Returns io.EOF when the stream is closed by the peer.
	Recv() (*game.UserFrame, error)
	// Send sends a TeamFrame to the client.
	Send(*game.TeamFrame) error
}

// TeamFrameStream is the server-facing stream interface (Bind's right side:
// the agent's TeamService_ConnectClient). Its outbound direction is UserFrame
// (Send to the server) and its inbound direction is TeamFrame (Recv from the
// server). The split prevents mixing directions: a client-shaped stream cannot
// be passed as left and vice versa
// (specs/035-proto-contract-refine/contracts/frame-split.md §6.1).
type TeamFrameStream interface {
	// Send sends a UserFrame to the server.
	Send(*game.UserFrame) error
	// Recv receives the next TeamFrame from the server.
	// Returns io.EOF when the stream is closed by the peer.
	Recv() (*game.TeamFrame, error)
}

// Binder binds a UserFrameStream and a TeamFrameStream for bidirectional
// forwarding. Frames received on one stream are forwarded to the other until
// either stream closes or an error occurs.
type Binder interface {
	// Bind starts bidirectional forwarding between left and right streams:
	// left.Recv() (UserFrame) → right.Send() (UserFrame);
	// right.Recv() (TeamFrame) → left.Send() (TeamFrame).
	// Blocks until the first goroutine reports an error (or nil on clean close).
	// io.EOF and context.Canceled are treated as clean closes (nil).
	Bind(left UserFrameStream, right TeamFrameStream) error
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
//   - Recv goroutine (left→channel): reads UserFrame from left, writes to leftToRight
//   - Recv goroutine (right→channel): reads TeamFrame from right, writes to rightToLeft
//   - Send goroutine (channel→right): reads from leftToRight, sends UserFrame to right
//   - Send goroutine (channel→left): reads from rightToLeft, sends TeamFrame to left
//
// Blocks until the first goroutine reports an error (or nil on clean close).
// io.EOF and context.Canceled are treated as clean closes.
func (b *binder) Bind(left UserFrameStream, right TeamFrameStream) error {
	leftToRight := make(chan *game.UserFrame, 1)
	rightToLeft := make(chan *game.TeamFrame, 1)

	s := &bindState{
		errCh: make(chan error, 1),
	}

	// left recv (UserFrame) → leftToRight
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

	// right recv (TeamFrame) → rightToLeft
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

	// leftToRight → right send (UserFrame)
	go func() {
		for frame := range leftToRight {
			if err := right.Send(frame); err != nil {
				b.report(s, err)
				return
			}
		}
	}()

	// rightToLeft → left send (TeamFrame)
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
