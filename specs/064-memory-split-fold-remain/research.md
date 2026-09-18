# Research: memory 插件拆分 + 终局回合折叠 + remain 语义澄清

> Phase 0 输出。证据锚点：源码行号均在当前 HEAD；生产会话证据取自
> `GET https://game.liukexin.com/api/v2/templates/saolei/sessions/d4f677888cd38df8897f514eadb0c0fb/team/messages`
> （2026-09-14 查询，32 条归并消息，局 1–4）。

## D1: 拆分架构——域核心归属 service 包，工具包依赖之

**Decision**：`common/js/dsh-plugins/memory/` 拆为两个 workspace 包：

- `common/js/dsh-plugins/memory-service/`（`@dominion/dsh-memory-service`）：域核心 + host 服务面——`client.ts`（MemoryClient gRPC）、`operations.ts`（工具操作语义核心）、`snapshot.ts`（快照渲染 + section 标识常量）、`service.ts`（PlannerMemoryService 接口与实现）、`index.ts`（host 行：`provide("plannerMemory")`，无 inject）。re-export 域核心类型/常量供工具包消费。
- `common/js/dsh-plugins/memory/`（`@dominion/dsh-memory`，语义重载为纯工具面）：`tool.ts`（memory 工具定义）+ `index.ts`（agent 行：`inject = ["plannerMemory", "tools", "systemPrompt"]`，注册工具 + 快照 section，即现 `preset-row.ts` 内容升为主入口）。package.json 依赖 `@dominion/dsh-memory-service: workspace:*`（`PlannerMemoryService` 类型、`MEMORY_ACTIONS`/`MemoryToolArgs` 校验常量、snapshot section 常量），删除 `./preset-row` 子路径导出（终态无兼容垫片，宪法 VII）。

**Rationale**：
1. `service.ts` 的 `load` 调用 `renderMemorySnapshot`（snapshot.ts）、`applyCall` 调用 `applyMemoryCall`（operations.ts）——service 面是域核心的完整消费者，核心随提供方走（cordis 惯例：服务接口与数据类型归属 provider，对照 `@dominion/dsh-team` 拥有 `ctx.team` 契约）。
2. 工具包跨包面最小：`tool.ts` 仅需 `PlannerMemoryService`（type）+ `MEMORY_ACTIONS`/`MemoryToolArgs`（校验）；行入口仅需 snapshot section 常量。依赖方向 = 消费者 → 提供者，与 cordis inject 语义同向。
3. 运行时仍零耦合：工具行经 `inject: ["plannerMemory"]` 按名解析 host 行提供的服务（isolate 边界语义保持，059 T023 实测修订不动）。

**Alternatives considered**：
- 域核心放工具包、service 包反向依赖（依赖倒置）——提供方（infra）依赖模型面包，方向别扭；且 `client.ts`（gRPC）天然属 infra 侧，核心整体搬家必然拆散 `operations/snapshot`，被否。
- 工具包零依赖（自持结构化类型 + 复制常量，orchestrator 先例 `LoadPlannerMemory`）——orchestrator 只需一个函数签名故结构化即可；工具包需要 `MEMORY_ACTIONS` 等域常量，复制引入漂移风险，被否。
- 第三共享核心包——两包间共享面太小（约 3 个导入点），不必要（spec Assumption 已排除优先）。

## D2: 折叠判定规则——"无最终答案步且无 interrupted 标记 ⇒ 末步锚折叠"

**Decision**：`projects/game/web/frontend/src/components/ChatView.tsx` 的 `CompletedTurn` 折叠判定扩展为两段：

1. 先按现状找最终答案步（非空 text、无 toolCall、非 interrupted）——命中则以之为锚（现状）。
2. 未命中且回合内**所有**消息均无 `interrupted` 标记（完成收束回合，062 终局形态）⇒ 以**末步**为锚：`process = messages[0..anchor)` 折叠进"思考过程（n 步骤 · m 次工具调用）"，末步整步可见。
3. 未命中且存在 `interrupted` 标记（失败/终止回合）⇒ 保持现状整体展开（054 基线）。
4. 锚步序号 ≤ 0（单步回合）不渲染折叠控件（现状规则自然覆盖）。

**Rationale**：
- store 既有终态语义直接导出一致性：live 路径 `turn_end{COMPLETED}` → `closeLiveTurn(…, false)`（无 interrupted 投影），`ERROR`/`CANCELED` → `closeLiveTurn(…, true)`（尾步 `interrupted: true`，`projects/game/web/frontend/src/store/chat.ts:671-695`）；回填路径服务端 HistoryMessage 同型标记（054 data-model §1.5）。两条到达路径（FR-006）无需新增信号即收敛。
- 不引入服务端 API 变更（HistoryMessage 无 turn 状态字段，加字段是更大的契约面改动）。

