package testplan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	"dominion/common/gopkg/testtool"
)

const (
	// headerEnv routes the request into this run's deployment
	// (tools/release/deploy/README.md §环境类型).
	headerEnv = "env"

	// appID is the {app} segment of the test resources; it differs from the
	// deployed app so a run's resources never collide with anything else.
	appID = "redis-demo-test"

	// pathPrefix is the shared HTTPRoute prefix; each language implementation
	// is reached below its own sub-prefix
	// (specs/067-redis-infra-support/contracts/redis-demo.md §1).
	pathPrefix = "/experimental/redis-demo"

	langGo = "go"
	langJs = "js"
)

// kvEntry mirrors the KvEntry JSON body returned by the gateway.
type kvEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// kvEntryName builds a run-unique resource name apps/{app}/kvEntries/{id}
// (AIP-122). The name is also the redis key in both implementations
// (specs/067-redis-infra-support/contracts/redis-demo.md §2), so the tests
// never translate it between resource name and storage key.
func kvEntryName(caseID string) string {
	return fmt.Sprintf("apps/%s/kvEntries/%s-%d", appID, caseID, time.Now().UnixNano())
}

// kvURL builds a kvEntries URL for one language implementation.
func kvURL(baseURL, lang, name string) string {
	return fmt.Sprintf("%s%s/%s/v1/%s", baseURL, pathPrefix, lang, name)
}

// doKvRequest sends one kvEntries request through the public HTTP entry with
// the trace context attached (style/large_test.md §测试用例) and returns the
// status and the raw body.
func doKvRequest(t *testing.T, ctx context.Context, method, url, envName string, body []byte) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext %s %s: %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(headerEnv, envName)

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: tracecontext.NewHTTPTransport(http.DefaultTransport),
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response %s %s: %v", method, url, err)
	}
	return resp.StatusCode, respBody
}

// mustSetKvEntry upserts value at name through the given language; the path
// name overrides the body name field, so only the value travels in the body.
func mustSetKvEntry(t *testing.T, ctx context.Context, baseURL, envName, lang, name, value string) {
	t.Helper()

	body := []byte(fmt.Sprintf(`{"value": %q}`, value))
	status, respBody := doKvRequest(t, ctx, http.MethodPost, kvURL(baseURL, lang, name)+":set", envName, body)
	if status != http.StatusOK {
		t.Fatalf("set %s via %s status = %d, want %d (body: %s)", name, lang, status, http.StatusOK, respBody)
	}
}

// mustGetKvEntry reads name through the given language and returns the decoded
// resource.
func mustGetKvEntry(t *testing.T, ctx context.Context, baseURL, envName, lang, name string) kvEntry {
	t.Helper()

	status, respBody := doKvRequest(t, ctx, http.MethodGet, kvURL(baseURL, lang, name), envName, nil)
	if status != http.StatusOK {
		t.Fatalf("get %s via %s status = %d, want %d (body: %s)", name, lang, status, http.StatusOK, respBody)
	}

	got := new(kvEntry)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("json.Unmarshal(%s): %v", respBody, err)
	}
	return *got
}

// mustDeleteKvEntry deletes name through the given language.
func mustDeleteKvEntry(t *testing.T, ctx context.Context, baseURL, envName, lang, name string) {
	t.Helper()

	status, respBody := doKvRequest(t, ctx, http.MethodDelete, kvURL(baseURL, lang, name), envName, nil)
	if status != http.StatusOK {
		t.Fatalf("delete %s via %s status = %d, want %d (body: %s)", name, lang, status, http.StatusOK, respBody)
	}
}

// traceContext returns a context carrying a W3C trace context and prints the
// trace id for log/trace correlation (style/large_test.md §测试用例).
func traceContext(t *testing.T) context.Context {
	t.Helper()

	ctx := tracecontext.FromEnv(context.Background())
	t.Logf("trace_id: %s", tracecontext.ID(ctx))
	return ctx
}

// TestKvEntryRoundTrip covers write/read: each implementation reads back its
// own write, and a value written by one is readable by the other
// (specs/067-redis-infra-support/contracts/redis-demo.md §4 cases 1–4).
func TestKvEntryRoundTrip(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	tests := []struct {
		name   string
		caseID string
		writer string
		reader string
	}{
		{name: "go write go read", caseID: "go-self", writer: langGo, reader: langGo},
		{name: "js write js read", caseID: "js-self", writer: langJs, reader: langJs},
		{name: "go write js read", caseID: "go-to-js", writer: langGo, reader: langJs},
		{name: "js write go read", caseID: "js-to-go", writer: langJs, reader: langGo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := traceContext(t)
			name := kvEntryName(tt.caseID)
			value := "value-" + tt.caseID

			mustSetKvEntry(t, ctx, sutHostURL, sutEnvName, tt.writer, name, value)
			got := mustGetKvEntry(t, ctx, sutHostURL, sutEnvName, tt.reader, name)

			if got.Name != name {
				t.Errorf("name = %q, want %q", got.Name, name)
			}
			if got.Value != value {
				t.Errorf("value = %q, want %q", got.Value, value)
			}
		})
	}
}

// TestKvEntryDeleteRemovesEntry covers delete: a deleted entry reads back as
// NOT_FOUND through both implementations
// (specs/067-redis-infra-support/contracts/redis-demo.md §4 case 5).
func TestKvEntryDeleteRemovesEntry(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	ctx := traceContext(t)
	name := kvEntryName("delete")
	mustSetKvEntry(t, ctx, sutHostURL, sutEnvName, langGo, name, "to-be-deleted")

	mustDeleteKvEntry(t, ctx, sutHostURL, sutEnvName, langGo, name)

	for _, lang := range []string{langGo, langJs} {
		status, respBody := doKvRequest(t, ctx, http.MethodGet, kvURL(sutHostURL, lang, name), sutEnvName, nil)
		if status != http.StatusNotFound {
			t.Errorf("get deleted %s via %s status = %d, want %d (body: %s)", name, lang, status, http.StatusNotFound, respBody)
		}
	}
}
