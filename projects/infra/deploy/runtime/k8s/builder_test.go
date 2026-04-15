package k8s

import (
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func testHTTPRouteWorkload() *HTTPRouteWorkload {
	return &HTTPRouteWorkload{
		ServiceName:      "myservice",
		EnvironmentName:  "dev",
		App:              "myapp",
		Hostnames:        []string{"myapp.example.com"},
		BackendService:   "myservice-backend",
		GatewayName:      "test-gateway",
		GatewayNamespace: "ingress",
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
	w := testHTTPRouteWorkload()

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
