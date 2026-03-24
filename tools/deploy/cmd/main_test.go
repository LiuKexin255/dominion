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
		{name: "env only", args: []string{"--env=dev"}},
		{name: "env with app", args: []string{"--env=dev", "--app=test-app"}},
		{name: "deploy with env", args: []string{"--deploy=deploy.yaml", "--env=dev"}},
		{name: "deploy with env and app", args: []string{"--deploy=deploy.yaml", "--env=dev", "--app=test-app"}},
		{name: "delete only", args: []string{"--del=dev"}},
		{name: "delete with app", args: []string{"--del=dev", "--app=test-app"}},
		{name: "missing args", args: nil, wantErr: true},
		{name: "deploy without env", args: []string{"--deploy=deploy.yaml"}, wantErr: true},
		{name: "delete with env", args: []string{"--del=dev", "--env=dev"}, wantErr: true},
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
