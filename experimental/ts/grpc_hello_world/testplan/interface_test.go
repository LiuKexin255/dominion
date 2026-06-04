package testplan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
)

func TestSayHello(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	client := &http.Client{Timeout: 5 * time.Second}
	reqURL := fmt.Sprintf("%s/experimental/ts/grpc-hello-world/say-hello?name=World", sutHostURL)

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("env", sutEnvName)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var respBody struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if respBody.Message != "Hello World" {
		t.Fatalf("expected message %q, got %q", "Hello World", respBody.Message)
	}
}
