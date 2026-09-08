# Research: 057 agent-v2 web/desktop 界面修复二期

**Feature**: [spec.md](./spec.md) | **Date**: 2026-09-06

本文件记录 plan 阶段的根因复核与实现决策（Decision + Rationale + Alternatives）。引用事实均来自仓库内代码/契约或外部权威来源（宪法原则 I）。

## 1. 根因复核记录（spec Motivation 的实现层确认）

### 1.1 US1 重建后对话不刷新（FR-001）

事实链（全部已读源码确认）：

1. **服务端重建即清空历史**：`projects/game/agent_v2/src/session.ts` 的 `doMaterialize` 对已存在 entry 先 `teardownEntry` + `handle.dispose()`，再以 `const history = new SessionHistory()` 建新 entry——ListAgentMessages 在重建后返回空集合。语义契约：`specs/051-agent-v2-dsh-migration/contracts/agent-api.md` §2.1（"无论配置是否变化都执行清理重建"）。
2. **web 应用成功回调不触达对话状态**：`projects/game/web/frontend/src/App.tsx` 的 `onApplied` 仅 `setAgent` / `setAgentStatus('materialized')` / `setPanelOpen(false)`——不调用 `listHistory`、不调用 `store.loadHistory`。ChatStore 持有的旧 `history` / `queue` / `error` / `canceled` 原样保留。
3. **空闲无通知路径**：Send 流只在回合存在期间打开。空闲 agent 重建时没有任何流会向客户端投递 `turn_end{ABORTED}`（ABORTED 帧只发给在途回合的流）。忙时 agent 的在途流会收到 ABORTED，store 归约为 `EMPTY_STATE`（`projects/game/web/frontend/src/store/chat.ts` 的 `TURN_STATUS_ABORTED` 分支）——即忙时自愈、空闲停滞，与用户报告一致。
4. **既有回填路径与让位守卫**：ChatPanel 挂载 effect 调 `listHistory(session)` 后经 `sentSinceBackfill` 守卫决定是否 `store.loadHistory(messages)`（send 一经开始，回填整体让位——`projects/game/web/frontend/src/App.tsx` 注释与 `specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §4）。`loadHistory` 语义：`{ history, live: null, queue: [], error: null, canceled: false }` 全量重建——正是重建同步需要的终态。

### 1.2 US2 长名悬停不滚动（FR-002）

事实链：

1. **悬停类切换本身工作正常**：`projects/game/web/frontend/src/components/SessionList.tsx` 以 `mouseenter/mouseleave` 切换 `.session-name.scrollable` 类（jsdom 有既有断言，`SessionList.test.tsx`）；`projects/game/web/frontend/src/theme.css` 的 `.session-name.scrollable` 置 `overflow-x: auto` 并移除渐隐遮罩。
2. **滚动不可达的产品性根因**：滚动条被刻意隐藏（`scrollbar-width: none` + `::-webkit-scrollbar { display: none }`）；主流浏览器对"仅横向可滚动容器"默认不把垂直滚轮映射为横向滚动（wheel 默认动作无效果，Firefox auto-dir 实验特性默认关闭）——见 [w3c/csswg-drafts#4380](https://github.com/w3c/csswg-drafts/issues/4380)（Firefox auto-dir 默认关闭的官方说明）与 [Stack Overflow: Propagate wheel event from a scrollable container to the window](https://stackoverflow.com/questions/68677344/propagate-wheel-event-from-a-scrollable-container-to-the-window)（Chrome 下悬停 + 垂直滚轮 = 无动作）。结论：**修复不必依赖浏览器 wheel 行为**，由前端主动驱动滚动（D2）。

### 1.3 US3 desktop 无手动刷新（FR-003）

事实链：`projects/game/desktop/frontend/src/App.svelte` 的 sessions-toolbar 仅含模板标识 span；既有 `handleRefresh()`（listSessions + loading/error 语义）被 onMount、模板切换、055 交付的返回导航三处复用。051 迁移时按退化清单移除了 desktop 列表管理操作（`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §5）；本 feature 恢复"刷新"单项（spec A4 显式修订）。

## 2. 决策

### D1: US1 同步机制 = 提取共享回填函数，应用成功后复用（epoch 守卫复位）

**Decision**: 在 ChatPanel 内将挂载 effect 中的"listHistory → 守卫 → loadHistory/错误分支"提取为 `runBackfill`（含 `sentSinceBackfill.current = false` 的 epoch 复位）；挂载 effect 与 `onApplied` 共同调用。`onApplied` 追加 `void runBackfill()`，其余行为（setAgent/setAgentStatus/setPanelOpen）不变。

