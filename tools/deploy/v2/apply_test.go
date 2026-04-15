package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	deploy "dominion/projects/infra/deploy"
	"dominion/tools/deploy/pkg/imagepush"
	clientpkg "dominion/tools/deploy/v2/client"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

const (
	serviceAPath = "tools/deploy/v2/compiler/testdata/service-a.yaml"
	serviceBPath = "tools/deploy/v2/compiler/testdata/service-b.yaml"
)

var applyServiceFixtures = map[string]string{
	serviceAPath: strings.Join([]string{
		"name: service-a",
		"app: alpha",
		"desc: service a",
		"artifacts:",
		"  - name: service-a",
		"    type: deployment",
		"    target: :service_a_image",
		"    tls: true",
		"    ports:",
		"      - name: grpc",
		"        port: 50051",
		"      - name: http",
		"        port: 8080",
	}, "\n") + "\n",
	serviceBPath: strings.Join([]string{
		"name: service-b",
		"app: beta",
		"desc: service b",
		"artifacts:",
		"  - name: service-b",
		"    type: deployment",
		"    target: :service_b_image",
		"    ports:",
		"      - name: http",
		"        port: 8080",
	}, "\n") + "\n",
}

func TestApplyCommand(t *testing.T) {
	tests := []struct {
		name              string
		deployYAML        string
		imageOutputs      map[string]*imagepush.PushOutput
		imageErrors       map[string]error
		serverSteps       []applyResponseStep
		pollTimeout       time.Duration
		wantOutputSubstr  string
		wantErrIs         error
		wantErrSubstr     string
		wantRequestCount  int32
		wantNoServerCalls bool
	}{
		{
			name: "new environment creation",
			deployYAML: strings.Join([]string{
				"name: team.dev",
				"desc: alpha env",
				"services:",
				"  - artifact:",
				"      path: //tools/deploy/v2/compiler/testdata/service-a.yaml",
				"      name: service-a",
				"    http:",
				"      hostnames:",
				"        - api.example.com",
				"      matches:",
				"        - backend: grpc",
				"          path:",
				"            type: PathPrefix",
				"            value: /v1",
			}, "\n") + "\n",
			imageOutputs: map[string]*imagepush.PushOutput{
				"//tools/deploy/v2/compiler/testdata:service_a_image_push": {Repository: "registry.example.com/service-a", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			serverSteps: []applyResponseStep{
				{
					method: http.MethodGet,
					path:   "/v1/deploy/scopes/team/environments/dev",
					status: http.StatusNotFound,
					body:   map[string]any{"code": 5, "message": "not found"},
				},
				{
					method: http.MethodPost,
					path:   "/v1/deploy/scopes/team/environments",
					status: http.StatusOK,
					body:   &deploy.Environment{Name: "deploy/scopes/team/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}},
					assertBody: &deploy.CreateEnvironmentRequest{
						Parent:  "deploy/scopes/team",
						EnvName: "dev",
						Environment: &deploy.Environment{
							Description: "alpha env",
							DesiredState: &deploy.EnvironmentDesiredState{
								Services: []*deploy.ServiceSpec{{
									Name:       "service-a",
									App:        "alpha",
									Image:      "registry.example.com/service-a@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
									Replicas:   1,
									TlsEnabled: true,
									Ports: []*deploy.ServicePortSpec{
										{Name: "grpc", Port: 50051},
										{Name: "http", Port: 8080},
									},
								}},
								HttpRoutes: []*deploy.HTTPRouteSpec{{
									Hostnames: []string{"api.example.com"},
									Matches: []*deploy.HTTPRouteRule{{
										Backend: "service-a",
										Path:    &deploy.HTTPPathRule{Type: deploy.HTTPPathRuleType_HTTP_PATH_RULE_TYPE_PATH_PREFIX, Value: "/v1"},
									}},
								}},
							},
						},
					},
				},
				{
					method: http.MethodGet,
					path:   "/v1/deploy/scopes/team/environments/dev",
					status: http.StatusOK,
					body:   &deploy.Environment{Name: "deploy/scopes/team/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY}},
				},
			},
			pollTimeout:      50 * time.Millisecond,
			wantOutputSubstr: "环境 team.dev 已应用，状态: ENVIRONMENT_STATE_READY",
			wantRequestCount: 3,
		},
		{
			name: "environment update",
			deployYAML: strings.Join([]string{
				"name: team.dev",
				"desc: beta env",
				"services:",
				"  - artifact:",
				"      path: //tools/deploy/v2/compiler/testdata/service-b.yaml",
				"      name: service-b",
			}, "\n") + "\n",
			imageOutputs: map[string]*imagepush.PushOutput{
				"//tools/deploy/v2/compiler/testdata:service_b_image_push": {Repository: "registry.example.com/service-b", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			},
			serverSteps: []applyResponseStep{
				{
					method: http.MethodGet,
					path:   "/v1/deploy/scopes/team/environments/dev",
					status: http.StatusOK,
					body:   &deploy.Environment{Name: "deploy/scopes/team/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY}},
				},
				{
					method: http.MethodPatch,
					path:   "/v1/deploy/scopes/team/environments/dev",
					status: http.StatusOK,
					body:   &deploy.Environment{Name: "deploy/scopes/team/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}},
					assertBody: &deploy.UpdateEnvironmentRequest{
						Environment: &deploy.Environment{
							Name: "deploy/scopes/team/environments/dev",
							DesiredState: &deploy.EnvironmentDesiredState{
								Services: []*deploy.ServiceSpec{{
									Name:     "service-b",
									App:      "beta",
									Image:    "registry.example.com/service-b@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
									Replicas: 1,
									Ports:    []*deploy.ServicePortSpec{{Name: "http", Port: 8080}},
								}},
							},
						},
						UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
					},
				},
				{
					method: http.MethodGet,
					path:   "/v1/deploy/scopes/team/environments/dev",
					status: http.StatusOK,
					body:   &deploy.Environment{Name: "deploy/scopes/team/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY}},
				},
			},
			pollTimeout:      50 * time.Millisecond,
			wantOutputSubstr: "环境 team.dev 已应用，状态: ENVIRONMENT_STATE_READY",
			wantRequestCount: 3,
		},
		{
			name:              "yaml parsing failure",
			deployYAML:        "name: team.dev\nservices: [\n",
			imageOutputs:      map[string]*imagepush.PushOutput{},
			pollTimeout:       50 * time.Millisecond,
			wantErrSubstr:     "sequence end token ']' not found",
			wantNoServerCalls: true,
		},
		{
			name: "image push failure",
			deployYAML: strings.Join([]string{
				"name: team.dev",
				"desc: alpha env",
				"services:",
				"  - artifact:",
				"      path: //tools/deploy/v2/compiler/testdata/service-a.yaml",
				"      name: service-a",
			}, "\n") + "\n",
			imageOutputs: map[string]*imagepush.PushOutput{},
			imageErrors: map[string]error{
				"//tools/deploy/v2/compiler/testdata:service_a_image_push": errors.New("push failed"),
			},
			pollTimeout:       50 * time.Millisecond,
			wantErrSubstr:     "push failed",
			wantNoServerCalls: true,
		},
		{
			name: "service failed state",
			deployYAML: strings.Join([]string{
				"name: team.dev",
				"desc: alpha env",
				"services:",
				"  - artifact:",
				"      path: //tools/deploy/v2/compiler/testdata/service-a.yaml",
				"      name: service-a",
			}, "\n") + "\n",
			imageOutputs: map[string]*imagepush.PushOutput{
				"//tools/deploy/v2/compiler/testdata:service_a_image_push": {Repository: "registry.example.com/service-a", Digest: "sha256:cccccccccccccccccccccccccccccccc"},
			},
			serverSteps: []applyResponseStep{
				{method: http.MethodGet, path: "/v1/deploy/scopes/team/environments/dev", status: http.StatusNotFound, body: map[string]any{"code": 5, "message": "not found"}},
				{method: http.MethodPost, path: "/v1/deploy/scopes/team/environments", status: http.StatusOK, body: &deploy.Environment{Name: "deploy/scopes/team/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}}},
				{method: http.MethodGet, path: "/v1/deploy/scopes/team/environments/dev", status: http.StatusOK, body: &deploy.Environment{Name: "deploy/scopes/team/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_FAILED, Message: "image pull failed"}}},
			},
			pollTimeout:      50 * time.Millisecond,
			wantErrIs:        clientpkg.ErrFailed,
			wantErrSubstr:    "image pull failed",
			wantRequestCount: 3,
		},
		{
			name: "timeout scenario",
			deployYAML: strings.Join([]string{
				"name: team.dev",
				"desc: alpha env",
				"services:",
				"  - artifact:",
				"      path: //tools/deploy/v2/compiler/testdata/service-a.yaml",
				"      name: service-a",
			}, "\n") + "\n",
			imageOutputs: map[string]*imagepush.PushOutput{
				"//tools/deploy/v2/compiler/testdata:service_a_image_push": {Repository: "registry.example.com/service-a", Digest: "sha256:dddddddddddddddddddddddddddddddd"},
			},
			serverSteps: []applyResponseStep{
				{method: http.MethodGet, path: "/v1/deploy/scopes/team/environments/dev", status: http.StatusOK, body: &deploy.Environment{Name: "deploy/scopes/team/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY}}},
				{method: http.MethodPatch, path: "/v1/deploy/scopes/team/environments/dev", status: http.StatusOK, body: &deploy.Environment{Name: "deploy/scopes/team/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}}},
				{method: http.MethodGet, path: "/v1/deploy/scopes/team/environments/dev", status: http.StatusOK, body: &deploy.Environment{Name: "deploy/scopes/team/environments/dev", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}}},
			},
			pollTimeout:      15 * time.Millisecond,
			wantErrIs:        context.DeadlineExceeded,
			wantErrSubstr:    "poll until ready",
			wantRequestCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, deployPath := newApplyWorkspace(t, tt.deployYAML)

			server, requestCount := newApplyTestServer(t, tt.serverSteps)
			defer server.Close()

			oldRunner := newImageRunner
			newImageRunner = func() (imagepush.Runner, error) {
				return &fakeImageRunner{outputs: tt.imageOutputs, errs: tt.imageErrors}, nil
			}
			defer func() { newImageRunner = oldRunner }()

			var out bytes.Buffer
			oldStdout := stdout
			stdout = &out
			defer func() { stdout = oldStdout }()

			err := applyCommand(&options{
				target:   deployPath,
				endpoint: server.URL,
				timeout:  tt.pollTimeout,
				scope:    "team",
			})

			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("applyCommand() error = %v, want %v", err, tt.wantErrIs)
				}
			} else if tt.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("applyCommand() error = %v, want substring %q", err, tt.wantErrSubstr)
				}
			} else if err != nil {
				t.Fatalf("applyCommand() unexpected error: %v", err)
			}

			if tt.wantErrIs != nil && tt.wantErrSubstr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrSubstr)) {
				t.Fatalf("applyCommand() error = %v, want substring %q", err, tt.wantErrSubstr)
			}
			if tt.wantOutputSubstr != "" && !strings.Contains(out.String(), tt.wantOutputSubstr) {
				t.Fatalf("applyCommand() output = %q, want substring %q", out.String(), tt.wantOutputSubstr)
			}
			if tt.wantNoServerCalls && requestCount.Load() != 0 {
				t.Fatalf("server request count = %d, want 0", requestCount.Load())
			}
			if tt.wantRequestCount > 0 && requestCount.Load() != tt.wantRequestCount {
				t.Fatalf("server request count = %d, want %d", requestCount.Load(), tt.wantRequestCount)
			}
		})
	}
}

type fakeImageRunner struct {
	outputs map[string]*imagepush.PushOutput
	errs    map[string]error
}

func (r *fakeImageRunner) Run(ctx context.Context, pushTarget string) (*imagepush.PushOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err, ok := r.errs[pushTarget]; ok {
		return nil, err
	}
	if output, ok := r.outputs[pushTarget]; ok {
		return output, nil
	}
	return nil, errors.New("unexpected push target: " + pushTarget)
}

type applyResponseStep struct {
	method     string
	path       string
	status     int
	body       any
	assertBody proto.Message
}

func newApplyTestServer(t *testing.T, steps []applyResponseStep) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	requestCount := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(requestCount.Add(1)) - 1
		if len(steps) == 0 {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if idx >= len(steps) {
			idx = len(steps) - 1
		}

		step := steps[idx]
		if r.Method != step.method {
			t.Fatalf("request %d method = %s, want %s", idx, r.Method, step.method)
		}
		if r.URL.Path != step.path {
			t.Fatalf("request %d path = %s, want %s", idx, r.URL.Path, step.path)
		}

		if step.assertBody != nil {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			assertProtoJSONBody(t, body, step.assertBody)
		}

		writeJSONResponse(t, w, step.status, step.body)
	}))

	return server, requestCount
}

func assertProtoJSONBody(t *testing.T, raw []byte, want proto.Message) {
	t.Helper()

	got := proto.Clone(want)
	proto.Reset(got)
	if err := protojson.Unmarshal(raw, got); err != nil {
		t.Fatalf("protojson.Unmarshal() failed: %v", err)
	}
	if !proto.Equal(got, want) {
		gotRaw, _ := protojson.Marshal(got)
		wantRaw, _ := protojson.Marshal(want)
		t.Fatalf("json body = %s, want %s", gotRaw, wantRaw)
	}
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, statusCode int, body any) {
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

func newApplyWorkspace(t *testing.T, deployYAML string) (string, string) {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile(MODULE.bazel) failed: %v", err)
	}

	for path, content := range applyServiceFixtures {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) failed: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) failed: %v", fullPath, err)
		}
	}
	deployPath := filepath.Join(root, "deploy.yaml")
	if err := os.WriteFile(deployPath, []byte(deployYAML), 0o644); err != nil {
		t.Fatalf("WriteFile(deploy.yaml) failed: %v", err)
	}
	withApplyWorkingDir(t, root)

	return root, deployPath
}

func withApplyWorkingDir(t *testing.T, dir string) {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working dir failed: %v", err)
		}
	})
}
