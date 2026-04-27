# Deploy v3 artifact_image 镜像构建与推送方案

## 目标

本方案用于为 `tools/release/deploy` 增加 v3 配置与镜像推送链路，目标是：

* 将 deploy 配置版本化，让 v3 CLI 只处理明确声明为 `version: "3.0"` 的 `deploy.yaml` 与 `service.yaml`，避免破坏现有 v2 配置。
* 引入 `artifact_image` Bazel 封装，统一服务镜像的 tar 层、OCI image、push target 与元数据生成，减少业务 BUILD 文件重复样板。
* 将服务二进制在镜像内的部署路径固化为 `/dominion/{app}/{service}/bin/{binary}`，让镜像结构与服务身份绑定。
* 让 `service.yaml` 的 artifact target 指向稳定的 `artifact_image` target，而不是裸 `oci_image` 或 `oci_push` target。
* 继续复用 `rules_oci` 的 `oci_push` / `crane` 能力完成镜像推送，避免依赖本地 Docker / Podman，也避免 deploy CLI 自行实现完整 registry push。
* 将镜像仓库固定为 `registry.liukexin.com/{app}/{service}`，上传 tag 固定为 `latest`，但提交给 deploy service 的镜像仍使用不可变 digest ref。

最终效果：业务服务只声明一个 `artifact_image` 构建目标和 v3 `service.yaml`，deploy v3 CLI 能够校验配置身份、运行预生成的 push target 推送镜像，并将 `registry.liukexin.com/{app}/{service}@sha256:...` 写入 desired state。

## 范围

本方案覆盖以下内容：

* `deploy.yaml` 与 `service.yaml` 的 `version` 字段和 v2 / v3 配置边界。
* `artifact_image` Bazel macro / rule 的产物模型。
* v3 `service.yaml` 中 artifact target 的语义变化。
* v3 deploy CLI 的镜像解析、身份校验、push target 执行和 digest ref 生成。
* v2 / v3 CLI 共存策略。
* 相关单测与集成验证要求。

本方案不包括：

* 修改 deploy service 的 desired state API 或 Kubernetes runtime 模型。
* 修改 v2 配置的现有镜像推送语义。
* 支持动态 registry、动态 tag 或多 registry 推送。
* deploy CLI 自行实现 OCI registry push 协议。
* 依赖 Docker / Podman 等本地容器运行时。

## 当前问题

当前 deploy v2 镜像链路为：

```text
service.yaml artifact.target -> oci_image target
deploy CLI -> target + "_push"
bazel run <oci_push target>
deploy CLI 反解析 rules_oci 生成的 push shell script
deploy CLI 从 OCI layout index.json 获取 digest
compiler -> repository@digest
```

该链路存在以下问题：

* `service.yaml` 明面上配置的是 `oci_image` target，但 deploy CLI 隐式要求同包存在 `{target}_push`。
* 业务 BUILD 文件需要重复声明 `tar`、`oci_image`、`oci_push` 与 tag 文件。
* deploy CLI 依赖 `rules_oci` 生成 shell script 的内部结构来获取 `IMAGE_DIR`、repository 和 digest，该结构不是 Dominion 自己拥有的稳定契约。
* 镜像内二进制路径由各业务 BUILD 自行维护，无法从 `app` / `service` 身份上统一校验。
* 当前 `service.yaml` 的 `app`、`name` 与 Bazel target 之间没有交叉校验，配置漂移时容易推送或部署错误镜像。

因此，v3 需要把稳定契约迁移到 Dominion 自己控制的 `artifact_image` target 与 metadata 上，同时继续利用 `rules_oci` 执行实际推送。

## 总体方案

v3 方案采用“配置版本隔离 + artifact_image 预生成 push target”的模型。

核心链路为：

```text
v3 service.yaml artifact.target -> artifact_image target
bazel build <artifact_image target>
deploy CLI 读取 artifact_image metadata
deploy CLI 校验 app / service / repository
deploy CLI bazel run <metadata.push_target>
rules_oci oci_push 推送 registry.liukexin.com/{app}/{service}:latest
deploy CLI 读取 OCI layout digest
compiler -> registry.liukexin.com/{app}/{service}@sha256:...
```

关键决策：

* `artifact_image` 为每个服务镜像预生成一个专属 `oci_push` target。
* 一个 push target 绑定一个 image target；不同镜像内容通过不同的 `artifact_image` 生成不同 push target。
* repository 固化为 `registry.liukexin.com/{app}/{service}`。
* tag 固化为 `latest`，但 deploy desired state 不使用 `:latest`，而使用 push 后对应的 digest ref。
* v3 CLI 不动态写临时 BUILD 文件生成 `oci_push`，避免污染 workspace 和破坏 Bazel 缓存模型。

## 配置版本模型

### v2 配置

现有配置保持 v2 语义。推荐逐步补齐：

```yaml
version: "2.0"
```

未声明 `version` 的旧配置在 v2 parser 中视为 `2.0`，用于兼容已有文件。

### v3 配置

v3 `deploy.yaml` 与其引用的 `service.yaml` 都必须声明：

