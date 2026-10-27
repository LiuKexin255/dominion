# Research: agent_v2 host 层场景化重构支持主子模型

> Feature: [spec.md](spec.md) | Plan: [plan.md](plan.md)
> 输入裁定：spec 决策基线 1-5（survey [sts2-agent-model.md](../../survey/sts2-agent-model.md) A1-A4 + 本 feature 两项）。
> 现状证据：`projects/game/agent_v2/src/session.ts`（host 消费面）、`common/js/dsh-plugins/saolei/loop/src/orchestrator.ts`（编排公开面）、`projects/game/agent_v2/src/scenes.ts`（079 数据级场景表）、`projects/game/testplan/README.md`（suite 惯例）。

## D1: 编排 seam 中立接口的落位——host 侧接口 + 场景适配器

**Decision**: 场景中立编排 seam 接口定义在 host（新模块 `projects/game/agent_v2/src/orchestration.ts`）；saolei 经 host 侧薄适配器（`scene-saolei.ts`）包装 `TeamOrchestrator`（`@dominion/dsh-saolei-loop`），sts2-loop 新包（D5）直接实现同构公开面。`scenes.ts` 以闭包把本场景数据（`spec`）传给 `createSaoleiOrchestration(ctx, deps, spec)`；场景模块不 import `scenes.ts`，避免模块循环。TS 结构化类型 + 契约文档（[contracts/scene-registry.md](contracts/scene-registry.md)）双锚定。

**Rationale**: 依赖方向不可逆——host 依赖插件包（cordis 组合 + import），插件包不能 import host；为仅两个实现新建共享契约包是过度设计（宪法 II）。适配器层让 saolei-loop 包**零改动或最小改动**即被纳入（saolei 零变化约束 FR-008 的风险最小路径：`TeamOrchestrator` 的调用点全部集中在 host `session.ts`，全仓库无其他消费者）。

**Alternatives considered**:
- 新建共享契约包 `@dominion/dsh-scene-seam`（saolei-loop/sts2-loop/host 三方依赖）——否决：两实现、无第三方消费者，新增包维护成本不成比例。
- host 直接消费 saolei-loop 类型、sts2-loop 模仿其形状——否决：seam 上残留 saolei 词汇（`TeamRole`/`OrchestrationPhase`/`TeamMaterializeOptions` 的 player/planner 槽位），违反 FR-005。

## D2: 成员集合的所有权——编排成员投影（seam `members()`）

**Decision**: host `TeamEntry` 的成员运行时（`MemberRuntime`，agent/announce-only 两类）改由**编排物化后的成员投影**派生：seam 暴露 `members(): readonly SceneMember[]`（`{role, kind: "agent" | "announce-only"}`），agent 句柄经 `member(role)` 获取。saolei 投影 = player + planner + announcer（announcer 不变式 `announcer === undefined → throw` 移入 saolei 适配器物化自检）；sts2 投影 = 单 `main`。成员名预检与 roster 错误文案由场景注册的 `faceMembers`（saolei `player/planner/announcer`、sts2 `main`，仅名称面）提供，避免 host 回退到 announcer 字面量。

**Rationale**: 成员集合是编排物化的产物——谁被物化只有编排知道（announcer 是 saolei-loop 注册的成员、sts2 没有）；host 硬编码 `[player, planner, announcer]` 正是 survey §3 列名的耦合点。投影派生后 host 通用路径对成员形状零假设（FR-002/FR-004）。

**Alternatives considered**:
- materialize 返回成员列表——否决：与现有 accessor 风格（`member(role)`/`announcer`）不一致，且刷新重建时返回值传递链更长。
- 场景注册静态声明成员模型——否决：announcer 是物化期注册的服务端成员（非调用方配置），静态声明表达不了"物化后才确定"。

## D3: 物化输入中立化——seam 输入 = 会话 + 成员配置列表

