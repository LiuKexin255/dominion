# Quickstart: 057 agent-v2 web/desktop 界面修复二期 验证指南

**Feature**: [spec.md](./spec.md) | **Date**: 2026-09-06

本指南验证三项修复端到端生效：web 重建同步（FR-001）、web 长名悬停自动滚动（FR-002）、desktop 手动刷新（FR-003）。交互契约见 [contracts/ui-interactions.md](./contracts/ui-interactions.md)，状态收敛见 [data-model.md](./data-model.md)。

## 1. 前置条件

- 仓库根目录执行；bazel 可用（`AGENTS.md` 操作命令参考）。
- 人工场景需要部署环境（web + agent_v2 + desktop 可达；参照 `projects/game/testplan/` 既有部署或 054 testplan suites 环境）。

## 2. 自动化验证（组件级，每次代码变更必跑）

```bash
# 编译 + 全量单测（宪法原则 IV）
bazel build //...
bazel test //projects/game/web/frontend:lib_test //projects/game/desktop/frontend:lib_test
```

预期：全部通过。覆盖面（对照契约测试口径）：

- §1 长名悬停：悬停 250ms 延迟后 30ms 步进 scrollLeft 至 max 并 hold；移出复位 0；短名/`prefers-reduced-motion: reduce` 不启动；scrollable 类切换与遮罩样式零回归。
- §2 重建同步：应用成功触发回填且对话区清空；慢回填让位于紧随 send；回填失败提示不清空；应用失败不触发回填。
- §3 手动刷新：点击触发重列；失败保列表 + 错误；在途禁用；既有三时机零回归。

## 3. 人工验证（部署环境，验收记录闭合——spec A3/FR-004）

### 3.1 web 长名悬停自动滚动（FR-002）

1. 打开 web，创建一个 session（id 为长 UUID；若既有列表已有长 id 条目可直接用）。
2. **悬停**长 id 条目 → 观察约 0.25s 后名称自动向左滚动，尾部到达后停止保持；**移出** → 名称复位、渐隐恢复。
3. 悬停短名条目 → 无任何滚动/布局动作。
4. 快速掠过多个长名条目后停住 → 无残留滚动错位。
5. （可选）系统开启"减少动态效果"后悬停 → 不自动滚动（保持渐隐默认态）。

### 3.2 web 重建后对话即时同步（FR-001）

1. 进入一个已物化、空闲的 session，进行 ≥2 轮对话。
2. 打开"设置 agent"面板 → **再次应用**（同一或不同配置均可）。
3. 面板关闭后**不刷新页面**：对话区立即清空（旧对话、排队、错误/终止标识全清）；状态区显示已物化。
4. 立即发送一条新消息 → 正常流式输出，旧内容不再出现。
5. 忙时路径：流式输出中再次应用 → 输出终止、对话收敛为空态（与步骤 3 终态一致）。
6. 系统开启"减少动态效果"与本项无关；多标签页行为不在范围（spec A2）。

### 3.3 desktop 手动刷新（FR-003）

1. 打开 desktop，进入 sessions 列表页 → 工具栏右侧出现 `Refresh` 按钮。
2. 在 web 侧新建/删除一个 session → desktop **不操作**保持停留 → 点击 `Refresh` → 列表反映最新集合。
3. 刷新进行中：按钮禁用、列表区短暂 "Loading sessions..."。
4. 失败路径（如临时断开 gateway）：点击 `Refresh` → 错误呈现、既有列表不清空。
5. 回归：返回导航/模板切换/启动加载三个既有刷新时机行为与 055 交付一致。

## 4. 回归面

- 049/051/054/055 既有组件与 store 测试全绿（§2 命令覆盖）。
- 054 既有 testplan suites 零回归（如涉部署调整，通过 testplan skill 执行验证——`style/large_test.md` 规范）。

## 5. 预期结果汇总

| 修复 | 自动化断言 | 人工记录 |
|------|-----------|----------|
| FR-001 重建同步 | App.test.tsx（§2 覆盖面） | §3.2 步骤 3/4/5 |
| FR-002 长名揭示 | SessionList.test.tsx（§2 覆盖面） | §3.1 步骤 2/3/4 |
| FR-003 手动刷新 | App.test.ts（§2 覆盖面） | §3.3 步骤 2/3/4 |
