# Feature Specification: memory 插件双面拆分 + webUI 终局回合折叠 + saolei_remain 语义澄清

**Feature Branch**: `064-memory-split-fold-remain`

**Created**: 2026-09-14

**Status**: Draft

**Input**: User description: "1. memory 现在分为 dsh-memory 和 dsh-memory/preset-row 两个插件行。如果 dsh-memory 作为连接 memory 服务的基建，那么它不应该出现在 player forbidden 里。但现在 dsh-memory 又要挂载 host 层面，又要在 agent 维度被禁止，插件边界不清晰。干脆直接拆成两个插件，一个作为 host 层基建，一个作为工具插入agent。2. web ui player 在游戏结束后，没有出现类似'思考过程（n步骤，m工具调用）'这样的折叠。游戏结束后 player 的 turn 结束，也应该像 planner 那样被折叠。3. remain 工具的描述或配套的 prompt 是否说明不准确，remain 返回的是当前格子周围'剩余地雷数量'，但我看 player 输出以为是周围旗子的数量。可以用 session_id : d4f677888cd38df8897f514eadb0c0fb 作为案例，获取历史记录验证。"

## Motivation

三个独立缺陷，共同背景是 agent_v2 team 模式（059/060/062 产线）运行后的观察：

**① memory 插件双面挂载，边界不清**。`@dominion/dsh-memory` 一个包承担两个 cordis 挂载面：主入口是 host 服务面（提供 `ctx.plannerMemory`，挂 `projects/game/agent_v2/cordis.yml:155`），`./preset-row` 子路径导出是 agent 工具面（memory 工具 + 快照 section，挂 planner 池模板 `projects/game/agent_v2/preset-templates/planner/planner/agent.cordis.yml:5-6`）。由于主入口本身也是可被 preset 组合行按裸包名挂载的合法插件，宿主模板规则（`projects/game/agent_v2/cordis.yml:138-149` templateRules）被迫在 player forbidden 中同时列出 `@dominion/dsh-memory` 与 `@dominion/dsh-memory/preset-row`——连接 memory 服务的**基建**出现在 **agent 维度**的禁止清单里。同一包"又挂 host 层、又在 agent 维度被禁"，违反了本仓库插件单挂载面的直觉边界（对照：`@dominion/dsh-team` 纯 host 面、`@dominion/dsh-saolei` 纯 preset 面，均无双面情况）。

**② 游戏结束的 player 回合在 webUI 不折叠**。062 引入终局工具结果自然收束（concludesTurn）后，player 回合以终局工具调用步收尾、无最终文本答案步。webUI 的回合折叠规则只认"最终答案步"（含非空文本且无工具调用块，`projects/game/web/frontend/src/components/ChatView.tsx:187-193` isFinalAnswer），无最终答案的回合整体保持展开（`ChatView.tsx:228-232` 注释"无最终答案的回合（失败/终止/纯工具结束）保持全部过程内容可见不折叠"）。生产实证（会话 `templates/saolei/sessions/d4f677888cd38df8897f514eadb0c0fb`，经 `GET https://game.liukexin.com/api/v2/templates/saolei/sessions/d4f677888cd38df8897f514eadb0c0fb/team/messages` 获取）：局 1 的 player 回合为 seq 2–9 共 8 步，末步（seq 9）为 THINK|TEXT|TOOL:saolei_operate，其结果 `saolei_operate → stopped at click(7,10) (lost)` 即终局收束——无最终答案步 → 整回合 8 步全部展开呈现；而 planner 复盘回合（seq 10–11）以 THINK|TEXT 步收尾，正常折叠。用户期望：终局收束的 player 回合同样折叠。

**③ saolei_remain 语义表述可被误读为"旗子数量"**。工具描述（`common/js/dsh-plugins/saolei/src/index.ts:238-246`）与玩法规则 prompt（`common/js/dsh-plugins/saolei-loop/src/index.ts:145`）均以"数字 − 相邻旗数"（number − adjacent flags）公式表述；结果体（`common/js/dsh-plugins/saolei-loop/src/game/text.ts:101-108` remainText）为 outcome 行 + 状态行 + 网格，**网格前无任何语义标注**。人类读者扫视 player 输出/工具结果时把网格数值当成"周围旗子数量"（用户实证）；模型侧同会话亦出现 remain 视图坐标转置误读（局 2 复盘：`row9, col10 = 2` 被误读为 (9,10)，真实为 (10,9)=3 还差 2 雷）。另有近义概念干扰：规则 prompt 第 139 行的全局"剩余雷数计数 = 总雷数 − 已标旗数"（经典扫雷顶部计数器）与 per-cell 的 remain 值同用"剩余雷数"词汇，加重混淆。

