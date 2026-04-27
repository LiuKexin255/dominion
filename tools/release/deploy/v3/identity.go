package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	errNoDefaultScope     = errors.New("没有默认 scope")
	errInvalidScope       = errors.New("非法 scope")
	errInvalidEnvName     = errors.New("非法环境名")
	errInvalidFullEnvName = errors.New("非法完整环境名")

	envPartRegexp = regexp.MustCompile(`^[a-z][a-z0-9]{0,7}$`)
	fullEnvRegexp = regexp.MustCompile(`^[a-z][a-z0-9]{0,7}\.[a-z][a-z0-9]{0,7}$`)
)

func NewFullEnvName(scope, name string) (string, error) {
	if IsFullEnvName(name) {
		if err := ValidateFullEnvName(name); err != nil {
			return "", err
		}
		return name, nil
	}

	if scope == "" {
		return "", errNoDefaultScope
	}
	if err := ValidateScope(scope); err != nil {
		return "", err
	}
	if err := validateEnvName(name); err != nil {
		return "", err
	}

	return scope + "." + name, nil
}

func IsFullEnvName(name string) bool {
	return strings.Contains(name, ".")
}

func ValidateScope(scope string) error {
	if !envPartRegexp.MatchString(scope) {
		return fmt.Errorf("%w: %q", errInvalidScope, scope)
	}
	return nil
}

func ValidateFullEnvName(name string) error {
	if !fullEnvRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q", errInvalidFullEnvName, name)
	}
	return nil
}

func ParseFullEnvName(name string) (scope, envName string, err error) {
	if err := ValidateFullEnvName(name); err != nil {
		return "", "", err
	}

	scope, envName, _ = strings.Cut(name, ".")
	return scope, envName, nil
}

func validateEnvName(name string) error {
	if !envPartRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q", errInvalidEnvName, name)
	}
	return nil
}
