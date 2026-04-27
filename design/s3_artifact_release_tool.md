# S3 artifact release 通用工具方案

## 目标

本方案用于将 `projects/game/windows_agent/release/` 中的 S3 制品发布能力迁移到 `tools/release/`，并抽象为仓库通用的 Bazel 发布工具，目标是：

* 让任意 Bazel 单文件制品可以通过声明式 `s3_artifacts_push` target 发布到统一 S3 目录。
* 让发布入口采用类似 `oci_push` 的使用方式：`bazel build` 只构建制品，`bazel run //...:<name>` 才执行 S3 上传副作用。
* 让发布元信息由 YAML manifest 维护，避免在 `BUILD.bazel` 中重复维护版本、平台、架构和发布文件名等业务语义。
* 让 Bazel rule 的 `artifacts` 参数只承担依赖声明职责，确保 `bazel run` 前 Bazel 能正确构建所有待发布制品。
* 让发布目录、manifest、checksum 和上传行为在所有项目中保持一致，避免每个项目维护临时发布脚本。

本方案希望达成的效果是：开发者为项目编写一个 `release.yaml` 和一个 `s3_artifacts_push` target 后，可以通过 `bazel run` 将一个版本下的多个平台/架构制品发布到固定 S3 路径，并自动生成 `manifest.json` 与 `SHA256SUMS`。

## 范围

本方案覆盖：

* `tools/release` 下通用 S3 artifact push Bazel rule。
* `release.yaml` manifest 配置模型。
* 发布运行时工具的代码分层、校验规则和上传语义。
* `projects/game/windows_agent/release/` 到通用工具的迁移方式。
* 单版本多平台、多架构制品发布。
* `manifest.json` 与 `SHA256SUMS` 生成规则。

本方案不包括：

* S3 bucket、endpoint、region 的项目级配置化。
* 自动创建 bucket、清理旧版本、生命周期策略或权限编排。
* 自动更新、latest 指针、发布回滚或版本废弃机制。
* 多个制品共享同一 `platform + arch` 的场景。
* 在 Bazel build/test action 中执行网络上传。
* 对 OCI image push 体系的改造。

## 当前问题

当前 `projects/game/windows_agent/release/` 中已经存在项目级 S3 发布工具：

* `main.go` 暴露 `publish_s3 --version --s3-url --dist-dir`。
* `publish.go` 校验 dist 目录、解析 `s3://bucket/prefix`、生成 manifest/checksum，并使用 `pkg/s3` 上传目录内文件。
* `manifest.go` 生成的 manifest 强耦合 Windows agent，包含 `platform = windows/amd64` 以及 `wails`、`ffmpeg`、`inputHelper` 等字段。
* `BUILD.bazel` 只提供项目级 `go_binary(name = "publish_s3")`，与 `windows_agent_win_zip` Bazel target 没有声明式依赖关系。

这带来几个问题：

* 发布工具只能服务 Windows agent，难以复用于其他制品。
* `--dist-dir` 需要调用者先手工准备目录，Bazel 不知道待发布 target 与 publish target 的依赖关系。
* 版本、平台、架构、发布文件名和远端路径分散在命令行或项目脚本中，不利于代码审查和复现。
* 项目级 manifest schema 与通用 release 语义混在一起，后续扩展成本高。

## 总体方案

本方案采用以下收敛方向：

1. 在 `tools/release` 中新增通用 `s3_artifacts_push` Bazel rule。
2. 项目在 `BUILD.bazel` 中声明 publish target，并通过 `artifacts` 参数列出待发布 Bazel 制品 target。
3. 项目通过 `release.yaml` 维护发布元信息，包括 `name`、`version`、每个制品的 `target`、`filename`、`platform` 和 `arch`。
4. 远端地址不作为 BUILD 参数或 manifest 参数暴露，固定推导为：

```text
s3://artifacts/{name}/{version}/
```

5. `bazel build //...:<push-target>` 只构建 launcher、manifest 和 artifact inputs，不执行上传。
6. `bazel run //...:<push-target>` 构建所有 artifact 后，运行 push 工具执行 staging、manifest/checksum 生成和 S3 上传。

