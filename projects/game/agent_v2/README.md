# game agent-v2

agent-v2 是 game 域的 **team agent 服务**（服务发现名 `agent-v2`，deploy API
命名约束要求连字符——`specs/049-agent-v2-dsh-init/research.md` D13；项目目录与
bazel target 保留 `agent_v2`）：以 **dsh 进程内嵌入（B1 模式）**宿主
[deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)（版本线
0.1.1-rc.2，与 `third_party/dsh/core` 同线），通过自研 LLM 插件
`@dominion/dsh-llm-glm`（`common/js/dsh-plugins/llm-glm`）接入 GLM codingplan
的 OpenAI **Responses** 协议端点（`https://open.bigmodel.cn/api/v1`，
https://docs.bigmodel.cn/cn/coding-plan/tool/others），并经
`@dominion/dsh-llm-opencode-go`（`common/js/dsh-plugins/llm-opencode-go`）接入
opencode-go 网关的 OpenAI **Chat Completions** 协议端点
（`https://opencode.ai/zen/go/v1`，https://opencode.ai/docs/zh-cn/go/ ）；模型
选择面以 `provider/model-id` 复合标识联合呈现两 provider 目录
（`specs/063-llm-reliability-opencode-go/contracts/model-selection.md`）。
session 的组织模型是
team：一个 session 至多物化一个 team——恰含 player 与 planner 两个 LLM 成员
（各持独立历史与 system prompt）与一个非物化的“扫雷系统”成员（role
`saolei`：无 preset/model、不经 LLM 驱动、不进 proto 成员面；终局交接时播报
本局统计，`specs/065-agent-v2-team-refine/spec.md`）；需求与验收锚点见
`specs/059-agent-v2-team-mode/spec.md` 与
`specs/060-agent-v2-team-optimize/spec.md`，接口契约见两者
`contracts/` 下的 team-api/preset-derivation/deploy-env/prompt-sections
（060 以增量修订方式更新团队面与提示词面的语义；065 增量见
`specs/065-agent-v2-team-refine/contracts/`）。

## 服务形态与拓扑

- **`kind: stateful`**：team 注册表、每 session FIFO 排队、成员对话历史、编排
  状态与游戏状态都驻留进程内存，因此会话面**必须**经 proxy 的 owner 亲和路由
  定向到同一实例（`specs/049-agent-v2-dsh-init/research.md` D4）。
- gRPC 监听 `0.0.0.0:50051`，三个服务（`projects/game/agent_v2.proto`）：
  - **AgentService**（team 会话面，
    `specs/059-agent-v2-team-mode/contracts/team-api.md`，060 增量见
    `specs/060-agent-v2-team-optimize/contracts/team-api.md`）：team 是 session
    的单例资源（AIP-156，`templates/{template}/sessions/{session}/team`），成员
    资源名即 `.../team/members/{role}`。RPC：`UpdateTeam`（显式物化/刷新，见
    下节）、`GetTeam`（含 output-only `active_member`）、`GetTeamMember`（含
    output-only `system_prompt`）、`ListTeamMessages`（团队视图归并序列）、
    `ListMemberMessages`（成员视角）、`Send`（server-streaming team 流——成员
    事件帧 + `team_message`/`member_view` team 级帧，经 gateway 以 NDJSON
    输出）、`Cancel`（team 语义取消）。
  - **PresetService**（无状态配置面）：分池 preset 标准 CRUD + `ListModels`。
  - **DesktopBridgeService**：desktop 的 flow 控制流 WebSocket 入口
    （`/api/v2/templates/{template}/sessions/{session}/connect`），session 单位、
    新连接接管；桌面控制由 player 独占
    （`specs/059-agent-v2-team-mode/spec.md` FR-012）。
- gateway 的 `/api/v2` 路由按 RPC 状态归属拆分
  （`specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md` §3）：
  会话面（AgentService + DesktopBridgeService）走
  `gateway → proxy → agent-v2 实例` 两跳，proxy 以 Mongo
  `game_proxy.agent_v2_owners` 做 owner 亲和路由；配置面（PresetService：状态在
  Mongo、模型目录静态）由 gateway 直连任一实例。web（`projects/game/web/`）由
  同 hostname 的 `/` PathPrefix
  服务，`/api/v1/`（session 与 memory 管理路由）和 `/api/v2/` 由 gateway 服务
  （`specs/049-agent-v2-dsh-init/research.md` D5）。
