package k8s

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	// reservedEnvNameServiceApp 为 service app 注入环境变量名。
	reservedEnvNameServiceApp = "SERVICE_APP"
	// reservedEnvNameDominionEnvironment 为 Dominion 环境注入环境变量名。
	reservedEnvNameDominionEnvironment = "DOMINION_ENVIRONMENT"
	// reservedEnvNamePodNamespace 为 Pod 命名空间注入环境变量名。
	reservedEnvNamePodNamespace = "POD_NAMESPACE"

	// tlsVolumeName 为 TLS projected volume 固定名称。
	tlsVolumeName = "tls"
	// tlsMountPath 为 TLS 文件固定挂载目录。
	tlsMountPath = "/etc/tls"
	// tlsCAFileName 为容器内固定的 CA 文件名。
	tlsCAFileName = "ca.crt"
	// tlsCertFileName 为容器内固定的证书文件名。
	tlsCertFileName = "tls.crt"
	// tlsKeyFileName 为容器内固定的私钥文件名。
	tlsKeyFileName = "tls.key"
	// envTLSCertFile 为 TLS 证书文件环境变量名。
	envTLSCertFile = "TLS_CERT_FILE"
	// envTLSKeyFile 为 TLS 私钥文件环境变量名。
	envTLSKeyFile = "TLS_KEY_FILE"
	// envTLSCAFile 为 TLS CA 文件环境变量名。
	envTLSCAFile = "TLS_CA_FILE"
	// envTLSDomain 为 TLS 服务名环境变量名。
	envTLSDomain = "TLS_SERVER_NAME"

	// httpRouteKind 是 Gateway API HTTPRoute 资源类型。
	httpRouteKind = "HTTPRoute"

	// MongoDB 相关常量。
	mongoPasswordHMACKey   = "dominion-mongo-stable-password"
	mongoPasswordAlphabet  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	mongoPasswordMinLen    = 24
	mongoPasswordJoiner    = "\x00"
	mongoPortName          = "mongo"
	mongoSecretUsernameKey = "username"
	mongoSecretPasswordKey = "password"
	mongoDataVolumeName    = "mongo-data"
	mongoDataMountPath     = "/data/db"
	mongoInitContainerName = "mongo-init"
	mongoInitMarkerFile    = "/data/db/.admin-initialized"
	mongoInitLogPath       = "/tmp/mongod-init.log"
	mongoInitPIDPath       = "/tmp/mongod-init.pid"
	mongoAdminDatabaseName = "admin"
	mongoRootRoleName      = "root"
	mongoPodFieldPathNS    = "metadata.namespace"
	mongoLocalHost         = "127.0.0.1"
	mongoImageTagJoiner    = ":"
	mongoEnvRootUsername   = "MONGO_INITDB_ROOT_USERNAME"
	mongoEnvRootPassword   = "MONGO_INITDB_ROOT_PASSWORD"
	mongoInitScript        = `set -euo pipefail

shutdown_mongod() {
	if [ -f "` + mongoInitPIDPath + `" ]; then
		mongod --dbpath "` + mongoDataMountPath + `" --shutdown >/dev/null 2>&1 || true
	fi
}

trap shutdown_mongod EXIT

mongod --bind_ip "` + mongoLocalHost + `" --port 27017 --dbpath "` + mongoDataMountPath + `" --fork --logpath "` + mongoInitLogPath + `" --pidfilepath "` + mongoInitPIDPath + `" --auth

until mongosh --host "` + mongoLocalHost + `" --port 27017 --quiet --eval 'db.adminCommand({ ping: 1 })' >/dev/null 2>&1; do
	sleep 1
done

if [ ! -f "` + mongoInitMarkerFile + `" ]; then
	mongosh --host "` + mongoLocalHost + `" --port 27017 --quiet --eval 'db.getSiblingDB("` + mongoAdminDatabaseName + `").createUser({user: process.env.MONGO_INITDB_ROOT_USERNAME, pwd: process.env.MONGO_INITDB_ROOT_PASSWORD, roles: [{role: "` + mongoRootRoleName + `", db: "` + mongoAdminDatabaseName + `"}]})'
	touch "` + mongoInitMarkerFile + `"
	exit 0
fi

mongosh --host "` + mongoLocalHost + `" --port 27017 --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase "` + mongoAdminDatabaseName + `" --quiet --eval 'db.adminCommand({ ping: 1 })' >/dev/null`
)

