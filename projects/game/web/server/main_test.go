package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func Test_newHandler(t *testing.T) {
	// when: served through a real server so requests run the full handler
	// chain
	srv := httptest.NewServer(newHandler())
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET / unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// then: the placeholder handler answers 404 until the static hosting
	// wiring lands
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET / status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}
