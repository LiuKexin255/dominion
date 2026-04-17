package domain

import "testing"

func TestParseServiceEndpointsName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ServiceEndpointsName
		wantErr bool
	}{
		{
			name:  "valid resource name",
			input: "deploy/scopes/scope1/environments/dev/apps/my-app/services/api-v1/endpoints",
			want: ServiceEndpointsName{
				scope:   "scope1",
				envName: "dev",
				app:     "my-app",
				service: "api-v1",
			},
		},
		{name: "empty", input: "", wantErr: true},
		{name: "missing segments", input: "deploy/scopes/scope1/environments/dev/apps/my-app/services/api-v1", wantErr: true},
		{name: "invalid scope", input: "deploy/scopes/Scope1/environments/dev/apps/my-app/services/api-v1/endpoints", wantErr: true},
		{name: "invalid env name", input: "deploy/scopes/scope1/environments/Dev/apps/my-app/services/api-v1/endpoints", wantErr: true},
		{name: "invalid app", input: "deploy/scopes/scope1/environments/dev/apps/MyApp/services/api-v1/endpoints", wantErr: true},
		{name: "invalid service", input: "deploy/scopes/scope1/environments/dev/apps/my-app/services/API/endpoints", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			input := tt.input

			// when
			got, err := ParseServiceEndpointsName(input)

			// then
			if tt.wantErr {
				if err != ErrInvalidName {
					t.Fatalf("ParseServiceEndpointsName(%q) error = %v, want %v", input, err, ErrInvalidName)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseServiceEndpointsName(%q) unexpected error: %v", input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseServiceEndpointsName(%q) = %#v, want %#v", input, got, tt.want)
			}
			if got.String() != input {
				t.Fatalf("String() = %q, want %q", got.String(), input)
			}
		})
	}
}

func TestNewServiceEndpointsName(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		envName string
		app     string
		service string
		want    ServiceEndpointsName
		wantErr bool
	}{
		{
			name:    "valid service endpoints name",
			scope:   "scope1",
			envName: "dev",
			app:     "my-app",
			service: "api-v1",
			want: ServiceEndpointsName{
				scope:   "scope1",
				envName: "dev",
				app:     "my-app",
				service: "api-v1",
			},
		},
		{name: "empty scope", scope: "", envName: "dev", app: "my-app", service: "api-v1", wantErr: true},
		{name: "empty env name", scope: "scope1", envName: "", app: "my-app", service: "api-v1", wantErr: true},
		{name: "empty app", scope: "scope1", envName: "dev", app: "", service: "api-v1", wantErr: true},
		{name: "empty service", scope: "scope1", envName: "dev", app: "my-app", service: "", wantErr: true},
		{name: "invalid scope chars", scope: "Scope1", envName: "dev", app: "my-app", service: "api-v1", wantErr: true},
		{name: "invalid env chars", scope: "scope1", envName: "Dev", app: "my-app", service: "api-v1", wantErr: true},
		{name: "invalid app chars", scope: "scope1", envName: "dev", app: "MyApp", service: "api-v1", wantErr: true},
		{name: "invalid service chars", scope: "scope1", envName: "dev", app: "my-app", service: "API", wantErr: true},
		{name: "scope too long", scope: "scope1234", envName: "dev", app: "my-app", service: "api-v1", wantErr: true},
		{name: "env name too long", scope: "scope1", envName: "dev123456", app: "my-app", service: "api-v1", wantErr: true},
		{name: "app too long", scope: "scope1", envName: "dev", app: "my-app-123456789012345", service: "api-v1", wantErr: true},
		{name: "service too long", scope: "scope1", envName: "dev", app: "my-app", service: "api-v1-123456789012345", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			scope := tt.scope
			envName := tt.envName
			app := tt.app
			service := tt.service

			// when
			got, err := NewServiceEndpointsName(scope, envName, app, service)

			// then
			if tt.wantErr {
				if err != ErrInvalidName {
					t.Fatalf("NewServiceEndpointsName(%q, %q, %q, %q) error = %v, want %v", scope, envName, app, service, err, ErrInvalidName)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewServiceEndpointsName(%q, %q, %q, %q) unexpected error: %v", scope, envName, app, service, err)
			}
			if got != tt.want {
				t.Fatalf("NewServiceEndpointsName(%q, %q, %q, %q) = %#v, want %#v", scope, envName, app, service, got, tt.want)
			}
			if got.String() != "deploy/scopes/scope1/environments/dev/apps/my-app/services/api-v1/endpoints" {
				t.Fatalf("String() = %q, want %q", got.String(), "deploy/scopes/scope1/environments/dev/apps/my-app/services/api-v1/endpoints")
			}
			if got.Scope() != scope {
				t.Fatalf("Scope() = %q, want %q", got.Scope(), scope)
			}
			if got.EnvName() != envName {
				t.Fatalf("EnvName() = %q, want %q", got.EnvName(), envName)
			}
			if got.App() != app {
				t.Fatalf("App() = %q, want %q", got.App(), app)
			}
			if got.Service() != service {
				t.Fatalf("Service() = %q, want %q", got.Service(), service)
			}
		})
	}
}

func TestServiceEndpointsName_EnvironmentName(t *testing.T) {
	// given
	name := ServiceEndpointsName{
		scope:   "scope1",
		envName: "dev",
		app:     "my-app",
		service: "api-v1",
	}

	// when
	got, err := name.EnvironmentName()

	// then
	if err != nil {
		t.Fatalf("EnvironmentName() unexpected error: %v", err)
	}
	want, err := NewEnvironmentName("scope1", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("EnvironmentName() = %#v, want %#v", got, want)
	}
}

func TestServiceEndpointsName_EnvLabel(t *testing.T) {
	// given
	name := ServiceEndpointsName{
		scope:   "scope1",
		envName: "dev",
		app:     "my-app",
		service: "api-v1",
	}

	// when
	got := name.EnvLabel()

	// then
	want := "scope1.dev"
	if got != want {
		t.Fatalf("EnvLabel() = %q, want %q", got, want)
	}
}