- 周边服务：session 服务（`/api/v1` sessions CRUD）、memory 服务（planner 长期
  记忆存储 + `/api/v1/.../memories` 管理路由）、Mongo（preset 持久化与 proxy
  owner 记录）。
- 进程内组件生命周期（启停序列
  `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md` §3.1、health
  端点 §4；agent_v2 的特定顺序见 `projects/game/agent_v2/src/bootstrap.ts`
  头注释）：dsh 组合（成员会话 + root fiber；Mongo 连接由组合内 preset-authoring
  行持有——组合启动时连接并建索引，存储失败 boot fail-loud，关闭随组合 unwind）
  最先启动、最后停止；gRPC server 后启动、先停止（tryShutdown 在关停预算内排空，
  未完成则 forceShutdown）；组合停止时先逐 session dispose（在途回合 abort）再
  unwind root fiber（关闭 Mongo client）；38080 `/healthz` 在全部组件启动后开始
  服务、在停止序列首位关闭
  （`specs/052-deploy-health-probe/contracts/deploy-probe.md`）。

## 组合清单（cordis.yml）

`cordis.yml` 是启用插件集的唯一事实源
（`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §5）：

- dsh 核心件：`llm`/`session`/`system-prompt`/`tools`/`agents`/`invariants`/
  `llm-retry`（`system-prompt` 关闭 harness 身份与运行时上下文注入——部署自有
  完整 system prompt）；`timer`/`invariants`/`system-prompt` 经框架基线
  `@dominion/dsh-core` 进入。
- **官方 `dsh-agent-loop` 行**：成员 agent 的驱动权归属官方 loop
  （`Config.agents[]` 留空，成员全部经 `ctx.agents.create` 动态物化）；自研
  `saolei-loop` 升为 team loop 编排层（驱动时机 + 游戏阶段机），不含 turn/step
  状态机（`specs/059-agent-v2-team-mode/research.md` R7）。
- 一致性防护伴生：`dsh-session`/`dsh-agent`/`dsh-scope` 三个包的 `/invariant`
  subpath 插件（官方 loop 的 request-reconstruction 不变量）。
- `llm-glm`（`@dominion/dsh-llm-glm`）：GLM Responses 端点接入；`models[]` 是该
  provider 的部署模型目录（`ListModels` 联合目录由各已注册 provider 的
  `listModels` 聚合，见
  `specs/063-llm-reliability-opencode-go/contracts/model-selection.md` §2；物化
  校验按复合标识切分后对所选 provider 目录进行）。
- `llm-opencode-go`（`@dominion/dsh-llm-opencode-go`）：opencode-go 网关 Chat
  Completions 端点接入（`specs/063-llm-reliability-opencode-go/spec.md` FR-015
  默认目录 = 官方该路由 16 项）；与 `llm-glm` 同套
  失败分类/重试/看护义务
  （`specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md`）。
- `agent-presets`（`@deepseek-ai/dsh-agent-presets`）：preset roster，roots =
  两个模板 system root（player/planner 池各一，镜像内
  `preset-templates/{player,planner}`）；用户 preset 的组合在物化时从 store
  记录派生，无 user root；`default` 指向空 id，preset 选择是必选语义（无 id
  的 resolve fail-loud）。
- `preset-authoring`（`@dominion/dsh-preset-authoring`）：创作/编辑面（Store
  Mongo 实现，`game_agent_v2.presets`）+ 模板行校验（`templateRules`）。
- `team`（`ctx.team`）与 `memory`（`ctx.plannerMemory` host 服务面）：
  自研群聊原语（含全员广播的单一 XML 标注格式与 team section 格式约定）与
  planner 记忆服务面；工具行不经 host 层——`saolei` 经 player 池模板 preset
  挂载、`memory` 经 planner 池模板 preset 挂载（工具可见性由挂载层隔离）。
- `desktop-bridge`（`@dominion/dsh-desktop-bridge`）与 `saolei-loop`
  （`@dominion/dsh-saolei-loop`）：后者 boot 时经 `apply(ctx)` 全局注册
  `saolei:game` prompt section（玩法 + 可用操作，全员可见——见「广播与提示词
  分层」）。

## team 物化与刷新（UpdateTeam）

team 不经 Send 懒创建，必须经 `UpdateTeam`（AIP-134 create-or-update，
`allow_missing=true`）**显式物化**
（`specs/059-agent-v2-team-mode/contracts/team-api.md` §2）：

- **物化输入 = `members` 列表**：每成员 `{role, preset, model?}`——proto 会话面
  是场景无关 team 原语（无 role 枚举、无场景字段）。role 为场景词汇字符串
  （saolei 下 `"player"`/`"planner"`）；preset 为完整 preset 资源名；model 可空
  = 部署默认。输出 members 与输入同形（生效 model、服务端按 role 构造的成员
  资源名），`desktop_connected`、`active_member` 与时间戳为 output-only；
  `system_prompt` 仅 GetTeamMember 返回。
- **两层 fail-fast 校验（先于任何 teardown，无半物化）**：结构层（场景无关）
  ——members 非空、每成员 role 非空、preset 为合法 preset 资源名；saolei 场景
  层——members 恰 2 且 role 集合恰为 `{"player", "planner"}`、每成员 preset
  存在且 `preset.role` 与成员 role 字符串相等、model 非空时在 ListModels 目录。
  失败均为 `INVALID_ARGUMENT`（`projects/game/agent_v2/src/session.ts`）。
- **物化编排**：逐成员 `ctx.agents.create`（官方 agent-loop 驱动）挂载 roster
  preset 组合，player 侧注册 agent-scoped `saoleiGame`（GameRuntime），planner
  侧 `ctx.plannerMemory.load` 预取记忆快照（fail-loud）；随后 `ctx.team.register`
  注册群聊服务。任一步失败**整体回滚**：不残留半物化 team（GetTeam
  NOT_FOUND），可重试。
- **成员消息源与扫雷系统角色**：team 的成员抽象为消息源接口
  （`TeamMemberSource`，
  `specs/065-agent-v2-team-refine/contracts/team-member-source.md` §1）：
  player/planner 经 `agentMemberSource` 适配器接入（语义零变化），物化同时注册
  announce-only 的“扫雷系统”成员（role `saolei`、summary 为“扫雷系统，终局播报
  对局结果与操作统计”，`common/js/dsh-plugins/saolei-loop/src/announcer.ts`；
  无 preset/model、不接收广播、不进 `members`/`active_member`）。终局交接时它
  先于 planner 复盘播报本局统计（`gameStatsText` 模板：结果 + 单个操作总数 +
  click/flag/chord 分项，`specs/065-agent-v2-team-refine/contracts/
  game-stats-broadcast.md` §2/§3），宿主经订阅将其投影为归并序列条目与
  `team_message{member="saolei"}` 帧（同契约 §4），消息作为广播进入 planner
  复盘输入与 player 下次结构性驱动输入；每局至多一条（记录恒等 guard，重试
  不重发；排队消化优先，被跳过局不补发）。
- **静止等待与用户首驱**：物化成功后 team 静止等待——初始激活成员 = planner、
  初始相位 = planning，**不自动驱动任何成员**；游戏首次驱动由用户第一条消息
  触发（planner 处理并产出开局策略），随后 player 被驱动开始游戏，终局后
  planner 复盘，planner 完成静止后编排层结构性续驱 player 进入下一轮。一切
  驱动输入 = 成员未消费的团队消息 + 排队用户消息，编排层不合成任何驱动消息
  （`specs/059-agent-v2-team-mode/spec.md` FR-009/FR-010；
  `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §2）。
