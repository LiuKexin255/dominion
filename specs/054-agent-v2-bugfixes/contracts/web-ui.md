# Contract: web 前端（web-ui）

**Feature**: [spec.md](spec.md) FR-002/003/004/005/006/007/008/009/010/011/013/014/015/016/017/019/020/021/022/023 | **Research**: [research.md](../research.md) D2/D4/D6/D12 | **Data**: [data-model.md](../data-model.md) §5

049/051 前端契约（工程基座、store reducer 不变量、侧栏交互、物化面板）为基线延续；本文定义本 feature 的前端变更面。

**官方对齐基线**（research D1 L3——行为对齐、非代码移植）：

- `@deepseek-ai/dsh-client-ui-chat@0.1.2-rc.1` README "Turn Process Folding" 章节（https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-chat ，源仓库 https://github.com/deepseek-ai/deepseek-harness ）——分段折叠规则的行为陈述。
- `@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2` README "Markdown rendering" 章节——MarkdownText 能力与流式增量语义。
- `@deepseek-ai/dsh-client-ui-theme@0.1.1-rc.2` README——token sheets 清单与引入顺序（`src/styles/`：base → design-platform → scrollbar → gradient-shadow-text → shiki）。

## 1. 主题与 token（FR-021/022）

| 项 | 契约 |
|---|---|
| 引入 | `@deepseek-ai/dsh-client-ui-theme@0.1.1-rc.2` 仅消费 `src/styles/*.css`（按官方顺序 import；dsh 插件族统一 catalog 管理，直接版本仅限 `third_party/dsh/core` 底座），不引入其 cordis runtime |
| 主题形态 | 深色单一主题：以 token sheets 的 dark 值激活（`body[data-ds-dark-theme]` 或 sheets 实际选择器，引入时实读确认）；不提供主题切换 |
| 自有样式 | `theme.css` 保留 `--app-*` 布局变量与布局规则；手写的 `--dsw-*` 变量子集**删除**（token sheets 为唯一权威，防漂移） |
| 验收 | Menu 卡片呈现背景/边框/阴影（组件测试断言计算样式或 token 变量存在）；既有组件（Button/Input/StateDot/ReasoningRow 等）视觉无缺失 |

## 2. 对话页分段与折叠（FR-004/005/006/007）

### 2.1 store（`store/chat.ts`）

- `LiveTurn.steps: StepDraft[]`（step 分组，见 data-model §5.1）；块事件按 `step` 路由；缺 step 归组 0（退化不崩溃）。
- `turn_end{COMPLETED}` → steps 依序投影为多条 HistoryMessage（**废除**整回合合并单条）。
- `turn_end{ERROR/CANCELED}` → 已呈现 step 保留并入本地历史，未完成尾块 interrupted 呈现；提示独立（error / "已终止"）。
- `turn_end{ABORTED}` → 清空（051 既有，App 层编排）。
- 049 reducer 不变量延续（turn 序、块 index 对齐、delta 拼接、tool_id 关联、未知事件忽略）。

### 2.2 渲染（`ChatView.tsx`）

| 状态 | 呈现 |
|---|---|
| 流式中（turn 打开） | 各 step 分段依次独立呈现，全部展开；步骤内 think→ReasoningRow（折叠块）、toolCall→ToolCard、text→正文气泡（分类分列，官方折叠规则"while a Turn is open … remain expanded"） |
| turn COMPLETED | **折叠**：最终答案 step（最后一个含非空 text 且无 tool-call 的 step）独立呈现；此前 step 折叠进"思考过程"摘要区（规模提示：步骤数/工具调用数；点击展开全过程；手动展开在页面会话内保持） |
| 无最终答案（ERROR/CANCELED/纯工具结束） | 全部过程可见，不折叠（官方规则 "a closed Turn with no final answer keeps all process evidence visible"） |
| 回填 | 默认折叠态（每回合独立折叠）；与流式结束时形态一致 |

用户消息、排队指示（queue-chip）、错误提示呈现不变。

### 2.3 官方差异声明

官方折叠状态含持久化的显示偏好（Normal/Compact 设置）与 generation 记忆——**不引入**（单一默认 Compact 形态+页面会话内手动展开即可，游戏域无设置系统）。

