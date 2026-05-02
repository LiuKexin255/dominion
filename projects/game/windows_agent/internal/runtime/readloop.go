package runtime

import (
	"fmt"
	"log"
)

// startReadLoopConsumer starts a goroutine that consumes ReadLoop messages.
func (r *Runtime) startReadLoopConsumer() {
	ctx := r.ctx
	ch, err := r.transport.ReadLoop(ctx)
	if err != nil {
		r.setError(fmt.Errorf("start read loop: %w", err))
		return
	}
	go func() {
		for msg := range ch {
			switch {
			case msg.ControlRequest != nil:
				if err := r.handleControlRequest(msg.ControlRequest); err != nil {
					log.Printf("runtime: handle control request: %v", err)
				}
			case msg.Ping != nil:
				session := r.currentSession()
				if session != nil {
					_ = r.transport.SendPong(ctx, session.ID, msg.Ping.Nonce)
				}
			case msg.Error != nil:
				r.setError(fmt.Errorf("gateway error: code=%s message=%s", msg.Error.GetCode(), msg.Error.GetMessage()))
				return
			}
		}
	}()
}
