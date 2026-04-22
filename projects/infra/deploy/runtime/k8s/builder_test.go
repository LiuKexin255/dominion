package k8s

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"dominion/projects/infra/deploy/domain"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func testK8sConfig() *K8sConfig {
	return &K8sConfig{
		Namespace: "test-ns",
		ManagedBy: "dominion.io",
		Gateway: GatewayConfig{
			Name:      "test-gateway",
			Namespace: "ingress",
		},
		TLS: TLSConfig{
			Secret: "test-tls-secret",
			Domain: "test.example.com",
			CAConfigMap: ConfigMapConfig{
				Name: "test-ca",
				Key:  "ca.crt",
			},
		},
		OSS: OSSConfig{
			Secret:    "test-oss-secret",
			AccessKey: "access_key",
			SecretKey: "secret_key",
		},
		MongoDB: map[string]*MongoProfileConfig{
			"dev-single": {
				Image:         "mongo",
				Version:       "7.0",
				Port:          27017,
				AdminUsername: "admin",
				Security: MongoSecurityConfig{
					RunAsUser:  1000,
					RunAsGroup: 3000,
				},
				Storage: MongoStorageConfig{
					StorageClassName: "local-path",
					Capacity:         "1Gi",
					AccessModes:      []string{"ReadWriteOnce"},
					VolumeMode:       "Filesystem",
				},
			},
		},
	}
}

func testDeploymentWorkload() *DeploymentWorkload {
	return &DeploymentWorkload{
		ServiceName:     "myservice",
		EnvironmentName: "dev",
		App:             "myapp",
		Image:           "repo/myapp:v1",
		Replicas:        2,
		Ports: []*DeploymentPort{
			{Name: "http", Port: 8080},
			{Name: "grpc", Port: 9090},
		},
	}
}

func testStatefulWorkload() *StatefulWorkload {
	return &StatefulWorkload{
		ServiceName:     "myservice",
		EnvironmentName: "dev",
		App:             "myapp",
		Image:           "repo/myapp:v1",
		Replicas:        3,
		Ports: []*DeploymentPort{
			{Name: "http", Port: 8080},
			{Name: "grpc", Port: 9090},
		},
	}
}

func testHTTPRouteWorkload(envType domain.EnvironmentType, envLabel string) *HTTPRouteWorkload {
	return &HTTPRouteWorkload{
		ServiceName:      "myservice",
		EnvironmentName:  envLabel,
		App:              "myapp",
		Hostnames:        []string{"myapp.example.com"},
		BackendService:   "myservice-backend",
		GatewayName:      "test-gateway",
		GatewayNamespace: "ingress",
		EnvType:          envType,
		Matches: []*HTTPRoutePathMatch{
			{Type: HTTPPathMatchTypePathPrefix, Value: "/v1", BackendPort: 8080},
		},
	}
}

func testMongoDBWorkload() *MongoDBWorkload {
	return &MongoDBWorkload{
		ServiceName:     "mymongo",
		EnvironmentName: "dev",
		App:             "myapp",
		ProfileName:     "dev-single",
		Persistence:     PersistenceConfig{Enabled: true},
	}
}

// --- BuildDeployment ---

func TestBuildDeployment(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()

	deploy, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}

	// Verify object meta.
	if deploy.Namespace != cfg.Namespace {
		t.Fatalf("Namespace = %q, want %q", deploy.Namespace, cfg.Namespace)
	}
	if deploy.Name != w.WorkloadName() {
		t.Fatalf("Name = %q, want %q", deploy.Name, w.WorkloadName())
	}

	// Verify labels.
	wantObjectLabels := buildLabels(
		withApp(w.App),
		withService(w.ServiceName),
		withDominionEnvironment(w.EnvironmentName),
		withManagedBy(cfg.ManagedBy),
	)
	for key, want := range wantObjectLabels {
		if got := deploy.Labels[key]; got != want {
			t.Fatalf("Label[%q] = %q, want %q", key, got, want)
		}
	}

	// Verify selector labels (no managed-by).
	if deploy.Spec.Selector.MatchLabels[managedByLabelKey] != "" {
		t.Fatalf("Selector should not contain managed-by label")
	}
	if deploy.Spec.Selector.MatchLabels[appLabelKey] != w.App {
		t.Fatalf("Selector[%q] = %q, want %q", appLabelKey, deploy.Spec.Selector.MatchLabels[appLabelKey], w.App)
	}

	// Verify replicas.
	if *deploy.Spec.Replicas != w.Replicas {
		t.Fatalf("Replicas = %d, want %d", *deploy.Spec.Replicas, w.Replicas)
	}

	// Verify container.
	if len(deploy.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Containers count = %d, want 1", len(deploy.Spec.Template.Spec.Containers))
	}
	container := deploy.Spec.Template.Spec.Containers[0]
	if container.Image != w.Image {
		t.Fatalf("Image = %q, want %q", container.Image, w.Image)
	}
	if container.Name != w.WorkloadName() {
		t.Fatalf("Container Name = %q, want %q", container.Name, w.WorkloadName())
	}

	// Verify ports.
	if len(container.Ports) != 2 {
		t.Fatalf("Container Ports count = %d, want 2", len(container.Ports))
	}
	if container.Ports[0].Name != "http" || container.Ports[0].ContainerPort != 8080 {
		t.Fatalf("Port[0] = {Name: %q, ContainerPort: %d}, want {Name: \"http\", ContainerPort: 8080}", container.Ports[0].Name, container.Ports[0].ContainerPort)
	}
	if container.Ports[1].Name != "grpc" || container.Ports[1].ContainerPort != 9090 {
		t.Fatalf("Port[1] = {Name: %q, ContainerPort: %d}, want {Name: \"grpc\", ContainerPort: 9090}", container.Ports[1].Name, container.Ports[1].ContainerPort)
	}

	// Verify env vars.
	envMap := envVarsToMap(container.Env)
	if envMap[reservedEnvNameServiceApp] != w.App {
		t.Fatalf("Env[%q] = %q, want %q", reservedEnvNameServiceApp, envMap[reservedEnvNameServiceApp], w.App)
	}
	if envMap[reservedEnvNameDominionEnvironment] != w.EnvironmentName {
		t.Fatalf("Env[%q] = %q, want %q", reservedEnvNameDominionEnvironment, envMap[reservedEnvNameDominionEnvironment], w.EnvironmentName)
	}
	if envMap[reservedEnvNamePodNamespace] != cfg.Namespace {
		t.Fatalf("Env[%q] = %q, want %q", reservedEnvNamePodNamespace, envMap[reservedEnvNamePodNamespace], cfg.Namespace)
	}

	// No TLS volumes when TLSEnabled is false.
	if len(deploy.Spec.Template.Spec.Volumes) != 0 {
		t.Fatalf("Volumes count = %d, want 0 (TLS disabled)", len(deploy.Spec.Template.Spec.Volumes))
	}
	if len(container.VolumeMounts) != 0 {
		t.Fatalf("VolumeMounts count = %d, want 0 (TLS disabled)", len(container.VolumeMounts))
	}
}

