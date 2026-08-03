package domain

import "testing"

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
			if got.String() != "deploy/scopes/scope1/environments/dev" {
				t.Fatalf("String() = %q, want %q", got.String(), "deploy/scopes/scope1/environments/dev")
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

func TestValidateScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		wantErr bool
	}{
		{name: "valid scope", scope: "scope1"},
		{name: "single char scope", scope: "a"},
		{name: "max length scope", scope: "a1234567"},
		{name: "empty scope", scope: "", wantErr: true},
		{name: "invalid scope chars", scope: "Scope1", wantErr: true},
		{name: "scope too long", scope: "scope1234", wantErr: true},
		{name: "scope starting with digit", scope: "1abc", wantErr: true},
		{name: "scope with underscore", scope: "scope_1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			scope := tt.scope

			// when
			err := ValidateScope(scope)

			// then
			if tt.wantErr {
				if err != ErrInvalidName {
					t.Fatalf("ValidateScope(%q) error = %v, want %v", scope, err, ErrInvalidName)
				}
				return
			}

			if err != nil {
				t.Fatalf("ValidateScope(%q) unexpected error: %v", scope, err)
			}
		})
	}
}
