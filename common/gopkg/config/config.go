package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// envConfigDir is the platform-injected environment variable pointing at the
// config root directory; its value is defined by the runtime contract
// (specs/045-deploy-config/contracts/runtime-contract.md §1).
const envConfigDir = "DOMINION_CONFIG_DIR"

// Read reads the config entry (block, key) and returns a new value with the
// file content deep-merged over defaults. defaults is never modified.
//
// The config file is discovered through the platform-injected
// DOMINION_CONFIG_DIR environment variable at {DOMINION_CONFIG_DIR}/{block}/{key},
// and is always parsed as YAML, which also accepts JSON content since JSON is
// a subset of YAML (specs/045-deploy-config/research.md R4).
//
// Merge semantics: objects/maps merge recursively; arrays and scalars are
// replaced wholesale by the config value, and an explicit null overrides the
// default with the zero value. See the package documentation and
// specs/045-deploy-config/data-model.md "Deep Merge Semantics".
//
// Read returns an error when DOMINION_CONFIG_DIR is not set, when the config
// file does not exist (the block was not selected in deploy.yaml), or when
// the file content cannot be parsed or decoded into T.
func Read[T any](block, key string, defaults T) (T, error) {
	var out T
	dir, ok := os.LookupEnv(envConfigDir)
	if !ok || dir == "" {
		return out, fmt.Errorf("read config (%q, %q): %s is not set", block, key, envConfigDir)
	}
	path := filepath.Join(dir, block, key)
	content, err := os.ReadFile(path)
	if err != nil {
		return out, fmt.Errorf("read config (%q, %q) from %q: %w", block, key, path, err)
	}
	cfgMap, err := toMap(content)
	if err != nil {
		return out, fmt.Errorf("parse config (%q, %q) from %q: %w", block, key, path, err)
	}
	defMap, err := defaultsMap(defaults)
	if err != nil {
		return out, fmt.Errorf("encode defaults for config (%q, %q): %w", block, key, err)
	}
	merged, err := yaml.Marshal(mergeInto(defMap, cfgMap))
	if err != nil {
		return out, fmt.Errorf("encode merged config (%q, %q): %w", block, key, err)
	}
	if err := yaml.Unmarshal(merged, &out); err != nil {
		return out, fmt.Errorf("decode merged config (%q, %q): %w", block, key, err)
	}
	return out, nil
}

// toMap decodes YAML content into a string-keyed map. The same parser is
// used for every file regardless of its declared type, because valid JSON is
// valid YAML (specs/045-deploy-config/research.md R4).
func toMap(content []byte) (map[string]any, error) {
	var m map[string]any
	if err := yaml.Unmarshal(content, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// defaultsMap deep-copies defaults into a map via a marshal/unmarshal round
// trip (specs/045-deploy-config/contracts/sdk-go.md §2 step 3), so that
// merging never touches the caller's object.
func defaultsMap(defaults any) (map[string]any, error) {
	raw, err := yaml.Marshal(defaults)
	if err != nil {
		return nil, err
	}
	return toMap(raw)
}

// mergeInto recursively merges src over dst and returns dst. Maps merge
// recursively; every other value — arrays, scalars and explicit nulls —
// replaces the destination wholesale, so arrays are never merged by index
// (specs/045-deploy-config/data-model.md "Deep Merge Semantics").
func mergeInto(dst, src map[string]any) map[string]any {
	for key, srcVal := range src {
		dstVal, dstHas := dst[key]
		srcMap, srcIsMap := srcVal.(map[string]any)
		dstMap, dstIsMap := dstVal.(map[string]any)
		if dstHas && srcIsMap && dstIsMap {
			mergeInto(dstMap, srcMap)
			continue
		}
		dst[key] = srcVal
	}
	return dst
}
