// Package service provides the proxy orchestration layer between the gRPC
// handler and the domain/runtime implementations.
package service

import (
	"dominion/common/gopkg/solver"
	"dominion/projects/game/proxy/domain"
)

// ProxyService holds the dependencies for proxy operations.
type ProxyService struct {
	OwnerStore       domain.OwnerStore
	OwnerPicker      domain.OwnerPicker
	StatefulResolver solver.StatefulResolver
}

// NewProxyService creates a new ProxyService with the given dependencies.
func NewProxyService(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	statefulResolver solver.StatefulResolver,
) *ProxyService {
	return &ProxyService{
		OwnerStore:       ownerStore,
		OwnerPicker:      ownerPicker,
		StatefulResolver: statefulResolver,
	}
}
