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
	"dominion/projects/game/desktop/internal/chatstream"
)

func main() {
	// Enable DPI awareness before creating any windows (Windows only).
	setProcessDPIAware()

	// Create logger
	logger := applog.NewLogger()

	// Create app
	app := NewApp(logger)

	// One chatstream Registry + Server per process. Built before wails.Run
	// so OnStartup can start listening synchronously and Endpoint() returns
	// the real URL before the frontend boots (F10).
	chatReg := chatstream.NewRegistry(logger)
	chatSrv := chatstream.NewServer(chatReg, logger)

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

			// Bind address synchronously: Start calls net.Listen before
			// returning, so Endpoint() is ready for the frontend
			// immediately after OnStartup (F10). Serve runs in a goroutine.
			if err := chatSrv.Start(ctx); err != nil {
				logger.Error("backend", "chatstream start failed",
					map[string]any{"error": err.Error()})
			}
			app.SetChatStream(chatReg, chatSrv)
		},
		OnShutdown: func(ctx context.Context) {
			// Close the agent socket first (returns promptly — the R5
			// WSClient.Close deadlock fix ensures it never blocks on
			// RecvFrame). Then tear the SSE server down with Close()
			// (forceful; Shutdown would block on long-lived SSE
			// connections — F6). The whole teardown is bounded by a 5s
			// safety timeout so a stuck component can't hang app exit.
			done := make(chan struct{})
			go func() {
				defer close(done)
				if err := app.CloseAgent(); err != nil {
					logger.Error("backend", "chatstream close agent failed",
						map[string]any{"error": err.Error()})
				}
				if err := chatSrv.Stop(ctx); err != nil {
					logger.Error("backend", "chatstream stop failed",
						map[string]any{"error": err.Error()})
				}
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				logger.Error("backend", "chatstream shutdown timed out", nil)
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
