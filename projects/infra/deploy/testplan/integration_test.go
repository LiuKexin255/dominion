package deploy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	deployServiceURLEnvVar = "SUT_HOST_URL"
	pendingState           = "ENVIRONMENT_STATE_PENDING"
	reconcilingState       = "ENVIRONMENT_STATE_RECONCILING"
	readyState             = "ENVIRONMENT_STATE_READY"
	pollInterval           = 200 * time.Millisecond
	pollTimeout            = 10 * time.Second
)

type environmentResponse struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	DesiredState desiredStateJSON  `json:"desiredState"`
	Status       environmentStatus `json:"status"`
}

type environmentStatus struct {
	State string `json:"state"`
}

type desiredStateJSON struct {
	Services   []serviceSpecJSON   `json:"services,omitempty"`
	Infras     []infraSpecJSON     `json:"infras,omitempty"`
	HTTPRoutes []httpRouteSpecJSON `json:"httpRoutes,omitempty"`
}

type serviceSpecJSON struct {
	Name       string            `json:"name"`
	App        string            `json:"app"`
	Image      string            `json:"image"`
	Ports      []servicePortJSON `json:"ports,omitempty"`
	Replicas   int               `json:"replicas"`
	TLSEnabled bool              `json:"tlsEnabled"`
}

type servicePortJSON struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

type infraSpecJSON struct {
	Resource           string `json:"resource"`
	Profile            string `json:"profile"`
	Name               string `json:"name"`
	App                string `json:"app"`
	PersistenceEnabled bool   `json:"persistenceEnabled"`
}

type httpRouteSpecJSON struct {
	Hostnames []string            `json:"hostnames,omitempty"`
	Matches   []httpRouteRuleJSON `json:"matches,omitempty"`
}

type httpRouteRuleJSON struct {
	Backend string           `json:"backend"`
	Path    httpPathRuleJSON `json:"path"`
}

type httpPathRuleJSON struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type listEnvironmentsResponse struct {
	Environments  []environmentResponse `json:"environments"`
	NextPageToken string                `json:"nextPageToken"`
}

func serviceURL(t *testing.T) string {
	t.Helper()

	baseURL := strings.TrimRight(os.Getenv(deployServiceURLEnvVar), "/")
	if baseURL == "" {
		t.Skipf("%s is required for manual integration tests", deployServiceURLEnvVar)
	}

	return baseURL
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

func uniqueScope() string {
	return "it" + strconv.FormatInt(time.Now().UnixNano()%100000, 10)
}

func newDesiredStateJSON() desiredStateJSON {
	return desiredStateJSON{
		Services: []serviceSpecJSON{{
			Name:       "api",
			App:        "gateway",
			Image:      "example.com/gateway:v1",
			Ports:      []servicePortJSON{{Name: "http", Port: 8080}},
			Replicas:   1,
			TLSEnabled: false,
		}},
		Infras: []infraSpecJSON{{
			Resource:           "redis",
			Profile:            "cache",
			Name:               "redis-main",
			App:                "gateway",
			PersistenceEnabled: true,
		}},
		HTTPRoutes: []httpRouteSpecJSON{{
			Hostnames: []string{"dev.example.com"},
			Matches: []httpRouteRuleJSON{{
				Backend: "api",
				Path: httpPathRuleJSON{
					Type:  "HTTP_PATH_RULE_TYPE_PATH_PREFIX",
					Value: "/",
				},
			}},
		}},
	}
}

func newUpdatedDesiredStateJSON() desiredStateJSON {
	state := newDesiredStateJSON()
	state.Services[0].Image = "example.com/gateway:v2"
	state.Services[0].Replicas = 2
	return state
}

func doHTTPRequest(t *testing.T, client *http.Client, baseURL, method, path string, bodyValue any) (int, []byte) {
	t.Helper()

	var body io.Reader
	if bodyValue != nil {
		payload, err := json.Marshal(bodyValue)
		if err != nil {
			t.Fatalf("json.Marshal(%T) error = %v", bodyValue, err)
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, baseURL+path, body)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext(%q, %q) error = %v", method, path, err)
	}
	if bodyValue != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Client.Do(%q %q) error = %v", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll(%q %q) error = %v", method, path, err)
	}

	return resp.StatusCode, respBody
}

