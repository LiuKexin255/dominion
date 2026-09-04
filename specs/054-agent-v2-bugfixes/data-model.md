# Data Model: agent-v2 对话呈现与游戏链路缺陷修复 + testplan 重构

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Research**: [research.md](research.md)

本文只定义**变更面**；未提及的 051 数据模型（preset 资源、agent 单例、消息子资源、游戏状态/历史、桥接帧）全部延续（基线：`specs/051-agent-v2-dsh-migration/data-model.md`）。

## 1. 协议变更（`projects/game/agent_v2.proto`）

### 1.1 ChatEvent 块事件扩展 step（FR-004）

`BlockStartEvent`（:393）、`BlockDeltaEvent`（:404）、`BlockEndEvent`（:409）各新增：

```proto
int32 step = <next>;  // 块所属的模型输出步骤序号（turn 内从 0 单调递增）
```

- 语义：turn 内 step 序号与服务端 step 循环一致；同 step 的块共享序号；step 变化即新分段。
- 兼容性：proto3 可选字段——旧客户端忽略未知字段（051 既有 forward-compat 方向不变）；服务端在事件 `data.step` 缺失时置 0。
- `ToolResultEvent`（:417）不扩展（结果按 tool_id 关联既有语义不变，无需 step）。
- turn-global `index` 字段保留不变（049 reducer 不变量延续，step 为分组维度、index 为块序维度）。

### 1.2 TurnStatus 新增 CANCELED（FR-015/016）

```proto
enum TurnStatus {
  TURN_STATUS_UNSPECIFIED = 0;
  TURN_STATUS_COMPLETED = 1;
  TURN_STATUS_ERROR = 2;
  TURN_STATUS_ABORTED = 3;
  TURN_STATUS_CANCELED = 4;  // 用户经 :cancel 终止（新增）
}
```

| 终态 | 语义 | 前端处置（store 归约） |
|---|---|---|
| COMPLETED | 回合正常完成 | 按 step 分段并入本地历史；终态折叠（最终答案独立） |
| ERROR | 回合失败 | **保留**已呈现分段（尾块 interrupted 呈现）；错误提示独立 |
| CANCELED | 用户终止（新增） | 同 ERROR 的保留语义；终态标识"已终止" |
| ABORTED | 会话销毁/重物化（既有，语义收窄） | 清空（051 既有处置不变） |

未知枚举值：消费端 forward-compat 忽略/视作 ERROR 呈现（既有方向）。

### 1.3 Cancel 方法（FR-015/016/017）

```proto
rpc Cancel(CancelRequest) returns (CancelResponse) // POST /api/v2/{session}/agent:cancel
```

- `CancelRequest`：仅 `name` 路径参数（agent 资源名），无额外字段。
- `CancelResponse`：空（或后续按需扩展）；幂等——无在途回合且无排队消息时成功 no-op。
- 服务端行为（时序）：终止在途回合（cancel 传播）→ 清空待处理队列（落地语义，§3）→ 在途流收到 `turn_end{CANCELED}` → 新 Send 立即可用。

### 1.4 Agent 消息扩展连接状态（FR-002）

`Agent` 消息（GetAgent 响应）新增：

```proto
bool desktop_connected = <next>;  // 该 session 的桌面桥接连接事实（agent 侧注册表）
```

- 事实来源：`@dominion/dsh-desktop-bridge` 连接注册表（新查询面 `isDesktopConnected(sessionName)`）。
- 刷新时效：前端轮询 10s + 关键时刻（进入会话/send 前/turn 结束）；接管/断开在下一个轮询沿反映（SC-005）。
- agent 未物化（GetAgent 404）：前端降级"未知"（不显示已连接）。

### 1.5 HistoryMessage 扩展 interrupted（FR-005）

`HistoryMessage`（:507）新增：

```proto
bool interrupted = 5;  // 该 assistant step 为中断前缀（流失败/终止前已产出）
```

- 语义：true 表示该消息内容是 §2 的中断固化前缀（`interrupted: true` append），非终态答案；仅 agent 消息可为 true（user 消息恒缺省）。消费面：web 折叠判定排除该消息（FR-005 无最终答案 → 全可见不折叠，前端无法从内容形态区分完整正文与中断部分正文——信号经本字段传递）。
- 传递：driver `assistant/message` 事件 data 的 `interrupted: true` → `SessionHistory.appendAssistant` 记录 → List 响应透出；本地路径 store 投影对尾步消息同构标记（§5.2）。
- 兼容性：proto3 默认 false 缺省（protojson 仅 true 时输出）；字段扩展经 gateway/proxy 既有透传自动生效（同 §1.4 `desktop_connected`）。设计裁定见 [revisions/phase4-failed-turn-folding.md](revisions/phase4-failed-turn-folding.md)。

## 2. 历史固化语义变更（FR-012，saolei-loop driver）

| 回合结局 | 现状 | 变更后 |
|---|---|---|
| step 正常完成 | append `assistant/message`（进历史） | 不变 |
| abort（既有） | append interrupted 前缀 | 不变 |
| **ERROR（LLM 流失败/finish error/异常冒泡）** | **不 append（整 step 丢弃）** | **有部分内容时 append `interrupted: true`**（对齐 abort 路径与官方 interrupted 语义） |

