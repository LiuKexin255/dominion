package k8s

import (
	"strings"
	"testing"

	"dominion/tools/deploy/pkg/config"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type deploymentExpectation struct {
	name         string
	namespace    string
	managedBy    string
	app          string
	dominionApp  string
	serviceName  string
	environment  string
	replicas     int32
	image        string
	ports        []corev1.ContainerPort
	volumes      []corev1.Volume
	volumeMounts []corev1.VolumeMount
	env          []corev1.EnvVar
}

type serviceExpectation struct {
	name        string
	namespace   string
	managedBy   string
	app         string
	dominionApp string
	serviceName string
	environment string
	ports       []corev1.ServicePort
}

type httpRouteExpectation struct {
	apiVersion  string
	kind        string
	name        string
	namespace   string
	managedBy   string
	app         string
	dominionApp string
	serviceName string
	environment string
	hostnames   []string
	gatewayName string
	gatewayNS   string
	rulePaths   []string
	rulePorts   []int64
}

func TestBuildDeployment(t *testing.T) {
	tests := []struct {
		name        string
		workload    *DeploymentWorkload
		k8sConfig   *K8sConfig
		wantErr     bool
		errContains string
		want        *deploymentExpectation
	}{
		{
			name:      "success",
			workload:  newTestDeploymentWorkload(),
			k8sConfig: newTestK8sConfig(),
			want: &deploymentExpectation{
				name:        newTestDeploymentWorkload().WorkloadName(),
				namespace:   "team-dev",
				managedBy:   "deploy-tool",
				app:         "grpc-hello-world",
				dominionApp: "grpc-hello-world",
				serviceName: "gateway",
				environment: "dev",
				replicas:    3,
				image:       "registry.local/gateway:latest",
				ports: []corev1.ContainerPort{
					{Name: "http", ContainerPort: 8080},
					{Name: "grpc", ContainerPort: 9090},
				},
				volumes:      nil,
				volumeMounts: nil,
				env: []corev1.EnvVar{
					{Name: reservedEnvNameServiceApp, Value: "grpc-hello-world"},
					{Name: reservedEnvNameDominionEnvironment, Value: "dev"},
					{Name: reservedEnvNamePodNamespace, Value: "team-dev"},
				},
			},
		},
		{
			name:      "renders tls secret volume mount and env vars",
			workload:  newTestDeploymentWorkloadWithTLS(),
			k8sConfig: newTestK8sConfigWithTLS(),
			want: &deploymentExpectation{
				name:        newTestDeploymentWorkloadWithTLS().WorkloadName(),
				namespace:   "team-dev",
				managedBy:   "deploy-tool",
				app:         "grpc-hello-world",
				dominionApp: "grpc-hello-world",
				serviceName: "gateway",
				environment: "dev",
				replicas:    3,
				image:       "registry.local/gateway:latest",
				ports: []corev1.ContainerPort{
					{Name: "http", ContainerPort: 8080},
					{Name: "grpc", ContainerPort: 9090},
				},
				volumes: []corev1.Volume{{
					Name: "tls",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{
							{Secret: &corev1.SecretProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "gateway-tls"}}},
							{ConfigMap: &corev1.ConfigMapProjection{
								LocalObjectReference: corev1.LocalObjectReference{Name: "gateway-ca"},
								Items:                []corev1.KeyToPath{{Key: "bundle.pem", Path: "ca.crt"}},
							}},
						}},
					},
				}},
				volumeMounts: []corev1.VolumeMount{{
					Name:      "tls",
					MountPath: "/etc/tls",
					ReadOnly:  true,
				}},
				env: []corev1.EnvVar{
					{Name: reservedEnvNameServiceApp, Value: "grpc-hello-world"},
					{Name: reservedEnvNameDominionEnvironment, Value: "dev"},
					{Name: reservedEnvNamePodNamespace, Value: "team-dev"},
					{Name: "TLS_CERT_FILE", Value: "/etc/tls/tls.crt"},
					{Name: "TLS_KEY_FILE", Value: "/etc/tls/tls.key"},
					{Name: "TLS_CA_FILE", Value: "/etc/tls/ca.crt"},
					{Name: "TLS_SERVER_NAME", Value: "gateway.internal.example.com"},
				},
			},
		},
		{
			name: "injects service app env from workload app while keeping dominion label",
			workload: func() *DeploymentWorkload {
				workload := newTestDeploymentWorkload()
				workload.App = "gateway-service"
				workload.DominionApp = "grpc-hello-world"
				return workload
			}(),
			k8sConfig: newTestK8sConfig(),
			want: &deploymentExpectation{
				name: func() string {
					workload := newTestDeploymentWorkload()
					workload.App = "gateway-service"
					return workload.WorkloadName()
				}(),
				namespace:   "team-dev",
				managedBy:   "deploy-tool",
				app:         "gateway-service",
				dominionApp: "grpc-hello-world",
				serviceName: "gateway",
				environment: "dev",
				replicas:    3,
				image:       "registry.local/gateway:latest",
				ports: []corev1.ContainerPort{
					{Name: "http", ContainerPort: 8080},
					{Name: "grpc", ContainerPort: 9090},
				},
				volumes:      nil,
				volumeMounts: nil,
				env: []corev1.EnvVar{
					{Name: reservedEnvNameServiceApp, Value: "gateway-service"},
					{Name: reservedEnvNameDominionEnvironment, Value: "dev"},
					{Name: reservedEnvNamePodNamespace, Value: "team-dev"},
				},
			},
		},
		{
			name:        "returns error when deployment ports contain nil entry",
			workload:    newTestDeploymentWorkloadWithNilPort(),
			k8sConfig:   newTestK8sConfig(),
			wantErr:     true,
			errContains: "构建 deployment ports 失败: 端口为空",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoadK8sConfig(t, tt.k8sConfig)

			got, err := BuildDeployment(tt.workload)
			if tt.wantErr {
				if err == nil {
					t.Fatal("BuildDeployment() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}
				if got != nil {
					t.Fatal("BuildDeployment() returned object on failure")
				}

				return
			}

			if err != nil {
				t.Fatalf("BuildDeployment() failed: %v", err)
			}

			assertDeployment(t, got, tt.want)
		})
	}
}