## 最终模型

## BUILD 模型

项目侧示例：

```starlark
load("//tools/release:defs.bzl", "s3_artifacts_push")

s3_artifacts_push(
    name = "windows_agent_push",
    manifest = "release.yaml",
    artifacts = [
        ":windows_agent_win_x64_zip",
        ":windows_agent_win_x86_zip",
    ],
    visibility = ["//visibility:public"],
)
```

字段语义：

* `name`：Bazel publish target 名称。
* `manifest`：发布 manifest YAML 文件。
* `artifacts`：待发布 Bazel 制品 target 列表，只承担 Bazel 依赖声明职责。

`artifacts` 参数保留的原因是：Bazel 普通 rule 无法在分析期读取由另一个 target 生成的 manifest 再动态声明依赖；显式 `artifacts` 可以让 `bazel run` 在运行前先构建所有待发布制品，符合 Bazel 分析期与执行期模型。

执行方式：

```bash
bazel run //projects/game/windows_agent:windows_agent_push
```

## YAML manifest 模型

`release.yaml` 示例：

```yaml
name: windows-agent
version: 0.1.0

artifacts:
  - target: //projects/game/windows_agent:windows_agent_win_x64_zip
    filename: windows-agent-0.1.0-windows-amd64.zip
    platform: windows
    arch: amd64

  - target: //projects/game/windows_agent:windows_agent_win_x86_zip
    filename: windows-agent-0.1.0-windows-386.zip
    platform: windows
    arch: "386"
```

### 顶层字段

* `name`：发布制品名称，必填。用于构造统一远端路径。
* `version`：发布版本，必填，格式固定为 `x.y.z`。
* `artifacts`：本版本发布的制品列表，必填且非空。

### artifact 字段

* `target`：制品对应的 Bazel target，必填，用于与 BUILD 中 `artifacts` 参数做一致性校验。
* `filename`：上传到 S3 后的文件名，必填，允许与 Bazel 原始输出文件名不同。
* `platform`：平台枚举，必填。
* `arch`：架构枚举，必填。

### version 约束

`version` 必须满足：

```text
^[0-9]+\.[0-9]+\.[0-9]+$
```

合法示例：

* `0.1.0`
* `1.2.3`

非法示例：

* `v1.2.3`
* `1.2`
* `1.2.3-beta.1`

### platform 枚举

`platform` 只允许以下值：

* `windows`
* `linux`
* `darwin`

### arch 枚举

`arch` 只允许以下值：

* `amd64`
* `386`
* `arm64`
* `arm`

### 唯一性约束

同一个 `release.yaml` 中：

* `artifacts[].target` 必须唯一。
* `artifacts[].filename` 必须唯一。
* `platform + arch` 组合必须唯一。

本方案明确不允许同一 `platform + arch` 下存在多个产物。若后续需要同时发布 zip、installer、tar 等同平台多形态产物，应另行设计 `kind` 或等价字段，不在本方案范围内。

## 远端路径模型

远端路径固定由 manifest 顶层字段推导：

```text
s3://artifacts/{name}/{version}/
```

例如：

```yaml
name: windows-agent
version: 0.1.0
```

对应发布目录：

```text
s3://artifacts/windows-agent/0.1.0/
```

发布后的目录形态：

```text
s3://artifacts/windows-agent/0.1.0/
  ├── windows-agent-0.1.0-windows-amd64.zip
  ├── windows-agent-0.1.0-windows-386.zip
  ├── manifest.json
  └── SHA256SUMS
```

S3 bucket 固定写在发布工具代码中，初始使用占位符值，完成实现后由开发者替换为正式值。当前方案约定正式 bucket 名称为 `artifacts`。

`artifacts` bucket 与 `{name}/{version}` 目录规则不暴露为 BUILD 参数或 manifest 参数，避免不同项目各自定义发布路径。

## 输出 manifest.json 模型

发布工具在 staging 目录中生成最终 `manifest.json`，格式如下：

```json
{
  "name": "windows-agent",
  "version": "0.1.0",
  "artifacts": [
    {
      "filename": "windows-agent-0.1.0-windows-amd64.zip",
      "platform": "windows",
      "arch": "amd64",
      "size": 123456,
      "sha256": "..."
    }
  ]
}
```