**既有测试影响（必须同批修正，否则规则翻转即红）**：
- `ChatView.test.tsx:504`（无最终答案回合全可见 + 陈旧 RUNNING 中断推导）：fixture 无 interrupted 标记但含 RUNNING-无-result 陈旧工具块——该形态语义上是异常收束（正常完成回合结果必达），fixture 补 `interrupted: true` 于尾步以归入失败桶，断言不变。
- `ChatView.test.tsx:542`（注入失败回合）：同理尾步补 `interrupted: true`，断言不变。
- 新增用例：终局收束回合（末步 THINK|TEXT|TOOL 且工具块 SUCCEEDED 带 result、无 interrupted）折叠 + 末步锚可见；单步终局回合不折叠。

**Alternatives considered**：
- 服务端在 history 消息加 turn 终态字段——契约面大、回填/实时双路径改造，收益仅是消除"LLM 失败恰落步间（无 interrupted 步）误折叠"极端形态（该形态末步仍可见，损失限于过程需点击展开，spec Edge Cases 已裁定可接受），被否。
- 按工具结果内容判定终局（前端解析 tool result 文本找 won/lost）——前端解析模型可见文本契约，脆弱且越界，被否。

## D3: 组合三面原子变更清单（package.json ⟷ cordis.yml ⟷ BUILD/物化）

**Decision**（`specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md` §4 既有模式）：

1. `projects/game/agent_v2/package.json`：dependencies 增 `"@dominion/dsh-memory-service": "workspace:*"`（`@dominion/dsh-memory` 保留）。
2. `projects/game/agent_v2/cordis.yml`：
   - host 行（:155）`name: '@dominion/dsh-memory'` → `'@dominion/dsh-memory-service'`；
   - templateRules（:138-149）player.forbidden `['@dominion/dsh-memory','@dominion/dsh-memory/preset-row']` → `['@dominion/dsh-memory']`；planner.required `'@dominion/dsh-memory/preset-row'` → `'@dominion/dsh-memory'`；
   - 头部注释（:11）`@dominion/dsh-memory/preset-row` 裸名解析说明同步。
3. `projects/game/agent_v2/preset-templates/planner/planner/agent.cordis.yml:6`：行名 → `'@dominion/dsh-memory'`（preset-templates 经 `projects/game/agent_v2/BUILD.bazel:95,190` 的 glob 进入镜像，行名变更随镜像重建生效）。
4. `projects/game/agent_v2/BUILD.bazel`：runtime_deps 增 `"//common/js/dsh-plugins/memory-service:runtime_pkg"`（memory:runtime_pkg 保留——工具行仍按裸名从宿主闭包解析）。
5. 新包 `common/js/dsh-plugins/memory-service/BUILD.bazel`：同 memory 包形态（ts_project + js_library pkg + js_runtime_library runtime_pkg（package_name `@dominion/dsh-memory-service`，npm_deps = grpc-js/proto-loader）+ vitest_test）；memory 包 BUILD 收缩（npm_deps 去 grpc 系、runtime_deps 去 resolver/logs、加 memory-service:runtime_pkg；ts_project deps 调整）。gazelle 生成后按需补 runtime_pkg target（AGENTS.md 惯例）。

**Rationale**：058 composition-manifest §4 三面原子律；workspace 包仅走 runtime_deps 通道（agent_v2 BUILD.bazel :100-110 既有裁定）。

**测试同步**：`projects/game/agent_v2/src/dsh.test.ts:541-545,711-725` 断言名更新；`projects/game/agent_v2/README.md:163` 模板守则描述更新；`common/js/dsh-plugins/preset-authoring/src/*.test.ts` 中的 memory 包名为不透明 fixture 字符串，语义不变，随 T003 对齐新名（T008 的 `dsh-memory/preset-row` 终态零命中判据要求）。

## D4: remain 语义标注——结果体 legend 行 + 三处表述同步

**Decision**：
1. `common/js/dsh-plugins/saolei-loop/src/game/text.ts` `remainText`：在 `board size <w>*<h>` 行之后、网格之前插入 legend 行（英文，与既有结果体一致），语义 = 每格值是该数字格周围**剩余未标记雷数**（= 数字 − 相邻已标旗数，可为 0 或负）、非旗子数量、列号=x 行号=y（对齐 `saolei_operate` 参数）。既有结果体结构（outcome/状态/board size/网格）与网格本体格式不变。
2. `common/js/dsh-plugins/saolei/src/index.ts:238-246` 工具 description：主语义前置为"剩余未标记雷数"，显式排除旗数误读，保留公式作派生说明。
3. `common/js/dsh-plugins/saolei-loop/src/index.ts:139,145` 规则 prompt：145 行 remain 表述同向改写；139 行全局剩余雷数计数表述保留但加区分（明确它是顶部计数器概念、与 per-cell remain 不同）。
4. `common/js/dsh-plugins/saolei/README.md` 与 `SAOLEI_GUIDANCE`（:317）工具条目同步。

