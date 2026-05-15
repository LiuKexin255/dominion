package runtime

import (
	"fmt"
	"io"
	"sync/atomic"

	"dominion/projects/game/windows_agent/internal/log"
	"dominion/projects/game/windows_agent/internal/media"
	"dominion/projects/game/windows_agent/internal/transport"
)

// startMediaFlow reads encoder stdout, parses fMP4, and forwards media to transport.
func (r *Runtime) startMediaFlow() error {
	if r.encoder == nil {
		return fmt.Errorf("encoder is not configured")
	}
	stdout := r.encoder.StdoutPipe()
	if stdout == nil {
		return fmt.Errorf("encoder stdout is not available")
	}
	session := r.currentSession()
	if session == nil {
		return fmt.Errorf("session is not initialized")
	}
	if r.parseMedia == nil {
		return fmt.Errorf("media parser is not configured")
	}

	done := make(chan error, 1)
	r.mu.Lock()
	r.mediaDone = done
	ctx := r.ctx
	r.mu.Unlock()

	go func(reader io.Reader, sessionID string) {
		err := r.parseMedia(reader,
			func(initData []byte) error {
				log.Printf("media-flow", "sending init segment: session=%s size=%d", sessionID, len(initData))
				if err := r.transport.SendMediaInit(ctx, sessionID, transport.MimeTypeMP4, initData); err != nil {
					log.Errorf("media-flow", "send init failed: session=%s error=%v", sessionID, err)
					return err
				}
				return nil
			},
			func(seg *media.MediaSegment) error {
				segmentID := fmt.Sprintf("seg-%d", seg.SeqNum)
				log.Printf("media-flow", "sending segment: session=%s seg=%s size=%d keyframe=%v", sessionID, segmentID, len(seg.Data), seg.KeyFrame)
				if err := r.transport.SendMediaSegment(ctx, sessionID, segmentID, seg.Data, seg.KeyFrame); err != nil {
					log.Errorf("media-flow", "send segment failed: session=%s seg=%s error=%v", sessionID, segmentID, err)
					return err
				}
				atomic.AddInt64(&r.segCount, 1)
				return nil
			},
		)
		if err != nil {
			log.Errorf("media-flow", "media flow ended with error: session=%s error=%v", sessionID, err)
		} else {
			log.Printf("media-flow", "media flow ended: session=%s segments=%d", sessionID, r.segCount)
		}
		done <- err
	}(stdout, session.ID)
	return nil
}
