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
		name       string
		target     string
		handler    http.HandlerFunc
		wantOutput string
		wantErrIs  error
	}{
		{
			name:   "failed with message, no per-service data",
			target: "dev.api",
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
			name:   "ready per-service statuses",
			target: "dev.api",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State:             deploy.EnvironmentState_ENVIRONMENT_STATE_READY,
					Message:           "ready",
					LastReconcileTime: timestamppb.New(lastReconciled),
					Services: []*deploy.ServiceStatus{
						{Name: "gateway", App: "game", Kind: deploy.ServiceKind_SERVICE_KIND_ARTIFACT, State: deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_READY},
						{Name: "mongo", App: "game", Kind: deploy.ServiceKind_SERVICE_KIND_INFRA, State: deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_READY},
					},
				},
				DesiredState: &deploy.EnvironmentDesiredState{
					Artifacts: []*deploy.ArtifactSpec{{Name: "gateway", App: "game"}},
					Infras:    []*deploy.InfraSpec{{Name: "mongo", App: "game", Resource: "mongodb"}},
				},
			}),
			wantOutput: `环境 dev.api
状态: 就绪
服务:
  - gateway (app=game) [artifact] 就绪
  - mongo (app=game) [infra: mongodb] 就绪
最近调和: 2026-08-02T10:30:00Z
最近成功: -`,
		},
		{
			name:   "waiting per-service status",
			target: "dev.api",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State:   deploy.EnvironmentState_ENVIRONMENT_STATE_WAITING_ROLLOUT,
					Message: `service "gateway" rollout waiting`,
					Services: []*deploy.ServiceStatus{
						{Name: "gateway", App: "game", Kind: deploy.ServiceKind_SERVICE_KIND_ARTIFACT, State: deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_WAITING, Message: "可用副本不足（available: 0/1）"},
					},
				},
				DesiredState: &deploy.EnvironmentDesiredState{
					Artifacts: []*deploy.ArtifactSpec{{Name: "gateway", App: "game"}},
				},
			}),
			wantOutput: `环境 dev.api
状态: 等待滚动发布
服务:
  - gateway (app=game) [artifact] 等待发布: 可用副本不足（available: 0/1）
最近调和: -
最近成功: -`,
		},
		{
			name:   "failed per-service status",
			target: "dev.api",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State:   deploy.EnvironmentState_ENVIRONMENT_STATE_FAILED,
					Message: `service "gateway" rollout failed`,
					Services: []*deploy.ServiceStatus{
						{Name: "gateway", App: "game", Kind: deploy.ServiceKind_SERVICE_KIND_ARTIFACT, State: deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_FAILED, Message: "ImagePullBackOff"},
					},
				},
				DesiredState: &deploy.EnvironmentDesiredState{
					Artifacts: []*deploy.ArtifactSpec{{Name: "gateway", App: "game"}},
				},
			}),
			wantOutput: `环境 dev.api
状态: 失败
服务:
  - gateway (app=game) [artifact] 失败: ImagePullBackOff
最近调和: -
最近成功: -`,
		},
		{
			name:   "pending per-service status",
			target: "dev.api",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State:   deploy.EnvironmentState_ENVIRONMENT_STATE_WAITING_ROLLOUT,
					Message: "service rollout pending",
					Services: []*deploy.ServiceStatus{
						{Name: "mongo", App: "game", Kind: deploy.ServiceKind_SERVICE_KIND_INFRA, State: deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_PENDING},
					},
				},
				DesiredState: &deploy.EnvironmentDesiredState{
					Infras: []*deploy.InfraSpec{{Name: "mongo", App: "game", Resource: "mongodb"}},
				},
			}),
			wantOutput: `环境 dev.api
状态: 等待滚动发布
服务:
  - mongo (app=game) [infra: mongodb] 已提交，等待观测
最近调和: -
最近成功: -`,
		},
		{
			name:   "no per-service data with message",
			target: "dev.api",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State:   deploy.EnvironmentState_ENVIRONMENT_STATE_FAILED,
					Message: "retry count exhausted",
				},
				DesiredState: &deploy.EnvironmentDesiredState{
					Artifacts: []*deploy.ArtifactSpec{{Name: "gateway", App: "game"}},
				},
			}),
			wantOutput: `环境 dev.api
状态: 失败
说明: retry count exhausted
服务:
  - gateway (app=game) [artifact]
最近调和: -
最近成功: -`,
		},
		{
			name:   "no per-service data without message",
			target: "dev.api",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING,
				},
				DesiredState: &deploy.EnvironmentDesiredState{
					Artifacts: []*deploy.ArtifactSpec{{Name: "gateway", App: "game"}},
				},
			}),
			wantOutput: `环境 dev.api
状态: 部署中
服务:
  - gateway (app=game) [artifact]
最近调和: -
最近成功: -`,
		},
		{
			name:   "unspecified service rollout state no append",
			target: "dev.api",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING,
					Services: []*deploy.ServiceStatus{
						{Name: "gateway", App: "game", Kind: deploy.ServiceKind_SERVICE_KIND_ARTIFACT, State: deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_UNSPECIFIED},
					},
				},
				DesiredState: &deploy.EnvironmentDesiredState{
					Artifacts: []*deploy.ArtifactSpec{{Name: "gateway", App: "game"}},
				},
			}),
			wantOutput: `环境 dev.api
状态: 部署中
服务:
  - gateway (app=game) [artifact]
最近调和: -
最近成功: -`,
		},
		{
			name:   "service matched by kind triple only",
			target: "dev.api",
			handler: describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusOK, &deploy.Environment{
				Name: "deploy/scopes/dev/environments/api",
				Status: &deploy.EnvironmentStatus{
					State: deploy.EnvironmentState_ENVIRONMENT_STATE_WAITING_ROLLOUT,
					Services: []*deploy.ServiceStatus{
						{Name: "gateway", App: "game", Kind: deploy.ServiceKind_SERVICE_KIND_INFRA, State: deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_READY},
					},
				},
				DesiredState: &deploy.EnvironmentDesiredState{
					Artifacts: []*deploy.ArtifactSpec{{Name: "gateway", App: "game"}},
					Infras:    []*deploy.InfraSpec{{Name: "gateway", App: "game", Resource: "mongodb"}},
				},
			}),
			wantOutput: `环境 dev.api
状态: 等待滚动发布
服务:
  - gateway (app=game) [artifact]
  - gateway (app=game) [infra: mongodb] 就绪
最近调和: -
最近成功: -`,
		},
		{
			name:   "ready minimal",
			target: "dev.api",
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
			target: "dev.api",
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
			target: "dev.api",
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
			target: "dev.api",
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
			target: "dev.api",
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
			name:       "environment not found",
			target:     "dev.api",
			handler:    describeHandler(t, "/v1/deploy/scopes/dev/environments/api", http.StatusNotFound, map[string]any{"code": 5, "message": "not found"}),
			wantOutput: `环境 dev.api 不存在`,
			wantErrIs:  clientpkg.ErrNotFound,
		},
		{
			// US3 验收场景 1（specs/033-deploy-scope-cleanup/spec.md:76）：
			// 短名（无点号）须被拒绝并说明需要完整 {scope}.{env_name} 格式，
			// 且不得发起任何 HTTP 调用。
			name:   "short name rejected",
			target: "dev",
			handler: func(w http.ResponseWriter, r *http.Request) {
				t.Fatalf("short name should be rejected before any HTTP call")
			},
			wantErrIs: errInvalidFullEnvName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			t.Cleanup(server.Close)

			_, cwd := newDelListWorkspace(t)
			withWorkingDir(t, cwd)

			gotOutput, err := captureDelListOutput(t, func() error {
				return describeCommand(context.Background(), &options{target: tt.target, endpoint: server.URL, timeout: 50 * time.Millisecond, apiClient: clientpkg.NewClient(server.URL)})
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