func TestBuildDeployment_WithTLS(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.TLSEnabled = true

	deploy, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}

	container := deploy.Spec.Template.Spec.Containers[0]

	// Verify TLS volume.
	if len(deploy.Spec.Template.Spec.Volumes) != 1 {
		t.Fatalf("Volumes count = %d, want 1", len(deploy.Spec.Template.Spec.Volumes))
	}
	vol := deploy.Spec.Template.Spec.Volumes[0]
	if vol.Name != tlsVolumeName {
		t.Fatalf("Volume Name = %q, want %q", vol.Name, tlsVolumeName)
	}
	if vol.Projected == nil {
		t.Fatalf("Volume should use projected source")
	}
	if len(vol.Projected.Sources) != 2 {
		t.Fatalf("Projected Sources count = %d, want 2", len(vol.Projected.Sources))
	}
	// Verify secret projection.
	if vol.Projected.Sources[0].Secret == nil {
		t.Fatalf("First projected source should be secret")
	}
	if vol.Projected.Sources[0].Secret.Name != cfg.TLS.Secret {
		t.Fatalf("Secret Name = %q, want %q", vol.Projected.Sources[0].Secret.Name, cfg.TLS.Secret)
	}
	// Verify configmap projection.
	if vol.Projected.Sources[1].ConfigMap == nil {
		t.Fatalf("Second projected source should be configmap")
	}
	if vol.Projected.Sources[1].ConfigMap.Name != cfg.TLS.CAConfigMap.Name {
		t.Fatalf("ConfigMap Name = %q, want %q", vol.Projected.Sources[1].ConfigMap.Name, cfg.TLS.CAConfigMap.Name)
	}

	// Verify volume mount.
	if len(container.VolumeMounts) != 1 {
		t.Fatalf("VolumeMounts count = %d, want 1", len(container.VolumeMounts))
	}
	mount := container.VolumeMounts[0]
	if mount.Name != tlsVolumeName || mount.MountPath != tlsMountPath || !mount.ReadOnly {
		t.Fatalf("VolumeMount = {Name: %q, MountPath: %q, ReadOnly: %v}, want TLS mount", mount.Name, mount.MountPath, mount.ReadOnly)
	}

	// Verify TLS env vars.
	envMap := envVarsToMap(container.Env)
	if envMap[envTLSCertFile] != filepath.Join(tlsMountPath, tlsCertFileName) {
		t.Fatalf("Env[%q] = %q, want %q", envTLSCertFile, envMap[envTLSCertFile], filepath.Join(tlsMountPath, tlsCertFileName))
	}
	if envMap[envTLSKeyFile] != filepath.Join(tlsMountPath, tlsKeyFileName) {
		t.Fatalf("Env[%q] = %q, want %q", envTLSKeyFile, envMap[envTLSKeyFile], filepath.Join(tlsMountPath, tlsKeyFileName))
	}
	if envMap[envTLSCAFile] != filepath.Join(tlsMountPath, tlsCAFileName) {
		t.Fatalf("Env[%q] = %q, want %q", envTLSCAFile, envMap[envTLSCAFile], filepath.Join(tlsMountPath, tlsCAFileName))
	}
	if envMap[envTLSDomain] != cfg.TLS.Domain {
		t.Fatalf("Env[%q] = %q, want %q", envTLSDomain, envMap[envTLSDomain], cfg.TLS.Domain)
	}
}

func TestBuildDeployment_NoPorts(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.Ports = nil

	deploy, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}
	if len(deploy.Spec.Template.Spec.Containers[0].Ports) != 0 {
		t.Fatalf("Ports count = %d, want 0", len(deploy.Spec.Template.Spec.Containers[0].Ports))
	}
}

func TestBuildDeployment_NilPort(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.Ports = []*DeploymentPort{nil}

	_, err := BuildDeployment(w, cfg)
	if err == nil {
		t.Fatalf("BuildDeployment() expected error for nil port")
	}
}

func TestBuildDeployment_UserEnvSortedBeforeReserved(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.Env = map[string]string{
		"Z_VAR": "z",
		"A_VAR": "a",
		"M_VAR": "m",
		"B_VAR": "b",
	}

	deploy, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}

	container := deploy.Spec.Template.Spec.Containers[0]
	envs := container.Env

	// User env sorted: A_VAR, B_VAR, M_VAR, Z_VAR.
	// Reserved: SERVICE_APP, DOMINION_ENVIRONMENT, POD_NAMESPACE.
	// Total: 7 env vars.
	if len(envs) != 7 {
		t.Fatalf("Env count = %d, want 7", len(envs))
	}

	wantOrder := []struct{ name, value string }{
		{"A_VAR", "a"},
		{"B_VAR", "b"},
		{"M_VAR", "m"},
		{"Z_VAR", "z"},
		{reservedEnvNameServiceApp, w.App},
		{reservedEnvNameDominionEnvironment, w.EnvironmentName},
		{reservedEnvNamePodNamespace, cfg.Namespace},
	}
	for i, want := range wantOrder {
		if envs[i].Name != want.name || envs[i].Value != want.value {
			t.Fatalf("Env[%d] = {Name: %q, Value: %q}, want {Name: %q, Value: %q}", i, envs[i].Name, envs[i].Value, want.name, want.value)
		}
	}
}

func TestBuildDeployment_UserEnvWithTLS_ReservedAfterUserBeforeTLS(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.TLSEnabled = true
	w.Env = map[string]string{
		"APP_DEBUG": "true",
		"LOG_LEVEL": "info",
	}

	deploy, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}

	container := deploy.Spec.Template.Spec.Containers[0]
	envs := container.Env

	// APP_DEBUG, LOG_LEVEL, SERVICE_APP, DOMINION_ENVIRONMENT, POD_NAMESPACE, TLS_CERT_FILE, TLS_KEY_FILE, TLS_CA_FILE, TLS_SERVER_NAME.
	if len(envs) != 9 {
		t.Fatalf("Env count = %d, want 9", len(envs))
	}

	// Verify user env comes first, sorted.
	if envs[0].Name != "APP_DEBUG" || envs[1].Name != "LOG_LEVEL" {
		t.Fatalf("User env order = [%q, %q], want [APP_DEBUG, LOG_LEVEL]", envs[0].Name, envs[1].Name)
	}

	// Verify reserved env after user.
	if envs[2].Name != reservedEnvNameServiceApp {
		t.Fatalf("Env[2] Name = %q, want %q", envs[2].Name, reservedEnvNameServiceApp)
	}

	// Verify TLS env last.
	if envs[5].Name != envTLSCertFile {
		t.Fatalf("Env[5] Name = %q, want %q", envs[5].Name, envTLSCertFile)
	}
	if envs[8].Name != envTLSDomain {
		t.Fatalf("Env[8] Name = %q, want %q", envs[8].Name, envTLSDomain)
	}
}

func TestBuildDeployment_NilEnv_BackwardCompatible(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.Env = nil

	deploy, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}

	container := deploy.Spec.Template.Spec.Containers[0]

	// Only reserved env, no user env.
	if len(container.Env) != 3 {
		t.Fatalf("Env count = %d, want 3", len(container.Env))
	}
	if container.Env[0].Name != reservedEnvNameServiceApp {
		t.Fatalf("Env[0] Name = %q, want %q", container.Env[0].Name, reservedEnvNameServiceApp)
	}
}

func TestBuildDeployment_UserEnvSortStability(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.Env = map[string]string{
		"FOO": "1",
		"BAR": "2",
		"BAZ": "3",
	}

	deploy1, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}
	deploy2, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}

	envs1 := deploy1.Spec.Template.Spec.Containers[0].Env
	envs2 := deploy2.Spec.Template.Spec.Containers[0].Env

	for i := range envs1 {
		if envs1[i].Name != envs2[i].Name || envs1[i].Value != envs2[i].Value {
			t.Fatalf("Env[%d] differs between builds: %q/%q vs %q/%q", i, envs1[i].Name, envs1[i].Value, envs2[i].Name, envs2[i].Value)
		}
	}
}

func TestBuildDeployment_WithOSS(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.OSSEnabled = true

	deploy, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}

	container := deploy.Spec.Template.Spec.Containers[0]
	envs := container.Env

	// 3 reserved + 2 OSS = 5.
	if len(envs) != 5 {
		t.Fatalf("Env count = %d, want 5", len(envs))
	}

	// Verify S3_ACCESS_KEY SecretKeyRef.
	accessKeyEnv := envs[3]
	if accessKeyEnv.Name != envS3AccessKey {
		t.Fatalf("Env[3] Name = %q, want %q", accessKeyEnv.Name, envS3AccessKey)
	}
	if accessKeyEnv.ValueFrom == nil || accessKeyEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("Env[3] should use SecretKeyRef")
	}
	if accessKeyEnv.ValueFrom.SecretKeyRef.Name != cfg.OSS.Secret {
		t.Fatalf("SecretKeyRef Name = %q, want %q", accessKeyEnv.ValueFrom.SecretKeyRef.Name, cfg.OSS.Secret)
	}
	if accessKeyEnv.ValueFrom.SecretKeyRef.Key != cfg.OSS.AccessKey {
		t.Fatalf("SecretKeyRef Key = %q, want %q", accessKeyEnv.ValueFrom.SecretKeyRef.Key, cfg.OSS.AccessKey)
	}

	// Verify S3_SECRET_KEY SecretKeyRef.
	secretKeyEnv := envs[4]
	if secretKeyEnv.Name != envS3SecretKey {
		t.Fatalf("Env[4] Name = %q, want %q", secretKeyEnv.Name, envS3SecretKey)
	}
	if secretKeyEnv.ValueFrom == nil || secretKeyEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("Env[4] should use SecretKeyRef")
	}
	if secretKeyEnv.ValueFrom.SecretKeyRef.Name != cfg.OSS.Secret {
		t.Fatalf("SecretKeyRef Name = %q, want %q", secretKeyEnv.ValueFrom.SecretKeyRef.Name, cfg.OSS.Secret)
	}
	if secretKeyEnv.ValueFrom.SecretKeyRef.Key != cfg.OSS.SecretKey {
		t.Fatalf("SecretKeyRef Key = %q, want %q", secretKeyEnv.ValueFrom.SecretKeyRef.Key, cfg.OSS.SecretKey)
	}
}

