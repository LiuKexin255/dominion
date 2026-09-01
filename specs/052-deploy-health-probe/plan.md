# Implementation Plan: Deploy Health 探针支持

**Branch**: `052-deploy-health-probe` | **Date**: 2026-09-01 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/052-deploy-health-probe/spec.md`

## Summary

为 deploy 工具生成的服务工作负载（Deployment/StatefulSet）无条件附加指向固定约定端点（端口 38080、路径 /healthz）的 startupProbe（10s×30=300s 启动预算）与 livenessProbe（10s×3 判死）；就绪判定由 k8s 原生探针机制接管，deploy 侧 rollout/检查逻辑零修改。health 服务由 bootstrap 体系实现：Go 侧作为 `common/gopkg/bootstrap` `RunSignal` 的核心生命周期步骤（全部组件启动后启动、关闭序列首位停止、绑定失败即回滚退出）；JS 侧不交付共享库（统一 JS bootstrap 公共库为后续独立工作），用于验证的 `experimental/` JS 服务按同一约定自行实现 health 端点。验证载体为 `experimental/grpc_chain`（双语言就绪路径）与 `experimental/ts/grpc_hello_world`（定时停 health 的自愈路径），经 `guitar run` 大型测试闭环验收。

技术决策与备选方案见 [research.md](research.md)；契约见 [contracts/deploy-probe.md](contracts/deploy-probe.md)、[contracts/bootstrap-health.md](contracts/bootstrap-health.md)、[contracts/verification-testplan.md](contracts/verification-testplan.md)；实体见 [data-model.md](data-model.md)；验证步骤见 [quickstart.md](quickstart.md)。

## Technical Context

**Language/Version**: Go（仓库 module `dominion`）+ TypeScript（pnpm workspace，版本统一于 `pnpm-workspace.yaml` catalog）

**Primary Dependencies**: `k8s.io/api` corev1/appsv1（deploy 服务既有）；Go `net/http`（bootstrap health）；JS `node:http`（experimental 服务自行实现 health）

**Storage**: N/A（无持久化数据）

**Testing**: Go 单测（`bazel test`）；JS vitest（协同于包内）；大型测试 testplan skill `guitar run`（宪法 VI）

**Target Platform**: Linux 容器（distroless Go/Node 镜像），k8s 集群部署

**Project Type**: 平台工具链（deploy 服务）+ 共享库（Go bootstrap health）

**Performance Goals**: health 端点响应 < 10ms（极小 handler）；启动预算 300s；liveness 判死 ≈30s；自愈恢复分钟级（SC-003）

**Constraints**: 端口/路径固定约定、零校验代码（FR-008/SC-006）；deploy 检查逻辑零修改（FR-003/SC-005）；`experimental/` 之外服务代码零修改（FR-009）

**Scale/Scope**: 改动面 4 处代码（builder、Go bootstrap、两个 experimental JS 服务的 bootstrap.ts）+ 测试 + 文档

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 检查 | 状态 |
|------|------|------|
| I 引用溯源 | research/contracts/quickstart 全部引用带仓库相对路径或完整 URL | ✅ |
| II 重构式变更 | health 作为 bootstrap 编排核心步骤而非补丁（结构性保证顺序，research.md D5）；无堆叠式补丁 | ✅ |
| III 接口优先设计 | 契约先行：`contracts/deploy-probe.md`（生成契约）、`contracts/bootstrap-health.md`（Go 生命周期序列 + JS 自行实现约定）、`contracts/verification-testplan.md` | ✅ |
| IV 测试颗粒度 | 单测随代码变更（builder/bootstrap/health 包）；大型测试独立验收（guitar run，全部通过） | ✅ |
| V 编码前阅读文档 | tasks 阶段按三分类清单显式列出（见 tasks-template 要求） | ⏳ tasks 阶段落实 |
| VI 大型测试验收 | `contracts/verification-testplan.md` 定义两个 guitar plan（就绪 + 自愈），实际执行 `guitar run` 全 case 通过；不以 build 替代 | ✅ |
| VII 终态表述 | spec 已清除迭代痕迹（适配范围两次决策均以终态表述 + Clarifications 记录）；plan/contracts 只述终态 | ✅ |

**Phase 1 复查（设计后）**：设计未引入超出 spec 范围的配置面或校验；接口契约先于实现；无原则违规需要 Complexity Tracking 记录。✅ 通过

## Project Structure

### Documentation (this feature)

```text
specs/052-deploy-health-probe/
├── plan.md                        # 本文件
├── spec.md                        # 需求（含 Clarifications）
├── research.md                    # Phase 0：决策 D1–D10
├── data-model.md                  # Phase 1：契约级实体
├── quickstart.md                  # Phase 1：验证指南
├── contracts/
│   ├── deploy-probe.md            # deploy 生成契约（参数/零修改边界/校验禁止）
│   ├── bootstrap-health.md        # Go/JS health 行为契约
│   └── verification-testplan.md   # 大型测试设计
└── tasks.md                       # Phase 2（/speckit.tasks 生成）
```

### Source Code (repository root)

```text
projects/infra/deploy/runtime/k8s/
├── builder.go                     # BuildDeployment/BuildStatefulSet 容器附加探针（D1/D2/D3）
└── builder_test.go                # 探针字段与参数断言（SC-001）

common/gopkg/bootstrap/
├── bootstrap.go                   # RunSignal 编排：组件启动后 start health；关闭首位 stop health（D5）
├── health.go                      # 新文件：health 服务器（:38080, /healthz → 200）
└── bootstrap_test.go / health_test.go  # 顺序/端点/失败回滚单测（SC-004/FR-010）

experimental/grpc_chain/mid/src/bootstrap.ts          # 自行实现 health 端点（FR-009 允许）
experimental/ts/grpc_hello_world/
├── src/bootstrap.ts               # 自行实现 health 端点 + test-only HEALTH_STOP_AFTER_MS（D7）
└── testplan/                      # 自愈 deploy yaml + 新 case binary + plan yaml

tools/release/deploy/README.md     # 新增"健康探针约定"章节（D8）
```

**Structure Decision**: 改动收敛于既有分层：deploy 生成侧（k8s builder）、Go bootstrap 核心、experimental 验证载体。不新增 proto/schema/config 面（D1），不新增 JS 公共包（D6：统一 JS bootstrap 公共库为后续独立工作）。

## Complexity Tracking

> 无宪法违规需要论证；无表项。