```yaml
version: "3.0"
```

约束：

* v3 CLI 只接受 `version >= 3.0` 的配置。
* v3 CLI 遇到未声明版本或 `version: "2.0"` 的配置时直接拒绝，并提示使用 v2 CLI 或升级配置。
* v3 `deploy.yaml` 引用 v2 `service.yaml` 时拒绝。
* v2 CLI 保持现有行为，不要求立即迁移到 v3。

## `artifact_image` 模型设计

业务 BUILD 文件中声明：

```python
artifact_image(
    name = "cmd_image",
    app = "game",
    service = "session",
    binary = ":cmd",
)
```

`artifact_image` 内部生成：

```text
:cmd_image          # 对外 target，默认输出 metadata
:cmd_image_layer    # 内部 tar layer
:cmd_image_oci      # 内部 oci_image，输出 OCI layout
:cmd_image_tags     # 内部 tag 文件，内容为 latest
:cmd_image_push     # 内部 oci_push，绑定 :cmd_image_oci
```

命名决策：

* push target 使用 `{name}_push`，例如 `:cmd_image_push`，延续当前仓库已有 `oci_push` 命名习惯。
* image target 使用 `{name}_oci`，避免与对外 `artifact_image` target 重名。
* 对外 `artifact_image` target 默认输出 metadata，供 deploy v3 CLI 读取。

### 镜像内路径

`artifact_image` 生成的 tar layer 必须将二进制放到：

```text
/dominion/{app}/{service}/bin/{binary}
```

例如：

```text
/dominion/game/session/bin/cmd
```

`oci_image` 的 entrypoint 对应该路径：

```text
/dominion/game/session/bin/cmd
```

### metadata

`artifact_image` 默认输出 metadata JSON，例如：

```json
{
  "schema_version": "3.0",
  "app": "game",
  "service": "session",
  "binary": "cmd",
  "entrypoint": "/dominion/game/session/bin/cmd",
  "image_target": "//projects/game/session/app/cmd:cmd_image_oci",
  "push_target": "//projects/game/session/app/cmd:cmd_image_push",
  "repository": "registry.liukexin.com/game/session",
  "tag": "latest"
}
```

metadata 是 deploy v3 CLI 与 Bazel 构建目标之间的稳定契约。CLI 不再解析 `rules_oci` 生成的 push shell script 来获取 push target 或 repository。

## v3 `service.yaml` 模型

示例：

```yaml
version: "3.0"
name: session
app: game
desc: game session service
kind: stateless
artifacts:
  - name: cmd
    target: app/cmd:cmd_image
    tls: true
    ports:
      - name: http
        port: 8081
```

字段语义：

* `name` 表示服务名，用于镜像仓库路径中的 `{service}`。
* `app` 表示应用名，用于镜像仓库路径中的 `{app}`。
* `artifacts[].target` 指向 `artifact_image` target，而不是裸 `oci_image` 或 `oci_push`。

v3 CLI 读取 metadata 后执行校验：

* `service.yaml.app == metadata.app`
* `service.yaml.name == metadata.service`
* `metadata.repository == registry.liukexin.com/{app}/{service}`
* `metadata.tag == latest`
* `metadata.push_target` 非空且为合法 Bazel label
* `metadata.image_target` 非空且为合法 Bazel label

如果任一校验失败，本次 deploy 在推送镜像前失败。

## 镜像推送设计

### 预生成 push target 的含义

预生成 push target 不是指一个 target 可以运行时切换任意 image，而是每个 `artifact_image` 自动生成一个专属 `oci_push` target。

例如：

```python
artifact_image(name = "session_image", app = "game", service = "session", binary = ":session")
artifact_image(name = "gateway_image", app = "game", service = "gateway", binary = ":gateway")
```

展开后：

```text
:session_image_push -> image = :session_image_oci -> registry.liukexin.com/game/session
:gateway_image_push -> image = :gateway_image_oci -> registry.liukexin.com/game/gateway
```

因此：

* 不同镜像内容通过不同 `artifact_image` 生成不同 push target。
* 同一个 push target 的 image 输入固定。
* repository / tag 由 `artifact_image` 的 app / service 固化，v3 CLI 只校验并执行。

### v3 CLI 推送流程

v3 CLI 对每个 artifact target 执行：

```text
bazel build <artifact_image target>
读取 artifact_image metadata
校验 app / service / repository / tag
bazel run <metadata.push_target>
读取 metadata.image_target 对应 OCI layout 的 index.json
提取 manifests[0].digest
返回 registry.liukexin.com/{app}/{service}@sha256:...
```

推送由 `rules_oci` 的 `oci_push` 完成，底层复用 `crane`。CLI 不依赖 Docker / Podman，也不自行实现 registry push。

### digest ref

虽然 push target 将镜像上传为：

```text
registry.liukexin.com/{app}/{service}:latest
```

但 deploy desired state 中使用：

```text
registry.liukexin.com/{app}/{service}@sha256:...
```

原因：

