package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ArtifactMapping maps a Bazel canonical label to its resolved runfile path.
type ArtifactMapping struct {
	Label    string
	FilePath string
}

// RunConfig holds the parsed launcher arguments.
type RunConfig struct {
	ManifestPath string
	Artifacts    []*ArtifactMapping
}

// ParseArgs parses launcher CLI arguments in the format:
//
//	--manifest=<path> --artifact=<label>,<filepath> [--artifact=<label>,<filepath> ...]
func ParseArgs(args []string) (*RunConfig, error) {
	config := new(RunConfig)
	seenLabels := map[string]bool{}

	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--manifest="):
			val := strings.TrimPrefix(arg, "--manifest=")
			if val == "" {
				return nil, fmt.Errorf("--manifest value is empty")
			}
			config.ManifestPath = val

		case strings.HasPrefix(arg, "--artifact="):
			val := strings.TrimPrefix(arg, "--artifact=")
			label, filepath, err := parseArtifactValue(val)
			if err != nil {
				return nil, fmt.Errorf("invalid --artifact %q: %w", val, err)
			}
			if seenLabels[label] {
				return nil, fmt.Errorf("duplicate artifact label %q", label)
			}
			seenLabels[label] = true
			config.Artifacts = append(config.Artifacts, &ArtifactMapping{
				Label:    label,
				FilePath: filepath,
			})

		default:
			return nil, fmt.Errorf("unrecognized argument: %s", arg)
		}
	}

	if config.ManifestPath == "" {
		return nil, errors.New("--manifest is required")
	}
	if len(config.Artifacts) == 0 {
		return nil, errors.New("at least one --artifact is required")
	}

	return config, nil
}

// parseArtifactValue splits a "--artifact" value into label and filepath.
// Expected format: "<label>,<filepath>"
func parseArtifactValue(val string) (label, filepath string, err error) {
	parts := strings.SplitN(val, ",", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected format label,filepath")
	}
	label = parts[0]
	filepath = parts[1]
	if label == "" {
		return "", "", fmt.Errorf("label is empty")
	}
	if filepath == "" {
		return "", "", fmt.Errorf("filepath is empty")
	}
	return label, filepath, nil
}

// ResolveArtifacts converts RunConfig into a label→filepath map and
// verifies that each file exists on disk.
func ResolveArtifacts(config *RunConfig) (map[string]string, error) {
	result := make(map[string]string, len(config.Artifacts))
	for _, a := range config.Artifacts {
		if _, err := os.Stat(a.FilePath); err != nil {
			return nil, fmt.Errorf("artifact %q file %q: %w", a.Label, a.FilePath, err)
		}
		result[a.Label] = a.FilePath
	}
	return result, nil
}
