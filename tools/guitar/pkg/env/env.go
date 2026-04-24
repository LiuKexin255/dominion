package env

import (
	"strings"

	"dominion/tools/guitar/pkg/config"
)

const (
	// envKeyPrefix is the prefix for the suite environment variable.
	envKeyPrefix = "TESTTOOL_ENV"
	// endpointKeyPrefix is the prefix for endpoint environment variables.
	endpointKeyPrefix = "TESTTOOL_ENDPOINT_"
	// dominionEnvironmentKey is the env var for dominion environment identity.
	dominionEnvironmentKey = "DOMINION_ENVIRONMENT"
)

// BuildEnvVars generates the environment variable map for a suite.
func BuildEnvVars(suite *config.Suite) map[string]string {
	envVars := map[string]string{
		envKeyPrefix:           suite.Env,
		dominionEnvironmentKey: suite.Env,
	}

	for protocol, endpoints := range suite.Endpoint {
		for name, url := range endpoints {
			key := endpointKeyPrefix + strings.ToUpper(protocol) + "_" + strings.ToUpper(name)
			envVars[key] = url
		}
	}

	return envVars
}

// BuildTestEnvFlags generates bazel test --test_env flags for a suite.
func BuildTestEnvFlags(suite *config.Suite) []string {
	envVars := BuildEnvVars(suite)
	if len(envVars) == 0 {
		return nil
	}

	flags := make([]string, 0, len(envVars))
	for key, value := range envVars {
		flags = append(flags, "--test_env="+key+"="+value)
	}

	return flags
}
