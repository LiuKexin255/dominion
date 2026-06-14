package testplan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"dominion/common/gopkg/testtool"
)

const (
	headerEnv  = "env"
	pathPrefix = "/experimental/grpc-chain/echo/"
)

type echoResponse struct {
	Message string `json:"message"`
	Chain   string `json:"chain"`
}

func TestGatewayToMidService(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	tests := []struct {
		name        string
		pathSegment string
	}{
		{name: "hello", pathSegment: "hello"},
		{name: "world", pathSegment: "world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqURL := fmt.Sprintf("%s%s%s", sutHostURL, pathPrefix, tt.pathSegment)

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
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}

			got := new(echoResponse)
			if err := json.NewDecoder(resp.Body).Decode(got); err != nil {
				t.Fatalf("json.Decode unexpected error: %v", err)
			}

		if got.Chain != "mid→backend" {
			t.Errorf("Chain = %q, want %q (proves full chain gateway→grpc-js→grpc-go)", got.Chain, "mid→backend")
			}
		})
	}
}