func TestBuildService(t *testing.T) {
	tests := []struct {
		name        string
		workload    *DeploymentWorkload
		k8sConfig   *K8sConfig
		wantErr     bool
		errContains string
		want        *serviceExpectation
	}{
		{
			name:      "success",
			workload:  newTestDeploymentWorkload(),
			k8sConfig: newTestK8sConfig(),
			want: &serviceExpectation{
				name:        newTestDeploymentWorkload().ServiceResourceName(),
				namespace:   "team-dev",
				managedBy:   "deploy-tool",
				app:         "grpc-hello-world",
				dominionApp: "grpc-hello-world",
				serviceName: "gateway",
				environment: "dev",
				ports: []corev1.ServicePort{
					{Name: "http", Port: 8080, TargetPort: intstr.FromString("http")},
					{Name: "grpc", Port: 9090, TargetPort: intstr.FromString("grpc")},
				},
			},
		},
		{
			name:        "returns error when service ports contain nil entry",
			workload:    newTestDeploymentWorkloadWithNilPort(),
			k8sConfig:   newTestK8sConfig(),
			wantErr:     true,
			errContains: "构建 service ports 失败: 端口为空",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoadK8sConfig(t, tt.k8sConfig)

			got, err := BuildService(tt.workload)
			if tt.wantErr {
				if err == nil {
					t.Fatal("BuildService() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}
				if got != nil {
					t.Fatal("BuildService() returned object on failure")
				}

				return
			}

			if err != nil {
				t.Fatalf("BuildService() failed: %v", err)
			}

			assertService(t, got, tt.want)
		})
	}
}

func TestBuildHTTPRoute(t *testing.T) {
	tests := []struct {
		name      string
		workload  *HTTPRouteWorkload
		k8sConfig *K8sConfig
		want      *httpRouteExpectation
	}{
		{
			name:      "success",
			workload:  newTestHTTPRouteWorkload(),
			k8sConfig: newTestK8sConfig(),
			want: &httpRouteExpectation{
				apiVersion:  gatewayv1.GroupVersion.String(),
				kind:        httpRouteKind,
				name:        newTestHTTPRouteWorkload().ResourceName(),
				namespace:   "team-dev",
				managedBy:   "deploy-tool",
				app:         "grpc-hello-world",
				dominionApp: "grpc-hello-world",
				serviceName: "gateway",
				environment: "dev",
				hostnames:   []string{"gateway.example.com", "gateway.dev.example.com"},
				gatewayName: "shared-gateway",
				gatewayNS:   "infra-system",
				rulePaths:   []string{"/v1", "/grpc"},
				rulePorts:   []int64{8080, 9090},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoadK8sConfig(t, tt.k8sConfig)

			got, err := BuildHTTPRoute(tt.workload)
			if err != nil {
				t.Fatalf("BuildHTTPRoute() failed: %v", err)
			}

			assertHTTPRoute(t, got, tt.want)
		})
	}
}

func TestBuildHTTPRoute_TLSDoesNotAffectRouteRendering(t *testing.T) {
	stubLoadK8sConfig(t, newTestK8sConfig())

	got, err := BuildHTTPRoute(newTestHTTPRouteWorkload())
	if err != nil {
		t.Fatalf("BuildHTTPRoute() failed: %v", err)
	}

	assertHTTPRoute(t, got, &httpRouteExpectation{
		apiVersion:  gatewayv1.GroupVersion.String(),
		kind:        httpRouteKind,
		name:        newTestHTTPRouteWorkload().ResourceName(),
		namespace:   "team-dev",
		managedBy:   "deploy-tool",
		app:         "grpc-hello-world",
		dominionApp: "grpc-hello-world",
		serviceName: "gateway",
		environment: "dev",
		hostnames:   []string{"gateway.example.com", "gateway.dev.example.com"},
		gatewayName: "shared-gateway",
		gatewayNS:   "infra-system",
		rulePaths:   []string{"/v1", "/grpc"},
		rulePorts:   []int64{8080, 9090},
	})
}

func Test_buildContainerPorts(t *testing.T) {
	tests := []struct {
		name        string
		ports       []*DeploymentPort
		wantNil     bool
		want        []corev1.ContainerPort
		wantErr     bool
		errContains string
	}{
		{
			name:    "nil ports returns nil",
			ports:   nil,
			wantNil: true,
		},
		{
			name:  "maps deployment ports",
			ports: []*DeploymentPort{{Name: "http", Port: 8080}, {Name: "grpc", Port: 9090}},
			want: []corev1.ContainerPort{
				{Name: "http", ContainerPort: 8080},
				{Name: "grpc", ContainerPort: 9090},
			},
		},
		{
			name:        "nil port entry returns error",
			ports:       []*DeploymentPort{nil},
			wantErr:     true,
			errContains: "端口为空",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildContainerPorts(tt.ports)
			if tt.wantErr {
				if err == nil {
					t.Fatal("buildContainerPorts() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}
				if got != nil {
					t.Fatal("buildContainerPorts() returned ports on failure")
				}

				return
			}

			if err != nil {
				t.Fatalf("buildContainerPorts() failed: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("ports = %#v, want nil", got)
				}

				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ports len = %d, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("ports[%d] = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func Test_buildServicePorts(t *testing.T) {
	tests := []struct {
		name        string
		ports       []*DeploymentPort
		wantNil     bool
		want        []corev1.ServicePort
		wantErr     bool
		errContains string
	}{
		{
			name:    "nil ports returns nil",
			ports:   nil,
			wantNil: true,
		},
		{
			name:  "maps service ports with named target ports",
			ports: []*DeploymentPort{{Name: "http", Port: 8080}, {Name: "grpc", Port: 9090}},
			want: []corev1.ServicePort{
				{Name: "http", Port: 8080, TargetPort: intstr.FromString("http")},
				{Name: "grpc", Port: 9090, TargetPort: intstr.FromString("grpc")},
			},
		},
		{
			name:        "nil port entry returns error",
			ports:       []*DeploymentPort{nil},
			wantErr:     true,
			errContains: "端口为空",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildServicePorts(tt.ports)
			if tt.wantErr {
				if err == nil {
					t.Fatal("buildServicePorts() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}
				if got != nil {
					t.Fatal("buildServicePorts() returned ports on failure")
				}

				return
			}

			if err != nil {
				t.Fatalf("buildServicePorts() failed: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("ports = %#v, want nil", got)
				}

				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ports len = %d, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i].Name != tt.want[i].Name || got[i].Port != tt.want[i].Port || got[i].TargetPort != tt.want[i].TargetPort {
					t.Fatalf("ports[%d] = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func newTestK8sConfig() *K8sConfig {
	return &K8sConfig{
		Namespace: "team-dev",
		ManagedBy: "deploy-tool",
		Gateway: GatewayConfig{
			Name:      "shared-gateway",
			Namespace: "infra-system",
		},
	}
}

func newTestK8sConfigWithTLS() *K8sConfig {
	k8sConfig := newTestK8sConfig()
	k8sConfig.TLS = TLSConfig{
		Secret: "gateway-tls",
		Domain: "gateway.internal.example.com",
		CAConfigMap: ConfigMapConfig{
			Name: "gateway-ca",
			Key:  "bundle.pem",
		},
	}

	return k8sConfig
}

func newTestDeploymentWorkload() *DeploymentWorkload {
	return &DeploymentWorkload{
		ServiceName:     "gateway",
		EnvironmentName: "dev",
		App:             "grpc-hello-world",
		DominionApp:     "grpc-hello-world",
		Desc:            "gateway service",
		Image:           "registry.local/gateway:latest",
		Replicas:        3,
		Ports: []*DeploymentPort{
			{Name: "http", Port: 8080},
			{Name: "grpc", Port: 9090},
		},
	}
}

func newTestDeploymentWorkloadWithNilPort() *DeploymentWorkload {
	workload := newTestDeploymentWorkload()
	workload.Ports = []*DeploymentPort{{Name: "http", Port: 8080}, nil}

	return workload
}

func newTestDeploymentWorkloadWithTLS() *DeploymentWorkload {
	workload := newTestDeploymentWorkload()
	workload.TLSEnabled = true

	return workload
}

func newTestHTTPRouteWorkload() *HTTPRouteWorkload {
	backendService := newTestDeploymentWorkload().ServiceResourceName()

	return &HTTPRouteWorkload{
		ServiceName:      "gateway",
		EnvironmentName:  "dev",
		App:              "grpc-hello-world",
		DominionApp:      "grpc-hello-world",
		Hostnames:        []string{"gateway.example.com", "gateway.dev.example.com"},
		BackendService:   backendService,
		GatewayName:      "shared-gateway",
		GatewayNamespace: "infra-system",
		Matches: []*HTTPRoutePathMatch{
			{Type: config.HTTPPathMatchTypePrefix, Value: "/v1", BackendName: "http", BackendPort: 8080},
			{Type: config.HTTPPathMatchTypePrefix, Value: "/grpc", BackendName: "grpc", BackendPort: 9090},
		},
	}
}

func assertDeployment(t *testing.T, got *appsv1.Deployment, want *deploymentExpectation) {
	t.Helper()

	if got.Name != want.name {
		t.Fatalf("name = %q, want %q", got.Name, want.name)
	}
	if got.Namespace != want.namespace {
		t.Fatalf("namespace = %q, want %q", got.Namespace, want.namespace)
	}
	assertManagedLabels(t, got.Labels, want.app, want.serviceName, want.dominionApp, want.environment, want.managedBy)
	assertSelectorLabels(t, got.Spec.Selector.MatchLabels, want.app, want.serviceName, want.dominionApp, want.environment)
	assertManagedLabels(t, got.Spec.Template.Labels, want.app, want.serviceName, want.dominionApp, want.environment, want.managedBy)
	if got.Spec.Replicas == nil || *got.Spec.Replicas != want.replicas {
		t.Fatalf("replicas = %v, want %d", got.Spec.Replicas, want.replicas)
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
	assertVolumes(t, got.Spec.Template.Spec.Volumes, want.volumes)
	assertVolumeMounts(t, container.VolumeMounts, want.volumeMounts)
	assertContainerEnv(t, container.Env, want.env)
}

func assertVolumes(t *testing.T, got []corev1.Volume, want []corev1.Volume) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("volumes len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Fatalf("volumes[%d].name = %q, want %q", i, got[i].Name, want[i].Name)
		}
		if want[i].Projected == nil {
			if got[i].Projected != nil {
				t.Fatalf("volumes[%d].projected = %#v, want nil", i, got[i].Projected)
			}
			continue
		}
		if got[i].Projected == nil {
			t.Fatalf("volumes[%d].projected = nil, want non-nil", i)
		}
		if len(got[i].Projected.Sources) != len(want[i].Projected.Sources) {
			t.Fatalf("volumes[%d].projected.sources len = %d, want %d", i, len(got[i].Projected.Sources), len(want[i].Projected.Sources))
		}
		for j := range want[i].Projected.Sources {
			assertProjectedVolumeSource(t, got[i].Projected.Sources[j], want[i].Projected.Sources[j], i, j)
		}
	}
}

func assertProjectedVolumeSource(t *testing.T, got corev1.VolumeProjection, want corev1.VolumeProjection, volumeIndex int, sourceIndex int) {
	t.Helper()

	if want.Secret != nil {
		if got.Secret == nil {
			t.Fatalf("volumes[%d].projected.sources[%d].secret = nil, want non-nil", volumeIndex, sourceIndex)
		}
		if got.Secret.Name != want.Secret.Name {
			t.Fatalf("volumes[%d].projected.sources[%d].secret.name = %q, want %q", volumeIndex, sourceIndex, got.Secret.Name, want.Secret.Name)
		}
		return
	}
	if want.ConfigMap != nil {
		if got.ConfigMap == nil {
			t.Fatalf("volumes[%d].projected.sources[%d].configMap = nil, want non-nil", volumeIndex, sourceIndex)
		}
		if got.ConfigMap.Name != want.ConfigMap.Name {
			t.Fatalf("volumes[%d].projected.sources[%d].configMap.name = %q, want %q", volumeIndex, sourceIndex, got.ConfigMap.Name, want.ConfigMap.Name)
		}
		if len(got.ConfigMap.Items) != len(want.ConfigMap.Items) {
			t.Fatalf("volumes[%d].projected.sources[%d].configMap.items len = %d, want %d", volumeIndex, sourceIndex, len(got.ConfigMap.Items), len(want.ConfigMap.Items))
		}
		for itemIndex := range want.ConfigMap.Items {
			if got.ConfigMap.Items[itemIndex].Key != want.ConfigMap.Items[itemIndex].Key {
				t.Fatalf("volumes[%d].projected.sources[%d].configMap.items[%d].key = %q, want %q", volumeIndex, sourceIndex, itemIndex, got.ConfigMap.Items[itemIndex].Key, want.ConfigMap.Items[itemIndex].Key)
			}
			if got.ConfigMap.Items[itemIndex].Path != want.ConfigMap.Items[itemIndex].Path {
				t.Fatalf("volumes[%d].projected.sources[%d].configMap.items[%d].path = %q, want %q", volumeIndex, sourceIndex, itemIndex, got.ConfigMap.Items[itemIndex].Path, want.ConfigMap.Items[itemIndex].Path)
			}
		}
		return
	}

	t.Fatalf("volumes[%d].projected.sources[%d] expected secret or configMap", volumeIndex, sourceIndex)
}

func assertVolumeMounts(t *testing.T, got []corev1.VolumeMount, want []corev1.VolumeMount) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("volume mounts len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Fatalf("volumeMounts[%d].name = %q, want %q", i, got[i].Name, want[i].Name)
		}
		if got[i].MountPath != want[i].MountPath {
			t.Fatalf("volumeMounts[%d].mountPath = %q, want %q", i, got[i].MountPath, want[i].MountPath)
		}
		if got[i].ReadOnly != want[i].ReadOnly {
			t.Fatalf("volumeMounts[%d].readOnly = %v, want %v", i, got[i].ReadOnly, want[i].ReadOnly)
		}
	}
}

func assertContainerEnv(t *testing.T, got []corev1.EnvVar, want []corev1.EnvVar) {
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
		assertEnvValueSource(t, got[i].ValueFrom, want[i].ValueFrom, i)
	}
}

func assertEnvValueSource(t *testing.T, got *corev1.EnvVarSource, want *corev1.EnvVarSource, index int) {
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
	if want.FieldRef == nil {
		if got.FieldRef != nil {
			t.Fatalf("env[%d].fieldRef = %#v, want nil", index, got.FieldRef)
		}
		return
	}
	if got.FieldRef == nil {
		t.Fatalf("env[%d].fieldRef = nil, want non-nil", index)
	}
	if got.FieldRef.FieldPath != want.FieldRef.FieldPath {
		t.Fatalf("env[%d].fieldRef.fieldPath = %q, want %q", index, got.FieldRef.FieldPath, want.FieldRef.FieldPath)
	}
}

func assertService(t *testing.T, got *corev1.Service, want *serviceExpectation) {
	t.Helper()

	if got.Name != want.name {
		t.Fatalf("name = %q, want %q", got.Name, want.name)
	}
	if got.Namespace != want.namespace {
		t.Fatalf("namespace = %q, want %q", got.Namespace, want.namespace)
	}
	assertManagedLabels(t, got.Labels, want.app, want.serviceName, want.dominionApp, want.environment, want.managedBy)
	assertSelectorLabels(t, got.Spec.Selector, want.app, want.serviceName, want.dominionApp, want.environment)
	if len(got.Spec.Ports) != len(want.ports) {
		t.Fatalf("ports len = %d, want %d", len(got.Spec.Ports), len(want.ports))
	}
	for i := range want.ports {
		if got.Spec.Ports[i].Name != want.ports[i].Name || got.Spec.Ports[i].Port != want.ports[i].Port || got.Spec.Ports[i].TargetPort != want.ports[i].TargetPort {
			t.Fatalf("ports[%d] = %#v, want %#v", i, got.Spec.Ports[i], want.ports[i])
		}
	}
}

func assertHTTPRoute(t *testing.T, got *unstructured.Unstructured, want *httpRouteExpectation) {
	t.Helper()

	if got.GetAPIVersion() != want.apiVersion {
		t.Fatalf("apiVersion = %q, want %q", got.GetAPIVersion(), want.apiVersion)
	}
	if got.GetKind() != want.kind {
		t.Fatalf("kind = %q, want %q", got.GetKind(), want.kind)
	}
	if got.GetName() != want.name {
		t.Fatalf("name = %q, want %q", got.GetName(), want.name)
	}
	if got.GetNamespace() != want.namespace {
		t.Fatalf("namespace = %q, want %q", got.GetNamespace(), want.namespace)
	}
	assertManagedLabels(t, got.GetLabels(), want.app, want.serviceName, want.dominionApp, want.environment, want.managedBy)

	hostnames, found, err := unstructured.NestedStringSlice(got.Object, "spec", "hostnames")
	if err != nil || !found {
		t.Fatalf("hostnames lookup failed: found=%v err=%v", found, err)
	}
	if len(hostnames) != len(want.hostnames) {
		t.Fatalf("hostnames len = %d, want %d", len(hostnames), len(want.hostnames))
	}
	for i := range want.hostnames {
		if hostnames[i] != want.hostnames[i] {
			t.Fatalf("hostnames[%d] = %q, want %q", i, hostnames[i], want.hostnames[i])
		}
	}

	parentRefs, found, err := unstructured.NestedSlice(got.Object, "spec", "parentRefs")
	if err != nil || !found {
		t.Fatalf("parentRefs lookup failed: found=%v err=%v", found, err)
	}
	if len(parentRefs) != 1 {
		t.Fatalf("parentRefs len = %d, want 1", len(parentRefs))
	}
	parentRef, ok := parentRefs[0].(map[string]any)
	if !ok {
		t.Fatalf("parentRef type = %T, want map[string]any", parentRefs[0])
	}
	if parentRef["name"] != want.gatewayName || parentRef["namespace"] != want.gatewayNS {
		t.Fatalf("parentRef = %#v, want gateway reference", parentRef)
	}

	rules, found, err := unstructured.NestedSlice(got.Object, "spec", "rules")
	if err != nil || !found {
		t.Fatalf("rules lookup failed: found=%v err=%v", found, err)
	}
	if len(rules) != len(want.rulePaths) {
		t.Fatalf("rules len = %d, want %d", len(rules), len(want.rulePaths))
	}
	for i := range want.rulePaths {
		assertHTTPRouteRule(t, rules[i], want.rulePaths[i], want.rulePorts[i])
	}
}

func assertManagedLabels(t *testing.T, got map[string]string, app string, serviceName string, dominionApp string, dominionEnvironment string, managedBy string) {
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
	if got[dominionAppLabelKey] != dominionApp {
		t.Fatalf("dominion app label = %q, want %q", got[dominionAppLabelKey], dominionApp)
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
	if len(got) != 5 {
		t.Fatalf("managed labels len = %d, want 5", len(got))
	}
}

func assertSelectorLabels(t *testing.T, got map[string]string, app string, serviceName string, dominionApp string, dominionEnvironment string) {
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
	if got[dominionAppLabelKey] != dominionApp {
		t.Fatalf("dominion app label = %q, want %q", got[dominionAppLabelKey], dominionApp)
	}
	if got[dominionEnvironmentLabelKey] != dominionEnvironment {
		t.Fatalf("dominion environment label = %q, want %q", got[dominionEnvironmentLabelKey], dominionEnvironment)
	}
	if len(got) != 4 {
		t.Fatalf("selector labels len = %d, want 4", len(got))
	}
}

func assertHTTPRouteRule(t *testing.T, rawRule any, pathValue string, backendPort int64) {
	t.Helper()

	rule, ok := rawRule.(map[string]any)
	if !ok {
		t.Fatalf("rule type = %T, want map[string]any", rawRule)
	}

	matches, ok := rule["matches"].([]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("matches = %#v, want one match", rule["matches"])
	}
	match, ok := matches[0].(map[string]any)
	if !ok {
		t.Fatalf("match type = %T, want map[string]any", matches[0])
	}
	path, ok := match["path"].(map[string]any)
	if !ok {
		t.Fatalf("path type = %T, want map[string]any", match["path"])
	}
	if path["type"] != string(config.HTTPPathMatchTypePrefix) || path["value"] != pathValue {
		t.Fatalf("path = %#v, want PathPrefix/%s", path, pathValue)
	}

	backendRefs, ok := rule["backendRefs"].([]any)
	if !ok || len(backendRefs) != 1 {
		t.Fatalf("backendRefs = %#v, want one backendRef", rule["backendRefs"])
	}
	backendRef, ok := backendRefs[0].(map[string]any)
	if !ok {
		t.Fatalf("backendRef type = %T, want map[string]any", backendRefs[0])
	}
	if backendRef["name"] != newTestDeploymentWorkload().ServiceResourceName() {
		t.Fatalf("backendRef.name = %#v, want %q", backendRef["name"], newTestDeploymentWorkload().ServiceResourceName())
	}
	if backendRef["port"] != float64(backendPort) {
		t.Fatalf("backendRef = %#v, want service name with port %d", backendRef, backendPort)
	}
}
