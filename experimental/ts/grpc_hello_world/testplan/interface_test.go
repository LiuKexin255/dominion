package testplan

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
)

func TestSayHello(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	client := &http.Client{Timeout: 5 * time.Second}
	reqURL := fmt.Sprintf("%s/say-hello?name=World", sutHostURL)

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("X-Env", sutEnvName)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	expected := "Hello World"
	if string(body) != expected {
		t.Fatalf("expected %q, got %q", expected, string(body))
	}
}