目标与现状的差距（本 feature 要完成的工作）：

| 维度 | 现状 | 目标 |
|---|---|---|
| memory 插件边界 | 一个包两个挂载面；host 基建包名出现在 player forbidden | 拆为两个插件：host 层基建插件（服务连接）+ agent 工具插件（插入 agent）；preset 维度规则只引用工具插件 |
| 终局回合呈现 | 终局收束的 player 回合整体展开（无折叠控件） | 与 planner 回合同型折叠：过程步折叠进"思考过程（n 步骤 · m 次工具调用）"，终局步保持可见 |
| remain 语义 | 公式化表述 + 结果网格无语义标注；与全局雷数计数器词汇混淆 | 描述/prompt/结果体三处澄清：每格值 = 该数字格周围**剩余未标记雷数**，显式排除"旗子数量"误读，并与全局计数器区分 |

## Clarifications

### Session 2026-09-14

- Q: 游戏结束的 player 回合折叠后，终局工具调用步（展示终局棋盘的末步）应该如何呈现？ → A: 过程步折叠进"思考过程"开关，终局末步整步（THINK/TEXT/工具卡片）保持可见——与 planner"最终答案步可见 + 过程折叠"同型的锚步方案。
- Q: memory 插件拆分后，两个新插件的包名应该采用哪种命名方案？ → A: 工具插件保留主名 `@dominion/dsh-memory`（planner 模板行由 `@dominion/dsh-memory/preset-row` 简化为 `@dominion/dsh-memory`）；基建插件命名 `@dominion/dsh-memory-service`（`-service` 后缀自证 host 挂载面）。
- Q: `saolei_remain` 的语义澄清范围，是否在排除"旗子数量"误读之外，同时强化坐标读法以防转置误读？ → A: 旗数语义澄清（描述/prompt/结果体三处）+ 结果体语义标注行同时锚定坐标读法（列号=x、行号=y）；网格本体格式不变，坐标强化仅限标注行一处。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - memory 插件拆分：host 基建与 agent 工具各归其位 (Priority: P1) 🎯

开发者维护 agent_v2 的插件组合。今天他要在组合清单里理解 memory 的挂载：一个包名既是 host 行又出现在 player forbidden 里，读代码时需要额外心智模型区分两个功能面。拆分后，memory 拆为两个独立插件——一个 host 层基建插件（连接 memory 服务、提供 `ctx.plannerMemory` 服务面），一个 agent 工具插件（memory 工具 + 快照 section，经 planner 池模板 preset 行插入 agent）。组合清单中 host 行只属于 host 层；preset 模板与模板规则（player forbidden / planner required）只引用工具插件——"连接服务的基建不出现在 agent 维度规则里"成为结构事实而非约定。planner 的 memory 功能（工具调用、快照注入、物化预取 fail-loud）行为零变化；player 依旧无法挂载 memory 工具。

**Why this priority**: 用户裁定的架构修正项（Input 第 1 条），是后续插件演进（新增 host 服务/新增 agent 工具）的边界基准；行为零回归使其成为纯结构重构，风险可控。

**Independent Test**: 组合面断言：host 组合清单恰含一条 memory 基建行；planner 模板恰含一条 memory 工具行（required）；player forbidden 只列 memory 工具插件（不含任何 host 基建包名）；功能回归：既有 planner memory 单测/大型测试（物化预取、快照注入、工具读写）全量通过。

**Acceptance Scenarios**:

1. **Given** 拆分后的组合，**When** 读取 agent_v2 组合清单，**Then** memory 基建以独立插件的 host 行挂载（提供 plannerMemory 服务），且该插件包名不出现在任何 preset 模板行或 templateRules 中。
2. **Given** 拆分后的组合，**When** 读取模板规则，**Then** planner required 与 player forbidden 均只引用 memory 工具插件一个包名。
3. **Given** planner 成员物化，**When** setup 执行，**Then** 快照预取经基建插件提供的服务面完成（失败仍 fail-loud 回滚整队物化），memory 工具与快照 section 经工具插件行注册——对外行为与拆分前一致。
4. **Given** 用户经 preset 创作面复制 player 模板，**When** 派生组合，**Then** 派生行不含任何 memory 行（沿用既有 copy-then-patch 语义，模板本身不含）。

