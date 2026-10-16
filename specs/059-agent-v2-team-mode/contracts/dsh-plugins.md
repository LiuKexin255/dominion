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
interface TeamHandle {
  dispose(): void;
  events(): TeamEventStream;   // team 作用域统一成员事件流（specs/077-announcer-member-unification/contracts/member-event-stream.md §2.2）
}
ctx.team.register(registration): TeamHandle
ctx.team.drain(member): UserMessage[]   // 注入就绪的广播消息（team 内部按锚点读取实际内容并构造；
                                        //   索引与读取是 team 内部逻辑，不对外暴露索引）
```

**行为规范**：

1. **register**：幂等注册（同 agent 重复注册刷新条目）；对每个成员经 `agent.ctx` 注册 team section（order 1–49；内容 = goal + 名册（每成员 `[role] summary` 第三人称）+ 广播格式约定；**不含成员第一人称身份**，R2）；订阅成员 `session/event` 收集产出；成员 dispose 时注册与 buffer 自动清理（scope unwind）。
2. **收集**：成员 log 的 `assistant/message`（发言）与 `tool/call`+`tool/result`（按 callId 配对为一条广播单元）——"team-visible means logged"，广播面严格限于成员 log 产出（team-mode 决策 ⑧）。
3. **广播（引用投递）**：成员产出事件 → 将锚点（messageId / callId 对）按事件到达序追加进**除发送者外每个成员的待消费列表**（不写 agent log——决策 ⑦；不复制消息内容）。team 不维护全局消息副本队列；若实现存在中转列表，锚点进入全部接收方列表后即移除，不滞留已完成广播的消息。**投递时机**：锚点追加 pending 为可靠基线（拉动式，驱动装配时 drain）；在途广播快路在追加 pending 的同时经接收方投递面 `deliverInFlight` 提前投递一份——该消息经与 drain 完全相同的构造路径构建，是叠加的提前投递、不改变基线（specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md §2）。
4. **drain（构造注入）**：编排层调用 `drain(member)` → team 按锚点从成员 session log（既有读取面）读取实际内容 → 渲染广播格式 → 返回注入就绪的 UserMessage 序列（drain 同时标记消费——注入落 log 后由消费锚点闭环，见 5）。广播格式：
   - 发言：`[sender] 摘要` + `<sender-message>…正文…</sender-message>`；
   - 工具：`[sender] 工具调用 <tool> (context)` + `<sender-tool-call>tool/args/result</sender-tool-call>`（result 原样全文，wire 序列化差异不算；不摘要不聚合）。
   > 格式条款已被 `specs/060-agent-v2-team-optimize/contracts/team-api.md` §4 修订（单一 XML 标注形态，头行废止，2026-09-11 裁定）；本条款保留 059 基线记录形态，现行格式以该修订为准。
   - 标签词汇（`<*-message>`/`<*-tool-call>`）在 team section 中声明，不与 dsh 自身标签冲突。
5. **待消费列表派生重建**（决策 ⑮ + 引用模型）：待消费列表非事实源（仅锚点、无内容副本），可在重建时由「sender log 产出 − receiver 消费锚点（receiver log 中 `source.kind === 'team-broadcast'` 的 `user/message` 的 `messageId` 集合）」推导；重建即一致、exactly-once 天然成立（丢失自愈、已消费不重复）；顺序：串行驱动下按驱动轮次归并（锚点插入序即事件到达序）。
6. **MessageSource 扩展**（merge，declaration-merge）：`"team-broadcast": { kind: "team-broadcast"; role: string; senderSessionId: SessionId; messageId: MessageId; context?: string } & ContextFormed`（`form: 'relay'`）。
7. **team 只投递不驱动**：不调用成员的 `followup/inject/cancel`；不持有游戏概念；索引细节与读取路径不对外部（编排层/宿主）暴露。**投递面归属**：在途广播快路下 team 只调用成员供给的 `deliverInFlight` 闭包，**不直接调用** `agent.inject`——实际 `inject` 发生在成员适配器（loop 侧）内，本条目不变量保持成立（specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md §2.1）。

## 2. saolei-loop 插件（`@dominion/dsh-saolei-loop`，重构为 team loop）

**职责**：驱动权唯一归属（游戏阶段机 + 驱动时机 + buffer 消费策略）、物化编排、游戏事件流持有。**不再** `setFactory`、不含 turn/step 驱动状态机（官方 `dsh-agent-loop` 行回归承担）。

**物化编排契约**（apply 期依赖装配 + 由宿主 `UpdateTeam` 触发的成员物化）：

```text
apply 期依赖装配（specs/074-dsh-grpc-runtime/contracts/team-assembly.md §4）：
  for row in config.plugins:
    entry = await dynamicImport(row.name)   # 按裸包名动态解析（宿主闭包，见 §5 末注）
    await ctx.plugin(entry, row.config)     # 能力提供方子插件（fail-loud）
  saoleiTools / memoryMount = ctx.get(...)  # reflect 注册面的 Context 便捷读取（与 reflect.get 等价）；提供面缺失 → throw（组合启动失败）

