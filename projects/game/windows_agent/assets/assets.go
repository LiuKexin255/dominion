// Package assets provides embedded frontend build output for the Wails application.
// frontend_dist is a Bazel-generated directory containing Vite build output,
// produced by the frontend_embed_assets rule in the assets package.
package assets

import "embed"

//go:embed all:frontend_dist
var FrontendDist embed.FS
