package solver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"dominion/projects/infra/deploy"
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
				Endpoints:         []string{"10.0.0.1:8080"},
				Ports:             map[string]int32{"http": 8080},
				StatefulInstances: nil,
				IsStateful:        false,
			},
		},
		{
			name:           "success with stateful instances parses and sorts by index",
			responseStatus: http.StatusOK,
			responseBody:   `{"endpoints":["10.0.0.1:8080","10.0.0.2:8080","10.0.0.3:8080"],"ports":{"http":8080},"is_stateful":true,"stateful_instances":[{"index":2,"endpoints":["10.0.0.3:8080"]},{"index":0,"endpoints":["10.0.0.1:8080"]},{"index":1,"endpoints":["10.0.0.2:8080"]}]}`,
			wantPath:       "/v1/deploy/scopes/dev/environments/alpha/apps/app-a/services/service-b/endpoints",
			want: &ServiceEndpointsInfo{
				Endpoints: []string{"10.0.0.1:8080", "10.0.0.2:8080", "10.0.0.3:8080"},
				Ports:     map[string]int32{"http": 8080},
				StatefulInstances: []*StatefulInstance{
					{Index: 0, Endpoints: []string{"10.0.0.1:8080"}},
					{Index: 1, Endpoints: []string{"10.0.0.2:8080"}},
					{Index: 2, Endpoints: []string{"10.0.0.3:8080"}},
				},
				IsStateful: true,
			},
		},
		{
			name:           "success with empty stateful instances returns nil",
			responseStatus: http.StatusOK,
			responseBody:   `{"endpoints":["10.0.0.1:8080"],"ports":{"http":8080},"is_stateful":true,"stateful_instances":[]}`,
			wantPath:       "/v1/deploy/scopes/dev/environments/alpha/apps/app-a/services/service-b/endpoints",
			want: &ServiceEndpointsInfo{
				Endpoints:         []string{"10.0.0.1:8080"},
				Ports:             map[string]int32{"http": 8080},
				StatefulInstances: nil,
				IsStateful:        true,
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

func Test_convertStatefulInstances(t *testing.T) {
	tests := []struct {
		name string
		in   []*deploy.StatefulServiceInstance
		want []*StatefulInstance
	}{
		{
			name: "nil input returns nil",
			in:   nil,
			want: nil,
		},
		{
			name: "empty slice returns nil",
			in:   []*deploy.StatefulServiceInstance{},
			want: nil,
		},
		{
			name: "single instance",
			in: []*deploy.StatefulServiceInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:8080"}},
			},
			want: []*StatefulInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:8080"}},
			},
		},
		{
			name: "multiple instances sorted by index",
			in: []*deploy.StatefulServiceInstance{
				{Index: 2, Endpoints: []string{"10.0.0.3:8080"}},
				{Index: 0, Endpoints: []string{"10.0.0.1:8080"}},
				{Index: 1, Endpoints: []string{"10.0.0.2:8080"}},
			},
			want: []*StatefulInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:8080"}},
				{Index: 1, Endpoints: []string{"10.0.0.2:8080"}},
				{Index: 2, Endpoints: []string{"10.0.0.3:8080"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertStatefulInstances(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("convertStatefulInstances() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