---

### User Story 2 - 游戏结束的 player 回合折叠 (Priority: P1) 🎯

用户在 webUI 团队视图观察一局扫雷。player 回合多步推理后某次 saolei_operate 致终局，回合在终局工具结果处自然收束（062 语义：无最终文本答案步，turn_end 为 COMPLETED）。刷新或回填后，该回合呈现与 planner 回合同型：一个"思考过程（n 步骤 · m 次工具调用）"折叠开关，点击展开过程步骤；回合尾部的终局步（含展示终局棋盘的工具调用卡片）保持可见，用户一眼看到"这局怎么结束的"。成员视角视图（player 视角看自己的回合）同型折叠。失败/用户终止的回合保持现状整体展开（054 基线），与终局收束的完成回合明确区分。

**Why this priority**: 用户直接可感的呈现缺陷（Input 第 2 条）；终局信息（末步棋盘）与过程噪音的分层是该视图的核心可读性。

**Independent Test**: webUI 组件测试：构造无最终答案步但含已 settle 工具块的完成回合（终局收束形态），断言渲染折叠开关、计数正确（n 步骤 · m 次工具调用）、终局步可见；构造 interrupted 回合断言不折叠（回归）；既有最终答案回合折叠用例回归。

**Acceptance Scenarios**:

1. **Given** 某成员回合以终局工具调用步收尾（turn 以 completed 收束、无最终文本答案步、无 interrupted 标记），**When** 该回合经团队视图或成员视角渲染，**Then** 终局步之前的全部步骤折叠进"思考过程（n 步骤 · m 次工具调用）"开关（默认收起、可手动展开、页面会话内保持展开状态），终局步独立可见。
2. **Given** 单步终局收束回合（回合仅 1 步且即终局，如 init 即终局棋盘），**When** 渲染，**Then** 无过程可收，不渲染折叠控件，该步直接呈现（与单步最终答案回合同型）。
3. **Given** 失败或用户终止的回合（存在 interrupted 步骤标记），**When** 渲染，**Then** 保持既有整体展开呈现（零回归）。
4. **Given** 回填与实时两种到达路径（List 回填 / 流式 turn_end COMPLETED 收束），**When** 同一终局回合呈现，**Then** 折叠形态一致（实时收束后即折叠，刷新后回填仍折叠）。

---

### User Story 3 - saolei_remain 语义澄清 (Priority: P2)

player 调用 `saolei_remain` 后，工具结果网格的每个数值对读者（模型与人类）都无歧义地表达"该数字格周围**剩余未标记的雷**的数量"（= 数字 − 相邻已标旗数，可为 0 或负）。工具描述、玩法规则 prompt、结果体三处协同澄清：结果体在网格前自描述语义（读结果者不依赖外部文档即可正确解读）；prompt 与描述显式排除"旗子数量"误读，并与经典扫雷的**全局**剩余雷数计数器（总雷数 − 已标旗数）区分。工具名 `saolei_remain` 不变。

**Why this priority**: 影响模型推理正确性与人类可读性（Input 第 3 条），但发生频率低于前两项（player 大多数调用解读正确，误读集中在表述歧义处）。

**Independent Test**: 单测断言三处文本契约（工具 description、规则 section、remain 结果体首行语义标注）包含"剩余未标记雷数"语义且含旗数排除表述；既有 remain 结果格式测试更新后通过；大型测试断言 fake-llm player 消费新结果体后输出坐标/数值引用正确。

**Acceptance Scenarios**:

1. **Given** player 调用 `saolei_remain`，**When** 工具结果返回，**Then** 结果体在网格前携带语义标注行（每个数值 = 该数字格周围剩余未标记雷数 = 数字 − 相邻已标旗数；非旗子数量）且锚定坐标读法（列号 = x、行号 = y），读者无需外部上下文即可正确解读数值与格子位置。
2. **Given** 模型组装 system prompt，**When** 读取玩法规则 section 与工具 description，**Then** 两处对 remain 的表述一致且显式排除"返回旗子数量"的读法，并与全局剩余雷数计数器（总雷数 − 已标旗数）区分表述。
3. **Given** 一次真实对局复盘，**When** 用户查看 player 的 remain 调用结果与 player 的解读输出，**Then** 结果网格语义自明，人类读者不再把数值当成周围旗子数量。

