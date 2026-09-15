# Research: agent-v2-team-refine

**Feature**: specs/065-agent-v2-team-refine/spec.md
**Date**: 2026-09-15
**方法**: 源码级调研（本地物化依赖 0.1.1-rc.2 + 本仓库插件/宿主/服务源码），无外部新依赖引入。所有行号锚点以当前 `main` 工作树为准。

## R0. 关键机制核实（决策前提）

- **drain 的读权威是 `reconcile` 重建，不是 live 追加**：`Team.drain` 先 `reconcile(state)`——从各 sender 的 log 派生单元（`deriveUnits`）减去消费锚（`consumedAnchors`）与已弹出未落日志锚（`member.drained`）后**整体重建** `state.pending`（`common/js/dsh-plugins/team/src/team.ts:185-203,397-425`）。推论：成员消息的派生源是**成员 log**——任何成员（agent 或非 agent）只要提供词汇表兼容的 log，其产出就经同一机制进入接收方（D1 的出发点）。
- **消费闭环按 `source.messageId` 锚**：注入消息携带 `source: {kind: "team-broadcast", messageId: anchor}` 落入成员 log 的 `user/message` 事件后，`consumedAnchors` 将其闭包（`common/js/dsh-plugins/team/src/broadcast.ts:162-175`）；`drained` 集合保护"已弹出未落日志"窗口（`team.ts:194-203`）。
- **dsh session 抽象与成员 log 同构**：`dsh-session` 自述 "Event-sourced session log and **in-memory store**... Persistence is intentionally not implemented here"（README）；`session.events` 为冻结快照、事件词汇表（`assistant/message` 等）merge-extensible、`SessionEvent` 携带 `time/seq`——一个持有 `SessionEvent[]` 形态 log 的成员与 agent 成员在派生/渲染/消费闭包上完全同权。
- **不用真 dsh `Session` 承载扫雷成员 log**：`assistant/message` 要求 provider/model provenance，且本组合挂载的 `/invariant` 子路径插件检查 turn/step enclosure——系统成员的播报不是 agent-loop 执行转录。采用**结构兼容的最小自有 log**：条目按 `SessionEvent` 形态构造——`message` 经 `createAssistantMessage`（`source` 用固定合成 provider/model），`turn`/`step` 固定占位（派生/渲染不读取）；"类似 agent log"由词汇表兼容满足。
- **agent log 当前不是长期持久化**（四层核实）：组合无持久化插件行（`projects/game/agent_v2/cordis.yml` 全行清单）；框架基线 `@dominion/dsh-core` 仅 boot 胶水（`third_party/dsh/core/package.json`）；部署无 volume/PVC（`projects/game/agent_v2/service.yaml`、`projects/game/deploy.yaml`——`kind: stateful` 是 owner 亲和路由语义非存储语义）；跨重启仅 preset（Mongo）、planner 长期记忆（memory 服务）、session 元数据（session 服务）存活。→ 扫雷成员 log 与 agent 成员 log 对齐 = 进程内存、随物化清零（D9）。
- **终局交接分支结构**（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts:896-941`）：case 1 排队消化优先 → case 2 pendingReview 重试 → case 3 planning/reviewing→player → case 4 `peekGameEvent() !== reviewedGameEvent` 时 `drain(planner)` 非空才置 pendingReview 并驱动复盘，否则落入 player 续驱。**播报必须放在 case 4 内、`drain(planner)` 之前**（D2）。
- **跳局语义即 case 1 优先序 + 终局记录单槽覆盖**：排队消息先驱动 player（current=player）；player 开新局致终局时 runtime 的 `gameEvent` 被新记录覆盖（`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts:302-310` 仅持 LATEST）——旧局交接永不发生 → 不播报即跳过，零补发逻辑。
- **proto 会话面零改动可行**：`TeamMessage.member` 为 string（wire 直通）；web 团队视图按 wire 角色串直接渲染（`projects/game/web/frontend/src/components/ChatView.tsx:204-210`）；成员视图注入按 `source.role` 标注（`projects/game/agent_v2/src/history.ts:404-423`）。role `saolei` 全链路透传，前端零改动（D7）。
- **memory 服务已返回 update_time**：proto `Memory.update_time`（OUTPUT_ONLY，`projects/game/game.proto:158`）；Go 服务 `memoryToProto` 映射（`projects/game/memory/handler/handler.go:219-234`）；JS 客户端当前丢弃该字段（`common/js/dsh-plugins/memory-service/src/client.ts:303-310`）——客户端捕获即可，服务零改动。
- **proto-loader Timestamp 表示**：当前加载选项 `longs: String`（`client.ts:151-159`）下 `google.protobuf.Timestamp` 以 `{seconds: string, nanos: number}` 对象到达——归一化为 epoch ms 整数（D4）。

## 决策清单

### D1: team 插件定义成员消息源接口（依赖倒置）——成员实现接口提供消息，team 不感知成员种类

- **Decision**: `TeamMemberRegistration.agent: AgentHandle` 泛化为 `source: TeamMemberSource`——team 插件拥有的成员消息源契约：
  - `id: string`（稳定成员标识：team 映射键、广播 sender 键）；
  - `events: readonly SessionEvent[]`（产出 log，共享事件词汇表，快照语义）；
  - `subscribe?(onEvent)`（可选：产出实时通知；drain 时派生是读权威，live 仅为优化与投影订阅面）；
  - `sectionTarget?`（可选：team section 注入目标；缺省 = 无 prompt 注入）；
  - `consumes?: boolean`（缺省 true；false = announce-only——不是 relay 接收方、不建 pending、不可 drain）。
  team 同时导出 `agentMemberSource(handle: AgentHandle): TeamMemberSource` 适配器（id = agent id、events = `agent.session.events`、subscribe = `agent.ctx.on("session/event")` 按成员过滤、sectionTarget = `ctx.systemPrompt.section`、consumes = true）——现有 agent 成员语义零变化。`deriveUnits`/`consumedAnchors`/`renderBroadcast`/`buildBroadcastMessage` 逻辑零改动，改读 `source.events`；`PendingUnit.senderSessionId`/`TeamBroadcastSource.senderSessionId` 语义放宽为"sender 成员 id"（agent 即 session id），类型放宽为 string。
- **Rationale**: 消息的派生源是成员 log（R0 第 1 条）——把"成员"抽象为消息源是群聊原语的本来形状（与 dsh "session = event-sourced log" 同构，R0 第 3 条）。非 agent 成员（本次扫雷系统、未来任意系统角色）作为 source 实现者接入，经**完全相同的**派生/渲染/消费闭包路径，team 无任何"系统广播"特设概念；后续新增成员种类不改 team。
- **Alternatives considered**:
  - team 插件加 `systemBroadcast` API + team 态 system 单元表：在 team 内引入第二类单元与专用广播概念，特设路径与成员产出双机制并存，架构负担留在 team。
  - orchestrator 直接把合成消息塞进驱动消息集：消费记账搬到编排层，破坏"驱动输入 = drain 结果 + 排队消息"的不变量形态，player 侧投递状态需跨阶段维护。
  - 给 saolei 建真实 Agent 成员：需 preset/model，违反 FR-001。

### D2: 播报触发点 = orchestrator `nextStep()` case 4 内、`drain(planner)` 之前；exactly-once 用记录级 guard

- **Decision**: case 4 命中未复盘终局记录时，先 `this.saolei.announce(gameStatsText(event))`（向扫雷成员 log 追加一条 `assistant/message` 形态事件），再 `drain(planner)`（派生必含该单元，排复盘输入集末位——followup 位）。guard：`statsSentFor: GameEventRecord | null` 记录级对象恒等（镜像 `reviewedGameEvent` 模式），同记录不重复 announce；pendingReview 重试（case 2）短路 case 4，无二次播报。附带行为增强：存在未复盘终局记录时 planner drain 必非空 → 复盘保证启动（原 `relays.length === 0` 落入 player 续驱的分支对该场景不再可达）。
- **Rationale**: FR-003 绑定交接路径的精确落点；跳局不补发由 case 1 优先序 + 记录单槽覆盖零特判导出（R0）；取消（paused 短路 pump）与刷新（dispose 清态）语义零改动。
- **Alternatives considered**: runtime 侧终局时直接播报——runtime 无成员 log 所有权且呈现决策下沉游戏层；team 侧按锚去重——防重责任分散且无法区分"同记录重发"与"新记录"。

### D3: 每局 per-type 操作计数在 GameRuntime 内维护，随 `GameStats` 携带

- **Decision**: `GameRuntimeService` 增 `operationsByType: Record<OperationType, number>`（init 清零、`executeOperation` kind==="ok" 时按 `op.type` 递增——与 `operationCount` 同点同口径，分项和恒等于总数）；`computeGameStats` 签名扩展接收该表，`GameStats` 增 `operationsByType` 字段，随 `GameEventRecord.stats` 终局携带。
- **Rationale**: `GameStats` 就是"每局定量统计"实体（`common/js/dsh-plugins/saolei-loop/src/game/board.ts:177-186`）；计数器模式与 `operationCount` 一致。
- **Alternatives considered**: 播报时从 `gameLog` 重放推导——口径绑死日志结构；放 `GameEventRecord` 顶层——归属不如 stats 内聚。

### D4: 记忆快照近因注入——客户端捕获 `update_time` 归一化 epoch ms，渲染层倒排 + 截取

- **Decision**: `MemoryEntry` 增 `updateTime?: number`（epoch ms 整数；`{seconds: String, nanos}` → `Math.round(Number(seconds)*1000 + nanos/1e6)`，缺失保持 undefined）；`renderMemorySnapshot(entries)` 按 `updateTime` 降序（undefined 视为最旧）、并列以 `memory_id` 升序打破，截取前 10 条；空集仍渲染空串。写路径与快照冻结时机零改动。
- **Rationale**: 服务已返回字段（R0），客户端捕获是最小改动面；排序/截断属呈现策略。
- **Alternatives considered**: Go 服务实现 order_by——面大且无第二消费者；快照标注条目总数——spec Assumptions 已裁定不标注。

### D5: 扫雷系统成员以常规 announce-only 成员注册（roster 自然渲染）

- **Decision**: orchestrator 物化注册时，registration.members 增第三项 `{source: saolei 成员 source, role: "saolei", summary: "扫雷系统，终局播报对局结果与操作统计"}`（saolei-loop 常量并导出）。成员 id = `${session}/saolei`（与 `${session}/player`、`${session}/planner` 命名同构；不是 dsh session，仅标识）。team section roster 三行自然渲染，无需独立的 announcer 声明面。
- **Rationale**: 成员消息源接口下"广播成员"就是成员——role+summary 即全部注册信息；提示词所有权保持在 team section（059 R2 边界）。
- **Alternatives considered**: `TeamRegistration.announcers` 独立清单——接口泛化后冗余；写进 persona 模板——用户可编辑面不承载系统角色介绍。

### D6: 宿主历史落地 = 订阅扫雷成员的 productions（与 MemberCollector 同构的投影路径）

- **Decision**: 扫雷成员 source 实现 `subscribe`；`TeamSessions.doMaterialize` 在 orchestrator 物化后经 `orchestrator.announcer` 访问器取得该成员并订阅其产出事件，逐条调用 `TeamHistory.appendAnnouncement(role, text)`（`ROLE_AGENT` + 单 text block + `appendMerge(member=role)`——merge 入列 + `team_message` 帧扇出）；退订随 entry teardown。成员视图帧不需专门通路：消费成员经既有 `appendMemberViewUser` 按 `source.kind === "team-broadcast"` 记 `user: [saolei] …` 并扇出 `member_view` 帧。
- **Rationale**: 与既有分层一致——team 拥传输（派生/渲染），宿主拥投影（订阅成员产出写历史），镜像 MemberCollector 订阅 agent `session/event` 的模式；无专用"系统广播事件"。
- **Alternatives considered**: team 服务发 `team/system-broadcast` 事件——投影触发器进 team，特设事件面；从首个成员 log 观测推导 merge 条目——消息未被消费时团队视图永不落条目。

### D7: 统计消息的 wire 形态与前端呈现

- **Decision**: HistoryMessage role = `ROLE_AGENT`；`TeamMergeEntry.member` 本为 string、`appendMerge` 参数类型放宽收 string；前端零改动（团队视图按 member 串渲染标签并按连续同 member 分组，saolei 消息自成一组；成员视图经 `member_view` 呈现注入形态）。
- **Rationale**: 复用既有渲染路径；`ROLE_AGENT` 与"非用户输入的成员侧消息"语义一致。
- **Alternatives considered**: `ROLE_USER`——与用户输入语义不符；系统气泡样式——spec 已裁定不做（普通成员消息样式）。

### D8: team section 提示词补两处表述

- **Decision**: (a) 广播格式说明段末尾追加：标签格式仅是系统呈现**他人**输出的包装，成员自身输出不需要也不应使用 `<角色-message>`/`<角色-tool-call>` 等标签自我包装；(b) roster 含 saolei 行（D5，成员注册自然导出）。措辞终态见 `contracts/team-member-source.md` §4。
- **Rationale**: FR-008；单一所有者在 team section（060 §4 wire 形态同源约定）。
- **Alternatives considered**: 各 persona 模板分别补——用户可编辑面不承载系统级约定。

### D9: 扫雷成员播报 log 内存态、随物化生命周期（2026-09-15 用户裁定）

- **Decision**: 扫雷成员 log 为进程内存态，生命周期 = team 物化（刷新 dispose 即清、重启即失）——与 agent 成员 log 的实际行为完全对齐（R0 第 5 条：agent log 本就非长期持久化）。持久化不做；成员源接口（`events` 由成员自持）已把 log 隔离在 saolei-loop 内，未来加持久化 store 不动 team。
- **Rationale**: 语义一致性（059 刷新清空短期记忆、team 进程内存态均为既有约定）；避免隐式改变刷新/重启后的重投行为（新成员 reconcile 无消费锚会全量重收历史播报，需独立产品裁定）。
- **Alternatives considered**: 进程内跨刷新保留/落盘跨重启——均超出 agent 成员行为对齐面，重投语义需另行裁定，留作后续可选演进。

## 涉更改文件清单（供 tasks 规划参考）

| 层 | 文件 | 变更 |
|---|---|---|
| team 插件 | `common/js/dsh-plugins/team/src/team.ts` | `TeamMemberSource` 接口 + `agentMemberSource` 适配器 + 注册/drain/relay/reconcile 读 source（announce-only 能力位） |
| team 插件 | `common/js/dsh-plugins/team/src/broadcast.ts` | sender 键语义放宽（成员 id，类型 string） |
| team 插件 | `common/js/dsh-plugins/team/src/section.ts` | roster（含 saolei 行）+ "仅输入侧"表述 |
| team 插件 | `common/js/dsh-plugins/team/src/index.ts` | 导出面：`TeamMemberSource` / `agentMemberSource` / 类型形状更新 |
| saolei-loop | `common/js/dsh-plugins/saolei-loop/src/announcer.ts`（新） | `SaoleiSystemMember`：内存 log + `announce()` + source 实现（subscribe/announce-only） |
| saolei-loop | `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` | case 4 announce + `statsSentFor` guard + 注册第三成员 + `announcer` 访问器 |
| saolei-loop | `common/js/dsh-plugins/saolei-loop/src/game/{runtime,board,text}.ts` | per-type 计数 + GameStats 扩展 + 消息模板 |
| saolei-loop | `common/js/dsh-plugins/saolei-loop/src/index.ts` | 导出面更新 |
| memory-service | `common/js/dsh-plugins/memory-service/src/{client,snapshot}.ts` | updateTime 捕获归一 + 倒排截取 |
| 宿主 | `projects/game/agent_v2/src/{session,history}.ts` | announcer 订阅 → `appendAnnouncement`；merge member 放宽 string |
| 测试计划 | `projects/game/testplan/agent_v2_{game,conversation}_test.go` | 播报/跳局断言 |

proto / Go 服务 / web 前端 / preset 模板：零改动。

## 结论

无 NEEDS CLARIFICATION 残留；三个子需求的技术路径均已收敛为单一可选方案（D1–D9），契约面见 contracts/ 三份文档。