// BuildDeployment 将 deployment workload 构造成可直接下发的 Deployment 对象。
func BuildDeployment(workload *DeploymentWorkload, cfg *K8sConfig) (*appsv1.Deployment, error) {
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(cfg.ManagedBy),
	)
	selectorLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
	)
	ports, err := buildContainerPorts(workload.Ports)
	if err != nil {
		return nil, fmt.Errorf("构建 deployment ports 失败: %w", err)
	}

	replicas := workload.Replicas
	containerEnv := []corev1.EnvVar{
		{Name: reservedEnvNameServiceApp, Value: workload.App},
		{Name: reservedEnvNameDominionEnvironment, Value: workload.EnvironmentName},
		{Name: reservedEnvNamePodNamespace, Value: cfg.Namespace},
	}
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	if workload.TLSEnabled {
		volumes = []corev1.Volume{{
			Name: tlsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{Secret: &corev1.SecretProjection{LocalObjectReference: corev1.LocalObjectReference{Name: cfg.TLS.Secret}}},
						{ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: cfg.TLS.CAConfigMap.Name},
							Items: []corev1.KeyToPath{{
								Key:  cfg.TLS.CAConfigMap.Key,
								Path: tlsCAFileName,
							}},
						}},
					},
				},
			},
		}}
		volumeMounts = []corev1.VolumeMount{{
			Name:      tlsVolumeName,
			MountPath: tlsMountPath,
			ReadOnly:  true,
		}}
		containerEnv = append(containerEnv,
			corev1.EnvVar{Name: envTLSCertFile, Value: filepath.Join(tlsMountPath, tlsCertFileName)},
			corev1.EnvVar{Name: envTLSKeyFile, Value: filepath.Join(tlsMountPath, tlsKeyFileName)},
			corev1.EnvVar{Name: envTLSCAFile, Value: filepath.Join(tlsMountPath, tlsCAFileName)},
			corev1.EnvVar{Name: envTLSDomain, Value: cfg.TLS.Domain},
		)
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload.WorkloadName(),
			Namespace: cfg.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string(selectorLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string(objectLabels),
				},
				Spec: corev1.PodSpec{
					Volumes: volumes,
					Containers: []corev1.Container{{
						Name:         workload.WorkloadName(),
						Image:        workload.Image,
						Ports:        ports,
						VolumeMounts: volumeMounts,
						Env:          containerEnv,
					}},
				},
			},
		},
	}, nil
}

// BuildService 将 deployment workload 构造成可直接下发的 Service 对象。
func BuildService(workload *DeploymentWorkload, cfg *K8sConfig) (*corev1.Service, error) {
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(cfg.ManagedBy),
	)
	selectorLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
	)
	ports, err := buildServicePorts(workload.Ports)
	if err != nil {
		return nil, fmt.Errorf("构建 service ports 失败: %w", err)
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload.ServiceResourceName(),
			Namespace: cfg.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string(selectorLabels),
			Ports:    ports,
		},
	}, nil
}

// BuildHTTPRoute 将 HTTPRoute workload 构造成可直接下发的动态对象。
func BuildHTTPRoute(workload *HTTPRouteWorkload, cfg *K8sConfig) (*unstructured.Unstructured, error) {
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(cfg.ManagedBy),
	)
	var hostnames []gatewayv1.Hostname
	for _, hostname := range workload.Hostnames {
		hostnames = append(hostnames, gatewayv1.Hostname(hostname))
	}

	gatewayNamespace := gatewayv1.Namespace(workload.GatewayNamespace)
	var rules []gatewayv1.HTTPRouteRule
	for _, match := range workload.Matches {
		pathType := gatewayv1.PathMatchType(match.Type)
		pathValue := match.Value
		backendName := gatewayv1.ObjectName(workload.BackendService)
		backendPort := gatewayv1.PortNumber(match.BackendPort)

		rules = append(rules, gatewayv1.HTTPRouteRule{
			Matches: []gatewayv1.HTTPRouteMatch{{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  &pathType,
					Value: &pathValue,
				},
			}},
			BackendRefs: []gatewayv1.HTTPBackendRef{{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: backendName,
						Port: &backendPort,
					},
				},
			}},
		})
	}

	typedRoute := &gatewayv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayv1.GroupVersion.String(),
			Kind:       httpRouteKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload.ResourceName(),
			Namespace: cfg.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: hostnames,
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Name:      gatewayv1.ObjectName(workload.GatewayName),
					Namespace: &gatewayNamespace,
				}},
			},
			Rules: rules,
		},
	}

	rawBytes, err := json.Marshal(typedRoute)
	if err != nil {
		return nil, fmt.Errorf("序列化 http route 失败: %w", err)
	}

	rawMap := make(map[string]any)
	if err := json.Unmarshal(rawBytes, &rawMap); err != nil {
		return nil, fmt.Errorf("反序列化 http route 失败: %w", err)
	}

	route := &unstructured.Unstructured{Object: rawMap}

	return route, nil
}