func decodeJSON(t *testing.T, body []byte, out any) {
	t.Helper()

	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("json.Unmarshal(%T) error = %v, body = %s", out, err, string(body))
	}
}

func assertHTTPStatus(t *testing.T, got, want int, body []byte) {
	t.Helper()

	if got != want {
		t.Fatalf("HTTP status = %d, want %d, body = %s", got, want, string(body))
	}
}

func cleanupEnvironment(t *testing.T, client *http.Client, baseURL, name string) {
	t.Helper()

	statusCode, body := doHTTPRequest(t, client, baseURL, http.MethodDelete, "/v1/"+name, nil)
	if statusCode != http.StatusOK && statusCode != http.StatusNoContent && statusCode != http.StatusNotFound {
		t.Fatalf("cleanup delete status = %d, want 200/204/404, body = %s", statusCode, string(body))
	}
}

func waitForEnvironmentState(t *testing.T, client *http.Client, baseURL, name, want string) environmentResponse {
	t.Helper()

	deadline := time.Now().Add(pollTimeout)
	lastState := ""
	for time.Now().Before(deadline) {
		statusCode, body := doHTTPRequest(t, client, baseURL, http.MethodGet, "/v1/"+name, nil)
		assertHTTPStatus(t, statusCode, http.StatusOK, body)

		environment := environmentResponse{}
		decodeJSON(t, body, &environment)
		lastState = environment.Status.State
		if lastState == want {
			return environment
		}

		time.Sleep(pollInterval)
	}

	t.Fatalf("timed out waiting for %q to reach state %s, last state = %s", name, want, lastState)
	return environmentResponse{}
}

func createEnvironment(t *testing.T, client *http.Client, baseURL, parent, envID, description string, desiredState desiredStateJSON) environmentResponse {
	t.Helper()

	createReq := map[string]any{
		"parent":  parent,
		"envName": envID,
		"environment": map[string]any{
			"description":  description,
			"desiredState": desiredState,
		},
	}

	statusCode, body := doHTTPRequest(t, client, baseURL, http.MethodPost, "/v1/"+parent+"/environments", createReq)
	assertHTTPStatus(t, statusCode, http.StatusOK, body)

	created := environmentResponse{}
	decodeJSON(t, body, &created)
	return created
}

func getEnvironment(t *testing.T, client *http.Client, baseURL, name string) environmentResponse {
	t.Helper()

	statusCode, body := doHTTPRequest(t, client, baseURL, http.MethodGet, "/v1/"+name, nil)
	assertHTTPStatus(t, statusCode, http.StatusOK, body)

	environment := environmentResponse{}
	decodeJSON(t, body, &environment)
	return environment
}

func listEnvironments(t *testing.T, client *http.Client, baseURL, parent string, query url.Values) listEnvironmentsResponse {
	t.Helper()

	path := "/v1/" + parent + "/environments"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	statusCode, body := doHTTPRequest(t, client, baseURL, http.MethodGet, path, nil)
	assertHTTPStatus(t, statusCode, http.StatusOK, body)

	resp := listEnvironmentsResponse{}
	decodeJSON(t, body, &resp)
	return resp
}

func TestIntegration_CreateEnvironmentPersistsDesiredState(t *testing.T) {
	baseURL := serviceURL(t)
	client := newHTTPClient()
	scope := uniqueScope()
	parent := fmt.Sprintf("deploy/scopes/%s", scope)
	envName := fmt.Sprintf("%s/environments/env1", parent)
	desiredState := newDesiredStateJSON()

	t.Cleanup(func() {
		cleanupEnvironment(t, client, baseURL, envName)
	})

	created := createEnvironment(t, client, baseURL, parent, "env1", "integration test env", desiredState)
	if created.Name != envName {
		t.Fatalf("CreateEnvironment() name = %q, want %q", created.Name, envName)
	}
	if created.Status.State != reconcilingState {
		t.Fatalf("CreateEnvironment() state = %q, want %q", created.Status.State, reconcilingState)
	}

	got := waitForEnvironmentState(t, client, baseURL, envName, readyState)
	if got.Name != envName {
		t.Fatalf("GetEnvironment() name = %q, want %q", got.Name, envName)
	}
	if got.Description != "integration test env" {
		t.Fatalf("GetEnvironment() description = %q, want %q", got.Description, "integration test env")
	}
	if !reflect.DeepEqual(got.DesiredState, desiredState) {
		t.Fatalf("GetEnvironment() desired state = %#v, want %#v", got.DesiredState, desiredState)
	}
}

