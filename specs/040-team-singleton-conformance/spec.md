# Feature Specification: Team 单例 AIP-156 一致化

**Feature Branch**: `040-team-singleton-conformance`

**Created**: 2026-08-07

**Status**: Draft

**Input**: User description: "将 Team 修改为符合规范的（AIP-156）单例——移除 CreateTeam，改为经 UpdateTeam(allow_missing=true) 物化；profile 落到 Team 资源体并支持变更（重建 graph）；消除现有对 AIP-133 的偏离。"

**Context**: 本特性 supersede [`specs/031-team-template-mode/spec.md`](../031-team-template-mode/spec.md) FR-033（Team 经 `CreateTeam` 显式创建）。调研结论：`CreateTeam` 违反 [AIP-156](https://google.aip.dev/156)（单例 MUST NOT 定义 Create；should Get+Update）；且 Team 并非真正单例——单例须"因其父存在而始终存在"，而 Team 在 Session 存在时尚不存在。跨服务父子（Session 属 SessionService，Team 属 TeamService）下，单例由其自身服务在首次 `Update` 时物化，父服务不参与创建（实践范例：[Access Approval Settings](https://github.com/googleapis/googleapis/blob/master/google/cloud/accessapproval/v1/accessapproval.proto)，Get+Update、无 Create）。机制依据 [AIP-134 create-or-update](https://google.aip.dev/134#create-or-update)（`allow_missing` upsert）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 符合规范的单例生命周期 (Priority: P1)

API 消费者（桌面客户端、API 开发者）面对的 Team 资源不再暴露非标准的"对单例做 Create"。Team 作为 [AIP-156](https://google.aip.dev/156) 单例：无 Create/Delete，经 `UpdateTeam(allow_missing=true)` 物化，提供 Get。这是本特性的核心一致性目标，也是其它行为变更的前提。

**Why this priority**: 消除 API 契约对 AIP-156 的违反，使 Team 成为名副其实的单例；单例 pattern 末段保持单数静态段 `team`（AIP-156：资源 pattern 用单数，仅 List 集合路径用复数，而 Team 无 List）。

**Independent Test**: 对一个新会话调用 `UpdateTeam(allow_missing=true)`，断言 Team 被物化且可被 `GetTeam` 读回；断言 API 表面无 `CreateTeam`。

**Acceptance Scenarios**:

1. **Given** 一个已存在的 Session 且 Team 尚未物化，**When** API 消费者调用 `UpdateTeam(allow_missing=true, profile=P)`，**Then** Team 被创建，响应携带该 Team（含 profile=P）。
2. **Given** 已物化 Team 的会话，**When** 调用 `GetTeam`，**Then** 返回该 Team（含其当前 profile）。
3. **Given** 一个尚未物化 Team 的会话，**When** 调用 `GetTeam`，**Then** 返回 NOT_FOUND（无隐式/懒加载创建）。

---

### User Story 2 - 幂等配置（多标签并发安全） (Priority: P2)

桌面端多标签页并发配置同一会话 Team 时，无需依赖自定义"同 profile 幂等"规则——单次 `UpdateTeam(allow_missing=true)` 天然幂等：缺失则创建、存在且 profile 相同则原样返回。这同时移除 [`specs/031-team-template-mode/contracts/api-contract.md`](../031-team-template-mode/contracts/api-contract.md) 中对 AIP-133 的偏离（"不同 profile 返回 ALREADY_EXISTS"的特殊逻辑）。

**Why this priority**: 让配置流程更稳健、移除自创偏离；但它是建立在 P1 单例化之上的体验优化。

**Independent Test**: 对同一会话连续两次 `UpdateTeam(allow_missing=true, profile=P)`，断言第二次成功返回既有 Team（非错误）；并发场景下 owner 分配竞态收敛于胜者。

**Acceptance Scenarios**:

1. **Given** 已以 profile=P 物化的 Team，**When** 再次 `UpdateTeam(allow_missing=true, profile=P)`，**Then** 成功返回既有 Team（幂等，无错误）。
2. **Given** 两个并发 `UpdateTeam(allow_missing=true)` 针对同一未物化会话，**When** 二者完成，**Then** 二者均成功，Team 物化一次，owner 分配收敛于同一 agent 实例。
3. **Given** 任意已物化或未物化的 Team，**When** 调用配置流程，**Then** 不再出现"不同 profile → ALREADY_EXISTS"的旧行为（该偏离被移除）。

---

### User Story 3 - 可变更的 Team profile (Priority: P3)

Team 物化后，其 profile（player/planner 模型与 base prompt）可经 `UpdateTeam` 变更；变更在下一个 turn 生效（重建 team graph），且保留会话的对话与游戏状态。变更在 turn 进行中时被拒绝。

**Why this priority**: profile 可变性是 `Update` 单例自然衍生的能力，有价值但属增量，且伴随 graph 重建的复杂度与风险。

**Independent Test**: 物化 Team 后，以不同 profile 调用 `UpdateTeam`，断言后续 turn 使用新 profile 且历史消息不丢失。

**Acceptance Scenarios**:

1. **Given** 已以 profile=P1 物化的 Team，**When** 调用 `UpdateTeam(profile=P2)`（无 turn 进行），**Then** Team 的 profile 变为 P2，且会话既有对话/游戏状态被保留。
2. **Given** Team 的一个 turn 正在进行，**When** 调用 `UpdateTeam(profile=P2)`，**Then** 返回 FAILED_PRECONDITION，既有 Team 与进行中的 turn 不受影响。
3. **Given** profile 变更后的 Team，**When** 发起下一个 turn，**Then** 该 turn 使用 P2 对应的模型与 prompt。

---

### Edge Cases

- **profile 模板段与会话模板段不一致**：`UpdateTeam` 的 profile 其 template 路径段与 `team.name` 的 template 段不一致 → INVALID_ARGUMENT。
- **profile 引用不存在的 TeamProfile**：底层 PromptService 返回 NOT_FOUND → 透传给调用方。
- **allow_missing=false 对未物化 Team 调用 Update**：返回 NOT_FOUND（标准 [AIP-134](https://google.aip.dev/134) 语义）。
- **profile 变更重建失败**（如新模型不可用）：既有 Team 保持不变，返回错误，不留下半重建状态。
- **会话删除后**：Team 随父级失效（单例生命周期与父绑定；本特性不引入 Team 的独立 Delete）。
- **initInstruction 触发时机**（关联 spec 039，尚未实现）：触发点随 graph 首建移动到 `UpdateTeam` 物化路径；profile 变更重建不重跑 init。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001** (supersede 031 FR-003/FR-033): Team MUST 作为 [AIP-156](https://google.aip.dev/156) 单例：MUST NOT 暴露 `CreateTeam` 标准方法；MUST 经 `UpdateTeam(allow_missing=true)` 物化；MUST 提供 `GetTeam`。Team 资源名 pattern 保持 `templates/{template}/sessions/{session}/team`（单数静态段，AIP-156 合规）。
- **FR-002**: `UpdateTeam(allow_missing=true)` 对未物化 Team MUST 创建之（物化）；对已物化且 profile 相同的 Team MUST 幂等返回既有 Team（无错误）。
- **FR-003**: `GetTeam` 对未物化 Team 的会话 MUST 返回 NOT_FOUND；系统 MUST NOT 在 `GetTeam`/`Connect`/`ListMessages`/`RefreshTeam` 上提供隐式/懒加载物化。
- **FR-004**: Team 资源体 MUST 暴露 `profile` 字段（其当前所基于的 TeamProfile 全名）；`GetTeam` 与 `UpdateTeam` 响应 MUST 携带当前 profile。
- **FR-005**: `UpdateTeam` 对已物化 Team 且 profile 不同时 MUST 重建 team graph 使新 profile 在下一 turn 生效；重建 MUST 保留会话既有对话与游戏状态；重建 MUST NOT 丢失或重复历史。
- **FR-006**: 当 Team 的一个 turn 正在进行时，`UpdateTeam`（profile 变更类）MUST 返回 FAILED_PRECONDITION，且 MUST NOT 影响既有 Team 与进行中的 turn。
- **FR-007**: 系统 MUST 移除 031 契约中"不同 profile → ALREADY_EXISTS"的偏离及相关特殊幂等逻辑；Team 配置流程 MUST NOT 对调用方产生 ALREADY_EXISTS（owner 分配竞态由底层 get-or-create 收敛，不外泄）。
- **FR-008**: `UpdateTeam` MUST 校验 profile 的 template 路径段与 `team.name` 的 template 段一致，不一致 → INVALID_ARGUMENT。
- **FR-009**: `UpdateTeam` 在 profile 引用不存在的 TeamProfile 时 MUST 返回 NOT_FOUND（透传底层 PromptService 错误）。

### Key Entities *(include if feature involves data)*

- **Team**（单例资源，`templates/{template}/sessions/{session}/team`）：会话的执行主体；新增 `profile` 属性（指向 TeamProfile 全名，可变更）；其 `agents`（player/planner 等）由模板 schema 派生，OUTPUT_ONLY。父资源 Session 属另一服务（SessionService）；跨服务关系靠资源名 + resource_reference 表达，无生命周期耦合——父服务不参与 Team 物化。
- **TeamProfile**（`templates/{template}/profiles/{profile}`）：被 Team.profile 引用的配置；经 PromptService 管理（不在本特性范围内变更）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Team 资源的公开方法集符合 [AIP-156](https://google.aip.dev/156)——不存在 Create/Delete，存在 Get 与 Update（含 allow_missing）——经契约审查可验证。
- **SC-002**: 所有既有的 Team 配置 / 进入会话端到端流程（单标签进入、多标签并发进入、发送消息）在变更后行为等价（"更优"仅指多标签并发场景由 `allow_missing` 幂等收敛取代原"失败后 GetTeam 兜底重读"，验收底线为行为等价），全部大型测试用例通过。
- **SC-003**: profile 变更后，下一 turn 使用新 profile 且会话历史消息零丢失（经大型测试用例验证：变更前后的消息计数与内容一致）。
- **SC-004**: API 表面不再存在对 [AIP-133](https://google.aip.dev/133) 的自定义偏离（无可触发 ALREADY_EXISTS 的 Team 配置路径）——经契约审查可验证。

## Assumptions

- Team 仍由 TeamService 管理；其父 Session 由 SessionService 管理（跨服务单例）。父服务不参与 Team 物化——这是 [AIP-156](https://google.aip.dev/156) "随父隐式创建"在跨服务场景下的正确落地（单例在自身服务首次 Update 时物化）。
- profile 变更的 graph 重建通过复用既有 checkpointer（按会话 thread 键）保留状态——属 HOW（plan/tasks 阶段细化与验证），此处仅作为可行前提假设记录。
- spec [`039-planner-memory-calibration`](../039-planner-memory-calibration/)（尚未实现）的 initInstruction 触发点将由"CreateTeam"改为"UpdateTeam 物化路径（graph 首建）"；profile 变更重建时不重跑 init。本特性仅更新 039 的相关文档，不实现 039。
- 本特性不引入 Team 的独立 Delete；Team 生命周期随父 Session（既有不变量）。
- 既有 `Connect`/`ListMessages`/`RefreshTeam` 的 NOT_FOUND 不变量与 owner 分配（get-or-create + 竞态重读）语义保持不变。
