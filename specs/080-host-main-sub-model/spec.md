# Feature Specification: agent_v2 host 层场景化重构支持主子模型

**Feature Branch**: `080-host-main-sub-model`

**Created**: 2026-10-25

**Status**: Draft

**Input**: User description: "@projects/game/agent_v2/ 在支持 sts2 之前，需要对 host 层进行改造以支持主子模型。前置调研 @survey/sts2-agent-model.md"

**决策基线**（[survey/sts2-agent-model.md](../../survey/sts2-agent-model.md) §0 需求方裁定，本 feature 直接继承）：

1. **A1/A2（agent 模型）**：sts2 采用主子模型——单主 player agent 统筹、按房间类型动态派发子代理，**不经 team 群聊**（dsh-agent 原语直接驱动）。host 改造的目标承载形态即此。
2. **A3（host 落位）**：agent_v2 场景化重构先行——把 saolei 场景校验/编排从 host 抽出为**场景注册**（saolei team 场景 + sts2 非 team 场景），sts2 在重构后的多场景 host 上落地（宪法 II：架构随新场景需求演化，而非并行补丁面）。本 feature 即 A3 的重构载体。
3. **A4（派发机制延后）**：子代理派发机制（工具派发 vs 编排层派发）、子代理生命周期、主/子能力与 section 分配（survey §5 S3/S4）、房间类型 preset 池模板（S6）均属后续 sts2-loop feature；本 feature 只负责让 host **能够承载**该形态，不实现派发本身。
4. **验收载体 = 最小 sts2 接线（2026-10-25 本 feature 裁定）**：本 feature 以 FR-009 的最小 sts2 接线作为主子形态的端到端验收载体（宪法 VI）；否决"仅结构性重构 + 合成测试场景"案（主子能力无端到端证明）。
5. **子代理可见性 = 名册固定（2026-10-25 本 feature 裁定，survey S2 关闭）**：face 名册物化时固定、仅含主 agent；子代理活动经成员标注帧与归并序列条目呈现，无子代理成员资源。否决"动态名册"（成员资源命名/名册涨缩/刷新窗口交互复杂度过高）与"完全不可见"（与 S5 子代理派发记录呈现相悖）两案。

现状基础：079 已完成数据级泛化——`projects/game/agent_v2/src/scenes.ts` 的 `SceneSpec`（roster/presetRoles/rolePoolTemplate/orchestratable/teamGoal）已把场景差异收敛为按 template 的数据表；本 feature 前 sts2 为未接线条目（079 期语义，[specs/079-sts2-template-scaffold/spec.md](../079-sts2-template-scaffold/spec.md) FR-004）。本 feature 把该数据表**演进为行为级场景注册**：survey §3 所列 host 耦合点（roster 形状、编排工厂硬连、announcer 不变式、memory 挂载、face 语义）全部下沉到场景侧。

## Clarifications

### Session 2026-10-25

- Q: sts2 最小编排（直驱主 agent 的编排实现）应落位在哪里？ → A: 新建 `@dominion/dsh-sts2-loop` 包（`common/js/dsh-plugins/sts2/loop/`），cordis.yml 新增对应行；后续 sts2-loop feature 在此包内填充派发与房间代理（对齐 saolei/{game,loop} 分组先例）。
- Q: web 是否需要能打开 sts2 会话（模板入口）？ → A: web 维持固定 saolei（零改动）；sts2 会话经 API/testplan 验收，web 呈现（含模板入口）整体归 S5 下游 feature。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - host 场景注册：通用路径场景中立，saolei 经注册接入零变化 (Priority: P1)

作为后续 sts2 场景 feature 的开发者，我希望 agent_v2 host 的会话通用路径（物化校验、编排构造、成员事件消费、历史投影、流收束）消费**按 template 的场景注册**——场景提供 roster 定义（host 通用路径据此执行物化校验）、编排工厂与目标文案（场景能力如 team 中继、announcer、memory 挂载由注册模块内部承载）——使得"非 team、无 announcer、无 planner memory、单主 agent 物化 + 编排动态物化子代理"的主子形态场景无需修改 host 通用代码即可承载；saolei 经其场景注册接入后全部行为逐字节不变。