**Rationale**：三处协同（FR-007/FR-008）；legend 落在网格前使结果自描述（模型与人类读者都不依赖外部文档）；坐标锚定直击生产实证的转置败因（局 2：`row9, col10` 误读为 (9,10)）。

**测试影响**：`runtime.test.ts:582` 断言 `toContain("saolei_remain → computed\ngame status: playing")` 前缀不破坏（legend 在其后）；补 toContain legend 关键词断言。fake-llm 关键词匹配仅扫 user turn 文本（`projects/game/fake-llm/service/handler.go:67-70`），结果体变更不影响匹配；`projects/game/testplan/` 无 saolei_remain 文本断言（grep 零命中）——大型测试仅回归验证。

**Alternatives considered**：重命名工具（如 `saolei_mines_left`）——契约面与 planner 长期记忆中已固化引用全动，spec Assumption 已排除；网格行尾重复行号（C 选项）——网格本体契约变更面大，被用户裁定否（Clarifications 2026-09-14）。

## D5: 契约文档修订策略——064 新契约 + 旧契约加修订注记

**Decision**（060 修订 059 team-api 的先例模式）：
- 新增 `specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`（memory 插件拆分终态契约，两包清单/inject/provide/模板规则终态）、`contracts/web-ui.md`（CompletedTurn 折叠规则修订）、`contracts/saolei-plugins.md`（remain 结果体与表述修订）。
- 旧契约加顶部修订注记指向 064：`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §3/§5（memory 形态与组合行）、`specs/054-agent-v2-bugfixes/contracts/web-ui.md` §2.2（折叠规则，经 tasks 阶段补注记）、`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2.2 与 `specs/051-agent-v2-dsh-migration/data-model.md` §2.5（结果文本契约，remain 条目）、`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §1/§2（两处 remain 表述）。
- 涉改代码的注释引用同步重指向 064 契约（memory 双包文件、ChatView.tsx、text.ts、saolei/index.ts）。

**Rationale**：宪法 I（引用可溯）+ VII（终态表述）；既有 spec 文档是被代码注释引用的活契约，修订注记保持单一代码注释锚点（旧文档不整体重写，历史 spec 语义由其自身版本承载）。

## D6: 验证面与阶段划分建议（供 tasks 参考）

**Decision**：三个独立可验证切面 + 收尾：
- **Phase A（拆分）**：双包落地 + 组合三面 + 单测迁移（client/operations/service → memory-service；tool/preset-row→index → memory）；验证 = `bazel build //...` + 两个包 vitest + agent_v2 dsh.test.ts。
- **Phase B（折叠）**：CompletedTurn 规则 + 既有 fixture 修正 + 新用例；验证 = web frontend vitest。
- **Phase C（remain）**：legend + 三处表述 + 断言；验证 = saolei-loop/saolei vitest。
- **Phase D（收尾）**：契约文档 + 修订注记 + README + 注释重指向 + 大型测试全量执行（`guitar run`，宪法 VI：部署→测试→清理闭环、全用例通过）。

大型测试回归面：`projects/game/testplan/system_test.yaml` 的四个 suite（`game-system` / `game-disconnect` / `game-memory-down`（`agent_v2_memory_down_test.go` 所在的 memory 服务缺失编排）/ `game-stall`）——拆分不改服务目标，仅插件包重组，预期零影响、全量跑通即验收。

**Rationale**：宪法 IV（编译+单测随变更）+ VI（大型测试验收）；三切面无相互依赖，可独立中断恢复。

## D7: live 进行中回合提前折叠（T004 回归）——条目级 `open` 标记 + 回填接受折叠