---

### Edge Cases

- **拆分后的 preset 行引用面**：loader 按裸包名动态 import，基建插件包在闭包内技术上仍可被 preset 行引用——本 feature 不为基建插件增加 forbidden 守卫（用户裁定：基建不出现在 agent 维度规则里）；挂错面的风险由"preset 行清单只出自池模板 + 模板规则锁定工具插件"的结构控制（现状同等）。
- **共享代码归属**：memory 两个插件间的共享逻辑（存储 client、操作语义、快照渲染、工具定义）的包归属与依赖方向是 plan 决策；约束仅一条——工具插件注入 plannerMemory 服务（服务名与语义不变），基建插件提供之。
- **终局收束回合后紧跟排队消化回合**：消化回合是独立回合（正常模型停手 → 有最终答案步）→ 正常折叠；两回合分组互不影响（连续同成员条目按 turn 边界分组——现有分组语义保持）。
- **终局步同时含文本与工具块**（生产实证 seq 9 形态：THINK|TEXT|TOOL）：终局步作为折叠锚整步保持可见（其文本与工具卡片一并呈现），不拆分。
- **同回合多工具块**：折叠计数沿用现状口径（过程步内全部 TOOL_CALL 块计数）；终局步内的工具块计入终局步呈现、不计入过程计数（锚步不计入自身）。
- **回填侧终局判定信号**：回填消息序列不含 turn 状态字段；折叠判定以消息形态推导（无最终答案步 + 无 interrupted 标记 ⇒ 按"以工具调用收尾的完成回合"折叠）。live 侧以 turn_end COMPLETED + 无 interrupted 收束同型收敛。LLM 失败恰落在步间（无 interrupted 步）的极端形态按此规则会折叠——末步仍可见，信息损失限于过程步需点击展开，可接受。
- **remain 语义措辞语言**：工具结果体为英文（现有三工具结果体一致），语义标注行用英文；规则 prompt 玩法段为中文，两处措辞语义对齐、各自随所在文本语言。

## Requirements *(mandatory)*

### Functional Requirements

**memory 插件拆分**

- **FR-001**: memory 的 host 服务面（memory 服务连接、快照缓存、`ctx.plannerMemory` 服务提供）MUST 拆分为独立插件 `@dominion/dsh-memory-service`（独立包，命名见 Clarifications 2026-09-14），仅经 host 组合层挂载；该插件包名 MUST NOT 出现在任何 preset 模板组合行或模板规则（required/forbidden）中。
- **FR-002**: memory 的模型面（memory 工具 + 快照 section，现 `./preset-row` 面）MUST 拆分为独立插件 `@dominion/dsh-memory`（继承主名，planner 模板行由 `@dominion/dsh-memory/preset-row` 简化而来），作为 agent 维度唯一的 memory 插件行：planner 池模板 required 恰含其一；player 模板 forbidden 恰含其一（不再列任何 host 基建包名）。
- **FR-003**: 拆分 MUST 保持对外行为零变化：plannerMemory 服务名与语义、工具契约（名称/参数/结果语义）、快照 section 行为、物化预取 fail-loud 语义、编排层 `loadPlannerMemory` seam 均不变；组合三面（package manifest ⟷ 组合清单 ⟷ 物化闭包）原子变更。

**终局回合折叠**

- **FR-004**: webUI（团队视图与成员视角视图共用回合呈现）MUST 将"以工具调用步收尾的完成回合"（无最终文本答案步、无 interrupted 标记的成员回合）按与最终答案回合同型的折叠规则呈现：终局步（末步）独立可见，此前全部步骤默认折叠进"思考过程（n 步骤 · m 次工具调用）"开关，手动展开在页面会话内保持。
- **FR-005**: 折叠判定 MUST NOT 改变既有回合边界：连续同成员 AGENT 条目构成一个回合（USER 或另一成员条目断开）的分组语义保持；单步回合（终局步即首步）不渲染折叠控件。
- **FR-006**: 失败/终止回合（含 interrupted 标记的回合）MUST 保持既有整体展开呈现，零回归；实时（流式收束）与回填（List 消息）两条到达路径的折叠形态 MUST 一致。

**remain 语义澄清**