- **当前激活成员（`active_member`，单一合并值）**：成员回合在途时 = 该回合的
  驱动成员（任一时刻至多一个）；静止时 = 下一条输入的归属成员（activation，
  物化后初始 planner，取消/暂停不改变归属）。team 物化后恒非空；web 对话页
  由该值叠加流式 `turn_start` 帧推导呈现
  （`specs/060-agent-v2-team-optimize/contracts/team-api.md` §1/§5）。
- **刷新**：已物化 team 再次 UpdateTeam 即刷新——终止在途成员回合
  （`turn_end{ABORTED}`）、排队作废、清空两个成员的短期记忆（历史/游戏状态）、
  按新配置重建；`create_time` 保留、`update_time` 更新；重复 Update 幂等。
- 未物化 session 的 `Send` 以明确错误拒绝（FAILED_PRECONDITION，提示先物化）；
  `GetTeam`/`GetTeamMember`/List 面为 NOT_FOUND。

## preset 分池与角色工具锁定

preset 数据按角色分池
（`specs/059-agent-v2-team-mode/contracts/preset-api.md`）：

- **role 为场景词汇字符串**（saolei 下 `"player"`/`"planner"`），create 时必填
  且不可变（改 role 被拒）；同 template 内 preset id 全局唯一（跨池不重复）；
  `ListPresets` 支持 role 过滤（空 = 不过滤）。