func TestBuildDeployment_WithTLSAndOSS(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.TLSEnabled = true
	w.OSSEnabled = true

	deploy, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}

	container := deploy.Spec.Template.Spec.Containers[0]
	envs := container.Env

	// 3 reserved + 4 TLS + 2 OSS = 9.
	if len(envs) != 9 {
		t.Fatalf("Env count = %d, want 9", len(envs))
	}

	// Verify OSS env comes after TLS env.
	if envs[7].Name != envS3AccessKey {
		t.Fatalf("Env[7] Name = %q, want %q", envs[7].Name, envS3AccessKey)
	}
	if envs[8].Name != envS3SecretKey {
		t.Fatalf("Env[8] Name = %q, want %q", envs[8].Name, envS3SecretKey)
	}
}

func TestBuildDeployment_UserEnvWithOSS_ReservedAfterUserAfterTLS(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.TLSEnabled = true
	w.OSSEnabled = true
	w.Env = map[string]string{
		"APP_DEBUG": "true",
		"LOG_LEVEL": "info",
	}

	deploy, err := BuildDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildDeployment() error: %v", err)
	}

	container := deploy.Spec.Template.Spec.Containers[0]
	envs := container.Env

	// 2 user + 3 reserved + 4 TLS + 2 OSS = 11.
	if len(envs) != 11 {
		t.Fatalf("Env count = %d, want 11", len(envs))
	}

	wantOrder := []string{
		"APP_DEBUG", "LOG_LEVEL",
		reservedEnvNameServiceApp, reservedEnvNameDominionEnvironment, reservedEnvNamePodNamespace,
		envTLSCertFile, envTLSKeyFile, envTLSCAFile, envTLSDomain,
		envS3AccessKey, envS3SecretKey,
	}
	for i, want := range wantOrder {
		if envs[i].Name != want {
			t.Fatalf("Env[%d] Name = %q, want %q", i, envs[i].Name, want)
		}
	}
}

func TestBuildStatefulSet_WithOSS(t *testing.T) {
	cfg := testK8sConfig()
	w := testStatefulWorkload()
	w.OSSEnabled = true

	sts, err := BuildStatefulSet(w, cfg)
	if err != nil {
		t.Fatalf("BuildStatefulSet() error: %v", err)
	}

	container := sts.Spec.Template.Spec.Containers[0]
	envs := container.Env

	// 3 reserved + 2 OSS = 5.
	if len(envs) != 5 {
		t.Fatalf("Env count = %d, want 5", len(envs))
	}

	accessKeyEnv := envs[3]
	if accessKeyEnv.Name != envS3AccessKey {
		t.Fatalf("Env[3] Name = %q, want %q", accessKeyEnv.Name, envS3AccessKey)
	}
	if accessKeyEnv.ValueFrom == nil || accessKeyEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("Env[3] should use SecretKeyRef")
	}
	if accessKeyEnv.ValueFrom.SecretKeyRef.Name != cfg.OSS.Secret {
		t.Fatalf("SecretKeyRef Name = %q, want %q", accessKeyEnv.ValueFrom.SecretKeyRef.Name, cfg.OSS.Secret)
	}
	if accessKeyEnv.ValueFrom.SecretKeyRef.Key != cfg.OSS.AccessKey {
		t.Fatalf("SecretKeyRef Key = %q, want %q", accessKeyEnv.ValueFrom.SecretKeyRef.Key, cfg.OSS.AccessKey)
	}

	secretKeyEnv := envs[4]
	if secretKeyEnv.Name != envS3SecretKey {
		t.Fatalf("Env[4] Name = %q, want %q", secretKeyEnv.Name, envS3SecretKey)
	}
	if secretKeyEnv.ValueFrom == nil || secretKeyEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("Env[4] should use SecretKeyRef")
	}
	if secretKeyEnv.ValueFrom.SecretKeyRef.Name != cfg.OSS.Secret {
		t.Fatalf("SecretKeyRef Name = %q, want %q", secretKeyEnv.ValueFrom.SecretKeyRef.Name, cfg.OSS.Secret)
	}
	if secretKeyEnv.ValueFrom.SecretKeyRef.Key != cfg.OSS.SecretKey {
		t.Fatalf("SecretKeyRef Key = %q, want %q", secretKeyEnv.ValueFrom.SecretKeyRef.Key, cfg.OSS.SecretKey)
	}
}

// --- BuildStatefulSet ---

