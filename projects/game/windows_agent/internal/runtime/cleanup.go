package runtime

import "errors"

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
	r.connState = ConnDisconnected
	r.streamState = StreamIdle
	r.boundWindow = nil
	r.mediaDone = nil
	r.session = nil
	r.mu.Unlock()
	return err
}
