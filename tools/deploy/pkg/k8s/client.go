package k8s

import (
	"fmt"
	"strings"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	// runtimeRESTConfigLoader 加载 kubeconfig 并返回 REST 配置。
	runtimeRESTConfigLoader = loadRuntimeRESTConfig
	// runtimeTypedClientConstructor 根据 REST 配置创建 typed Kubernetes 客户端。
	runtimeTypedClientConstructor = func(config *rest.Config) (kubernetes.Interface, error) {
		return kubernetes.NewForConfig(config)
	}
	// runtimeDynamicClientConstructor 根据 REST 配置创建 dynamic Kubernetes 客户端。
	runtimeDynamicClientConstructor = func(config *rest.Config) (dynamic.Interface, error) {
		return dynamic.NewForConfig(config)
	}
)

// RuntimeClient 聚合部署流程所需的 Kubernetes 运行时客户端与静态配置。
type RuntimeClient struct {
	TypedClient   kubernetes.Interface
	DynamicClient dynamic.Interface
	K8sConfig     *K8sConfig
}

// NewRuntimeClient 按标准 kubeconfig 加载规则初始化运行时客户端。
func NewRuntimeClient(kubeconfigPath string) (*RuntimeClient, error) {
	restConfig, err := runtimeRESTConfigLoader(kubeconfigPath)
	if err != nil {
		trimmedPath := strings.TrimSpace(kubeconfigPath)
		if trimmedPath != "" {
			return nil, fmt.Errorf("加载 kubeconfig %q 失败: %w", trimmedPath, err)
		}

		return nil, fmt.Errorf("按默认规则加载 kubeconfig 失败: %w", err)
	}

	return NewRuntimeClientWithConfig(restConfig)
}

// NewRuntimeClientWithConfig 基于给定的 REST 配置初始化运行时客户端。
func NewRuntimeClientWithConfig(restConfig *rest.Config) (*RuntimeClient, error) {
	if restConfig == nil {
		return nil, fmt.Errorf("rest config 为空")
	}

	typedClient, err := runtimeTypedClientConstructor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("初始化 typed client 失败: %w", err)
	}

	dynamicClient, err := runtimeDynamicClientConstructor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("初始化 dynamic client 失败: %w", err)
	}

	k8sConfig := LoadK8sConfig()

	return &RuntimeClient{
		TypedClient:   typedClient,
		DynamicClient: dynamicClient,
		K8sConfig:     k8sConfig,
	}, nil
}

func loadRuntimeRESTConfig(kubeconfigPath string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if trimmedPath := strings.TrimSpace(kubeconfigPath); trimmedPath != "" {
		loadingRules.ExplicitPath = trimmedPath
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)

	return clientConfig.ClientConfig()
}
