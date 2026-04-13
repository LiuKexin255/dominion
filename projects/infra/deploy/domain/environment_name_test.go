package domain

import "testing"

func TestParseResourceName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    EnvironmentName
		wantErr bool
	}{
		{
			name:  "valid resource name",
			input: "deploy/scope1/environments/dev",
			want: EnvironmentName{
				scope:   "scope1",
				envName: "dev",
			},
		},
		{name: "empty", input: "", wantErr: true},
		{name: "wrong format", input: "deploy/scope1/dev", wantErr: true},
		{name: "invalid characters", input: "deploy/Scope1/environments/dev", wantErr: true},
		{name: "too long", input: "deploy/scope1234/environments/dev", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			input := tt.input

			// when
			got, err := ParseResourceName(input)

			// then
			if tt.wantErr {
				if err != ErrInvalidName {
					t.Fatalf("ParseResourceName(%q) error = %v, want %v", input, err, ErrInvalidName)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseResourceName(%q) unexpected error: %v", input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseResourceName(%q) = %#v, want %#v", input, got, tt.want)
			}
		})
	}
}

func TestNewEnvironmentName(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		envName string
		want    EnvironmentName
		wantErr bool
	}{
		{
			name:    "valid environment name",
			scope:   "scope1",
			envName: "dev",
			want: EnvironmentName{
				scope:   "scope1",
				envName: "dev",
			},
		},
		{name: "empty scope", scope: "", envName: "dev", wantErr: true},
		{name: "empty env name", scope: "scope1", envName: "", wantErr: true},
		{name: "invalid scope chars", scope: "Scope1", envName: "dev", wantErr: true},
		{name: "invalid env chars", scope: "scope1", envName: "Dev", wantErr: true},
		{name: "scope too long", scope: "scope1234", envName: "dev", wantErr: true},
		{name: "env name too long", scope: "scope1", envName: "dev123456", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			scope := tt.scope
			envName := tt.envName

			// when
			got, err := NewEnvironmentName(scope, envName)

			// then
			if tt.wantErr {
				if err != ErrInvalidName {
					t.Fatalf("NewEnvironmentName(%q, %q) error = %v, want %v", scope, envName, err, ErrInvalidName)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewEnvironmentName(%q, %q) unexpected error: %v", scope, envName, err)
			}
			if got != tt.want {
				t.Fatalf("NewEnvironmentName(%q, %q) = %#v, want %#v", scope, envName, got, tt.want)
			}
			if got.String() != "deploy/scope1/environments/dev" {
				t.Fatalf("String() = %q, want %q", got.String(), "deploy/scope1/environments/dev")
			}
			if got.Scope() != scope {
				t.Fatalf("Scope() = %q, want %q", got.Scope(), scope)
			}
			if got.EnvName() != envName {
				t.Fatalf("EnvName() = %q, want %q", got.EnvName(), envName)
			}
		})
	}
}
