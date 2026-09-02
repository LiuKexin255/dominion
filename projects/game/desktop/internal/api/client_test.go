package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestClient_ListSessions
// ---------------------------------------------------------------------------

func TestClient_ListSessions(t *testing.T) {
	tests := []struct {
		name       string
		template   string
		pageSize   int32
		pageToken  string
		statusCode int
		respBody   string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "success with page_size and page_token",
			template:   "saolei",
			pageSize:   10,
			pageToken:  "token1",
			statusCode: http.StatusOK,
			respBody:   `{"sessions":[{"name":"templates/saolei/sessions/s1","createTime":"2024-01-01T00:00:00Z"},{"name":"templates/saolei/sessions/s2","createTime":"2024-01-02T00:00:00Z"}],"nextPageToken":"next"}`,
			wantErr:    false,
		},
		{
			name:       "success with page_size only",
			template:   "saolei",
			pageSize:   5,
			statusCode: http.StatusOK,
			respBody:   `{"sessions":[],"nextPageToken":""}`,
			wantErr:    false,
		},
		{
			name:       "success with no parameters",
			template:   "saolei",
			statusCode: http.StatusOK,
			respBody:   `{"sessions":[{"name":"templates/saolei/sessions/s1","createTime":"2024-01-01T00:00:00Z"}]}`,
			wantErr:    false,
		},
		{
			name:       "server error",
			template:   "saolei",
			pageSize:   10,
			statusCode: http.StatusInternalServerError,
			respBody:   "internal error",
			wantErr:    true,
			wantErrMsg: "list sessions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				wantPath := "/api/v1/templates/" + tt.template + "/sessions"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}

				if tt.pageSize > 0 {
					if got := r.URL.Query().Get("page_size"); got != fmtInt32(tt.pageSize) {
						t.Errorf("expected page_size %d, got %s", tt.pageSize, got)
					}
				}
				if tt.pageToken != "" {
					if got := r.URL.Query().Get("page_token"); got != tt.pageToken {
						t.Errorf("expected page_token %q, got %q", tt.pageToken, got)
					}
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			resp, err := client.ListSessions(context.Background(), tt.template, tt.pageSize, tt.pageToken)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error should contain %q, got %q", tt.wantErrMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected response, got nil")
			}
			if tt.pageToken == "token1" {
				if resp.GetNextPageToken() != "next" {
					t.Errorf("expected next_page_token %q, got %q", "next", resp.GetNextPageToken())
				}
				if len(resp.GetSessions()) != 2 {
					t.Errorf("expected 2 sessions, got %d", len(resp.GetSessions()))
				}
				if resp.GetSessions()[0].GetName() != "templates/saolei/sessions/s1" {
					t.Errorf("expected first name %q, got %q", "templates/saolei/sessions/s1", resp.GetSessions()[0].GetName())
				}
			}
			if tt.pageSize == 5 {
				if len(resp.GetSessions()) != 0 {
					t.Errorf("expected 0 sessions, got %d", len(resp.GetSessions()))
				}
			}
		})
	}
}

// fmtInt32 formats an int32 as a string without importing fmt.
func fmtInt32(v int32) string {
	return fmt.Sprintf("%d", v)
}

// ---------------------------------------------------------------------------
// TestClient_URLTrailingSlash
// ---------------------------------------------------------------------------

func TestClient_URLTrailingSlash(t *testing.T) {
	// given: config with trailing slash in GatewayURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "//") {
			t.Errorf("URL path contains double slash: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"sessions":[]}`))
	}))
	defer srv.Close()

	client := NewClient(Config{GatewayURL: srv.URL + "/"})

	// when: make a request
	resp, err := client.ListSessions(context.Background(), "saolei", 0, "")

	// then: no double slash, request succeeds
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestClient_EnvHeader
// ---------------------------------------------------------------------------

func TestClient_EnvHeader(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		wantEnv string
	}{
		{
			name:    "env header set",
			env:     "production",
			wantEnv: "production",
		},
		{
			name:    "env header empty",
			env:     "",
			wantEnv: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.Header.Get("env")
				if got != tt.wantEnv {
					t.Errorf("expected env header %q, got %q", tt.wantEnv, got)
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"sessions":[]}`))
			}))
			defer srv.Close()

			client := NewClient(Config{
				GatewayURL: srv.URL,
				Env:        tt.env,
			})

			// when
			_, err := client.ListSessions(context.Background(), "saolei", 0, "")

			// then
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_URLTrailingSlash
// ---------------------------------------------------------------------------
