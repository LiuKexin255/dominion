// Package domain contains the deploy service domain model.
package domain

// EnvironmentType describes the intended usage category of an environment.
type EnvironmentType int

const (
	// EnvironmentTypeUnspecified indicates that no environment type has been assigned.
	EnvironmentTypeUnspecified EnvironmentType = 0
	// EnvironmentTypeProd indicates that the environment is a production environment.
	EnvironmentTypeProd EnvironmentType = 1
	// EnvironmentTypeTest indicates that the environment is a test environment.
	EnvironmentTypeTest EnvironmentType = 2
	// EnvironmentTypeDev indicates that the environment is a development environment.
	EnvironmentTypeDev EnvironmentType = 3
)
