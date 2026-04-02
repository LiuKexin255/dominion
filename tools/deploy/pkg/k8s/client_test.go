package k8s

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	apiRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestNewRuntimeClient(t *testing.T) {
	staticConfig := LoadK8sConfig()

	tests := []struct {
		name           string
		kubeconfigPath string
		restConfig     *rest.Config
		loadErr        error
		wantErr        bool
		errContains    string
		wantErrPath    bool
	}{
		{
			name:           "custom kubeconfig path success",
			kubeconfigPath: "/tmp/custom-kubeconfig",
			restConfig:     &rest.Config{Host: "https://cluster.example.test"},
		},
		{
			name:           "explicit path failure includes path",
			kubeconfigPath: "/tmp/missing-kubeconfig",
			loadErr:        errors.New("boom"),
			wantErr:        true,
			errContains:    "加载 kubeconfig",
			wantErrPath:    true,
		},
		{
			name:        "default loading failure uses default-rules message",
			loadErr:     errors.New("boom"),
			wantErr:     true,
			errContains: "按默认规则加载 kubeconfig 失败",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalConfigLoader := runtimeRESTConfigLoader
			originalTypedConstructor := runtimeTypedClientConstructor
			originalDynamicConstructor := runtimeDynamicClientConstructor
			t.Cleanup(func() {
				runtimeRESTConfigLoader = originalConfigLoader
				runtimeTypedClientConstructor = originalTypedConstructor
				runtimeDynamicClientConstructor = originalDynamicConstructor
			})

			var gotKubeconfigPath string
			var gotTypedConfig *rest.Config
			var gotDynamicConfig *rest.Config

			fakeTypedClient := kubernetesfake.NewSimpleClientset()
			fakeDynamicClient := dynamicfake.NewSimpleDynamicClient(apiRuntime.NewScheme())

			runtimeRESTConfigLoader = func(kubeconfigPath string) (*rest.Config, error) {
				gotKubeconfigPath = kubeconfigPath
				if tt.loadErr != nil {
					return nil, tt.loadErr
				}

				return tt.restConfig, nil
			}
			runtimeTypedClientConstructor = func(config *rest.Config) (kubernetes.Interface, error) {
				gotTypedConfig = config
				return fakeTypedClient, nil
			}
			runtimeDynamicClientConstructor = func(config *rest.Config) (dynamic.Interface, error) {
				gotDynamicConfig = config
				return fakeDynamicClient, nil
			}

			client, err := NewRuntimeClient(tt.kubeconfigPath)
			if tt.wantErr {
				if err == nil {
					t.Fatal("NewRuntimeClient() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}
				if tt.wantErrPath && !strings.Contains(err.Error(), tt.kubeconfigPath) {
					t.Fatalf("error = %v, want contains path %q", err, tt.kubeconfigPath)
				}
				if client != nil {
					t.Fatal("NewRuntimeClient() returned runtime client on failure")
				}

				return
			}

			if err != nil {
				t.Fatalf("NewRuntimeClient() failed: %v", err)
			}
			if gotKubeconfigPath != tt.kubeconfigPath {
				t.Fatalf("kubeconfig path = %q, want %q", gotKubeconfigPath, tt.kubeconfigPath)
			}
			if gotTypedConfig != tt.restConfig {
				t.Fatal("typed client did not receive the original rest config")
			}
			if gotDynamicConfig != tt.restConfig {
				t.Fatal("dynamic client did not receive the original rest config")
			}

			assertRuntimeClient(t, client, fakeTypedClient, fakeDynamicClient, staticConfig)
		})
	}
}

func TestNewRuntimeClientWithConfig(t *testing.T) {
	wantK8sConfig := LoadK8sConfig()

	tests := []struct {
		name              string
		restConfig        *rest.Config
		typedErr          error
		dynamicErr        error
		wantErr           bool
		errContains       string
		wantTypedClient   kubernetes.Interface
		wantDynamicClient dynamic.Interface
	}{
		{
			name:              "success",
			restConfig:        &rest.Config{Host: "https://cluster.example.test"},
			wantTypedClient:   kubernetesfake.NewSimpleClientset(),
			wantDynamicClient: dynamicfake.NewSimpleDynamicClient(apiRuntime.NewScheme()),
		},
		{
			name:        "nil rest config",
			wantErr:     true,
			errContains: "rest config 为空",
		},
		{
			name:        "typed client init failure",
			restConfig:  &rest.Config{Host: "https://cluster.example.test"},
			typedErr:    errors.New("typed boom"),
			wantErr:     true,
			errContains: "初始化 typed client 失败",
		},
		{
			name:        "dynamic client init failure",
			restConfig:  &rest.Config{Host: "https://cluster.example.test"},
			dynamicErr:  errors.New("dynamic boom"),
			wantErr:     true,
			errContains: "初始化 dynamic client 失败",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalTypedConstructor := runtimeTypedClientConstructor
			originalDynamicConstructor := runtimeDynamicClientConstructor
			t.Cleanup(func() {
				runtimeTypedClientConstructor = originalTypedConstructor
				runtimeDynamicClientConstructor = originalDynamicConstructor
			})

			typedClient := tt.wantTypedClient
			if typedClient == nil {
				typedClient = kubernetesfake.NewSimpleClientset()
			}
			dynamicClient := tt.wantDynamicClient
			if dynamicClient == nil {
				dynamicClient = dynamicfake.NewSimpleDynamicClient(apiRuntime.NewScheme())
			}

			runtimeTypedClientConstructor = func(config *rest.Config) (kubernetes.Interface, error) {
				if tt.typedErr != nil {
					return nil, tt.typedErr
				}

				return typedClient, nil
			}
			runtimeDynamicClientConstructor = func(config *rest.Config) (dynamic.Interface, error) {
				if tt.dynamicErr != nil {
					return nil, tt.dynamicErr
				}

				return dynamicClient, nil
			}

			client, err := NewRuntimeClientWithConfig(tt.restConfig)
			if tt.wantErr {
				if err == nil {
					t.Fatal("NewRuntimeClientWithConfig() expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want contains %q", err, tt.errContains)
				}
				if client != nil {
					t.Fatal("NewRuntimeClientWithConfig() returned runtime client on failure")
				}

				return
			}

			if err != nil {
				t.Fatalf("NewRuntimeClientWithConfig() failed: %v", err)
			}

			assertRuntimeClient(t, client, typedClient, dynamicClient, wantK8sConfig)
		})
	}
}

func assertRuntimeClient(
	t *testing.T,
	client *RuntimeClient,
	wantTypedClient kubernetes.Interface,
	wantDynamicClient dynamic.Interface,
	wantK8sConfig *K8sConfig,
) {
	t.Helper()

	if client == nil {
		t.Fatal("runtime client is nil")
	}
	if client.TypedClient != wantTypedClient {
		t.Fatal("typed client was not propagated into runtime client")
	}
	if client.DynamicClient != wantDynamicClient {
		t.Fatal("dynamic client was not propagated into runtime client")
	}

	clientType := reflect.TypeFor[RuntimeClient]()
	if _, ok := clientType.FieldByName("Namespace"); ok {
		t.Fatal("runtime client should expose namespace through K8sConfig only")
	}
	if _, ok := clientType.FieldByName("ManagedBy"); ok {
		t.Fatal("runtime client should expose managedBy through K8sConfig only")
	}
	if client.K8sConfig == nil {
		t.Fatal("k8s config was not propagated into runtime client")
	}
	if !reflect.DeepEqual(client.K8sConfig, wantK8sConfig) {
		t.Fatalf("k8s config = %#v, want %#v", client.K8sConfig, wantK8sConfig)
	}
}
