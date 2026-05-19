package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync/atomic"
	"time"

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

	hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", session.ID, time.Now().UnixNano())))
	r.streamID = hex.EncodeToString(hash[:])
	r.sequence = 0

	done := make(chan error, 1)
	r.mu.Lock()
	r.mediaDone = done
	ctx := r.ctx
	r.mu.Unlock()

	go func(reader io.Reader, sessionID string) {
		err := r.parseMedia(reader,
			func(initSeg *media.InitSegment) error {
				r.initID = initSeg.InitID
				log.Printf("media-flow", "sending init: session=%s stream=%s init=%s size=%d", sessionID, r.streamID, r.initID, len(initSeg.Data))
				if err := r.transport.SendMediaInit(ctx, sessionID, r.streamID, r.initID, transport.MimeTypeMP4, transport.CodecH264AVC, initSeg.Data); err != nil {
					log.Errorf("media-flow", "send init failed: session=%s error=%v", sessionID, err)
					return err
				}
				return nil
			},
			func(seg *media.MediaSegment) error {
				r.sequence++
				ra := seg.RandomAccess
				if err := r.transport.SendMediaSegment(ctx, sessionID, r.streamID, r.initID, r.sequence, seg.Data, &ra, seg.DurationMS, seg.Discontinuity); err != nil {
					log.Errorf("media-flow", "send segment failed: session=%s seq=%d error=%v", sessionID, r.sequence, err)
					return err
				}
				atomic.AddInt64(&r.segCount, 1)
				return nil
			},
		)
		if err != nil {
			log.Errorf("media-flow", "media flow ended with error: session=%s segments=%d error=%v", sessionID, r.segCount, err)
		} else {
			log.Printf("media-flow", "media flow ended: session=%s segments=%d", sessionID, r.segCount)
		}
		done <- err
	}(stdout, session.ID)
	return nil
}
