package testtool

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	envKeyName        = "TESTTOOL_ENV"
	endpointKeyPrefix = "TESTTOOL_ENDPOINT_"
	endpointNameExpr  = `^[a-zA-Z][a-zA-Z0-9]*$`
	missingEnvMessage = "missing required env %s"
)

// lookupEnv is the function used to look up environment variables.
// It is a variable to allow injection during tests.
var lookupEnv = os.LookupEnv

var endpointNamePattern = regexp.MustCompile(endpointNameExpr)

// EnvKey returns the environment variable name for the test environment identifier.
func EnvKey() string {
	return envKeyName
}

// EndpointKey returns the environment variable name for the given protocol and endpoint name.
func EndpointKey(protocol, name string) string {
	return endpointKeyPrefix + strings.ToUpper(protocol) + "_" + strings.ToUpper(name)
}

// Env returns the current test environment identifier.
func Env() (string, error) {
	return lookupRequiredEnv(EnvKey())
}

// MustEnv returns the current test environment identifier, panicking if missing.
func MustEnv() string {
	value, err := Env()
	if err != nil {
		panic(err)
	}
	return value
}

// Endpoint returns the URL for the given protocol and endpoint name.
func Endpoint(protocol, name string) (string, error) {
	if !endpointNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid endpoint name %q: must match [a-zA-Z][a-zA-Z0-9]*", name)
	}
	return lookupRequiredEnv(EndpointKey(protocol, name))
}

// MustEndpoint returns the URL for the given protocol and endpoint name, panicking if missing.
func MustEndpoint(protocol, name string) string {
	value, err := Endpoint(protocol, name)
	if err != nil {
		panic(err)
	}
	return value
}

func lookupRequiredEnv(key string) (string, error) {
	value, ok := lookupEnv(key)
	if !ok {
		return "", fmt.Errorf(missingEnvMessage, key)
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf(missingEnvMessage, key)
	}

	return value, nil
}
