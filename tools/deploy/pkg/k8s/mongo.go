package k8s

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"

	"dominion/tools/deploy/pkg/config"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
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

// MongoDBWorkload 描述 MongoDB workload 生成所需字段。
type MongoDBWorkload struct {
	ServiceName     string
	EnvironmentName string
	App             string
	DominionApp     string
	ProfileName     string
	Persistence     config.DeployInfraPersistence
}

// ResourceName 返回 MongoDB workload 对应的资源名。
// 若 w 为空，则返回空字符串。
func (w *MongoDBWorkload) ResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindMongoDB, w.App, w.DominionApp, w.ServiceName, w.EnvironmentName)
}

// ServiceResourceName 返回 MongoDB Service 对应的资源名。
// 若 w 为空，则返回空字符串。
func (w *MongoDBWorkload) ServiceResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindService, w.App, w.DominionApp, w.ServiceName, w.EnvironmentName)
}

// SecretResourceName 返回 MongoDB Secret 对应的资源名。
// 若 w 为空，则返回空字符串。
func (w *MongoDBWorkload) SecretResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindSecret, w.App, w.DominionApp, w.ServiceName, w.EnvironmentName)
}

// PVCResourceName 返回 MongoDB PVC 对应的资源名。
// 若 w 为空，则返回空字符串。
func (w *MongoDBWorkload) PVCResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindPVC, w.App, w.DominionApp, w.ServiceName, w.EnvironmentName)
}

// Validate 校验 MongoDB workload 字段是否合法。
func (w *MongoDBWorkload) Validate() error {
	if w == nil {
		return fmt.Errorf("mongo workload 为空")
	}
	if strings.TrimSpace(w.ServiceName) == "" {
		return fmt.Errorf("mongo workload 缺少 service name")
	}
	if strings.TrimSpace(w.EnvironmentName) == "" {
		return fmt.Errorf("mongo workload 缺少 environment name")
	}
	if strings.TrimSpace(w.App) == "" {
		return fmt.Errorf("mongo workload 缺少 app")
	}
	if len(w.ResourceName()) > maxK8sResourceNameSize {
		return fmt.Errorf("mongo workload name 超过 63 字符")
	}
	if strings.TrimSpace(w.ProfileName) == "" {
		return fmt.Errorf("mongo workload 缺少 profile name")
	}

	return nil
}

func generateStablePassword(inputs ...string) string {
	normalized := make([]string, 0, len(inputs))
	for _, input := range inputs {
		normalized = append(normalized, strings.TrimSpace(input))
	}

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

func newMongoDBWorkload(infra config.DeployInfra, envName string, dominionApp string) (*MongoDBWorkload, error) {
	w := &MongoDBWorkload{
		ServiceName:     strings.TrimSpace(infra.Name),
		EnvironmentName: strings.TrimSpace(envName),
		App:             strings.TrimSpace(dominionApp),
		DominionApp:     strings.TrimSpace(dominionApp),
		ProfileName:     strings.TrimSpace(infra.Profile),
		Persistence:     infra.Persistence,
	}

	if err := w.Validate(); err != nil {
		return nil, err
	}

	return w, nil
}

// BuildMongoDBDeployment 将 MongoDB workload 构造成可直接下发的 Deployment 对象。
func BuildMongoDBDeployment(workload *MongoDBWorkload) (*appsv1.Deployment, error) {
	if workload == nil {
		return nil, fmt.Errorf("mongo workload 为空")
	}
	if err := workload.Validate(); err != nil {
		return nil, err
	}

	k8sConfig := LoadK8sConfig()
	profile := k8sConfig.MongoProfile(workload.ProfileName)
	if profile == nil {
		return nil, fmt.Errorf("mongo profile %s 不存在", strings.TrimSpace(workload.ProfileName))
	}

	deploymentName := workload.ResourceName()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(k8sConfig.ManagedBy),
	)
	selectorLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
	)
	containerImage := strings.TrimSpace(profile.Image) + mongoImageTagJoiner + strings.TrimSpace(profile.Version)
	containerEnv := buildMongoDBContainerEnv(workload, workload.SecretResourceName())
	volumeMounts := []corev1.VolumeMount{{
		Name:      mongoDataVolumeName,
		MountPath: mongoDataMountPath,
	}}
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
			Namespace: k8sConfig.Namespace,
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

// BuildMongoDBPVC 将 MongoDB PVC workload 构造成可直接下发的 PersistentVolumeClaim 对象。
func BuildMongoDBPVC(workload *MongoDBWorkload) (*corev1.PersistentVolumeClaim, error) {
	if workload == nil {
		return nil, fmt.Errorf("mongo workload 为空")
	}
	if err := workload.Validate(); err != nil {
		return nil, err
	}

	k8sConfig := LoadK8sConfig()
	profile := k8sConfig.MongoProfile(workload.ProfileName)
	if profile == nil {
		return nil, fmt.Errorf("mongo profile %s 不存在", strings.TrimSpace(workload.ProfileName))
	}

	pvcName := workload.PVCResourceName()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(k8sConfig.ManagedBy),
	)
	storageClassName := strings.TrimSpace(profile.Storage.StorageClassName)
	volumeMode := corev1.PersistentVolumeMode(strings.TrimSpace(profile.Storage.VolumeMode))
	storageQuantity := resource.MustParse(strings.TrimSpace(profile.Storage.Capacity))

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: k8sConfig.Namespace,
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