- **用户可编辑内容仅 persona**（update_mask 仅 `persona`）；persona 为空时物化
  回退该角色默认 base（第一人称身份声明开头，
  `specs/059-agent-v2-team-mode/data-model.md` §2）。
- **创作 = store-only**：Create/Update/DeletePreset 只写 Mongo store（唯一
  事实源）；创建时校验模板行（`cordis.yml` `templateRules`）：player 模板
  恰含 `@dominion/dsh-saolei` 行、不得含 `@dominion/dsh-memory`，planner
  模板恰含 `@dominion/dsh-memory` 行，违者 INVALID_ARGUMENT 且不落任何
  产物。
  池模板（`projects/game/agent_v2/preset-templates/{player,planner}/`，镜像内
  system 信任根）不可写/不可删。
- **角色工具锁定（行级绑定）**：player preset 绑定扫雷游戏工具插件组
  （`saolei_init`/`saolei_operate`/`saolei_remain` 工具 + `saolei:guidance`
  守则），planner preset 绑定 memory 插件组（memory 工具 + 快照 section）——
  工具与其配套守则作为整体生效或缺席，物化零定制。删除 preset 无 fan-out：
  已物化成员保持其物化时组合，再次物化引用被拒
  （`specs/059-agent-v2-team-mode/spec.md` FR-006）。
- **物化 = 使用时派生**：成员物化消费的组合在使用时从 store 记录派生——读池
  模板组合、以记录 persona 替换 persona 行（空值回退模板默认 base，记录
  persona 即用户可编辑内容），写入系统临时目录的纯临时文件，经官方
  `mountPreset` 直接挂载；磁盘上不存在需要与 store 对账的副本
  （`specs/060-agent-v2-team-optimize/contracts/preset-derivation.md` §2）。

## team 流与编排（Send）

`Send` 建立的 stream 是 **team 流**
（`specs/059-agent-v2-team-mode/contracts/team-api.md` §3；060 增量见
`specs/060-agent-v2-team-optimize/contracts/team-api.md` §2/§3）：

- 当前成员回合中到达的用户消息入 team FIFO，本流首帧回
  `queued{position}`；用户消息在 Send 被接受时即固化入归并序列（enqueue 即
  固化），并以 `team_message{member="user"}` 帧扇出。
- **帧三类**：成员事件帧（`turn_start`/`block_start`/`delta`/`block_end`/
  `tool_result`/`turn_end`）外层 `member` 标注产出成员（block index/step 以成员
  回合为一个 index 空间，前端按 `(member, turn_id)` 分组增量渲染）；team 级帧
  `team_message` `{member, message, seq}` 与 `ListTeamMessages` 元素同构、seq
  同源同值，保证实时归并序与 List 回填跨视图一致；team 级帧 `member_view`
  `{member, sender, message}` 是成员消费一条输入（用户输入或广播注入）写入其
  视角的实时事实（扇出早于/伴随该成员回合事件），`message` 与
  `ListMemberMessages` 投影同源同值，幂等锚 `message.messageId`
  （`specs/060-agent-v2-team-optimize/contracts/team-api.md` §2）。
- **工具结果实时性**：`tool_result` 帧携带 `toolId` + 终态 + 结果文本，服务端
  帧序为 block_end（toolId 首次完整浮现）→ team_message（step 固化）→
  tool_result（终态）；前端在一次归约内对 live 草稿、归并序列条目、成员视角
  条目三个投影面按 `toolId` 幂等终态化，工具块在结果帧到达时即时转为终态并
  显示结果文本（`specs/060-agent-v2-team-optimize/contracts/team-api.md` §3）。
- **静止终点**：流从发起持续输出，覆盖其间全部成员回合——含编排自动驱动的
  结构性续驱、gameEnded 复盘、排队消化与多局循环——直到 team 静止（无在途
  回合且无待消化输入）才结束；自然收敛与 Cancel 后的暂停静止都属静止点。