func TestBuildStatefulSet(t *testing.T) {
	tests := []struct {
		name  string
		given func() (*StatefulWorkload, *K8sConfig)
		then  func(*testing.T, *appsv1.StatefulSet, *StatefulWorkload, *K8sConfig)
	}{
		{
			name: "builds statefulset with deployment-equivalent pod template",
			given: func() (*StatefulWorkload, *K8sConfig) {
				return testStatefulWorkload(), testK8sConfig()
			},
			then: func(t *testing.T, sts *appsv1.StatefulSet, workload *StatefulWorkload, cfg *K8sConfig) {
				t.Helper()

				if sts.Namespace != cfg.Namespace {
					t.Fatalf("Namespace = %q, want %q", sts.Namespace, cfg.Namespace)
				}
				if sts.Name != workload.WorkloadName() {
					t.Fatalf("Name = %q, want %q", sts.Name, workload.WorkloadName())
				}
				if sts.Spec.ServiceName != workload.ServiceResourceName() {
					t.Fatalf("ServiceName = %q, want %q", sts.Spec.ServiceName, workload.ServiceResourceName())
				}
				if sts.Spec.Replicas == nil || *sts.Spec.Replicas != workload.Replicas {
					t.Fatalf("Replicas = %v, want %d", sts.Spec.Replicas, workload.Replicas)
				}
				if len(sts.Spec.VolumeClaimTemplates) != 0 {
					t.Fatalf("VolumeClaimTemplates count = %d, want 0", len(sts.Spec.VolumeClaimTemplates))
				}

				wantObjectLabels := buildLabels(
					withApp(workload.App),
					withService(workload.ServiceName),
					withDominionEnvironment(workload.EnvironmentName),
					withManagedBy(cfg.ManagedBy),
				)
				for key, want := range wantObjectLabels {
					if got := sts.Labels[key]; got != want {
						t.Fatalf("Label[%q] = %q, want %q", key, got, want)
					}
					if got := sts.Spec.Template.Labels[key]; got != want {
						t.Fatalf("Template Label[%q] = %q, want %q", key, got, want)
					}
				}

				wantSelectorLabels := buildLabels(
					withApp(workload.App),
					withService(workload.ServiceName),
					withDominionEnvironment(workload.EnvironmentName),
				)
				for key, want := range wantSelectorLabels {
					if got := sts.Spec.Selector.MatchLabels[key]; got != want {
						t.Fatalf("Selector[%q] = %q, want %q", key, got, want)
					}
				}
				if sts.Spec.Selector.MatchLabels[managedByLabelKey] != "" {
					t.Fatalf("Selector should not contain managed-by")
				}

				if len(sts.Spec.Template.Spec.Containers) != 1 {
					t.Fatalf("Containers count = %d, want 1", len(sts.Spec.Template.Spec.Containers))
				}
				container := sts.Spec.Template.Spec.Containers[0]
				if container.Name != workload.WorkloadName() {
					t.Fatalf("Container Name = %q, want %q", container.Name, workload.WorkloadName())
				}
				if container.Image != workload.Image {
					t.Fatalf("Container Image = %q, want %q", container.Image, workload.Image)
				}
				if len(container.Ports) != 2 {
					t.Fatalf("Ports count = %d, want 2", len(container.Ports))
				}

				envMap := envVarsToMap(container.Env)
				if envMap[reservedEnvNameServiceApp] != workload.App {
					t.Fatalf("Env[%q] = %q, want %q", reservedEnvNameServiceApp, envMap[reservedEnvNameServiceApp], workload.App)
				}
				if envMap[reservedEnvNameDominionEnvironment] != workload.EnvironmentName {
					t.Fatalf("Env[%q] = %q, want %q", reservedEnvNameDominionEnvironment, envMap[reservedEnvNameDominionEnvironment], workload.EnvironmentName)
				}
				if envMap[reservedEnvNamePodNamespace] != cfg.Namespace {
					t.Fatalf("Env[%q] = %q, want %q", reservedEnvNamePodNamespace, envMap[reservedEnvNamePodNamespace], cfg.Namespace)
				}
				if len(sts.Spec.Template.Spec.Volumes) != 0 {
					t.Fatalf("Volumes count = %d, want 0", len(sts.Spec.Template.Spec.Volumes))
				}
				if len(container.VolumeMounts) != 0 {
					t.Fatalf("VolumeMounts count = %d, want 0", len(container.VolumeMounts))
				}
			},
		},
		{
			name: "injects tls settings like deployment",
			given: func() (*StatefulWorkload, *K8sConfig) {
				workload := testStatefulWorkload()
				workload.TLSEnabled = true
				return workload, testK8sConfig()
			},
			then: func(t *testing.T, sts *appsv1.StatefulSet, workload *StatefulWorkload, cfg *K8sConfig) {
				t.Helper()

				if len(sts.Spec.Template.Spec.Volumes) != 1 {
					t.Fatalf("Volumes count = %d, want 1", len(sts.Spec.Template.Spec.Volumes))
				}
				volume := sts.Spec.Template.Spec.Volumes[0]
				if volume.Name != tlsVolumeName {
					t.Fatalf("Volume Name = %q, want %q", volume.Name, tlsVolumeName)
				}
				if volume.Projected == nil || len(volume.Projected.Sources) != 2 {
					t.Fatalf("Projected sources count = %d, want 2", len(volume.Projected.Sources))
				}
				if volume.Projected.Sources[0].Secret == nil || volume.Projected.Sources[0].Secret.Name != cfg.TLS.Secret {
					t.Fatalf("Secret projection mismatch")
				}
				if volume.Projected.Sources[1].ConfigMap == nil || volume.Projected.Sources[1].ConfigMap.Name != cfg.TLS.CAConfigMap.Name {
					t.Fatalf("ConfigMap projection mismatch")
				}

				container := sts.Spec.Template.Spec.Containers[0]
				if len(container.VolumeMounts) != 1 {
					t.Fatalf("VolumeMounts count = %d, want 1", len(container.VolumeMounts))
				}
				mount := container.VolumeMounts[0]
				if mount.Name != tlsVolumeName || mount.MountPath != tlsMountPath || !mount.ReadOnly {
					t.Fatalf("VolumeMount = %#v, want TLS mount", mount)
				}

				envMap := envVarsToMap(container.Env)
				if envMap[envTLSCertFile] != filepath.Join(tlsMountPath, tlsCertFileName) {
					t.Fatalf("Env[%q] = %q, want %q", envTLSCertFile, envMap[envTLSCertFile], filepath.Join(tlsMountPath, tlsCertFileName))
				}
				if envMap[envTLSKeyFile] != filepath.Join(tlsMountPath, tlsKeyFileName) {
					t.Fatalf("Env[%q] = %q, want %q", envTLSKeyFile, envMap[envTLSKeyFile], filepath.Join(tlsMountPath, tlsKeyFileName))
				}
				if envMap[envTLSCAFile] != filepath.Join(tlsMountPath, tlsCAFileName) {
					t.Fatalf("Env[%q] = %q, want %q", envTLSCAFile, envMap[envTLSCAFile], filepath.Join(tlsMountPath, tlsCAFileName))
				}
				if envMap[envTLSDomain] != cfg.TLS.Domain {
					t.Fatalf("Env[%q] = %q, want %q", envTLSDomain, envMap[envTLSDomain], cfg.TLS.Domain)
				}
				if workload == nil {
					t.Fatalf("workload should not be nil")
				}
			},
		},
		{
			name: "injects user env sorted before reserved",
			given: func() (*StatefulWorkload, *K8sConfig) {
				workload := testStatefulWorkload()
				workload.Env = map[string]string{
					"Z_VAR": "z",
					"A_VAR": "a",
				}
				return workload, testK8sConfig()
			},
			then: func(t *testing.T, sts *appsv1.StatefulSet, workload *StatefulWorkload, cfg *K8sConfig) {
				t.Helper()

				container := sts.Spec.Template.Spec.Containers[0]
				envs := container.Env
				if len(envs) != 5 {
					t.Fatalf("Env count = %d, want 5", len(envs))
				}

				// User env sorted first.
				if envs[0].Name != "A_VAR" || envs[0].Value != "a" {
					t.Fatalf("Env[0] = {Name: %q, Value: %q}, want A_VAR/a", envs[0].Name, envs[0].Value)
				}
				if envs[1].Name != "Z_VAR" || envs[1].Value != "z" {
					t.Fatalf("Env[1] = {Name: %q, Value: %q}, want Z_VAR/z", envs[1].Name, envs[1].Value)
				}

				// Reserved after user.
				if envs[2].Name != reservedEnvNameServiceApp {
					t.Fatalf("Env[2] Name = %q, want %q", envs[2].Name, reservedEnvNameServiceApp)
				}
				if envs[3].Name != reservedEnvNameDominionEnvironment {
					t.Fatalf("Env[3] Name = %q, want %q", envs[3].Name, reservedEnvNameDominionEnvironment)
				}
				if envs[4].Name != reservedEnvNamePodNamespace {
					t.Fatalf("Env[4] Name = %q, want %q", envs[4].Name, reservedEnvNamePodNamespace)
				}
			},
		},
		{
			name: "injects user env sorted before reserved with tls",
			given: func() (*StatefulWorkload, *K8sConfig) {
				workload := testStatefulWorkload()
				workload.TLSEnabled = true
				workload.Env = map[string]string{
					"LOG_LEVEL": "debug",
				}
				return workload, testK8sConfig()
			},
			then: func(t *testing.T, sts *appsv1.StatefulSet, workload *StatefulWorkload, cfg *K8sConfig) {
				t.Helper()

				container := sts.Spec.Template.Spec.Containers[0]
				envs := container.Env
				if len(envs) != 8 {
					t.Fatalf("Env count = %d, want 8", len(envs))
				}

				// User env first.
				if envs[0].Name != "LOG_LEVEL" || envs[0].Value != "debug" {
					t.Fatalf("Env[0] = {Name: %q, Value: %q}, want LOG_LEVEL/debug", envs[0].Name, envs[0].Value)
				}

				// Reserved after user.
				if envs[1].Name != reservedEnvNameServiceApp {
					t.Fatalf("Env[1] Name = %q, want %q", envs[1].Name, reservedEnvNameServiceApp)
				}

				// TLS last.
				if envs[4].Name != envTLSCertFile {
					t.Fatalf("Env[4] Name = %q, want %q", envs[4].Name, envTLSCertFile)
				}
				if envs[7].Name != envTLSDomain {
					t.Fatalf("Env[7] Name = %q, want %q", envs[7].Name, envTLSDomain)
				}
			},
		},
		{
			name: "nil env only reserved",
			given: func() (*StatefulWorkload, *K8sConfig) {
				workload := testStatefulWorkload()
				workload.Env = nil
				return workload, testK8sConfig()
			},
			then: func(t *testing.T, sts *appsv1.StatefulSet, workload *StatefulWorkload, cfg *K8sConfig) {
				t.Helper()

				container := sts.Spec.Template.Spec.Containers[0]
				if len(container.Env) != 3 {
					t.Fatalf("Env count = %d, want 3", len(container.Env))
				}
				if container.Env[0].Name != reservedEnvNameServiceApp {
					t.Fatalf("Env[0] Name = %q, want %q", container.Env[0].Name, reservedEnvNameServiceApp)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			workload, cfg := tt.given()

			// when
			sts, err := BuildStatefulSet(workload, cfg)
			if err != nil {
				t.Fatalf("BuildStatefulSet() error: %v", err)
			}

			// then
			tt.then(t, sts, workload, cfg)
		})
	}
}

// --- BuildGoverningService ---

func TestBuildGoverningService(t *testing.T) {
	tests := []struct {
		name  string
		given func() (*StatefulWorkload, *K8sConfig)
	}{
		{
			name: "builds headless service for all statefulset pods",
			given: func() (*StatefulWorkload, *K8sConfig) {
				return testStatefulWorkload(), testK8sConfig()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			workload, cfg := tt.given()

			// when
			svc, err := BuildGoverningService(workload, cfg)
			if err != nil {
				t.Fatalf("BuildGoverningService() error: %v", err)
			}

			// then
			if svc.Name != workload.ServiceResourceName() {
				t.Fatalf("Name = %q, want %q", svc.Name, workload.ServiceResourceName())
			}
			if svc.Spec.ClusterIP != corev1.ClusterIPNone {
				t.Fatalf("ClusterIP = %q, want %q", svc.Spec.ClusterIP, corev1.ClusterIPNone)
			}
			wantSelector := buildLabels(
				withApp(workload.App),
				withService(workload.ServiceName),
				withDominionEnvironment(workload.EnvironmentName),
			)
			for key, want := range wantSelector {
				if got := svc.Spec.Selector[key]; got != want {
					t.Fatalf("Selector[%q] = %q, want %q", key, got, want)
				}
			}
			if len(svc.Spec.Ports) != 2 {
				t.Fatalf("Ports count = %d, want 2", len(svc.Spec.Ports))
			}
		})
	}
}

// --- BuildPerInstanceService ---

func TestBuildPerInstanceService(t *testing.T) {
	tests := []struct {
		name          string
		instanceIndex int
		given         func() (*StatefulWorkload, *K8sConfig)
	}{
		{
			name:          "selects a single pod by pod-name label",
			instanceIndex: 2,
			given: func() (*StatefulWorkload, *K8sConfig) {
				return testStatefulWorkload(), testK8sConfig()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			workload, cfg := tt.given()

			// when
			svc, err := BuildPerInstanceService(workload, cfg, tt.instanceIndex)
			if err != nil {
				t.Fatalf("BuildPerInstanceService() error: %v", err)
			}

			// then
			wantName := newInstanceObjectName(WorkloadKindInstanceService, workload.EnvironmentName, workload.ServiceName, tt.instanceIndex)
			if svc.Name != wantName {
				t.Fatalf("Name = %q, want %q", svc.Name, wantName)
			}
			wantPodName := workload.WorkloadName() + "-2"
			if svc.Spec.Selector["statefulset.kubernetes.io/pod-name"] != wantPodName {
				t.Fatalf("Selector[pod-name] = %q, want %q", svc.Spec.Selector["statefulset.kubernetes.io/pod-name"], wantPodName)
			}
			if len(svc.Spec.Ports) != 2 {
				t.Fatalf("Ports count = %d, want 2", len(svc.Spec.Ports))
			}
		})
	}
}

// --- BuildPerInstanceHTTPRoute ---

func TestBuildPerInstanceHTTPRoute(t *testing.T) {
	tests := []struct {
		name          string
		instanceIndex int
		given         func() (*HTTPRouteWorkload, *K8sConfig)
		then          func(*testing.T, *gatewayv1.HTTPRoute, *HTTPRouteWorkload)
	}{
		{
			name:          "builds catch-all route for prod instance",
			instanceIndex: 1,
			given: func() (*HTTPRouteWorkload, *K8sConfig) {
				workload := testHTTPRouteWorkload(domain.EnvironmentTypeProd, "tstscope.prod")
				workload.BackendService = newInstanceObjectName(WorkloadKindInstanceService, workload.EnvironmentName, workload.ServiceName, 1)
				return workload, testK8sConfig()
			},
			then: func(t *testing.T, route *gatewayv1.HTTPRoute, workload *HTTPRouteWorkload) {
				t.Helper()

				if route.Name != newInstanceObjectName(WorkloadKindInstanceRoute, workload.EnvironmentName, workload.ServiceName, 1) {
					t.Fatalf("Name = %q, want %q", route.Name, newInstanceObjectName(WorkloadKindInstanceRoute, workload.EnvironmentName, workload.ServiceName, 1))
				}
				if len(route.Spec.Hostnames) != 1 || string(route.Spec.Hostnames[0]) != workload.Hostnames[0] {
					t.Fatalf("Hostnames = %v, want %v", route.Spec.Hostnames, workload.Hostnames)
				}
				if len(route.Spec.Rules) != 1 || len(route.Spec.Rules[0].Matches) != 1 {
					t.Fatalf("Rules or matches count mismatch")
				}
				match := route.Spec.Rules[0].Matches[0]
				if match.Path == nil || match.Path.Type == nil || *match.Path.Type != gatewayv1.PathMatchPathPrefix || match.Path.Value == nil || *match.Path.Value != "/" {
					t.Fatalf("Path match = %#v, want catch-all prefix '/'", match.Path)
				}
				if len(match.Headers) != 0 {
					t.Fatalf("Headers count = %d, want 0", len(match.Headers))
				}
				backendRef := route.Spec.Rules[0].BackendRefs[0].BackendRef.BackendObjectReference
				if string(backendRef.Name) != workload.BackendService {
					t.Fatalf("Backend name = %q, want %q", backendRef.Name, workload.BackendService)
				}
				if backendRef.Port == nil || int(*backendRef.Port) != workload.Matches[0].BackendPort {
					t.Fatalf("Backend port = %v, want %d", backendRef.Port, workload.Matches[0].BackendPort)
				}
			},
		},
		{
			name:          "adds env header match outside prod",
			instanceIndex: 0,
			given: func() (*HTTPRouteWorkload, *K8sConfig) {
				workload := testHTTPRouteWorkload(domain.EnvironmentTypeDev, "tstscope.dev")
				workload.BackendService = newInstanceObjectName(WorkloadKindInstanceService, workload.EnvironmentName, workload.ServiceName, 0)
				return workload, testK8sConfig()
			},
			then: func(t *testing.T, route *gatewayv1.HTTPRoute, workload *HTTPRouteWorkload) {
				t.Helper()
				assertHTTPRouteEnvHeader(t, route, workload.EnvironmentName)
				match := route.Spec.Rules[0].Matches[0]
				if match.Path == nil || match.Path.Value == nil || *match.Path.Value != "/" {
					t.Fatalf("Path value = %v, want /", match.Path)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			workload, cfg := tt.given()

			// when
			route, err := BuildPerInstanceHTTPRoute(workload, cfg, tt.instanceIndex)
			if err != nil {
				t.Fatalf("BuildPerInstanceHTTPRoute() error: %v", err)
			}

			// then
			tt.then(t, decodeHTTPRoute(t, route), workload)
		})
	}
}

// --- BuildService ---

func TestBuildService(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()

	svc, err := BuildService(w, cfg)
	if err != nil {
		t.Fatalf("BuildService() error: %v", err)
	}

	// Verify object meta.
	if svc.Namespace != cfg.Namespace {
		t.Fatalf("Namespace = %q, want %q", svc.Namespace, cfg.Namespace)
	}
	if svc.Name != w.ServiceResourceName() {
		t.Fatalf("Name = %q, want %q", svc.Name, w.ServiceResourceName())
	}

	// Verify labels.
	if svc.Labels[appLabelKey] != w.App {
		t.Fatalf("Label[%q] = %q, want %q", appLabelKey, svc.Labels[appLabelKey], w.App)
	}
	if svc.Labels[managedByLabelKey] != cfg.ManagedBy {
		t.Fatalf("Label[%q] = %q, want %q", managedByLabelKey, svc.Labels[managedByLabelKey], cfg.ManagedBy)
	}

	// Verify selector labels.
	if svc.Spec.Selector[appLabelKey] != w.App {
		t.Fatalf("Selector[%q] = %q, want %q", appLabelKey, svc.Spec.Selector[appLabelKey], w.App)
	}
	if svc.Spec.Selector[managedByLabelKey] != "" {
		t.Fatalf("Selector should not contain managed-by")
	}

	// Verify ports.
	if len(svc.Spec.Ports) != 2 {
		t.Fatalf("Ports count = %d, want 2", len(svc.Spec.Ports))
	}
	if svc.Spec.Ports[0].Name != "http" || svc.Spec.Ports[0].Port != 8080 {
		t.Fatalf("Port[0] = {Name: %q, Port: %d}, want {Name: \"http\", Port: 8080}", svc.Spec.Ports[0].Name, svc.Spec.Ports[0].Port)
	}
	if svc.Spec.Ports[0].TargetPort.StrVal != "http" {
		t.Fatalf("TargetPort = %q, want %q", svc.Spec.Ports[0].TargetPort.StrVal, "http")
	}
}

func TestBuildService_NoPorts(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.Ports = nil

	svc, err := BuildService(w, cfg)
	if err != nil {
		t.Fatalf("BuildService() error: %v", err)
	}
	if len(svc.Spec.Ports) != 0 {
		t.Fatalf("Ports count = %d, want 0", len(svc.Spec.Ports))
	}
}

func TestBuildService_NilPort(t *testing.T) {
	cfg := testK8sConfig()
	w := testDeploymentWorkload()
	w.Ports = []*DeploymentPort{nil}

	_, err := BuildService(w, cfg)
	if err == nil {
		t.Fatalf("BuildService() expected error for nil port")
	}
}

// --- BuildHTTPRoute ---

func TestBuildHTTPRoute(t *testing.T) {
	cfg := testK8sConfig()
	w := testHTTPRouteWorkload(domain.EnvironmentTypeProd, "tstscope.prod")

	route, err := BuildHTTPRoute(w, cfg)
	if err != nil {
		t.Fatalf("BuildHTTPRoute() error: %v", err)
	}

	// Verify it's an unstructured object with correct kind.
	if route.GetKind() != httpRouteKind {
		t.Fatalf("Kind = %q, want %q", route.GetKind(), httpRouteKind)
	}
	if route.GetNamespace() != cfg.Namespace {
		t.Fatalf("Namespace = %q, want %q", route.GetNamespace(), cfg.Namespace)
	}
	if route.GetName() != w.ResourceName() {
		t.Fatalf("Name = %q, want %q", route.GetName(), w.ResourceName())
	}

	// Verify labels.
	labels := route.GetLabels()
	if labels[appLabelKey] != w.App {
		t.Fatalf("Label[%q] = %q, want %q", appLabelKey, labels[appLabelKey], w.App)
	}
	if labels[managedByLabelKey] != cfg.ManagedBy {
		t.Fatalf("Label[%q] = %q, want %q", managedByLabelKey, labels[managedByLabelKey], cfg.ManagedBy)
	}
}

func TestBuildHTTPRoute_Prod_NoHeaderMatch(t *testing.T) {
	cfg := testK8sConfig()
	workload := testHTTPRouteWorkload(domain.EnvironmentTypeProd, "tstscope.prod")

	route, err := BuildHTTPRoute(workload, cfg)
	if err != nil {
		t.Fatalf("BuildHTTPRoute() error: %v", err)
	}

	typedRoute := decodeHTTPRoute(t, route)
	if len(typedRoute.Spec.Rules) != 1 {
		t.Fatalf("Rules count = %d, want 1", len(typedRoute.Spec.Rules))
	}
	if len(typedRoute.Spec.Rules[0].Matches) != 1 {
		t.Fatalf("Matches count = %d, want 1", len(typedRoute.Spec.Rules[0].Matches))
	}
	if len(typedRoute.Spec.Rules[0].Matches[0].Headers) != 0 {
		t.Fatalf("Headers count = %d, want 0", len(typedRoute.Spec.Rules[0].Matches[0].Headers))
	}
}

func TestBuildHTTPRoute_Test_HasEnvHeader(t *testing.T) {
	cfg := testK8sConfig()
	workload := testHTTPRouteWorkload(domain.EnvironmentTypeTest, "tstscope.test")

	route, err := BuildHTTPRoute(workload, cfg)
	if err != nil {
		t.Fatalf("BuildHTTPRoute() error: %v", err)
	}

	assertHTTPRouteEnvHeader(t, decodeHTTPRoute(t, route), "tstscope.test")
}

func TestBuildHTTPRoute_Dev_HasEnvHeader(t *testing.T) {
	cfg := testK8sConfig()
	workload := testHTTPRouteWorkload(domain.EnvironmentTypeDev, "tstscope.dev")

	route, err := BuildHTTPRoute(workload, cfg)
	if err != nil {
		t.Fatalf("BuildHTTPRoute() error: %v", err)
	}

	assertHTTPRouteEnvHeader(t, decodeHTTPRoute(t, route), "tstscope.dev")
}

// --- BuildMongoDBDeployment ---

func TestBuildMongoDBDeployment(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()

	deploy, err := BuildMongoDBDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildMongoDBDeployment() error: %v", err)
	}

	// Verify object meta.
	if deploy.Namespace != cfg.Namespace {
		t.Fatalf("Namespace = %q, want %q", deploy.Namespace, cfg.Namespace)
	}
	if deploy.Name != w.ResourceName() {
		t.Fatalf("Name = %q, want %q", deploy.Name, w.ResourceName())
	}

	// Verify labels.
	if deploy.Labels[appLabelKey] != w.App {
		t.Fatalf("Label[%q] = %q, want %q", appLabelKey, deploy.Labels[appLabelKey], w.App)
	}
	if deploy.Labels[managedByLabelKey] != cfg.ManagedBy {
		t.Fatalf("Label[%q] = %q, want %q", managedByLabelKey, deploy.Labels[managedByLabelKey], cfg.ManagedBy)
	}

	// Verify replicas is 1.
	if *deploy.Spec.Replicas != 1 {
		t.Fatalf("Replicas = %d, want 1", *deploy.Spec.Replicas)
	}

	// Verify container image.
	profile := cfg.MongoDB["dev-single"]
	wantImage := profile.Image + ":" + profile.Version
	container := deploy.Spec.Template.Spec.Containers[0]
	if container.Image != wantImage {
		t.Fatalf("Image = %q, want %q", container.Image, wantImage)
	}

	// Verify container port.
	if len(container.Ports) != 1 {
		t.Fatalf("Ports count = %d, want 1", len(container.Ports))
	}
	if container.Ports[0].Name != mongoPortName || container.Ports[0].ContainerPort != int32(profile.Port) {
		t.Fatalf("Port = {Name: %q, ContainerPort: %d}, want {Name: %q, ContainerPort: %d}", container.Ports[0].Name, container.Ports[0].ContainerPort, mongoPortName, profile.Port)
	}

	// Verify probes.
	if container.LivenessProbe == nil || container.LivenessProbe.TCPSocket == nil {
		t.Fatalf("LivenessProbe should use TCP socket")
	}
	if container.ReadinessProbe == nil || container.ReadinessProbe.TCPSocket == nil {
		t.Fatalf("ReadinessProbe should use TCP socket")
	}

	// Verify security context.
	sc := deploy.Spec.Template.Spec.SecurityContext
	if sc == nil {
		t.Fatalf("SecurityContext should not be nil")
	}
	if *sc.RunAsUser != profile.Security.RunAsUser {
		t.Fatalf("RunAsUser = %d, want %d", *sc.RunAsUser, profile.Security.RunAsUser)
	}
	if *sc.RunAsGroup != profile.Security.RunAsGroup {
		t.Fatalf("RunAsGroup = %d, want %d", *sc.RunAsGroup, profile.Security.RunAsGroup)
	}

	// Verify volumes (PVC).
	if len(deploy.Spec.Template.Spec.Volumes) != 1 {
		t.Fatalf("Volumes count = %d, want 1", len(deploy.Spec.Template.Spec.Volumes))
	}
	vol := deploy.Spec.Template.Spec.Volumes[0]
	if vol.PersistentVolumeClaim == nil || vol.PersistentVolumeClaim.ClaimName != w.PVCResourceName() {
		t.Fatalf("Volume should reference PVC %q", w.PVCResourceName())
	}

	// Verify init container.
	if len(deploy.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("InitContainers count = %d, want 1", len(deploy.Spec.Template.Spec.InitContainers))
	}
	initContainer := deploy.Spec.Template.Spec.InitContainers[0]
	if initContainer.Name != mongoInitContainerName {
		t.Fatalf("InitContainer Name = %q, want %q", initContainer.Name, mongoInitContainerName)
	}
	if initContainer.Image != wantImage {
		t.Fatalf("InitContainer Image = %q, want %q", initContainer.Image, wantImage)
	}

	// Verify env vars reference the secret.
	envMap := envVarsToMap(container.Env)
	if envMap[reservedEnvNameServiceApp] != w.App {
		t.Fatalf("Env[%q] = %q, want %q", reservedEnvNameServiceApp, envMap[reservedEnvNameServiceApp], w.App)
	}
	if envMap[reservedEnvNameDominionEnvironment] != w.EnvironmentName {
		t.Fatalf("Env[%q] = %q, want %q", reservedEnvNameDominionEnvironment, envMap[reservedEnvNameDominionEnvironment], w.EnvironmentName)
	}
}

func TestBuildMongoDBDeployment_NilWorkload(t *testing.T) {
	cfg := testK8sConfig()
	_, err := BuildMongoDBDeployment(nil, cfg)
	if err == nil {
		t.Fatalf("BuildMongoDBDeployment(nil) expected error")
	}
}

func TestBuildMongoDBDeployment_InvalidWorkload(t *testing.T) {
	cfg := testK8sConfig()
	w := &MongoDBWorkload{} // missing fields
	_, err := BuildMongoDBDeployment(w, cfg)
	if err == nil {
		t.Fatalf("BuildMongoDBDeployment() expected error for invalid workload")
	}
}

func TestBuildMongoDBDeployment_UnknownProfile(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()
	w.ProfileName = "nonexistent"
	_, err := BuildMongoDBDeployment(w, cfg)
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("BuildMongoDBDeployment() expected profile not found error, got: %v", err)
	}
}

