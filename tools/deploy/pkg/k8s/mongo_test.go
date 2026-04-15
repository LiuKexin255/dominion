package k8s

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"dominion/tools/deploy/pkg/config"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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
				ProfileName:     "dev-single",
			},
		},
		{name: "nil workload", wantErr: true},
		{
			name: "missing service name",
			input: &MongoDBWorkload{
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
			},
			want: "mongo-grpc-hello-world-mongo-main-" + shortNameHash("Dev"),
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

func TestMongoDBWorkloadSecretResourceName(t *testing.T) {
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
				App:             "grpc-hello-world",
			},
			want: "secret-grpc-hello-world-mongo-main-" + shortNameHash("Dev"),
		},
		{name: "nil workload", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.SecretResourceName(); got != tt.want {
				t.Fatalf("SecretResourceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMongoDBWorkloadPVCResourceName(t *testing.T) {
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
				App:             "grpc-hello-world",
				ProfileName:     "dev-single",
			},
			want: "pvc-grpc-hello-world-mongo-main-" + shortNameHash("Dev"),
		},
		{name: "nil workload", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.PVCResourceName(); got != tt.want {
				t.Fatalf("PVCResourceName() = %q, want %q", got, tt.want)
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
				App:      "grpc-hello-world",
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
				ProfileName:     "dev-single",
				Persistence:     config.DeployInfraPersistence{Enabled: true},
			},
		},
		{
			name: "profile not in config still creates workload",
			infra: config.DeployInfra{
				Profile: "missing",
				Name:    "mongo-main",
				App:     "grpc-hello-world",
			},
			cfg: &K8sConfig{MongoDB: map[string]*MongoProfileConfig{}},
			want: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				ProfileName:     "missing",
				Persistence:     config.DeployInfraPersistence{},
			},
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

			got, err := newMongoDBWorkload(tt.infra, "dev")
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
			if got.ProfileName != tt.want.ProfileName {
				t.Fatalf("ProfileName = %q, want %q", got.ProfileName, tt.want.ProfileName)
			}
			if got.Persistence != tt.want.Persistence {
				t.Fatalf("Persistence = %#v, want %#v", got.Persistence, tt.want.Persistence)
			}
		})
	}
}

