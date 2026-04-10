package k8s

import (
	"regexp"
	"testing"

	"dominion/tools/deploy/pkg/config"
)

func TestMongoDBWorkloadValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   *MongoDBWorkload
		wantErr bool
	}{
		{
			name: "valid",
			input: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				DominionApp:     "grpc-hello-world",
				Desc:            "mongo builtin service",
				ProfileName:     "dev-single",
			},
		},
		{name: "nil workload", wantErr: true},
		{
			name: "missing service name",
			input: &MongoDBWorkload{
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "mongo builtin service",
				ProfileName:     "dev-single",
			},
			wantErr: true,
		},
		{
			name: "missing desc",
			input: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				ProfileName:     "dev-single",
			},
			wantErr: true,
		},
		{
			name: "missing profile name",
			input: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "mongo builtin service",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() failed: %v", err)
			}
		})
	}
}

func TestMongoDBWorkloadResourceName(t *testing.T) {
	tests := []struct {
		name  string
		input *MongoDBWorkload
		want  string
	}{
		{
			name: "valid",
			input: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "Dev",
				App:             "GRPC_HELLO.WORLD",
				DominionApp:     "grpc-hello-world",
			},
			want: "mongo-dev-mongo-main-" + shortNameHash("GRPC_HELLO.WORLD", "grpc-hello-world"),
		},
		{name: "nil workload", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.ResourceName(); got != tt.want {
				t.Fatalf("ResourceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMongoDBSecretWorkloadValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   *MongoDBSecretWorkload
		wantErr bool
	}{
		{
			name: "valid",
			input: &MongoDBSecretWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				DominionApp:     "grpc-hello-world",
			},
		},
		{name: "nil workload", wantErr: true},
		{
			name: "missing app",
			input: &MongoDBSecretWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() failed: %v", err)
			}
		})
	}
}

func TestMongoDBSecretWorkloadResourceName(t *testing.T) {
	tests := []struct {
		name  string
		input *MongoDBSecretWorkload
		want  string
	}{
		{
			name: "valid",
			input: &MongoDBSecretWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "Dev",
				App:             "grpc-hello-world",
				DominionApp:     "grpc-hello-world",
			},
			want: "secret-dev-mongo-main-" + shortNameHash("grpc-hello-world", "grpc-hello-world"),
		},
		{name: "nil workload", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.ResourceName(); got != tt.want {
				t.Fatalf("ResourceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMongoDBPVCWorkloadValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   *MongoDBPVCWorkload
		wantErr bool
	}{
		{
			name: "valid",
			input: &MongoDBPVCWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				DominionApp:     "grpc-hello-world",
			},
		},
		{name: "nil workload", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() failed: %v", err)
			}
		})
	}
}