**Why this priority**: 这是 A3 裁定的核心结构性工作；host 不场景中立，后续每个场景（不止 sts2）都要改动并回归混杂场景假设的通用路径。主子形态的其他价值（face 语义、验收载体）都挂载在本切片之上。

**Independent Test**: saolei 端到端全链路零回归（单测全量 + 既有大型测试全部用例通过）；host 通用代码中 saolei 专属假设（announcer 不变式、恰双成员 roster、player/planner 词汇、memoryScope/goal 注入、`TeamOrchestrator` 直构）零残留，quickstart V3 固定命令可验。

**Acceptance Scenarios**:

1. **Given** 重构完成后的 agent_v2，**When** saolei session 走全链路（preset CRUD、UpdateTeam 物化、Send 流、双视图历史、memory 快照、hooks、Cancel/刷新），**Then** 全部行为与重构前逐项一致（079 scene-dispatch.md §4 泛化不变量模式的延续：错误文案、校验时序、编排、prompt sections 均不变）。
2. **Given** host 通用代码（`projects/game/agent_v2/src/session.ts`、`server.ts`、`history.ts` 的通用路径与新 seam 模块 `orchestration.ts`），**When** 检查场景专属假设，**Then** announcer 成员及其不变式、双成员 roster 形状断言、player/planner 角色词汇、planner memoryScope 计算、goal 文案注入、saolei-loop 编排直构全部下沉至 saolei 场景注册，通用路径按场景注册声明派生。
3. **Given** 一个声明"无 announcer、无 team 中继、无 memory 挂载"的场景，**When** 经 host 物化，**Then** 不触发任何 missing-announcer 类不变式违例，物化/驱动/刷新生命周期完整可用。

---

### User Story 2 - 主子会话的 face 语义映射（S2 落地） (Priority: P2)

作为 sts2-loop feature 的开发者与 agent-v2 对话面（gateway/web）的调用方，我希望 `UpdateTeam` 对主子场景映射为"按场景 roster 物化主 agent"（单成员配置），且动态子代理的 face 可见性有明确裁定并落为契约，使得后续 loop feature、web 呈现（S5）不再面对未定义语义。

**Why this priority**: face 是 host 与调用方（web/gateway/testplan）的协作边界（宪法 III 接口优先）；S2 在 survey §5 中明确归属本 feature，其裁定是 sts2-loop feature 的直接输入。

**Independent Test**: 单成员 roster 的物化/刷新/名册/激活成员语义可独立经 face 验证（配置面 preset + `UpdateTeam`/`GetTeam`/`Send`）；子代理可见性裁定落为契约文档。

**Acceptance Scenarios**:

1. **Given** 单成员场景经场景注册物化（合成场景或 US3 最小 sts2 接线），**When** `UpdateTeam` 携带场景 roster 的成员配置（单 `main` 成员：role/preset/model），**Then** 物化成功——`GetTeam.members` 为场景名册（无服务端附加成员）、`active_member` 语义成立（在途 = 驱动成员、静止 = activation）、刷新语义（终止在途回合 ABORTED、未消费输入作废、清空短期记忆、`create_time` 保留、幂等）与既有契约同构。
2. **Given** 物化后的主子会话，**When** 场景编排随后动态物化子代理并发出成员标注事件，**Then** 按名册固定裁定呈现：成员标注帧 + 归并序列条目可见（face 名册不变，无子代理成员资源）。
3. **Given** proto 面（`projects/game/agent_v2.proto`），**When** 本 feature 交付，**Then** 场景无关 team 原语零变更——主子语义映射全部在 host/场景层承载。

---

### User Story 3 - sts2 最小接线：主子形态的端到端验收载体 (Priority: P3)

作为本 feature 的验收负责人与后续 sts2-loop feature 的开发者，我希望 sts2 场景以最小形态接线（roster=`["main"]`、preset 词汇 `main`、官方 agent-loop 直驱主 agent；无游戏工具、无子代理派发、无 desktop flow），使"host 已能承载主子形态"经 testplan 端到端验收，且 sts2-loop feature 在被证明的接线上开发而非在仅理论重构过的 host 上叠加。

