package solver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrServiceNotFound indicates that the requested deploy resource does not exist.
var ErrServiceNotFound = errors.New("service not found")

// DeployHTTPClient fetches service endpoints from the deploy service via HTTP.
type DeployHTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

const (
	// deployEndpointsNameFormat is the resource name pattern for deploy service endpoints.
	deployEndpointsNameFormat = "deploy/scopes/%s/environments/%s/apps/%s/services/%s/endpoints"
	defaultDeployServiceURL   = "http://infra.liukexin.com"
)

// NewDeployHTTPClient creates an HTTP client for the deploy service.
// baseURL is the scheme+host of the deploy service.
func NewDeployHTTPClient() *DeployHTTPClient {
	return &DeployHTTPClient{
		baseURL:    defaultDeployServiceURL,
		httpClient: new(http.Client),
	}
}

// GetServiceEndpoints calls GET /v1/{name} on the deploy service.
func (c *DeployHTTPClient) GetServiceEndpoints(ctx context.Context, name string) (*ServiceEndpointsInfo, error) {
	url := fmt.Sprintf("%s/v1/%s", c.baseURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get service endpoints: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrServiceNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get service endpoints: status %d: %s", resp.StatusCode, string(body))
	}

	result := new(serviceEndpointsResponse)
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, fmt.Errorf("decode service endpoints: %w", err)
	}

	return &ServiceEndpointsInfo{
		Endpoints: result.Endpoints,
		Ports:     result.Ports,
	}, nil
}

// serviceEndpointsResponse maps the JSON response from the deploy service.
type serviceEndpointsResponse struct {
	Endpoints []string         `json:"endpoints"`
	Ports     map[string]int32 `json:"ports"`
}
