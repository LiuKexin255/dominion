# Research: 077 成员统一性补缺（announcer 同权 + team 统一消息流 + 在途广播快路）

**性质**: plan 阶段设计决策记录 | **Date**: 2026-10-16
**Spec**: [spec.md](spec.md) | **调研基线**: `survey/saolei-hook-scripts.md`（K4/§2.2.4/§2.2.5 裁定）

本文件记录 plan 阶段裁定的技术决策（D1–D9）。每项含决策、理由与被否决备选。用户裁定项标注日期。

## D1 统一成员事件流的接入路径（用户裁定 2026-10-16，扩展性视角）

**Decision**: team 插件**拥有统一流的类型与语义**；loop（orchestrator）只做**零形状传递**；宿主**单路订阅 team 作用域事件流**。

分层终态：

1. **team 拥有**：`TeamMemberSource` 增加可选 `live` 面（D2）；`register()` 返回的 `TeamHandle` 增加事件流面 `events()`（D3）。两者均为场景无关原语——角色是开放字符串、成员是消息源，未来任何场景的 loop 注册成员后即自然拥有该流。
2. **loop 零形状传递**：`TeamOrchestrator` 新增只读读取面（如 `teamEvents()`），**直接返回 team 拥有的流对象**——无场景词汇、无自有形状（返回类型即 team 插件类型）。未来场景的 loop 同样一行传递。
3. **宿主只面向 team 类型编程**：单路订阅 team 作用域流、按信封成员标注分发；投影层不知道、也不需要知道成员来自哪个 loop。

**Rationale**: 用户追问"假设以后会增加 saolei 以外的场景怎么设计"后的裁定。若读取面是各 loop 自有形状，宿主投影层与每个 loop 逐一耦合（每新增场景适配一次）——这正是分层要避免的；若宿主直连 team 并自持 source id 键规则（`String(agent.id)` 是 `agentMemberSource` 的内部细节，`common/js/dsh-plugins/team/src/team.ts:99-122`），键规则泄漏到宿主，且未来非 agent 成员可能没有等价 id 语义。

**Alternatives**:
- 宿主直连 `ctx.team`（按成员 source id 解析 team）：少一层传递被否决——键规则泄漏（见上）。
- per-member 订阅（每成员一个流句柄）：被否决——"单路"语义弱，announcer 与 agent 事件订阅形态不对称。
- loop 自有形状读取面（orchestrator 定义自己的事件流类型）：被否决——每场景一份形状定义，宿主逐 loop 适配。

## D2 agent-only 事件的搬运：source 可选 live 面

**Decision**: `TeamMemberSource` 增加可选 `live` 订阅面：`live?: { subscribe(onEvent: (event: TeamLiveEvent) => void): () => void }`。`agentMemberSource` 实现它——订阅 dsh 根事件 `agent/assistant-stream`/`agent/status`/`agent/error`，按 agent 身份过滤后转发（与既有 `subscribe` 对 `session/event` 的过滤模式同构，`team.ts:110-116`）；announcer（announce-only）结构性无此面。

`TeamLiveEvent` 是 agent 适配层的类型联合（assistant-stream 帧 / status 迁移 / error），定义在 team 插件的 agent 适配器旁——team 的**成员抽象**（`TeamMemberSource` 接口语义）保持不感知"成员是 agent 还是系统角色"（065 §1 依赖倒置）；live 面对 team 是"成员活动通知"，team 只标注转发、不解释。

**Rationale**: 宿主现有三类投影输入中两类是 agent 独有事件（`projects/game/agent_v2/src/history.ts:601-620` 的四类根事件订阅）；搬运放 source 适配层使 team 服务对事件词汇保持中立，依赖倒置不破坏（survey §2.4 目标方向 1）。

**Alternatives**:
- team 服务直接订阅 dsh 根事件：被否决——team 感知 agent 词汇，破坏 065 §1 依赖倒置。
- 宿主保留根事件订阅（只收敛 session 事件）：被否决——缺口②未闭合，三路订阅仍在。

## D3 事件流面形态：`TeamHandle.events()` 与信封

**Decision**: `TeamHandle`（`register()` 返回值，`team.ts:143-145`）增加 `events(): TeamEventStream`；`TeamEventStream = { subscribe(consumer: (e: TeamMemberEvent) => void): () => void }`。信封：

```ts
interface TeamMemberEvent {
  readonly role: string;      // 注册角色（开放字符串：player/planner/announcer/未来场景角色）
  readonly memberId: string;  // source id（agent 成员 = dsh session id）
  readonly channel: "log" | "live";
  readonly event: SessionEvent | TeamLiveEvent;  // log = 成员 log 产出事件；live = 成员活动事件
}
```

