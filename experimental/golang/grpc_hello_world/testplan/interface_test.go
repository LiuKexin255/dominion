package testplan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
)

const (
	headerEnv  = "env"
	pathPrefix = "/v1/world/"
)

// Expected greeting values applied to the deployed service; they must match
// the greeting entry of service/service.yaml (block service_config) and the
// GREETING_SUFFIX env of testplan/deploy.yaml
// (specs/045-deploy-config/quickstart.md 场景 1).
const (
	wantGreetingMessage = "hello from config"
	wantGreetingTimes   = 3
	wantGreetingSuffix  = "-from-env"
)

// wantGreeting is the full response message the service must return: the
// configured message repeated Times times plus the env suffix, mirroring the
// construction in service/main.go GetHello.
var wantGreeting = strings.TrimSpace(strings.Repeat(wantGreetingMessage+" ", wantGreetingTimes)) + wantGreetingSuffix

type helloResponse struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func TestGetHello(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	tests := []struct {
		name        string
		pathSegment string
		wantName    string
	}{
		{
			name:        "normal name",
			pathSegment: "Alice",
			wantName:    "world/Alice",
		},
		{
			name:        "another valid name",
			pathSegment: "Bob",
			wantName:    "world/Bob",
		},
		{
			name:        "url encoded name",
			pathSegment: "Carol%20Smith",
			wantName:    "world/Carol Smith",
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
			if got.Message != wantGreeting {
				t.Errorf("got.Message = %q, want %q", got.Message, wantGreeting)
			}
		})
	}
}

func TestGetHelloConfigOverride(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	reqURL := fmt.Sprintf("%s%s%s", sutHostURL, pathPrefix, "world")
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
	// The response must reflect the mounted config over the defaults: the
	// default message "hello" is replaced by "hello from config" and Times is
	// 3 instead of 1, proving mount, SDK read and deep merge (FR-013~015
	// specs/045-deploy-config/spec.md). The GREETING_SUFFIX suffix proves
	// config and user env coexist independently (FR-016).
	if !strings.Contains(got.Message, wantGreetingMessage) {
		t.Errorf("got.Message = %q, want it to contain config message %q (FR-015)", got.Message, wantGreetingMessage)
	}
	// The full repeated fragment proves Times=3 overrides the default 1.
	repeated := strings.TrimSpace(strings.Repeat(wantGreetingMessage+" ", wantGreetingTimes))
	if !strings.Contains(got.Message, repeated) {
		t.Errorf("got.Message = %q, want it to contain %q (Times %d, FR-015)", got.Message, repeated, wantGreetingTimes)
	}
	if !strings.Contains(got.Message, wantGreetingSuffix) {
		t.Errorf("got.Message = %q, want it to contain GREETING_SUFFIX %q (FR-016)", got.Message, wantGreetingSuffix)
	}
}

func TestOTelIntegration(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	reqURL := fmt.Sprintf("%s%s%s", sutHostURL, pathPrefix, "world")
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest(%q) unexpected error: %v", reqURL, err)
	}
	req.Header.Set(headerEnv, sutEnvName)

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

	// Verify response is valid (basic check - OTel is integrated if service responds)
	if got.Name != "world/world" {
		t.Errorf("got.Name = %q, want %q", got.Name, "world/world")
	}
	// This assertion implicitly depends on the config pipeline: the deploy
	// selects service_config, so a config failure (mount/read/deep-merge)
	// would fail this case too, although it is not the OTel under test.
	if got.Message != wantGreeting {
		t.Errorf("got.Message = %q, want %q", got.Message, wantGreeting)
	}
}
