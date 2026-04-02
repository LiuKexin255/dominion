package main

import (
	"testing"
)

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "empty args", args: nil, wantErr: true},
		{name: "use only", args: []string{"use", "dev"}},
		{name: "use with app", args: []string{"use", "--app=test-app", "dev"}},
		{name: "use with app postfix", args: []string{"use", "dev", "--app=test-app"}},
		{name: "deploy only", args: []string{"deploy", "deploy.yaml"}},
		{name: "deploy with app", args: []string{"deploy", "--app=test-app", "deploy.yaml"}, wantErr: true},
		{name: "deploy with app postfix", args: []string{"deploy", "deploy.yaml", "--app=test-app"}, wantErr: true},
		{name: "del only", args: []string{"del", "dev"}},
		{name: "del with app", args: []string{"del", "--app=test-app", "dev"}},
		{name: "del with app postfix", args: []string{"del", "dev", "--app=test-app"}},
		{name: "list only", args: []string{"list"}},
		{name: "list with positional arg", args: []string{"list", "dev"}, wantErr: true},
		{name: "list with app", args: []string{"list", "--app=test-app"}, wantErr: true},
		{name: "cur only", args: []string{"cur"}},
		{name: "cur with positional arg", args: []string{"cur", "dev"}, wantErr: true},
		{name: "cur with app", args: []string{"cur", "--app=test-app"}, wantErr: true},
		{name: "unknown command", args: []string{"switch", "dev"}, wantErr: true},
		{name: "use missing env", args: []string{"use"}, wantErr: true},
		{name: "deploy missing path", args: []string{"deploy"}, wantErr: true},
		{name: "unknown option", args: []string{"use", "--env=dev", "dev"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOptions(tt.args)
			if tt.wantErr && err == nil {
				t.Fatalf("parseOptions(%v) expected error", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("parseOptions(%v) unexpected error: %v", tt.args, err)
			}
		})
	}
}

func TestValidateUseOptions(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "valid", target: "dev"},
		{name: "missing target", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUseOptions(&options{target: tt.target})
			if tt.wantErr && err == nil {
				t.Fatal("validateUseOptions() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateUseOptions() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateDeployOptions(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		app     string
		wantErr bool
	}{
		{name: "valid", target: "deploy.yaml"},
		{name: "missing target", wantErr: true},
		{name: "with app", target: "deploy.yaml", app: "test-app", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDeployOptions(&options{target: tt.target, app: tt.app})
			if tt.wantErr && err == nil {
				t.Fatal("validateDeployOptions() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateDeployOptions() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateDelOptions(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "valid", target: "dev"},
		{name: "missing target", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDelOptions(&options{target: tt.target})
			if tt.wantErr && err == nil {
				t.Fatal("validateDelOptions() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateDelOptions() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateListOptions(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "valid"},
		{name: "with positional arg", target: "dev", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateListOptions(&options{target: tt.target})
			if tt.wantErr && err == nil {
				t.Fatal("validateListOptions() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateListOptions() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateCurOptions(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "valid"},
		{name: "with positional arg", target: "dev", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCurOptions(&options{target: tt.target})
			if tt.wantErr && err == nil {
				t.Fatal("validateCurOptions() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateCurOptions() unexpected error: %v", err)
			}
		})
	}
}

func TestDeployAndActivate_RequiresActiveEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		opts    *options
		wantErr string
	}{
		{
			name:    "missing active env",
			opts:    &options{target: "deploy.yaml"},
			wantErr: "deploy 需要当前已激活环境",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BUILD_WORKSPACE_DIRECTORY", t.TempDir())

			err := deployAndActivate(tt.opts)
			if err == nil {
				t.Fatal("deployAndActivate() expected error")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("deployAndActivate() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}