validate(presets, models)                       # fail-fast：preset 存在 + role 匹配 + model 在目录
for each (role, presetId, model) in {player, planner}:
  ctx.agents.create({
    sessionId: <成员 sessionId 命名（挂 session 命名空间）>,
    agentOptions: { provider: "glm-responses", model },
    meta: { agentPreset: presetId },
    setup: async (agentCtx, agent) => {
      await mountFromRoster(agentCtx, presetId)          # compose() 返回的 setup（resolve 提前已做）
      if (role === player):
        registerSaoleiTools(agentCtx)                    # saoleiTools 单元：三工具 + saolei:guidance section → 成员域
        agentCtx.systemPrompt.section(gameSection)       # saolei:game（基础面 gameSection）
        句柄注册：agentCtx.isolate("saoleiGame").provide("saoleiGame", this.game)
                                                         # 实例由编排持有（materialize 经 createGameRuntime 构造）
      if (role === planner):
        agentCtx.systemPrompt.section(gameSection)       # saolei:game（074 遗留修复）
        unit = memoryMount.build(memoryScope)            # scope = {template, preset}（specs/071-memory-preset-scope/contracts/dsh-plugins.md §1）显式传参
        register unit                                    # faces("memory") → tools×4 → sections×3 → agentCtx
        await unit.ready()                               # fail-loud 预取；reject → setup throw → 物化回滚
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
- 游戏事实（局开始/结束、胜负）由 GameRuntime 的游戏事件流承载（game 模块位于 `@dominion/dsh-saolei`：`common/js/dsh-plugins/saolei/src/game/runtime.ts` 的 `createGameRuntime` 工厂产出，编排持有实例、player 侧经 agent-scoped `saoleiGame` 句柄暴露；gameEnded 事实随 saolei 工具返回终局结果一并确立），与 team 消息流解耦（决策 ⑨）；**不经 team 广播游戏事件**（buffer 派生前提，team-mode §4.4a 边界）。

## 3. memory 能力（公共库 `@dominion/common-js-memory` + 薄 adapter `@dominion/dsh-saolei-memory`）

> 契约全文见 specs/074-dsh-grpc-runtime/contracts/memory-library.md（库导出面与能力单元）与 specs/074-dsh-grpc-runtime/contracts/team-assembly.md §3.2（adapter 提供方）；本节为 agent_v2 组合视角摘要。能力语义基线（memory-plugin 决策 ①②③⑧）不变。

**形态**（组合清单无 memory host 行、模板无 memory 工具行——§5）：

| 层 | 包 | 形态与挂载 |
|---|---|---|
| 领域公共库 | `@dominion/common-js-memory`（`common/js/memory/`） | 纯库（无 `name`/`inject`/`apply` 导出，永不作为组合行挂载）；原子能力单元工厂 `buildMemoryCapabilities({scope, memoryClient, wikiClient})` + 原语（store/face/快照渲染） |
| 薄 adapter | `@dominion/dsh-saolei-memory`（`common/js/dsh-plugins/saolei-memory/`） | cordis 插件，经 saolei-loop `config.plugins` 挂载为子插件；`inject: ["grpc"]`；config `{ target, protoPath, label? }`——部署连接参数在此，两个 service client 经组合级 `ctx.grpc` 基建构造（同 target 共享 channel）；apply `ctx.provide("memoryMount", { build(scope) })`——per-member 工厂，scope 由编排显式传参 |

**能力单元**（`build(scope)` 产出——faces/tools/sections/ready 原子不可拆，编排整单元注册进 planner 成员域，注册序 faces → tools → sections → ready）：

1. **faces**：`memory` agent-scoped isolate 面；face 创建即绑定 `{template, preset}` scope（specs/071-memory-preset-scope/contracts/dsh-plugins.md §1；wiki 面仅转发 `scope.template`——specs/069 template 级契约）。
2. **tools ×4**：`memory` 工具——参数 `{action: add|replace|remove, content, old_text}` 单操作形式 XOR `{operations[]}` 批量形式（互斥校验；批量原子：preflight 全过才提交）；`old_text` 大小写敏感子串定位（0 命中/多命中返回全部条目或预览的文本结果）；无 read 动作；**失败也是文本结果**（不抛错、不中断对话）；加 `wiki_search`/`wiki_get`/`wiki_write`（specs/069-wiki-template-web/contracts/dsh-plugins.md）。execute 经 `exec.agent.ctx.memory` 属性穿越解析（缺面 fail-loud 文案见 specs/074-dsh-grpc-runtime/contracts/memory-library.md §5）。
3. **sections ×3**：`memory:guidance`(110)、`wiki:guidance`(120)、`memory:snapshot`(200，函数式 `text` 闭包——空快照自动不渲染)；由编排注册（saolei-loop planner setup，specs/074-dsh-grpc-runtime/contracts/team-assembly.md §5②）。
4. **ready**：`() => face.prefetch()`——fail-loud 预取：一次 `listMemories(template, preset, {orderBy: "update_time desc", pageSize: 10})` → 渲染纯文本快照（每行一条、不含 id），冻结于预取时点；reject → setup throw → 物化整体回滚（决策 ⑧）。

写路径（add/replace/remove/批量）以绑定 scope 的 preset 寻址落 memory 服务，立即持久化；同内容去重（ALREADY_EXISTS → dedupe 文本）不变（specs/071-memory-preset-scope/contracts/dsh-plugins.md §3）。

## 4. saolei 游戏基础与工具插件（`@dominion/dsh-saolei` / `@dominion/dsh-saolei-tools`）

- `@dominion/dsh-saolei`（游戏基础）：组合清单**顶级领域基础行**，apply 提供组合级服务面 `ctx.saolei`（`{ createGameRuntime, gameSection }`）；游戏运行时（`GameRuntime` 普通类，构造器仅收 deps）归本包（游戏类型单一出口）——MUST NOT 持有工具或全局注册（specs/075-saolei-game-foundation/contracts/saolei-foundation.md §1/§2/§5）。
- `@dominion/dsh-saolei-tools`（工具插件）：saolei-loop `config.plugins` 子插件（组合级）；三工具（`saolei_init`/`saolei_operate`/`saolei_remain`）与 `saolei:guidance` section 定义零变化，apply 以 `ctx.provide("saoleiTools", { tools, sections })` 暴露静态单元（sections 仅 `saolei:guidance`）——单元由编排 player setup 注册进成员域，不做全局注册（§2；specs/074-dsh-grpc-runtime/contracts/team-assembly.md §3.1）。`saolei:game` section 归游戏基础面，由编排对 player/planner 注册。
- player 隔离为结构性保证：编排 player 分支只消费 saoleiTools、planner 分支只消费 memoryMount（§3）——planner 工具面与 prompt 无 saolei 工具与 guidance 痕迹，不依赖模板行校验（specs/074-dsh-grpc-runtime/contracts/preset-templates.md §3）。
- 工具经 `exec.agent.ctx.saoleiGame` 属性穿越解析 agent-scoped 游戏运行时句柄（`GameRuntime`：棋盘/规则/操作执行/胜负判定 + desktop 派发绑定 sessionName）——实例由编排持有并经 player setup 注册句柄（§2）；缺失时 fail-loud（现状语义）。

## 5. 组合清单（agent_v2 `cordis.yml` 终态行清单）

> 全量行集合与断言契约见 specs/070-dsh-upgrade/contracts/composition-manifest.md §2；模板与能力依赖的分界判据及终态 diff 见 specs/074-dsh-grpc-runtime/contracts/preset-templates.md §5。team 模式相关行：

```yaml
# 既有基线：timer / llm / session / session-projection / system-prompt / tools / agents
#           + invariants / invariant-session / invariant-agent / invariant-scope / llm-retry
#           + llm-glm / llm-opencode-go / desktop-bridge / preset-authoring
- { id: agent-loop, name: '@deepseek-ai/dsh-agent-loop' }                       # 官方驱动回归（Config.agents[] 留空，动态物化）
- { id: agent-preset-registry, name: '@deepseek-ai/dsh-agent-preset-registry' } # roster registry（default: ''，fail-loud）
- { id: grpc-runtime, name: '@dominion/dsh-grpc-runtime' }                      # ctx.grpc 组合级 gRPC client 基建（§3 adapter 依赖）
- { id: saolei, name: '@dominion/dsh-saolei' }                                   # ctx.saolei 游戏基础服务面（§4）
- { id: team, name: '@dominion/dsh-team' }                                      # ctx.team
- id: saolei-loop                                                               # team loop 编排层（inject 'saolei'）
  name: '@dominion/dsh-saolei-loop'
  config:
    plugins:                                                                   # 能力依赖声明（扁平，无角色语义）
      - name: '@dominion/dsh-saolei-tools'                                     # saoleiTools 静态单元提供方（§4）
      - name: '@dominion/dsh-saolei-memory'                                    # memoryMount 工厂提供方（§3）
        config: { target: 'dominion:///game/memory:50051', protoPath: 'projects/game/game.proto' }
```

preset persona 子行（`@deepseek-ai/dsh-persona`）与能力依赖子行（`@dominion/dsh-saolei-tools`、`@dominion/dsh-saolei-memory`）均按裸包名经宿主闭包解析：包声明于本宿主 package.json 并随闭包交付（specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md §4 同型）。模板 plugins 仅 persona 行——工具能力不经模板行挂载（specs/074-dsh-grpc-runtime/contracts/preset-templates.md §1）；组合三面（package.json ⟷ cordis.yml ⟷ tar 物化）原子变更（闭包审计）。
