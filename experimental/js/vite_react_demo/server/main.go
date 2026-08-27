// The vite-react-demo server hosts the demo frontend's vite build output as
// static pages: the dist tree artifact is embedded into the binary at build
// time and served as-is — no API, routing logic, or persistence beyond
// process observability
// (specs/050-vite-react-bazel/contracts/static-server-deploy.md §1.2).
package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net/http"

	"dominion/common/gopkg/bootstrap"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
	"dominion/experimental/js/vite_react_demo/server/assets"
)

// embedDistDir is the directory prefix the staged dist lives under inside the
// embed FS — the wails_asset_library default out
// (tools/release/wails/private/assets.bzl).
const embedDistDir = "frontend_dist"

var port = flag.String("port", "8080", "Port to listen on")

func newHandler() (http.Handler, error) {
	dist, err := fs.Sub(assets.FrontendDist, embedDistDir)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServerFS(dist))
	return mux, nil
}

func main() {
	flag.Parse()

	handler, err := newHandler()
	if err != nil {
		log.Fatalf("failed to build static file handler: %v", err)
	}

	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(handler, "vite-react-demo"),
	}

	log.Printf("vite-react-demo listening on :%s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}
