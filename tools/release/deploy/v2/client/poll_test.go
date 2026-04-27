package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	deploy "dominion/projects/infra/deploy"
)

func TestPollUntilReady(t *testing.T) {
	tests := []struct {
		name          string
		statuses      []int
		responses     []any
		wantState     deploy.EnvironmentState
		wantErrIs     error
		wantErrSubstr string
	}{
		{
			name:      "quickly reaches ready",
			statuses:  []int{http.StatusOK, http.StatusOK},
			responses: []any{&deploy.Environment{Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}}, &deploy.Environment{Name: "deploy/scopes/dev/environments/api", Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_READY}}},
			wantState: deploy.EnvironmentState_ENVIRONMENT_STATE_READY,
		},
		{
			name:          "reaches failed",
			statuses:      []int{http.StatusOK, http.StatusOK},
			responses:     []any{&deploy.Environment{Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}}, &deploy.Environment{Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_FAILED, Message: "image pull failed"}}},
			wantErrIs:     ErrFailed,
			wantErrSubstr: "image pull failed",
		},
		{
			name:          "timeout",
			statuses:      []int{http.StatusOK, http.StatusOK, http.StatusOK},
			responses:     []any{&deploy.Environment{Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}}, &deploy.Environment{Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}}, &deploy.Environment{Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING}}},
			wantErrIs:     context.DeadlineExceeded,
			wantErrSubstr: "poll until ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newPollingClient(t, tt.statuses, tt.responses)

			got, err := PollUntilReady(context.Background(), client, "deploy/scopes/dev/environments/api", 5*time.Millisecond, 20*time.Millisecond)
			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("PollUntilReady() error = %v, want %v", err, tt.wantErrIs)
				}
				if tt.wantErrSubstr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrSubstr)) {
					t.Fatalf("PollUntilReady() error = %v, want substring %q", err, tt.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PollUntilReady() unexpected error: %v", err)
			}
			if got == nil || got.Status == nil || got.Status.State != tt.wantState {
				t.Fatalf("PollUntilReady() = %#v, want state %q", got, tt.wantState)
			}
		})
	}
}

func TestPollUntilDeleted(t *testing.T) {
	tests := []struct {
		name          string
		statuses      []int
		responses     []any
		wantErrIs     error
		wantErrSubstr string
	}{
		{
			name:      "success when resource becomes not found",
			statuses:  []int{http.StatusOK, http.StatusNotFound},
			responses: []any{&deploy.Environment{Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_DELETING}}, map[string]any{"code": 5, "message": "environment not found"}},
		},
		{
			name:          "timeout",
			statuses:      []int{http.StatusOK, http.StatusOK, http.StatusOK},
			responses:     []any{&deploy.Environment{Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_DELETING}}, &deploy.Environment{Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_DELETING}}, &deploy.Environment{Status: &deploy.EnvironmentStatus{State: deploy.EnvironmentState_ENVIRONMENT_STATE_DELETING}}},
			wantErrIs:     context.DeadlineExceeded,
			wantErrSubstr: "poll until deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newPollingClient(t, tt.statuses, tt.responses)

			err := PollUntilDeleted(context.Background(), client, "deploy/scopes/dev/environments/api", 5*time.Millisecond, 20*time.Millisecond)
			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("PollUntilDeleted() error = %v, want %v", err, tt.wantErrIs)
				}
				if tt.wantErrSubstr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrSubstr)) {
					t.Fatalf("PollUntilDeleted() error = %v, want substring %q", err, tt.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PollUntilDeleted() unexpected error: %v", err)
			}
		})
	}
}

func newPollingClient(t *testing.T, statuses []int, responses []any) *Client {
	t.Helper()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(calls.Add(1)) - 1
		if index >= len(statuses) {
			index = len(statuses) - 1
		}

		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v1/deploy/scopes/dev/environments/api" {
			t.Fatalf("path = %s, want %s", r.URL.Path, "/v1/deploy/scopes/dev/environments/api")
		}

		writeJSONResponse(t, w, statuses[index], responses[index])
	}))

	t.Cleanup(server.Close)

	client := NewClient(server.URL)
	client.httpClient = server.Client()
	return client
}
