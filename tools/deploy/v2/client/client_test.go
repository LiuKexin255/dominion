package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	deploy "dominion/projects/infra/deploy"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestClient_GetEnvironment(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   any
		want       *deploy.Environment
		wantErrIs  error
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response: &deploy.Environment{
				Name:        "deploy/scopes/dev/environments/api",
				Description: "api env",
				DesiredState: &deploy.EnvironmentDesiredState{
					Artifacts: []*deploy.ArtifactSpec{{Name: "api", App: "gateway", Image: "example.com/gateway:v1", Replicas: 1}},
				},
				Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY},
			},
			want: &deploy.Environment{
				Name:        "deploy/scopes/dev/environments/api",
				Description: "api env",
				DesiredState: &deploy.EnvironmentDesiredState{
					Artifacts: []*deploy.ArtifactSpec{{Name: "api", App: "gateway", Image: "example.com/gateway:v1", Replicas: 1}},
				},
				Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY},
			},
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response: map[string]any{
				"code":    5,
				"message": "environment not found",
			},
			wantErrIs: ErrNotFound,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			response: map[string]any{
				"code":    13,
				"message": "internal error",
			},
			wantErrIs: ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatalf("method = %s, want %s", r.Method, http.MethodGet)
				}
				if r.URL.Path != "/v1/deploy/scopes/dev/environments/api" {
					t.Fatalf("path = %s, want %s", r.URL.Path, "/v1/deploy/scopes/dev/environments/api")
				}
				writeJSONResponse(t, w, tt.statusCode, tt.response)
			}))
			defer server.Close()

			client := NewClient(server.URL)
			client.httpClient = server.Client()

			got, err := client.GetEnvironment(context.Background(), "deploy/scopes/dev/environments/api")
			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("GetEnvironment() error = %v, want %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetEnvironment() unexpected error: %v", err)
			}
			if !proto.Equal(got, tt.want) {
				t.Fatalf("GetEnvironment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClient_CreateEnvironment(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   any
		wantErrIs  error
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response:   &deploy.Environment{Name: "deploy/scopes/dev/environments/api", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}},
		},
		{
			name:       "already exists",
			statusCode: http.StatusConflict,
			response: map[string]any{
				"code":    6,
				"message": "environment already exists",
			},
			wantErrIs: ErrAlreadyExists,
		},
	}

	env := &deploy.Environment{
		Description: "api env",
		DesiredState: &deploy.EnvironmentDesiredState{
			Artifacts: []*deploy.ArtifactSpec{{
				Name:       "api",
				App:        "gateway",
				Image:      "example.com/gateway:v1",
				Replicas:   1,
				TlsEnabled: true,
				Http: &deploy.ArtifactHTTPSpec{
					Hostnames: []string{"api.example.com"},
					Matches:   []*deploy.HTTPRouteRule{{Backend: "http", Path: &deploy.HTTPPathRule{Type: deploy.HTTPPathRuleType_HTTP_PATH_RULE_TYPE_PATH_PREFIX, Value: "/v1"}}},
				},
			}},
			Infras: []*deploy.InfraSpec{{Resource: "redis", Profile: "cache", Name: "redis-main", App: "gateway", Persistence: &deploy.InfraPersistenceSpec{Enabled: true}}},
		},
	}

	wantRequest := &deploy.CreateEnvironmentRequest{
		Parent:      "deploy/scopes/dev",
		EnvName:     "api",
		Environment: env,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
				}
				if r.URL.Path != "/v1/deploy/scopes/dev/environments" {
					t.Fatalf("path = %s, want %s", r.URL.Path, "/v1/deploy/scopes/dev/environments")
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				assertProtoJSONBody(t, body, new(deploy.CreateEnvironmentRequest), wantRequest)

				writeJSONResponse(t, w, tt.statusCode, tt.response)
			}))
			defer server.Close()

			client := NewClient(server.URL)
			client.httpClient = server.Client()

			got, err := client.CreateEnvironment(context.Background(), "deploy/scopes/dev", "api", env)
			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("CreateEnvironment() error = %v, want %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateEnvironment() unexpected error: %v", err)
			}
			if got == nil || got.Name != "deploy/scopes/dev/environments/api" {
				t.Fatalf("CreateEnvironment() name = %#v, want %q", got, "deploy/scopes/dev/environments/api")
			}
		})
	}
}

