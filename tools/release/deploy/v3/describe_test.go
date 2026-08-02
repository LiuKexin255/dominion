package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	deploy "dominion/projects/infra/deploy"
	clientpkg "dominion/tools/release/deploy/v2/client"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDescribeCommand(t *testing.T) {
	lastReconciled := time.Date(2026, 8, 2, 10, 30, 0, 0, time.UTC)
	lastSuccess := time.Date(2026, 8, 2, 10, 20, 0, 0, time.UTC)

	tests := []struct {
		name         string
		target       string
		scope        string
		defaultScope string
		handler      http.HandlerFunc
		wantOutput   string
		wantErrIs    error
	}{
		{
			name:   "failed with message and services",
			target: "api",
			scope:  "dev",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State:             deploy.EnvironmentState_ENVIRONMENT_STATE_FAILED,
					Message:           `service "gateway" rollout failed: ImagePullBackOff`,
					LastReconcileTime: timestamppb.New(lastReconciled),
				},
				DesiredState: &deploy.EnvironmentDesiredState{
					Artifacts: []*deploy.ArtifactSpec{{Name: "gateway", App: "game"}},
					Infras:    []*deploy.InfraSpec{{Name: "mongo", App: "game", Resource: "mongodb"}},
				},
			}),
			wantOutput: `环境 dev.api
状态: 失败
说明: service "gateway" rollout failed: ImagePullBackOff
服务:
  - gateway (app=game) [artifact]
  - mongo (app=game) [infra: mongodb]
最近调和: 2026-08-02T10:30:00Z
最近成功: -`,
		},
		{
			name:   "ready minimal",
			target: "api",
			scope:  "dev",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY,
				},
			}),
			wantOutput: `环境 dev.api
状态: 就绪
服务: （无）
最近调和: -
最近成功: -`,
		},
		{
			name:   "pending with timestamps",
			target: "api",
			scope:  "dev",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State:             deploy.EnvironmentState_ENVIRONMENT_STATE_PENDING,
					LastReconcileTime: timestamppb.New(lastReconciled),
					LastSuccessTime:   timestamppb.New(lastSuccess),
				},
			}),
			wantOutput: `环境 dev.api
状态: 等待中
服务: （无）
最近调和: 2026-08-02T10:30:00Z
最近成功: 2026-08-02T10:20:00Z`,
		},
		{
			name:   "waiting rollout",
			target: "api",
			scope:  "dev",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State: deploy.EnvironmentState_ENVIRONMENT_STATE_WAITING_ROLLOUT,
				},
			}),
			wantOutput: `环境 dev.api
状态: 等待滚动发布
服务: （无）
最近调和: -
最近成功: -`,
		},
		{
			name:   "deleting",
			target: "api",
			scope:  "dev",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State: deploy.EnvironmentState_ENVIRONMENT_STATE_DELETING,
				},
			}),
			wantOutput: `环境 dev.api
状态: 删除中
服务: （无）
最近调和: -
最近成功: -`,
		},
		{
			name:   "unspecified state",
			target: "api",
			scope:  "dev",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State: deploy.EnvironmentState_ENVIRONMENT_STATE_UNSPECIFIED,
				},
			}),
			wantOutput: `环境 dev.api
状态: 未知
服务: （无）
最近调和: -
最近成功: -`,
		},
		{
			name:         "default scope from config",
			target:       "api",
			defaultScope: "dev",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY,
				},
			}),
			wantOutput: `环境 dev.api
状态: 就绪
服务: （无）
最近调和: -
最近成功: -`,
		},
		{
			name:       "environment not found",
			target:     "dev.api",
			handler:    describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusNotFound, map[string]any{"code": 5, "message": "not found"}),
			wantOutput: `环境 dev.api 不存在`,
			wantErrIs:  clientpkg.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			t.Cleanup(server.Close)

			root, cwd := newDelListWorkspace(t)
			withWorkingDir(t, cwd)
			if tt.defaultScope != "" {
				if err := saveConfig(root, &cliConfig{DefaultScope: tt.defaultScope}); err != nil {
					t.Fatalf("saveConfig() failed: %v", err)
				}
			}

			gotOutput, err := captureDelListOutput(t, func() error {
				return describeCommand(context.Background(), &options{target: tt.target, scope: tt.scope, endpoint: server.URL, timeout: 50 * time.Millisecond, apiClient: clientpkg.NewClient(server.URL)})
			})

			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("describeCommand() error = %v, want %v", err, tt.wantErrIs)
				}
			} else {
				if err != nil {
					t.Fatalf("describeCommand() unexpected error: %v", err)
				}
			}
			if strings.TrimSpace(gotOutput) != tt.wantOutput {
				t.Fatalf("describeCommand() output = %q, want %q", strings.TrimSpace(gotOutput), tt.wantOutput)
			}
		})
	}
}

// describeHandler 构造 describe 的 HTTP 桩：校验 GET 方法与路径，返回给定响应体。
func describeHandler(t *testing.T, path string, statusCode int, body any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != path {
			t.Fatalf("path = %s, want %s", r.URL.Path, path)
		}
		writeDelListJSONResponse(t, w, statusCode, body)
	}
}
