// The game web server hosts the React frontend's vite build output as static
// pages: the dist tree artifact is embedded into the binary at build time via
// the //projects/game/web/server/assets asset library (Bazel wiring mirrors
// experimental/js/vite_react_demo/server, specs/049-agent-v2-dsh-init/plan.md
// D8). The static file-server wiring lands with the hosting step
// (specs/049-agent-v2-dsh-init/tasks.md T022); until then every request is
// answered by the 404 placeholder below.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"dominion/common/gopkg/bootstrap"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/web/server/assets"
)

// FrontendDist is the embedded frontend dist tree staged under the
// wails_asset_library default "frontend_dist" prefix
// (tools/release/wails/private/assets.bzl); the hosting step serves
// fs.Sub(FrontendDist, "frontend_dist") at "/". The reference keeps the
// assets package linked ahead of that step.
var _ = assets.FrontendDist

var port = flag.String("port", "8080", "Port to listen on")

// newHandler returns the web service's HTTP handler. Placeholder 404 until
// the static hosting wiring (T022).
func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", http.NotFound)
	return mux
}

func main() {
	flag.Parse()

	handler := newHandler()

	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(handler, "game-web"),
	}

	log.Printf("game web listening on :%s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}