- **FR-007**: `saolei_remain` 结果体 MUST 在网格前自描述数值语义：每格值 = 该数字格周围剩余未标记雷数（= 数字 − 相邻已标旗数，可为 0 或负），并显式声明不是旗子数量；该语义标注行 MUST 同时锚定坐标读法（列号 = x、行号 = y，对齐 `saolei_operate` 的 `(x, y)` 参数）。工具名不变，既有结果体结构（outcome 行 / 状态行 / board size 行 / 网格）保持；语义标注行插入于 `board size` 行与网格之间，不得破坏既有字段消费面；网格本体格式不变。
- **FR-008**: 工具 description 与玩法规则 prompt 的 remain 表述 MUST 更新为同一无歧义语义（剩余未标记雷数，显式排除旗数误读），且 MUST 与全局剩余雷数计数器（总雷数 − 已标旗数）明确区分；`saolei_operate` / `saolei_init` 的描述与结果体不受本条影响。

### Key Entities

- **memory 基建插件**（拆分产物，`@dominion/dsh-memory-service`）：host 组合行插件——memory 服务 gRPC 连接、per-agent 快照缓存、`plannerMemory` 服务提供（load/写路径）；仅 host 层挂载。
- **memory 工具插件**（拆分产物，`@dominion/dsh-memory`，继承主名）：preset 组合行插件——注册 memory 工具与快照 section，注入 `plannerMemory` 服务；agent 维度唯一 memory 行。
- **终局收束回合**：经 062 concludesTurn 语义收束的成员回合——末步含已 settle 的终局工具调用块、无最终文本答案步、无 interrupted 标记；本 feature 赋予其与最终答案回合同型的折叠呈现。
- **remain 视图**：`saolei_remain` 的结果网格——每格值 = 对应数字格周围剩余未标记雷数（数字 − 相邻已标旗数，可为 0/负），非旗子数量；本 feature 使其三处表述（description / 规则 prompt / 结果体）自明且一致。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 组合面断言全绿：host 组合清单恰含一条 memory 基建行；planner 模板恰含一条 memory 工具行；player forbidden 仅含 memory 工具插件一个包名（无 host 基建包名）；preset 维度（模板行 + 规则）零基建包名引用。
- **SC-002**: 行为零回归：既有 memory 插件单测、agent_v2 组合/物化测试（含 planner memory 预取 fail-loud）、team 大型测试全量通过；服务接口（plannerMemory 服务、memory 工具契约）消费方零改动。
- **SC-003**: webUI 组件测试：终局收束回合（生产实证形态：末步 THINK|TEXT|TOOL 且工具结果为终局）渲染折叠开关、计数与末步可见性正确；interrupted 回合与最终答案回合既有用例回归通过；团队视图与成员视角双视图覆盖。
- **SC-004**: 文本契约测试：remain 结果体含语义标注行（含旗数排除表述与坐标读法锚定）；工具 description 与规则 section 的 remain 措辞断言更新后通过；三处表述语义一致性由同批测试锚定。
- **SC-005**: 大型测试闭环：以 062 既有终局收束场景为基（终局后仍有后续脚本步骤的 player），webUI 侧终局回合折叠呈现进入断言面（或以组件测试等效锚定），全部用例通过。

## Assumptions

- **拆分不引入第三包**：两插件间的共享逻辑归属（基建插件导出、工具插件依赖之，或反之）为 plan 决策；不预设共享核独立成包，除非 plan 论证必要。
- **服务名稳定**：`plannerMemory` 服务名跨拆分保持（编排层 seam 与宿主 `ctx.get("plannerMemory")` 消费面零改动，`projects/game/agent_v2/src/session.ts:828`）。
- **折叠锚 = 末步**：终局收束回合以末步（终局工具调用步）整步为可见锚（THINK/TEXT/工具卡片一并呈现，不拆块）——用户裁定（Clarifications 2026-09-14）；不折叠全部内容（终局棋盘是最有信息量的呈现）。
- **工具不重命名**：`saolei_remain` 名称保持（重命名牵动契约面与记忆中已固化的策略文本，非本 feature 义务）。
- **提示词既有策略文本兼容**：planner 长期记忆中已固化的"先跑 remain 核验"纪律引用的是工具名与用途，语义澄清不使其失效。
- **测试基建联动**：fake-llm player 消费新 remain 结果体、webUI 组件测试新增终局折叠用例随实现同批落地（constitution 原则 VI：大型测试全量通过作为验收）。
- **062 无痕性不破坏**：终局收束回合的折叠是纯前端呈现逻辑，不改变 session log/turn_end 语义（FR-003/FR-006 的零回归边界）。
