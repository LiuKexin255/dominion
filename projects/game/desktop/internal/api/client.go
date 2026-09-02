package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	game "dominion/projects/game"
	"dominion/projects/game/desktop/internal/trace"

	"google.golang.org/protobuf/encoding/protojson"
)

// Config holds the desktop client configuration.
type Config struct {
	GatewayURL string `json:"gateway_url"`
	Env        string `json:"env,omitempty"`
}

// Client is an HTTP client for the game gateway REST API.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient creates a new Client with the given config.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Transport: trace.NewHTTPTransport()},
	}
}

// url constructs a full URL from the config's GatewayURL and the given path.
// It strips any trailing slash from GatewayURL before appending the path.
func (c *Client) url(path string) string {
	return strings.TrimSuffix(c.cfg.GatewayURL, "/") + path
}

// newRequest builds an http.Request with context, method, path, and body,
// setting common headers (Content-Type, and env if configured).
func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.Env != "" {
		req.Header.Set("env", c.cfg.Env)
	}
	return req, nil
}

// templatePath prefixes a REST path with the template segment. template is
// the Template path segment (e.g. "saolei" — the Template resource name
// "templates/{template}" without the "templates/" prefix; AIP-122, spec
// 031-team-template-mode contracts/api-contract.md §1). It escapes the
// segment so a malicious/garbage template cannot inject path separators.
func templatePath(template, suffix string) string {
	return "/api/v1/templates/" + url.PathEscape(template) + suffix
}

// ListSessions lists sessions under a template via GET to
// /api/v1/templates/{template}/sessions (AIP-132). Query parameters
// page_size and page_token control pagination. The desktop consumes this as
// the read-only session picker data source (contracts/desktop-bridge.md §5).
func (c *Client) ListSessions(ctx context.Context, template string, pageSize int32, pageToken string) (*game.ListSessionsResponse, error) {
	q := url.Values{}
	if pageSize > 0 {
		q.Set("page_size", fmt.Sprintf("%d", pageSize))
	}
	if pageToken != "" {
		q.Set("page_token", pageToken)
	}
	u := &url.URL{Path: templatePath(template, "/sessions"), RawQuery: q.Encode()}

	req, err := c.newRequest(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("list sessions: status %d: %s", resp.StatusCode, string(respBody))
	}

	result := new(game.ListSessionsResponse)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, result); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return result, nil
}
