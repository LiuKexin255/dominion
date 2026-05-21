package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/solver"
)

// HTTPGatewayClient implements GatewayClient using HTTP calls to gateway instances.
type HTTPGatewayClient struct {
	httpClient    *http.Client
	aggregateHost string
	resolver      solver.StatefulResolver
	target        *solver.Target
}

// NewHTTPGatewayClient creates an HTTPGatewayClient that discovers gateway instances
// via the given resolver and target, and uses aggregateHost for connect URL generation.
func NewHTTPGatewayClient(aggregateHost string, resolver solver.StatefulResolver, target *solver.Target) *HTTPGatewayClient {
	return &HTTPGatewayClient{
		httpClient:    new(http.Client),
		aggregateHost: aggregateHost,
		resolver:      resolver,
		target:        target,
	}
}

// AggregateHost returns the public aggregate host for building connect URLs.
func (c *HTTPGatewayClient) AggregateHost() string {
	return c.aggregateHost
}

// InitGameRuntime creates a game runtime on a gateway for the given session.
// It resolves gateway instances, picks the first ready one, and POSTs to the
// CreateGameRuntime endpoint.
func (c *HTTPGatewayClient) InitGameRuntime(ctx context.Context, sessionID string, reconnectGeneration int64) (*InitResult, error) {
	endpoint, err := c.resolveReadyEndpoint(ctx)
	if err != nil {
		return nil, fmt.Errorf("init game runtime: resolve gateway: %w", err)
	}

	body := initRequestBody{ReconnectGeneration: reconnectGeneration}
	reqBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("init game runtime: marshal request: %w", err)
	}

	url := fmt.Sprintf("http://%s/v1/sessions/%s/game/runtime", endpoint, sessionID)
	logs.Info(ctx, "init game runtime", event.String("url", url))

	respBytes, err := c.doPost(ctx, url, reqBytes)
	if err != nil {
		return nil, fmt.Errorf("init game runtime: %w", err)
	}

	var resp initGameRuntimeResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("init game runtime: parse response: %w", err)
	}

	return &InitResult{
		OwnerGatewayID: resp.OwnerGatewayID,
		OwnerEpoch:     resp.OwnerEpoch,
		Token:          resp.Token,
		ExpiresAt:      resp.ExpiresAt,
	}, nil
}

// RefreshGameRuntime refreshes a game runtime on a gateway for the given session.
// It resolves gateway instances, picks the first ready one, and POSTs to the
// RefreshGameRuntime endpoint.
func (c *HTTPGatewayClient) RefreshGameRuntime(ctx context.Context, sessionID string, oldToken string) (*RefreshResult, error) {
	endpoint, err := c.resolveReadyEndpoint(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh game runtime: resolve gateway: %w", err)
	}

	body := refreshRequestBody{OldToken: oldToken}
	reqBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("refresh game runtime: marshal request: %w", err)
	}

	url := fmt.Sprintf("http://%s/v1/sessions/%s/game/runtime:refresh", endpoint, sessionID)
	logs.Info(ctx, "refresh game runtime", event.String("url", url))

	respBytes, err := c.doPost(ctx, url, reqBytes)
	if err != nil {
		return nil, fmt.Errorf("refresh game runtime: %w", err)
	}

	var resp refreshGameRuntimeResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("refresh game runtime: parse response: %w", err)
	}

	return &RefreshResult{
		OwnerGatewayID:      resp.OwnerGatewayID,
		OwnerEpoch:          resp.OwnerEpoch,
		ReconnectGeneration: resp.ReconnectGeneration,
		Token:               resp.Token,
		ExpiresAt:           resp.ExpiresAt,
	}, nil
}

func (c *HTTPGatewayClient) resolveReadyEndpoint(ctx context.Context) (string, error) {
	instances, err := c.resolver.Resolve(ctx, c.target)
	if err != nil {
		return "", err
	}

	for _, inst := range instances {
		if len(inst.Endpoints) > 0 {
			return inst.Endpoints[0], nil
		}
	}

	return "", ErrNoGatewayAvailable
}

func (c *HTTPGatewayClient) doPost(ctx context.Context, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http post %s: status %d: %s", url, resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}

// Request/response types for JSON serialization.
// Field names use snake_case to match proto JSON conventions.

type initRequestBody struct {
	ReconnectGeneration int64 `json:"reconnect_generation"`
}

type initGameRuntimeResponse struct {
	OwnerGatewayID string    `json:"owner_gateway_id"`
	OwnerEpoch     int64     `json:"owner_epoch"`
	Token          string    `json:"token"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type refreshRequestBody struct {
	OldToken string `json:"old_token"`
}

type refreshGameRuntimeResponse struct {
	OwnerGatewayID      string    `json:"owner_gateway_id"`
	OwnerEpoch          int64     `json:"owner_epoch"`
	ReconnectGeneration int64     `json:"reconnect_generation"`
	Token               string    `json:"token"`
	ExpiresAt           time.Time `json:"expires_at"`
}
