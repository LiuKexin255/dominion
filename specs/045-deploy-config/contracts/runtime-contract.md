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
| 卷名 | `dominion-config`（ProjectedVolume，单一卷，每块一个 source） | `builder.go` 常量 `configVolumeName` |
| 挂载模式 | 只读 | `VolumeMount.ReadOnly = true` |
| ConfigMap 粒度 | **每个配置块一个 ConfigMap object**（block 即逻辑配置单元） | `builder.go` 命名；`executor.go` 创建 |
| ConfigMap 名 | `{workload}-config-{block}`（每块一个） | `builder.go` 命名；长度校验见 §2 |
| ConfigMap data key | 条目名（块内唯一，原样作为 key，不拼接 block） | `builder.go` |
| 触发条件 | 仅当 artifact 有 ≥1 个 config block 时创建 ConfigMap(s)/卷/挂载/env | `builder.go` |

> **对称性**：与 secret 约定（`DOMINION_SECRET_DIR` / `/mnt/dominion/secret` / 卷 `dominion-secrets`）完全对称，见 `specs/002-deploy-secret-config`。
>
> **per-block 映射**：每个配置块对应一个独立的 ConfigMap object，块内每个条目（`(block,key)` 寻址中的 key）直接作为该 ConfigMap 的一个 data key（key = 条目名，value = 原始数据文本，不做任何拼接/解析）。容器内文件布局仍为 `{DOMINION_CONFIG_DIR}/{block}/{key}`，由投影 `KeyToPath.Path` 还原目录层级（见 §2）。

---

## 2. ConfigMap 创建（控制面新增 apply 路径）

控制面当前**不创建**任何 ConfigMap（仅引用预存 TLS CA ConfigMap）。config 特性首次引入控制面创建数据型资源。

**映射模型**：**每个配置块（config block）一个 ConfigMap object**。块内每个条目（entry）直接作为该 ConfigMap 的一个 data key——key 为条目名（块内唯一，原样），value 为原始数据文本（service.yaml `configs[].data[].value` 原样）。一个 workload 选中 N 个配置块即产生 N 个 ConfigMap。

### 命名与长度校验

每个 ConfigMap 命名为 `{workload}-config-{block}`，其中 `{workload}` 为 `DeploymentWorkload`/`StatefulWorkload` 的 `WorkloadName()`（格式见 `projects/infra/deploy/runtime/k8s/naming.go:41` `newObjectName`），`{block}` 为配置块名。

builder MUST 对每个生成的 ConfigMap 名做显式长度校验：`len(name) > maxK8sResourceNameSize`（`naming.go:38`，值 `63`）时立即 fail-fast 返回 error，错误信息 MUST 包含 workload 名、block 名与计算后的长度。该校验收敛既有 follow-up（旧设计 `{workload}-config` 在 workload 名接近 63 字符时即可能超限，per-block 进一步加长名字故更甚）；与代码库统一的 63 字符资源名上限一致（`Validate()` 对 Deployment/StatefulSet workload 名施加同一上限）。

```go
// 命名 + 长度校验（伪代码）
name := workload.WorkloadName() + "-config-" + block
if len(name) > maxK8sResourceNameSize {
    return nil, fmt.Errorf("configmap name %q (workload=%q block=%q) 超过 %d 字符上限",
        name, workload.WorkloadName(), block, maxK8sResourceNameSize)
}
```

> **命名理由**：`{workload}-config-{block}` 延续 `{workload}-config` 风格；`-config-` infix 使其可 grep（区别于 TLS CA 等其他 ConfigMap）；labels（app/service/environment/managed-by，与 Deployment 一致）标注 ownership，供 executor 按 label 发现/prune（见 `executor.go` `pruneConfigMaps`）。无需从名字反解出 block——构建是确定性的（workload+block），发现/清理走 label 而非名字解析。

### ConfigMap 内容（`BuildConfigMaps`）

由 `runtime/k8s/builder.go` 的 `BuildConfigMaps(workload, cfg)` 生成，**直接迭代 `workload.ConfigBlocks`**（层级结构本身就是 per-block，无需 builder 层分组），返回 N 个 ConfigMap，顺序按 ConfigBlocks 列表顺序（compiler 已保留 service.yaml 声明顺序，保证确定性）：

```go
// BuildConfigMaps 为 workload 的每个 ConfigBlock 生成一个 ConfigMap。
// data key = 条目名（块内唯一），value = 原始数据文本。
// 返回顺序按 workload.ConfigBlocks 列表顺序（确定性，无需重新分组）。
func BuildConfigMaps(workload configMapWorkload, cfg *K8sConfig) ([]*corev1.ConfigMap, error)

// 单块构造（伪代码）：
//   data := map[string]string{}
//   for _, ce := range cb.Entries {          // cb 即一个 ConfigBlock
//       data[ce.Key] = ce.Value              // key = 条目名，原样；不拼接 block
//   }
//   ObjectMeta: Name = workload.WorkloadName() + "-config-" + cb.Block,
//               Namespace = cfg.Namespace,
//               Labels 与 Deployment 一致（app/service/environment/managed-by）
```