- **流与编排解耦**：服务端向该 session 全部活跃流扇出（`queued` 仅回执其所响应
  的流）；客户端断开不终止编排，取消编排仅经 `Cancel`；多流重复帧由前端按
  seq / `(member, turn_id)` 锚去重，断开经 List 面回填补齐。
- **`Cancel`**：终止在途回合（`turn_end{CANCELED}`）+ 暂停自动续驱 + 排队消息
  保留为已固化历史且不触发新驱动；幂等；再次 Send 即恢复（消息由当前激活成员
  处理，建立新 team 流）。

## 双视图、激活成员与 system prompt 查看

web（`projects/game/web/frontend/src/`）以 session → team 模型组织
（`specs/059-agent-v2-team-mode/contracts/web-views.md`）：

- **团队视图**（1 个，`ListTeamMessages` 回填 + team 流实时）：全部消息按 seq
  归并，成员消息取该成员的原始输出（正文/思考/工具调用与结果），归属到成员
  名下，不显示广播包装形态。
- **成员视角视图**（每成员 1 个，共 2 个，`ListMemberMessages` 回填 +
  `member_view` 实时）：用户消息 → user（该成员消费时经 `member_view` 帧即时
  追加，无需等待回填）、自己的输出 → agent、其他成员的消息 → 标注来源的 user
  消息（渲染为 `user: [sender] 正文`）。
- 切换器（团队 | player | planner）为纯前端状态，各视图历史常驻不重填；同一
  消息跨视图正文一致（`specs/059-agent-v2-team-mode/spec.md` SC-003）。
- **激活成员**：对话页 team 工具条呈现当前激活成员徽标——实时推导为最近成员
  `turn_start` 帧的 member（流式期间覆盖最近 GetTeam 值），live 全部收束后以
  GetTeam 的 `active_member` 兜底（刷新时机沿用现状：进入会话/发送前/10s
  轮询/回合静止）；未物化时不呈现激活成员
  （`specs/060-agent-v2-team-optimize/contracts/team-api.md` §5）。
- **system prompt 查看**：对话页 team 工具条的成员清单区为每成员提供入口
  （点击经 `GetTeamMember` 读取并以只读浮层展示 `system_prompt` 全文），设置
  面板内入口保留。内容从成员实例的 system prompt 装配面读取（persona + team
  section + saolei:game 玩法 section + 工具守则 + planner 的长期记忆快照），
  与该实例实际发给模型的一致，非另行拼装；刷新 team 后随新配置更新
  （`specs/059-agent-v2-team-mode/spec.md` FR-016）。

## 广播与提示词分层

成员间 1:1 广播（team 插件渲染、注入接收方成员上下文）采用**单一 XML 标注
形态**（`specs/060-agent-v2-team-optimize/contracts/team-api.md` §4）：标签名
即发送者标注，正文只出现一次、不另有发送者摘要行。

- 发言单元：`<{role}-message>` 标签对包裹发言正文原文（仅 text 块，不含
  think/reasoning 内容）。
- 工具单元：`<{role}-tool-call>` 标签对包裹 `tool:`/`args:`/`result:` 行
  （完整参数与结果全文，不截断不摘要）；广播方提供局上下文时前置 `context:`
  行。
- 成员视角渲染为 `user: [sender] 注入原文`（XML 标签对保留、正文仅呈现
  一次）；team section 的格式约定与该 wire 形态同源。
- 成员清单（roster）由注册成员渲染，含扫雷系统行
  `- [saolei] 扫雷系统，终局播报对局结果与操作统计`；team section 同时明确
  这些标签格式**仅输入侧**——只用于系统向接收方呈现他人输出，成员自身输出不
  使用 `<角色-message>`/`<角色-tool-call>` 自我包装
  （`specs/065-agent-v2-team-refine/contracts/team-member-source.md` §3/§4）。

提示词三层所有权
（`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md`）：

| 层 | 所有者 | 生效范围 | 内容边界 |
|---|---|---|---|
| 玩法 + 可用操作（section `saolei:game`，order 50） | saolei-loop（host 行 boot 注册） | 全员（player/planner 同源同文） | 经典扫雷规则 + 与三工具对齐的可用操作；不含调用形态/结果格式 |
| 工具守则（section `saolei:guidance`，order 100） | saolei preset 行（player 池） | 仅 player | 仅工具用法（符号表/坐标/结果三层结构/校验拒绝语义/示例/纪律） |
| persona（order 0） | preset 模板 persona 行 | 各成员 | 身份/职责/风格；不重复玩法与操作描述 |

