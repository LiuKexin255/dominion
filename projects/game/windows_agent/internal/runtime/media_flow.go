package runtime

import (
	"fmt"
	"io"
	"sync/atomic"
)

const mediaMimeType = "video/mp4; codecs=\"avc1.42E01E\""

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
		result, err := r.parseMedia(reader)
		if err != nil {
			done <- err
			return
		}
		if result.InitSegment != nil {
			if err := r.transport.SendMediaInit(ctx, sessionID, mediaMimeType, result.InitSegment.Data); err != nil {
				done <- err
				return
			}
		}
		for _, segment := range result.MediaSegs {
			segmentID := fmt.Sprintf("seg-%d", segment.SeqNum)
			if err := r.transport.SendMediaSegment(ctx, sessionID, segmentID, segment.Data, segment.KeyFrame); err != nil {
				done <- err
				return
			}
			atomic.AddInt64(&r.segCount, 1)
		}
		done <- nil
	}(stdout, session.ID)
	return nil
}