字段语义：

* `name`、`version` 来自 `release.yaml`。
* `artifacts[].filename`、`platform`、`arch` 来自 `release.yaml`。
* `artifacts[].size`、`sha256` 由发布工具根据 staging 后文件计算。

最终 `manifest.json` 不包含 Bazel target 信息。target 是构建与发布映射信息，不属于面向下载方的制品元信息。

## SHA256SUMS 模型

`SHA256SUMS` 使用标准文本格式：

```text
<sha256>  <filename>
```

生成规则：

* 包含所有发布 artifact 文件。
* 包含 `manifest.json`。
* 不包含 `SHA256SUMS` 自身。
* 文件顺序按文件名稳定排序。

## 模型设计

### Bazel rule 模型

`tools/release/defs.bzl` 提供：

```starlark
s3_artifacts_push(
    name,
    manifest,
    artifacts,
    visibility = None,
)
```

内部可由 public macro 包装 private executable rule 实现，遵循当前 `tools/dev/wails/defs.bzl` 的模式：

* public macro 提供稳定 API。
* private rule 生成 launcher。
* push 工具通过 private executable attr 注入。
* `manifest` 和 `artifacts` 进入 runfiles。
* rule 返回 `DefaultInfo(executable = launcher, runfiles = ...)`。

### 运行时模型

`bazel run` 执行 publish target 时，launcher 将以下信息传给 push 工具：

* manifest runfile 路径。
* artifact target 到 runfile 路径的映射。
* artifact target 到 Bazel label 字符串的映射。

push 工具执行流程：

1. 读取并解析 `release.yaml`。
2. 校验 manifest 字段、枚举、唯一性和版本格式。
3. 校验 manifest 中的 `target` 集合与 Bazel rule `artifacts` 集合完全一致。
4. 校验每个 artifact target 只有一个输出文件。
5. 创建临时 staging 目录。
6. 将每个 artifact 复制到 staging 目录，并按 manifest 中 `filename` 重命名。
7. 计算 artifact size 与 SHA256。
8. 生成 `manifest.json`。
9. 生成 `SHA256SUMS`。
10. 使用 `pkg/s3` 创建 S3 client。
11. 上传 artifact 文件。
12. 上传 `manifest.json`。
13. 上传 `SHA256SUMS`。

### S3 client 模型

发布工具复用现有 `pkg/s3`：

* endpoint 固定为 `s3.liukexin.com`。
* bucket 固定为代码内常量，初始使用占位符值，正式值为 `artifacts`。
* 凭证从运行时环境变量读取：
  * `S3_ACCESS_KEY`
  * `S3_SECRET_KEY`
* region 使用 `pkg/s3` 默认值。

发布工具不得自行创建 MinIO client，也不得重复维护 endpoint、credential 或 region 读取逻辑；连接 S3 必须通过 `pkg/s3.NewS3Client("")` 创建客户端。

S3 凭证不得出现在：

* `BUILD.bazel`
* `release.yaml`
* Bazel action inputs
* generated launcher 内容
* runfiles 配置文件

## 代码分层

建议新增结构：

```text
tools/release/
  BUILD.bazel
  defs.bzl
  s3push/
    BUILD.bazel
    main.go
    manifest.go
    publish.go
    staging.go
    runfiles.go
    *_test.go
```

职责划分：

* `tools/release/defs.bzl`：定义 `s3_artifacts_push` macro/rule。
* `tools/release/s3push/main.go`：CLI 入口，解析 launcher 传入参数。
* `tools/release/s3push/manifest.go`：YAML manifest 模型、枚举、版本格式和一致性校验。
* `tools/release/s3push/staging.go`：artifact 复制、重命名、size/SHA256 计算、`manifest.json` 与 `SHA256SUMS` 生成。
* `tools/release/s3push/publish.go`：固定 bucket 常量、S3 object key 构造、`pkg/s3` client 创建与上传顺序。
* `tools/release/s3push/runfiles.go`：Bazel runfiles 路径解析。

Windows agent 迁移后，删除项目级发布实现：

