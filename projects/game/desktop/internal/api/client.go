package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client is an HTTP client for the game gateway REST API.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient creates a new Client with the given config.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{},
	}
}

// url constructs a full URL from the config's GatewayURL and the given path.
// It strips any trailing slash from GatewayURL before appending the path.
func (c *Client) url(path string) string {
	return strings.TrimSuffix(c.cfg.GatewayURL, "/") + path
}

// setCommonHeaders sets headers common to all requests.
func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.Env != "" {
		req.Header.Set("env", c.cfg.Env)
	}
}

// CreateSession creates a new game session via POST to /api/v1/sessions.
func (c *Client) CreateSession(sessionID string) (*Session, error) {
	body := map[string]string{"session_id": sessionID}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.url("/api/v1/sessions"), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	c.setCommonHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("create session: status %d: %s", resp.StatusCode, string(respBody))
	}

	var session Session
	if err := json.Unmarshal(respBody, &session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &session, nil
}

// GetSession retrieves a session by ID via GET to /api/v1/sessions/{sessionID}.
func (c *Client) GetSession(sessionID string) (*Session, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.url("/api/v1/sessions/"+sessionID), nil)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	c.setCommonHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get session: status %d: %s", resp.StatusCode, string(respBody))
	}

	var session Session
	if err := json.Unmarshal(respBody, &session); err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &session, nil
}

// DeleteSession deletes a session by ID via DELETE to /api/v1/sessions/{sessionID}.
func (c *Client) DeleteSession(sessionID string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, c.url("/api/v1/sessions/"+sessionID), nil)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	c.setCommonHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete session: status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// CreateAgent creates a new agent for a session via POST to /api/v1/sessions/{sessionID}/agent.
func (c *Client) CreateAgent(sessionID string) (*Agent, error) {
	body := map[string]interface{}{"agent": struct{}{}}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.url("/api/v1/sessions/"+sessionID+"/agent"), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	c.setCommonHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("create agent: status %d: %s", resp.StatusCode, string(respBody))
	}

	var agent Agent
	if err := json.Unmarshal(respBody, &agent); err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	return &agent, nil
}

// GetAgent retrieves the agent for a session via GET to /api/v1/sessions/{sessionID}/agent.
func (c *Client) GetAgent(sessionID string) (*Agent, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.url("/api/v1/sessions/"+sessionID+"/agent"), nil)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	c.setCommonHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get agent: status %d: %s", resp.StatusCode, string(respBody))
	}

	var agent Agent
	if err := json.Unmarshal(respBody, &agent); err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	return &agent, nil
}

// DeleteAgent deletes the agent for a session via DELETE to /api/v1/sessions/{sessionID}/agent.
func (c *Client) DeleteAgent(sessionID string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, c.url("/api/v1/sessions/"+sessionID+"/agent"), nil)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	c.setCommonHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete agent: status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
