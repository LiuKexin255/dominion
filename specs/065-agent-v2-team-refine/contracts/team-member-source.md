# Contract: team 插件——成员消息源接口（TeamMemberSource）

**Feature**: specs/065-agent-v2-team-refine/spec.md（FR-001/FR-004/FR-006/FR-008）
**实现锚点**: `common/js/dsh-plugins/team/src/{team,broadcast,section}.ts`
**基线契约**: `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §1（群聊原语）、`specs/060-agent-v2-team-optimize/contracts/team-api.md` §4（广播单一 XML 形态）——本文件是两者的增量修订，冲突处以本文件为准。

## §1 成员消息源接口（依赖倒置点）

team 插件只认"成员 = 消息源"，不感知成员是 agent 还是系统角色：

```ts
/** 一个 team 成员的消息提供面——依赖倒置点：成员实现它以提供消息。 */
interface TeamMemberSource {
  /** 稳定成员标识：team 映射键、广播 sender 键（agent 成员 = 其 dsh session id）。 */
  readonly id: string;
  /** 成员产出 log（共享 session-event 词汇表，只读快照语义）。 */
  readonly events: readonly SessionEvent[];
  /** 产出实时通知（可选）：只投递该成员自己的产出事件；drain 时派生是读权威，live 仅为优化与投影订阅面。 */
  subscribe?(onEvent: (event: SessionEvent) => void): () => void;
  /** team section 注入目标（可选）：缺省 = 无 prompt 注入。 */
  readonly sectionTarget?: TeamSectionTarget;
  /** 是否接收广播（缺省 true）；false = announce-only——不是 relay 接收方、不建 pending、不可 drain。 */
  readonly consumes?: boolean;
}

interface TeamSectionTarget {
  section(spec: { name: string; order: number; text: string }): () => void;
}

/** Agent 成员的适配器：现有 agent 成员语义零变化。 */
function agentMemberSource(handle: AgentHandle): TeamMemberSource;
// id = String(agent.agent.id)；events = agent.agent.session.events；
// subscribe = agent.ctx.on("session/event") 按 session id 过滤；
// sectionTarget = agent.ctx.systemPrompt.section 委托；consumes = true。
```

`TeamMemberRegistration` 以 `source: TeamMemberSource` 取代 `agent: AgentHandle`（role/summary 语义不变）；`PendingUnit` 的 sender 键与 `TeamBroadcastSource.senderSessionId` 语义放宽为"sender 成员 id"（agent 成员即其 session id），类型放宽为 string（字段名保留——注入消息的既有持久来源形态）。

## §2 传输语义（派生/渲染/消费全复用，逻辑零改动）

1. `deriveUnits`/`consumedAnchors`/`renderBroadcast`/`buildBroadcastMessage` 改读 `source.events`，函数逻辑不变：任何成员的 `assistant/message` 形态产出（非空正文）即一个发言单元，`tool/call`+`tool/result` 配对即工具单元——**系统成员的播报与 agent 成员的发言走完全相同的路径**。
2. 渲染形态：`<{role}-message>` 单标签对、无头行（060 §4 既有形态）；注入消息 `source = {kind: "team-broadcast", role, senderSessionId: <成员 id>, messageId: <发言锚>, form: "relay"}`。
3. 消费闭包：接收方 log 出现 `source.messageId === 锚` 的注入即闭包；`drained` 集合保护"已弹出未落日志"窗口——per-member exactly-once、自愈、排序（`compareUnits`：time 升序、seq 打破并列）全部既有语义。
4. **announce-only 成员**：`consumes: false` —— live relay 与 reconcile 均不为其投递/构建 pending；`drain` 对其 throw（fail-loud，防御性——编排从不 drain 播报成员）。其 section 不注册（`sectionTarget` 缺省）；roster 照常渲染其 role+summary。
5. 成员移除路径：registration dispose（显式）+ agent 成员的 scope unwind（既有 `agent/disposed` 监听按 source id 匹配，仅命中 agent 成员）。
6. team 服务既有义务不变：从不驱动任何成员（无 `followup`/`inject`/`cancel`）、不解释场景语义。

## §3 roster 与成员注册

`register` 的 members 列表容纳任意 source 成员；saolei 场景注册三项（saolei-loop 侧常量，见 [game-stats-broadcast.md](game-stats-broadcast.md) §3）：

```text
{ source: agentMemberSource(playerHandle), role: "player",  summary: "执行扫雷操作，独占桌面控制" }
{ source: agentMemberSource(plannerHandle), role: "planner", summary: "复盘对局与制定策略，不操作" }
{ source: saolei 成员 source（saolei-loop 实现）, role: "saolei",  summary: "扫雷系统，终局播报对局结果与操作统计" }
```

roster 渲染全部成员（含 announce-only）的 role + 一行摘要；无独立 announcer 声明面。

## §4 team section 文本增量（终态措辞）

`renderTeamSection` 在既有结构上做一处追加、roster 随 §3 自然多一行（其余行不变）：

广播格式说明段（既有"其他成员的输出会以群聊广播消息发送给你…"列表）末尾追加一行：

```text
- 这些标签格式只用于系统向你呈现他人的输出：你自己的输出不需要、也不应该使用 `<角色-message>`/`<角色-tool-call>` 等标签自我包装（正文直接输出，工具调用按工具协议发起）。
```

## §5 验收面（对应 spec FR/SC）

单测：

- 适配器等价：`agentMemberSource` 包装的成员行为与泛化前一致（section 注册、事件过滤、drain、relay）——既有 team 单测全量回归。
- announce-only：不建 pending、不被 relay、drain throw、roster 含其行。
- 派生同权：非 agent source 的 `assistant/message` 事件 → 发言单元 → `<{role}-message>` 渲染 → 注入 → 消费闭包（同 agent 路径断言）。
- section 增量措辞存在（"仅输入侧"行）。
- 大型测试断言面见 [game-stats-broadcast.md](game-stats-broadcast.md) §5 与 quickstart.md。