func TestMongoDBWorkloadServiceResourceName(t *testing.T) {
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
			},
			want: "svc-grpc-hello-world-mongo-main-" + shortNameHash("Dev"),
		},
		{name: "nil workload", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.ServiceResourceName(); got != tt.want {
				t.Fatalf("ServiceResourceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildMongoDBService(t *testing.T) {
	tests := []struct {
		name        string
		workload    *MongoDBWorkload
		k8sConfig   *K8sConfig
		wantErr     bool
		errContains string
		want        *mongoDBServiceExpectation
	}{
		{
			name:     "success",
			workload: newTestMongoDBWorkload(),
			k8sConfig: &K8sConfig{
				Namespace: "team-dev",
				ManagedBy: "deploy-tool",
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
			want: &mongoDBServiceExpectation{
				name:                newTestMongoDBWorkload().ServiceResourceName(),
				namespace:           "team-dev",
				managedBy:           "deploy-tool",
				app:                 "grpc-hello-world",
				serviceName:         "mongo-main",
				environment:         "dev",
				selectorServiceName: "mongo-main",
				ports: []corev1.ServicePort{
					{Name: "mongo", Port: 27017, TargetPort: intstr.FromString("mongo")},
				},
			},
		},
		{
			name:        "nil workload returns error",
			workload:    nil,
			k8sConfig:   newTestK8sConfig(),
			wantErr:     true,
			errContains: "mongo workload 为空",
		},
		{
			name: "missing profile returns error",
			workload: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				ProfileName:     "nonexistent",
			},
			k8sConfig:   &K8sConfig{Namespace: "team-dev", ManagedBy: "deploy-tool", MongoDB: map[string]*MongoProfileConfig{}},
			wantErr:     true,
			errContains: "mongo profile nonexistent 不存在",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoadK8sConfig(t, tt.k8sConfig)

			got, err := BuildMongoDBService(tt.workload)
			if tt.wantErr {
				if err == nil {
					t.Fatal("BuildMongoDBService() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}
				if got != nil {
					t.Fatal("BuildMongoDBService() returned object on failure")
				}

				return
			}

			if err != nil {
				t.Fatalf("BuildMongoDBService() failed: %v", err)
			}

			assertMongoDBService(t, got, tt.want)
		})
	}
}

type mongoDBServiceExpectation struct {
	name                string
	namespace           string
	managedBy           string
	app                 string
	serviceName         string
	environment         string
	selectorServiceName string
	ports               []corev1.ServicePort
}

func assertMongoDBService(t *testing.T, got *corev1.Service, want *mongoDBServiceExpectation) {
	t.Helper()

	if got.Name != want.name {
		t.Fatalf("name = %q, want %q", got.Name, want.name)
	}
	if got.Namespace != want.namespace {
		t.Fatalf("namespace = %q, want %q", got.Namespace, want.namespace)
	}
	assertMongoDBManagedLabels(t, got.Labels, want.app, want.serviceName, want.environment, want.managedBy)
	assertMongoDBSelectorLabels(t, got.Spec.Selector, want.app, want.selectorServiceName, want.environment)
	if len(got.Spec.Ports) != len(want.ports) {
		t.Fatalf("ports len = %d, want %d", len(got.Spec.Ports), len(want.ports))
	}
	for i := range want.ports {
		if got.Spec.Ports[i].Name != want.ports[i].Name || got.Spec.Ports[i].Port != want.ports[i].Port || got.Spec.Ports[i].TargetPort != want.ports[i].TargetPort {
			t.Fatalf("ports[%d] = %#v, want %#v", i, got.Spec.Ports[i], want.ports[i])
		}
	}
	if got.Spec.Type != "" {
		t.Fatalf("service type = %q, want empty (ClusterIP default)", got.Spec.Type)
	}
}

func assertMongoDBManagedLabels(t *testing.T, got map[string]string, app string, serviceName string, dominionEnvironment string, managedBy string) {
	t.Helper()

	if _, ok := got["app"]; ok {
		t.Fatalf("unexpected legacy app label key present")
	}
	if _, ok := got["service"]; ok {
		t.Fatalf("unexpected legacy service label key present")
	}
	if _, ok := got["environment"]; ok {
		t.Fatalf("unexpected legacy environment label key present")
	}
	if got[appLabelKey] != app {
		t.Fatalf("app label = %q, want %q", got[appLabelKey], app)
	}
	if got[serviceLabelKey] != serviceName {
		t.Fatalf("service label = %q, want %q", got[serviceLabelKey], serviceName)
	}
	if got[dominionEnvironmentLabelKey] != dominionEnvironment {
		t.Fatalf("dominion environment label = %q, want %q", got[dominionEnvironmentLabelKey], dominionEnvironment)
	}
	if _, ok := got["managed-by"]; ok {
		t.Fatalf("unexpected legacy managed-by label key present")
	}
	if got[managedByLabelKey] != managedBy {
		t.Fatalf("managed-by label = %q, want %q", got[managedByLabelKey], managedBy)
	}
	if len(got) != 4 {
		t.Fatalf("managed labels len = %d, want 4", len(got))
	}
}

func assertMongoDBSelectorLabels(t *testing.T, got map[string]string, app string, serviceName string, dominionEnvironment string) {
	t.Helper()

	if _, ok := got["app"]; ok {
		t.Fatalf("unexpected legacy app label key present")
	}
	if _, ok := got["service"]; ok {
		t.Fatalf("unexpected legacy service label key present")
	}
	if _, ok := got["environment"]; ok {
		t.Fatalf("unexpected legacy environment label key present")
	}
	if got[appLabelKey] != app {
		t.Fatalf("app label = %q, want %q", got[appLabelKey], app)
	}
	if got[serviceLabelKey] != serviceName {
		t.Fatalf("service label = %q, want %q", got[serviceLabelKey], serviceName)
	}
	if got[dominionEnvironmentLabelKey] != dominionEnvironment {
		t.Fatalf("dominion environment label = %q, want %q", got[dominionEnvironmentLabelKey], dominionEnvironment)
	}
	if len(got) != 3 {
		t.Fatalf("selector labels len = %d, want 3", len(got))
	}
}

func newTestMongoDBWorkload() *MongoDBWorkload {
	return &MongoDBWorkload{
		ServiceName:     "mongo-main",
		EnvironmentName: "dev",
		App:             "grpc-hello-world",
		ProfileName:     "dev-single",
	}
}

func newTestK8sConfigWithMongoProfile() *K8sConfig {
	return &K8sConfig{
		Namespace: "team-dev",
		ManagedBy: "deploy-tool",
		MongoDB: map[string]*MongoProfileConfig{
			"dev-single": {
				Image:         "mongo",
				Version:       "7.0",
				Port:          27017,
				AdminUsername: "admin",
				Security:      MongoSecurityConfig{RunAsUser: 1000, RunAsGroup: 3000},
				Storage:       MongoStorageConfig{StorageClassName: "local-path", Capacity: "1Gi", AccessModes: []string{"ReadWriteOnce"}, VolumeMode: "Filesystem"},
			},
		},
	}
}

func TestBuildMongoDBSecret(t *testing.T) {
	tests := []struct {
		name        string
		workload    *MongoDBWorkload
		k8sConfig   *K8sConfig
		wantErr     bool
		errContains string
		want        *mongoDBSecretExpectation
	}{
		{
			name:     "success",
			workload: newTestMongoDBWorkload(),
			k8sConfig: &K8sConfig{
				Namespace: "team-dev",
				ManagedBy: "deploy-tool",
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
			want: &mongoDBSecretExpectation{
				name:        newTestMongoDBWorkload().SecretResourceName(),
				namespace:   "team-dev",
				managedBy:   "deploy-tool",
				app:         "grpc-hello-world",
				serviceName: "mongo-main",
				environment: "dev",
				secretType:  corev1.SecretTypeOpaque,
				username:    "admin",
				password:    generateStablePassword(newTestMongoDBWorkload().App, "dev", "mongo-main"),
			},
		},
		{
			name: "password uses service app instead of dominion app",
			workload: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "service-app",
				ProfileName:     "dev-single",
			},
			k8sConfig: &K8sConfig{
				Namespace: "team-dev",
				ManagedBy: "deploy-tool",
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
			want: &mongoDBSecretExpectation{
				name:        (&MongoDBWorkload{ServiceName: "mongo-main", EnvironmentName: "dev", App: "service-app"}).SecretResourceName(),
				namespace:   "team-dev",
				managedBy:   "deploy-tool",
				app:         "service-app",
				serviceName: "mongo-main",
				environment: "dev",
				secretType:  corev1.SecretTypeOpaque,
				username:    "admin",
				password:    generateStablePassword("service-app", "dev", "mongo-main"),
			},
		},
		{
			name:        "nil workload returns error",
			workload:    nil,
			k8sConfig:   newTestK8sConfig(),
			wantErr:     true,
			errContains: "mongo workload 为空",
		},
		{
			name: "missing profile returns error",
			workload: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				ProfileName:     "nonexistent",
			},
			k8sConfig:   &K8sConfig{Namespace: "team-dev", ManagedBy: "deploy-tool", MongoDB: map[string]*MongoProfileConfig{}},
			wantErr:     true,
			errContains: "mongo profile nonexistent 不存在",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoadK8sConfig(t, tt.k8sConfig)

			got, err := BuildMongoDBSecret(tt.workload)
			if tt.wantErr {
				if err == nil {
					t.Fatal("BuildMongoDBSecret() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}
				if got != nil {
					t.Fatal("BuildMongoDBSecret() returned object on failure")
				}

				return
			}

			if err != nil {
				t.Fatalf("BuildMongoDBSecret() failed: %v", err)
			}

			assertMongoDBSecret(t, got, tt.want)
		})
	}
}

func TestBuildMongoDBPVC(t *testing.T) {
	tests := []struct {
		name        string
		workload    *MongoDBWorkload
		k8sConfig   *K8sConfig
		wantErr     bool
		errContains string
		want        *mongoDBPVCExpectation
	}{
		{
			name:      "success",
			workload:  newTestMongoDBWorkload(),
			k8sConfig: newTestK8sConfigWithMongoProfile(),
			want: &mongoDBPVCExpectation{
				name:             newTestMongoDBWorkload().PVCResourceName(),
				namespace:        "team-dev",
				managedBy:        "deploy-tool",
				app:              "grpc-hello-world",
				serviceName:      "mongo-main",
				environment:      "dev",
				storageClassName: "local-path",
				accessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				volumeMode:       corev1.PersistentVolumeFilesystem,
				storageCapacity:  resource.MustParse("1Gi"),
			},
		},
		{
			name:        "nil workload returns error",
			workload:    nil,
			k8sConfig:   newTestK8sConfigWithMongoProfile(),
			wantErr:     true,
			errContains: "mongo workload 为空",
		},
		{
			name: "missing profile returns error",
			workload: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				ProfileName:     "nonexistent",
			},
			k8sConfig:   &K8sConfig{Namespace: "team-dev", ManagedBy: "deploy-tool", MongoDB: map[string]*MongoProfileConfig{}},
			wantErr:     true,
			errContains: "mongo profile nonexistent 不存在",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoadK8sConfig(t, tt.k8sConfig)

			got, err := BuildMongoDBPVC(tt.workload)
			if tt.wantErr {
				if err == nil {
					t.Fatal("BuildMongoDBPVC() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}
				if got != nil {
					t.Fatal("BuildMongoDBPVC() returned object on failure")
				}

				return
			}

			if err != nil {
				t.Fatalf("BuildMongoDBPVC() failed: %v", err)
			}

			assertMongoDBPVC(t, got, tt.want)
		})
	}
}

func TestCheckPVCCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		existing    *corev1.PersistentVolumeClaim
		desired     *MongoDBWorkload
		k8sConfig   *K8sConfig
		wantErr     bool
		errContains string
	}{
		{
			name:      "compatible pvc",
			existing:  newCompatibleMongoPVC(),
			desired:   newTestMongoDBWorkload(),
			k8sConfig: newTestK8sConfigWithMongoProfile(),
		},
		{
			name:      "custom label ignored",
			existing:  newMongoPVCWithMutation(func(pvc *corev1.PersistentVolumeClaim) { pvc.Labels["custom-label"] = "other-app" }),
			desired:   newTestMongoDBWorkload(),
			k8sConfig: newTestK8sConfigWithMongoProfile(),
		},
		{
			name:        "dominion environment mismatch",
			existing:    newMongoPVCWithMutation(func(pvc *corev1.PersistentVolumeClaim) { pvc.Labels[dominionEnvironmentLabelKey] = "prod" }),
			desired:     newTestMongoDBWorkload(),
			k8sConfig:   newTestK8sConfigWithMongoProfile(),
			wantErr:     true,
			errContains: dominionEnvironmentLabelKey,
		},
		{
			name: "storage class mismatch",
			existing: newMongoPVCWithMutation(func(pvc *corev1.PersistentVolumeClaim) {
				storageClassName := "remote-path"
				pvc.Spec.StorageClassName = &storageClassName
			}),
			desired:     newTestMongoDBWorkload(),
			k8sConfig:   newTestK8sConfigWithMongoProfile(),
			wantErr:     true,
			errContains: "storageClassName 不兼容",
		},
		{
			name: "access modes mismatch",
			existing: newMongoPVCWithMutation(func(pvc *corev1.PersistentVolumeClaim) {
				pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadOnlyMany}
			}),
			desired:     newTestMongoDBWorkload(),
			k8sConfig:   newTestK8sConfigWithMongoProfile(),
			wantErr:     true,
			errContains: "accessModes 不兼容",
		},
		{
			name: "volume mode mismatch",
			existing: newMongoPVCWithMutation(func(pvc *corev1.PersistentVolumeClaim) {
				volumeMode := corev1.PersistentVolumeBlock
				pvc.Spec.VolumeMode = &volumeMode
			}),
			desired:     newTestMongoDBWorkload(),
			k8sConfig:   newTestK8sConfigWithMongoProfile(),
			wantErr:     true,
			errContains: "volumeMode 不兼容",
		},
		{
			name: "existing capacity exceeds desired",
			existing: newMongoPVCWithMutation(func(pvc *corev1.PersistentVolumeClaim) {
				pvc.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("2Gi")
			}),
			desired:     newTestMongoDBWorkload(),
			k8sConfig:   newTestK8sConfigWithMongoProfile(),
			wantErr:     true,
			errContains: "storage capacity 不兼容",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoadK8sConfig(t, tt.k8sConfig)

			err := CheckPVCCompatibility(tt.existing, tt.desired)
			if tt.wantErr {
				if err == nil {
					t.Fatal("CheckPVCCompatibility() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}

				return
			}

			if err != nil {
				t.Fatalf("CheckPVCCompatibility() failed: %v", err)
			}
		})
	}
}

type mongoDBPVCExpectation struct {
	name             string
	namespace        string
	managedBy        string
	app              string
	serviceName      string
	environment      string
	storageClassName string
	accessModes      []corev1.PersistentVolumeAccessMode
	volumeMode       corev1.PersistentVolumeMode
	storageCapacity  resource.Quantity
}

func assertMongoDBPVC(t *testing.T, got *corev1.PersistentVolumeClaim, want *mongoDBPVCExpectation) {
	t.Helper()

	if got.Name != want.name {
		t.Fatalf("name = %q, want %q", got.Name, want.name)
	}
	if got.Namespace != want.namespace {
		t.Fatalf("namespace = %q, want %q", got.Namespace, want.namespace)
	}
	assertMongoDBManagedLabels(t, got.Labels, want.app, want.serviceName, want.environment, want.managedBy)
	if got.Spec.StorageClassName == nil || *got.Spec.StorageClassName != want.storageClassName {
		t.Fatalf("storageClassName = %v, want %q", got.Spec.StorageClassName, want.storageClassName)
	}
	if !slices.Equal(got.Spec.AccessModes, want.accessModes) {
		t.Fatalf("accessModes = %v, want %v", got.Spec.AccessModes, want.accessModes)
	}
	if got.Spec.VolumeMode == nil || *got.Spec.VolumeMode != want.volumeMode {
		t.Fatalf("volumeMode = %v, want %q", got.Spec.VolumeMode, want.volumeMode)
	}
	storageCapacity, ok := got.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok {
		t.Fatal("storage request missing")
	}
	if storageCapacity.Cmp(want.storageCapacity) != 0 {
		t.Fatalf("storageCapacity = %s, want %s", storageCapacity.String(), want.storageCapacity.String())
	}
}

func newCompatibleMongoPVC() *corev1.PersistentVolumeClaim {
	storageClassName := "local-path"
	volumeMode := corev1.PersistentVolumeFilesystem

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      newTestMongoDBWorkload().PVCResourceName(),
			Namespace: "team-dev",
			Labels: map[string]string(buildLabels(
				withApp("grpc-hello-world"),
				withService("mongo-main"),
				withDominionEnvironment("dev"),
				withManagedBy("deploy-tool"),
			)),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeMode:       &volumeMode,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
}

func newMongoPVCWithMutation(mutate func(*corev1.PersistentVolumeClaim)) *corev1.PersistentVolumeClaim {
	pvc := newCompatibleMongoPVC()
	if mutate != nil {
		mutate(pvc)
	}

	return pvc
}

type mongoDBSecretExpectation struct {
	name        string
	namespace   string
	managedBy   string
	app         string
	serviceName string
	environment string
	secretType  corev1.SecretType
	username    string
	password    string
}

func assertMongoDBSecret(t *testing.T, got *corev1.Secret, want *mongoDBSecretExpectation) {
	t.Helper()

	if got.Name != want.name {
		t.Fatalf("name = %q, want %q", got.Name, want.name)
	}
	if got.Namespace != want.namespace {
		t.Fatalf("namespace = %q, want %q", got.Namespace, want.namespace)
	}
	assertMongoDBManagedLabels(t, got.Labels, want.app, want.serviceName, want.environment, want.managedBy)
	if got.Type != want.secretType {
		t.Fatalf("type = %q, want %q", got.Type, want.secretType)
	}
	if len(got.Data) != 2 {
		t.Fatalf("data len = %d, want 2", len(got.Data))
	}
	if string(got.Data[mongoSecretUsernameKey]) != want.username {
		t.Fatalf("username = %q, want %q", string(got.Data[mongoSecretUsernameKey]), want.username)
	}
	if string(got.Data[mongoSecretPasswordKey]) != want.password {
		t.Fatalf("password = %q, want %q", string(got.Data[mongoSecretPasswordKey]), want.password)
	}
}

func TestBuildMongoDBDeployment(t *testing.T) {
	runAsUser := int64(1000)
	runAsGroup := int64(3000)

	tests := []struct {
		name        string
		workload    *MongoDBWorkload
		k8sConfig   *K8sConfig
		wantErr     bool
		errContains string
		want        *mongoDBDeploymentExpectation
	}{
		{
			name:      "success",
			workload:  newTestMongoDBWorkload(),
			k8sConfig: newTestK8sConfigWithMongoProfile(),
			want: &mongoDBDeploymentExpectation{
				name:        newTestMongoDBWorkload().ResourceName(),
				namespace:   "team-dev",
				managedBy:   "deploy-tool",
				app:         "grpc-hello-world",
				serviceName: "mongo-main",
				environment: "dev",
				replicas:    1,
				image:       "mongo:7.0",
				securityContext: &corev1.PodSecurityContext{
					RunAsUser:  &runAsUser,
					RunAsGroup: &runAsGroup,
				},
				ports: []corev1.ContainerPort{{
					Name:          mongoPortName,
					ContainerPort: 27017,
				}},
				volumes: []corev1.Volume{{
					Name: mongoDataVolumeName,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: newTestMongoDBWorkload().PVCResourceName(),
						},
					},
				}},
				volumeMounts: []corev1.VolumeMount{{
					Name:      mongoDataVolumeName,
					MountPath: mongoDataMountPath,
				}},
				env: []corev1.EnvVar{
					{Name: reservedEnvNameServiceApp, Value: "grpc-hello-world"},
					{Name: reservedEnvNameDominionEnvironment, Value: "dev"},
					{
						Name:      reservedEnvNamePodNamespace,
						ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: mongoPodFieldPathNS}},
					},
					{
						Name: mongoEnvRootUsername,
						ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: newTestMongoDBWorkload().SecretResourceName()},
							Key:                  mongoSecretUsernameKey,
						}},
					},
					{
						Name: mongoEnvRootPassword,
						ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: newTestMongoDBWorkload().SecretResourceName()},
							Key:                  mongoSecretPasswordKey,
						}},
					},
				},
			},
		},
		{
			name: "deployment env uses service app",
			workload: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "service-app",
				ProfileName:     "dev-single",
			},
			k8sConfig: newTestK8sConfigWithMongoProfile(),
			want: &mongoDBDeploymentExpectation{
				name:        (&MongoDBWorkload{ServiceName: "mongo-main", EnvironmentName: "dev", App: "service-app", ProfileName: "dev-single"}).ResourceName(),
				namespace:   "team-dev",
				managedBy:   "deploy-tool",
				app:         "service-app",
				serviceName: "mongo-main",
				environment: "dev",
				replicas:    1,
				image:       "mongo:7.0",
				securityContext: &corev1.PodSecurityContext{
					RunAsUser:  &runAsUser,
					RunAsGroup: &runAsGroup,
				},
				ports: []corev1.ContainerPort{{
					Name:          mongoPortName,
					ContainerPort: 27017,
				}},
				volumes: []corev1.Volume{{
					Name: mongoDataVolumeName,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: (&MongoDBWorkload{ServiceName: "mongo-main", EnvironmentName: "dev", App: "service-app"}).PVCResourceName(),
						},
					},
				}},
				volumeMounts: []corev1.VolumeMount{{
					Name:      mongoDataVolumeName,
					MountPath: mongoDataMountPath,
				}},
				env: []corev1.EnvVar{
					{Name: reservedEnvNameServiceApp, Value: "service-app"},
					{Name: reservedEnvNameDominionEnvironment, Value: "dev"},
					{
						Name:      reservedEnvNamePodNamespace,
						ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: mongoPodFieldPathNS}},
					},
					{
						Name: mongoEnvRootUsername,
						ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: (&MongoDBWorkload{ServiceName: "mongo-main", EnvironmentName: "dev", App: "service-app"}).SecretResourceName()},
							Key:                  mongoSecretUsernameKey,
						}},
					},
					{
						Name: mongoEnvRootPassword,
						ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: (&MongoDBWorkload{ServiceName: "mongo-main", EnvironmentName: "dev", App: "service-app"}).SecretResourceName()},
							Key:                  mongoSecretPasswordKey,
						}},
					},
				},
			},
		},
		{
			name:        "nil workload returns error",
			workload:    nil,
			k8sConfig:   newTestK8sConfigWithMongoProfile(),
			wantErr:     true,
			errContains: "mongo workload 为空",
		},
		{
			name: "missing profile returns error",
			workload: &MongoDBWorkload{
				ServiceName:     "mongo-main",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				ProfileName:     "nonexistent",
			},
			k8sConfig:   &K8sConfig{Namespace: "team-dev", ManagedBy: "deploy-tool", MongoDB: map[string]*MongoProfileConfig{}},
			wantErr:     true,
			errContains: "mongo profile nonexistent 不存在",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoadK8sConfig(t, tt.k8sConfig)

			got, err := BuildMongoDBDeployment(tt.workload)
			if tt.wantErr {
				if err == nil {
					t.Fatal("BuildMongoDBDeployment() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}
				if got != nil {
					t.Fatal("BuildMongoDBDeployment() returned object on failure")
				}

				return
			}

			if err != nil {
				t.Fatalf("BuildMongoDBDeployment() failed: %v", err)
			}

			assertMongoDBDeployment(t, got, tt.want)
		})
	}
}