func TestBuildMongoDBDeployment_WithoutPersistenceUsesEmptyDir(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()
	w.Persistence.Enabled = false

	deploy, err := BuildMongoDBDeployment(w, cfg)
	if err != nil {
		t.Fatalf("BuildMongoDBDeployment() error: %v", err)
	}

	if len(deploy.Spec.Template.Spec.Volumes) != 1 {
		t.Fatalf("Volumes count = %d, want 1", len(deploy.Spec.Template.Spec.Volumes))
	}
	vol := deploy.Spec.Template.Spec.Volumes[0]
	if vol.EmptyDir == nil {
		t.Fatalf("Volume should use emptyDir when persistence is disabled")
	}
	if vol.PersistentVolumeClaim != nil {
		t.Fatalf("Volume should not reference PVC when persistence is disabled")
	}

	if len(deploy.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("InitContainers count = %d, want 1", len(deploy.Spec.Template.Spec.InitContainers))
	}
	initContainer := deploy.Spec.Template.Spec.InitContainers[0]
	if len(initContainer.VolumeMounts) != 1 || initContainer.VolumeMounts[0].MountPath != mongoDataMountPath {
		t.Fatalf("InitContainer should keep shared data volume mount at %q", mongoDataMountPath)
	}

	container := deploy.Spec.Template.Spec.Containers[0]
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].MountPath != mongoDataMountPath {
		t.Fatalf("Container should keep shared data volume mount at %q", mongoDataMountPath)
	}
}

