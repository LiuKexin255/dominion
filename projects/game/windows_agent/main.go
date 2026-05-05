package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"dominion/projects/game/windows_agent/assets"
	"dominion/projects/game/windows_agent/internal/app"
	"dominion/projects/game/windows_agent/internal/encoder"
	"dominion/projects/game/windows_agent/internal/input"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	agentApp := newApp()

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
		Bind: []interface{}{
			agentApp,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}

func newApp() *app.App {
	var initErrors []string
	var ffmpegPath, helperPath string

	exePath, err := os.Executable()
	if err != nil {
		initErrors = append(initErrors, "resolve executable path: "+err.Error())
	} else {
		agentDir := filepath.Dir(exePath)

		ffmpegPath, err = encoder.ResolveFFmpegPath(agentDir)
		if err != nil {
			initErrors = append(initErrors, "resolve ffmpeg path: "+err.Error())
		} else if err := encoder.ValidateFFmpeg(ffmpegPath); err != nil {
			initErrors = append(initErrors, "validate ffmpeg: "+err.Error())
			ffmpegPath = ""
		}

		helperPath, err = input.ResolveHelperPath(agentDir)
		if err != nil {
			initErrors = append(initErrors, "resolve input helper path: "+err.Error())
		}
	}

	return app.NewApp(
		app.WithFFmpegPath(ffmpegPath),
		app.WithHelperPath(helperPath),
		app.WithInitErrors(initErrors),
	)
}