- "log" 通道：team 既有 `state.eventOff` 订阅（`source.subscribe`，relay 的输入）**同源复用**——fan-out 给 relay 之外再扇出给流订阅者，两类消费者看到同一事件、同一序。
- "live" 通道：source 的 live 面事件（D2），team 标注转发。
- 生命周期：流随 registration 存续——成员移除/团队 dispose 后不再有事件；订阅 detach 由消费者负责（宿主 entry teardown 单路退订）。

**Rationale**: TeamHandle 天然 team 作用域（register 幂等刷新语义不变）；orchestrator 持有 handle（`orchestrator.ts:518` `this.teamRegistration = ...register(...)`），读取面传递零成本。

**Alternatives**:
- `Team` 服务新增 id 解析面（`ctx.team.events(memberId)`）：被否决——要求消费者持键（D1 已否决泄漏路径）。
- 信封携带完整 role 类型联合：被否决——角色是开放字符串（065 §3），未来场景角色零改动流通优先。

## D4 成员投递面（快路的机械 seam）

**Decision**: `TeamMemberSource` 增加可选 `deliverInFlight?: (message: UserMessage) => void`——与 `consumes` 同级的**结构性属性**（类型可缺省、语义非配置项）。供给点：orchestrator 的 `createMember`（`orchestrator.ts:790-796`，source 与 AgentMember 适配器同域）组合装配——agent 成员的投递面闭包捕获适配器：

```text
策略（agent 成员统一实现，一处）：
  适配器在途（busy）→ agent.inject(message)   // 不唤醒驱动，下一 step 边界认领
  适配器静止        → no-op                    // "我不是接收方"
异常处理：inject 抛出 → 捕获 + warn 日志，回落 pending 基线（不向上传播——announce 路径不容中断）
```

非 agent 成员（announcer）结构性无此面（不给 `deliverInFlight` 即可，与 `consumes: false` 的既有缺席形态一致）。

**Rationale**: survey §2.2.5①/③ 裁定（team 注册面提供投递面、成员侧供统一策略、种类结构性属性非选择项）；组合点选 createMember 因为 source（`agentMemberSource(handle)`）与适配器（`new AgentMember({...})`）在那里同时可见，无需改 `agentMemberSource` 纯函数签名。

**Alternatives**:
- `agentMemberSource(handle, adapter)` 双参扩展：可行但把 loop 层类型引进 team 适配器签名；组合式（展开覆盖）更松。
- 投递面放 `TeamMemberRegistration`（注册项）：被否决——语义上是成员的固有能力面而非注册数据，与 `consumes` 同位（source 上）更一致。

## D5 快路触发点与消息构造

**Decision**: team 的 `relay()`（`team.ts:471+`）在处理 **message 单元**（`kind: "message"`，即插入式产出——发送方为非在途成员的发言；tool 单元不触发——survey §2.2.5② 发送方视角定义）时，对每个带投递面的 consuming 接收方：

1. pending 锚点追加**照常发生**（基线不动——叠加关系）；
2. 经**与 drain 完全相同的构造路径** `buildMessage(unit, team)`（`team.ts:304-313` → `buildBroadcastMessage`：同锚点、同 `team-broadcast` source、同渲染）构建消息；
3. 调用接收方投递面（D4 策略过滤后实际生效的只有当前在途成员）。

构造时读取 sender log 的**回读语义**不变——relay 时事件已在 sender log 中（announcer 先追加后通知，`announcer.ts:196-199`；agent session/event 语义同理），锚点可回读；读取失败（corruption）按既有 fail-loud（`renderBroadcast` throw）向上传播——与 drain 同一错误面，不新增错误种类。

**Rationale**: 单一构造路径保证"无快路特制消息形态"（spec FR-007）；exactly-once 不靠快路自身任何记账，完全复用锚点闭包（`consumedAnchors`/reconcile，`broadcast.ts:160-173`、`team.ts` drain 的 reconcile 前置）。

**Alternatives**:
- 快路独立构造（复制渲染逻辑）：被否决——形态分叉，违背"正常广播消息"裁定。
- tool 单元也触发：被否决——插入式产出发送方视角定义为 message；tool 单元的发送者即在途成员本人（relay 已排除 sender），触发面恒空。

## D6 工具单元级有界偏差：保留并契约披露（用户裁定 2026-10-16）

**Decision**: 保留快路消息相对同 step 后继兄弟工具单元的位置偏差（有界于单 step 窗口），在契约中披露；不在本 feature 消除。