**Decision**: seam 的 `materialize` 输入为 `{session, template, members: readonly [{role, preset, provider?, model?}]}`（场景 roster 顺序，preset 为 id、provider/model 为已拆分路由）。saolei 专属派生全部移入适配器内部：goal 取场景注册数据、`memoryScope` 从 planner 成员 preset + template 派生、双槽位（`player`/`planner`）映射、成员 sessionId 后缀。

**Rationale**: host 通用物化路径只保留场景无关职责——结构校验（成员非空/role 非空/preset 资源名合法且同模板）→ 场景 roster 校验（注册提供）→ preset 存在性 + role 匹配 + model 目录校验（两层 fail-fast，时序与文案逐字节保持）→ 交编排物化。saolei 语义（goal 进 team section、planner memory 作用域）对通用路径不可见（FR-003）。

**Alternatives considered**:
- seam 输入保留 goal/memoryScope 可选字段——否决：可选字段是场景词汇残留，sts2 永远传空，违反 FR-005 的词汇中立要求。

## D4: 统一成员事件流信封——沿用 `TeamMemberEvent` 词汇 + host 动态角色投影

**Decision**: seam 的成员事件流信封沿用 `@dominion/dsh-team` 的 `TeamMemberEvent`（`common/js/dsh-plugins/team/src/team.ts:154`——`{role, memberId, channel: "log"|"live", event}`，role 为开放串，类型注释已声明 "future scene roles flow through unchanged"）。sts2-loop **仅类型导入**（无 `ctx.team` 运行时依赖），把 main agent 的 session/live 事件以 `role="main"` 装入信封经 `teamEvents()` 透传。host 分发规则：信封 role ∈ 成员投影 → 按成员 kind 派发（agent → collector、announce-only → 归并播报投影）；信封 role ∉ 投影（名册外角色，如未来子代理）→ **动态创建 viewless 投影 collector**（计入归并序列与成员标注帧，不进 face 名册、无成员资源、不写成员视角）。

**Rationale**: 信封词汇本就是 077 为成员事件统一流设计的通用联合（agent 适配层词汇，非群聊专属）；再造一份信封类型将迫使 `MemberCollector`（`history.ts`）双轨消费。动态角色投影是 FR-007 名册固定裁定的机制面——本期落地机制（单测以合成信封验证），sts2-loop feature（S3 子代理派发）直接使用，不再改 host。

**Alternatives considered**:
- host 按名册闭集丢弃未知角色事件——否决：FR-007 明令禁止（子代理活动必须可见）；后续再开洞违反宪法 II。
- 信封类型复制到 host 侧新定义——否决：与 dsh-team 类型漂移风险，无收益。

## D5: `@dominion/dsh-sts2-loop` 最小编排包设计

**Decision**: 新包 `common/js/dsh-plugins/sts2/loop/`（`@dominion/dsh-sts2-loop`，布局对齐 `saolei/loop/` 先例，spec Clarifications 裁定）：

- **公开面**：`Sts2Orchestrator`（构造 `(ctx, deps)`，deps = `{compose, agents?, logger?, provider?}`——裁剪 saolei 的 `TeamOrchestratorDeps`，无 team/desktopBridge/cwd）+ 插件行 `sts2-loop`（本期空能力 `apply`：无 `config.plugins`、无 provide 面、inject 为空；组合清单新增惰性行，对齐 079 的 `sts2` 骨架行模式——后续 S3/S4 房间能力挂本行 config 生长）。
- **物化**：单成员 `{role:"main", preset, provider, model}` → `compose(preset)` → `ctx.agents.create`（sessionId = `${session}/main`）→ `preset.setup(agentCtx)`；失败整体回滚保持未物化（同 saolei 契约）。无 team 注册、无 announcer、无 memory、无游戏运行时。
- **驱动**（`drive.ts`，saolei `agent-member.ts` 先例的极简化）：静止时 submit → 装配输入（无广播 drain，仅用户消息）→ `inject`/末条 `followup` → `agent.whenIdle()` 收束；在途时 submit → `steer` 入队（inbox pending 计入快照 `queued`）；错误按六码分类（`TRANSIENT/QUOTA/CREDENTIAL/CONFIG/INPUT_TOO_LARGE/INTERNAL`，映射逻辑对齐 saolei `mapEngineCode`）。单 agent 无 flow 状态机——无 plan/play/review 相位、无自动续驱（turn 结束即静止，等下一条用户输入）。
- **快照**：`{active: "main"|null, activation: "main", paused, queued, failed, lastError}`；中立 seam 不含 `phase`（sts2 无相位语义；saolei 适配器在自身 logger 包装中保留 phase，host 通用 logger/seam 不暴露该字段，请求/流行为不变）。
- **事件**（`events.ts`）：订阅 main agent 的 session 事件与 `agent/assistant-stream` live 面，装入 `TeamMemberEvent` 信封（`memberId` = agent session id）。

