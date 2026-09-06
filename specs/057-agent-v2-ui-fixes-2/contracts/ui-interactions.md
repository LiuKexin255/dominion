# Contract: web/desktop 交互契约（057）

**Feature**: [spec.md](../spec.md) | **Date**: 2026-09-06

**范围**: web 重建同步（`projects/game/web/frontend/src/App.tsx`）、web session 列表长名悬停自动滚动（`projects/game/web/frontend/src/components/SessionList.tsx` + `src/theme.css`）、desktop sessions 页手动刷新（`projects/game/desktop/frontend/src/App.svelte`）。决策依据见 [research.md](../research.md)，状态模型见 [data-model.md](../data-model.md)。本 feature 无服务接口变更（proto/RPC 零改动）。

## §1 web 长名悬停自动滚动（FR-002）

SessionList 超宽名称条目的悬停揭示契约（交互模型 = 悬停自动滚动，2026-09-06 spec Clarifications Q1 裁定）：

1. **触发**：指针进入条目（既有 `mouseenter`，同一切换 `.session-name.scrollable` 类的入口）且同时满足：名称容器存在横向溢出（`scrollWidth > clientWidth`）、用户未启用 reduced-motion（`window.matchMedia('(prefers-reduced-motion: reduce)').matches` 为 false）。任一不满足则悬停无任何滚动动作（短名/降级场景零副作用）。
2. **节奏（终态基线，research D4）**：进入后延迟 **250ms** 启动（吸收指针掠过），随后每 **30ms** 步进 `scrollLeft += 3px`（≈100px/s），单程至 `scrollLeft` 最大值后**停止保持**（hold，research D3）；指针不离则维持尾部呈现。
3. **复位**：指针移出条目（既有 `mouseleave`）清除定时器并将 `scrollLeft = 0`，条目回到渐隐默认态（`.session-name` 遮罩恢复）。再次悬停重新完整执行。
4. **滚动约束**：滚动仅发生在名称容器内（`.session-name.scrollable` 的 `overflow-x: auto` 既有承载，滚动条保持隐藏）；列表垂直滚动与页面滚动不受劫持；自动滚动不得干扰条目既有交互（选择、`···` 菜单）。
5. **零回归不变式**：未悬停渐隐遮罩、类切换、mouseleave 复位（051 FR-004 既有断言）全部保持；条目仍为可聚焦按钮。
6. **实现边界**：驱动为组件内定时器步进 `scrollLeft`（research D2——不复制 DOM 文本、不依赖浏览器 wheel 行为）；定时器生命周期与悬停互斥（同一时刻至多一个条目在滚动，指针进入新条目先清理旧定时器）。
7. **测试口径**：jsdom stub 滚动几何（`scrollWidth`/`clientWidth`/`scrollLeft` defineProperty，模式参照 [deepseek-harness attachment-rail.client.spec.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-attachment/tests/attachment-rail.client.spec.tsx)）+ fake timers 推进；真实浏览器滚动可达性以部署环境人工验证记录闭合（spec A3）。
8. **可达性补充（决策）**：`.session-name` span 补充原生 `title` 属性（完整名称）作为悬停之外的静态阅读途径（spec Edge Case"悬停揭示是增强而非唯一阅读途径"；沿用本组件 IconButton 既有原生 title 模式）；不影响类切换/滚动/遮罩断言。

## §2 web 重建同步（FR-001）

ChatPanel 的应用成功 → 对话视图同步契约：

1. **触发**：agent 设置面板 Apply 成功（`updateAgent` resolve，`onApplied` 回调）。Apply 失败不触发（面板错误既有路径）。
2. **同步动作**：`onApplied` 在既有语义（`setAgent(materialized)` / `setAgentStatus('materialized')` / `setPanelOpen(false)`，零回归）之外，调用与挂载回填同一的回填函数（research D1）：epoch 复位（`sentSinceBackfill = false`）→ `listHistory(session)` → 响应落地且守卫仍 false 时 `store.loadHistory(messages)`；404 分支按既有未物化语义处理（应用成功后实际不发生——agent 已存在）。
3. **让位守卫（方向与既有回填一致）**：回填在途期间一经 send 开始，本次回填整体让位——重建后立即发送的新回合不被空历史覆盖。
4. **失败语义**：回填请求失败 → 既有 `backfillError` 呈现（提示可见、可重试），对话状态不清空、不静默。
5. **收敛**：空闲/忙时（ABORTED 流事件与回填任意序落地）/首次物化三路径收敛到同一干净终态（`loadHistory` 语义：history 重建、`live/queue/error/canceled` 复位）；收敛矩阵见 [data-model.md](../data-model.md) §1.3。
6. **边界（不变）**：多标签页/外部 API 触发的重建不在通知范围（spec A2）；`ChatStore` 归约不变式（`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §4）零改动。
7. **测试口径**：App 组件级（mock api 层）：应用成功触发第二次 `listHistory` 且对话区清空；慢回填 + 紧随 send 的让位；回填失败呈现；updateAgent reject 不触发回填；忙时收敛（ABORTED 流事件与回填任意序落地 → 同一干净终态）；首次物化（未物化→物化）回填 200 空、引导态消退。

## §3 desktop 手动刷新（FR-003）

sessions 列表页的显式刷新入口契约：

1. **入口**：sessions-toolbar 右侧文本按钮 `Refresh`（`class="btn btn-small"`，`data-testid="refresh-sessions"`，`disabled={loading}`——loading 期禁用即幂等，spec 场景 5 的选定路径）。
2. **行为**：点击调用既有 `handleRefresh()`（重新拉取当前模板会话列表）；状态写集仅 `{sessions, loading, error}`——不触碰导航/选择状态（055 FR-007 并发语义延续：刷新进行中进入会话不受干扰）。
3. **呈现复用**：loading/失败呈现与既有时机（启动、返回导航、模板切换）完全一致——loading 期列表区 "Loading sessions..."，失败错误呈现且不清空既有列表。
4. **只读边界（051 契约修订登记）**：本入口是对 `specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §5 退化清单中"移除 SessionList 管理操作（新建/删除/刷新）"的**显式修订**——仅恢复"刷新"单项（读操作）；新建/删除仍为移除态，`specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §5 只读控制面语义（无 session 管理操作）不变。
5. **测试口径**：App 组件级（055 "App sessions refresh" 同套）：点击触发第二次 `listSessions`；失败保持列表 + 错误；在途请求按钮 disabled；既有三时机零回归。

## §4 零改动面（登记）

- `projects/game/agent_v2`（服务端重建语义已满足需求，见 [research.md](../research.md) §1.1）、`projects/game/gateway`、`projects/game/proxy`、proto：零改动。
- web `ChatView.tsx` / `ReasoningRow.tsx` / `store/chat.ts`（归约器）、desktop `SessionList.svelte`：零改动。
- 无新增依赖（web/desktop 复用既有 vitest/jsdom 基建）。