**缺陷链**（部署验证发现，代码实读核实）：team 流中每个 step 完成即经 `team_message` 帧固化入归并序列（history）并推进 live 回合的 `fixedSteps`（`projects/game/web/frontend/src/store/chat.ts` teamMessage 分支 + `consumeFixedStep`）；live 组件只渲染未固化尾步（`steps.slice(fixedSteps)`，全展开），已固化步骤走 history 分组（连续同成员 AGENT 条目）→ `CompletedTurn` 三分类。进行中回合的已固化前缀（各步带 toolCall → 无最终答案；COMPLETED 路径无 interrupted；步数 > 1）与已收束终局回合消息形态**同形** → 局中每固化一步、新末步成为锚、此前步骤立即折叠。旧规则（仅最终答案折叠）下该路径天然安全：最终答案步只出现在回合最后一个 step，折叠时机与收束重合——T004 的第三分类首次让"无答案形态"在回合打开期间可折叠，属 T004 引入的回归。违反 contracts/web-ui.md §2 不变量"流式进行中的回合保持全展开"（官方 "Turn Process Folding"：rows "remain expanded while a Turn is open"，https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/README.md ）。

**Decision**：

1. **live 路径 = 条目级 `open` 标记**：store 在 `team_message` 归约时，若该成员存在打开的 live 回合（`live.some(t => t.member === member)`），则为固化的归并序列条目与成员视角条目落 `open: true`；回合收束全路径清除（`closeLiveTurn` 含全部步已固化的早退路径、`closePendingTurns` 兜底；`loadHistory` 防御清除 memberHistory 残留）。分组层（`TeamMessages`/`MemberMessages`）将含 `open` 条目的分组整组按流式语义展开（无折叠控件、不进 `CompletedTurn`、工具块流式语境）。`CompletedTurn` 输入不变（HistoryMessage[]）——三分类保持"已收束回合的消息形态纯函数"。
2. **回填路径 = 接受折叠**：进行中回合与"终局收束后、下一成员条目到达前"genuinely 同形（序列尾部、无最终答案、无 interrupted、步数 > 1），无服务端信号可区分；接受折叠（末步锚可见、过程一键展开，信息损失有界），与 spec Edge Cases"回填侧终局判定信号：折叠判定以消息形态推导"裁定同向。局中刷新后重连（下次 Send 建流）：在途回合错过的 `turn_start` 不重放（059 team-api §3.4），其剩余步骤的 `team_message` 帧无 live 回合可标记 → 延续回填裁定（保持折叠）至该回合收束，后续回合恢复 live 展开语义。
3. `open` 分组工具块按流式语境（RUNNING 无 result → 执行中）；标记清除后恢复历史语境中断推导——修复"展开后已固化步的执行中工具呈现已中断"的语境错位。

**Rationale**：

- 信号归属：回合生命周期（turn_start/turn_end）只有 store 观察得到——标记在归约时落定是唯一不依赖渲染期猜测的锚；分组层只消费标记，`CompletedTurn` 保持纯形态分类（分层：生命周期知识在 store/分组层，形态分类在 `CompletedTurn`）。
- 条目级标记覆盖"局中用户消息插队拆分分组"：用户消息 enqueue 即固化（seq 插入回合中段）会把进行中回分组拆成多段，条目级标记天然覆盖全部分段（组件层"尾部分组"启发式覆盖不了非尾段）。
- 迟到帧窄边界（已收合回合的 team_message 晚于该成员下一回合 turn_start 处理且以新条目插入 → 误标 open）：差异仅限临时展开、随下一回合收束自愈，可接受（contracts/web-ui.md §4）。

**既有测试影响**：T004 的实时路径新用例走单用户会话流（`store.send` 成员事件帧、无 `team_message` 帧，steps 只在 turn_end 进 history）——团队流"逐步固化"路径未覆盖，为本缺陷漏测面；修复同批补齐（team 流全生命周期用例：固化期间不折叠 → turn_end 后折叠）。既有 store 用例经核对零改动：`team_message` 固化期间标记、收束时清除，终态形态与既有断言（含 `chat.test.ts` `toEqual` 整形断言）一致。

**Alternatives considered**：

- 分组层以 live 推断（"成员存在 live 回合 → 尾部分组展开"）：局中用户消息插队拆分后**非尾段**仍提前折叠；"该成员全部分组展开"会误展开历史已收合回合（每开新回合闪现展开旧回合）→ 否。
- `CompletedTurn` 增 `open` 输入 prop：分类函数输入不再纯消息形态（破坏 §3 "分类是消息形态的纯函数" 契约面），且 `CompletedTurn`（名字即"已完成回合"）渲染进行中回合名实不符 → 否（分组层路由更符合分层）。
- 服务端加回合状态字段（HistoryMessage 或 team_message 帧）：D2 已否决（契约面大、回填/实时双路径改造）；live 信号客户端可自足派生，重提无必要 → 维持否决。
- 回填"会话尾部分组不折叠"特判 / 时间窗启发式：前者误伤"终局后、下一成员条目到达前"的合法折叠瞬间（FR-006 回填折叠一致性，终局后刷新必须折叠）；后者非确定性行为不可测 → 否。
