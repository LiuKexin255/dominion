package main

import (
	"fmt"
	"regexp"
	"slices"
	"sort"

	"gopkg.in/yaml.v3"
)

var (
	validNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	validVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

var validPlatforms = []string{"windows", "linux", "darwin"}
var validArchs = []string{"amd64", "386", "arm64", "arm"}

// Artifact describes one release file produced from a Bazel target.
type Artifact struct {
	Target   string `yaml:"target"`
	Filename string `yaml:"filename"`
	Platform string `yaml:"platform"`
	Arch     string `yaml:"arch"`
}

// ReleaseManifest describes the YAML model used to publish release artifacts.
type ReleaseManifest struct {
	Name      string      `yaml:"name"`
	Version   string      `yaml:"version"`
	Artifacts []*Artifact `yaml:"artifacts"`
}

// Validate checks required fields, enums, and uniqueness constraints.
func (m *ReleaseManifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !validNamePattern.MatchString(m.Name) {
		return fmt.Errorf("name %q must match %s", m.Name, validNamePattern.String())
	}
	if m.Version == "" {
		return fmt.Errorf("version is required")
	}
	if !validVersionPattern.MatchString(m.Version) {
		return fmt.Errorf("version %q must match %s", m.Version, validVersionPattern.String())
	}
	if len(m.Artifacts) == 0 {
		return fmt.Errorf("artifacts is required")
	}

	targets := map[string]bool{}
	filenames := map[string]bool{}
	platformArchs := map[string]bool{}
	for i, artifact := range m.Artifacts {
		if artifact == nil {
			return fmt.Errorf("artifacts[%d] is required", i)
		}
		if artifact.Target == "" {
			return fmt.Errorf("artifacts[%d].target is required", i)
		}
		if artifact.Filename == "" {
			return fmt.Errorf("artifacts[%d].filename is required", i)
		}
		if artifact.Platform == "" {
			return fmt.Errorf("artifacts[%d].platform is required", i)
		}
		if artifact.Arch == "" {
			return fmt.Errorf("artifacts[%d].arch is required", i)
		}
		if !stringInSlice(artifact.Platform, validPlatforms) {
			return fmt.Errorf("artifacts[%d].platform %q is invalid", i, artifact.Platform)
		}
		if !stringInSlice(artifact.Arch, validArchs) {
			return fmt.Errorf("artifacts[%d].arch %q is invalid", i, artifact.Arch)
		}
		if targets[artifact.Target] {
			return fmt.Errorf("duplicate target %q", artifact.Target)
		}
		if filenames[artifact.Filename] {
			return fmt.Errorf("duplicate filename %q", artifact.Filename)
		}

		platformArch := artifact.Platform + "/" + artifact.Arch
		if platformArchs[platformArch] {
			return fmt.Errorf("duplicate platform and arch %q", platformArch)
		}

		targets[artifact.Target] = true
		filenames[artifact.Filename] = true
		platformArchs[platformArch] = true
	}

	return nil
}

// ValidateTargets checks that manifest targets and Bazel targets are exactly equal.
func ValidateTargets(manifestTargets []string, bazelTargets []string) error {
	manifestSorted := append([]string(nil), manifestTargets...)
	bazelSorted := append([]string(nil), bazelTargets...)
	sort.Strings(manifestSorted)
	sort.Strings(bazelSorted)

	if len(manifestSorted) != len(bazelSorted) {
		return fmt.Errorf("manifest targets %v do not match bazel targets %v", manifestSorted, bazelSorted)
	}
	for i := range manifestSorted {
		if manifestSorted[i] != bazelSorted[i] {
			return fmt.Errorf("manifest targets %v do not match bazel targets %v", manifestSorted, bazelSorted)
		}
	}

	return nil
}

// ParseManifest unmarshals YAML data and validates the resulting manifest.
func ParseManifest(data []byte) (*ReleaseManifest, error) {
	manifest := new(ReleaseManifest)
	if err := yaml.Unmarshal(data, manifest); err != nil {
		return nil, fmt.Errorf("unmarshal manifest yaml: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}

	return manifest, nil
}

func stringInSlice(s string, slice []string) bool {
	return slices.Contains(slice, s)
}
