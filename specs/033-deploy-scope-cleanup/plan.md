# Implementation Plan: Deploy Scope Removal

**Branch**: `033-deploy-scope-cleanup` | **Date**: 2026-08-03 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/033-deploy-scope-cleanup/spec.md`

## Summary

移除 deploy CLI 工具中 scope 作为独立设计对象的概念。scope 仅保留为 `{scope}.{env_name}` 环境名格式的前缀部分。具体包括：(1) 删除 `scope` 命令和本地 `.env/cli.json` 配置逻辑；(2) 从 apply/del/describe 命令中移除 `--scope` 标志，这些命令改为直接使用完整环境名；(3) `list` 保留 `--scope` 作为可选过滤参数，不指定时通过 AIP-159 `-` 通配符列出所有 scope 的环境；(4) 后端 deploy service 扩展支持 `-` 作为 `ListEnvironments` parent scope 的通配符值。

## Technical Context

**Language/Version**: Go 1.26.2（见 `go.mod`；deploy CLI: `tools/release/deploy/v3/`；deploy service: `projects/infra/deploy/`）

**Primary Dependencies**: `github.com/spf13/pflag`（CLI 标志解析）、`google.golang.org/protobuf`（proto 序列化）、`go.mongodb.org/mongo-driver`（deploy service 存储）、grpc-gateway（HTTP transcoding，无需修改）

**Storage**: MongoDB（deploy service 端，`projects/infra/deploy/storage/mongo.go`）。CLI 端无存储（移除 `.env/cli.json`）。

**Testing**: Go 标准测试框架 `testing` + `bazel test`。CLI 测试在 `tools/release/deploy/v3/*_test.go`；service 测试在 `projects/infra/deploy/*_test.go`。

**Target Platform**: Linux（CLI 二进制 `deploy_v3`，通过 `bazel run //:deploy_install` 安装）

**Project Type**: CLI 工具（`tools/release/deploy/v3/`）+ 后端 gRPC 服务（`projects/infra/deploy/`）

**Performance Goals**: 无新增性能目标。list 跨 scope 查询复用现有 MongoDB 索引（按 `name` 字段排序）。

**Constraints**: proto HTTP 注解不得修改（FR-015）；deploy.yaml schema 不得修改（已强制 `{scope}.{env_name}` 格式）；`{{run}}` 占位符机制不变。

**Scale/Scope**: CLI 工具 13 个文件（v3 目录，含 2 个删除）；后端涉及 handler.go、storage/mongo.go、repository_fake_test.go 及另外 3 个 fake repository（仅注释一致性检查，见 T004）；domain 层逻辑无需修改（仅 `domain/repository.go` 接口注释更新）。共约 22 个文件变更（含测试；其中 2 个删除、2 个仅注释检查）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 状态 | 说明 |
|------|------|------|
| I. 引用溯源 | ✅ 通过 | spec 中所有代码引用使用仓库相对路径（如 `handler.go:777-789`），外部引用使用完整 URL（AIP-159 等）。plan 和 artifacts 延续此规范。 |
| II. 重构式变更 | ✅ 通过 | 本变更正是"现有架构相对新需求过度设计"的典型案例——scope 作为独立概念是不必要的抽象，需简化收缩。移除 scope 逻辑、简化为完整环境名符合原则 II 的"简化、收缩 scope 以保持简洁"。后端 `-` 通配符是必要的扩展（满足跨 scope list 需求）。 |
| III. 接口优先设计 | ✅ 通过 | 后端 API 契约（proto + HTTP 注解）已定义且不修改。CLI 命令接口变更在 `contracts/deploy-cli.md` 中明确定义。AIP-159 通配符模式是标准接口设计。 |
| IV. 测试颗粒度 | ✅ 通过 | 编译 + 单测作为代码变更的一部分执行，不单列 task。无大型测试需求（CLI 工具 + service 单测覆盖）。deploy service 是否需要大型测试：deploy service 是已有服务，本变更仅为 `ListEnvironments` 增加通配符支持，属于增量功能扩展，单测可充分验证。 |
| V. 编码前阅读文档 | ✅ 通过 | 将在 tasks.md 中为每个 phase 显式声明文档清单（style/golang.md、style/api.md、AIP-159 等）。 |
| VI. 大型测试验收 | ✅ 豁免 | deploy service 是后端服务，本变更为 API 增量扩展（`-` 通配符）。deploy service 当前无大型测试（testplan 目录无 deploy 相关计划）。CLI 工具为纯客户端工具，无需大型测试。单测充分覆盖行为正确性。 |

## Project Structure

### Documentation (this feature)

```text
specs/033-deploy-scope-cleanup/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── deploy-cli.md    # CLI command contracts
├── checklists/
│   └── requirements.md  # Spec quality checklist
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
tools/release/deploy/v3/
├── main.go          # 命令注册、flag 定义、parseOptions、usageText（移除 scope 命令/标志）
├── identity.go      # 环境名校验函数（简化：移除 NewFullEnvName/ValidateScope，保留 ParseFullEnvName/ValidateFullEnvName）
├── scope.go         # 删除整个文件（cliConfig、loadConfig、saveConfig、scopeCommand）
├── apply.go         # applyCommand（移除 scope 组合逻辑，直接使用 deploy.yaml name）
├── del.go           # delCommand（移除 config 加载和 scope 组合，直接校验完整名）
├── describe.go      # describeCommand（同 del）
├── list.go          # listCommand（移除 config 加载，scope 改为可选过滤，不指定时用 "-"）
└── *_test.go        # 对应测试文件

projects/infra/deploy/
├── handler.go               # parseParent 函数（特殊处理 "-" scope）
├── handler_test.go          # 新增 "-" 通配符测试用例；errorRepository.ListByScope 注释一致性检查（T004）
├── storage/mongo.go         # ListByScope（支持 "-" 时空过滤）
├── storage/mongo_test.go    # 新增 "-" 通配符测试用例
├── repository_fake_test.go  # fakeRepository.ListByScope（支持 "-" 时返回所有）
├── service/command_test.go  # fakeCommandRepository.ListByScope 注释一致性检查（T004）
├── service/reconcile_test.go# fakeReconcileRepository.ListByScope 注释一致性检查（T004）
├── domain/repository.go     # ListByScope 注释更新（记载 `-` 通配语义）
└── domain/environment_name.go  # 无需修改（仅 ListEnvironments 路径绕过校验）

tools/release/deploy/README.md  # 文档更新
```

**Structure Decision**: 现有代码结构不变——本变更是对现有文件的修改和删除，不新增文件或目录。

## Complexity Tracking

> 无 Constitution Check 违规，无需填表。