`configMapWorkload` 接口（`model.go`）暴露 `configBlocks() []*domain.ConfigBlock`——消费侧直接持有层级结构，**不存在 builder 层 `configEntriesByBlock` 分组**（期望状态已层级化建模，详见 [proto.md §6](proto.md)）。

### 投影为容器文件（单一 projected volume，每块一个 source）

`BuildDeployment` / `BuildStatefulSet` 中，当 `len(workload.ConfigBlocks) > 0`，创建**单一** volume `dominion-config`（ProjectedVolume），其 `sources` 含**每个块一个** `ConfigMapProjection`；每个 source 的 `Items` 将该块的每个条目映射为容器内路径 `{block}/{key}`（`KeyToPath.Path` 允许子目录，用于还原目录层级）：

```go
// 直接迭代 ConfigBlocks（层级结构，无需重新分组）
var configSources []corev1.VolumeProjection
for _, cb := range workload.ConfigBlocks {
    var items []corev1.KeyToPath
    for _, ce := range cb.Entries {
        items = append(items, corev1.KeyToPath{
            Key:  ce.Key,                    // ConfigMap data key = 条目名
            Path: cb.Block + "/" + ce.Key,   // 容器内路径 {block}/{key}
        })
    }
    configSources = append(configSources, corev1.VolumeProjection{
        ConfigMap: &corev1.ConfigMapProjection{
            LocalObjectReference: corev1.LocalObjectReference{Name: workload.WorkloadName() + "-config-" + cb.Block},
            Items:                items,
        },
    })
}
volumes = append(volumes, corev1.Volume{
    Name: configVolumeName,
    VolumeSource: corev1.VolumeSource{
        Projected: &corev1.ProjectedVolumeSource{Sources: configSources},
    },
})
volumeMounts = append(volumeMounts, corev1.VolumeMount{
    Name: configVolumeName, MountPath: configMountPath, ReadOnly: true,
})
containerEnv = append(containerEnv, corev1.EnvVar{Name: envConfigDir, Value: configMountPath})
```

### K8s 约束依据

- **ConfigMap data key**：须匹配 `[-._a-zA-Z0-9]+`，不允许 `/`（[`k8s.io/apimachinery` `configMapKeyFmt`](https://github.com/kubernetes/apimachinery/blob/master/pkg/util/validation/validation.go)，[kubernetes/kubernetes#87119](https://github.com/kubernetes/kubernetes/issues/87119) 确认 `/` 被拒）。条目名匹配 `^[a-z][a-z0-9_-]{0,63}$`（其严格子集），故条目名可直接作为 ConfigMap data key，无需扁平化拼接。
- **projected volume 多 source**：`ProjectedVolumeSource.sources []VolumeProjection` 支持多个 ConfigMap source 合法挂入同一目录（[K8s Projected Volumes 文档](https://kubernetes.io/docs/concepts/storage/projected-volumes/) 示例即 secret+downwardAPI+configMap 同卷）。本仓库 secret 投影（`builder.go` `dominion-secrets` 卷）已用同一机制（多 Secret source 入单卷）。
- **KeyToPath.Path 子目录**：`Path` 须相对、不含 `..`，允许 `/` 建子目录（[K8s Volume API](https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/volume/) "Paths must be relative and may not contain the '..' path"，[`configmap_test.go` "subdirs"](https://github.com/kubernetes/kubernetes/blob/master/pkg/volume/configmap/configmap_test.go) 验证 `path/to/1/2/3/foo.txt`）。故 `{block}/{key}` 可还原目录层级。

### executor apply/prune 集成

`executor.go` 的 `applyInner` 在引用 ConfigMap 的 Deployment/StatefulSet 之前 apply **该 workload 的全部 per-block ConfigMap**（N 个，按 `BuildConfigMaps` 返回顺序逐个 `applyTypedConfigMap`，Get→Create-if-NotFound→Update-with-ResourceVersion）；prune 列表（`pruneResources`）按 label 清理 ConfigMap（`pruneConfigMaps` 已 label-based，无需结构性改动）；`buildExpectedApplyResources` 的 `configMaps` 集合由 `workload.ConfigBlocks` 直接派生每个 block 名 `{workload}-config-{block}`（无需 `configEntriesByBlock` 分组）；`Delete` 的 `deleteConfigMaps` 已 label-based。

> **顺序**：全部 per-block ConfigMap MUST 在引用它们的 Deployment/StatefulSet 之前 apply（Deployment 投影 ConfigMap，ConfigMap 不存在时 Pod 启动失败）。同一 workload 的多个 per-block ConfigMap 间无依赖，apply 顺序按 `BuildConfigMaps` 返回顺序（ConfigBlocks 列表序）即可。

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