**Why this priority**: 最小接线是 US1/US2 的端到端验收载体与后续场景 feature 的落地板，价值依赖场景注册与 face 语义先就位，故优先级最低。

**Independent Test**: sts2 会话经 face 全生命周期（创建 preset → `UpdateTeam` 物化 → `Send` 对话 → 静止 → 刷新）可用；testplan 新增 sts2 最小 suite（fake-llm 既有能力）全部用例通过，saolei 既有 suite 同批零回归。

**Acceptance Scenarios**:

1. **Given** sts2 场景以最小形态接线，**When** 经 face 物化 sts2 会话（单 `main` 成员）并发送消息，**Then** 主 agent 回合完整呈现（成员事件帧、归并序列、`main` 成员视角），静止收束与取消语义成立。
2. **Given** testplan，**When** 执行新增 sts2 最小 suite，**Then** 部署 → 测试 → 清理闭环全部用例通过；saolei 既有 suite 同批全部通过（零回归）。

---

### Edge Cases

- **刷新窗口**：单成员场景的在途回合终止（`turn_end{ABORTED}`）、未消费输入作废、无半物化（失败整体回滚可重试）、同 session 并发物化串行——与 saolei 契约同构（`specs/059-agent-v2-team-mode/contracts/team-api.md` §2）。
- **无 announcer 场景的名册**：`GetTeam.members` 不含 announcer 行；成员读取/成员视角按场景名册派生，非名册角色（含子代理角色，按 FR-007 名册固定裁定）NOT_FOUND。
- **079 fail-loud 语义的收缩边界**：sts2 的 `UpdateTeam` 与 preset CRUD 面随最小接线开放（role 词汇 `main`），079 §3 的"场景未接入"拒绝面对 sts2 全部消失——本 feature 后不存在未接线场景；门禁函数与断言机制保留，以"未知模板键"的单测覆盖。游戏工具/desktop flow/房间 preset 不属 RPC 门禁面，按 FR-010 不在本 feature 实现。
- **web 不接入 sts2**：web 经 `CURRENT_TEMPLATE` 常量固定 saolei（079 契约 §5），本 feature 零 web 改动；sts2 会话经 API/testplan 验收，web 呈现（含模板入口与成员视图适配）整体归 S5 下游 feature。
- **desktop 控制面**：桌面控制是 saolei 场景能力（player 独占）；sts2 最小接线不含 desktop flow 消费，DesktopBridgeService 对 sts2 session 不产生游戏 flow 语义（游戏面属后续 feature）。
- **物化校验语义同构**：主子场景的 preset 存在性 + role 匹配、复合 model 目录校验复用既有两层 fail-fast 结构（`session.ts` `validateMemberStructure`/场景 roster 校验），错误码不变（INVALID_ARGUMENT）。
- **组合清单断言**：`dsh.test.ts` 对 cordis.yml 有逐行断言（079 Edge Cases 已载）；本 feature 若增改清单行（如 sts2 最小编排挂载行），必须同步断言与 `specs/070-dsh-upgrade/contracts/composition-manifest.md` 契约。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: host 会话层 MUST 消费按 template 索引的**场景注册**（`SceneSpec` 的行为级演进），注册项 MUST 至少含：roster 定义（物化 roster + face 成员名清单；host 通用路径据此执行物化校验与成员名/错误文案派生）、编排工厂、目标文案；场景能力（team 中继、announcer 行为、memory 挂载）MUST 由场景注册模块内部承载，MUST NOT 以声明式能力字段进入 `SceneSpec`；`faceMembers` 仅承载成员名/文案面，不构成能力声明。host 通用路径（`projects/game/agent_v2/src/session.ts`/`server.ts`/`history.ts` 通用部分及 seam 模块 `orchestration.ts`）MUST NOT 残留任何场景专属分支或字面量。
- **FR-002**: announcer 成员（名册呈现、物化不变式、播报投影）MUST 收敛为 saolei 场景注册的内部事务；saolei 场景 MUST 在 face 成员名清单中声明 announcer（仅用于名册/错误文案，不含能力/行为声明），其行为面仍由注册模块内部承载；声明无 announcer 的场景 MUST 可被 host 完整承载，MUST NOT 存在 host 侧 missing-announcer 类不变式违例路径。
- **FR-003**: host MUST NOT 要求场景使用群聊中继（`ctx.team`）或 memory 挂载；saolei 的 team relay 注册与 planner preset 作用域 memory 挂载（`memoryScope` 计算）MUST 移入其场景注册/编排侧，行为零变化。
- **FR-004**: 物化 roster 校验 MUST 由场景注册供给的 roster 定义驱动（host 通用路径执行），MUST 支持单成员（主子）与多成员（team）roster 形状；saolei 的校验时序与错误文案 MUST 逐字节不变。
- **FR-005**: host MUST 经场景注册的编排工厂构造会话编排；host 消费的编排 seam 契约（物化输入、成员访问、统一成员事件流、快照/静止判定、submit/cancel/dispose）MUST 场景中立——player/planner 角色、planning/playing/reviewing 相位、announcer 访问器等 saolei 词汇 MUST NOT 出现在通用 seam 上。
- **FR-006**: 主子场景的 `UpdateTeam` MUST 按场景 roster 物化（单 `main` 成员配置）；proto（`projects/game/agent_v2.proto`）MUST 零变更；刷新、幂等与无半物化语义 MUST 与既有契约同构。
- **FR-007**: 子代理的 face 可见性裁定为**名册固定**：物化名册 MUST 仅含场景 roster 成员（主子场景 = 单 `main`）；子代理活动 MUST 经成员标注帧与归并序列条目进入流与历史投影（经流/历史 API 对调用方可见；web 呈现归 S5 下游 feature），MUST NOT 产生子代理成员资源（`GetTeamMember`/`ListMemberMessages` 对非名册角色 NOT_FOUND）。host 通用路径 MUST 支持名册外角色的事件标注投影（编排 seam 的成员事件流按信封角色派生，不按名册闭集丢弃）。该裁定 MUST 落为契约并作为 sts2-loop/web（S3/S5）输入。
- **FR-008**: saolei 全部行为 MUST 零变化（组合清单、工具面、编排、prompt sections、preset 派生、错误文案、流与广播语义）；既有大型测试（`projects/game/testplan` 各 suite，经 testplan skill 完整执行部署→测试→清理闭环）MUST 全部用例通过，作为零回归验收。
- **FR-009**: 主子形态能力的验收载体裁定为**最小 sts2 接线**：sts2 场景注册 MUST 升级为可编排（roster=`["main"]`）；PresetService MUST 开放 sts2 preset 词汇（role `main` + `cordis.yml` preset-authoring `templates` 快照新增池模板 `main`）；最小编排 MUST 落位新建包 `@dominion/dsh-sts2-loop`（`common/js/dsh-plugins/sts2/loop/`，组合清单新增 sts2-loop 行）——官方 agent-loop 直驱主 agent（用户消息 → 驱动主 agent → 静止；无游戏工具、无子代理派发、无 desktop flow、无 memory 挂载、无 announcer）；testplan MUST 新增 sts2 最小 suite（fake-llm 既有能力）并全部用例通过。079 §3 的 sts2 fail-loud 语义随开放面收缩（`UpdateTeam` 与 sts2 preset CRUD 开放；079 §3 不再对 sts2 生效，其余非门禁面按 FR-010 不实现）。
- **FR-010**: 本 feature MUST NOT 实现（负向边界，属后续 sts2-loop/web feature）：子代理派发机制与生命周期（A4/S3）、主/子能力与 section 分配（S4）、房间类型 preset 池模板（S6 完整面）、sts2 游戏工具与运行时填充（`@dominion/dsh-sts2` 游戏基础包保持骨架）、fake-sts2 服务、desktop 桥 sts2 relay、web 主子呈现 UI（S5）。

