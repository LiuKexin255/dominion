package env

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	// envPartPattern 定义 scope 与环境名片段的合法格式。
	envPartPattern = "^[a-z][a-z0-9]{0,7}$"
	// fullEnvPattern 定义完整环境名的合法格式。
	fullEnvPattern = `^[a-z][a-z0-9]{0,7}\.[a-z][a-z0-9]{0,7}$`
)

var (
	envPartRegexp = regexp.MustCompile(envPartPattern)
	fullEnvRegexp = regexp.MustCompile(fullEnvPattern)
)

var (
	// ErrInvalidFullEnvName 表示完整环境名不合法。
	ErrInvalidFullEnvName = errors.New("非法完整环境名")
	// ErrInvalidScope 表示 scope 不合法。
	ErrInvalidScope = errors.New("非法 scope")
	// ErrInvalidEnvName 表示环境名不合法。
	ErrInvalidEnvName = errors.New("非法环境名")
	// ErrNoDefaultScope 表示缺少默认 scope。
	ErrNoDefaultScope = errors.New("缺少默认 scope")

	EmpytEnvName = FullEnvName("")
)

// NewFullEnvName 解析输入并返回完整环境名。
func NewFullEnvName(scope string, name string) (FullEnvName, error) {
	if IsFullEnvName(name) {
		if err := ValidateFullEnvName(name); err != nil {
			return "", err
		}
		return FullEnvName(name), nil
	}

	if scope == "" {
		return "", ErrNoDefaultScope
	}
	if err := ValidateScope(scope); err != nil {
		return "", err
	}
	if err := ValidateEnvName(name); err != nil {
		return "", err
	}
	return FullEnvName(scope + "." + name), nil
}

// IsFullEnvName 返回输入是否采用完整环境名形式。
func IsFullEnvName(name string) bool {
	return strings.Contains(name, ".")
}

// FullEnvName 表示完整环境名（scope.name 格式）。
type FullEnvName string

// String 返回完整环境名的字符串表示。
func (f FullEnvName) String() string {
	return string(f)
}

// Split 返回 scope 和 name
func (f FullEnvName) Split() (string, string) {
	scope, name, _ := strings.Cut(string(f), ".")
	return scope, name
}

func (f FullEnvName) SafeFileName() string {
	return strings.ReplaceAll(string(f), ".", "__")
}

// ValidateScope 校验 scope 是否合法。
func ValidateScope(scope string) error {
	if !envPartRegexp.MatchString(scope) {
		return fmt.Errorf("%w: %q", ErrInvalidScope, scope)
	}
	return nil
}

// ValidateEnvName 校验环境名是否合法。
func ValidateEnvName(name string) error {
	if !envPartRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidEnvName, name)
	}
	return nil
}

// ValidateFullEnvName 校验完整环境名是否合法。
func ValidateFullEnvName(name string) error {
	if !fullEnvRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidFullEnvName, name)
	}
	return nil
}