// --- BuildMongoDBService ---

func TestBuildMongoDBService(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()

	svc, err := BuildMongoDBService(w, cfg)
	if err != nil {
		t.Fatalf("BuildMongoDBService() error: %v", err)
	}

	// Verify object meta.
	if svc.Namespace != cfg.Namespace {
		t.Fatalf("Namespace = %q, want %q", svc.Namespace, cfg.Namespace)
	}
	if svc.Name != w.ServiceResourceName() {
		t.Fatalf("Name = %q, want %q", svc.Name, w.ServiceResourceName())
	}

	// Verify labels.
	if svc.Labels[appLabelKey] != w.App {
		t.Fatalf("Label[%q] = %q, want %q", appLabelKey, svc.Labels[appLabelKey], w.App)
	}

	// Verify selector.
	if svc.Spec.Selector[appLabelKey] != w.App {
		t.Fatalf("Selector[%q] = %q, want %q", appLabelKey, svc.Spec.Selector[appLabelKey], w.App)
	}

	// Verify port.
	profile := cfg.MongoDB["dev-single"]
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("Ports count = %d, want 1", len(svc.Spec.Ports))
	}
	port := svc.Spec.Ports[0]
	if port.Name != mongoPortName {
		t.Fatalf("Port Name = %q, want %q", port.Name, mongoPortName)
	}
	if port.Port != int32(profile.Port) {
		t.Fatalf("Port = %d, want %d", port.Port, profile.Port)
	}
	if port.TargetPort.StrVal != mongoPortName {
		t.Fatalf("TargetPort = %q, want %q", port.TargetPort.StrVal, mongoPortName)
	}
}

