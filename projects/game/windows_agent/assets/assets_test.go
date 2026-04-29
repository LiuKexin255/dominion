package assets_test

import (
	"embed"
	"io/fs"
	"strings"
	"testing"

	"dominion/projects/game/windows_agent/assets"
)

var _ embed.FS = assets.FrontendDist

func TestFrontendDistContainsIndexHTML(t *testing.T) {
	// Given frontend dist files embedded by the Bazel frontend_embed_assets rule.
	tests := []struct {
		name    string
		path    string
		wantSub string
	}{
		{name: "index.html exists", path: "frontend_dist/index.html", wantSub: "<!doctype html>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When reading the embedded frontend entry point.
			data, err := assets.FrontendDist.ReadFile(tt.path)

			// Then it contains the expected HTML document marker.
			if err != nil {
				t.Fatalf("FrontendDist.ReadFile(%q): %v", tt.path, err)
			}
			if !strings.Contains(string(data), tt.wantSub) {
				t.Errorf("FrontendDist.ReadFile(%q) content does not contain %q", tt.path, tt.wantSub)
			}
		})
	}
}

func TestFrontendDistHasAssets(t *testing.T) {
	// Given the embedded frontend asset directory.
	assetPath := "frontend_dist/assets"

	// When listing the assets emitted by the frontend build.
	entries, err := fs.ReadDir(assets.FrontendDist, assetPath)

	// Then both JavaScript and CSS bundles are present.
	if err != nil {
		t.Fatalf("fs.ReadDir(FrontendDist, %q): %v", assetPath, err)
	}
	hasJS := false
	hasCSS := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".js") {
			hasJS = true
		}
		if strings.HasSuffix(e.Name(), ".css") {
			hasCSS = true
		}
	}
	if !hasJS {
		t.Error("FrontendDist does not contain any .js asset")
	}
	if !hasCSS {
		t.Error("FrontendDist does not contain any .css asset")
	}
}