## 3. markdown 渲染（FR-008/009/010/011）

| 呈现面 | 组件 | 契约 |
|---|---|---|
| agent 正文气泡 | `MarkdownText`（替换 `MessageText`） | GFM（标题/列表/粗斜体/行内代码/代码块/表格/链接）；流式增量解析（尾部重解析，不整篇重渲染）；不完整片段不崩溃 |
| 思考折叠块展开体 | `MarkdownText` | 同正文能力 |
| 用户消息 | 不变（纯文本） | — |
| 工具结果棋盘 | ToolCard 内**预格式化等宽**呈现（`<pre>`/等宽样式） | 坐标标尺对齐；不 markdown 化；result 不再以 JSON 字符串字面量形态呈现 |

安全：MarkdownText 自带（不渲染 raw HTML、链接协议白名单）——不额外放宽。

## 4. 终止按钮（FR-015/016/017）

| 项 | 契约 |
|---|---|
| 入口 | composer 区按钮（发送按钮旁）；**仅在 live 回合运行中可见/可用**；空闲不呈现触发面 |
| 行为 | 点击 → `POST {session}/agent:cancel`（contracts/agent-api-changes.md §3）→ 流上等待 `turn_end{CANCELED}`；请求失败呈现错误（不吞） |
| 终态 | CANCELED：已产出分段保留、"已终止"标识（非错误文案）、输入立即可用 |
| 排队 | cancel 后队列清空呈现（queue-chip 移除）；排队消息以历史 user 消息形态出现在对话流（落地） |
| 重复/竞态 | 运行中重复点击防抖；turn 已结束时 no-op 成功 |

## 5. 桌面连接状态指示（FR-002/003）

| 项 | 契约 |
|---|---|
| 位置 | 对话页顶部（ChatPanel 头部区） |
| 状态 | 已连接（success 色+文案）/ 未连接（警示）/ 未知（降级：agent 未物化或查询失败，禁止显示为已连接） |
| 数据 | GetAgent `desktop_connected`；轮询 10s + 进入会话/send 前/turn 结束即时刷新 |
| 语义 | 指示为 agent 侧事实；连接/断开/接管后 ≤10s 反映（SC-005） |

## 6. Preset 独占编辑视图（FR-019/020）

| 项 | 契约 |
|---|---|
| 视图切换 | `FormMode ∈ {closed, create, edit}`：closed=列表视图；create/edit=**独占编辑视图**（列表不渲染） |
| 保存/取消 | 成功→返回列表（反映最新内容与更新时间）；取消→返回列表（不变）；失败→停留编辑视图+错误呈现+内容不丢 |
| 字段 | create：名称可输入（preset_id query）+ player_prompt；edit：名称只读+ player_prompt（051 语义延续） |
| 竞态 | 正在编辑条目被（外部）删除 → 返回列表（051 自动关闭语义延续） |
| 空态 | 无 preset 时列表视图空态引导（不变） |

## 7. SessionList 菜单（FR-021/022/023）

- 组件行为**零改动**（`···` Menu portal 两步确认已正确，见 `projects/game/web/frontend/src/components/SessionList.tsx`）；视觉修复由 §1 token 引入自动达成。
- 验收锚点转为视觉断言（§1）。

## 8. 测试义务（vitest 组件级/store 级）

1. **store**：step 分组（含缺 step 退化）、COMPLETED 多消息投影、ERROR/CANCELED 保留、ABORTED 清空、tool_id 跨 step 关联——reducer 用例全覆盖。
2. **ChatView**：流式分段依次呈现、完成后折叠/展开（含计数）、无最终答案不折叠、回填默认折叠、终止按钮可见性/点击/终态、连接状态三态。
3. **markdown**：GFM 元素渲染、流式增量稳定性、棋盘等宽对齐（ToolCard）。
4. **PresetsView**：视图切换矩阵（进入/保存/取消/失败）、字段语义。
5. **SessionList**：token 引入后 Menu 卡片视觉断言（新增）；既有交互用例零回归。
6. **App/ChatPanel**：cancel 编排（请求+流终态）、连接状态轮询触发面。
7. 049/051 既有用例零回归（API 客户端无路径变更预期；`MessageText→MarkdownText` 仅 agent 正文面）。
