package validate

import (
	"fmt"
	"net/url"
	"regexp"

	"dominion/tools/deploy/pkg/config"
	"dominion/tools/deploy/pkg/workspace"
	guitarconfig "dominion/tools/guitar/pkg/config"
)

// endpointNamePattern validates endpoint names: must start with a letter,
// followed by zero or more alphanumeric characters.
var endpointNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)

// envPattern validates suite env format: scope.env (e.g., "game.lt").
var envPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*\.[a-zA-Z][a-zA-Z0-9]*$`)

// Validate performs static validation on the entire testplan config.
// It checks required fields and validates each suite including its deploy config.
func Validate(cfg *guitarconfig.Config) error {
	if cfg.Name == "" {
		return fmt.Errorf("config name is required")
	}
	if len(cfg.Suites) == 0 {
		return fmt.Errorf("config suites must not be empty")
	}
	for i, suite := range cfg.Suites {
		if err := validateSuiteFields(i, suite); err != nil {
			return err
		}
		if err := ValidateSuite(suite); err != nil {
			return err
		}
	}
	return nil
}

// ValidateSuite validates a single suite including deploy config checks.
// It verifies the deploy type is "test" and that http endpoint hostnames
// exist in the deploy's service hostnames.
func ValidateSuite(suite *guitarconfig.Suite) error {
	deployPath := workspace.ResolvePath(suite.Deploy)
	deployCfg, err := config.ParseDeployConfig(deployPath)
	if err != nil {
		return fmt.Errorf("parse deploy config %s: %w", suite.Deploy, err)
	}

	if deployCfg.Type != config.EnvironmentTypeTest {
		return fmt.Errorf("deploy %s type must be %q, got %q", suite.Deploy, config.EnvironmentTypeTest, deployCfg.Type)
	}

	hostnameSet := collectHostnames(deployCfg)

	httpEndpoints, ok := suite.Endpoint["http"]
	if ok {
		for name, rawURL := range httpEndpoints {
			u, err := url.Parse(rawURL)
			if err != nil {
				return fmt.Errorf("endpoint http.%s: invalid URL %q: %w", name, rawURL, err)
			}
			host := u.Hostname()
			if !hostnameSet[host] {
				return fmt.Errorf("endpoint http.%s host %q not found in deploy %s http.hostnames", name, host, suite.Deploy)
			}
		}
	}

	return nil
}

func validateSuiteFields(index int, s *guitarconfig.Suite) error {
	if s.Name == "" {
		return fmt.Errorf("suite[%d]: name is required", index)
	}
	if s.Env == "" {
		return fmt.Errorf("suite[%d]: env is required", index)
	}
	if !envPattern.MatchString(s.Env) {
		return fmt.Errorf("suite[%d]: env %q must be in scope.env format (e.g., game.lt)", index, s.Env)
	}
	if s.Deploy == "" {
		return fmt.Errorf("suite[%d]: deploy is required", index)
	}
	if len(s.Cases) == 0 {
		return fmt.Errorf("suite[%d]: cases must not be empty", index)
	}
	for protocol, endpoints := range s.Endpoint {
		for name := range endpoints {
			if !endpointNamePattern.MatchString(name) {
				return fmt.Errorf("suite[%d]: endpoint[%s]: invalid endpoint name %q, must match ^[a-zA-Z][a-zA-Z0-9]*$", index, protocol, name)
			}
		}
	}
	return nil
}

func collectHostnames(deployCfg *config.DeployConfig) map[string]bool {
	set := make(map[string]bool)
	for _, svc := range deployCfg.Services {
		for _, h := range svc.HTTP.Hostnames {
			set[h] = true
		}
	}
	return set
}
