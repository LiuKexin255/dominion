# Contract: 完成回合折叠规则（web-ui 修订）

> 修订 `specs/054-agent-v2-bugfixes/contracts/web-ui.md` §2.2 的"无最终答案的回合保持全部过程内容可见"条款。最终答案回合折叠、interrupted（失败/终止）回合展开两块语义不变；修订点 = 新增第三类"终局收束回合"的折叠形态。消费组件：`projects/game/web/frontend/src/components/ChatView.tsx` `CompletedTurn`（团队视图与成员视角共用）。

## 1. 回合三分类（派生输入 = 回合内 `HistoryMessage[]`，无新增服务端信号）

| 类别 | 判定 | 呈现 |
|---|---|---|
| 最终答案回合 | 存在满足 isFinalAnswer 的步（非空 text ∧ 无 toolCall ∧ 非 interrupted） | 锚 = 该步可见，锚前过程折叠（现状，054 §2.2） |
| 终局收束回合 | 无 isFinalAnswer 步 ∧ 回合内全部步均无 `interrupted` 标记 ∧ 步数 > 1 | 锚 = **末步**整步可见（THINK/TEXT/工具卡片一并），末步前过程折叠（**新增**） |
| 失败/终止回合 | 无 isFinalAnswer 步 ∧ 存在 `interrupted` 标记 | 全部过程内容可见不折叠（现状，054 §2.2 / phase4-failed-turn-folding） |

单步回合（终局步即首步）无过程可收，不渲染折叠控件（锚序 ≤ 0，现状规则覆盖）。

## 2. 折叠控件与状态（不变量重申）

- 开关文案：`思考过程（{process.length} 步骤 · {toolCount} 次工具调用）`；`toolCount` = 过程步内全部 TOOL_CALL 块数（锚步不计入自身）。
- 默认收起；手动展开在页面会话内保持（展开状态键 `${view}:${组首 index}`，视图前缀隔离）；切换会话重置。
- 流式进行中的回合保持全展开（Turn 打开期间过程行保持展开，054 §2.2）——折叠只发生在回合收束后（`turn_end` 帧 / 回填）。

## 3. 到达路径一致性

- **live**：`turn_end{COMPLETED}` → store `closeLiveTurn(…, false)`（无 interrupted 投影）→ 归并序列 → 终局收束回合分类；`turn_end{ERROR|CANCELED}` → 尾步 `interrupted: true` → 失败/终止分类（`projects/game/web/frontend/src/store/chat.ts` §turn_end 归约）。
- **回填**：List 的 `HistoryMessage.interrupted` 同型标记 → 同一分类函数。
- 两路径 MUST 收敛到同一呈现（分类是消息形态的纯函数）。

## 4. 边界

- 回合边界分组不变：连续同成员 AGENT 条目为一回合，USER 或另一成员条目断开。
- 终局收束回合后紧跟的排队消化回合是独立回合，独立分类。
- 已知极端形态：LLM 失败恰落在步间（无 interrupted 步）按终局收束折叠——末步仍可见，损失限于过程需点击展开（spec Edge Cases 裁定可接受）。
