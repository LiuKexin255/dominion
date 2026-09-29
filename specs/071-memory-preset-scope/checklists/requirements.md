# Specification Quality Checklist: memory 作用域从 session 迁移至 preset、web memory 管理与侧栏二级导航

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-28（同日随增补需求修订）
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- 本特性的需求主体即资源层级（memory 的挂载点与关联关系）与 web 管理面的信息架构，因此资源名形态（`templates/{template}/presets/{preset}/memories/{memory}`）、RPC 寻址语义与侧栏两级导航结构属于 WHAT 层面的接口/产品契约（用户输入本身以 game.proto 资源结构与侧栏 tab 组织为诉求对象），非实现细节；与仓库先例 specs/069-wiki-template-web/spec.md 的书写口径一致。规格正文未出现代码文件路径、模块名、语言/存储实现等 HOW 层信息（输入锚点仅保留在头部 Input 引用中）。
- 2026-09-28 增补需求（web memory 管理页、侧栏二级导航、wiki 页面场景化文案清理）已落为 US4/US5/US6 与 FR-014~FR-017、SC-005~SC-007，并经 Clarifications（Session 2026-09-28）记录裁断；增补的模糊项均有合理默认（memory id 表单侧提供、二级菜单交互形态与组名为 plan 层设计项、场景无关化范围限 wiki/memory 两视图的用户可见文案），记录于 Assumptions，未产生 [NEEDS CLARIFICATION] 标记。
- 规格自洽性（终态）：Assumptions 称 web memory 视图为 US4 新增交付面、其余 web 视图不新增能力；Key Entities 的 Wiki 为"作用域不变、仅修正 web 管理页文案（US6）"；US5 mermaid 图的一级入口与 US5/FR-015 的两级结构表述一致（`一级入口：知识（固定按钮）`，入口文案已在 plan.md 定为「知识」）。
- 审计中确认的一致性要点：FR-002 唯一性作用域与 US1 场景 2 的三元组表述一致（含 id 本身的唯一约束）；FR-005/FR-006 与 US2 场景 1-3 对应；FR-008/FR-009/FR-010 与 US3 场景 1-4 对应；FR-014 与 US4 场景 1-5 对应；FR-015 与 US5 场景 1-4 对应；FR-016/FR-017 与 US6 场景 1-3 对应且与 Edge Cases 的 preset 选择器/无搜索框条目呼应；US4 场景 5 与 FR-016 在空态文案上交叉引用一致。
- Mermaid 图（US1、US5）已按宪章原则 VIII 使用标准 Mermaid 代码块，语法可渲染。