### Key Entities

- **场景注册（Scene Registration）**：按 template 索引的宿主侧场景契约（`SceneSpec` 数据表的演进）——roster 定义（物化 roster + face 成员名清单，host 据此校验并派生成员名/错误文案）、编排工厂、目标文案；场景能力（team 中继、announcer、memory 挂载）由场景注册模块内部承载，host 不感知。saolei 与 sts2 各持一条注册。
- **编排 seam（场景编排接口）**：host 消费的会话编排契约面：物化输入、成员访问、统一成员事件流、快照（active/queued/paused/lastError）、submit/cancel/静止判定/dispose。saolei-loop `TeamOrchestrator` 是其 saolei 实现。
- **主子会话形态**：单主 agent 经 face 物化 + 场景编排按需动态物化房间子代理（`@deepseek-ai/dsh-agent` `CreateAgentOptions` 的 parentAgent/origin/delegationDepth/seed 原语，后续 feature 消费）；子代理间不互通，结果回主上下文。
- **face 名册 vs 活动成员标注**：物化时固定的成员资源集合（GetTeam.members）与流/归并序列中动态产出者标注（member 标签）的区分——FR-007 裁定的核心概念。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: saolei 零回归：全仓构建与单测通过；既有大型测试全部 suite（game-system/game-disconnect/game-memory-down/game-stall）经 testplan 完整执行全部用例通过（无 failed/flaky）。
- **SC-002**: host 场景中立可验：通用代码路径零场景专属残留（在 079 SC-005 检查面基础上扩展至 US1 AC2 所列禁入面，含"恰双成员 roster 断言"残留面，并覆盖新模块 `projects/game/agent_v2/src/orchestration.ts`）；`quickstart.md` V3 给出固定、可直接执行的检查命令与判定口径。
- **SC-003**: sts2 最小接线端到端验收：sts2 会话全生命周期（物化 → 对话 → 静止 → 刷新）经 testplan 新增最小 suite（fake-llm 拓扑）全部用例通过。
- **SC-004**: 契约交付：场景注册契约（注册项与 seam 定义）与主子 face 语义映射（含 FR-007 子代理可见性裁定）落为 `contracts/` 文档，可直接作为后续 sts2-loop（S3/S4/S6）与 web（S5）feature 的引用输入。