**Rationale**: 发言级（assistant message）顺序已跨成员完全一致（drain 序按 `compareUnits` 生产坐标、快路消息落点在其生产时刻所属 step 窗口内）；消除需把工具单元锚点从 call 时间改为 result 时间（或 drain 按接收方认领位置归并）——改变既有 drain 排序保证与全量相关测试基线，风险大于收益；后续 hook 场景实测若有痛再评估。

**Alternatives**: 锚点改 result 时间 / 认领位置归并——被否决（改动面扩大到既有广播排序语义）。

## D7 宿主成员持有的统一与属性派生

**Decision**: `TeamEntry.members` 从 `Record<AgentMemberRole, MemberRuntime>` 泛化为覆盖三成员的统一集合；`MemberRuntime` 变为判别联合：

```text
MemberRuntime =
  | { kind: "agent"; role; preset; model; handle; collector }   // player/planner
  | { kind: "announce-only"; role }                              // announcer（无 runtime 字段）
```

派生规则（取代角色字符串特判，`session.ts:1041-1111` 的特例点全部消解）：

| 投影面 | 派生规则 |
|---|---|
| 名册（toTeamView） | 遍历统一成员集合；`announce-only` ⇒ 空 preset/model 项（无 `announcerMemberView` 特例构造） |
| GetTeamMember | `announce-only` ⇒ 同步返回空形态视图（无提前 return 角色分支；agent ⇒ 既有 assembly 读取路径不变） |
| 成员视图（listMemberMessages） | `announce-only`（⇒ `consumes: false` 语义）⇒ NOT_FOUND（属性派生，错误信息语义保持） |
| teardown | 按统一成员列表释放；`announceOff` 旁路字段消失（订阅释放并入统一流退订，D8） |
| 在途成员判定（activeAgentMember） | `snapshot().active` + 成员 kind 过滤（属性判定取代 `isAgentMemberRole` 角色串） |

**Rationale**: spec FR-004/FR-005；076 已建立呈现契约（名册/active_member/读取），本 feature 只清实现侧特例——行为保持由 076 验收面回归保证（`specs/076-agent-v2-optimize/contracts/announcer-member.md` §6）。

**Alternatives**: 保留 `Record<AgentMemberRole, ...>` + announcer 旁路字段的现状：被否决——即 K4 缺口①本身。

## D8 MemberCollector 演化与测试适配

**Decision**: `MemberCollector`（`history.ts:581+`）的**记账机械保留**（turn 簿记、流式块、工具 settle、idle 收束、markOutcome），**输入源切换**：构造不再订阅四类根事件，改为暴露信封分发入口 `applyEvent(envelope: TeamMemberEvent): void`（log 通道 → session 事件处理、live 通道 → stream/status/error 处理）；宿主 entry 持有**单路** team 流订阅，按信封 `role` 分发到对应成员记账对象；announcer 的 log 产出（`assistant/message`）→ 既有 `appendAnnouncement` 投影（触发源收敛，方法保留）。

测试适配：`session.test.ts` 的 `emit(h, name, ...args)` helper（root 事件驱动）改为经统一流驱动——测试构造真实 `Team` 服务（纯类，`new Team(ctx)`）或经成员 source 注入事件；**行为断言不变**（帧序/历史/视角对外等价，FR-003 基准）。

**Rationale**: 记账机械与输入源正交，保留前者使收敛风险最小；测试驱动面随生产驱动面切换是重构等价性的自然含义。

**Alternatives**: 宿主改为单一巨型 collector（无按成员对象）：被否决——按成员记账与 markOutcome 寻址天然按成员，无需摊平。

## D9 契约修订方式

**Decision**: `specs/065-agent-v2-team-refine/contracts/team-member-source.md`、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §1、`specs/076-agent-v2-optimize/contracts/flow-orchestration.md` 按**终态表述直接修订原文**（宪法 VII：不留增量过程痕迹；076 先例——修订处以本 feature spec 为引用来源）；本 feature `contracts/` 三份设计契约承载新增语义的完整定义：

- [contracts/member-event-stream.md](contracts/member-event-stream.md)——统一成员事件流（D1–D3）
- [contracts/inflight-broadcast-fastpath.md](contracts/inflight-broadcast-fastpath.md)——在途广播快路（D4–D6）
- [contracts/host-member-parity.md](contracts/host-member-parity.md)——宿主成员同权（D7–D8）

**Alternatives**: 在旧契约追加"增量勘误"段落：被否决——违背终态表述原则，读者需拼接多文档还原现状。