func TestIntegration_GetEnvironment(t *testing.T) {
	baseURL := serviceURL(t)
	client := newHTTPClient()
	scope := uniqueScope()
	parent := fmt.Sprintf("deploy/scopes/%s", scope)
	envName := fmt.Sprintf("%s/environments/env1", parent)

	t.Cleanup(func() {
		cleanupEnvironment(t, client, baseURL, envName)
	})

	createEnvironment(t, client, baseURL, parent, "env1", "get test", newDesiredStateJSON())
	waitForEnvironmentState(t, client, baseURL, envName, readyState)

	got := getEnvironment(t, client, baseURL, envName)
	if got.Name != envName {
		t.Fatalf("GetEnvironment() name = %q, want %q", got.Name, envName)
	}
	if got.Status.State != readyState {
		t.Fatalf("GetEnvironment() state = %q, want %q", got.Status.State, readyState)
	}
}

func TestIntegration_ListEnvironmentsWithPagination(t *testing.T) {
	baseURL := serviceURL(t)
	client := newHTTPClient()
	scope := uniqueScope()
	parent := fmt.Sprintf("deploy/scopes/%s", scope)

	for _, envID := range []string{"alpha", "beta", "gamma"} {
		envName := fmt.Sprintf("%s/environments/%s", parent, envID)
		t.Cleanup(func() {
			cleanupEnvironment(t, client, baseURL, envName)
		})

		createEnvironment(t, client, baseURL, parent, envID, "list test", newDesiredStateJSON())
	}

	firstPage := listEnvironments(t, client, baseURL, parent, url.Values{"pageSize": {"2"}})
	if len(firstPage.Environments) != 2 {
		t.Fatalf("ListEnvironments() first page got %d environments, want 2", len(firstPage.Environments))
	}
	if firstPage.Environments[0].Name != fmt.Sprintf("%s/environments/alpha", parent) {
		t.Fatalf("ListEnvironments() first page item[0] = %q, want %q", firstPage.Environments[0].Name, fmt.Sprintf("%s/environments/alpha", parent))
	}
	if firstPage.Environments[1].Name != fmt.Sprintf("%s/environments/beta", parent) {
		t.Fatalf("ListEnvironments() first page item[1] = %q, want %q", firstPage.Environments[1].Name, fmt.Sprintf("%s/environments/beta", parent))
	}
	if firstPage.NextPageToken == "" {
		t.Fatalf("ListEnvironments() next page token is empty, want non-empty token")
	}

	secondPage := listEnvironments(t, client, baseURL, parent, url.Values{
		"pageSize":  {"2"},
		"pageToken": {firstPage.NextPageToken},
	})
	if len(secondPage.Environments) != 1 {
		t.Fatalf("ListEnvironments() second page got %d environments, want 1", len(secondPage.Environments))
	}
	if secondPage.Environments[0].Name != fmt.Sprintf("%s/environments/gamma", parent) {
		t.Fatalf("ListEnvironments() second page item[0] = %q, want %q", secondPage.Environments[0].Name, fmt.Sprintf("%s/environments/gamma", parent))
	}
	if secondPage.NextPageToken != "" {
		t.Fatalf("ListEnvironments() second page next token = %q, want empty", secondPage.NextPageToken)
	}
}

