package runtime

import "errors"

// cleanup stops all subsystems in order: ffmpeg, input-helper, media, transport.
func (r *Runtime) cleanup() error {
	var err error
	if r.cancel != nil {
		r.cancel()
	}
	if r.encoder != nil {
		err = errors.Join(err, r.encoder.Stop())
	}
	if r.inputMgr != nil {
		err = errors.Join(err, r.inputMgr.ReleaseAll())
		err = errors.Join(err, r.inputMgr.Stop())
	}
	if r.transport != nil {
		err = errors.Join(err, r.transport.Close())
	}
	r.mu.Lock()
	r.state = StateDisconnected
	r.boundWindow = nil
	r.mediaDone = nil
	r.mu.Unlock()
	return err
}
