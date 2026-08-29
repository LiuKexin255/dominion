package main

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dominion/projects/game/web/server/assets"
)

func Test_newHandler(t *testing.T) {
	handler, err := newHandler()
	if err != nil {
		t.Fatalf("newHandler() unexpected error: %v", err)
	}

	// given: the first hashed JS asset embedded in the dist, addressed by its
	// URL path (embed paths carry the frontend_dist staging prefix).
	embedded, err := fs.Glob(assets.FrontendDist, embedDistDir+"/assets/*.js")
	if err != nil {
		t.Fatalf("fs.Glob(%q) unexpected error: %v", embedDistDir+"/assets/*.js", err)
	}
	if len(embedded) == 0 {
		t.Fatalf("embedded dist contains no assets under %s/assets/", embedDistDir)
	}
	assetPath := "/" + strings.TrimPrefix(embedded[0], embedDistDir+"/")

	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "root serves entry html referencing bundled assets",
			target:     "/",
			wantStatus: http.StatusOK,
			wantBody:   "/assets/",
		},
		{
			name:       "index.html serves the entry html",
			target:     "/index.html",
			wantStatus: http.StatusOK,
			wantBody:   `<div id="root">`,
		},
		{
			name:       "hashed asset is served",
			target:     assetPath,
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing asset returns 404",
			target:     "/missing.js",
			wantStatus: http.StatusNotFound,
		},
	}

	// when: served through a real server so the client follows redirects as a
	// browser would — http.FileServerFS canonically redirects /index.html to
	// ./ (https://pkg.go.dev/net/http#FileServerFS).
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := srv.Client().Get(srv.URL + tt.target)
			if err != nil {
				t.Fatalf("GET %s unexpected error: %v", tt.target, err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("GET %s read body unexpected error: %v", tt.target, err)
			}

			// then
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("GET %s status = %d, want %d (body: %s)", tt.target, resp.StatusCode, tt.wantStatus, body)
			}
			if tt.wantBody != "" && !strings.Contains(string(body), tt.wantBody) {
				t.Fatalf("GET %s body does not contain %q, got: %s", tt.target, tt.wantBody, body)
			}
		})
	}
}