// CheckPVCCompatibility 校验现有 PVC 是否与期望的 MongoDB PVC workload 兼容。
func CheckPVCCompatibility(existing *corev1.PersistentVolumeClaim, desired *MongoDBWorkload) error {
	if existing == nil {
		return fmt.Errorf("existing pvc 为空")
	}
	if desired == nil {
		return fmt.Errorf("mongo workload 为空")
	}
	if err := desired.Validate(); err != nil {
		return err
	}

	k8sConfig := LoadK8sConfig()
	profile := k8sConfig.MongoProfile(desired.ProfileName)
	if profile == nil {
		return fmt.Errorf("mongo profile %s 不存在", strings.TrimSpace(desired.ProfileName))
	}

	if strings.TrimSpace(existing.Labels[dominionAppLabelKey]) != strings.TrimSpace(desired.DominionApp) {
		return fmt.Errorf("pvc %s 标签 %s 不兼容: existing=%q desired=%q", existing.Name, dominionAppLabelKey, existing.Labels[dominionAppLabelKey], desired.DominionApp)
	}
	if strings.TrimSpace(existing.Labels[dominionEnvironmentLabelKey]) != strings.TrimSpace(desired.EnvironmentName) {
		return fmt.Errorf("pvc %s 标签 %s 不兼容: existing=%q desired=%q", existing.Name, dominionEnvironmentLabelKey, existing.Labels[dominionEnvironmentLabelKey], desired.EnvironmentName)
	}

	storageClassName := strings.TrimSpace(profile.Storage.StorageClassName)
	if existing.Spec.StorageClassName == nil || strings.TrimSpace(*existing.Spec.StorageClassName) != storageClassName {
		return fmt.Errorf("pvc %s storageClassName 不兼容: existing=%q desired=%q", existing.Name, mongoStringPtrValue(existing.Spec.StorageClassName), storageClassName)
	}

	desiredAccessModes := buildMongoDBPVCAccessModes(profile.Storage.AccessModes)
	if !slices.Equal(existing.Spec.AccessModes, desiredAccessModes) {
		return fmt.Errorf("pvc %s accessModes 不兼容: existing=%v desired=%v", existing.Name, existing.Spec.AccessModes, desiredAccessModes)
	}

	desiredVolumeMode := corev1.PersistentVolumeMode(strings.TrimSpace(profile.Storage.VolumeMode))
	if existing.Spec.VolumeMode == nil || *existing.Spec.VolumeMode != desiredVolumeMode {
		return fmt.Errorf("pvc %s volumeMode 不兼容: existing=%q desired=%q", existing.Name, mongoPVCVolumeModeValue(existing.Spec.VolumeMode), desiredVolumeMode)
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

// BuildMongoDBService 将 MongoDB workload 构造成可直接下发的 Service 对象。
func BuildMongoDBService(workload *MongoDBWorkload) (*corev1.Service, error) {
	if workload == nil {
		return nil, fmt.Errorf("mongo workload 为空")
	}

	k8sConfig := LoadK8sConfig()
	profile := k8sConfig.MongoProfile(workload.ProfileName)
	if profile == nil {
		return nil, fmt.Errorf("mongo profile %s 不存在", strings.TrimSpace(workload.ProfileName))
	}

	serviceName := workload.ServiceResourceName()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(k8sConfig.ManagedBy),
	)
	selectorLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
	)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: k8sConfig.Namespace,
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

// BuildMongoDBSecret 将 MongoDB Secret workload 构造成可直接下发的 Secret 对象。
func BuildMongoDBSecret(workload *MongoDBWorkload) (*corev1.Secret, error) {
	if workload == nil {
		return nil, fmt.Errorf("mongo workload 为空")
	}

	k8sConfig := LoadK8sConfig()
	profile := k8sConfig.MongoProfile(workload.ProfileName)
	if profile == nil {
		return nil, fmt.Errorf("mongo profile %s 不存在", strings.TrimSpace(workload.ProfileName))
	}

	secretName := workload.SecretResourceName()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(k8sConfig.ManagedBy),
	)
	password := generateStablePassword(workload.DominionApp, workload.EnvironmentName, workload.ServiceName)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: k8sConfig.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			mongoSecretUsernameKey: []byte(base64.StdEncoding.EncodeToString([]byte(profile.AdminUsername))),
			mongoSecretPasswordKey: []byte(base64.StdEncoding.EncodeToString([]byte(password))),
		},
	}, nil
}

func buildMongoDBContainerEnv(workload *MongoDBWorkload, secretName string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: reservedEnvNameDominionApp, Value: workload.App},
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

func mongoStringPtrValue(value *string) string {
	if value == nil {
		return ""
	}

	return strings.TrimSpace(*value)
}

func mongoPVCVolumeModeValue(value *corev1.PersistentVolumeMode) corev1.PersistentVolumeMode {
	if value == nil {
		return ""
	}

	return corev1.PersistentVolumeMode(strings.TrimSpace(string(*value)))
}
