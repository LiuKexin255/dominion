# Specification Quality Checklist: Deploy Health 探针支持

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-01
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
  - Note: k8s 探针（startupProbe/livenessProbe）、端口 38080、路径 /healthz 属于需求方明确给出的**契约级约定**（本特性即为该契约的落地），不涉及具体代码结构、语言 API 或实现方案。
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
  - Note: 本特性的利益相关方为平台/服务开发者；已按其可理解的契约与行为语义撰写。
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
  - Pass: FR-009 的适配范围已按最终决策落实：仅 `experimental/` 服务允许在本特性内适配，`experimental/` 之外的现存服务代码零修改（JS 生产服务 agent/agent_v2 不接入）；新增 SC-007 度量。
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
  - Note: SC-001 提及端口/探针名，为契约内容本身；判据本身（清单命中、READY 时序、重启行为、停止顺序）均可脱离实现验证。
- [x] All acceptance scenarios are defined
- [x] Edge cases identified
  - Note: 含未适配服务（未来新增）、慢响应、回滚、端口占用、异常退出五类边界。
- [x] Scope is clearly bounded
  - Note: 基础设施组件（Mongo）、deploy 自身清单、readinessProbe 均已在 Assumptions 中显式排除；适配范围限定于 `experimental/`（FR-009）。
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- Items marked incomplete require spec updates before `/speckit.clarify` or `/speckit.plan`
- 澄清记录（最终决策）：适配范围仅限 `experimental/`，不修改现存 service 代码；JS 生产服务 agent/agent_v2 不接入（接受 `system_test.yaml` 部署失败为已知范围外后果）；JS 统一 bootstrap 公共库为后续独立工作。已落实至 FR-006/FR-009、Edge Cases、SC-007 与 Assumptions（含对 `specs/050-vite-react-bazel/contracts/static-server-deploy.md` 既有"无探针"决策的取代说明）。
- 调研结论（仓库现状）：deploy 生成的服务工作负载当前无任何探针（仅 Mongo 基础设施有 TCP 探针）；端口 38080 与 /healthz 在仓库中无占用；Go 侧存在共享 bootstrap（`common/gopkg/bootstrap`，Component 生命周期 + 逆序停止）；JS 侧无共享 bootstrap，为各服务自带 `bootstrap.ts` 模式；使用共享 bootstrap 的 Go 服务（含自建 /health 端点的 fake-llm 与 050 静态文件服务的 server）经 bootstrap health 自动获得约定端点，无需逐个适配。