## planner memory（快照固定、近因注入与 fail-loud）

planner preset 锁定的 memory 插件组
（`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §3）：

- **memory 单工具**：`{action: add|replace|remove, ...}` 单操作 XOR
  `{operations[]}` 批量（互斥校验、批量原子——preflight 全过才提交）；
  `old_text` 大小写敏感子串定位（0/多命中返回条目文本）；无 read 动作；失败
  也是普通文本结果、不中断对话。修改经 memory 服务立即持久化（scope 键
  (template, session)，管理路由 `/api/v1/.../memories` 可查证），过程经 planner
  工具调用历史与团队消息流可见。
- **快照装载与透传渲染**：物化时 `ctx.plannerMemory.load` 以
  `order_by=update_time desc` + `page_size=10` 单页装载长期记忆——最近 10 条
  由 memory 服务 `ListMemories` 的服务端排序承担，客户端按服务返回序纯透传
  渲染。`order_by` 为通用机制：AIP-132 `{field} [desc]` 逗号分隔语法（空白
  不敏感）、白名单 `memory_id`/`update_time`、指定 tie-breaker `memory_id`
  ——排序键不含它时自动追加 `memory_id asc`（`update_time desc` 归一化为
  `[update_time desc, memory_id asc]`）；含 `memory_id` 于任意键位即已是全序，
  不追加。游标编码页末条目在最终排序键上的键值序列且与续页排序键不匹配时
  拒绝（跨序重放 INVALID_ARGUMENT）。快照注入 system prompt 的函数式 section
  （order 200+，空不渲染），在成员实例生命周期内固定，运行中的外部修改待下次
  物化（刷新 team）生效
  （`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md` §1/§2；
  截断只影响注入面，memory 工具写路径仍面向全量存储）。
- **fail-loud**：memory 服务不可达时预取 throw，team 物化整体回滚（无半物化），
  可重试。

## preset 持久化与环境变量 MONGO_URI

preset 数据持久化在 Mongo `game_agent_v2.presets`，agent-v2 重启不丢；preset
CRUD 只写该集合（唯一事实源），物化组合在成员创建时从记录派生（无磁盘副本需
维护，见上节）。team 本体——历史/物化配置/游戏状态——为内存态，重启后需重新
物化（见「已知限制」）。Mongo 连接解析（`projects/game/agent_v2/src/presets.ts`）：

1. 环境变量 `MONGO_URI` 已设则直连使用（本地/测试直连形态）。
2. 否则经 Dominion 服务发现解析 `dominion:///game/mongo:27017`，并按部署
   环境派生 admin 凭据拼出 credentialed URI——派生算法与 Go 服务
   （`common/gopkg/mongo/credentials.go`）字节一致，同一实例同一
   认证（`DOMINION_ENVIRONMENT` 参与派生，缺省 `default`）。

## preset 模板根与环境变量 DOMINION_ARTIFACT_DIR

roster 的两个模板 system root（player/planner 池）扫描**模板根**下的
`player`/`planner` 子目录；模板数据作为部署产物随镜像分发在
`/dominion/game/agent-v2/preset-templates`。模板根在 boot 前由
`projects/game/agent_v2/src/dsh.ts` 解析并写回组合 env（`cordis.yml` 的
`process.env.PRESET_TEMPLATES_ROOT` 表达式读取解析结果）：

1. `PRESET_TEMPLATES_ROOT` 已设则直用（本地/测试显式覆盖）。
2. 否则由 deploy 平台注入的 `DOMINION_ARTIFACT_DIR`（产物放置目录，
   `/dominion/{app}/{service}`，见 `tools/release/deploy/README.md`
   §环境变量配置）派生 `${DOMINION_ARTIFACT_DIR}/preset-templates`。
3. 两者皆缺时 boot fail-loud（错误信息含两个变量名，退出码 1）——模板根
   是 roster system roots 的必需输入。

部署清单不声明模板根（默认设置省略；
`specs/060-agent-v2-team-optimize/contracts/deploy-env.md` §2/§3）。

## 模型端点与凭据配置

两个 LLM 插件行（`llm-glm`/`llm-opencode-go`）共享同一宿主注入模式
（`projects/game/agent_v2/src/dsh.ts`，`specs/049-agent-v2-dsh-init/research.md`
D9；`specs/063-llm-reliability-opencode-go/research.md` D13）：