func TestIntegration_UpdateEnvironmentPersistsDesiredState(t *testing.T) {
	baseURL := serviceURL(t)
	client := newHTTPClient()
	scope := uniqueScope()
	parent := fmt.Sprintf("deploy/scopes/%s", scope)
	envName := fmt.Sprintf("%s/environments/env1", parent)
	updatedDesiredState := newUpdatedDesiredStateJSON()

	t.Cleanup(func() {
		cleanupEnvironment(t, client, baseURL, envName)
	})

	createEnvironment(t, client, baseURL, parent, "env1", "update test", newDesiredStateJSON())

	waitForEnvironmentState(t, client, baseURL, envName, readyState)

	updateReq := map[string]any{
		"environment": map[string]any{
			"name":         envName,
			"desiredState": updatedDesiredState,
		},
		"updateMask": "desiredState",
	}
	statusCode, body := doHTTPRequest(t, client, baseURL, http.MethodPatch, "/v1/"+envName, updateReq)
	assertHTTPStatus(t, statusCode, http.StatusOK, body)

	updated := environmentResponse{}
	decodeJSON(t, body, &updated)
	if updated.Status.State != reconcilingState {
		t.Fatalf("UpdateEnvironment() state = %q, want %q", updated.Status.State, reconcilingState)
	}

	got := waitForEnvironmentState(t, client, baseURL, envName, readyState)
	if !reflect.DeepEqual(got.DesiredState, updatedDesiredState) {
		t.Fatalf("GetEnvironment() desired state = %#v, want %#v", got.DesiredState, updatedDesiredState)
	}
}

func TestIntegration_DeleteEnvironmentRemovesPersistedState(t *testing.T) {
	baseURL := serviceURL(t)
	client := newHTTPClient()
	scope := uniqueScope()
	parent := fmt.Sprintf("deploy/scopes/%s", scope)
	envName := fmt.Sprintf("%s/environments/env1", parent)

	t.Cleanup(func() {
		cleanupEnvironment(t, client, baseURL, envName)
	})

	createEnvironment(t, client, baseURL, parent, "env1", "delete test", newDesiredStateJSON())

	statusCode, body := doHTTPRequest(t, client, baseURL, http.MethodDelete, "/v1/"+envName, nil)
	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		t.Fatalf("DeleteEnvironment() status = %d, want 200 or 204, body = %s", statusCode, string(body))
	}

	statusCode, body = doHTTPRequest(t, client, baseURL, http.MethodGet, "/v1/"+envName, nil)
	assertHTTPStatus(t, statusCode, http.StatusNotFound, body)
}

func TestIntegration_InvalidUpdateDoesNotCorruptPersistence(t *testing.T) {
	baseURL := serviceURL(t)
	client := newHTTPClient()
	scope := uniqueScope()
	parent := fmt.Sprintf("deploy/scopes/%s", scope)
	envName := fmt.Sprintf("%s/environments/env1", parent)
	originalDesiredState := newDesiredStateJSON()

	t.Cleanup(func() {
		cleanupEnvironment(t, client, baseURL, envName)
	})

	createEnvironment(t, client, baseURL, parent, "env1", "stable env", originalDesiredState)
	waitForEnvironmentState(t, client, baseURL, envName, readyState)

	invalidReq := map[string]any{
		"environment": map[string]any{
			"name": envName,
		},
		"updateMask": "desiredState",
	}
	statusCode, body := doHTTPRequest(t, client, baseURL, http.MethodPatch, "/v1/"+envName, invalidReq)
	assertHTTPStatus(t, statusCode, http.StatusBadRequest, body)

	got := getEnvironment(t, client, baseURL, envName)
	if got.Description != "stable env" {
		t.Fatalf("GetEnvironment() description = %q, want %q", got.Description, "stable env")
	}
	if !reflect.DeepEqual(got.DesiredState, originalDesiredState) {
		t.Fatalf("GetEnvironment() desired state = %#v, want %#v", got.DesiredState, originalDesiredState)
	}

	resp := listEnvironments(t, client, baseURL, parent, nil)
	if len(resp.Environments) != 1 {
		t.Fatalf("ListEnvironments() got %d environments, want 1", len(resp.Environments))
	}
	if resp.Environments[0].Name != envName {
		t.Fatalf("ListEnvironments() item[0] = %q, want %q", resp.Environments[0].Name, envName)
	}
}
