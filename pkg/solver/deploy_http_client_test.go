package solver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestNewDeployHTTPClient(t *testing.T) {
	// when
	got := NewDeployHTTPClient()

	// then
	if got.baseURL != defaultDeployServiceURL {
		t.Fatalf("NewDeployHTTPClient() baseURL = %q, want %q", got.baseURL, defaultDeployServiceURL)
	}
	if got.httpClient == nil {
		t.Fatal("NewDeployHTTPClient() returned nil httpClient")
	}
}

func TestDeployHTTPClient_GetServiceEndpoints(t *testing.T) {
	tests := []struct {
		name           string
		responseStatus int
		responseBody   string
		wantPath       string
		want           *ServiceEndpointsInfo
		wantErr        error
		errSubstr      string
	}{
		{
			name:           "success parses response and request path",
			responseStatus: http.StatusOK,
			responseBody:   `{"name":"deploy/scopes/dev/environments/alpha/apps/app-a/services/service-b/endpoints","endpoints":["10.0.0.1:8080"],"ports":{"http":8080}}`,
			wantPath:       "/v1/deploy/scopes/dev/environments/alpha/apps/app-a/services/service-b/endpoints",
			want: &ServiceEndpointsInfo{
				Endpoints: []string{"10.0.0.1:8080"},
				Ports:     map[string]int32{"http": 8080},
			},
		},
		{
			name:           "not found maps to service not found",
			responseStatus: http.StatusNotFound,
			responseBody:   `{"error":"missing"}`,
			wantPath:       "/v1/deploy/scopes/dev/environments/alpha/apps/app-a/services/service-b/endpoints",
			wantErr:        ErrServiceNotFound,
		},
		{
			name:           "non-200 returns response body in error",
			responseStatus: http.StatusBadGateway,
			responseBody:   `gateway failed`,
			wantPath:       "/v1/deploy/scopes/dev/environments/alpha/apps/app-a/services/service-b/endpoints",
			errSubstr:      "status 502: gateway failed",
		},
		{
			name:           "invalid json returns decode error",
			responseStatus: http.StatusOK,
			responseBody:   `{`,
			wantPath:       "/v1/deploy/scopes/dev/environments/alpha/apps/app-a/services/service-b/endpoints",
			errSubstr:      "decode service endpoints",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.WriteHeader(tt.responseStatus)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := &DeployHTTPClient{
				baseURL:    server.URL,
				httpClient: server.Client(),
			}

			// when
			got, err := client.GetServiceEndpoints(context.Background(), "deploy/scopes/dev/environments/alpha/apps/app-a/services/service-b/endpoints")

			// then
			if gotPath != tt.wantPath {
				t.Fatalf("GetServiceEndpoints() path = %q, want %q", gotPath, tt.wantPath)
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("GetServiceEndpoints() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if tt.errSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("GetServiceEndpoints() error = %v, want substring %q", err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetServiceEndpoints() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("GetServiceEndpoints() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