1. 模型端点（各自一对变量）：`*_BASE_URL` 直用 > `*_LLM_TARGET` 经 Dominion
   服务发现解析（追加 `/v1` 路径）> 默认端点——GLM 默认
   `https://open.bigmodel.cn/api/v1`，opencode-go 默认
   `https://opencode.ai/zen/go/v1`。
2. API token（`GLM_API_KEY`/`OPENCODE_API_KEY`，三级解析）：环境变量已设直用
   （trim 非空，空白视为未设置）→ 否则读取
   `$DOMINION_SECRET_DIR/{glm-api-token,opencode-api-token}` 文件 → 皆缺失/为空
   则**保持未设**并记录一条 warning（含 env 名与 secret 文件路径，不含任何 key
   内容——`specs/049-agent-v2-dsh-init/spec.md` SC-004）后继续 boot。token 缺失
   不阻塞启动：插件对空 key 的模型请求**不携带 Authorization header**
   （`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §3 义务 6；
   `specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §3）
   ——fake 端点对凭据容忍；真实端点对无 Authorization 请求返回 401，以首轮
   `turn_end{ERROR}` 明确呈现。
3. 模型默认与看护窗口（cordis 行 env 注入）：`GLM_MODEL`/`OPENCODE_MODEL`
   覆盖各自目录首项（缺省 `glm-5.3`）；`GLM_STREAM_IDLE_TIMEOUT_MS`/
   `OPENCODE_STREAM_IDLE_TIMEOUT_MS` 为流停滞看护窗口（缺省 300000）。环境变量
   总表见 `specs/063-llm-reliability-opencode-go/data-model.md` §6。

### 运维预置（k8s secret，仅生产需要）

生产部署前，运维需在集群中预置 secret：**`llm-secrets` 增加 key
`glm-codingplan` 与 key `opencode-go`**——前者为 GLM codingplan API Key（在
https://docs.bigmodel.cn/cn/coding-plan/quick-start 套餐页新建），后者为
opencode-go 订阅 token（官方网关文档
https://opencode.ai/docs/zh-cn/go/ ）。`service.yaml` 的生产 artifact
`agent-v2` 声明逻辑 secret `glm-api-token` 与 `opencode-api-token`，
`projects/game/deploy.yaml` 分别将其绑定到 `llm-secrets/glm-codingplan` 与
`llm-secrets/opencode-go`（deploy 工具的 secret 双向校验仅对所选 artifact
生效，`tools/release/deploy/README.md` §服务类型；
`specs/063-llm-reliability-opencode-go/research.md` D13）；运行期经 projected
volume 挂载到 `$DOMINION_SECRET_DIR/{glm-api-token,opencode-api-token}`
（`specs/002-deploy-secret-config/contracts/secret-config.md`）。

测试 artifact **`agent-v2-test`**（同 target/tls、无 secrets 声明）承载零 secret
测试部署（`projects/game/testplan/deploy_agent_v2.yaml` 选用），测试集群无需为
agent-v2 预置任何 secret。

## 大型测试

