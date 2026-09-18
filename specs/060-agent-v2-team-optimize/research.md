# Research: Agent v2 team 模式优化

> **状态**：Phase 0 完成。全部技术决策已定（无 NEEDS CLARIFICATION 残留）。
> **执行期用户指令**（US4）：先找到实时缺陷的引入根因再修复；找不到则停下说明。**两个根因均已定位并实证**（R5/R6），修复方案与根因一一对应。

---

## R1 产物位置保留环境变量：`DOMINION_ARTIFACT_DIR`（新增）

- **Decision**：deploy 平台为每个 artifact 服务注入新保留环境变量 **`DOMINION_ARTIFACT_DIR`**，值 = 打包工具放置该服务产物的目录（`/dominion/{app}/{service}`，`tools/release/deploy/README.md` §镜像布局 146-156 行）。注入点：`projects/infra/deploy/runtime/k8s/builder.go` 的保留变量块（stateful 约 L160-163、stateless 约 L335-338，`SERVICE_APP`/`DOMINION_ENVIRONMENT`/`POD_NAMESPACE` 之后追加），语义对齐既有保留名（平台值追加在用户 env 之后，K8s last-wins 覆盖用户同名声明）；保留名清单补入 `tools/release/deploy/README.md`（467 行清单处）与文档说明；`projects/infra/deploy/runtime/k8s/executor.go` 的 `ReservedEnvironmentVariableNames`（部署校验所读的保留名清单）同步追加。
- **Rationale**：现状**无**任何声明产物位置的变量（保留变量仅 `SERVICE_APP`/`DOMINION_ENVIRONMENT`/`POD_NAMESPACE`/`TLS_*`/`S3_*`/`DOMINION_SECRET_DIR`/`DOMINION_CONFIG_DIR`，builder.go L23-70 实证）——用户"如果没有则添加一个"的条件成立。命名对齐既有 `_DIR` 后缀词形（`DOMINION_SECRET_DIR`/`DOMINION_CONFIG_DIR`）。deploy env 为纯字符串（schema `additionalProperties: {type: string}`，无插值）→ 推导只能发生在服务代码内（R3）。
- **Alternatives**：`DOMINION_ARTIFACT_ROOT`（词形与既有 `_DIR` 不一致）；服务自带打包路径常量（回到硬编码不稳定前缀，被用户明确否定）。
- **引用**：`projects/infra/deploy/runtime/k8s/builder.go`；`projects/infra/deploy/runtime/k8s/executor.go`；`tools/release/deploy/README.md`；`tools/release/deploy/pkg/schema/deploy.schema.json`。

## R2 preset 派生机制：store 唯一事实源 + 使用时临时组合文件 + 官方 `mountPreset`

- **Decision**：用户创作 preset 的组合文件副本不再作为被维护对象：
  - **CRUD store-only**：Create/Update/Delete 只写 Mongo store（Create 时模板行校验 `templateRules` 保留——模板是镜像内文件，读模板组合校验不变）；
  - **compose 使用时派生**：`compose(presetId)` = store.get → 读模板组合（roster.resolve(template) 的文件）→ 以 store 记录的 persona 替换 persona 行 `config.text`（空 persona = 模板默认 base，语义同现状空值回退）→ 写**临时组合文件**（`mkdtemp` under `os.tmpdir()`，仅 `agent.cordis.yml`）→ 构造合成 `AgentPreset {id, trust: 'user', path}` → setup 内经官方 **`mountPreset(agentCtx, preset)`**（`@deepseek-ai/dsh-agent-presets` 根导出，mount.d.ts 实证导出）挂载。临时文件用完即弃（进程生命周期内不删亦无害；任何时刻删除不影响 store 与下次 compose 的幂等重建）；
  - **roster roots 收缩**：`cordis.yml` 的 roots 只剩两个模板 system 根（user root 条目删除，`PRESET_WRITABLE_ROOT` 语义消亡）；roster 仍服务模板发现/校验；`copy`/`remove` 创作面不再使用。
