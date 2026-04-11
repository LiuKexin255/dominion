# 目标

调整 `tools/deploy/` 的环境标识模型，将当前 `app + env + template` 的组合模型收敛为以**完整环境名**为中心的单一模型。

完整环境名格式为：`{scope}.{env_name}`。

本方案只覆盖本次环境标识改造，不展开未来规划。

## 本次实现范围

### 目标边界

本次只覆盖以下内容：

1. `deploy.yaml` 移除 `template`、`app`，保留字段名 `name`，但其含义改为**完整环境名**。
2. `deploy` 工具删除 `--app` 参数。
3. `deploy` 工具支持输入完整环境名和简版环境名；简版仅用于工具输入层。
4. `apply` 不再要求先执行 `use`；若目标环境不存在，则先创建环境，再继续部署。
5. 本地缓存文件名、目录结构、上下文模型统一改为围绕完整环境名。
6. Kubernetes 标签、删除选择器、资源命名中涉及旧环境归属字段的部分同步改造。
7. 不兼容旧格式，不做双读双写。

### 非目标

本次不解决以下问题：

1. 旧 `.env` 缓存格式兼容迁移。
2. 旧标签模型与新标签模型共存。
3. `service.yaml` 自身语义调整；其中的 `app` 仍表示服务自身归属。

## 核心模型

### 环境标识

环境唯一标识为完整环境名：

```text
{scope}.{env_name}
```

后续流程中统一只使用完整环境名；简版环境名只允许出现在 CLI 输入层。

### 命名约束

`scope` 和 `env_name` 都满足：

```regex
^[a-z][a-z0-9]{0,7}$
```

完整环境名满足：

```regex
^[a-z][a-z0-9]{0,7}\.[a-z][a-z0-9]{0,7}$
```

含义：

1. 只允许小写字母和数字。
2. 必须以小写字母开头。
3. 每段长度为 `1..8`。
4. 完整环境名恰好包含一个 `.`，左侧为 `scope`，右侧为 `env_name`。

## 配置与输入规则

### `deploy.yaml`

改造后的环境字段仍命名为 `name`，但表示完整环境名：

```yaml
name: alice.dev
desc: "开发环境"
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
```

约束：

1. `name` 必须匹配完整环境名正则。
2. `template` 字段删除。
3. `app` 字段删除。

### deploy 工具输入规则

命令行输入环境名时，支持两种形式：

1. **完整名**：如 `alice.dev`
2. **简版名**：如 `dev`

解析规则：

1. 输入包含 `.` 时，直接视为完整环境名。
2. 输入不包含 `.` 时，直接视为简版环境名，使用仓库级默认 `scope` 拼接为完整环境名。
3. 未配置默认 `scope` 时，任何简版输入直接报错。
4. 不做模糊匹配，不扫描已有环境，不推测。

## 默认 scope

### 来源

默认 `scope` 的唯一来源为 `deploy` 工具提供的**仓库级配置命令**。

### 语义要求

1. 默认 `scope` 与当前激活环境无关。
2. 删除当前环境不会影响默认 `scope`。
3. 当 `use dev`、`del dev` 等简版输入出现时，只能使用该仓库级配置补全。

## 命令语义调整

### `use`

`use` 的职责为“切换当前环境上下文；如环境不存在则创建并激活”。

行为：

1. 输入完整名时，直接切换/创建该完整环境。
2. 输入简版名时，先补全为完整名，再切换/创建。

### `apply`

`apply` 不再要求预先执行 `use`。

行为：

1. 读取 `deploy.yaml` 中的完整环境名 `name`。
2. 以该完整环境名查找环境。
3. 若不存在，则先创建环境。
4. 更新本地缓存。
5. 执行部署。
6. 部署成功后，将该环境设置为当前激活环境。

`apply` 的后续流程中不再进行“短名 -> 完整名”的二次推导。

### `del`

`del` 支持完整名和简版名输入；进入删除逻辑前统一解析为完整环境名。

