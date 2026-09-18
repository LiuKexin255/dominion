# Quickstart: team 排队消息 step 边界进入与 turn 语义表述修正

> 端到端验证指南。大型测试经 testplan skill 执行（constitution 原则 VI：实际 `guitar run` 部署→测试→清理闭环，全量通过为验收；构建通过不构成验收）。

## 1. 验证场景

### V1 — mid-turn 进入（SC-001，核心）

**部署**：`projects/game/testplan/` 既有 agent-v2 team 拓扑（fake-llm + fake-desktop）。

**步骤**：

1. 物化 team（player/planner preset）；
2. 发送开局消息驱动 player 进入多步工具回合（fake-llm 模板产出 tool call）；
3. 在工具执行期间（step N 与 N+1 之间）发送带探针关键词（如 `steer-probe`）的消息；
4. 断言流帧序与投影：
   - 该消息的 `member_view{sender:"user"}` 帧到达（消费时点）；
   - fake-llm 在下一 step 的响应命中 steer 关键词模板（该 step 的最后一条 user 消息即 steered 消息）——输出正文含对探针的响应文本；
   - 成员视角（ListMemberMessages 回填）中该输入位于 step N 与 step N+1 输出之间；
   - 归并序列中该消息位置 = 发送时刻位置（enqueue 即固化回归）；
   - 前端 store：chip 随 member_view 帧消除（不等 turn_end）。

### V2 — 回退路径与切换节点（SC-002）

**步骤**：

1. 驱动 player 回合，在**最后一个** step 执行中发送消息（此后 fake-llm 模板收尾纯文本，无后续 step）；
2. 断言：
   - turn 结束后**同一成员**（player）出现新回合消费该消息（steer-wake 自愈，SC-002a）；
   - 新回合结束后才发生切换/续驱（消化优先于切换）；
   - 该消息不触发第二次消化。

**变体 V2b（静止排队回归）**：team 静止时发送 → 直接驱动当前激活成员（现状路径零回归，既有用例覆盖）。

### V3 — 取消语义（SC-002/SC-003）

**步骤**：多步回合中先后发送两条消息（其一已被 step 消费、另一仍在 inbox/队列）→ 执行 Cancel → 再次 Send。

**断言**：在途回合以 `turn_end{CANCELED}` 终止；已消费消息作为普通用户消息保留于成员视角与归并序列；未消费消息保留为已固化历史且不触发新驱动；chips 全清；**再次 Send 后新回合的 LLM 请求历史含落地消息与新消息（fake-llm 响应可同时引用两者；落地消息未单独触发回合——Cancel 与 Send 之间无成员回合发生）**。

### V4 — 既有行为全量回归（SC-005）

既有 team 大型测试全量通过（多局闭环、排队消化、取消、刷新重建、回填/断开收敛/多流去重）。

### V5 — 文档/注释修正（SC-004）

文本检索（`rg`）：`specs/059-agent-v2-team-mode/` 与 `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 中"工具调用引发的后续 turn"零残留；`rg 'team turn' specs/059-agent-v2-team-mode/` 零命中（或每条命中同行含"非 dsh turn"澄清语）；`specs/049-agent-v2-dsh-init/`（FR-012 + research）与 `specs/059-agent-v2-team-mode/spec.md` FR-011 含 supersession 注记且原文未被重写。

### V6 — 终局收束 × 在途 steer（062 交互，FR-001/FR-003 补充断言）

**前置**：`specs/062-team-game-end-handoff/` 已先行落地（终局链脚本步骤成为"零执行"断言面——本场景的延展步是唯一例外：它响应 steered 消息）。

**步骤**：player 终局 step 的工具执行期间发送 steer 探针消息。

**断言**：turn **延展同 turn 一步**——steered 消息与终局工具结果同批进入（steer 探针模板响应，**纯文本收尾**保证延展 turn 收束）；延展步之后无更多模型输出；事件序：player 延展 turn 的 `turn_end` 先于 planner 复盘 `turn_start`（输入消费先于复盘交接）；复盘输入含终局工具单元（062 SC-003 回归）。

## 2. 手工冒烟（可选补充）

生产拓扑（真实 GLM）：物化 team → 开局 → player 操作期间发送纠正消息 → 观察 web 对话页：chip 即时消除、成员视角出现该输入、player 下一次操作体现纠正（模型行为，非断言面）。

## 3. 单测/组件测（每次代码变更随跑，bazel test）

- `common/js/dsh-plugins/saolei-loop`：submit 在途 steer / 静止 FIFO / steeredPending 计数闭环（user/message 事件）/ cancel 清 inbox / 静止判定（含 inbox 兜底）。
- `projects/game/agent_v2`：session.send 的 queued position（两路径）/ watchQuiescence 扩展。
- `projects/game/web/frontend`：chip 的 member_view 消除（含重复文本）/ turn_start 不再出队 / 终态全清回归。