type mongoDBDeploymentExpectation struct {
	name            string
	namespace       string
	managedBy       string
	app             string
	serviceName     string
	environment     string
	replicas        int32
	image           string
	securityContext *corev1.PodSecurityContext
	ports           []corev1.ContainerPort
	volumes         []corev1.Volume
	volumeMounts    []corev1.VolumeMount
	env             []corev1.EnvVar
}

func assertMongoDBDeployment(t *testing.T, got *appsv1.Deployment, want *mongoDBDeploymentExpectation) {
	t.Helper()

	if got.Name != want.name {
		t.Fatalf("name = %q, want %q", got.Name, want.name)
	}
	if got.Namespace != want.namespace {
		t.Fatalf("namespace = %q, want %q", got.Namespace, want.namespace)
	}
	assertMongoDBManagedLabels(t, got.Labels, want.app, want.serviceName, want.environment, want.managedBy)
	assertMongoDBSelectorLabels(t, got.Spec.Selector.MatchLabels, want.app, want.serviceName, want.environment)
	assertMongoDBManagedLabels(t, got.Spec.Template.Labels, want.app, want.serviceName, want.environment, want.managedBy)
	if got.Spec.Replicas == nil || *got.Spec.Replicas != want.replicas {
		t.Fatalf("replicas = %v, want %d", got.Spec.Replicas, want.replicas)
	}
	assertMongoDBPodSecurityContext(t, got.Spec.Template.Spec.SecurityContext, want.securityContext)
	assertVolumes(t, got.Spec.Template.Spec.Volumes, want.volumes)

	if len(got.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("init containers len = %d, want 1", len(got.Spec.Template.Spec.InitContainers))
	}
	initContainer := got.Spec.Template.Spec.InitContainers[0]
	if initContainer.Name != mongoInitContainerName {
		t.Fatalf("init container name = %q, want %q", initContainer.Name, mongoInitContainerName)
	}
	if initContainer.Image != want.image {
		t.Fatalf("init container image = %q, want %q", initContainer.Image, want.image)
	}
	if len(initContainer.Command) != 3 {
		t.Fatalf("init container command len = %d, want 3", len(initContainer.Command))
	}
	if initContainer.Command[0] != "bash" || initContainer.Command[1] != "-ec" || initContainer.Command[2] != mongoInitScript {
		t.Fatalf("init container command = %#v, want bash -ec mongoInitScript", initContainer.Command)
	}
	assertVolumeMounts(t, initContainer.VolumeMounts, want.volumeMounts)
	assertMongoDBContainerEnv(t, initContainer.Env, want.env)
	if !strings.Contains(initContainer.Command[2], mongoInitMarkerFile) {
		t.Fatalf("init script missing marker file check: %q", initContainer.Command[2])
	}
	if !strings.Contains(initContainer.Command[2], "createUser") {
		t.Fatalf("init script missing createUser: %q", initContainer.Command[2])
	}
	if !strings.Contains(initContainer.Command[2], "--username \"$MONGO_INITDB_ROOT_USERNAME\"") {
		t.Fatalf("init script missing credential validation: %q", initContainer.Command[2])
	}

	if len(got.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("containers len = %d, want 1", len(got.Spec.Template.Spec.Containers))
	}
	container := got.Spec.Template.Spec.Containers[0]
	if container.Name != want.name {
		t.Fatalf("container name = %q, want %q", container.Name, want.name)
	}
	if container.Image != want.image {
		t.Fatalf("image = %q, want %q", container.Image, want.image)
	}
	if len(container.Ports) != len(want.ports) {
		t.Fatalf("ports len = %d, want %d", len(container.Ports), len(want.ports))
	}
	for i := range want.ports {
		if container.Ports[i] != want.ports[i] {
			t.Fatalf("ports[%d] = %#v, want %#v", i, container.Ports[i], want.ports[i])
		}
	}
	assertVolumeMounts(t, container.VolumeMounts, want.volumeMounts)
	assertMongoDBContainerEnv(t, container.Env, want.env)
	assertMongoDBProbe(t, container.LivenessProbe, "liveness")
	assertMongoDBProbe(t, container.ReadinessProbe, "readiness")
}

