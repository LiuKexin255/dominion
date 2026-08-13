# Contract: Config Runtime Contract (K8s)

**Feature**: 045-deploy-config | **Spec**: [spec.md](../spec.md)

本契约定义 config 数据从期望状态到容器运行时的物化约定，以及 SDK 据此发现与读取配置的运行时契约。Go/JS SDK 均依赖本契约。

---

## 1. 运行时约定

| 项 | 值 | 实施位置 |
|----|----|----------|
| 配置根目录 | `/mnt/dominion/config` | `builder.go` 常量 `configMountPath` |
| 发现变量 | `DOMINION_CONFIG_DIR` = `/mnt/dominion/config` | `builder.go` 常量 `envConfigDir`；平台强制注入 |
| 文件布局 | `{DOMINION_CONFIG_DIR}/{block}/{key}` | ConfigMap `KeyToPath.Path` |
| 卷名 | `dominion-config`（ProjectedVolume） | `builder.go` 常量 `configVolumeName` |
| 挂载模式 | 只读 | `VolumeMount.ReadOnly = true` |
| ConfigMap 名 | `{workload}-config` | `executor.go` 创建 |
| ConfigMap data key | `{block}-{key}`（扁平，ConfigMap key 不允许含 `/`） | `executor.go` |
| 触发条件 | 仅当 artifact 有 ≥1 个 config entry 时创建 ConfigMap/卷/挂载/env | `builder.go` |

> **对称性**：与 secret 约定（`DOMINION_SECRET_DIR` / `/mnt/dominion/secret` / 卷 `dominion-secrets`）完全对称，见 `specs/002-deploy-secret-config`。

---

## 2. ConfigMap 创建（控制面新增 apply 路径）

控制面当前**不创建**任何 ConfigMap（仅引用预存 TLS CA ConfigMap）。config 特性首次引入控制面创建数据型资源。

### ConfigMap 内容

由 `runtime/k8s/builder.go` 新增 `BuildConfigMap(workload, cfg)` 生成：

```go
// ConfigMap data: 每个 ConfigEntry 一个 key
data := map[string]string{}
for _, ce := range workload.ConfigEntries {
    data[ce.Block + "-" + ce.Key] = ce.Value
}
// ObjectMeta: Name = workload.WorkloadName() + "-config", Namespace = cfg.Namespace,
//             Labels 与 Deployment 一致（app/service/environment/managed-by）
```

### 投影为容器文件

`BuildDeployment` / `BuildStatefulSet` 中，当 `len(workload.ConfigEntries) > 0`：

```go
var configItems []corev1.KeyToPath
for _, ce := range workload.ConfigEntries {
    configItems = append(configItems, corev1.KeyToPath{
        Key:  ce.Block + "-" + ce.Key,        // ConfigMap data key
        Path: ce.Block + "/" + ce.Key,        // 容器内路径 {block}/{key}
    })
}
volumes = append(volumes, corev1.Volume{
    Name: configVolumeName,
    VolumeSource: corev1.VolumeSource{
        Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{
            ConfigMap: &corev1.ConfigMapProjection{
                LocalObjectReference: corev1.LocalObjectReference{Name: workload.WorkloadName() + "-config"},
                Items:                configItems,
            },
        }}},
    },
})
volumeMounts = append(volumeMounts, corev1.VolumeMount{
    Name: configVolumeName, MountPath: configMountPath, ReadOnly: true,
})
containerEnv = append(containerEnv, corev1.EnvVar{Name: envConfigDir, Value: configMountPath})
```

### executor apply/prune 集成

`executor.go` 的 `applyInner`（lines 83-151）新增 ConfigMap apply（Get→Create-if-NotFound→Update-with-ResourceVersion，模式同 `applyTypedSecret` lines 999-1018）；prune 列表（lines 153-178）新增 ConfigMap；`expectedApplyResources`（lines 180-230）纳入 ConfigMap。

> **顺序**：ConfigMap 必须在引用它的 Deployment/StatefulSet 之前 apply（Deployment 引用 ConfigMap 投影，ConfigMap 不存在时 Pod 启动失败）。

---

## 3. SDK 运行时读取契约

SDK 通过环境变量 `DOMINION_CONFIG_DIR` 发现配置根目录，按 `{block}/{key}` 定位文件，**始终用 YAML 解析器**解析（兼容 json 与 yaml 内容，见 research.md R4）。

### 路径解析

```
file = {DOMINION_CONFIG_DIR}/{block}/{key}
```

- `block`、`key` 即 SDK 调用参数。
- 文件内容为 service.yaml 中声明的 `value` 原始文本。

### 解析

- 一律 YAML 解析（Go `gopkg.in/yaml.v3`；JS `js-yaml`）。
- 合法 JSON 是合法 YAML，故 json-type 与 yaml-type 文件均正确解析。

### 深度合并

按 [data-model.md "Deep Merge Semantics"](../data-model.md) 与 [research.md R2/R3](../research.md) 实施深度合并，结果反序列化为调用方指定类型。

### 错误语义

| 情况 | 行为 |
|------|------|
| `DOMINION_CONFIG_DIR` 未设置 | 错误（Go 返回 err；JS 抛异常）—— 表明非 dominion 部署环境 |
| 文件不存在（配置块未被 deploy 选择） | 错误（Go err；JS throw）—— 表明运行环境与代码预期不一致（spec User Story 3 场景 3） |
| 文件内容不可解析 | 错误（解析失败信息） |
| 解析成功 | 深度合并 over defaults，返回类型化结果 |

---

## 4. 与环境变量参数的关系（FR-016）

config 机制与 deploy.yaml `env` 注入的环境变量参数**完全独立**：
- config 通过文件挂载 + `DOMINION_CONFIG_DIR` 发现；env 通过容器环境变量注入。
- 两者可同时存在、互不读取、互不覆盖。
- `DOMINION_CONFIG_DIR` 本身是保留变量（用户 env 不可覆盖），但这与业务参数 env 无关。

---

## 5. 与 secret 机制的关系（FR-017）

- config 承载**非敏感**数据（ConfigMap）；secret 承载**敏感**数据（K8s Secret 投影）。
- 两者挂载路径独立（`/mnt/dominion/config` vs `/mnt/dominion/secret`）、发现变量独立（`DOMINION_CONFIG_DIR` vs `DOMINION_SECRET_DIR`）。
- 同一 artifact 可同时使用 config 与 secret，互不干扰。