**Rationale**: 最小编排只做"单主 agent 直驱会话"——这是 FR-009 的验收载体与 sts2-loop feature 的落地板；A4 派发机制、房间代理、section 分配全部延后（FR-010），但包结构（行 + orchestrator + drive + events 分层）为后续填充预留了与 saolei 同构的生长点。空能力 `apply` 行使 sts2-loop 能力依赖声明的挂载点先占位（与 saolei-loop `config.plugins` 同型演化路径）。

**Alternatives considered**:
- 宿主内模块（spec Clarification Q1 已否决）——二次搬家。
- 填充 `@dominion/dsh-sts2`（game 骨架包）——否决：game/loop 分层是 075 建立的领域/编排分离先例，编排塞进 game 包破坏分层。

## D6: sts2 场景开放面——场景表 + preset 池模板

**Decision**:
- `scenes.ts` 的 `SceneSpec.sts2` 开放：`roster: ["main"]`、`presetRoles: ["main"]`、`rolePoolTemplate: {main: "main"}`、`faceMembers: ["main"]`、`orchestratable: true`、编排工厂指向 `scene-sts2.ts`。`SceneSpec` 新增编排工厂字段与 `faceMembers`（名称/文案面；原 roster/preset 数据字段语义不变）。
- `cordis.yml` preset-authoring `templates` 快照新增 `main` 池模板（persona 行，`@deepseek-ai/dsh-persona` prefix 承载身份文案——含稳定锚串"sts2 主 agent"供 fake-llm `system_keywords` 锚定，D8）。
- 079 fail-loud 自动收缩：`server.ts` 的 preset 面（`scenePresetRoles` 空词表拒绝）与 `session.ts` 的 `wiredScene` 门均为数据驱动，词表非空/`orchestratable: true` 后即开放，错误路径无需改动；saolei 文案由既有"template + 场景表字段构造"机制保持逐字节不变。

**Rationale**: 079 把场景差异收敛为数据表正是为了让后续场景"声明条目即可挂接"（079 spec US2）；本 feature 兑现该承诺并验证泛化质量。

**Alternatives considered**:
- 为 sts2 另设 preset 校验路径——否决：结构同构，双路径违反 DRY 与零变化约束。

## D7: host 通用路径的泛化切分（session/history/server）

**Decision**:
- `history.ts`：`AgentMemberRole`（`"player"|"planner"`）、`MemberRole` 与 `isAgentMemberRole` 守卫删除，collector/视图/成员视角全部按 `string` role 工作；`TeamHistory.views` 改 `Map<string, MemberViewEntry[]>` 惰性创建；`appendMemberOutput` 增加 `recordView` 开关以支持名册外角色的 viewless 动态投影（不写成员视角、不发 `member_view`）；announce-only 判定改为成员 kind（`MemberRuntime` 判别联合已就位，仅类型放宽）。
- `session.ts`：删除 `orchestratedRoles`（恰双成员断言）、announcer 硬编码成员/不变式、`memoryScope`/`goal` 计算与 saolei 形 `materialize` 参数拼装、`createOrchestrator()` 直构——改为经场景注册工厂 + seam 调用；成员事件分发按 D4 规则（投影派生 + 名册外动态 collector）；`LOOP_ERROR_TEXT` 的 key 类型改为 host 侧六码联合（词汇自 saolei-loop 的 `LoopErrorCode` 解耦）。
- `server.ts`：无结构改动（preset 面与 UpdateTeam 门已数据驱动，D6）；仅随 scenes 类型演进同步。

