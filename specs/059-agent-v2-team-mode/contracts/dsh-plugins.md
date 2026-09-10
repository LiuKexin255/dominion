# Contract: 自研 dsh 插件内部契约（team / saolei-loop / memory / saolei）

> 进程内插件契约（`common/js/dsh-plugins/`）。组合挂载与依赖方向见 [research.md](../research.md) R1/R5/R6/R7；机制依据：`survey/deepseek-harness-team-mode.md` §4/§5/§9、`survey/deepseek-harness-memory-plugin.md`。

## 1. team 插件（`@dominion/dsh-team`，新增）

**服务面**：`ctx.team`（host 行；场景无关，不含 saolei 概念）。

```typescript
interface TeamMemberRegistration {
  agent: AgentHandle;
  role: string;            // 开放字符串（saolei 侧为 "player" | "planner"）
  summary: string;         // 一句话职责摘要（名册渲染用，R2 边界：索引而非详情）
}
interface TeamRegistration {
  goal: string;            // 团队目标文本
  members: TeamMemberRegistration[];
  context?: string;        // 泛化关联键（saolei 侧填局 id；team 不理解语义）
}
ctx.team.register(registration): TeamHandle
ctx.team.drain(member): UserMessage[]   // 注入就绪的广播消息（team 内部按锚点读取实际内容并构造；
                                        //   索引与读取是 team 内部逻辑，不对外暴露索引）
```

**行为规范**：

1. **register**：幂等注册（同 agent 重复注册刷新条目）；对每个成员经 `agent.ctx` 注册 team section（order 1–49；内容 = goal + 名册（每成员 `[role] summary` 第三人称）+ 广播格式约定；**不含成员第一人称身份**，R2）；订阅成员 `session/event` 收集产出；成员 dispose 时注册与 buffer 自动清理（scope unwind）。
2. **收集**：成员 log 的 `assistant/message`（发言）与 `tool/call`+`tool/result`（按 callId 配对为一条广播单元）——"team-visible means logged"，广播面严格限于成员 log 产出（team-mode 决策 ⑧）。
3. **广播（引用投递）**：成员产出事件 → 将锚点（messageId / callId 对）按事件到达序追加进**除发送者外每个成员的待消费列表**（不写 agent log——决策 ⑦；不复制消息内容）。team 不维护全局消息副本队列；若实现存在中转列表，锚点进入全部接收方列表后即移除，不滞留已完成广播的消息。
4. **drain（构造注入）**：编排层调用 `drain(member)` → team 按锚点从成员 session log（既有读取面）读取实际内容 → 渲染广播格式 → 返回注入就绪的 UserMessage 序列（drain 同时标记消费——注入落 log 后由消费锚点闭环，见 5）。广播格式：
   - 发言：`[sender] 摘要` + `<sender-message>…正文…</sender-message>`；
   - 工具：`[sender] 工具调用 <tool> (context)` + `<sender-tool-call>tool/args/result</sender-tool-call>`（result 原样全文，wire 序列化差异不算；不摘要不聚合）。
   - 标签词汇（`<*-message>`/`<*-tool-call>`）在 team section 中声明，不与 dsh 自身标签冲突。
5. **待消费列表派生重建**（决策 ⑮ + 引用模型）：待消费列表非事实源（仅锚点、无内容副本），可在重建时由「sender log 产出 − receiver 消费锚点（receiver log 中 `source.kind === 'team-broadcast'` 的 `user/message` 的 `messageId` 集合）」推导；重建即一致、exactly-once 天然成立（丢失自愈、已消费不重复）；顺序：串行驱动下按驱动轮次归并（锚点插入序即事件到达序）。
6. **MessageSource 扩展**（merge，declaration-merge）：`"team-broadcast": { kind: "team-broadcast"; role: string; senderSessionId: SessionId; messageId: MessageId; context?: string } & ContextFormed`（`form: 'relay'`）。
7. **team 只投递不驱动**：不调用成员的 `followup/inject/cancel`；不持有游戏概念；索引细节与读取路径不对外部（编排层/宿主）暴露。

## 2. saolei-loop 插件（`@dominion/dsh-saolei-loop`，重构为 team loop）

**职责**：驱动权唯一归属（游戏阶段机 + 驱动时机 + buffer 消费策略）、物化编排、游戏事件流持有。**不再** `setFactory`、不含 turn/step 驱动状态机（官方 `dsh-agent-loop` 行回归承担）。

**物化编排契约**（由宿主 `UpdateTeam` 触发）：

```text
validate(presets, models)                       # fail-fast：preset 存在 + role 匹配 + model 在目录
for each (role, presetId, model) in {player, planner}:
  ctx.agents.create({
    sessionId: <成员 sessionId 命名（挂 session 命名空间）>,
    agentOptions: { provider: "glm-responses", model },
    meta: { agentPreset: presetId },
    setup: async (agentCtx) => {
      await mountFromRoster(agentCtx, presetId)          # compose() 返回的 setup（resolve 提前已做）
      if (role === player)  registerGameRuntime(agentCtx) # agent-scoped ctx.saoleiGame（GameRuntime）
      if (role === planner) await ctx.plannerMemory.load(agentCtx, {template, session})  # fail-loud
    },
  })
ctx.team.register({ goal, members, context? })
# 物化成功后静止等待：初始激活成员 = planner，不自动驱动任何成员（执行期用户裁定 2026-09-10）
# 游戏首次驱动由用户首条消息触发：驱动 planner（drain → followup；首驱输入 = 用户消息本身，drain 为空）
```

**编排状态机契约**（交替激活；详见 [data-model.md](../data-model.md) §5 状态图）：

