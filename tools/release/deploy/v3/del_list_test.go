package main

import (
	"bytes"
	"context"
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
	clientpkg "dominion/tools/release/deploy/v2/client"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestDelCommand(t *testing.T) {
	tests := []struct {
		name          string
		target        string
		timeout       time.Duration
		handler       http.HandlerFunc
		wantOutput    string
		wantErrIs     error
		wantErrSubstr string
	}{
		{
			name:    "success",
			target:  "dev.api",
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
					writeDelListJSONResponse(t, w, http.StatusNotFound, map[string]any{"code": 5, "message": "not found"})
				default:
					t.Fatalf("method = %s", r.Method)
				}
			},
			wantOutput: "环境 dev.api 已删除",
		},
		{
			// US3 验收场景 1（specs/033-deploy-scope-cleanup/spec.md:76）：
			// 短名（无点号）须被拒绝并说明需要完整 {scope}.{env_name} 格式，
			// 且不得发起任何 HTTP 调用。
			name:    "short name rejected",
			target:  "dev",
			timeout: 50 * time.Millisecond,
			handler: func(w http.ResponseWriter, r *http.Request) {
				t.Fatalf("short name should be rejected before any HTTP call")
			},
			wantErrSubstr: "非法完整环境名",
		},
		{
			name:    "environment not found",
			target:  "dev.api",
			timeout: 50 * time.Millisecond,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Fatalf("method = %s, want DELETE", r.Method)
				}
				writeDelListJSONResponse(t, w, http.StatusNotFound, map[string]any{"code": 5, "message": "not found"})
			},
			wantErrIs: clientpkg.ErrNotFound,
		},
		{
			name:    "timeout",
			target:  "dev.api",
			timeout: 20 * time.Millisecond,
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodDelete:
					w.WriteHeader(http.StatusOK)
				case http.MethodGet:
					writeDelListJSONResponse(t, w, http.StatusOK, &deploy.Environment{Name: "deploy/scopes/dev/environments/api", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_DELETING}})
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

			_, cwd := newDelListWorkspace(t)
			withWorkingDir(t, cwd)

			gotOutput, err := captureDelListOutput(t, func() error {
				return delCommand(context.Background(), &options{target: tt.target, endpoint: server.URL, timeout: tt.timeout, apiClient: clientpkg.NewClient(server.URL)})
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
		name       string
		scope      string
		wantPath   string
		response   any
		status     int
		wantOutput string
	}{
		{
			name:       "success with environments",
			scope:      "dev",
			wantPath:   "/v1/deploy/scopes/dev/environments",
			status:     http.StatusOK,
			response:   &deploy.ListEnvironmentsResponse{Environments: []*deploy.Environment{{Name: "deploy/scopes/dev/environments/api", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY}}, {Name: "deploy/scopes/dev/environments/web", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}}}},
			wantOutput: "dev.api\t就绪\ndev.web\t部署中",
		},
		{
			name:       "empty list",
			scope:      "dev",
			wantPath:   "/v1/deploy/scopes/dev/environments",
			status:     http.StatusOK,
			response:   &deploy.ListEnvironmentsResponse{},
			wantOutput: "",
		},
		{
			name:     "waiting rollout environment",
			scope:    "dev",
			wantPath: "/v1/deploy/scopes/dev/environments",
			status:   http.StatusOK,
			response: &deploy.ListEnvironmentsResponse{Environments: []*deploy.Environment{{
				Name:   "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_WAITING_ROLLOUT},
			}}},
			wantOutput: "dev.api\t等待滚动发布",
		},
		{
			// US4 验收场景 1/3（specs/033-deploy-scope-cleanup/spec.md:96,98）：
			// 不指定 --scope 时发送 `-` 通配符跨 scope 列出所有环境，
			// 输出使用响应中的实际完整环境名（而非 `-`），遵循 AIP-159。
			name:     "cross-scope listing",
			scope:    "",
			wantPath: "/v1/deploy/scopes/-/environments",
			status:   http.StatusOK,
			response: &deploy.ListEnvironmentsResponse{Environments: []*deploy.Environment{
				{Name: "deploy/scopes/alice/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY}},
				{Name: "deploy/scopes/bob/environments/prod", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY}},
			}},
			wantOutput: "alice.dev\t就绪\nbob.prod\t就绪",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatalf("method = %s, want GET", r.Method)
				}
				if r.URL.Path != tt.wantPath {
					t.Fatalf("path = %s, want %s", r.URL.Path, tt.wantPath)
				}
				writeDelListJSONResponse(t, w, tt.status, tt.response)
			}))
			t.Cleanup(server.Close)

			_, cwd := newDelListWorkspace(t)
			withWorkingDir(t, cwd)

			gotOutput := captureListStdout(t, func() error {
				return listCommand(context.Background(), &options{scope: tt.scope, endpoint: server.URL, timeout: 50 * time.Millisecond, apiClient: clientpkg.NewClient(server.URL)})
			})
			if strings.TrimSpace(gotOutput) != tt.wantOutput {
				t.Fatalf("listCommand() output = %q, want %q", strings.TrimSpace(gotOutput), tt.wantOutput)
			}
		})
	}
}

