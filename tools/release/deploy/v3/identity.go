package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	errInvalidFullEnvName = errors.New("非法完整环境名，须使用 {scope}.{env_name} 格式")

	envPartRegexp = regexp.MustCompile(`^[a-z][a-z0-9]{0,7}$`)
	fullEnvRegexp = regexp.MustCompile(`^[a-z][a-z0-9]{0,7}\.[a-z][a-z0-9]{0,7}$`)
)

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