func assertMongoDBPodSecurityContext(t *testing.T, got *corev1.PodSecurityContext, want *corev1.PodSecurityContext) {
	t.Helper()

	if want == nil {
		if got != nil {
			t.Fatalf("pod securityContext = %#v, want nil", got)
		}
		return
	}
	if got == nil {
		t.Fatal("pod securityContext = nil, want non-nil")
	}
	if got.RunAsUser == nil || *got.RunAsUser != *want.RunAsUser {
		t.Fatalf("pod securityContext.runAsUser = %v, want %d", got.RunAsUser, *want.RunAsUser)
	}
	if got.RunAsGroup == nil || *got.RunAsGroup != *want.RunAsGroup {
		t.Fatalf("pod securityContext.runAsGroup = %v, want %d", got.RunAsGroup, *want.RunAsGroup)
	}
}

func assertMongoDBContainerEnv(t *testing.T, got []corev1.EnvVar, want []corev1.EnvVar) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("env len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Fatalf("env[%d].name = %q, want %q", i, got[i].Name, want[i].Name)
		}
		if got[i].Value != want[i].Value {
			t.Fatalf("env[%d].value = %q, want %q", i, got[i].Value, want[i].Value)
		}
		assertMongoDBEnvValueSource(t, got[i].ValueFrom, want[i].ValueFrom, i)
	}
}

