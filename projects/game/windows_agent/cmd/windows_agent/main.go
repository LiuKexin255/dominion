package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"dominion/projects/game/windows_agent/assets"
	"dominion/projects/game/windows_agent/internal/app"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	agentApp := app.NewApp()

	// Signal handling for graceful shutdown on Ctrl+C.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		agentApp.WailsShutdown()
		os.Exit(0)
	}()

	err := wails.Run(&options.App{
		Title:     "Windows Agent",
		Width:     800,
		Height:    600,
		MinWidth:  640,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: assets.FrontendDist,
		},
		OnStartup:  func(ctx context.Context) { agentApp.WailsInit(ctx) },
		OnShutdown: func(ctx context.Context) { agentApp.WailsShutdown() },
	})

	if err != nil {
		log.Fatal(err)
	}
}