### `list` / `cur`

`list` 和 `cur` 统一展示完整环境名。

## 本地缓存模型

### 上下文文件

上下文中不再保存 `LastApp`。

建议上下文只保留：

1. 当前激活环境完整名
2. 默认 `scope`

即：

1. 不再保存 `ActiveEnv{Name, App}` 这样的拆分结构。
2. 不再保存 `LastApp`。

### 缓存目录结构

缓存目录继续使用现有 `.env/` 根目录，但文件与目录命名围绕完整环境名重做。

建议结构：

1. `.env/current.json`
2. `.env/profile/<safe-full-env>.json`
3. `.env/deploy/<safe-full-env>.yaml`
4. `.env/service/<safe-full-env>.yaml`

其中 `<safe-full-env>` 为统一安全编码后的完整环境名，不再使用多字段拼接文件名。

### 安全编码要求

完整环境名用于缓存文件名时，通过统一 helper 做安全编码。

要求：

1. 同一完整环境名编码结果稳定。
2. 不同完整环境名不会与旧的多字段文件名格式混淆。
3. 所有 profile / deploy / service 缓存共享同一编码规则。

## Kubernetes 标签与删除逻辑

### 标签模型

保留标签键：

```text
dominion.io/environment
```

其值改为**完整环境名**。

删除标签键：

```text
dominion.io/app
```

### 删除选择器

环境删除的唯一环境选择条件为完整环境名。

即：

1. 删除逻辑不再依赖 `app + environment` 双字段组合。
2. 删除逻辑只围绕 `dominion.io/environment=<full-env-name>` 构建环境归属。

## 资源命名

### 基本原则

标签和环境变量中使用完整环境名。

Kubernetes 资源名中不直接使用完整环境名原文，而是使用其短版 hash 结果参与命名，以满足长度与字符约束。

### 规则

1. 资源命名延续现有 `shortNameHash` 风格。
2. 将完整环境名作为输入，生成稳定短标识。
3. Deployment / Service / HTTPRoute / MongoDB 及其附属资源都使用同一套完整环境名短标识规则。
4. 同一完整环境名在所有资源类型中生成结果一致。

## 运行时环境变量

`DOMINION_ENVIRONMENT` 注入完整环境名。

运行时不再使用简版环境名。

## 涉及代码路径

以下路径是本次改造的核心锚点：

1. `tools/deploy/main.go`
2. `tools/deploy/main_test.go`
3. `tools/deploy/pkg/schema/deploy.schema.json`
4. `tools/deploy/pkg/schema/schema_test.go`
5. `tools/deploy/pkg/config/config.go`
6. `tools/deploy/pkg/config/config_test.go`
7. `tools/deploy/pkg/env/env.go`
8. `tools/deploy/pkg/env/env_test.go`
9. `tools/deploy/pkg/k8s/workload.go`
10. `tools/deploy/pkg/k8s/builder.go`
11. `tools/deploy/pkg/k8s/converter.go`
12. `tools/deploy/pkg/k8s/executor.go`
13. `tools/deploy/pkg/k8s/mongo.go`
14. `tools/deploy/README.md`

## 关键细节锚点

以下内容为本方案不可偏离的关键锚点：

1. `deploy.yaml.name` 表示完整环境名，不再表示简版环境名。
2. 简版环境名只允许出现在 CLI 输入层。
3. 默认 `scope` 是仓库级配置，且是简版补全的唯一来源。
4. 未配置默认 `scope` 时，所有简版输入直接报错。
5. 所有内部流程统一只使用完整环境名。
6. 本地缓存文件名不再使用旧的多字段拼接格式。
7. `dominion.io/environment` 保留，值改为完整环境名。
8. `dominion.io/app` 删除。
9. 删除逻辑以完整环境名为唯一环境选择条件。
10. 资源名使用完整环境名的短版 hash；标签与环境变量使用完整环境名原文。
