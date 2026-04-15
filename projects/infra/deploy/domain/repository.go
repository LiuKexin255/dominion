// Package domain contains the deploy service domain model and repository contract.
package domain

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
)

// Repository defines persistence operations for environments.
type Repository interface {
	// Get retrieves an environment by name.
	Get(ctx context.Context, name EnvironmentName) (*Environment, error)

	// ListByStates lists environments matching any of the provided states.
	ListByStates(ctx context.Context, states ...EnvironmentState) ([]*Environment, error)

	// ListByScope lists environments under a scope with pagination.
	// It returns the matching environments, the next page token, and an error.
	ListByScope(ctx context.Context, scope string, pageSize int32, pageToken string) ([]*Environment, string, error)

	// Save persists an environment, creating or updating it as needed.
	Save(ctx context.Context, env *Environment) error

	// Delete removes an environment by name.
	Delete(ctx context.Context, name EnvironmentName) error
}

// EncodePageToken encodes an offset into a page token string.
func EncodePageToken(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

// DecodePageToken decodes a page token string into an offset.
func DecodePageToken(token string) (int, error) {
	if token == "" {
		return 0, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return 0, errors.New("invalid page token")
	}

	offset, err := strconv.Atoi(string(decoded))
	if err != nil {
		return 0, errors.New("invalid page token")
	}

	return offset, nil
}