// BuildMongoDBDeployment 将 MongoDB workload 构造成可直接下发的 Deployment 对象。
func BuildMongoDBDeployment(workload *MongoDBWorkload, cfg *K8sConfig) (*appsv1.Deployment, error) {
	if workload == nil {
		return nil, fmt.Errorf("mongo workload 为空")
	}
	if err := workload.Validate(); err != nil {
		return nil, err
	}

	profile := cfg.MongoProfile(workload.ProfileName)
	if profile == nil {
		return nil, fmt.Errorf("mongo profile %s 不存在", strings.TrimSpace(workload.ProfileName))
	}

	deploymentName := workload.ResourceName()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(cfg.ManagedBy),
	)
	selectorLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
	)
	containerImage := strings.TrimSpace(profile.Image) + mongoImageTagJoiner + strings.TrimSpace(profile.Version)
	containerEnv := buildMongoDBContainerEnv(workload, workload.SecretResourceName(), cfg)
	runAsUser := profile.Security.RunAsUser
	runAsGroup := profile.Security.RunAsGroup
	volumeMounts := []corev1.VolumeMount{{
		Name:      mongoDataVolumeName,
		MountPath: mongoDataMountPath,
	}}
	podSecurityContext := &corev1.PodSecurityContext{
		RunAsUser:  &runAsUser,
		RunAsGroup: &runAsGroup,
	}
	volumes := []corev1.Volume{{
		Name: mongoDataVolumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: workload.PVCResourceName(),
			},
		},
	}}
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString(mongoPortName)},
		},
	}
	replicas := int32(1)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: cfg.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string(selectorLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string(objectLabels),
				},
				Spec: corev1.PodSpec{
					SecurityContext: podSecurityContext,
					Volumes:         volumes,
					InitContainers: []corev1.Container{{
						Name:         mongoInitContainerName,
						Image:        containerImage,
						Command:      []string{"bash", "-ec", mongoInitScript},
						Env:          containerEnv,
						VolumeMounts: volumeMounts,
					}},
					Containers: []corev1.Container{{
						Name:         deploymentName,
						Image:        containerImage,
						Ports:        []corev1.ContainerPort{{Name: mongoPortName, ContainerPort: int32(profile.Port)}},
						Env:          containerEnv,
						VolumeMounts: volumeMounts,
						LivenessProbe: &corev1.Probe{
							ProbeHandler: probe.ProbeHandler,
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: probe.ProbeHandler,
						},
					}},
				},
			},
		},
	}, nil
}

// BuildMongoDBService 将 MongoDB workload 构造成可直接下发的 Service 对象。
func BuildMongoDBService(workload *MongoDBWorkload, cfg *K8sConfig) (*corev1.Service, error) {
	if workload == nil {
		return nil, fmt.Errorf("mongo workload 为空")
	}

	profile := cfg.MongoProfile(workload.ProfileName)
	if profile == nil {
		return nil, fmt.Errorf("mongo profile %s 不存在", strings.TrimSpace(workload.ProfileName))
	}

	serviceName := workload.ServiceResourceName()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(cfg.ManagedBy),
	)
	selectorLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
	)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: cfg.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string(selectorLabels),
			Ports: []corev1.ServicePort{
				{
					Name:       mongoPortName,
					Port:       int32(profile.Port),
					TargetPort: intstr.FromString(mongoPortName),
				},
			},
		},
	}, nil
}

