package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// settings is a config shape that exercises every row of the deep-merge
// matrix (specs/045-deploy-config/data-model.md "Deep Merge Semantics").
type settings struct {
	Title   string            `yaml:"title"`
	Count   int               `yaml:"count"`
	Allowed []string          `yaml:"allowed"`
	Meta    map[string]string `yaml:"meta"`
}

// mergeDefaults is the defaults value shared by the merge matrix cases.
func mergeDefaults() settings {
	return settings{
		Title:   "hello",
		Count:   1,
		Allowed: []string{"a", "b"},
		Meta:    map[string]string{"region": "cn", "env": "dev"},
	}
}

// writeConfigFile writes one config file under dir/{block}/{key} inside the
// mock config directory; it is the runtime file layout
// {DOMINION_CONFIG_DIR}/{block}/{key} (specs/045-deploy-config/contracts/runtime-contract.md §3).
func writeConfigFile(t *testing.T, dir, block, key, content string) {
	t.Helper()
	path := filepath.Join(dir, block, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

// writeConfig writes one config file into a fresh temp directory and points
// DOMINION_CONFIG_DIR at it, returning the directory for further files.
func writeConfig(t *testing.T, block, key, content string) string {
	t.Helper()
	dir := t.TempDir()
	writeConfigFile(t, dir, block, key, content)
	t.Setenv(envConfigDir, dir)
	return dir
}

func TestReadDeepMerge(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    settings
		wantErr bool
	}{
		{
			name:    "object keys merge recursively",
			content: "meta:\n  env: prod\n",
			want: settings{
				Title:   "hello",
				Count:   1,
				Allowed: []string{"a", "b"},
				Meta:    map[string]string{"region": "cn", "env": "prod"},
			},
		},
		{
			name:    "scalar replaces default",
			content: "title: hi\ncount: 5\n",
			want: settings{
				Title:   "hi",
				Count:   5,
				Allowed: []string{"a", "b"},
				Meta:    map[string]string{"region": "cn", "env": "dev"},
			},
		},
		{
			name:    "array replaces default wholesale",
			content: "allowed:\n  - x\n",
			want: settings{
				Title:   "hello",
				Count:   1,
				Allowed: []string{"x"},
				Meta:    map[string]string{"region": "cn", "env": "dev"},
			},
		},
		{
			name:    "keys absent from config keep defaults",
			content: "count: 2\n",
			want: settings{
				Title:   "hello",
				Count:   2,
				Allowed: []string{"a", "b"},
				Meta:    map[string]string{"region": "cn", "env": "dev"},
			},
		},
		{
			name:    "explicit null overrides default with zero value",
			content: "title: null\n",
			want: settings{
				Title:   "",
				Count:   1,
				Allowed: []string{"a", "b"},
				Meta:    map[string]string{"region": "cn", "env": "dev"},
			},
		},
		{
			name:    "type mismatch is reported when decoding",
			content: "meta: 123\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeConfig(t, "service_config", "greeting", tt.content)
			got, err := Read("service_config", "greeting", mergeDefaults())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Read(service_config, greeting) = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Read(service_config, greeting) unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Read(service_config, greeting) = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestReadReplacementIntoAnyField shows that replacement is semantic, not
// type-driven: a scalar default replaced by an array succeeds when the target
// field type permits it (data-model.md "Deep Merge Semantics" row 2).
func TestReadReplacementIntoAnyField(t *testing.T) {
	type flexible struct {
		Value any `yaml:"value"`
	}
	writeConfig(t, "service_config", "greeting", "value: [1, 2]\n")
	got, err := Read("service_config", "greeting", flexible{Value: "string"})
	if err != nil {
		t.Fatalf("Read(service_config, greeting) unexpected error: %v", err)
	}
	want := flexible{Value: []any{1, 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read(service_config, greeting) = %+v, want %+v", got, want)
	}
}

func TestReadDefaultsUnmodified(t *testing.T) {
	defaults := mergeDefaults()
	writeConfig(t, "service_config", "greeting", "title: hi\nmeta:\n  env: prod\n")
	if _, err := Read("service_config", "greeting", defaults); err != nil {
		t.Fatalf("Read(service_config, greeting) unexpected error: %v", err)
	}
	if want := mergeDefaults(); !reflect.DeepEqual(defaults, want) {
		t.Errorf("defaults mutated by Read: got %+v, want %+v", defaults, want)
	}
}

func TestReadErrors(t *testing.T) {
	t.Run("config dir env not set", func(t *testing.T) {
		t.Setenv(envConfigDir, "")
		_, err := Read("service_config", "greeting", mergeDefaults())
		if err == nil {
			t.Fatal("Read(service_config, greeting) = nil error, want error")
		}
		if !strings.Contains(err.Error(), envConfigDir) {
			t.Errorf("Read(service_config, greeting) error %q does not mention %s", err, envConfigDir)
		}
	})
	t.Run("config file missing", func(t *testing.T) {
		t.Setenv(envConfigDir, t.TempDir())
		_, err := Read("service_config", "missing", mergeDefaults())
		if err == nil {
			t.Fatal("Read(service_config, missing) = nil error, want error")
		}
		for _, want := range []string{"service_config", "missing"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Read(service_config, missing) error %q does not mention %q", err, want)
			}
		}
	})
	t.Run("config content not parseable", func(t *testing.T) {
		writeConfig(t, "service_config", "greeting", "title: [unclosed\n")
		_, err := Read("service_config", "greeting", mergeDefaults())
		if err == nil {
			t.Fatal("Read(service_config, greeting) = nil error, want error")
		}
	})
	t.Run("config content not a mapping", func(t *testing.T) {
		writeConfig(t, "service_config", "greeting", "- a\n- b\n")
		_, err := Read("service_config", "greeting", mergeDefaults())
		if err == nil {
			t.Fatal("Read(service_config, greeting) = nil error, want error")
		}
	})
}

// TestReadJSONAndYAML verifies the runtime contract "always parse as YAML":
// a json-type file (valid JSON) and a yaml-type file yield identical merged
// results (specs/045-deploy-config/research.md R4).
func TestReadJSONAndYAML(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "yaml-type content",
			content: "title: hi\ncount: 5\nallowed:\n  - x\nmeta:\n  region: cn\n",
		},
		{
			name:    "json-type content",
			content: `{"title": "hi", "count": 5, "allowed": ["x"], "meta": {"region": "cn"}}`,
		},
	}
	want := settings{
		Title:   "hi",
		Count:   5,
		Allowed: []string{"x"},
		Meta:    map[string]string{"region": "cn", "env": "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeConfig(t, "service_config", "greeting", tt.content)
			got, err := Read("service_config", "greeting", mergeDefaults())
			if err != nil {
				t.Fatalf("Read(service_config, greeting) unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Read(service_config, greeting) = %+v, want %+v", got, want)
			}
		})
	}
}

// TestReadIndependentAddressing covers the spec edge case that multiple
// config blocks selected by one artifact are addressed independently: reads
// of different blocks or different entries never interfere
// (specs/045-deploy-config/spec.md Edge Cases).
func TestReadIndependentAddressing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envConfigDir, dir)
	writeConfigFile(t, dir, "service_config", "greeting", "title: from block one\n")
	writeConfigFile(t, dir, "service_config", "limits", "count: 7\n")
	writeConfigFile(t, dir, "feature_flags", "greeting", "title: from block two\n")

	defaults := settings{Title: "default", Count: 1, Allowed: []string{"a"}, Meta: map[string]string{"region": "cn"}}
	tests := []struct {
		name  string
		block string
		key   string
		want  settings
	}{
		{
			name:  "block one entry",
			block: "service_config",
			key:   "greeting",
			want:  settings{Title: "from block one", Count: 1, Allowed: []string{"a"}, Meta: map[string]string{"region": "cn"}},
		},
		{
			name:  "second entry of the same block",
			block: "service_config",
			key:   "limits",
			want:  settings{Title: "default", Count: 7, Allowed: []string{"a"}, Meta: map[string]string{"region": "cn"}},
		},
		{
			name:  "entry of another block",
			block: "feature_flags",
			key:   "greeting",
			want:  settings{Title: "from block two", Count: 1, Allowed: []string{"a"}, Meta: map[string]string{"region": "cn"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Read(tt.block, tt.key, defaults)
			if err != nil {
				t.Fatalf("Read(%s, %s) unexpected error: %v", tt.block, tt.key, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Read(%s, %s) = %+v, want %+v", tt.block, tt.key, got, tt.want)
			}
		})
	}
}
