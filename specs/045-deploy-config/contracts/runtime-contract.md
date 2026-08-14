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
| ConfigMap 名 | `{workload}-config-{sanitize(block)}`（每块一个；`sanitize` = 复用 `sanitizeNamePart` 的 RFC 1123 资源名清洗，见 §2） | `builder.go` 命名；校验见 §2 |
| ConfigMap data key | 条目名（块内唯一，原样作为 key，不拼接 block；data key 正则允许 `_`，**不清洗**） | `builder.go` |
| 触发条件 | 仅当 artifact 有 ≥1 个 config block 时创建 ConfigMap(s)/卷/挂载/env | `builder.go` |

> **对称性**：与 secret 约定（`DOMINION_SECRET_DIR` / `/mnt/dominion/secret` / 卷 `dominion-secrets`）完全对称，见 `specs/002-deploy-secret-config`。
>
> **per-block 映射**：每个配置块对应一个独立的 ConfigMap object，块内每个条目（`(block,key)` 寻址中的 key）直接作为该 ConfigMap 的一个 data key（key = 条目名，value = 原始数据文本，不做任何拼接/解析）。容器内文件布局仍为 `{DOMINION_CONFIG_DIR}/{block}/{key}`，由投影 `KeyToPath.Path` 还原目录层级（见 §2）。

---

## 2. ConfigMap 创建（控制面新增 apply 路径）

控制面当前**不创建**任何 ConfigMap（仅引用预存 TLS CA ConfigMap）。config 特性首次引入控制面创建数据型资源。

**映射模型**：**每个配置块（config block）一个 ConfigMap object**。块内每个条目（entry）直接作为该 ConfigMap 的一个 data key——key 为条目名（块内唯一，原样），value 为原始数据文本（service.yaml `configs[].data[].value` 原样）。一个 workload 选中 N 个配置块即产生 N 个 ConfigMap。

### 命名与长度校验

每个 ConfigMap 命名为 `{workload}-config-{sanitize(block)}`，其中：

- `{workload}` 为 `DeploymentWorkload`/`StatefulWorkload` 的 `WorkloadName()`（格式见 `projects/infra/deploy/runtime/k8s/naming.go` `newObjectName`，其各组成部分本就经 `sanitizeNamePart` 清洗）。
- `{sanitize(block)}` 为配置块名经 **`sanitizeNamePart`**（`naming.go`，与 `newObjectName` 清洗 env/service 名成分所用为同一函数）确定性清洗的结果：小写化 → 去首尾空白 → 将 `[^a-z0-9-]+` 连续段替换为单个 `-` → 去首尾 `-`。对 schema 合法块名（`^[a-z][a-z0-9_-]{0,63}$`，见 [yaml-schema.md §1](yaml-schema.md)），该清洗恰好退化为"连续 `_` 段折叠为单个 `-`"（如 `service_config` → `service-config`、`a__b` → `a-b`），其余字符原样保留；清洗确定且不增长长度。

