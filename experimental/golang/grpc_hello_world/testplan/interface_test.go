package testplan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
)

const (
	envSUTHostURL = "SUT_HOST_URL"
	envSUTEnvName = "SUT_ENV_NAME"
	headerEnv     = "env"
	pathPrefix    = "/v1/world/"
)

type helloResponse struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func TestGetHello(t *testing.T) {
	sutHostURL := os.Getenv(envSUTHostURL)
	if sutHostURL == "" {
		t.Fatalf("environment variable %s is required", envSUTHostURL)
	}

	sutEnvName := os.Getenv(envSUTEnvName)
	if sutEnvName == "" {
		t.Fatalf("environment variable %s is required", envSUTEnvName)
	}

	tests := []struct {
		name        string
		pathSegment string
		wantName    string
		wantMessage string
	}{
		{
			name:        "normal name",
			pathSegment: "Alice",
			wantName:    "world/Alice",
			wantMessage: "Hello, world/Alice!",
		},
		{
			name:        "another valid name",
			pathSegment: "Bob",
			wantName:    "world/Bob",
			wantMessage: "Hello, world/Bob!",
		},
		{
			name:        "url encoded name",
			pathSegment: "Carol%20Smith",
			wantName:    "world/Carol Smith",
			wantMessage: "Hello, world/Carol Smith!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			reqURL := fmt.Sprintf("%s%s%s", sutHostURL, pathPrefix, tt.pathSegment)

			req, err := http.NewRequest(http.MethodGet, reqURL, nil)
			if err != nil {
				t.Fatalf("http.NewRequest(%q) unexpected error: %v", reqURL, err)
			}
			req.Header.Set(headerEnv, sutEnvName)

			// when
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("http.Do(%q) unexpected error: %v", reqURL, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("http.Do(%q) status = %d, want %d", reqURL, resp.StatusCode, http.StatusOK)
			}
			got := new(helloResponse)
			if err := json.NewDecoder(resp.Body).Decode(got); err != nil {
				t.Fatalf("json.Decode unexpected error: %v", err)
			}

			// then
			if got.Name != tt.wantName {
				t.Errorf("got.Name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Message != tt.wantMessage {
				t.Errorf("got.Message = %q, want %q", got.Message, tt.wantMessage)
			}
		})
	}
}