- 固化粒度：已产出的块（assembler 的部分内容）；空 assembler 不 append（无内容可固化）。
- `SessionHistory.appendAssistant` 记录事件 data 的 `interrupted`（§1.5）；List 透出。收集时机不变（session-lifetime 既有）。
- 回填后失败/终止回合的呈现：无最终答案 → 全部过程可见不折叠（FR-005）——判定基准为内容形态 + `HistoryMessage.interrupted`（§1.5）：内容形态无法区分"完整正文（COMPLETED）"与"中断部分正文（ERROR/CANCELED）"，前端最终答案判定排除 interrupted 消息。
- 无 result 的工具块按中断终态呈现（Edge Cases 既有裁定，回填侧 ToolCard 状态映射补 INTERRUPTED 呈现——toolCall status 仍为 RUNNING 的陈旧块由前端在回填时按消息终态推导）。此处的"不改 proto"指**块级** ToolStatus 枚举与块状态不加中断值；消息级 interrupted 信号是 §1.5 的独立字段扩展，二者边界如此。
- 两形态边界（已裁定）：中断固化只保留 text/think 安全前缀（assembler `interruptedBlocks()` 丢弃未派发的 tool-call，不虚构其参数与结果），mid-tool-call 流死亡的尾步本地保留 RUNNING tool-call draft（呈现"已中断"卡片，FR-013 不原地清空）、回填无该块（刷新后卡片消失）；失败发生在下一 step 起步时（尾步为已完成的纯正文 step），回填按内容判定折叠而本地不折叠。正文/思考前缀在上述边界外两路径一致，折叠分歧仅影响摘要形态、内容零丢失。

## 3. 排队消息落地语义（FR-017，用户裁定）

| 时机 | 现状 | 变更后（:cancel 时） |
|---|---|---|
| enqueue | `appendUser` 进历史 + 入队 | 不变 |
| 队列消费 | 依序触发回合 | **cancel 后队列清空，不再触发**（历史 user 消息保留——落地事实已在 enqueue 时成立） |
| 流事件 | queued 事件（position） | 每个排队流收到 `turn_end{CANCELED}` 终帧并关闭（既有 turn_end 事件面，无新事件类型——cancel 原子清空队列无可重报 position，终帧使消费端确定性移除排队指示） |

无新实体；行为变更仅"清空待处理队列"。

## 4. 模型目录（FR-018，配置数据）

`projects/game/agent_v2/cordis.yml` llm-glm `models`（目录唯一来源）：

| id | contextWindow | 说明 |
|---|---|---|
| `glm-5.3` | 1000000 | 旗舰（新默认；`DEFAULT_MODEL = GLM_MODEL \|\| 'glm-5.3'`） |
| `glm-5.3-flash` | 实现期核实（D9 开放项） | 高速变体 |

- 移除 `glm-5.2` 条目（历史别名，官方端点自动切换——目录仅呈现实际有效模型）。
- ListModels/物化校验/默认模型全部同源（051 FR-006 机制零改动）。

## 5. 前端状态模型（store/chat.ts 变更）

### 5.1 LiveTurn 结构

```text
LiveTurn {
  steps: StepDraft[]          // 新：按 step 分段的草稿序列（替代单 blocks 平铺）
  …
}
StepDraft {
  step: number
  blocks: BlockDraft[]        // 既有 BlockDraft 结构不变（+step 归组）
  settled: boolean            // 下一 step 的块事件到达即置 true（分段边界）
}
```

- 归约：`blockStart/delta/blockEnd` 按 `event.step` 路由到对应 StepDraft（缺失则新建）；`tool_result` 跨 step 按 tool_id 全局匹配（既有语义）；`turn_end{COMPLETED}` 将 steps 依序投影为多条 HistoryMessage（对齐服务端每 step 一条），不再合并单条。
- 兼容：事件缺 step 字段（旧服务端/残留流）→ 归组 step=0（行为退化为现状，不崩溃）。

### 5.2 回合终态处置

| turn_end | steps 投影 | 终态 UI |
|---|---|---|
| COMPLETED | 全部 step 入历史 | 折叠：最终答案 step（最后一个含非空 text 块且无 tool-call 块且非 `interrupted` 的 step——§1.5）独立，此前 step 折叠进"思考过程"区（计数=step 数/工具调用数；手动展开页面会话内保持） |
| ERROR | 已呈现 step 入历史；尾步消息标记 `interrupted: true`（§1.5，未完成尾块原样投影） | 错误提示独立，不折叠（无最终答案） |
| CANCELED | 同 ERROR | "已终止"标识，不折叠 |
| ABORTED | 清空（不变） | —（App 层会话删除编排） |

### 5.3 连接状态（ChatPanel 级）

```text
DesktopConn = 'connected' | 'disconnected' | 'unknown'
```

来源：GetAgent 轮询（10s + 关键时刻）；404/请求失败 → unknown。

## 6. testplan 数据变更（详见 [contracts/testplan.md](contracts/testplan.md)）

- deploy：两拓扑保持——`deploy_agent_v2.yaml`（won）与 `deploy_agent_v2_drop.yaml`（drop）均为单 `fake-desktop` 实例（服务名不改，两 deploy 服务名空间独立），行为差异全由 env 驱动（`FAKE_DESKTOP_SESSION`/`FAKE_DESKTOP_SCENARIO`/`FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS`）。
- suite：`system_test.yaml` 7 suite → 2 suite——主 suite `game-system`（主 deploy，cases 顺序：session → memory → web → conversation → preset → game → desktop-flow）+ 断连 suite `game-disconnect`（drop deploy，disconnect case 置于主 suite 之后）。
- binary：8 个 target 全部保持——`agent_v2_game_disconnect_test` 维持独立文件与 target（guitar 以整个 bazel target 为 case 粒度、无测试函数筛选，断连用例绑定 drop 拓扑；`tools/test/guitar/pkg/run/run.go`）。