// BuildMongoDBPVC 将 MongoDB PVC workload 构造成可直接下发的 PersistentVolumeClaim 对象。
func BuildMongoDBPVC(workload *MongoDBWorkload, cfg *K8sConfig) (*corev1.PersistentVolumeClaim, error) {
	if workload == nil {
		return nil, fmt.Errorf("mongo workload 为空")
	}
	if err := workload.Validate(); err != nil {
		return nil, err
	}

	profile := cfg.MongoProfile(workload.ProfileName)
	if profile == nil {
		return nil, fmt.Errorf("mongo profile %s 不存在", strings.TrimSpace(workload.ProfileName))
	}

	pvcName := workload.PVCResourceName()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(cfg.ManagedBy),
	)
	storageClassName := strings.TrimSpace(profile.Storage.StorageClassName)
	volumeMode := corev1.PersistentVolumeMode(strings.TrimSpace(profile.Storage.VolumeMode))
	storageQuantity := resource.MustParse(strings.TrimSpace(profile.Storage.Capacity))

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cfg.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
			AccessModes:      buildMongoDBPVCAccessModes(profile.Storage.AccessModes),
			VolumeMode:       &volumeMode,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageQuantity,
				},
			},
		},
	}, nil
}

// BuildMongoDBSecret 将 MongoDB Secret workload 构造成可直接下发的 Secret 对象。
func BuildMongoDBSecret(workload *MongoDBWorkload, cfg *K8sConfig) (*corev1.Secret, error) {
	if workload == nil {
		return nil, fmt.Errorf("mongo workload 为空")
	}

	profile := cfg.MongoProfile(workload.ProfileName)
	if profile == nil {
		return nil, fmt.Errorf("mongo profile %s 不存在", strings.TrimSpace(workload.ProfileName))
	}

	secretName := workload.SecretResourceName()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(cfg.ManagedBy),
	)
	password := generateStablePassword(workload.App, workload.EnvironmentName, workload.ServiceName)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: cfg.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			mongoSecretUsernameKey: []byte(profile.AdminUsername),
			mongoSecretPasswordKey: []byte(password),
		},
	}, nil
}

// CheckPVCCompatibility 校验现有 PVC 是否与期望的 MongoDB PVC workload 兼容。
func CheckPVCCompatibility(existing *corev1.PersistentVolumeClaim, desired *MongoDBWorkload, cfg *K8sConfig) error {
	if existing == nil {
		return fmt.Errorf("existing pvc 为空")
	}
	if desired == nil {
		return fmt.Errorf("mongo workload 为空")
	}
	if err := desired.Validate(); err != nil {
		return err
	}

	profile := cfg.MongoProfile(desired.ProfileName)
	if profile == nil {
		return fmt.Errorf("mongo profile %s 不存在", strings.TrimSpace(desired.ProfileName))
	}

	if strings.TrimSpace(existing.Labels[dominionEnvironmentLabelKey]) != strings.TrimSpace(desired.EnvironmentName) {
		return fmt.Errorf("pvc %s 标签 %s 不兼容: existing=%q desired=%q", existing.Name, dominionEnvironmentLabelKey, existing.Labels[dominionEnvironmentLabelKey], desired.EnvironmentName)
	}

	storageClassName := strings.TrimSpace(profile.Storage.StorageClassName)
	if existing.Spec.StorageClassName == nil || strings.TrimSpace(*existing.Spec.StorageClassName) != storageClassName {
		return fmt.Errorf("pvc %s storageClassName 不兼容: existing=%q desired=%q", existing.Name, stringPtrValue(existing.Spec.StorageClassName), storageClassName)
	}

	desiredAccessModes := buildMongoDBPVCAccessModes(profile.Storage.AccessModes)
	if !slices.Equal(existing.Spec.AccessModes, desiredAccessModes) {
		return fmt.Errorf("pvc %s accessModes 不兼容: existing=%v desired=%v", existing.Name, existing.Spec.AccessModes, desiredAccessModes)
	}

	desiredVolumeMode := corev1.PersistentVolumeMode(strings.TrimSpace(profile.Storage.VolumeMode))
	if existing.Spec.VolumeMode == nil || *existing.Spec.VolumeMode != desiredVolumeMode {
		return fmt.Errorf("pvc %s volumeMode 不兼容: existing=%q desired=%q", existing.Name, pvcVolumeModeValue(existing.Spec.VolumeMode), desiredVolumeMode)
	}

	desiredCapacity := resource.MustParse(strings.TrimSpace(profile.Storage.Capacity))
	existingCapacity, ok := existing.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok {
		return fmt.Errorf("pvc %s storage capacity 不兼容: existing=missing desired=%s", existing.Name, desiredCapacity.String())
	}
	if existingCapacity.Cmp(desiredCapacity) > 0 {
		return fmt.Errorf("pvc %s storage capacity 不兼容: existing=%s desired=%s", existing.Name, existingCapacity.String(), desiredCapacity.String())
	}

	return nil
}

