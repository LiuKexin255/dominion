# Contract: 宿主成员同权（announcer 统一 member 化——持有/名册/读取/视图/teardown 属性派生）

**Feature**: [../spec.md](../spec.md)（FR-004–FR-006/FR-013；US2）| **决策依据**: [../research.md](../research.md) D7–D8
**实现锚点**: `projects/game/agent_v2/src/session.ts`（TeamEntry/memberStateView/toTeamView/getTeamMember/listMemberMessages/teardown）、`projects/game/agent_v2/src/history.ts`（MemberRole/MemberCollector）
**基线契约保持**: `specs/076-agent-v2-optimize/contracts/announcer-member.md` §3（呈现语义验收面全量回归）；本 feature 落地时该契约 §3 item 3（成员读取实现侧表述：现「放宽硬编码 `player`/`planner` roster guard」由成员 kind 派生取代——必做）与 `specs/076-agent-v2-optimize/contracts/flow-orchestration.md` §7 末行按终态修订：**宿主成员事件消费经 team 统一流**；**loop 侧 `orchestrator.announcer` 读取面保留**（仍供成员读取与 loop 测试使用，非宿主订阅路径）——修订为终态表述，引用本 feature spec。

## 1. 统一成员持有

`TeamEntry.members` 容纳全部注册成员（player/planner/announcer），类型为判别联合（[../data-model.md](../data-model.md) §3.1）：

```text
MemberRuntime =
  | { kind: "agent"; role: "player" | "planner"; preset; model; handle; collector }
  | { kind: "announce-only"; role: "announcer" }
```

- `announceOff` 旁路字段消失——announcer 的投影消费经统一流信封（[member-event-stream.md](member-event-stream.md) §5），订阅退订单路（`streamOff`）。
- 类型层面的 agent/非 agent 区分允许保留（判别联合的 kind）；**视图构造与生命周期管理中的角色字符串特判不允许**。

## 2. 派生规则（属性取代特判）

| 投影面 | 派生规则（取代的现状特例） |
|---|---|
| 名册（toTeamView） | 遍历统一成员集合构造；`announce-only` ⇒ 空 preset/model 项（取代 `announcerMemberView` 特例构造 + 三成员拼装特判，`session.ts:1086-1111`） |
| GetTeamMember | 按成员 kind 分派：`announce-only` ⇒ 同步空形态视图（`system_prompt` 空值、无 assembly、无 generation 重试需求——同步构造无竞态）；`agent` ⇒ 既有 assembly 读取路径（generation 重试）不变（取代 `member === ANNOUNCER_MEMBER_ROLE` 提前 return，`session.ts:409-411`） |
| 成员视图（listMemberMessages） | `announce-only` ⇒ NOT_FOUND——属性派生（announce-only ⇒ 无成员视角投影）；错误语义保持（`session.ts:453-462` 的角色硬编码收敛为 kind 判定） |
| 在途成员判定 | `snapshot().active`（MemberId）经成员 kind 过滤（取代 `isMemberRole`/`isAgentMemberRole`/`activeAgentMember` 角色串辅助，`session.ts:1041-1058`；快照消费/teardown markOutcome 行为不变） |
| teardown | 按统一成员列表释放（agent 成员 collector.dispose + 编排释放；announcer 随统一流退订与列表释放），取代 `entry.announceOff()` 单独调用（`session.ts:718-731`） |

## 3. 行为保持（验收基准）

1. **076 呈现契约回归**：GetTeam 三成员名册（announcer 空 preset/model）、`active_member` 可为 announcer、GetTeamMember 以与 LLM 成员一致的资源名服务 announcer（output-only `system_prompt` 空值）、请求侧校验保持严格（UpdateTeam 恰含 player/planner，announcer 不可由调用方配置）——`specs/076-agent-v2-optimize/contracts/announcer-member.md` §6 单测面全量通过。
2. **投影行为等价**：announcer 播报的归并条目 + `team_message{member="announcer"}` 帧与 `ListTeamMessages` 同源同值；刷新 teardown 后旧生命周期播报不进新 entry 投影（既有测试锚点 `session.test.ts` 刷新用例保持）。
3. **特例代码消失**（代码面检查）：`announcerMemberView`、`GetTeamMember` 的 announcer 提前 return 分支、`announceOff` 字段、`isAgentMemberRole`/`activeAgentMember` 角色串判定在 `projects/game/agent_v2/src/` 中不存在（`isMemberRole` 的资源名合法性校验可保留为 wire 校验，但其用途不得是成员类别分派）。

## 4. 验收面（对应 spec SC-002）

- 宿主单测：076 announcer 契约验收面回归（名册/读取/校验/web 渲染）；刷新 teardown 无帧泄漏；三成员统一持有下的视图派生断言。
- 代码面：§3 item 3 的消失清单 grep 检查为空。
- 大型测试：conversation 套件的 `member="announcer"` 统计消息、三成员名册断言不变通过。