* `latest` 是可变 tag，只适合作为上传入口。
* digest ref 是不可变引用，能保证 Kubernetes 实际部署的镜像内容与本次 deploy 编译结果一致。
* 并发 deploy 即使覆盖同一个 `latest` tag，也不会影响已经提交给 deploy service 的 digest ref。

## 代码分层

### Bazel release rules

位置建议：

```text
tools/release/defs.bzl
```

新增：

```python
artifact_image(...)
```

职责：

* 生成 tar layer。
* 生成 oci_image。
* 生成 latest tag 文件。
* 生成 oci_push。
* 生成 metadata JSON。

### config / schema

位置：

```text
tools/release/deploy/pkg/config
tools/release/deploy/pkg/schema
```

职责：

* 增加 `version` 字段。
* v2 parser 对未声明版本默认 `2.0`。
* v3 parser 强制 `version >= 3.0`。
* v3 service parser 保持 target 标准化，但 target 语义变更为 artifact_image target。

### image resolver

位置建议：

```text
tools/release/deploy/pkg/imagepush
```

保留 v2 resolver：

```text
artifact target -> DerivePushTarget -> bazel run oci_push
```

新增 v3 resolver：

```text
artifact target -> build metadata -> validate -> run metadata.push_target -> digest ref
```

v3 resolver 不再使用 `_push` 字符串派生作为公开契约，而是使用 metadata 中的 `push_target`。

### CLI

推荐新增独立路径：

```text
tools/release/deploy/v3
```

职责：

* 只处理 v3 配置。
* 读取 v3 deploy / service 配置。
* 调用 v3 image resolver。
* 复用现有 compiler 能力生成 desired state。
* 调用 deploy service API。

v2 CLI 保持：

```text
tools/release/deploy/v2
```

## 决策详情

### 为什么不由 CLI 动态写临时 BUILD 生成 `oci_push`

不采用该方案，原因是：

* 会污染 workspace 或需要复杂临时目录映射。
* 并发 deploy 时需要处理临时文件冲突与清理。
* Bazel analysis graph 不稳定，缓存与错误定位变差。
* 运行时生成 BUILD 文件会让 deploy 行为更难复现。

因此采用 `artifact_image` 在 Bazel 侧预生成 push target 的方式。

### 为什么 `service.yaml` 不直接指向 `oci_push`

不采用该方案，原因是：

* `service.yaml` 描述的是服务产物，而不是推送动作。
* 直接暴露 `oci_push` 会让部署配置绑定推送实现细节。
* v3 需要校验 `app` / `service` / 镜像内路径 / repository，这些都属于 artifact metadata，而不是单纯 push target 能表达的模型。

### 为什么 repository 和 tag 固化

repository 固化为：

```text
registry.liukexin.com/{app}/{service}
```

tag 固化为：

```text
latest
```

原因是：

* deploy 平台希望镜像归属与服务身份一致。
* 用户不需要在业务配置中维护 registry 路径，减少配置漂移。
* digest ref 仍保证最终部署确定性，因此 `latest` 的可变性不会进入 desired state。

## v2 / v3 共存与迁移

v2 保持现状：

```text
service.yaml target -> oci_image
deploy CLI derives target_push
bazel run oci_push
```

v3 使用新语义：

```text
service.yaml target -> artifact_image metadata target
metadata.push_target -> generated oci_push
```

迁移建议：

1. 先让 v2 parser 支持 `version`，未声明默认 `2.0`。
2. 新增 v3 parser / CLI，只接受 `version: "3.0"`。
3. 新增 `artifact_image` 并在测试 BUILD 中验证生成产物。
4. 新增 v3 image resolver。
5. 选择一个非核心示例服务迁移到 v3 配置验证链路。
6. 再逐步迁移业务服务配置。

## 测试与验收

需要覆盖：

* v2 未声明 `version` 时默认 `2.0`。
* v3 CLI 拒绝未声明 version 或 `version: "2.0"` 的 deploy 配置。
* v3 deploy 配置引用 v2 service 配置时失败。
* `artifact_image` 生成 metadata、OCI image、push target 与 latest tag 文件。
* tar layer 中二进制路径为 `/dominion/{app}/{service}/bin/{binary}`。
* metadata 中 app / service 与 `service.yaml` 不一致时失败。
* metadata repository 不是 `registry.liukexin.com/{app}/{service}` 时失败。
* v3 resolver 使用 metadata 中的 `push_target`，不再通过字符串拼接派生 `_push`。
* 推送完成后 desired state 中的 image 使用 `repository@sha256:...`，不使用 `repository:latest`。
* 可选集成测试：使用本地或 fake registry 验证 `oci_push` 能推送并返回可用 digest。

## 未来规划

以下内容不属于本方案交付范围，可在后续方案中单独设计：

* 支持多 registry 或按环境选择 registry。
* 支持除 `latest` 外的额外 tag。
* 支持非 Go binary 的 artifact_image 封装。
* 支持远程执行场景下更稳定地回收 OCI layout digest。
* 将 v2 配置批量升级为 v3 配置并移除旧 `_push` 派生逻辑。
