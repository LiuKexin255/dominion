package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	deploy "dominion/projects/infra/deploy"
	clientpkg "dominion/tools/deploy/v2/client"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestDelCommand(t *testing.T) {
	tests := []struct {
		name          string
		target        string
		scope         string
		timeout       time.Duration
		handler       http.HandlerFunc
		wantOutput    string
		wantErrIs     error
		wantErrSubstr string
	}{
		{
			name:    "success",
			target:  "api",
			scope:   "dev",
			timeout: 50 * time.Millisecond,
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodDelete:
					if r.URL.Path != "/v1/deploy/scopes/dev/environments/api" {
						t.Fatalf("delete path = %s", r.URL.Path)
					}
					w.WriteHeader(http.StatusOK)
				case http.MethodGet:
					if r.URL.Path != "/v1/deploy/scopes/dev/environments/api" {
						t.Fatalf("poll path = %s", r.URL.Path)
					}
					writeMainJSONResponse(t, w, http.StatusNotFound, map[string]any{"code": 5, "message": "not found"})
				default:
					t.Fatalf("method = %s", r.Method)
				}
			},
			wantOutput: "环境 dev.api 已删除",
		},
		{
			name:    "environment not found",
			target:  "dev.api",
			timeout: 50 * time.Millisecond,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Fatalf("method = %s, want DELETE", r.Method)
				}
				writeMainJSONResponse(t, w, http.StatusNotFound, map[string]any{"code": 5, "message": "not found"})
			},
			wantErrIs: clientpkg.ErrNotFound,
		},
		{
			name:    "timeout",
			target:  "api",
			scope:   "dev",
			timeout: 20 * time.Millisecond,
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodDelete:
					w.WriteHeader(http.StatusOK)
				case http.MethodGet:
					writeMainJSONResponse(t, w, http.StatusOK, &deploy.Environment{Name: "deploy/scopes/dev/environments/api", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_DELETING}})
				default:
					t.Fatalf("method = %s", r.Method)
				}
			},
			wantErrSubstr: "poll until deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			t.Cleanup(server.Close)

			root, cwd := newCommandWorkspace(t)
			withWorkingDir(t, cwd)
			if tt.scope != "" {
				if err := saveConfig(root, &cliConfig{DefaultScope: tt.scope}); err != nil {
					t.Fatalf("saveConfig() failed: %v", err)
				}
			}

			gotOutput, err := captureOutputAndError(t, func() error {
				return delCommand(&options{target: tt.target, scope: tt.scope, endpoint: server.URL, timeout: tt.timeout})
			})

			if tt.wantErrIs != nil || tt.wantErrSubstr != "" {
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("delCommand() error = %v, want %v", err, tt.wantErrIs)
				}
				if tt.wantErrSubstr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrSubstr)) {
					t.Fatalf("delCommand() error = %v, want substring %q", err, tt.wantErrSubstr)
				}
				return
			}

			if err != nil {
				t.Fatalf("delCommand() unexpected error: %v", err)
			}
			if strings.TrimSpace(gotOutput) != tt.wantOutput {
				t.Fatalf("delCommand() output = %q, want %q", strings.TrimSpace(gotOutput), tt.wantOutput)
			}
		})
	}
}

func TestListCommand(t *testing.T) {
	tests := []struct {
		name          string
		scope         string
		seed          *cliConfig
		response      any
		status        int
		wantOutput    string
		wantErrSubstr string
	}{
		{
			name:       "success with environments",
			scope:      "dev",
			status:     http.StatusOK,
			response:   &deploy.ListEnvironmentsResponse{Environments: []*deploy.Environment{{Name: "deploy/scopes/dev/environments/api"}, {Name: "deploy/scopes/dev/environments/web"}}},
			wantOutput: "deploy/scopes/dev/environments/api\ndeploy/scopes/dev/environments/web",
		},
		{
			name:       "empty list",
			scope:      "dev",
			status:     http.StatusOK,
			response:   &deploy.ListEnvironmentsResponse{},
			wantOutput: "",
		},
		{
			name:          "no scope error",
			status:        http.StatusOK,
			response:      &deploy.ListEnvironmentsResponse{},
			wantErrSubstr: "没有默认 scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErrSubstr == "" {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						t.Fatalf("method = %s, want GET", r.Method)
					}
					if r.URL.Path != "/v1/deploy/scopes/dev/environments" {
						t.Fatalf("path = %s", r.URL.Path)
					}
					writeMainJSONResponse(t, w, tt.status, tt.response)
				}))
				t.Cleanup(server.Close)

				root, cwd := newCommandWorkspace(t)
				withWorkingDir(t, cwd)
				if tt.seed != nil {
					if err := saveConfig(root, tt.seed); err != nil {
						t.Fatalf("saveConfig() failed: %v", err)
					}
				}

				gotOutput := captureStdout(t, func() error {
					return listCommand(&options{scope: tt.scope, endpoint: server.URL, timeout: 50 * time.Millisecond})
				})
				if strings.TrimSpace(gotOutput) != tt.wantOutput {
					t.Fatalf("listCommand() output = %q, want %q", strings.TrimSpace(gotOutput), tt.wantOutput)
				}
				return
			}

			root, cwd := newCommandWorkspace(t)
			withWorkingDir(t, cwd)
			if tt.seed != nil {
				if err := saveConfig(root, tt.seed); err != nil {
					t.Fatalf("saveConfig() failed: %v", err)
				}
			}

			err := listCommand(&options{endpoint: "http://127.0.0.1:1", timeout: 50 * time.Millisecond})
			if err == nil || !strings.Contains(err.Error(), tt.wantErrSubstr) {
				t.Fatalf("listCommand() error = %v, want substring %q", err, tt.wantErrSubstr)
			}
		})
	}
}

func newCommandWorkspace(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	cwd := filepath.Join(root, "apps", "svc")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	return root, cwd
}

func captureOutputAndError(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := stdout
	var out bytes.Buffer
	stdout = &out
	t.Cleanup(func() { stdout = oldStdout })

	err := fn()
	return out.String(), err
}

func writeMainJSONResponse(t *testing.T, w http.ResponseWriter, statusCode int, body any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if body == nil {
		return
	}

	if message, ok := body.(proto.Message); ok {
		payload, err := protojson.Marshal(message)
		if err != nil {
			t.Fatalf("protojson.Marshal() failed: %v", err)
		}
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
		return
	}

	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("json.NewEncoder() failed: %v", err)
	}
}
