package validate

import (
	"os"
	"path/filepath"
	"testing"

	guitarconfig "dominion/tools/guitar/pkg/config"
)

func TestValidate(t *testing.T) {
	_ = newBazelWorkspace(t)

	cfg := &guitarconfig.Config{
		Name: "game-session-large-test",
		Suites: []*guitarconfig.Suite{
			{
				Name:   "default",
				Env:    "game.lt",
				Deploy: "//testdata/deploy_test.yaml",
				Endpoint: map[string]guitarconfig.Endpoints{
					"http": {
						"public": "https://game.liukexin.com",
					},
				},
				Cases: []string{"//projects/game/testplan:testplan_test"},
			},
		},
	}

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestValidate_MissingName(t *testing.T) {
	_ = newBazelWorkspace(t)

	cfg := &guitarconfig.Config{
		Name: "",
		Suites: []*guitarconfig.Suite{
			{
				Name:   "default",
				Env:    "game.lt",
				Deploy: "//testdata/deploy_test.yaml",
				Cases:  []string{"//test:test"},
			},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() succeeded unexpectedly")
	}
}

func TestValidate_EmptySuites(t *testing.T) {
	_ = newBazelWorkspace(t)

	cfg := &guitarconfig.Config{
		Name:   "test",
		Suites: nil,
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() succeeded unexpectedly")
	}
}

func TestValidate_NonTestDeployType(t *testing.T) {
	_ = newBazelWorkspace(t)

	cfg := &guitarconfig.Config{
		Name: "test",
		Suites: []*guitarconfig.Suite{
			{
				Name:   "default",
				Env:    "game.lt",
				Deploy: "//testdata/deploy_prod.yaml",
				Cases:  []string{"//test:test"},
			},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() succeeded unexpectedly, expected non-test deploy type error")
	}
}

func TestValidate_HostnameMismatch(t *testing.T) {
	_ = newBazelWorkspace(t)

	cfg := &guitarconfig.Config{
		Name: "test",
		Suites: []*guitarconfig.Suite{
			{
				Name:   "default",
				Env:    "game.lt",
				Deploy: "//testdata/deploy_test.yaml",
				Endpoint: map[string]guitarconfig.Endpoints{
					"http": {
						"public": "https://unknown.example.com",
					},
				},
				Cases: []string{"//test:test"},
			},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() succeeded unexpectedly, expected hostname mismatch error")
	}
}

func TestValidate_HostnameMatch(t *testing.T) {
	_ = newBazelWorkspace(t)

	cfg := &guitarconfig.Config{
		Name: "test",
		Suites: []*guitarconfig.Suite{
			{
				Name:   "default",
				Env:    "game.lt",
				Deploy: "//testdata/deploy_test.yaml",
				Endpoint: map[string]guitarconfig.Endpoints{
					"http": {
						"public": "https://game.liukexin.com",
					},
				},
				Cases: []string{"//test:test"},
			},
		},
	}

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestValidate_InvalidEnvFormat(t *testing.T) {
	_ = newBazelWorkspace(t)

	tests := []struct {
		name string
		env  string
	}{
		{name: "no dot", env: "game"},
		{name: "trailing dot", env: "game."},
		{name: "leading dot", env: ".lt"},
		{name: "trailing extra dot", env: "game.lt."},
		{name: "empty", env: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &guitarconfig.Config{
				Name: "test",
				Suites: []*guitarconfig.Suite{
					{
						Name:   "default",
						Env:    tt.env,
						Deploy: "//testdata/deploy_test.yaml",
						Cases:  []string{"//test:test"},
					},
				},
			}

			err := Validate(cfg)
			if err == nil {
				t.Fatal("Validate() succeeded unexpectedly, expected env format error")
			}
		})
	}
}

func TestValidate_ValidEnvFormat(t *testing.T) {
	_ = newBazelWorkspace(t)

	cfg := &guitarconfig.Config{
		Name: "test",
		Suites: []*guitarconfig.Suite{
			{
				Name:   "default",
				Env:    "game.lt",
				Deploy: "//testdata/deploy_test.yaml",
				Cases:  []string{"//test:test"},
			},
		},
	}

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestValidate_InvalidEndpointName(t *testing.T) {
	_ = newBazelWorkspace(t)

	cfg := &guitarconfig.Config{
		Name: "test",
		Suites: []*guitarconfig.Suite{
			{
				Name:   "default",
				Env:    "game.lt",
				Deploy: "//testdata/deploy_test.yaml",
				Endpoint: map[string]guitarconfig.Endpoints{
					"http": {
						"123invalid": "https://game.liukexin.com",
					},
				},
				Cases: []string{"//test:test"},
			},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() succeeded unexpectedly, expected endpoint name error")
	}
}

func TestValidate_StatefulHostnameSuffixMatch(t *testing.T) {
	_ = newBazelWorkspace(t)

	cfg := &guitarconfig.Config{
		Name: "test",
		Suites: []*guitarconfig.Suite{
			{
				Name:   "default",
				Env:    "game.lt",
				Deploy: "//testdata/deploy_test.yaml",
				Endpoint: map[string]guitarconfig.Endpoints{
					"http": {
						"public": "https://game-gateway-0-game.liukexin.com",
					},
				},
				Cases: []string{"//test:test"},
			},
		},
	}

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestValidate_StatefulHostnameSuffixMismatch(t *testing.T) {
	_ = newBazelWorkspace(t)

	cfg := &guitarconfig.Config{
		Name: "test",
		Suites: []*guitarconfig.Suite{
			{
				Name:   "default",
				Env:    "game.lt",
				Deploy: "//testdata/deploy_test.yaml",
				Endpoint: map[string]guitarconfig.Endpoints{
					"http": {
						"public": "https://game-0.unknown.com",
					},
				},
				Cases: []string{"//test:test"},
			},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() succeeded unexpectedly, expected hostname mismatch error")
	}
}

func TestHostnameMatches(t *testing.T) {
	hostnameSet := map[string]bool{
		"game.liukexin.com": true,
		"api.example.com":   true,
	}

	tests := []struct {
		name    string
		host    string
		matches bool
	}{
		{name: "exact match", host: "game.liukexin.com", matches: true},
		{name: "suffix match instance 0", host: "game-gateway-0-game.liukexin.com", matches: true},
		{name: "suffix match instance 2", host: "game-gateway-2-game.liukexin.com", matches: true},
		{name: "suffix match different prefix", host: "other-api.example.com", matches: true},
		{name: "no match different domain", host: "unknown.example.com", matches: false},
		{name: "no match partial suffix", host: "xgame.liukexin.com", matches: false},
		{name: "no match empty host", host: "", matches: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostnameMatches(tt.host, hostnameSet)
			if got != tt.matches {
				t.Fatalf("hostnameMatches(%q) = %v, want %v", tt.host, got, tt.matches)
			}
		})
	}
}

func newBazelWorkspace(t *testing.T) string {
	t.Helper()

	srcRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	copyDir(t, filepath.Join(srcRoot, "testdata"), filepath.Join(root, "testdata"))
	withWorkingDir(t, root)
	return root
}

func copyDir(t *testing.T, src string, dst string) {
	t.Helper()

	err := filepath.Walk(src, func(srcPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(src, srcPath)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, os.ModePerm)
		}

		raw, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), os.ModePerm); err != nil {
			return err
		}

		return os.WriteFile(dstPath, raw, info.Mode())
	})
	if err != nil {
		t.Fatalf("copyDir() failed: %v", err)
	}
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working dir failed: %v", err)
		}
	})
}