**Rationale**: 全部改动点即 survey §3 耦合清单的逐项消除；`server.ts` 零结构改动是 079 泛化质量的直接复用。

**Alternatives considered**:
- 把 saolei 适配器放进 saolei-loop 包（`createSceneRegistration()` 导出）——备选保留：若实现中发现适配器需要访问包内私有类型过多，可移入包内导出；本 plan 默认 host 侧（公开面足够：研究确认 host 消费的全部是 public 成员）。

## D8: `game-sts2` suite——精简部署 + persona 锚定 fixture + helper 参数化

**Decision**:
- **部署**：新建 `projects/game/testplan/deploy_sts2.yaml`（mongo + session + fake-llm + proxy + agent-v2-test + gateway）；省略 web/fake-desktop/memory 的理由与精确拓扑见 [contracts/testplan-game-sts2.md](contracts/testplan-game-sts2.md) §1（memory 可缺省由 `deploy_agent_v2_memory_down.yaml` 证明）。
- **suite**：`system_test.yaml` suites 末尾新增 `game-sts2`，cases 为 bazel target 粒度（既有惯例）；挂载形态见契约 §2。
- **用例**：`sts2_conversation_test.go` 四个用例（物化与名册/对话回合/静止与刷新/非名册 NOT_FOUND），G/W/T 见契约 §3。
- **fixture**：`sts2_main.yaml` 新增 1 条 persona 锚定消息模板（总数 41→42）；schema、匹配语义与嵌入/lockstep 义务见契约 §4。
- **helper 参数化**：`agent_v2_helpers_test.go` 增加 `templateID` 参数 + `updateSts2MainTeam` 薄包装，细则见契约 §5。

**Rationale**: fake-llm 无 session/模板维度，唯一作用域手段是 `system_keywords` 锚 persona 身份行 + 唯一触发词（匹配语义：`specs/012-fake-llm-service/spec.md` FR-006/FR-007 的 keywords 子串命中与 name 字典序 tie-break；`system_keywords` 语义见 `specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §2/§3）；sts2 main persona 与 saolei persona 不同串，天然隔离。精简拓扑避免 web/fake-desktop/memory 冗余部署面（宪法 II：测试物料同样避免无关面）。

**Alternatives considered**:
- 复用 `deploy_agent_v2.yaml`——否决：带入 web/fake-desktop/memory 冗余（fake-desktop 会拉 saolei downlink 噪音）；suite 独立部署成本相同（每 suite 独立 run id + 60s settle，与部署文件无关）。
- sts2 用例追加为 `game-system` 的 case——否决：与 "game-system = 完整 saolei 系统拓扑" 语义混淆；spec FR-009 明确要求新增 suite。

## 决策-需求追溯

| 决策 | 覆盖需求 |
|---|---|
| D1/D2/D3/D4/D7 | FR-001~FR-005（场景注册、announcer 内化、team/memory 可选、roster 泛化、seam 中立） |
| D6 | FR-004/FR-009（sts2 开放、preset 词汇、fail-loud 收缩） |
| D5 | FR-009（最小编排落位与行为）、FR-010（负向边界） |
| D8 | FR-008/FR-009 验收面（suite、零回归 + 新 suite） |
| D4 动态投影 | FR-006/FR-007（face 名册固定 + 子代理标注可见的机制面） |