func TestBuildMongoDBService_NilWorkload(t *testing.T) {
	cfg := testK8sConfig()
	_, err := BuildMongoDBService(nil, cfg)
	if err == nil {
		t.Fatalf("BuildMongoDBService(nil) expected error")
	}
}

func TestBuildMongoDBService_UnknownProfile(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()
	w.ProfileName = "nonexistent"
	_, err := BuildMongoDBService(w, cfg)
	if err == nil {
		t.Fatalf("BuildMongoDBService() expected error for unknown profile")
	}
}

// --- BuildMongoDBPVC ---

func TestBuildMongoDBPVC(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()

	pvc, err := BuildMongoDBPVC(w, cfg)
	if err != nil {
		t.Fatalf("BuildMongoDBPVC() error: %v", err)
	}

	// Verify object meta.
	if pvc.Namespace != cfg.Namespace {
		t.Fatalf("Namespace = %q, want %q", pvc.Namespace, cfg.Namespace)
	}
	if pvc.Name != w.PVCResourceName() {
		t.Fatalf("Name = %q, want %q", pvc.Name, w.PVCResourceName())
	}

	// Verify labels.
	if pvc.Labels[appLabelKey] != w.App {
		t.Fatalf("Label[%q] = %q, want %q", appLabelKey, pvc.Labels[appLabelKey], w.App)
	}

	// Verify spec.
	profile := cfg.MongoDB["dev-single"]
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != profile.Storage.StorageClassName {
		t.Fatalf("StorageClassName mismatch")
	}
	if len(pvc.Spec.AccessModes) != 1 || pvc.Spec.AccessModes[0] != corev1.PersistentVolumeAccessMode(profile.Storage.AccessModes[0]) {
		t.Fatalf("AccessModes = %v, want [%v]", pvc.Spec.AccessModes, profile.Storage.AccessModes[0])
	}
	if pvc.Spec.VolumeMode == nil || *pvc.Spec.VolumeMode != corev1.PersistentVolumeMode(profile.Storage.VolumeMode) {
		t.Fatalf("VolumeMode mismatch")
	}
	capacity := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	wantCapacity := resource.MustParse(profile.Storage.Capacity)
	if capacity.Cmp(wantCapacity) != 0 {
		t.Fatalf("Capacity = %s, want %s", capacity.String(), wantCapacity.String())
	}
}

func TestBuildMongoDBPVC_NilWorkload(t *testing.T) {
	cfg := testK8sConfig()
	_, err := BuildMongoDBPVC(nil, cfg)
	if err == nil {
		t.Fatalf("BuildMongoDBPVC(nil) expected error")
	}
}

func TestBuildMongoDBPVC_UnknownProfile(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()
	w.ProfileName = "nonexistent"
	_, err := BuildMongoDBPVC(w, cfg)
	if err == nil {
		t.Fatalf("BuildMongoDBPVC() expected error for unknown profile")
	}
}

func TestBuildMongoDBPVC_PersistenceDisabled(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()
	w.Persistence.Enabled = false

	_, err := BuildMongoDBPVC(w, cfg)
	if err == nil || !strings.Contains(err.Error(), "未启用持久化") {
		t.Fatalf("BuildMongoDBPVC() expected persistence disabled error, got: %v", err)
	}
}

// --- BuildMongoDBSecret ---

