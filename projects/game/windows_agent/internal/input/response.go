package input

import (
	"encoding/json"
	"fmt"
)

// ResponseStatus describes the status value written by the helper to stdout.
// MUST match the helper's ResponseStatus type.
type ResponseStatus string

const (
	// StatusOK means the command executed successfully.
	StatusOK ResponseStatus = "ok"
	// StatusError means the command failed validation or execution.
	StatusError ResponseStatus = "error"
)

// Response is the JSON IPC result read from helper stdout for each command.
// MUST match the helper's Response struct exactly:
// {"status":"ok"} or {"status":"error","message":"..."}
type Response struct {
	Status  ResponseStatus `json:"status"`
	Message string         `json:"message,omitempty"`
}

// ParseResponse decodes a JSON-line response from helper stdout.
func ParseResponse(data []byte) (Response, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return Response{}, fmt.Errorf("parse response: %w", err)
	}
	return resp, nil
}
