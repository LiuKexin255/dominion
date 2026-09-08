# Data Model: 057 agent-v2 web/desktop 界面修复二期

**Feature**: [spec.md](./spec.md) | **Date**: 2026-09-06

本 feature 无持久化数据/传输形状变更（研究 §3）；本文件描述三处**行为状态模型**：web 重建同步的回填 epoch、web 长名揭示状态机、desktop 手动刷新的状态复用。状态字段均为既有组件状态（`projects/game/web/frontend/src/App.tsx` / `src/store/chat.ts` / `projects/game/desktop/frontend/src/App.svelte`），此处只登记本 feature 触及的语义。

## 1. web 重建同步（Rebuild Sync）

### 1.1 涉及状态（既有，形状不变）

| 状态 | 归属 | 语义 | 本 feature 触及点 |
|------|------|------|-------------------|
| `history` / `live` / `queue` / `error` / `canceled` | ChatStore（`src/store/chat.ts` ChatState） | 会话对话状态；`loadHistory` 全量重建为 `{history, live: null, queue: [], error: null, canceled: false}` | 应用成功后经回填重建（FR-001） |
| `backfillError` | ChatPanel | 回填失败提示（不清对话状态） | 应用触发的回填失败复用（场景 6） |
| `agentStatus` / `agent` | ChatPanel | 物化状态与配置 | 既有 onApplied 语义保持（场景 2） |
| `sentSinceBackfill`（ref） | ChatPanel | 回填让位守卫：自本次回填发起后是否有 send 开始 | 提取 `runBackfill` 时统一复位（§1.2） |

### 1.2 回填 epoch 与 runBackfill

回填以"发起即开新 epoch"运作：每次 `runBackfill` 调用先 `sentSinceBackfill.current = false`，随后 `listHistory(session)`；响应落地时仅当守卫仍为 false 才 `store.loadHistory(messages)`（send 一经开始整体让位——`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §4 既有方向）。

调用点（两处，同一函数）：

1. ChatPanel 挂载 effect（既有路径，行为不变：probe + 连接刷新 + 回填）。
2. `onApplied`（本 feature 新增）：应用成功（updateAgent resolve）后，在既有 setAgent/setAgentStatus/setPanelOpen 之外追加调用。

### 1.3 重建同步收敛表（FR-001 验收的状态矩阵）

| 路径 | 时序 | 终态 | 依据 |
|------|------|------|------|
| 空闲重建 | apply ok → 回填 200 空 → loadHistory([]) | 干净对话面（history 空、queue/live/error/canceled 复位） | 场景 1/7 |
| 忙时重建 | apply ok；在途流收 `turn_end{ABORTED}` → store EMPTY_STATE；回填 200 空 → loadHistory([])（两事件任意序） | 同上（收敛一致，无中间残留） | 场景 4 |
| 首次物化 | apply ok → 回填 200 空（agent 已存在，非 404）→ loadHistory([]) | 空对话面 + 引导态消退 | 场景 5 |
| 重建后即发 | apply ok → 回填在途 → send 开始（守卫 true）→ 回填落地让位 | 新回合完整呈现，不被空历史覆盖 | 场景 3 / Edge |
| 回填失败 | apply ok → listHistory reject → setBackfillError | 对话保持 + 错误提示（非静默误导） | 场景 6 |
| 应用失败 | updateAgent reject → onApplied 不触发 → 无回填 | 面板错误（既有路径） | — |

边界（不在同步范围）：多标签页/外部 API 触发的重建无推送通知，其他客户端经自身回填时机收敛（spec A2，`specs/049-agent-v2-dsh-init/research.md` 已知限制延续）。

## 2. web 长名揭示（Long-Name Reveal）

### 2.1 状态机（SessionList 内，per 条目；悬停互斥——指针同一时刻仅在一个条目）

```text
idle ──mouseenter(有溢出 && !reduced-motion)──▶ armed(250ms 延迟计时)
idle ──mouseenter(无溢出 或 reduced-motion)──▶ idle（无任何动作）
armed ──延迟到──▶ scrolling（每 30ms scrollLeft += 3px）
scrolling ──scrollLeft 达 max──▶ held（定时器清除，保持尾部）
任意态 ──mouseleave──▶ idle（定时器清除，scrollLeft = 0，遮罩恢复）
armed/scrolling ──条目卸载/列表刷新──▶ （自然消亡，无残留）
```

- 溢出判定：`scrollWidth > clientWidth`（元件 `.session-name`）。
- `reduced-motion`：`window.matchMedia('(prefers-reduced-motion: reduce)').matches` 为 true 时不进入 armed（维持渐隐默认态）。
- 参数为契约登记的终态基线（research D4）：延迟 250ms、步长 3px/30ms（≈100px/s）、单程到尾 hold（D3）。
- 呈现承载不变：hover 态仍切换 `.session-name.scrollable`（`projects/game/web/frontend/src/theme.css`，overflow-x:auto + 遮罩移除 + 滚动条隐藏）——滚动约束（不劫持列表/页面滚动）由该容器既有 overflow 语义保证。

## 3. desktop 手动刷新（Manual Refresh）

无新增状态。按钮点击调用既有 `handleRefresh()`（`projects/game/desktop/frontend/src/App.svelte`），其状态写集为 `{sessions, loading, error}`：

| 状态 | 刷新成功 | 刷新失败 | 既有呈现 |
|------|----------|----------|----------|
| `loading` | true → false | true → false | loading 期列表区显示 "Loading sessions..."，按钮 `disabled={loading}` |
| `sessions` | 替换为最新集合 | 不变（不清空） | — |
| `error` | null | 错误字符串 | 错误文案呈现（不清列表） |

只读边界：刷新是读操作；不引入新建/删除（spec A4 对 `specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §5 退化清单的显式修订，登记见 [contracts/ui-interactions.md](./contracts/ui-interactions.md) §3）。

## 4. 实体关系（Key Entities ↔ 状态模型）

- **重建同步（Rebuild Sync）** ↔ §1（epoch 守卫 + 收敛矩阵）。
- **长名揭示（Long-Name Reveal）** ↔ §2（状态机 + 参数基线）。
- **手动刷新（Manual Refresh）** ↔ §3（状态写集复用）。