func TestBuildMongoDBSecret(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()

	secret, err := BuildMongoDBSecret(w, cfg)
	if err != nil {
		t.Fatalf("BuildMongoDBSecret() error: %v", err)
	}

	// Verify object meta.
	if secret.Namespace != cfg.Namespace {
		t.Fatalf("Namespace = %q, want %q", secret.Namespace, cfg.Namespace)
	}
	if secret.Name != w.SecretResourceName() {
		t.Fatalf("Name = %q, want %q", secret.Name, w.SecretResourceName())
	}

	// Verify labels.
	if secret.Labels[appLabelKey] != w.App {
		t.Fatalf("Label[%q] = %q, want %q", appLabelKey, secret.Labels[appLabelKey], w.App)
	}

	// Verify type.
	if secret.Type != corev1.SecretTypeOpaque {
		t.Fatalf("Type = %q, want %q", secret.Type, corev1.SecretTypeOpaque)
	}

	// Verify data.
	profile := cfg.MongoDB["dev-single"]
	if string(secret.Data[mongoSecretUsernameKey]) != profile.AdminUsername {
		t.Fatalf("Username = %q, want %q", string(secret.Data[mongoSecretUsernameKey]), profile.AdminUsername)
	}
	password := string(secret.Data[mongoSecretPasswordKey])
	if len(password) < mongoPasswordMinLen {
		t.Fatalf("Password length = %d, want >= %d", len(password), mongoPasswordMinLen)
	}

	// Verify deterministic password.
	secret2, _ := BuildMongoDBSecret(w, cfg)
	if string(secret.Data[mongoSecretPasswordKey]) != string(secret2.Data[mongoSecretPasswordKey]) {
		t.Fatalf("Password should be deterministic")
	}
}

func TestBuildMongoDBSecret_NilWorkload(t *testing.T) {
	cfg := testK8sConfig()
	_, err := BuildMongoDBSecret(nil, cfg)
	if err == nil {
		t.Fatalf("BuildMongoDBSecret(nil) expected error")
	}
}

// --- CheckPVCCompatibility ---

func TestCheckPVCCompatibility(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()

	pvc, err := BuildMongoDBPVC(w, cfg)
	if err != nil {
		t.Fatalf("BuildMongoDBPVC() error: %v", err)
	}

	// Compatible PVC should pass.
	if err := CheckPVCCompatibility(pvc, w, cfg); err != nil {
		t.Fatalf("CheckPVCCompatibility() unexpected error: %v", err)
	}
}

func TestCheckPVCCompatibility_NilExisting(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()
	err := CheckPVCCompatibility(nil, w, cfg)
	if err == nil {
		t.Fatalf("CheckPVCCompatibility(nil, _) expected error")
	}
}

func TestCheckPVCCompatibility_NilDesired(t *testing.T) {
	cfg := testK8sConfig()
	err := CheckPVCCompatibility(&corev1.PersistentVolumeClaim{}, nil, cfg)
	if err == nil {
		t.Fatalf("CheckPVCCompatibility(_, nil) expected error")
	}
}

func TestCheckPVCCompatibility_EnvironmentMismatch(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()

	pvc, _ := BuildMongoDBPVC(w, cfg)
	pvc.Labels[dominionEnvironmentLabelKey] = "wrong-env"

	err := CheckPVCCompatibility(pvc, w, cfg)
	if err == nil || !strings.Contains(err.Error(), "不兼容") {
		t.Fatalf("Expected environment mismatch error, got: %v", err)
	}
}

func TestCheckPVCCompatibility_StorageClassMismatch(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()

	pvc, _ := BuildMongoDBPVC(w, cfg)
	wrongClass := "wrong-class"
	pvc.Spec.StorageClassName = &wrongClass

	err := CheckPVCCompatibility(pvc, w, cfg)
	if err == nil || !strings.Contains(err.Error(), "storageClassName") {
		t.Fatalf("Expected storage class mismatch error, got: %v", err)
	}
}

func TestCheckPVCCompatibility_CapacityShrinkDisallowed(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()

	pvc, _ := BuildMongoDBPVC(w, cfg)
	// Set existing capacity larger than desired.
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("100Gi")

	err := CheckPVCCompatibility(pvc, w, cfg)
	if err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("Expected capacity mismatch error, got: %v", err)
	}
}

func TestCheckPVCCompatibility_MissingStorageRequest(t *testing.T) {
	cfg := testK8sConfig()
	w := testMongoDBWorkload()

	pvc, _ := BuildMongoDBPVC(w, cfg)
	delete(pvc.Spec.Resources.Requests, corev1.ResourceStorage)

	err := CheckPVCCompatibility(pvc, w, cfg)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Expected missing capacity error, got: %v", err)
	}
}

// --- generateStablePassword ---

func Test_generateStablePassword(t *testing.T) {
	// Deterministic.
	p1 := generateStablePassword("app", "env", "svc")
	p2 := generateStablePassword("app", "env", "svc")
	if p1 != p2 {
		t.Fatalf("generateStablePassword() should be deterministic: %q != %q", p1, p2)
	}

	// Different inputs produce different passwords.
	p3 := generateStablePassword("other", "env", "svc")
	if p1 == p3 {
		t.Fatalf("generateStablePassword() should differ for different inputs")
	}

	// Minimum length.
	if len(p1) < mongoPasswordMinLen {
		t.Fatalf("generateStablePassword() length = %d, want >= %d", len(p1), mongoPasswordMinLen)
	}

	// Only valid characters.
	for _, c := range p1 {
		if !strings.ContainsRune(mongoPasswordAlphabet, c) {
			t.Fatalf("generateStablePassword() contains invalid character: %c", c)
		}
	}
}

// --- helper ---

func envVarsToMap(envs []corev1.EnvVar) map[string]string {
	m := make(map[string]string, len(envs))
	for _, e := range envs {
		if e.Value != "" {
			m[e.Name] = e.Value
		}
	}
	return m
}

func decodeHTTPRoute(t *testing.T, route *unstructured.Unstructured) *gatewayv1.HTTPRoute {
	t.Helper()

	raw, err := json.Marshal(route.Object)
	if err != nil {
		t.Fatalf("Marshal(route.Object) error: %v", err)
	}

	typedRoute := new(gatewayv1.HTTPRoute)
	if err := json.Unmarshal(raw, typedRoute); err != nil {
		t.Fatalf("Unmarshal(route.Object) error: %v", err)
	}

	return typedRoute
}

func assertHTTPRouteEnvHeader(t *testing.T, route *gatewayv1.HTTPRoute, wantLabel string) {
	t.Helper()

	if len(route.Spec.Rules) != 1 {
		t.Fatalf("Rules count = %d, want 1", len(route.Spec.Rules))
	}
	if len(route.Spec.Rules[0].Matches) != 1 {
		t.Fatalf("Matches count = %d, want 1", len(route.Spec.Rules[0].Matches))
	}

	headers := route.Spec.Rules[0].Matches[0].Headers
	if len(headers) != 1 {
		t.Fatalf("Headers count = %d, want 1", len(headers))
	}
	if headers[0].Type == nil || *headers[0].Type != gatewayv1.HeaderMatchExact {
		t.Fatalf("Header type = %v, want %q", headers[0].Type, gatewayv1.HeaderMatchExact)
	}
	if headers[0].Name != EnvHeaderMatchName {
		t.Fatalf("Header name = %v, want %q", headers[0].Name, EnvHeaderMatchName)
	}
	if headers[0].Value != wantLabel {
		t.Fatalf("Header value = %q, want %q", headers[0].Value, wantLabel)
	}
}

// verify that MongoDB resources implement the compile-time interface checks.
var _ *appsv1.Deployment = (*appsv1.Deployment)(nil)
var _ *corev1.Service = (*corev1.Service)(nil)
var _ *corev1.PersistentVolumeClaim = (*corev1.PersistentVolumeClaim)(nil)
var _ *corev1.Secret = (*corev1.Secret)(nil)

// Ensure namespace from cfg is consistently used.
func TestBuildMongoDBResources_UseConfigNamespace(t *testing.T) {
	cfg := testK8sConfig()
	cfg.Namespace = "custom-namespace"
	w := testMongoDBWorkload()

	deploy, _ := BuildMongoDBDeployment(w, cfg)
	svc, _ := BuildMongoDBService(w, cfg)
	pvc, _ := BuildMongoDBPVC(w, cfg)
	secret, _ := BuildMongoDBSecret(w, cfg)

	for _, obj := range []metav1.Object{deploy, svc, pvc, secret} {
		if obj.GetNamespace() != "custom-namespace" {
			t.Fatalf("%s Namespace = %q, want %q", obj.GetName(), obj.GetNamespace(), "custom-namespace")
		}
	}
}