// generateStablePassword 基于输入生成确定性密码。
func generateStablePassword(inputs ...string) string {
	normalized := make([]string, 0, len(inputs))
	for _, input := range inputs {
		normalized = append(normalized, strings.TrimSpace(input))
	}

	//nolint:gosec // HMAC 用于确定性密码生成而非安全校验。
	mac := hmac.New(sha256.New, []byte(mongoPasswordHMACKey))
	_, _ = mac.Write([]byte(strings.Join(normalized, mongoPasswordJoiner)))
	sum := mac.Sum(nil)

	encoded := make([]byte, 0, len(sum))
	for _, b := range sum {
		encoded = append(encoded, mongoPasswordAlphabet[int(b)%len(mongoPasswordAlphabet)])
	}
	if len(encoded) >= mongoPasswordMinLen {
		return string(encoded)
	}

	for len(encoded) < mongoPasswordMinLen {
		for _, b := range sum {
			encoded = append(encoded, mongoPasswordAlphabet[int(b)%len(mongoPasswordAlphabet)])
			if len(encoded) >= mongoPasswordMinLen {
				break
			}
		}
	}

	return string(encoded)
}

func buildContainerPorts(ports []*DeploymentPort) ([]corev1.ContainerPort, error) {
	if len(ports) == 0 {
		return nil, nil
	}

	containerPorts := make([]corev1.ContainerPort, 0, len(ports))
	for _, port := range ports {
		if port == nil {
			return nil, fmt.Errorf("端口为空")
		}

		containerPorts = append(containerPorts, corev1.ContainerPort{
			Name:          port.Name,
			ContainerPort: int32(port.Port),
		})
	}

	return containerPorts, nil
}

func buildServicePorts(ports []*DeploymentPort) ([]corev1.ServicePort, error) {
	if len(ports) == 0 {
		return nil, nil
	}

	servicePorts := make([]corev1.ServicePort, 0, len(ports))
	for _, port := range ports {
		if port == nil {
			return nil, fmt.Errorf("端口为空")
		}

		servicePorts = append(servicePorts, corev1.ServicePort{
			Name:       port.Name,
			Port:       int32(port.Port),
			TargetPort: intstr.FromString(port.Name),
		})
	}

	return servicePorts, nil
}

func buildMongoDBContainerEnv(workload *MongoDBWorkload, secretName string, cfg *K8sConfig) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: reservedEnvNameServiceApp, Value: workload.App},
		{Name: reservedEnvNameDominionEnvironment, Value: workload.EnvironmentName},
		{
			Name: reservedEnvNamePodNamespace,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: mongoPodFieldPathNS},
			},
		},
		{
			Name: mongoEnvRootUsername,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  mongoSecretUsernameKey,
				},
			},
		},
		{
			Name: mongoEnvRootPassword,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  mongoSecretPasswordKey,
				},
			},
		},
	}
}

func buildMongoDBPVCAccessModes(accessModes []string) []corev1.PersistentVolumeAccessMode {
	if len(accessModes) == 0 {
		return nil
	}

	result := make([]corev1.PersistentVolumeAccessMode, 0, len(accessModes))
	for _, accessMode := range accessModes {
		result = append(result, corev1.PersistentVolumeAccessMode(strings.TrimSpace(accessMode)))
	}

	return result
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}

	return strings.TrimSpace(*value)
}

func pvcVolumeModeValue(value *corev1.PersistentVolumeMode) corev1.PersistentVolumeMode {
	if value == nil {
		return ""
	}

	return corev1.PersistentVolumeMode(strings.TrimSpace(string(*value)))
}
