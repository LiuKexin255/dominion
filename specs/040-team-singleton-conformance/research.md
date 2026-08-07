# Research: Team 单例 AIP-156 一致化

**Feature**: `040-team-singleton-conformance` | **Spec**: [spec.md](spec.md) | **Date**: 2026-08-07

> 本文汇总本特性的设计决策。多数决策已在 `/speckit.specify` 前的调研/方案讨论中与用户敲定，此处固化结论、依据与备选，供 plan/tasks 引用。

---

## R1: Team 当前是否为 AIP-156 单例？为何不合规？

**Decision**: Team **不是**真正的 AIP-156 单例；本次将其改造为合规单例。

**Rationale**:
- [AIP-156](https://google.aip.dev/156)：单例"MUST NOT 定义 Create 或 Delete 标准方法"；"must always exist by virtue of the existence of its parent"；should 定义 Get + Update。
- 现状 `projects/game/game.proto:71` 的 `CreateTeam`（AIP-133 Create）违反"MUST NOT Create"。且 Team 在 Session 存在时并不存在（须显式创建），违反"因其父存在而始终存在"。
- 故现状是"带显式创建的固定名资源"，名不副实。

**Alternatives considered**:
- 保留 CreateTeam 但改称"非单例固定名资源"：诚实但 `POST .../team`（对单例路径做 Create）结构上仍非标准，且保留自创幂等偏离（见 R6）。
- 不予处理：持续违反 AIP-156，技术债累积。

---

## R2: 跨服务父子（Session≠Team 服务）时，单例如何创建？

**Decision**: **父服务不参与创建**。单例由自身服务（TeamService）在首次 `UpdateTeam(allow_missing=true)` 时物化。跨服务关系仅靠"资源名内嵌父路径 + `resource_reference`"表达，无生命周期耦合。

**Rationale**:
- AIP-156 的"随父隐式创建"是同服务内的理想化。跨服务时无法成立——SessionService 创建 Session 时对 Team 及其 profile 一无所知。
- 真实做法：单例在自身服务首次写操作（Update）时惰性物化。父存在性在写时校验。
- 实践范例：[Access Approval Settings](https://github.com/googleapis/googleapis/blob/master/google/cloud/accessapproval/v1/accessapproval.proto)——父 `projects/{project}` 属 Cloud Resource Manager（另一服务），settings 由 AccessApproval 服务用 Get+Update（**无 Create**）管理，首次 Update 即创建。

**Alternatives considered**:
- 让 SessionService 跨服务发事件触发 Team 创建（TeamService 订阅）：异步耦合两服务 + Team 需 profile 而 SessionService 没有 → 反模式，否决。
- 在 TeamService 的 Session 创建 hook：Session 不属本服务，无 hook 可挂。

---

## R3: 单例物化的具体机制？

**Decision**: [AIP-134 create-or-update](https://google.aip.dev/134#create-or-update) 的 `allow_missing`。`UpdateTeam(allow_missing=true)` 不存在则创建、存在则更新；天然幂等。

**Rationale**:
- AIP-134 官方 upsert 机制：`allow_missing=true` 时"resource 不存在 → 创建（忽略 mask，全字段应用）；存在且字段匹配 → 原样返回；存在 → 按 mask 更新"。
- 这正是单例物化的标准姿势，且幂等内建（见 R6 消除偏离）。

**Alternatives considered**:
- 自定义 `:provision`（AIP-136）：绕过 AIP-156"无 Create"约束，但本质还是创建，属命名技巧；不如直接用标准 Update + allow_missing。
- GetTeam 懒创建（首次 Get 物化）：但 Team 需 profile，无合理默认值 → 首次 Get 无从物化；且破坏"Get 不变更"语义。

---

## R4: Team 资源 pattern 末段用单数还是复数？

**Decision**: 保持单数静态段 `templates/{template}/sessions/{session}/team`（**不变**）。

**Rationale**:
- AIP-156：单例资源名 = "父名 + 一个**单数**静态段"，例 `users/1234/config`、`users/1234/thing`。**复数仅用于 List 集合路径**（`/v1/{parent=users/*}/configs`）。
- Team **无 List**（单例无需），故无复数集合路径；pattern 用单数 `team` 合规。
- Access Approval 范例同样用单数静态段 `accessApprovalSettings` 作 pattern。
- 资源定义的 `plural: "teams"` 字段保留（AIP-156 要求单例同时声明 singular 与 plural），但 pattern 不用它。

**Alternatives considered**:
- 改 pattern 为 `.../teams`（复数）：误读 AIP-156（复数仅 List 路径），且会波及 gateway URL 解析、`parseMessagesParent`、desktop client、testplan helpers 全链路，无收益，否决。

---

## R5: codegen（protoc-gen-go-aip）是否需要改动？

**Decision**: **不需要**。新增 `UpdateTeam` RPC + `UpdateTeamRequest{team, update_mask, allow_missing}` 与现有 codegen 兼容。

**Rationale**（已实测验证）:
- `protoc-gen-go-aip` v0.1.3 仅生成资源名 helper（`<Type>Name`/`ParseXxxName`），**无** singleton/Update/allow_missing 概念（源码 grep 零命中）。
- gRPC server/client 接口由 protoc-gen-go-grpc 生成，REST 路由由 grpc-gateway 生成，均由 proto 注解驱动——新增 PATCH 绑定自动产出 `TeamServiceServer.UpdateTeam` + REST 路由 `PATCH .../team`（`update_mask`、`allow_missing` 作为 query 参数，body 为 `team`）。
- Team 单例名解析已正确工作（既有 `TeamName` 对尾字面量段的处理已被 `experimental/golang/aip_codegen` 测试覆盖）。
- `UpdateTeamRequest` 触发 fieldmask `Validate()` 生成，但与现有 `UpdateTeamProfileRequest` 一样被 Bazel 静默丢弃（rules_go protoc wrapper `protoc.go:209-210` 丢弃未声明输出）——非本特性问题，无功能影响。

**Alternatives considered**: 无——实测确认无需改动。

---

## R6: 现有"AIP-133 偏离"如何处理？

**Decision**: **消除**偏离。`specs/031-team-template-mode/contracts/api-contract.md:46` 记录的"同 profile 重复 create 幂等返回既有 Team；不同 profile 返回 ALREADY_EXISTS"特殊逻辑整体移除。

**Rationale**:
- 该偏离是为桌面端多标签竞态重试而自创的（AIP-133 严格要求重复 create 返回 ALREADY_EXISTS）。
- 改用 `allow_missing` upsert 后，幂等是 AIP 内建语义：同 profile 重复 Update = 字段匹配 → 原样返回（天然幂等）；不同 profile = 合法 Update（重建，见 R7）。**ALREADY_EXISTS 在 Team 配置路径上不再出现**。
- owner 分配层（proxy `assignOwner`）的 get-or-create + `ErrOwnerAlreadyExists` 竞态重读（`handler.go:274-348`）**保留不变**——它本就对调用方透明，不外泄 ALREADY_EXISTS。

**Alternatives considered**: 保留偏离：与 AIP-156 合规目标矛盾，且 allow_missing 已提供更干净的方案，否决。

---

## R7: profile 变更后如何使新 profile 生效？（最高风险项）

**Decision**: **完整重建 team graph**，复用既有 MemorySaver checkpointer 保留状态，turn in-flight 时拒绝。

**Rationale**（已读 `projects/game/agent/src/team/graph.ts` 确认可行性）:
- model/prompt 在 graph 编译时闭包烘焙（`player.ts`、`planner.ts` 的 `createAgentFn`），改 profile 须重建 graph。
- `TeamGraphHandle.checkpointer`（`graph.ts:146`）**已暴露**为 public；`buildTeamGraph`（`graph.ts:213-259`）内部 `new MemorySaver()`（`graph.ts:241`）。
- **重建方案**：给 `buildTeamGraph`/`TeamGraphDeps` 增可选 `checkpointer?: MemorySaver` 入参，缺省时 `new MemorySaver()`（首建），重建时传入既有 checkpointer。`TeamState` 是模块私有同一 `Annotation.Root`（`graph.ts:67-89`），recompiled graph 读同一 MemorySaver 按 `thread_id=sessionId` 正确重建 playerMessages/plannerMessages/gameEnded/gameCounter 通道（reducer 语义稳定）。
- `buffer`/`bridge`/`sink`/MCP-host 缓存与 profile 无关，重建时复用不变。
- turn in-flight 守卫：复用 `RefreshTeam` 的 `team.isRunning()` 检查（`handler.ts:231-238`）→ FAILED_PRECONDITION。
- 单飞：复用 `SessionTeamStore` 的 `pending` map（`session-team.ts:505`）对重建路径同样单飞，避免并发重建。

**Alternatives considered**:
- profile 创建后不可变（UpdateTeam 仅用于 allow_missing 物化）：用户已确认需要可变更（见 spec US3/FR-005），否决。
- 热替换节点闭包（不重编译 graph）：LangGraph 节点在 compile 时定型，无热替换 API，不可行。
- 重建时丢弃旧 checkpoint（清空历史重新开始）：违反 FR-005"保留对话/游戏状态"，否决。

---

## R8: spec 039（initInstruction，未实现）受何影响？

**Decision**: 039 **仅改文档**（未实现，无代码迁移）。initInstruction 触发点由"CreateTeam 构建 graph 后异步"改为"UpdateTeam 物化路径（graph 首建）后异步"；profile 变更重建时**不**重跑 init（init 仅首建触发）。

**Rationale**:
- 已确认 039 的 `initInstruction`/`pendingInstruction` 在 `projects/game` 代码中不存在（grep 零命中），039 全部 task 未勾选（`specs/039-planner-memory-calibration/tasks.md`）。
- initInstruction 语义是"team graph 首建后异步产出一次无游戏历史的策略指令"。物化路径（allow_missing 首次创建）= graph 首建，触发点自然迁移至此。
- profile 变更重建 ≠ 首建（历史已存在），不应重跑 init；039 的 postCompact 场景是独立路径，不受影响。

**Alternatives considered**: 无——039 未实现，仅文档对齐。

---

## R9: 桌面端 create-if-missing 流程如何调整？

**Decision**: 将"GetTeam→NOT_FOUND→弹窗选 profile→createTeam"两步合并为单次 `updateTeam(profile, allow_missing=true)`。弹窗仍用于选 profile，但创建调用改为 update。

**Rationale**（已读 `projects/game/desktop/frontend/src/App.svelte`）:
- 现状：`App.svelte:385-397` 进入会话时 GetTeam，失败（NOT_FOUND）则 `showProfileSelect=true`；`handleProfileSelected`（`App.svelte:437-459`）调 `createTeam`，失败再 GetTeam 兜底（多标签竞态恢复）。
- 改后：选 profile 后直接 `updateTeam(profile, allowMissing=true)`——缺失则物化、存在则幂等返回。多标签并发天然收敛（R6），无需 GetTeam 兜底重读。
- Wails 链路：`App.svelte` → `api.ts:529 createTeam` → `app.go:1094 App.CreateTeam` → `client.go:233 Client.CreateTeam`（POST）。改为 `updateTeam` → `App.UpdateTeam` → `Client.UpdateTeam`（PATCH + `update_mask` + `allow_missing` query），模板照 `UpdateTeamProfile`（`client.go:476-513`）。

**Alternatives considered**: 保留 GetTeam 探测两步：多一次往返，且 allow_missing 已让单次 update 足够，无收益。

---

## R10: owner 分配是否随 CreateTeam→UpdateTeam 改变？

**Decision**: **不变**。`assignOwner`（`proxy/handler/handler.go:274-348`）原样搬入 UpdateTeam handler，仍是唯一 owner 分配点；get-or-create + `ErrOwnerAlreadyExists` 竞态重读语义保留。`Connect`/`GetTeam`/`ListMessages`/`RefreshTeam` 的 `lookupOwner`（`handler.go:255-266`）不变，未物化仍 NOT_FOUND。

**Rationale**: owner 分配与 profile/物化语义正交；仅入口 RPC 名与请求解析（parent→team.name）改变。`assignOwner` 的竞态幂等已被既有测试覆盖（`handler_test.go:360-390`）。

**Alternatives considered**: 无——保持不变最稳。
