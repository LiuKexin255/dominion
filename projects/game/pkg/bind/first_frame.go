package bind

import game "dominion/projects/game"

// firstFrameStream wraps an AgentFrameStream to replay a pre-read first frame.
type firstFrameStream struct {
	inner     AgentFrameStream
	first     *game.AgentFrame
	firstSent bool
}

// WithFirstFrame returns an AgentFrameStream that returns the given first frame
// on the first call to Recv(), then delegates all subsequent calls to inner.
// Send() is always delegated to inner.
// Panics if first is nil (defensive programming).
func WithFirstFrame(inner AgentFrameStream, first *game.AgentFrame) AgentFrameStream {
	if first == nil {
		panic("bind: first frame must not be nil")
	}
	return &firstFrameStream{inner: inner, first: first}
}

func (s *firstFrameStream) Recv() (*game.AgentFrame, error) {
	if !s.firstSent {
		s.firstSent = true
		return s.first, nil
	}
	return s.inner.Recv()
}

func (s *firstFrameStream) Send(frame *game.AgentFrame) error {
	return s.inner.Send(frame)
}
