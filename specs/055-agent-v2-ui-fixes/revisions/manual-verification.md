# 全量人工验证记录：quickstart §3 五场景（T015）

**Feature**: `specs/055-agent-v2-ui-fixes/spec.md`
**日期**: 2026-09-05（部署环境，用户执行）
**结论**: 五场景全部通过；过程中发现的两处实现缺陷（THINK-only 卡片塌缩、横幅变量作用域）已修复并复验通过（断点记录见 `specs/055-agent-v2-ui-fixes/revisions/fr001-investigation.md`）。

## 验证结果

| # | 场景（quickstart §3） | 结果 | 依据 |
| --- | --- | --- | --- |
| 1 | 窗口无滚动（思考折叠态 / 展开态 / 正文输出，FR-001） | ✅ 通过 | 展开态滚动条缺陷经三轮断点修复（contain: size 塌缩 → visually-hidden span 逃逸 ICB → code 块填宽）后复验通过；断点数据与复验记录见 `specs/055-agent-v2-ui-fixes/revisions/fr001-investigation.md` |
| 2 | 跟随不劫持与回底（FR-002~004） | ✅ 通过 | 贴底跟随、上滚位置保持、非贴底"回到底部"按钮、发新消息回底，用户部署环境逐项确认 |
| 3 | 横幅可读（FR-005/006） | ✅ 通过（修复后） | 初验发现白字黑底（`--app-banner-*` 声明在 `:root` 引用 body 域 var() 得 guaranteed-invalid 回退 unset）→ 修复移入 body 块（commit f2209ce）→ 复验通过：错误横幅深红底浅红字、"已终止"琥珀底黄字、两色系可区分 |
| 4 | desktop 刷新（FR-007） | ✅ 通过 | 返回列表自动刷新、刷新失败错误呈现且列表数据不清空，用户部署环境确认 |
| 5 | 对齐复查（FR-009/SC-005） | ✅ 通过 | 契约逐条款对照记录见 `specs/055-agent-v2-ui-fixes/revisions/parity-audit-review.md`；chevron 折叠开关与 sweep 动画深色主题可见性用户确认（思考行前导图标 hover 交换为包内设计，上游同款行为） |

## 验证过程中登记的范围外反馈（不在本 feature 处理，待立项）

1. web session 列表长 id 渐变截断、hover 无横向滚动（054 契约的有意设计，改交互属新 feature）。
2. desktop 列表页手动刷新按钮（spec A4 裁定刷新时机仅返回导航，扩展触发面属新产品决策）。
3. desktop 链接前置校验与 agent run 中断开优雅终止（改动服务行为，spec A5 边界外）。
4. `saolei_operate` 执行成功但棋盘未翻开（session `9b7c87cd7d9effcbe179c14e12358d65` / call `call_067e54af1b22449faab3904a`，功能缺陷，需按 trace 排查执行器链路）。