func TestClient_UpdateEnvironment(t *testing.T) {
	env := &deploy.Environment{
		Name: "deploy/scopes/dev/environments/api",
		DesiredState: &deploy.EnvironmentDesiredState{
			Artifacts: []*deploy.ArtifactSpec{{Name: "api", App: "gateway", Image: "example.com/gateway:v2", Replicas: 2}},
		},
	}

	wantRequest := &deploy.UpdateEnvironmentRequest{
		Environment: env,
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPatch)
		}
		if r.URL.Path != "/v1/deploy/scopes/dev/environments/api" {
			t.Fatalf("path = %s, want %s", r.URL.Path, "/v1/deploy/scopes/dev/environments/api")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		assertProtoJSONBody(t, body, new(deploy.UpdateEnvironmentRequest), wantRequest)

		writeJSONResponse(t, w, http.StatusOK, &deploy.Environment{Name: env.Name, Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	client.httpClient = server.Client()

	got, err := client.UpdateEnvironment(context.Background(), env)
	if err != nil {
		t.Fatalf("UpdateEnvironment() unexpected error: %v", err)
	}
	if got == nil || got.Status == nil || got.Status.State != deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING {
		t.Fatalf("UpdateEnvironment() = %#v, want reconciling state", got)
	}
}

func TestClient_DeleteEnvironment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodDelete)
		}
		if r.URL.Path != "/v1/deploy/scopes/dev/environments/api" {
			t.Fatalf("path = %s, want %s", r.URL.Path, "/v1/deploy/scopes/dev/environments/api")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	client.httpClient = server.Client()

	if err := client.DeleteEnvironment(context.Background(), "deploy/scopes/dev/environments/api"); err != nil {
		t.Fatalf("DeleteEnvironment() unexpected error: %v", err)
	}
}

func TestClient_ListEnvironments(t *testing.T) {
	tests := []struct {
		name     string
		response any
		want     []*deploy.Environment
	}{
		{
			name: "success",
			response: &deploy.ListEnvironmentsResponse{
				Environments: []*deploy.Environment{
					{Name: "deploy/scopes/dev/environments/api", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY}},
					{Name: "deploy/scopes/dev/environments/web", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}},
				},
			},
			want: []*deploy.Environment{
				{Name: "deploy/scopes/dev/environments/api", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY}},
				{Name: "deploy/scopes/dev/environments/web", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}},
			},
		},
		{
			name:     "empty list",
			response: &deploy.ListEnvironmentsResponse{},
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatalf("method = %s, want %s", r.Method, http.MethodGet)
				}
				if r.URL.Path != "/v1/deploy/scopes/dev/environments" {
					t.Fatalf("path = %s, want %s", r.URL.Path, "/v1/deploy/scopes/dev/environments")
				}
				writeJSONResponse(t, w, http.StatusOK, tt.response)
			}))
			defer server.Close()

			client := NewClient(server.URL)
			client.httpClient = server.Client()

			got, err := client.ListEnvironments(context.Background(), "deploy/scopes/dev")
			if err != nil {
				t.Fatalf("ListEnvironments() unexpected error: %v", err)
			}
			assertEnvironmentSliceEqual(t, got, tt.want)
		})
	}
}

func assertProtoJSONBody(t *testing.T, raw []byte, got proto.Message, want proto.Message) {
	t.Helper()

	if err := protojson.Unmarshal(raw, got); err != nil {
		t.Fatalf("protojson.Unmarshal() failed: %v", err)
	}
	if !proto.Equal(got, want) {
		gotRaw, _ := protojson.Marshal(got)
		wantRaw, _ := protojson.Marshal(want)
		t.Fatalf("request body = %s, want %s", gotRaw, wantRaw)
	}
}

func assertEnvironmentSliceEqual(t *testing.T, got, want []*deploy.Environment) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len(ListEnvironments()) = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if !proto.Equal(got[i], want[i]) {
			t.Fatalf("ListEnvironments()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, statusCode int, response any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if response == nil {
		return
	}

	if message, ok := response.(proto.Message); ok {
		payload, err := protojson.Marshal(message)
		if err != nil {
			t.Fatalf("protojson.Marshal() failed: %v", err)
		}
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
		return
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Fatalf("Encode() failed: %v", err)
	}
}