```text
projects/game/windows_agent/release/
```

项目侧只保留 `release.yaml` 与 `s3_artifacts_push` target。

## 关键细节

### 为什么不把上传放入 Bazel action

S3 上传是网络副作用，依赖运行时凭证和远端状态，不是可声明输出的 hermetic action。

如果在 `ctx.actions.run` 或 `genrule` 中执行上传，会带来：

* remote execution 或 cache replay 导致重复上传。
* 凭证泄露到 action env、日志或缓存元数据。
* `bazel build` / `bazel test` 意外修改外部 S3 状态。
* 构建图无法表达远端 object 的真实状态。

因此，本方案只在 `bazel run` 的 executable target 中执行上传，保持与 `oci_push` 类似的副作用边界。

### 为什么 manifest 与 artifacts 都需要 target

`artifacts` 参数用于 Bazel 分析期声明依赖，manifest 中的 `target` 用于发布语义与 Bazel target 的运行时映射校验。

不能只依赖 manifest 中的 target 自动声明 Bazel deps，原因是普通 Bazel rule 无法在分析期读取 YAML 后动态添加依赖。

不能只靠 `filename` 匹配，原因是：

* Bazel 输出文件名不一定等于最终发布文件名。
* 发布需要支持 staging 时重命名。
* 不同 target 的输出 basename 可能重复。
* target 才是构建依赖的稳定身份，filename 是发布目录中的展示身份。

因此最终规则是：

* Bazel `artifacts` 提供构建依赖集合。
* manifest `artifacts[].target` 提供发布配置映射。
* 两个 target 集合必须完全一致。
* `filename` 只决定最终上传文件名。

### 单文件 artifact 约束

每个 `artifacts` target 必须只产出一个文件。

如果 target 产出多个文件，push 工具无法无歧义地判断哪个文件对应 manifest 中的 `filename`，应直接报错。需要发布目录型产物时，应先通过上游 package rule 打包成 zip/tar 等单文件制品。

### 上传顺序

上传顺序固定为：

1. artifact 文件。
2. `manifest.json`。
3. `SHA256SUMS`。

这样可以降低下载方先看到 manifest 但 artifact 尚未上传完成的概率。当前方案不提供强事务语义；若上传中途失败，工具返回非 0，并报告失败 object。

### 覆盖语义

本方案默认不在设计中加入 overwrite 策略配置。发布路径包含不可变 `version`，开发者应避免重复发布同一版本。

实现时可先沿用 S3 `PutObject` 覆盖语义；若后续需要防误覆盖，再增加 `--fail-if-exists` 或等价能力。

## Windows agent 迁移示例

迁移后，`projects/game/windows_agent/release.yaml`：

```yaml
name: windows-agent
version: 0.1.0

artifacts:
  - target: //projects/game/windows_agent:windows_agent_win_zip
    filename: windows-agent-0.1.0-windows-amd64.zip
    platform: windows
    arch: amd64
```

`projects/game/windows_agent/BUILD.bazel` 中新增：

```starlark
load("//tools/release:defs.bzl", "s3_artifacts_push")

s3_artifacts_push(
    name = "windows_agent_push",
    manifest = "release.yaml",
    artifacts = [
        ":windows_agent_win_zip",
    ],
    visibility = ["//visibility:public"],
)
```

发布命令：

```bash
bazel run //projects/game/windows_agent:windows_agent_push
```

对应 S3 目录：

```text
s3://artifacts/windows-agent/0.1.0/
```

## 测试与验收标准

### 单元测试

需要覆盖：

* `release.yaml` 解析成功路径。
* `version` 格式校验。
* `platform` 枚举校验。
* `arch` 枚举校验。
* `name`、`version`、`artifacts` 必填校验。
* artifact `target`、`filename`、`platform`、`arch` 必填校验。
* `target` 唯一性校验。
* `filename` 唯一性校验。
* `platform + arch` 唯一性校验。
* manifest targets 与 Bazel `artifacts` targets 完全一致校验。
* 单文件 artifact 校验。
* `manifest.json` 生成内容。
* `SHA256SUMS` 包含 artifact 与 `manifest.json`，不包含自身。
* S3 bucket 固定为 `artifacts`，实现阶段可先使用占位符常量等待开发者替换。
* S3 endpoint 来自 `pkg/s3`，固定为 `s3.liukexin.com`。
* S3 client 必须通过 `pkg/s3.NewS3Client("")` 创建。
* S3 object key 为 `{name}/{version}/{filename}`。
* 上传失败时返回非 0 并包含失败 object 信息。