- 驱动成员 = `ctx.team.drain(member)` → 构造 UserMessage（source: `team-broadcast`/用户消息）→ `agent.followup()` → 等待 `agent/status` idle。一切驱动（用户首驱、结构性续驱、gameEnded 复盘驱动、排队消化）的输入皆为群聊消息（drain 返回的未消费团队消息 + 排队用户消息），编排层不合成任何驱动消息；无未消费团队消息且无排队消息时，编排层保持静止在当前激活成员（不合成消息、不空转）。
- 结构性续驱：planner 回合结束 → 若有排队用户消息先由 planner 消化（消化优先于切换）→ 消化完成且 planner 静止后续驱 player——静止 = turn 结束并且不会再触发新的 turn（工具调用引发的后续 turn、待消化排队消息两种触发源都不存在；机械信号即上面的 `agent/status` idle）——续驱仅在 player 存在未消费团队消息（planner 产出广播）时发生（驱动输入 = drain(player)，无合成消息；无输入则编排层静止在当前激活成员）；player 侧 gameEnded 事实确立于 saolei 工具返回游戏终局结果（won/lost，即 `saolei_operate` 终局结果——终局记录由 GameRuntime 在该次工具执行内写入，编排层经 `peekGameEvent` 读取）→ 驱动 planner 复盘（驱动输入 = drain(planner)——player 游戏过程的广播，无合成消息）。
- 取消：编排层暂停续驱标志（配合成员 `cancel()`）；用户再次 Send 恢复。
- 游戏事实（局开始/结束、胜负）由 GameRuntime 的游戏事件流承载（game 模块位于 saolei-loop：`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts`，经 agent-scoped `saoleiGame` 暴露给 player 侧；gameEnded 事实随 saolei 工具返回终局结果一并确立），与 team 消息流解耦（决策 ⑨）；**不经 team 广播游戏事件**（buffer 派生前提，team-mode §4.4a 边界）。

## 3. memory 插件（`@dominion/dsh-memory`，新增）

**两个功能面**（memory-plugin 决策 ①②③）+ **host 服务面**（决策 ⑥ 路径 A）：

1. **工具行（preset 行挂载）**：`defineTool` 注册单一 `memory` 工具——参数 `{action: add|replace|remove, content, old_text}` 单操作形式 XOR `{operations[]}` 批量形式（互斥校验；批量原子：preflight 全过才提交）；`old_text` 大小写敏感子串定位（0 命中/多命中返回全部条目或预览的文本结果）；无 read 动作；**失败也是文本结果**（不抛错、不中断对话）；存储访问经 host 服务面（工具 exec 时解析，per-agent scope 键控）。
2. **快照 section（preset 行挂载）**：函数式 `text: (context) => snapshotCache.get(context.scope) ?? ""`，order 200+，空自动不渲染；快照由物化 setup 的 `load` 预取填充（先于首次装配，无竞态）；实例生命周期固定。
3. **host 服务面（host 行，`ctx.plannerMemory`）**：`load(agentCtx, {template, session})`——异步读 memory 服务（gRPC client，迁移自 v1 `projects/game/agent/src/memory-client.ts`，`dominion:///game/memory:50051`）→ 渲染纯文本快照（每行一条、不含 id）→ 写入快照缓存并经 `agentCtx` 绑定 scope；**失败 throw**（= 物化回滚，fail-loud，决策 ⑧）；写路径（add/replace/remove/批量）同经此服务面落 memory 服务，立即持久化。
- **不注册 guidance section**（单工具无跨调用协调需求）；工具 description 改写为 v2 快照语义（"快照固定于 agent 启动"）。
- **guidance/工具一致性**：无 guidance 即无一致性问题；行级挂载保证不挂行则工具与 section 同时缺席。

## 4. saolei 插件（`@dominion/dsh-saolei`，演进为 preset 层挂载）

- 三工具（`saolei_init`/`saolei_operate`/`saolei_remain`）+ `saolei:guidance` section（order 100）结构不变（工具 + 配套守则同 `apply()` 注册，行级一致，V2-3 同型）。
- 挂载位置变化：从 host 组合层移入 **player 池模板 preset 的插件行**（planner 池不含此行——工具可见性由挂载层隔离，不用 per-agent restriction，team-mode §3.6）。
- 工具经 `exec.agent.ctx.get("saoleiGame")` 解析 agent-scoped 游戏运行时（GameRuntime：棋盘/规则/操作执行/胜负判定 + desktop 派发绑定 sessionName）——GameRuntime 注册点从现 saolei-loop factory 迁至编排层物化 setup（§2）；缺失时 fail-loud（现状语义）。

## 5. 组合清单（agent_v2 `cordis.yml` 终态行清单）

```yaml
# 既有基线：timer / llm / session / system-prompt / tools / agents / invariants
#           + invariant-session / invariant-agent / invariant-scope / llm-retry
#           + llm-glm / desktop-bridge / saolei-loop（语义已变为 team loop）/ saolei（若 host 层保留则移除——见下）
dsh-agent-loop        # 新增：官方驱动回归（setFactory；Config.agents[] 留空，动态创建）
dsh-agent-presets     # 新增：roster（roots: [player 池, planner 池]，不设 default）
@dominion/dsh-team    # 新增：ctx.team
@dominion/dsh-memory  # 新增：ctx.plannerMemory（host 服务面；工具行经 planner 模板 preset 挂载）
```

saolei 工具行从 host 层移入 player 模板 preset（host 层不再挂，避免全员可见）；组合三面（package.json ⟷ cordis.yml ⟷ tar 物化）原子变更（闭包审计）。
