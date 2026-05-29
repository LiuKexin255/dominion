package main

import (
	"context"

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
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