**为什么 object 名要清洗**：K8s `metadata.name` 对大多数资源类型要求为 RFC 1123 DNS subdomain——仅小写字母数字、`-`、`.`，不允许 `_`（[K8s Names: DNS Subdomain Names](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names)、[RFC 1123](https://tools.ietf.org/html/rfc1123)）。块名 schema 允许 `_`（与 secret 逻辑名 pattern 一致），而 `_` 在下列其余两个用途中合法，故仅在 K8s object 名这一边界清洗，不收紧 schema。

**为什么 data key 与文件路径不清洗**（schema 命名成分在不同 K8s 边界的合法性差异——block 成分进入 `metadata.name` 与文件路径，条目名进入 data key 与文件路径）：

| 用途 | 约束 | `_` 合法性 | 处理 |
|------|------|-----------|------|
| ConfigMap `metadata.name` 的 block 成分 | RFC 1123 subdomain（如上） | ❌ 禁止 | `sanitizeNamePart` 清洗（本节） |
| ConfigMap data key（= 条目名） | `[-._a-zA-Z0-9]+`（[`configMapKeyFmt`](https://github.com/kubernetes/apimachinery/blob/master/pkg/util/validation/validation.go)，同文件提供 `IsDNS1123Subdomain` 校验函数） | ✅ 允许 | 原样，不清洗 |
| 容器内文件路径 `KeyToPath.Path` = `{block}/{key}`（block 与条目名均为路径成分） | 相对路径、允许 `/` 建子目录（[K8s Volume API](https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/volume/)）；文件系统路径允许 `_` | ✅ 允许 | 原样（真实块名/条目名）；SDK 按 `{DOMINION_CONFIG_DIR}/{block}/{key}` 寻址，文件布局不变 |

即：**仅 ConfigMap object 名清洗；data key 与容器内目录层级使用真实块名**。object 名无需反解出 block（发现/清理走 label，见下），清洗的有损性无影响。

builder MUST 对每个生成的 ConfigMap 做显式校验（fail-fast 返回 error，错误信息 MUST 包含 workload 名、**原始** block 名与计算后的名字，便于定位）：

1. **长度**：`len(name) > maxK8sResourceNameSize`（`naming.go`，值 `63`）→ error。该校验收敛既有 follow-up（旧设计 `{workload}-config` 在 workload 名接近 63 字符时即可能超限，per-block 进一步加长名字故更甚）；与代码库统一的 63 字符资源名上限一致（`Validate()` 对 Deployment/StatefulSet workload 名施加同一上限）。清洗不增长长度，长度校验逻辑不受影响。
2. **清洗后为空**：`sanitizeNamePart(block) == ""`（块名全部为非法字符）→ error。否则合成名将 `-` 结尾，违反 RFC 1123。schema 合法块名以 `[a-z]` 开头不可能触发；该分支仅防御绕过 schema 的非 CLI 客户端（domain 层 `ConfigBlock.Validate()` 只校验非空，不校验 pattern）。
3. **清洗后碰撞**：两个不同原始块名清洗后得到同一 ConfigMap 名 → error（如 `service_config` 与 `service-config`——schema 层块名唯一性 VR-CB-3 与 domain 层 VR-CB-6 均是**清洗前**的唯一性，不排除此情况）。碰撞检测位于 `BuildConfigMaps`（维护"计算名 → 原始块名"映射，计算名已存在即 error：原始块名不同为清洗碰撞，相同为重复块——后者已由 VR-CB-6 在 domain 层拒绝，此处为防御纵深兜底）；拦截有效的原因：executor `applyConfigMaps` 先调 `BuildConfigMaps` 再逐个 apply，且 `applyInner` 在 Deployment/StatefulSet 之前 apply 全部 ConfigMap（`executor.go`），故碰撞错误在任何资源变更前中止整个 apply。投影（`buildConfigProjection`）与 executor 期望集合（`buildExpectedApplyResources`）复用同一 `configMapName`，命名天然一致，无需各自检测。

```go
// 命名 + 校验（伪代码）
name := workload.WorkloadName() + "-config-" + sanitizeNamePart(block)   // configMapName 单一出口
if len(name) > maxK8sResourceNameSize {
    return nil, fmt.Errorf("configmap name %q (workload=%q block=%q) 超过 %d 字符上限",
        name, workload.WorkloadName(), block, maxK8sResourceNameSize)
}
// BuildConfigMaps 内：seen map[计算名]原始块名
//   - 计算名已存在于 seen → error（原始块名不同 = 清洗碰撞；相同 = 重复块，VR-CB-6 兜底）
//   - sanitizeNamePart(block) == "" → error
```

> **命名理由**：`{workload}-config-{sanitize(block)}` 延续 `{workload}-config` 风格；`-config-` infix 使其可 grep（区别于 TLS CA 等其他 ConfigMap）；labels（app/service/environment/managed-by，与 Deployment 一致）标注 ownership，供 executor 按 label 发现/prune（见 `executor.go` `pruneConfigMaps`）。无需从名字反解出 block——构建是确定性的（workload+block 经固定清洗），发现/清理走 label 而非名字解析。
>
> **拒绝收紧 schema 的理由（方向 B）**：另一方案是收紧块名 pattern 禁 `_`（`^[a-z][a-z0-9-]{0,63}$`）使资源名零歧义。拒绝原因：(1) 块名同时承担容器目录/SDK 寻址与用户命名两个角色，二者均合法且惯用 `_`，为一处边界约束收紧三处命名，牺牲用户命名表达力；(2) 破坏与 secret 逻辑名 pattern 的一致性（[002 VR-SD-2](../../002-deploy-secret-config/data-model.md) 允许 `_`；secret 逻辑名不出现在控制面创建的资源名中——这正是两特性用途差异所在，pattern 一致性是用户体验层面的对称）；(3) 全链路 fixture/demo/testdata（`service_config` 等）、schema、解析与单测均需改名，属 spec 层契约变更并偏离 spec.md Assumption"对齐现有命名规范"，成本高；(4) 仓库已有清洗先例——`newObjectName` 对 env/service 名成分同样经 `sanitizeNamePart` 清洗后进入资源名，本修复是将该既有模式应用于遗漏的 block 成分，而非引入新机制。清洗后碰撞以 builder fail-fast 兜底（见校验 3），唯一性语义保持完整。

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
//       data[ce.Key] = ce.Value              // key = 条目名，原样；不拼接 block、不清洗（data key 允许 `_`）
//   }
//   ObjectMeta: Name = configMapName(workload, cb.Block),   // {workload}-config-{sanitize(block)}，见"命名与长度校验"
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
            LocalObjectReference: corev1.LocalObjectReference{Name: configMapName(workload, cb.Block)}, // 同 BuildConfigMaps 的清洗后命名（configMapName 单一出口）
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

`executor.go` 的 `applyInner` 在引用 ConfigMap 的 Deployment/StatefulSet 之前 apply **该 workload 的全部 per-block ConfigMap**（N 个，按 `BuildConfigMaps` 返回顺序逐个 `applyTypedConfigMap`，Get→Create-if-NotFound→Update-with-ResourceVersion）；prune 列表（`pruneResources`）按 label 清理 ConfigMap（`pruneConfigMaps` 已 label-based，无需结构性改动）；`buildExpectedApplyResources` 的 `configMaps` 集合由 `workload.ConfigBlocks` 直接派生每个 block 名 `{workload}-config-{sanitize(block)}`（与 `BuildConfigMaps` 复用同一 `configMapName`，无漂移）；`Delete` 的 `deleteConfigMaps` 已 label-based。`BuildConfigMaps` 的 fail-fast（超长/清洗后为空/清洗后碰撞）在 `applyConfigMaps` 内先于任何 apply 生效，故非法命名与碰撞不会产生部分 apply。

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
