// Package assets provides embedded frontend build output for the Wails application.
// frontend_dist is a symlink to ../frontend/dist created by the build system.
package assets

import "embed"

//go:embed all:frontend_dist
var FrontendDist embed.FS