func TestMongoDBPVCWorkloadResourceName(t *testing.T) {
	tests := []struct {
		name  string
		input *MongoDBPVCWorkload
		want  string
	}{
		{
			name: "valid",
			input: &MongoDBPVCWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "Dev",
				App:             "grpc-hello-world",
				DominionApp:     "grpc-hello-world",
			},
			want: "pvc-dev-mongo-main-" + shortNameHash("grpc-hello-world", "grpc-hello-world"),
		},
		{name: "nil workload", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.ResourceName(); got != tt.want {
				t.Fatalf("ResourceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_generateStablePassword(t *testing.T) {
	base62Pattern := regexp.MustCompile(`^[a-zA-Z0-9]+$`)

	passwordA := generateStablePassword("grpc-hello-world", "dev", "mongo-main")
	passwordB := generateStablePassword("grpc-hello-world", "dev", "mongo-main")
	passwordC := generateStablePassword("grpc-hello-world", "dev", "mongo-replica")

	if passwordA != passwordB {
		t.Fatalf("generateStablePassword() should be deterministic: %q != %q", passwordA, passwordB)
	}
	if passwordA == passwordC {
		t.Fatalf("generateStablePassword() should vary with different inputs: %q == %q", passwordA, passwordC)
	}
	if len(passwordA) < mongoPasswordMinLen {
		t.Fatalf("generateStablePassword() len = %d, want >= %d", len(passwordA), mongoPasswordMinLen)
	}
	if !base62Pattern.MatchString(passwordA) {
		t.Fatalf("generateStablePassword() = %q, want base62 characters only", passwordA)
	}
}

func Test_newMongoDBWorkload(t *testing.T) {
	tests := []struct {
		name    string
		infra   config.DeployInfra
		cfg     *K8sConfig
		want    *MongoDBWorkload
		wantErr bool
	}{
		{
			name: "valid",
			infra: config.DeployInfra{
				Resource: "mongo",
				Profile:  "dev-single",
				Name:     "mongo-main",
				Persistence: config.DeployInfraPersistence{
					Enabled: true,
				},
			},
			cfg: &K8sConfig{
				MongoDB: map[string]*MongoProfileConfig{
					"dev-single": {
						Image:         "mongo",
						Version:       "7.0",
						Port:          27017,
						AdminUsername: "admin",
						Storage:       MongoStorageConfig{StorageClassName: "local-path", Capacity: "1Gi", AccessModes: []string{"ReadWriteOnce"}, VolumeMode: "Filesystem"},
					},
				},
			},
			want: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				DominionApp:     "grpc-hello-world",
				Desc:            mongoResourceDesc,
				ProfileName:     "dev-single",
				Persistence:     config.DeployInfraPersistence{Enabled: true},
			},
		},
		{
			name: "missing profile",
			infra: config.DeployInfra{
				Profile: "missing",
				Name:    "mongo-main",
			},
			cfg:     &K8sConfig{MongoDB: map[string]*MongoProfileConfig{}},
			wantErr: true,
		},
		{
			name: "validation failure",
			infra: config.DeployInfra{
				Profile: "dev-single",
				Name:    "",
			},
			cfg: &K8sConfig{
				MongoDB: map[string]*MongoProfileConfig{
					"dev-single": {
						Image:         "mongo",
						Version:       "7.0",
						Port:          27017,
						AdminUsername: "admin",
						Storage:       MongoStorageConfig{StorageClassName: "local-path", Capacity: "1Gi", AccessModes: []string{"ReadWriteOnce"}, VolumeMode: "Filesystem"},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoadK8sConfig(t, tt.cfg)

			got, err := newMongoDBWorkload(tt.infra, "dev", "grpc-hello-world")
			if tt.wantErr {
				if err == nil {
					t.Fatal("newMongoDBWorkload() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("newMongoDBWorkload() failed: %v", err)
			}
			if got == nil {
				t.Fatal("newMongoDBWorkload() returned nil workload")
			}
			if got.ServiceName != tt.want.ServiceName {
				t.Fatalf("ServiceName = %q, want %q", got.ServiceName, tt.want.ServiceName)
			}
			if got.EnvironmentName != tt.want.EnvironmentName {
				t.Fatalf("EnvironmentName = %q, want %q", got.EnvironmentName, tt.want.EnvironmentName)
			}
			if got.App != tt.want.App {
				t.Fatalf("App = %q, want %q", got.App, tt.want.App)
			}
			if got.DominionApp != tt.want.DominionApp {
				t.Fatalf("DominionApp = %q, want %q", got.DominionApp, tt.want.DominionApp)
			}
			if got.Desc != tt.want.Desc {
				t.Fatalf("Desc = %q, want %q", got.Desc, tt.want.Desc)
			}
			if got.ProfileName != tt.want.ProfileName {
				t.Fatalf("ProfileName = %q, want %q", got.ProfileName, tt.want.ProfileName)
			}
			if got.Persistence != tt.want.Persistence {
				t.Fatalf("Persistence = %#v, want %#v", got.Persistence, tt.want.Persistence)
			}
		})
	}
}
