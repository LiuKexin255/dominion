# Implementation Plan: Deploy Secret Configuration

**Branch**: `002-deploy-secret-config` | **Date**: 2026-06-02 | **Spec**: `specs/002-deploy-secret-config/spec.md`

**Input**: Feature specification from `/specs/002-deploy-secret-config/spec.md`

## Summary

为 deploy 工具增加 secret 配置支持：在 `service.yaml` 的 artifact 中声明所需 secret 逻辑名，在 `deploy.yaml` 的 artifact 条目中将逻辑名绑定到 Kubernetes Secret + key，deploy 工具校验绑定完整性后将绑定信息写入期望状态，控制面在 reconciliation 时为容器注入 `/mnt/dominion/secret/{name}` 文件挂载和 `DOMINION_SECRET_DIR` 环境变量。

## Technical Context

**Language/Version**: Go 1.x（通过 rules_go / Bazel 构建）

**Primary Dependencies**:
- `github.com/goccy/go-yaml` — YAML 解析
- `github.com/santhosh-tekuri/jsonschema/v6` — JSON Schema 校验
- `k8s.io/api` — Kubernetes API 类型（corev1, appsv1）
- `google.golang.org/protobuf` — protobuf 消息

**Storage**: N/A（无持久化存储，secret 绑定存在于 Environment 期望状态中）

**Testing**: Go 单元测试（`go_test` / Gazelle 规则）；deploy service README 声明不做大型测试

**Target Platform**: Linux server / Kubernetes 集群

**Project Type**: CLI 工具（deploy CLI）+ gRPC 控制面服务（deploy service）

**Performance Goals**: 无额外性能要求；校验和编译属于 O(n) 操作

**Constraints**: 向后兼容 — 不声明 secrets 的 artifact 行为不变；保留环境变量 `DOMINION_SECRET_DIR` 不可被用户覆盖

**Scale/Scope**: 每个服务的 secret 数量预期 ≤ 20；绑定校验在 CLI 端同步完成

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Plan identifies applicable files: `tools/release/deploy/README.md`（部署工具文档），`projects/infra/deploy/README.md`（服务文档），`style/golang.md`。所有实现者必须读取 `style/golang.md` 后再修改代码。
- **Bazel Integrity**: PASS. Go 构建通过 `rules_go`；proto 编译通过 Bazel protobuf 规则；`BUILD.bazel` 通过 Gazelle 生成。不引入新依赖。
- **Generated Files & Dependencies**: PASS. Proto 源文件修改后 `bazel build //...` 会重新生成 Go 代码；不手动提交生成的 proto 代码。无新增外部依赖。
- **Testing Strategy**: PASS. 采用 TDD：先为 schema、config、compiler、builder 编写失败测试，再实现功能。deploy service README 明确声明不进行大型测试，因此仅执行单元测试。
- **Behavioral Acceptance**: PASS. 验证标准包括：1) 无 secrets 声明的 artifact 行为不变；2) 声明 secrets 但绑定不完整时 CLI 报错终止；3) secret 文件挂载路径正确；4) `DOMINION_SECRET_DIR` 环境变量正确注入。
- **Review Scope**: PASS. 计划包含代码风格审查（`style/golang.md`）和测试代码审查。
- **Repository Verification**: PASS. 最终验证包含 `bazel build //...` 和 `bazel test //...`。
- **Testplan Execution**: N/A. 无现有大型测试计划与本次变更相关；deploy service 明确声明不做大型测试。

## Project Structure

### Documentation (this feature)

```text
specs/002-deploy-secret-config/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
# Schema 校验层
tools/release/deploy/pkg/schema/
├── service.schema.json  # 添加 secrets 数组字段
├── deploy.schema.json   # 添加 secrets 映射字段
└── schema.go            # 无变更

# 配置解析层
tools/release/deploy/pkg/config/
├── config.go            # 添加 Secrets 字段和 Secrets map (YAML: secrets) + 校验
├── config_test.go       # 新增 secret 相关测试用例
├── v3.go                # 无变更
└── testdata/            # 新增 secret 相关 testdata 文件

# 编译层
tools/release/deploy/v2/compiler/
├── compiler.go          # 传递 secret bindings、校验完整性
└── compiler_test.go     # 新增 secret 编译测试

# Proto 定义
projects/infra/deploy/
├── deploy.proto         # 添加 SecretBinding message + ArtifactSpec 字段
└── ... (generated Go 代码由 Bazel 重新生成)

# 领域模型
projects/infra/deploy/domain/
├── spec.go              # 添加 SecretBinding 类型 + 校验
└── spec_test.go         # 新增 secret 校验测试

# K8s 运行时
projects/infra/deploy/runtime/k8s/
├── converter.go         # 传递 secret bindings 到 workload
├── converter_test.go    # 新增转换测试
├── builder.go           # 构建 projected volume + env var
├── builder_test.go      # 新增构建测试
└── model.go             # 无变更（SecretBindings 可直接附加到 DeploymentWorkload）

# 文档
tools/release/deploy/README.md  # 更新 scheme 文档
```

**Structure Decision**: 遵循现有分层结构：schema → config → compiler → proto/domain → converter → builder。每个层次在现有文件中添加字段和逻辑，不创建新文件。

## Complexity Tracking

> No Constitution violations to justify.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| (none)    |            |                                      |
