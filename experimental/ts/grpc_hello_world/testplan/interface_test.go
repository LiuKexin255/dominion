package testplan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
)

const (
	headerEnv  = "env"
	pathPrefix = "/experimental/ts/grpc-hello-world/say-hello"
	// Expected values sourced from the SUT config/env declarations:
	//   configMessage / configTimes — service.yaml configs.service_config.greeting
	//     (contracts/yaml-schema.md §1; overrides the SDK defaults "hello"/1,
	//     proving FR-015 deep merge)
	//   greetingSuffixEnv — deploy.yaml service artifact env GREETING_SUFFIX
	//     (specs/045-deploy-config/spec.md SC-006/FR-016: config and env coexist)
	configMessage     = "hello from ts config"
	configTimes       = 3
	greetingSuffixEnv = "ts-suffix"
)

type helloResponse struct {
	Message string `json:"message"`
}

func TestSayHello(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	client := &http.Client{Timeout: 5 * time.Second}

	tests := []struct {
		name      string
		queryName string
		want      string
	}{
		{
			name:      "plain name",
			queryName: "World",
			want:      "hello from ts config World x3 ts-suffix",
		},
		{
			name:      "another name",
			queryName: "Alice",
			want:      "hello from ts config Alice x3 ts-suffix",
		},
		{
			name:      "url encoded name",
			queryName: "Carol%20Smith",
			want:      "hello from ts config Carol Smith x3 ts-suffix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			reqURL := fmt.Sprintf("%s%s?name=%s", sutHostURL, pathPrefix, tt.queryName)

			req, err := http.NewRequest(http.MethodGet, reqURL, nil)
			if err != nil {
				t.Fatalf("http.NewRequest(%q) unexpected error: %v", reqURL, err)
			}
			req.Header.Set(headerEnv, sutEnvName)

			// when
			resp, err := client.Do(req)
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
			if got.Message != tt.want {
				t.Errorf("got.Message = %q, want %q", got.Message, tt.want)
			}
		})
	}
}

// TestSayHelloConfigOverrides verifies that config parameters and env
// parameters coexist without interference (SC-006/FR-016,
// specs/045-deploy-config/spec.md): the response carries the
// config-overridden greeting (FR-015 deep merge: message and times both
// overridden) AND the GREETING_SUFFIX env content.
func TestSayHelloConfigOverrides(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	reqURL := fmt.Sprintf("%s%s?name=World", sutHostURL, pathPrefix)
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
	if !strings.Contains(got.Message, configMessage) {
		t.Errorf("got.Message = %q, want it to contain config message %q (FR-015)", got.Message, configMessage)
	}
	if !strings.Contains(got.Message, fmt.Sprintf("x%d", configTimes)) {
		t.Errorf("got.Message = %q, want it to contain times %d (FR-015)", got.Message, configTimes)
	}
	if !strings.Contains(got.Message, greetingSuffixEnv) {
		t.Errorf("got.Message = %q, want it to contain GREETING_SUFFIX %q (FR-016)", got.Message, greetingSuffixEnv)
	}
}
