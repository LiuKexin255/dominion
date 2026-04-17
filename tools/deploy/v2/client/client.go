// Package client provides a thin HTTP client for the deploy v2 service.
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"dominion/projects/infra/deploy"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

const basePath = "/v1/"

var (
	jsonMarshaler   = protojson.MarshalOptions{EmitUnpopulated: true}
	jsonUnmarshaler = protojson.UnmarshalOptions{}
)

var (
	// ErrNotFound indicates that the requested environment does not exist.
	ErrNotFound = errors.New("deploy client: not found")
	// ErrAlreadyExists indicates that the environment already exists.
	ErrAlreadyExists = errors.New("deploy client: already exists")
	// ErrInternal indicates that the service returned an internal error.
	ErrInternal = errors.New("deploy client: internal error")
	// ErrFailed indicates that the environment reached a failed terminal state.
	ErrFailed = errors.New("deploy client: environment failed")
)

// Client calls the deploy service HTTP gateway.
type Client struct {
	endpoint   string
	httpClient *http.Client
}

// APIError captures an HTTP status with a parsed error message.
type APIError struct {
	StatusCode int
	Message    string
	Err        error
}

// Error implements error.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("deploy client: status %d", e.StatusCode)
	}
	return fmt.Sprintf("deploy client: status %d: %s", e.StatusCode, e.Message)
}

// Unwrap returns the classified sentinel error.
func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewClient creates a deploy service client.
func NewClient(endpoint string) *Client {
	return &Client{
		endpoint:   strings.TrimRight(endpoint, "/"),
		httpClient: &http.Client{},
	}
}

// GetEnvironment fetches an environment by resource name.
func (c *Client) GetEnvironment(ctx context.Context, name string) (*deploy.Environment, error) {
	env := new(deploy.Environment)
	if err := c.doJSON(ctx, http.MethodGet, resourcePath(name), nil, env); err != nil {
		return nil, err
	}
	return env, nil
}

// CreateEnvironment creates a new environment under a parent scope.
func (c *Client) CreateEnvironment(ctx context.Context, parent, envName string, env *deploy.Environment) (*deploy.Environment, error) {
	request := &deploy.CreateEnvironmentRequest{
		Parent:      parent,
		EnvName:     envName,
		Environment: env,
	}

	created := new(deploy.Environment)
	if err := c.doJSON(ctx, http.MethodPost, resourcePath(parent)+"/environments", request, created); err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateEnvironment updates an environment desired state.
func (c *Client) UpdateEnvironment(ctx context.Context, env *deploy.Environment) (*deploy.Environment, error) {
	request := &deploy.UpdateEnvironmentRequest{
		Environment: env,
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
	}

	updated := new(deploy.Environment)
	if err := c.doJSON(ctx, http.MethodPatch, resourcePath(env.Name), request, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteEnvironment deletes an environment by resource name.
func (c *Client) DeleteEnvironment(ctx context.Context, name string) error {
	return c.doJSON(ctx, http.MethodDelete, resourcePath(name), nil, nil)
}

// ListEnvironments lists environments under a parent scope.
func (c *Client) ListEnvironments(ctx context.Context, parent string) ([]*deploy.Environment, error) {
	response := new(deploy.ListEnvironmentsResponse)
	if err := c.doJSON(ctx, http.MethodGet, resourcePath(parent)+"/environments", nil, response); err != nil {
		return nil, err
	}
	if len(response.GetEnvironments()) == 0 {
		return nil, nil
	}
	return response.GetEnvironments(), nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestBody proto.Message, responseBody proto.Message) error {
	var body io.Reader
	if requestBody != nil {
		payload, err := jsonMarshaler.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(payload)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return decodeAPIError(response)
	}
	if responseBody == nil {
		return nil
	}

	responsePayload, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if err := jsonUnmarshaler.Unmarshal(responsePayload, responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func decodeAPIError(response *http.Response) error {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return &APIError{StatusCode: response.StatusCode, Message: response.Status, Err: classifyHTTPError(response.StatusCode)}
	}

	parsed := new(status.Status)
	message := ""
	if len(body) > 0 && jsonUnmarshaler.Unmarshal(body, parsed) == nil {
		message = parsed.GetMessage()
	}
	if message == "" {
		message = strings.TrimSpace(string(body))
	}

	return &APIError{
		StatusCode: response.StatusCode,
		Message:    message,
		Err:        classifyHTTPError(response.StatusCode),
	}
}

func classifyHTTPError(statusCode int) error {
	switch statusCode {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrAlreadyExists
	case http.StatusInternalServerError:
		return ErrInternal
	default:
		return nil
	}
}

func resourcePath(name string) string {
	return basePath + strings.TrimPrefix(name, "/")
}
