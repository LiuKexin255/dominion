// Package k8s provides Kubernetes label helpers for the deploy runtime.
package k8s

import (
	"strings"

	"k8s.io/apimachinery/pkg/labels"
)

const (
	managedByLabelKey           = "app.kubernetes.io/managed-by"
	appLabelKey                 = "app.kubernetes.io/name"
	serviceLabelKey             = "app.kubernetes.io/component"
	dominionEnvironmentLabelKey = "dominion.io/environment"
)

type labelOption func(*labelSet)

type labelSet struct {
	app                 string
	service             string
	dominionEnvironment string
	managedBy           string
}

func withApp(name string) labelOption {
	return func(set *labelSet) {
		set.app = strings.TrimSpace(name)
	}
}

func withService(name string) labelOption {
	return func(set *labelSet) {
		set.service = strings.TrimSpace(name)
	}
}

func withDominionEnvironment(name string) labelOption {
	return func(set *labelSet) {
		set.dominionEnvironment = strings.TrimSpace(name)
	}
}

func withManagedBy(name string) labelOption {
	return func(set *labelSet) {
		set.managedBy = strings.TrimSpace(name)
	}
}

func buildLabels(options ...labelOption) labels.Set {
	set := &labelSet{}
	for _, option := range options {
		if option != nil {
			option(set)
		}
	}

	result := labels.Set{}
	if set.app != "" {
		result[appLabelKey] = set.app
	}
	if set.service != "" {
		result[serviceLabelKey] = set.service
	}
	if set.dominionEnvironment != "" {
		result[dominionEnvironmentLabelKey] = set.dominionEnvironment
	}
	if set.managedBy != "" {
		result[managedByLabelKey] = set.managedBy
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func hasAllLabels(current map[string]string, want labels.Set) bool {
	for key, value := range want {
		if current[key] != value {
			return false
		}
	}

	return true
}

func buildLabelSelector(matchLabels labels.Set) string {
	selectorLabels := labels.Set{}
	for key, value := range matchLabels {
		if !isValidLabelValue(value) {
			continue
		}
		selectorLabels[key] = value
	}

	return selectorLabels.String()
}

func isValidLabelValue(value string) bool {
	if value == "" {
		return true
	}

	for i, r := range value {
		if !isValidLabelValueChar(r) {
			return false
		}
		if (i == 0 || i == len(value)-1) && !isASCIIAlphaNumeric(r) {
			return false
		}
	}

	return true
}

func isValidLabelValueChar(r rune) bool {
	return isASCIIAlphaNumeric(r) || r == '-' || r == '_' || r == '.'
}

func isASCIIAlphaNumeric(r rune) bool {
	return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}
