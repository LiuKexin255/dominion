// Package stream provides bidirectional streaming logic for the proxy service.
package stream

import (
	"context"
	"errors"
	"io"

	game "dominion/projects/game"

	"golang.org/x/sync/errgroup"
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
// closes or the context is cancelled.
type Binder interface {
	// Bind starts bidirectional forwarding between left and right streams.
	// It blocks until both forwarding goroutines complete.
	// Returns nil on clean close (io.EOF, context.Canceled).
	Bind(ctx context.Context, left AgentFrameStream, right AgentFrameStream) error
}

// binder implements Binder using errgroup for goroutine coordination.
type binder struct{}

// NewBinder creates a new Binder instance.
func NewBinder() Binder {
	return new(binder)
}

// Bind starts two goroutines: left→right and right→left forwarding.
// When any goroutine encounters an error, the errgroup cancels the shared context,
// causing the other goroutine to see context.Canceled and exit.
// io.EOF and context.Canceled are treated as clean closes.
func (b *binder) Bind(ctx context.Context, left AgentFrameStream, right AgentFrameStream) error {
	g, gCtx := errgroup.WithContext(ctx)

	// left → right
	g.Go(func() error {
		return forward(gCtx, left, right)
	})

	// right → left
	g.Go(func() error {
		return forward(gCtx, right, left)
	})

	if err := g.Wait(); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}

// forward reads frames from src and sends them to dst until an error occurs.
// io.EOF and context.Canceled are returned as-is so Bind can handle them.
func forward(_ context.Context, src AgentFrameStream, dst AgentFrameStream) error {
	for {
		frame, err := src.Recv()
		if err != nil {
			return err
		}
		if err := dst.Send(frame); err != nil {
			return err
		}
	}
}