### Bazel 验证

迁移完成后应通过：

```bash
bazel test //tools/release/...
bazel build //projects/game/windows_agent:windows_agent_push
```

无 S3 凭证时：

* `bazel build //projects/game/windows_agent:windows_agent_push` 必须成功。
* `bazel run //projects/game/windows_agent:windows_agent_push` 必须明确报错缺少 `S3_ACCESS_KEY` 或 `S3_SECRET_KEY`，且不得上传任何文件。

有 S3 凭证时：

* `bazel run //projects/game/windows_agent:windows_agent_push` 应上传所有 artifact、`manifest.json` 和 `SHA256SUMS`。
* 上传路径必须为 `s3://artifacts/windows-agent/0.1.0/`。

## 决策详情

### 决策 1：发布入口使用 `bazel run` target

原因：

* 与 `oci_push` 的构建/发布分层一致。
* Bazel 可以先构建 artifact，再运行发布工具。
* 上传副作用不会混入普通 build/test action。
* 凭证只存在于运行时环境。

### 决策 2：保留 BUILD `artifacts` 参数

原因：

* Bazel 需要在分析期知道 publish target 依赖哪些 artifact。
* YAML manifest 是运行时文件，不能用于普通 rule 的分析期动态依赖声明。
* 显式 `artifacts` 是最简单、最稳定、最符合 Bazel 模型的方案。

### 决策 3：manifest 中也保留 `target`

原因：

* 用于校验 manifest 与 BUILD 依赖集合完全一致。
* 用于把发布元信息映射到具体 Bazel artifact。
* 避免依赖不稳定的输出 basename 或最终发布 filename。

### 决策 4：远端路径固定为 `s3://artifacts/{name}/{version}/`

原因：

* 降低不同项目发布目录约定分裂。
* 避免在 BUILD 文件或命令行中维护远端路径。
* 让 release manifest 的 `name/version` 成为发布位置的唯一来源。

### 决策 5：`platform` 与 `arch` 使用枚举

原因：

* 避免 `x64`、`amd64`、`win64` 等自由文本混用。
* 便于后续下载方按平台和架构查找制品。
* 便于测试和 schema 校验。

### 决策 6：不允许同一 `platform + arch` 多产物

原因：

* 当前 release manifest 没有 `kind` 字段，无法区分 zip、installer、tar 等同平台多形态制品。
* 禁止重复可以保持下载方选择逻辑简单。
* 后续若确有需求，应通过新增模型字段单独设计，而不是放宽当前约束。

## 待定项

无。

本方案已明确：

* BUILD 保留 `artifacts` 参数。
* manifest 保留 `target` 字段。
* `version` 固定为 `x.y.z`。
* `platform` 与 `arch` 使用枚举。
* 不允许同一 `platform + arch` 多产物。
* 远端路径固定为 `s3://artifacts/{name}/{version}/`。
* S3 endpoint 固定为 `s3.liukexin.com`。
* S3 bucket 固定为代码内常量，正式值为 `artifacts`，实现初期允许使用占位符等待替换。
* S3 client 使用 `pkg/s3` 创建。
* S3 上传只在 `bazel run` 中执行。

## 后续优化

以下能力不阻塞本方案落地，可在后续版本评估：

* 增加 `kind` 字段，支持同一 `platform + arch` 下发布 zip、installer、tar 等多形态产物。
* 增加 `--dry-run`，输出将要上传的 object 列表但不执行上传。
* 增加 `--fail-if-exists`，防止重复发布同一版本覆盖已有 object。
* 增加 latest 指针或 channel 机制，例如 stable、beta、nightly。
* 增加发布后 HEAD 校验，确认远端 object size 与 checksum 符合预期。
