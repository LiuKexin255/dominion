# Contract: 完成回合折叠规则（web-ui 修订）

> 修订 `specs/054-agent-v2-bugfixes/contracts/web-ui.md` §2.2 的"无最终答案的回合保持全部过程内容可见"条款。最终答案回合折叠、interrupted（失败/终止）回合展开两块语义不变；修订点 = ① 新增第三类"终局收束回合"的折叠形态；② 三分类的适用前置——仅作用于**已收束**回合，live 流式进行中回合整组保持展开（客户端 `open` 标记承载，§3）。消费组件：`projects/game/web/frontend/src/components/ChatView.tsx` `CompletedTurn`（团队视图与成员视角共用）。

## 1. 回合三分类（分类输入 = 回合内 `HistoryMessage[]`，无新增服务端信号；仅适用于已收束回合）

| 类别 | 判定 | 呈现 |
|---|---|---|
| 最终答案回合 | 存在满足 isFinalAnswer 的步（非空 text ∧ 无 toolCall ∧ 非 interrupted） | 锚 = 该步可见，锚前过程折叠（现状，054 §2.2） |
| 终局收束回合 | 无 isFinalAnswer 步 ∧ 回合内全部步均无 `interrupted` 标记 ∧ 步数 > 1 | 锚 = **末步**整步可见（THINK/TEXT/工具卡片一并），末步前过程折叠（新增） |
| 失败/终止回合 | 无 isFinalAnswer 步 ∧ 存在 `interrupted` 标记 | 全部过程内容可见不折叠（现状，054 §2.2 / phase4-failed-turn-folding） |

单步回合（终局步即首步）无过程可收，不渲染折叠控件（锚序 ≤ 0，现状规则覆盖）。

**适用前置**：分组内条目全部无客户端 `open` 标记（分组属于已收束回合）。含 `open` 标记条目的分组（live 流式进行中回合的已固化前缀，含被局中用户消息插队拆分的分段——条目级标记与分组位置无关）MUST 整组按流式语义展开：无折叠控件、每步独立呈现、不进入三分类。`open` 是 store 从 team 流生命周期派生的客户端本地信号（§3 / data-model §3），不是服务端信号。

## 2. 折叠控件与状态（不变量重申）

- 开关文案：`思考过程（{process.length} 步骤 · {toolCount} 次工具调用）`；`toolCount` = 过程步内全部 TOOL_CALL 块数（锚步不计入自身）。
- 默认收起；手动展开在页面会话内保持（展开状态键 `${view}:${组首 index}`，视图前缀隔离）；切换会话重置。
- 流式进行中的回合保持全展开（Turn 打开期间过程行保持展开，054 §2.2 / 官方 "Turn Process Folding"：rows "remain expanded while a Turn is open"）——live 路径由 `open` 标记承载（生命周期见 §3）；回填路径无进行中信号，按消息形态分类（进行中回合的已固化前缀与终局收束回合同形 → 折叠，裁定见 §4）。折叠只发生在回合收束后（`turn_end` 帧 / 回填）。
- `open` 分组的内容按流式语境呈现：工具块 status RUNNING 且无 result 呈现为执行中（不做历史语境的中断推导）；回合收束（标记清除）后恢复历史语境推导（陈旧 RUNNING 无 result → 中断终态）。

## 3. 到达路径一致性与 `open` 标记生命周期

`open` 标记（客户端派生，仅 live 路径；实体与状态图见 data-model §3.1）：

- **标记**：`team_message` 归约时该成员存在打开的 live 回合（turn_start 已见、turn_end 未到）→ 该帧固化的归并序列条目与成员视角条目均落 `open: true`。
- **清除**：`closeLiveTurn` 全路径（`turn_end{COMPLETED}` / `ERROR` / `CANCELED` / 流断开与流尾兜底 `closePendingTurns`）清除该成员全部条目的 `open` 标记——含"全部步已固化、无尾步可投影"的早退路径；`turn_end{ABORTED}` 清空整态；`loadHistory` 重建归并序列时防御性清除 memberHistory 残留标记（live 同步复位后不再有清除事件来源）。
- 投影占位条目（projected 尾步）与用户消息条目（member="user"）恒不携带标记。

到达路径：

- **live**：`turn_start` 后、`turn_end` 前，该成员经 `team_message` 帧逐步固化的条目携带 `open`（分组保持展开，此为团队流"回合内逐步落定"固化的固有形态，059 team-api §3.2）；`turn_end{COMPLETED}` → `closeLiveTurn(…, false)`（无 interrupted 投影）+ 清除标记 → 分组即时进入三分类（终局收束回合收束即折叠）；`turn_end{ERROR|CANCELED}` → 尾步 `interrupted: true` + 清除标记 → 失败/终止分类（`projects/game/web/frontend/src/store/chat.ts` §turn_end 归约）。
- **回填**：List 重建条目无 `open` 标记（服务端 HistoryMessage 无回合状态字段，research D2 裁定不引入），分组直接进入三分类。
- **收敛边界**：**已收束回合**两路径 MUST 收敛到同一呈现（分类是消息形态的纯函数）；**进行中回合**两路径合法分歧——live 展开、回填折叠——`open` 是客户端 live 状态的派生而非服务端信号，回填侧无信号可用（裁定与代价见 §4）。

## 4. 边界

- 回合边界分组不变：连续同成员 AGENT 条目为一回合，USER 或另一成员条目断开。
- 终局收束回合后紧跟的排队消化回合是独立回合，独立分类。
- 已知极端形态：LLM 失败恰落在步间（无 interrupted 步）按终局收束折叠——末步仍可见，损失限于过程需点击展开（spec Edge Cases 裁定可接受）。
- **回填进行中回合（局中刷新）**：List 返回进行中回合的部分步骤，与"终局收束后、下一成员条目到达前"的已收束回合 genuinely 同形（序列尾部、无最终答案、无 interrupted、步数 > 1）且无服务端信号可区分——裁定：**接受折叠**（spec Edge Cases"回填侧终局判定信号：折叠判定以消息形态推导"的自然延伸；末步锚可见、过程一键展开，信息损失有界）。驳回"会话尾部分组不折叠"特判：终局后刷新回填必须折叠（FR-006 回填路径折叠一致性），该特判误伤此合法折叠瞬间；驳回时间窗启发式：非确定性行为，不可测。
- **局中刷新后重连**（下次 Send 建流）：在途回合错过的 `turn_start` 不重放（059 team-api §3.4 中途建流只接收订阅点之后的帧），其剩余步骤仅以 `team_message` 帧到达且无 live 回合可标记——延续回填视角裁定（保持折叠）至该回合收束；后续回合 `turn_start` 到达后恢复 live 展开语义。
- **迟到帧窄边界**：成员已收合回合的 `team_message` 帧晚于该成员下一回合 `turn_start` 处理（并发流乱序）且以新条目插入时会被误标 `open`——该条目临时按展开呈现，随下一回合收束清除标记后恢复折叠；差异仅限临时展开，可接受。
