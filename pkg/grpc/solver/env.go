package solver

import (
	"fmt"
	"os"
	"strings"
)

const (
	// dominionAppEnvKey stores the current dominion app name.
	dominionAppEnvKey = "DOMINION_APP"
	// dominionEnvironmentEnvKey stores the current dominion environment name.
	dominionEnvironmentEnvKey = "DOMINION_ENVIRONMENT"
	// podNamespaceEnvKey stores the current pod namespace.
	podNamespaceEnvKey = "POD_NAMESPACE"
)

// Environment captures the runtime values used by dominion resolver lookup.
type Environment struct {
	Name      string
	App       string
	Namespace string
}

// EnvLoader loads runtime environment data.
type EnvLoader interface {
	Load(target *Target) (*Environment, error)
}

// OSEnvLoader loads runtime environment data from process env vars.
type OSEnvLoader struct{}

// Load returns the current runtime environment from process env vars.
func (*OSEnvLoader) Load(target *Target) (*Environment, error) {
	return LoadEnvironment(target)
}

// LoadEnvironment returns the runtime environment required by the resolver.
func LoadEnvironment(target *Target) (*Environment, error) {
	return loadEnvironment(target, os.LookupEnv)
}

// loadEnvironment loads and validates runtime environment using the provided lookup.
func loadEnvironment(target *Target, lookupEnv func(string) (string, bool)) (*Environment, error) {
	app, err := lookupRequiredEnv(lookupEnv, dominionAppEnvKey)
	if err != nil {
		return nil, err
	}

	if target.App != app {
		return nil, fmt.Errorf("target app %q does not match %s %q", target.App, dominionAppEnvKey, app)
	}

	environmentName, err := lookupRequiredEnv(lookupEnv, dominionEnvironmentEnvKey)
	if err != nil {
		return nil, err
	}

	namespace, err := lookupRequiredEnv(lookupEnv, podNamespaceEnvKey)
	if err != nil {
		return nil, err
	}

	return &Environment{
		Name:      environmentName,
		App:       app,
		Namespace: namespace,
	}, nil
}

// lookupRequiredEnv reads a required env var and rejects missing or blank values.
func lookupRequiredEnv(lookupEnv func(string) (string, bool), key string) (string, error) {
	value, ok := lookupEnv(key)
	if !ok {
		return "", fmt.Errorf("missing required env %s", key)
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("missing required env %s", key)
	}

	return value, nil
}
