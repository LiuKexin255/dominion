package main

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"dominion/projects/game/desktop/assets"
	"dominion/projects/game/desktop/internal/applog"
)

func main() {
	// Enable DPI awareness before creating any windows (Windows only).
	setProcessDPIAware()

	// Create logger
	logger := applog.NewLogger()

	// Create app
	app := NewApp(logger)

	// Run Wails
	err := wails.Run(&options.App{
		Title:  "Game Desktop Client",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets.FrontendDist,
		},
		OnStartup: func(ctx context.Context) {
			app.SetContext(ctx)
			// Wire up log event sink to Wails runtime
			logger.SetEventSink(func(entry applog.Entry) {
				runtime.EventsEmit(ctx, "game:log", entry)
			})
		},
		OnShutdown: func(ctx context.Context) {
			// CloseAgent waits on the continuous reader's recvDone after
			// tearing the socket down. The whole teardown is bounded by a 5s
			// safety timeout so a stuck component can't hang app exit.
			done := make(chan struct{})
			go func() {
				defer close(done)
				if err := app.CloseAgent(); err != nil {
					logger.Error("backend", "agent close failed",
						map[string]any{"error": err.Error()})
				}
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				logger.Error("backend", "agent shutdown timed out", nil)
			}
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