**Rationale**: 同步语义与既有"刷新/切换会话 → 全量重建"回填完全同构（`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §4），复用同一函数使两处行为永不分叉；epoch 复位后，紧随应用的发送使回填让位（新回合属于新 agent，不得被空历史覆盖）——沿用既有守卫方向，不引入新竞态语义。首物化路径：应用成功后 agent 已存在，回填返回 200 空集合（不再是 404），`loadHistory([])` 与挂载期 404 分支的 `loadHistory([])` 收敛一致。

**Alternatives considered**:
- *本地直接清空（`store.loadHistory([])`，不发请求）*：少一次请求，但把"重建后必为空"的服务端实现细节硬编码进客户端；若服务端将来演进（如历史持久化），客户端立即失真。拒绝。
- *服务端推送重建事件*：需要新事件通道（Send 流外推送/轮询），违反 spec A2（多标签页范围外、不引入推送），远超修复所需。拒绝。

### D2: US2 驱动机制 = JS 步进 scrollLeft（组件内定时器），不用 CSS 双副本 marquee

**Decision**: 悬停（且存在横向溢出且用户未开启 reduced-motion）时，由 SessionList 内部定时器按固定步长推进 `.session-name` 的 `scrollLeft` 至最大值后保持；移出时清除定时器并复位 `scrollLeft = 0`（复用既有 mouseleave 复位点）。继续沿用现有 `.session-name` / `.session-name.scrollable` 类承载遮罩切换与 overflow，不改变 DOM 结构（不复制文本节点）。

**Rationale**: (a) `scrollLeft` 复位链路已存在（`SessionList.tsx` mouseleave 已做 `scrollLeft = 0`），步进方案是对既有机制的延伸而非替换（宪法原则 II：在既有设计上收敛）；(b) 单一 DOM 文本节点，无复制副本的可访问性/维护负担；(c) jsdom 可经 stub 滚动几何断言（`scrollWidth`/`clientWidth`/`scrollLeft` defineProperty，模式参照上游 [deepseek-harness attachment-rail.client.spec.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-attachment/tests/attachment-rail.client.spec.tsx)）+ `vi.useFakeTimers()` 推进定时器，行为确定性可测；(d) 055 从 ReasoningRow 移除 rAF 的原因是**流式期间逐 chunk 布局读取**的成本——本场景为悬停期间单一元素、短生命周期交互，成本不可比。

**Alternatives considered**:
- *纯 CSS 双副本 marquee（两份文本 + `translateX(-50%)` 循环）*：需要复制文本节点（aria-hidden 克隆）与 DOM 结构改动；循环形态与本 feature"单程到尾保持"的端点行为（D3）不匹配；leave 时动画中断会跳变。拒绝。
- *滚轮驱动映射*：2026-09-06 用户已裁定不采用（spec Clarifications Q1）。不采纳。

### D3: US2 端点行为 = 单程滚动到尾部后保持（hold），移出复位

**Decision**: 悬停启动（延迟后）单向步进至 `scrollLeft` 最大值后停止并保持；移出复位为 0 回到渐隐态；再次悬停重新走一遍。

**Rationale**: 完整名称可读的达成路径最小化——默认态展示头部、单程后展示尾部，两次观察覆盖全名；确定性强、无循环动画的持续干扰（Edge Cases 已约束"不无限干扰条目交互"）；实现与断言最简。

**Alternatives considered**:
- *ping-pong 往返*：可反复阅读但状态机与测试面翻倍，收益边际。拒绝。
- *无限循环 marquee*：持续运动干扰阅读与点击，且需无缝衔接的内容复制。拒绝。

### D4: US2 参数与降级 = 启动延迟 250ms、步长 3px/30ms（≈100px/s）、短名不启动、reduced-motion 跳过

**Decision**:
- 启动延迟 **250ms**：吸收指针掠过（快速移过条目不启动，Edge Case"快速掠过无残留"）。
- 步进 **3px / 30ms**（≈100px/s）：侧栏 260px 列宽下典型溢出约 100~250px，单程 1~2.5s，阅读友好。
- 短名判定：`scrollWidth > clientWidth` 才启动（不溢出不产生任何滚动/定时器动作，FR-002 短名无副作用）。
- `prefers-reduced-motion: reduce`（`window.matchMedia` 匹配）时跳过自动滚动，保持渐隐默认态——web 前端已有该媒体查询先例（`projects/game/web/frontend/src/theme.css` 的既有 `@media (prefers-reduced-motion: reduce)` 规则），本决策把同一无障碍约定扩展到 JS 驱动动画。

**Rationale**: 数值为默认参数，在契约中显式登记（任务实现与测试据此断言，避免"节奏"不可测）；速度/延迟非spec 约束项，plan 落定后即为终态基线。reduced-motion 降级为"维持现状"（不滚动）而非替代动画——尊重系统设置；title 静态阅读途径的补充决策见契约 §1（采纳：`.session-name` 原生 title，作为悬停之外的可达性兜底）。

### D5: US3 按钮契约 = sessions-toolbar 右侧文本按钮，复用 handleRefresh，loading 禁用

**Decision**: 在 `projects/game/desktop/frontend/src/App.svelte` 的 sessions-toolbar（`justify-content: space-between`，右侧空位）添加 `<button class="btn btn-small" data-testid="refresh-sessions" onclick={handleRefresh} disabled={loading}>Refresh</button>`。不新增状态、不改 SessionList.svelte（loading/error 呈现复用既有：loading 态列表区显示 "Loading sessions..."，失败显示错误且不清空——055 交付语义）。

**Rationale**: desktop UI 为英文文本按钮风格（"← Back to Sessions"/"Apply Config"），无图标 primitives 依赖；`handleRefresh` 只写 sessions/loading/error、不触碰导航状态（055 已验证的并发安全），`disabled={loading}` 天然消解重复点击（spec 场景 5 的幂等路径）；按钮位置在工具栏与模板标识分列两端，视觉与既有 `space-between` 布局零冲突。`data-testid` 沿用 web 侧 `refresh-sessions` 同名口径，降低跨端测试认知成本。

**Alternatives considered**:
- *放进 SessionList.svelte 组件头部*：组件当前为纯展示（props 驱动），引入刷新回调需穿透 props 且改变组件职责边界；handleRefresh 在 App 层。拒绝。
- *图标按钮*：desktop 无图标库依赖，为此引入新依赖不值。拒绝。

### D6: 测试口径 = jsdom stub 几何 + fake timers（web）/ 055 App.test 模式（desktop）

**Decision**:
- **web SessionList marquee**（`SessionList.test.tsx` 扩展）：对 `.session-name` 元素 `defineProperty` stub `scrollWidth`/`clientWidth`/`scrollLeft`（上游 attachment-rail 模式）+ `vi.useFakeTimers()`；断言：悬停后推进 250ms 延迟 + N×30ms 步进 → scrollLeft 步进至 max 且到 max 后不再增长（hold）；mouseleave → 定时器清除 + scrollLeft=0；短名（stub 无溢出）悬停无 scrollLeft 变化；`matchMedia` stub 为 reduce 时跳过；既有 scrollable 类切换/遮罩样式断言零回归。
- **web 重建同步**（`App.test.tsx` 扩展，mock `./api/agent.js` / `./api/conversation.js`）：应用成功（updateAgent resolve）后断言触发第二次 listHistory 且对话区清空（旧消息消失）；listHistory 慢返回与紧随 send 的竞态——send 先开始则空历史不覆盖（守卫）；listHistory reject → 既有回填错误呈现、对话不清空；updateAgent reject → 不触发回填（面板错误既有路径）；忙时收敛（ABORTED 流事件与回填任意序落地 → 与空闲路径同一干净终态）；首次物化（未物化→物化）Apply 成功 → 回填 200 空、引导态消退。既有 `AgentSettingsPanel.test.tsx` "App 未物化引导" 用例（经 App 全流程驱动 Apply）的 messages mock 按物化状态区分 404 / 200 空集合——apply 路径回填在物化成功后命中该路由，mock 须如实建模服务端语义（`specs/051-agent-v2-dsh-migration/contracts/agent-api.md` §2.1/§2.3：物化成功后 ListAgentMessages 恒 200 空）。
- **desktop 刷新**（`App.test.ts` 扩展，对齐 055 "App sessions refresh" 块）：点击 refresh-sessions 触发第二次 listSessions；失败保持列表 + 错误呈现；请求在途时按钮 disabled。
- **回归**：`bazel test //projects/game/web/frontend:lib_test //projects/game/desktop/frontend:lib_test` 全绿；布局/滚动可达性与重建同步的真实体验以部署环境人工验证记录闭合（spec A3，对齐 055 A5 口径）。

**Rationale**: 全部沿用仓库既有测试基建（vitest + jsdom + `?raw` CSS 断言 + desktop jsdom mount），无新依赖、无浏览器 E2E。

## 3. 影响面与零改动确认

- **无服务端/proto/契约变更**：US1 为纯 web 客户端消费既有 RPC（UpdateAgent/ListAgentMessages）；US3 为纯 desktop 客户端复用既有 ListSessions。`projects/game/agent_v2`、`projects/game/gateway`、`projects/game/proxy` 零改动。
- **无新依赖**：web/desktop 均复用现有 devDeps（vitest/jsdom；desktop jsdom 已于 055 引入）。
- **不变式保留**：`.session-name.scrollable` 类切换与 mouseleave `scrollLeft = 0` 复位（051 FR-004 既有断言）、ChatStore 归约不变式（`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §4）、desktop handleRefresh 只写列表三态——本 feature 全部在其上扩展，不推翻。