## Assumptions

- proto 零变更（`projects/game/agent_v2.proto` 场景无关 team 原语已可承载单成员物化；名册固定裁定下无 proto 层诉求）。
- web 零改动：经 `CURRENT_TEMPLATE` 常量固定 saolei（079 契约 §5 约束延续）；sts2 会话不经 web 验收，web 呈现（含模板入口、成员视图适配）整体归 S5 下游 feature。
- sts2 主 agent 角色词为 `main`（[survey/sts2-agent-model.md](../../survey/sts2-agent-model.md) §1 用语）、preset 池模板 id 为 `main`；角色词表与池模板的后续扩展（房间类型角色）由 S6 承载，本 feature 只开最小面。
- dsh-agent 子代理原语（parentAgent/`meta.origin:'subagent'`/delegationDepth/seed）由场景编排侧在后续 feature 消费；host 本期不直接触碰 agent 创建细节（经编排 seam 隔离）。
- 079 的 sts2 fail-loud 面随最小接线按开放面收缩（`UpdateTeam` + sts2 preset CRUD），sts2 数据面（session/wiki/memory 结构接受）行为不变。
- fake-llm 既有 Responses 协议能力足以支撑 sts2 最小 suite（主 agent 为纯对话，不需要新增 fake-llm 服务能力；仅新增一条 persona 锚定的 `sts2_main.yaml` 消息模板，见 FR-009 与 `contracts/testplan-game-sts2.md` §4）。
- sts2 验收所用 preset 的 persona 为空——空 persona 保留 main 池模板 prefix base（含 fake-llm `system_keywords` 依赖的锚串）；非空 persona 会覆盖 prefix（`common/js/dsh-plugins/preset-authoring/src/derive.ts`）。
- saolei-loop 包（`@dominion/dsh-saolei-loop`）自身的内部重构（如编排接口化的落点在包内还是新建契约模块）由 plan 阶段裁定；本 spec 只约束 host 侧消费面场景中立。
