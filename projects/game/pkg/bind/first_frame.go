package bind

import "dominion/projects/game"

// firstFrameStream wraps a UserFrameStream to replay a pre-read first frame.
type firstFrameStream struct {
	inner     UserFrameStream
	first     *game.UserFrame
	firstSent bool
}

// WithFirstFrame returns a UserFrameStream that returns the given first frame
// on the first call to Recv(), then delegates all subsequent calls to inner.
// Send() is always delegated to inner.
// Panics if first is nil (defensive programming).
func WithFirstFrame(inner UserFrameStream, first *game.UserFrame) UserFrameStream {
	if first == nil {
		panic("bind: first frame must not be nil")
	}
	return &firstFrameStream{inner: inner, first: first}
}

func (s *firstFrameStream) Recv() (*game.UserFrame, error) {
	if !s.firstSent {
		s.firstSent = true
		return s.first, nil
	}
	return s.inner.Recv()
}

func (s *firstFrameStream) Send(frame *game.TeamFrame) error {
	return s.inner.Send(frame)
}