// TestNoConfigFileAccess 验证 US1 验收场景 2（specs/033-deploy-scope-cleanup/spec.md）：
// 任意命令（del/describe/list）都不读取或写入 .env/cli.json，预置的 default_scope 配置被忽略。
func TestNoConfigFileAccess(t *testing.T) {
	readyEnv := &deploy.Environment{
		Name: "deploy/scopes/prod/environments/api",
		Status: &deploy.EnvironmentStatus{
			State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY,
		},
	}

	const seededConfig = `{"default_scope":"dev"}`

	tests := []struct {
		name    string
		seed    bool
		run     func(ctx context.Context, opts *options) error
		opts    *options
		handler http.HandlerFunc
	}{
		{
			name: "del does not create config",
			run:  delCommand,
			opts: &options{target: "prod.api", timeout: 50 * time.Millisecond},
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodDelete:
					if r.URL.Path != "/v1/deploy/scopes/prod/environments/api" {
						t.Fatalf("delete path = %s", r.URL.Path)
					}
					w.WriteHeader(http.StatusOK)
				case http.MethodGet:
					if r.URL.Path != "/v1/deploy/scopes/prod/environments/api" {
						t.Fatalf("poll path = %s", r.URL.Path)
					}
					writeDelListJSONResponse(t, w, http.StatusNotFound, map[string]any{"code": 5, "message": "not found"})
				default:
					t.Fatalf("method = %s", r.Method)
				}
			},
		},
		{
			name: "del ignores default_scope config",
			seed: true,
			run:  delCommand,
			opts: &options{target: "prod.api", timeout: 50 * time.Millisecond},
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodDelete:
					if r.URL.Path != "/v1/deploy/scopes/prod/environments/api" {
						t.Fatalf("delete path = %s", r.URL.Path)
					}
					w.WriteHeader(http.StatusOK)
				case http.MethodGet:
					if r.URL.Path != "/v1/deploy/scopes/prod/environments/api" {
						t.Fatalf("poll path = %s", r.URL.Path)
					}
					writeDelListJSONResponse(t, w, http.StatusNotFound, map[string]any{"code": 5, "message": "not found"})
				default:
					t.Fatalf("method = %s", r.Method)
				}
			},
		},
		{
			name:    "describe does not create config",
			run:     describeCommand,
			opts:    &options{target: "prod.api", timeout: 50 * time.Millisecond},
			handler: describeHandler(t, "/v1/deploy/scopes/prod/environments/api", http.StatusOK, readyEnv),
		},
		{
			name:    "describe ignores default_scope config",
			seed:    true,
			run:     describeCommand,
			opts:    &options{target: "prod.api", timeout: 50 * time.Millisecond},
			handler: describeHandler(t, "/v1/deploy/scopes/prod/environments/api", http.StatusOK, readyEnv),
		},
		{
			name: "list does not create config",
			run:  listCommand,
			opts: &options{scope: "prod", timeout: 50 * time.Millisecond},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatalf("method = %s, want GET", r.Method)
				}
				if r.URL.Path != "/v1/deploy/scopes/prod/environments" {
					t.Fatalf("path = %s", r.URL.Path)
				}
				writeDelListJSONResponse(t, w, http.StatusOK, &deploy.ListEnvironmentsResponse{Environments: []*deploy.Environment{readyEnv}})
			},
		},
		{
			name: "list ignores default_scope config",
			seed: true,
			run:  listCommand,
			opts: &options{scope: "prod", timeout: 50 * time.Millisecond},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatalf("method = %s, want GET", r.Method)
				}
				if r.URL.Path != "/v1/deploy/scopes/prod/environments" {
					t.Fatalf("path = %s", r.URL.Path)
				}
				writeDelListJSONResponse(t, w, http.StatusOK, &deploy.ListEnvironmentsResponse{Environments: []*deploy.Environment{readyEnv}})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			t.Cleanup(server.Close)

			root, cwd := newDelListWorkspace(t)
			withWorkingDir(t, cwd)
			if tt.seed {
				writeFile(t, filepath.Join(root, ".env", "cli.json"), seededConfig)
			}

			tt.opts.endpoint = server.URL
			tt.opts.apiClient = clientpkg.NewClient(server.URL)

			if _, err := captureDelListOutput(t, func() error {
				return tt.run(context.Background(), tt.opts)
			}); err != nil {
				t.Fatalf("%s() unexpected error: %v", tt.name, err)
			}

			raw, err := os.ReadFile(filepath.Join(root, ".env", "cli.json"))
			if tt.seed {
				if err != nil {
					t.Fatalf("seeded config missing: %v", err)
				}
				if string(raw) != seededConfig {
					t.Fatalf("seeded config modified: %q", raw)
				}
				return
			}
			if !os.IsNotExist(err) {
				t.Fatalf("config created unexpectedly: %v", err)
			}
		})
	}
}

func newDelListWorkspace(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "MODULE.bazel"), "")
	cwd := filepath.Join(root, "apps", "svc")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	return root, cwd
}

func captureDelListOutput(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := stdout
	var out bytes.Buffer
	stdout = &out
	t.Cleanup(func() { stdout = oldStdout })

	err := fn()
	return out.String(), err
}

func captureListStdout(t *testing.T, fn func() error) string {
	t.Helper()

	oldStdout := stdout
	var out bytes.Buffer
	stdout = &out
	t.Cleanup(func() { stdout = oldStdout })

	callErr := fn()
	if callErr != nil {
		t.Fatalf("call failed: %v", callErr)
	}
	return out.String()
}

func writeDelListJSONResponse(t *testing.T, w http.ResponseWriter, statusCode int, body any) {
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