`projects/game/testplan/system_test.yaml` 以四个 suite 覆盖 agent-v2 面与
周边模块：主 suite `game-system`（部署
`projects/game/testplan/deploy_agent_v2.yaml`——won 拓扑，fake-desktop
以 won 场景绑定 `desktop-e2e-won`）按模块顺序串行执行配置面、对话面
（/api/v2 team 流语义、双视图历史投影、排队/取消/刷新窗口、preset 分池与物化、
成员 system prompt 读取）、游戏面（won 链路、终局复盘与 memory 写入、
desktop 缺席、多会话隔离）与 desktop flow 面；断连 suite `game-disconnect`
（部署 `projects/game/testplan/deploy_agent_v2_drop.yaml`——同拓扑、fake-desktop
以 progressive 场景 + 断连故障 env 绑定 `desktop-e2e-drop`）承载 mid-game
断连恢复三局序列；memory 故障 suite `game-memory-down`（部署
`projects/game/testplan/deploy_agent_v2_memory_down.yaml`——无 memory
服务的拓扑变体）承载物化 fail-loud 回滚断言；停滞看护 suite `game-stall`
（部署 `projects/game/testplan/deploy_agent_v2_stall.yaml`——同拓扑、
agent-v2-test 以 `GLM_STREAM_IDLE_TIMEOUT_MS=2000` 使用专用看护窗口）承载
Responses wire 停滞模板在窗口内的有界超时收敛断言
（`specs/063-llm-reliability-opencode-go/spec.md` SC-004a，
`agent_v2_stall_test.go`）。测试替换面：fake-llm
`/v1/responses` 替换真实端点、fake-desktop 替换真实桌面、零外部网络；套件-拓扑
对照见 `projects/game/testplan/README.md` §2。060 增量断言：Send 流中 planner
消费用户输入的 `member_view` 帧与工具 `block_end → team_message →
tool_result` 帧序（`agent_v2_conversation_test.go`）、`GetTeam.active_member`
阶段流转（物化后 planner、回合在途为驱动成员、静止为 activation；
`agent_v2_game_test.go`）、preset store 派生链路（创建 → 物化 persona 与记录
一致 → 编辑记录后再物化取新值；`agent_v2_preset_test.go`）、广播单一 XML 形态
与提示词分层锚点（fake-llm 夹具 + 对话/游戏断言）。063 增量断言：单次瞬时失败
经重试恢复、planner 失败保持激活、配额/认证零重试风暴、opencode-go 复合标识
多局会话与合成 token 零泄漏（`agent_v2_conversation_test.go`）；ListModels 联合
目录与 UpdateTeam 复合标识三分支（`agent_v2_preset_test.go`）；停滞看护有界
收敛（`agent_v2_stall_test.go`）。065 增量断言：每局交接后归并序列恰一条
`member="saolei"` 统计消息（正文为 `gameStatsText` 模板、数值与该局
fake-desktop 实际成功派发序列一致，含批量多操作对照），planner 复盘输入与
player 成员视图消费该消息、`GetTeam`/`active_member` 不含该角色
（`agent_v2_game_test.go`）；排队跳局（排队消息驱动 player 开新局）被跳过局
无统计、新局交接恰一条新局统计，对照局（终局后无排队）即时播报
（`agent_v2_conversation_test.go`）；system prompt 含 roster 扫雷系统行与
“仅输入侧”表述、planner 快照 ≤10 条且由 memory 服务通用 `order_by`
（`update_time desc` 归一化含指定 tie-breaker）服务端有序（>10 条记忆夹具
对照）——
验收面见 `specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §5
与 `specs/065-agent-v2-team-refine/quickstart.md` §3。注入设施与触发词见
`projects/game/fake-llm/README.md`。

## 已知限制

- **compact 明确排除**（`specs/059-agent-v2-team-mode/spec.md` FR-018）：成员
  历史不裁剪、不压缩；多局后 planner 视角的工具调用历史持续增长（工具结果
  原样广播），token 代价接受为已知限制，治理留待后续 feature。
- **team 为进程内存态**：对话历史、游戏状态与物化配置（preset/model 绑定）
  随重启丢失；重启后再次对话前必须重新经 UpdateTeam 物化
  （`specs/059-agent-v2-team-mode/spec.md` Edge Cases 与 Assumptions）。仅
  preset 资源数据持久化（见上节）。
- **preset 组合为使用时派生物**：物化消费的组合在成员创建时从 Mongo store
  记录 + 池模板派生为系统临时目录下的纯临时文件（不维护、无清理承诺；任何
  时刻删除都不影响 store 数据与后续物化——重建幂等）。磁盘上不存在需要与
  store 对账的副本，也不存在 preset 专用可写根路径
  （`specs/060-agent-v2-team-optimize/contracts/preset-derivation.md` §1/§2）。
- **模板发现无 watch**：roster 的模板发现（resolve/list）每次调用落盘
  readdir，未注册 watch；两个 system 模板根为镜像内静态数据（用户 preset
  CRUD 只写 store，无运行时创作面），开销固定且有界
  （`specs/059-agent-v2-team-mode/research.md` R3 实现注意 ④）。
- **多标签页无实时推送**：team 流只覆盖已建立流（Send）的页面；未发送消息的
  标签页看不到其他页面的回合进展，可刷新经 `ListTeamMessages`/
  `ListMemberMessages` 回填查询。
- **session 删除不联动 team 清理**：删除 session 仅移除元数据，进程内 team
  （含历史与 flow 连接）不受影响；以相同资源名重建 session 会命中残留 team
  （`specs/059-agent-v2-team-mode/spec.md` Edge Cases）。