func assertMongoDBEnvValueSource(t *testing.T, got *corev1.EnvVarSource, want *corev1.EnvVarSource, index int) {
	t.Helper()

	if want == nil {
		if got != nil {
			t.Fatalf("env[%d].valueFrom = %#v, want nil", index, got)
		}
		return
	}
	if got == nil {
		t.Fatalf("env[%d].valueFrom = nil, want non-nil", index)
	}
	if want.FieldRef != nil {
		if got.FieldRef == nil {
			t.Fatalf("env[%d].fieldRef = nil, want non-nil", index)
		}
		if got.FieldRef.FieldPath != want.FieldRef.FieldPath {
			t.Fatalf("env[%d].fieldRef.fieldPath = %q, want %q", index, got.FieldRef.FieldPath, want.FieldRef.FieldPath)
		}
		return
	}
	if want.SecretKeyRef != nil {
		if got.SecretKeyRef == nil {
			t.Fatalf("env[%d].secretKeyRef = nil, want non-nil", index)
		}
		if got.SecretKeyRef.Name != want.SecretKeyRef.Name {
			t.Fatalf("env[%d].secretKeyRef.name = %q, want %q", index, got.SecretKeyRef.Name, want.SecretKeyRef.Name)
		}
		if got.SecretKeyRef.Key != want.SecretKeyRef.Key {
			t.Fatalf("env[%d].secretKeyRef.key = %q, want %q", index, got.SecretKeyRef.Key, want.SecretKeyRef.Key)
		}
		return
	}

	t.Fatalf("env[%d].valueFrom expected fieldRef or secretKeyRef", index)
}

func assertMongoDBProbe(t *testing.T, got *corev1.Probe, probeName string) {
	t.Helper()

	if got == nil {
		t.Fatalf("%s probe = nil, want non-nil", probeName)
	}
	if got.TCPSocket == nil {
		t.Fatalf("%s probe tcpSocket = nil, want non-nil", probeName)
	}
	if got.TCPSocket.Port != intstr.FromString(mongoPortName) {
		t.Fatalf("%s probe port = %#v, want %q", probeName, got.TCPSocket.Port, mongoPortName)
	}
}