- **Rationale**：官方 `mountPreset` 的实现（mount.js L317-348）只消费 `preset.path`（`config = {path: pathToFileURL(preset.path).href}` → `agentCtx.plugin(PresetTree, config)`），**不要求 preset 被 root 扫描发现**——合成对象可直接挂载，完整保留官方挂载保障（inactive-rows 检查、root-realm 泄漏检查、scope 隔离）。同时官方面**没有内存组合 API**（`AgentPreset.path` 必填文件路径）——用户偏好序"最好内存直用"的条件不成立，落在其明确批准的第二分支"纯当临时文件、不维护"。每次物化一次挂载、fiber 随 agent 卸载（mount.d.ts："The subtree is owned by agentCtx's fiber"）——与 per-team 物化生命周期天然对齐，规避 standing-mount 跨 team 共享语义的额外论证。
- **Alternatives**：① 内存行挂载（绕过 roster 逐行 `ctx.plugin(persona/saolei/memory)`）——无官方组合挂载 API，自研逐行挂载丢失 loader 审计/inactive-rows/泄漏防护，且模板行 config（如 memory preset-row）需另行硬编码；被否决（用户偏好序第二分支明确可用时优先官方路径）。② 维持 copy-then-patch 双写（被本 feature 否决的维护成本）。③ 临时文件放 `PRESET_WRITABLE_ROOT` 类专有路径（用户已否定专有路径；`os.tmpdir()` 为通用临时目录语义）。
- **引用**：`@deepseek-ai/dsh-agent-presets` [lib/types/mount.d.ts（unpkg 0.1.1-rc.2）](https://unpkg.com/@deepseek-ai/dsh-agent-presets@0.1.1-rc.2/lib/types/mount.d.ts)、[lib/types/index.d.ts（unpkg）](https://unpkg.com/@deepseek-ai/dsh-agent-presets@0.1.1-rc.2/lib/types/index.d.ts)（pnpm store 0.1.1-rc.2 实证）；`common/js/dsh-plugins/preset-authoring/src/index.ts`（compose/rebuildCopy 现状）、`src/materialize.ts`（被替换的 copy-then-patch）；spec FR-003、Clarifications「用户偏好顺序」。

## R3 agent-v2 模板根推导与部署清单收敛

- **Decision**：`projects/game/agent_v2/src/dsh.ts` 新增模板根解析：`PRESET_TEMPLATES_ROOT`（显式覆盖，本地/测试形态）> `${DOMINION_ARTIFACT_DIR}/preset-templates`（平台注入派生）> 两者皆缺 boot fail-loud（对齐 roster 根解析失败的既有 fail-loud 语义，`specs/058-dsh-preset-roster-demo/quickstart.md` boot 失败排查同型）。解析结果经既有宿主注入模式写入组合配置（cordis.yml 的 `!!js process.env.PRESET_TEMPLATES_ROOT` 表达式不变——默认值由 dsh.ts 在 boot 前写入 env，声明式读法零改动）。`projects/game/deploy.yaml` agent-v2 env 块与 `projects/game/testplan/deploy_agent_v2*.yaml`（三份）的 `PRESET_TEMPLATES_ROOT`/`PRESET_WRITABLE_ROOT` 全部移除（非必要不配置；agent-v2 env 块仅剩 secret 绑定）。
- **Rationale**：打包布局 `/dominion/{app}/{service}` 由打包工具决定（`artifact_pkg_js` data 分发），部署清单硬编码该前缀不稳定（用户指出）；平台变量派生使镜像布局调整不再波及部署清单。显式覆盖保留本地/测试可用性（现状单测/大型测试经 env 注入测试目录，`projects/game/agent_v2/src/dsh.test.ts`）。
- **Alternatives**：deploy env 支持变量插值（schema 改动 + 平台语义扩张，非必要）；部署清单写相对路径（打包工具无此约定）。
- **引用**：`projects/game/agent_v2/cordis.yml` L62-73（roster 行现状）；`projects/game/deploy.yaml` L34-41；`projects/game/testplan/deploy_agent_v2.yaml` L68-69（另两份同型）；`projects/game/agent_v2/BUILD.bazel` L84-90（打包 data 布局）。

## R4 常量库（Go + JS）与采用范围

- **Decision**：新增 `common/gopkg/constants`（Go 包）与 `common/js/constants`（npm 包 `@dominion/common-js-constants`，对齐 `common/js/*` 包形态与 catalog 管理）。首批收录 deploy 平台保留环境变量名全集：`SERVICE_APP`、`DOMINION_ENVIRONMENT`、`POD_NAMESPACE`、`TLS_CERT_FILE`、`TLS_KEY_FILE`、`TLS_CA_FILE`、`TLS_SERVER_NAME`、`S3_ACCESS_KEY`、`S3_SECRET_KEY`、`DOMINION_SECRET_DIR`、`DOMINION_CONFIG_DIR`、**`DOMINION_ARTIFACT_DIR`（新增）**。采用范围（spec FR-005 + Clarifications 2026-09-11 裁定）：builder.go 的保留变量定义与注入、`tools/release/deploy` 文档/校验中的保留名引用、`projects/game/` 下消费点（agent_v2 的 `DOMINION_ENVIRONMENT`（presets.ts）、`DOMINION_SECRET_DIR`（dsh.ts）、新增 `DOMINION_ARTIFACT_DIR`）。**common 既有公共包不改**（它们自身是其领域常量的权威来源；收录原则 = 一致性与避免冗余，非机械收集）。
- **Rationale**：保留名散落定义于 builder.go 常量块与各消费方字面量（gopkg config/otel/solver/mongo/grpc/s3、js config/resolver/otel、agent_v2 等，rg 实证 20+ 文件）；deploy 工具/服务与 game 服务是注入与消费的两端，统一引用消除拼写漂移。Go 侧 builder 引入 common/gopkg/constants 需确认依赖方向（projects/infra/deploy → common/gopkg，与既有 common 依赖同向，无环）。
- **Alternatives**：常量收进单一现成包（如 gopkg/config）（领域错位——config 包成为全仓常量倾倒场，违背"一致性非机械收集"裁定）。
- **引用**：spec FR-004/FR-005、Clarifications 2026-09-11 第 1 条；`common/gopkg/*`、`common/js/*` 目录形态；`projects/infra/deploy/runtime/k8s/builder.go` L23-70。

## R5 US4 缺陷一（成员视角实时丢失用户输入）——根因与修复

- **根因（实证）**：**059 的设计缺口**——帧词汇不含"成员消费输入"的实时事件。
  1. `specs/059-agent-v2-team-mode/contracts/team-api.md` §3.2 定义双帧承载：成员事件帧 + `team_message` 帧；用户消息仅在 **Send 接受时**以 `team_message{member="user"}` 进入**归并序列**（§3）。
  2. 服务端成员视角的写入点存在且正确：编排驱动成员注入消息 → 成员 log 记录 `user/message` → `projects/game/agent_v2/src/history.ts` `appendMemberViewUser`（L585-590）写成员视角——但**该时刻无任何流帧扇出**。
  3. 前端按 059 契约实现为回填-only：`projects/game/web/frontend/src/store/chat.ts` 对 `member === "user"` 的 team_message 帧只入归并序列（L476-479 注释"消费锚只有服务端历史可见，故经 ListMemberMessages 回填"）；`App.tsx` 的 `runMemberBackfill` 仅在挂载/回合静止（live→空迁移）/Apply 后触发。
  4. **引入 commit**：`d3fb732`（059 phase 3，契约与 server 面）/`4dff297`（059 phase 6+7，web 成员视角）。非"改坏了什么"，而是契约设计时以"前端无消费事实，不伪造"为由选择了回填通道，遗漏了"消费事实可以由服务端成帧实时推送"这一选项。
- **修复（与根因一致——补上缺失的帧，不触碰既有回填收敛）**：服务端在 `appendMemberViewUser` 写入成员视角的同时扇出**新 team 级帧 `member_view`**：`{member, sender, message}`（member=消费该输入的成员；sender=来源标注：`"user"` 或广播发送成员 role；message=该成员视角的消息投影，与 ListMemberMessages 元素同构）。前端 store 归约：按 `member` 追加进 `memberHistory[member]`（messageId 幂等，与 `appendMemberView` 同型），不触碰归并序列/回填路径。契约修订见 [contracts/team-api.md](contracts/team-api.md) §3.2。
- **为什么不是别的修法**：让前端在 `team_message{user}` 时向全部成员视角写入 = 伪造消费（违背 059"消费前不出现"语义，US4 场景 2 明确要求保持）；改用轮询 = 违背 team 流承载裁定（059 R8 已否决 List 面定时拉取）。
- **引用**：`projects/game/agent_v2/src/history.ts` L581-590；`projects/game/web/frontend/src/store/chat.ts` L440-486；`projects/game/web/frontend/src/App.tsx` L157-205；059 contracts/team-api.md §3；git log `4dff297`/`d3fb732`。

## R6 US4 缺陷二（工具完成后结果/状态不及时更新）——根因与修复

- **根因（实证）**：**059 phase 3 渲染枢轴未同步 settle 路径**——工具块的可见渲染副本已从 live 草稿移到归并序列条目，但 `tool_result` 归约仍在命中 live 草稿后提前返回。
  1. **057（commit `2a3c4d4`）建立现行归约形态**：`toolResult` 分支 = live 草稿 settle 优先 → **命中即 early-return** → 未命中才回退 history。该形态在 057 的单 agent 渲染模型下**正确**：在途回合的可见渲染就是 live 草稿，回合结束时 `stepsToHistory` 从（已 settle 的）live 草稿投影入历史，终态自然携带。
  2. **059 phase 3（commit `d3fb732`）引入 `team_message` 帧 + `consumeFixedStep`**：服务端在每 step 落定（`assistant/message`）即扇出 `team_message` 帧，前端把该 step **立即固化进归并序列与成员视角**（与 List 同源），live 草稿的前导 step 经 `fixedSteps` **跳过渲染**（`TeamMessages` 的 `steps.slice(turn.fixedSteps)`）。工具块的**可见副本由此变为归并序列条目**（其 toolCall 块由 `blockToContentBlock` 恒映射为 RUNNING——终态本应由 tool_result 帧补）。
  3. **缺口**：`tool_result` 帧到达时（服务端时序实证：官方 loop 先 `session.append("assistant/message")` 再执行工具落 `tool/result`，dsh-agent-loop lib/index.js step() L610-690，即 team_message 帧必然先于 tool_result 帧），live 草稿经 block_end 叠加已携带真实 toolId（glm adapter 在 `output_item.done` 发 block-end 携带 id/name，`common/js/dsh-plugins/llm-glm/src/wire.ts` L224-228 注释即此设计的记载）→ settle 命中 live 草稿 → **early-return，已固化的归并序列条目与成员视角条目（共享 frame.message 对象）保持 RUNNING 无结果**，直到 List 回填（刷新）或静止后成员视角回填替换——与用户观察逐点吻合（"刷新页面以及完全静止后可以通过 history 修正"）。
  4. **掩盖因素**：`projects/game/web/frontend/src/store/chat.test.ts` L301-320 的单测喂的是 057 形态帧序——`blockStart` **带 `toolId`**（生产服务端从不发送：dsh StreamChunk 的 block-start 无 id 字段，`chunkToChatEvent` 的 `chunk.id ?? ""` 恒空）且**无 team_message 帧**，恰好绕开 059 形态的缺口；大型测试（`agent_v2_helpers_test.go` assertTeamMemberTurnWellFormed）只断言 wire 帧良构（block_end 带 tool_id、tool_result 配对），不运行前端 store。
- **修复（与根因一致——补全 settle 的投影面，不改帧、不改时序、不改回填）**：`chat.ts` 的 `toolResult` 归约改为**跨三投影面幂等 settle**：live 草稿（settleDraft）、归并序列条目（settleHistoryEntry）、成员视角条目（settleMemberHistory）在一次归约内全部应用（各面按 toolId + RUNNING 匹配，settle 本身幂等——已终态块不再命中），删除 live 命中后的 early-return。服务端零改动（帧序列已良构，大型测试实证）。
- **为什么不是别的修法**：改服务端让 `team_message` 帧等待工具终态再发（破坏"step 落定即固化"的 seq 锚语义与流式渐进呈现）；去掉 `fixedSteps` 渲染（回退到 live 渲染主导，破坏归并序一致性设计）；让 `blockToContentBlock` 不再恒映射 RUNNING（服务端条目在 settleToolResult 前就是 RUNNING 事实）。
- **引用**：`projects/game/web/frontend/src/store/chat.ts` L578-631（现行分支）、L429-438（consumeFixedStep）、`ChatView.tsx` L380-381（slice(fixedSteps)）；`git show 2a3c4d4:projects/game/web/frontend/src/store/chat.ts`（057 原型）；`common/js/dsh-plugins/llm-glm/src/wire.ts` L221-228；dsh-agent-loop lib/index.js step()（pnpm store 0.1.1-rc.2）；`projects/game/testplan/agent_v2_helpers_test.go` L562-601。

## R7 当前激活成员：单一合并值（GetTeam.active_member + web 呈现）

- **Decision**：`OrchestratorSnapshot` 增加 `activation`（下一条输入归属成员，即 orchestrator 的 `current` 字段，`orchestrator.ts` L344-345；现状 snapshot 只外露 driving 的 `active` L553）；`TeamView` 增加 `activeMember = driving?.role ?? activation`（用户裁定单一合并值：回合在途 = 驱动成员，静止 = 下一条输入归属；物化后恒非空，初始 planner）。proto `Team` 增加 output-only `active_member`（string；未物化时 GetTeam 本就 NOT_FOUND，无空值歧义）。web：team 工具条呈现激活成员徽标；**实时性经前端推导**——任一成员 `turn_start` 帧 → 激活 = 该 member（覆盖最近 GetTeam 值）；live 全部收束 → 以最近 GetTeam 值兜底（GetTeam 刷新时机沿用现状：进入会话/send 前/10s 轮询/live→空迁移即时刷新）。不新增流帧。
- **Rationale**：编排层已持有全部事实，仅未外露；两概念在串行驱动下行为不分离（驱动成员即当前激活成员），单一值完整回答"现在是谁在处理"（spec Clarifications 2026-09-11 第 2 条裁定）。前端推导避免为呈现增加帧词汇；GetTeam 兜底覆盖静止期切换（gameEnded 后 activation 变化的静止窗口由 10s 轮询/下次 send 前刷新收敛，呈现精度足够）。
- **Alternatives**：新增激活变更流帧（帧词汇扩张非必要）；GetTeam 轮询-only（用户明确"实时同步（无需刷新）"预期）。
- **引用**：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` L343-348、L549-556；`projects/game/agent_v2/src/session.ts` toTeamView；spec FR-008。

## R8 广播净化：think 移除 + 单一 XML 标注形态

- **Decision**：
  - **think 移除**：`messageBody` 只取 `text` 块（移除 `reasoning` 分支）；`toolResultBody` 同型（text 原样，其余块保留无损 JSON 形态——工具结果本无 reasoning，防御性同规则）。发言正文与工具调用的 args/result 全文 1:1 原样语义不变。
  - **单一 XML 标注（择一裁定的选择）**：删除头行（`[role] 摘要` 与 `[role] 工具调用 <tool> (context)`），保留标签对包裹形态；工具单元的 `context`（局 id）移入包裹体内 `context:` 行（缺席省略）。终态格式：
    ```
    <player-message>
    {发言正文原文}
    </player-message>

    <player-tool-call>
    context: {局 id}          ← 可选行
    tool: {name}
    args: {完整参数原文}
    result: {完整结果原文}
    </player-tool-call>
    ```
  - `section.ts` 广播格式约定同步（去掉"首行 [角色] 摘要"表述）；`summarize`/`boundContextSummary` 引用随头行一并移除。
- **Rationale**：用户裁定"二者选其一"；选 XML 形态而非 `[role]` 头行形态的理由：多行正文需要无歧义定界（头行形态无闭合边界）；fake-llm 夹具锚点 `<player-message>`/`<player-tool-call>`（team_planner.yaml L84-85）**保持不变**（头行消失不影响锚点匹配），夹具改动最小化；成员视角 relay 呈现的正文随 wire 净化同步去重。059 spec FR-008"头行可含简短摘要标签"条款由本裁定修订（记录于 [contracts/team-api.md](contracts/team-api.md) §4）。
- **Alternatives**：`[role]` 头行形态（无定界、夹具锚点需全部改写）；双形态并存（用户明确否决）。
- **引用**：`common/js/dsh-plugins/team/src/broadcast.ts` L86-109、L242-276；`common/js/dsh-plugins/team/src/section.ts` L40-43；`projects/game/fake-llm/service/testdata/team_planner.yaml` L75-85；spec FR-010/FR-011。

## R9 提示词三层所有权（玩法进 saolei-loop / 工具守则仅用法 / persona 去重）

- **Decision**：
  - **`saolei:game` section（saolei-loop 所有，host 行注册，全员可见）**：`saolei-loop` 的 `apply(ctx)` 经 `ctx.systemPrompt.section({name: 'saolei:game', order: 50, text})` 注册（host 组合行于 boot 时生效；dsh-system-prompt 的 section 注册在**调用 ctx 的 scope** 生效且全局 section 对所有 agent 装配可见——types/index.d.ts L180-187，比 team 插件的按成员注册更简，内容为静态全员事实）。order 50 = persona(0)/team(1-49) 之后、工具守则(100-199) 之前。内容 = **经典扫雷玩法**（依据 https://en.wikipedia.org/wiki/Microsoft_Minesweeper 与 https://en.wikipedia.org/wiki/Minesweeper_(video_game) ：目标为揭示全部非雷格、数字 1-8 表示八邻雷数、空格（0）触发级联展开、右键标旗、已揭示数字满足旗数时可 chord（左右同击）展开其余邻格、踩雷即负、全部非雷格揭示即胜、剩余雷数计数器）+ **可用操作**（与 saolei 插件三工具能力取交集：新开一局（saolei_init，重开重播种）、点击揭示/标旗/chord（saolei_operate 单发或有序批量）、只读剩余雷数查询（saolei_remain）——不声明未实现操作（如 Win98 的 ? 问号标记），tool 语义细节（参数形态/结果三层结构/拒绝语义）不在此重复，归属工具守则）。
  - **`saolei:guidance` 收缩**：保留符号表（棋盘文本符号是工具结果的读法）、坐标标尺、结果三层结构、校验与拒绝语义、示例流（调用形态）、Do-not（工具使用纪律）；**移除**玩法陈述（数字含义、级联、旗的用途对游戏的意义、chord 的展开条件作为游戏规则、胜负判定语义——迁移至 `saolei:game`）。切分原则：**"怎么调、返回什么形状" 留守则；"规则是什么、操作意味着什么" 进玩法 section**。
  - **persona 去重**：player 模板 persona 移除操作清单描述（"调用 saolei 工具开新局、点击/标记/双击揭示格子并查询剩余雷数…"），保留身份/职责/风格（"你是扫雷 player，负责操作桌面扫雷窗口完成对局…你独占桌面控制；以工具返回的棋盘事实为准…"）；planner 模板 persona 不变（本无玩法描述）。
- **Rationale**：spec FR-012/013/014；planner 此前无任何游戏规则输入（persona 无玩法、无 saolei 行）——策略与复盘质量受限；分层后所有权单一（玩法归 loop、用法归工具行、身份归 persona），player 侧 token 净减（guidance 与 persona 的重复陈述消除）。fake-llm 夹具 `system_keywords` 若锚定被移动的文案需同步（tasks 阶段核对 team_player/team_planner.yaml 的 keywords）。
- **Alternatives**：玩法进 team section（team section 承载团队协作事实，游戏规则是场景事实而非团队结构事实，且 team section 由 register 参数渲染——静态内容不该走注册参数）；每角色各写一份玩法（违背所有权单一）。
- **引用**：`common/js/dsh-plugins/saolei/src/index.ts` L260-337（现状 guidance）；`common/js/dsh-plugins/saolei-loop/src/index.ts` L88-104（现状空 apply）；`projects/game/agent_v2/preset-templates/`；[@deepseek-ai/dsh-system-prompt lib/types/index.d.ts（unpkg 0.1.1-rc.2）](https://unpkg.com/@deepseek-ai/dsh-system-prompt@0.1.1-rc.2/lib/types/index.d.ts)（`SystemPrompt.section()` 与 section 作用域语义）。

## R10 webUI system prompt 主界面入口

- **Decision**：对话页 team 工具条的成员清单区（`team-members`）为每成员增加可点击入口（点击经既有 `getTeamMember` API 读取并浮层展示 `system_prompt` 全文，只读，行为复用 TeamSettingsPanel 内既有浮层组件逻辑，入口从设置面板迁移/复制到工具条）；设置面板内入口保留不变。
- **Rationale**：能力已存在（059 FR-016，`TeamSettingsPanel.tsx` openSystemPrompt），仅入口不可发现（藏在"设置 team"面板内）；主界面入口满足 spec FR-009，零新 API。
- **引用**：`projects/game/web/frontend/src/components/TeamSettingsPanel.tsx` L87-108、L220-234；`projects/game/web/frontend/src/api/agent.ts` getTeamMember。

## R11 组合清单与部署面变更汇总

- **Decision**：`projects/game/agent_v2/cordis.yml`：agent-presets 行 roots 删 user root 条目（两 system 根保留，路径仍经 `PRESET_TEMPLATES_ROOT` env 表达式——默认值由 dsh.ts 从 `DOMINION_ARTIFACT_DIR` 派生注入，R3）；preset-authoring 行配置不变（templateRules 保留）；组合行集合无增删（roster 行降级为模板发现/校验服务）。deploy 面：`projects/game/deploy.yaml` agent-v2 env 块移除两项 preset env；三份 testplan 拓扑同步；`tools/release/deploy/README.md` 保留清单 + 新变量文档；builder.go 两 workload 构建器注入新变量（含 builder_test.go env 计数断言更新）。
- **Rationale**：组合三面（package.json ⟷ cordis.yml ⟷ tar 物化）无行级增删 → 无闭包审计要求（059 R3 实现注意①的触发条件不成立）；部署面变更为纯减法 + 平台加法。
- **引用**：`projects/game/agent_v2/cordis.yml`；`projects/infra/deploy/runtime/k8s/builder_test.go`（env 数量断言 L341/L382/L418 等）。

## R12 测试策略

- **Decision**：
  - **单测**（每次变更必过，`bazel test`）：常量库内容与两语言一致性；builder 注入（新变量存在/平台覆盖语义/计数断言更新）；dsh.ts 模板根解析三分支与 fail-loud；preset-authoring derive（store→组合行替换→临时文件→合成 AgentPreset；空 persona 回退；幂等重建；CRUD store-only 后无 roster 副本交互）；team broadcast（think 缺席、单一 XML 形态、context 行、1:1 全文保持）；section 文案（格式约定与 render 一致）；**chat store：tool_result 三投影面 settle（新增 059 真实帧序用例：blockStart 无 toolId + blockEnd 带 toolId + team_message 固化 + tool_result——覆盖 R6 缺口）；member_view 帧归约（幂等/追加/不影响归并序列）**；激活成员推导（turn_start 覆盖/live 收束回退）；saolei:game section 注册与文本；guidance 收缩后无玩法关键词。
  - **大型测试**（验收，`guitar run` 全量通过）：既有三 suite 全回归（夹具锚点与断言随 R8/R9 同步更新——广播格式断言改单一 XML 形态、system_keywords 若锚定被移动文案则更新）；新增断言：Send 流中出现 `member_view` 帧（planner 首驱消费用户输入即达）、GetTeam.active_member 随阶段流转（物化后 planner→player 回合→复盘）、部署拓扑 env 无 preset 路径项且服务 boot 成功、Pod 重建（重启 agent-v2）后 store preset 直接可物化（无副本重建路径）。
- **Rationale**：constitution 原则 IV/VI；R5/R6 修复的回归锚 = 新增的 059 真实帧序 store 用例（此前缺失的掩盖因素闭环）。
- **引用**：`projects/game/testplan/system_test.yaml`；`projects/game/fake-llm/service/testdata/`。

---

## 风险与缓解（汇总）

1. **临时文件挂载的生命周期**：`mountPreset` 的 fiber 持有 config.path 引用——挂载 settle 后文件不再被读取（loader 无 watch，production 形态）；临时文件保留至进程退出（OS tmpdir 随容器销毁），不主动删除规避 dev 形态 config-update 路径的边界；文档标注"可随时删除，下次 compose 幂等重建"。
2. **R6 修复的回归面**：三投影面 settle 改动触及并发流去重语义（多流重复 tool_result 帧）——settle 幂等（RUNNING 匹配）天然去重；单测覆盖重复帧用例。
3. **R8 格式变更的联动面**：fake-llm 夹具锚点（标签保持不变，头行相关断言需核对）、testplan 广播断言、059 contracts/dsh-plugins.md §1 的格式条款——同批原子更新，无并存窗口（team 内存态，重启即新格式）。
4. **激活成员呈现精度**：静止期 activation 切换（gameEnded 后）到下次 GetTeam 刷新前呈现滞后 ≤10s（轮询节奏）——demo 场景可接受，文档标注。
5. **`DOMINION_ARTIFACT_DIR` 命名稳定性**：保留名一经发布即平台契约——本 feature 内定名并入库常量库（后续改名成本高，命名评审在 tasks 阶段复核一次）。

## 对 tasks 阶段的输入

- 实现顺序建议（切片独立可验）：常量库 + 平台变量（R4/R1）→ 模板根推导与部署清单收敛（R3/R11）→ preset 派生化（R2）→ US4 双修复（R5/R6）→ 激活成员 + prompt 入口（R7/R10）→ 广播净化（R8）→ 提示词分层（R9）→ 大型测试收口（R12）。
- 每个 phase 的文档清单（constitution 原则 V 三分类）由 `/speckit.tasks` 生成；本文与 [contracts/](contracts/)、[data-model.md](data-model.md) 是必读输入。
- US4 修复的边界纪律（用户指令）：只做 R5/R6 描述的最小变更；回填收敛、并发流去重、断开投影等既有语义零改动（回归用例守护）。
