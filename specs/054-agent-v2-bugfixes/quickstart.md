# Quickstart: agent-v2 对话呈现与游戏链路缺陷修复 + testplan 重构

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Contracts**: [agent-api-changes.md](contracts/agent-api-changes.md) / [web-ui.md](contracts/web-ui.md) / [testplan.md](contracts/testplan.md)

本文是验证指引（非实现步骤）；实现细节见 tasks.md。按验证面分层，自下而上执行。

## 0. 前置

- 仓库构建/测试入口：`bazel build //...` / `bazel test //...`
- 大型测试：testplan skill（`guitar run <plan.yaml>`，规范 `style/large_test.md`）
- 排查工具：signoz skill（tracing/log 查询）

## 1. 单测/组件级（每次变更必过）

```bash
bazel test //projects/game/web/frontend/...        # store/组件 vitest（分段/折叠/markdown/终止/连接状态/preset 视图/菜单视觉）
bazel test //common/js/dsh-plugins/saolei-loop/... # driver ERROR 固化（interrupted append）
bazel test //common/js/dsh-plugins/desktop-bridge/... # isDesktopConnected
bazel test //projects/game/agent_v2/...            # :cancel/step 映射/模型目录/历史固化
```

关键断言面（对应契约"义务与验收锚点"）：step 分组与缺 step 退化、COMPLETED 多消息投影、ERROR/CANCELED 内容保留、cancel 幂等与排队落地、desktop_connected 事实一致、模型目录同源。

## 2. testplan 重构后全量执行（FR-024 / constitution 原则 VI）

```bash
# 经 testplan skill 执行（部署→测试→清理闭环，全部用例须 green）
guitar run projects/game/testplan/system_test.yaml
```

预期：

- 单 suite 单部署（deploy 合并后含双 fake-desktop 实例），cases 顺序执行；
- 全部用例通过（含新增：step 分段/ERROR 回填/cancel 语义/连接状态/模型目录；含既有：session/memory/web/conversation/preset/game(won+drop)/desktop-flow 零回归）；
- 总执行时间较重构前（7 次部署 + 超预算需 `--timeout=90m`）显著下降；
- `projects/game/testplan/deploy_agent_v2_drop.yaml` 已删除且无残留引用。

## 3. 真实环境端到端（SC-001，US1 验收）

前置：`projects/game/deploy.yaml` 正式部署（真实 GLM 端点 + 无 fake 组件）；Windows desktop 已构建。

步骤：

1. desktop 选择目标 session 连接（确认连接成功）并绑定扫雷窗口；
2. web 对话页确认该 session 桌面连接状态指示为"已连接"；
3. 物化 agent（preset 选既有 player 提示词；模型目录应显示 glm-5.3 与 glm-5.3-flash，默认 glm-5.3）；
4. 发送"开始一局扫雷"——断言：
   - 对话页流式期间按步骤分段呈现（思考折叠块/工具卡片/正文分列），**非单个"思考过程"大气泡**；
   - desktop 侧出现操作执行记录，**桌面扫雷游戏真实开始**（新局按键、格子点击真实发生）；
   - 工具结果棋盘与桌面实际画面一致；正文 markdown（列表/代码块等）正常渲染；
5. 游戏进行中点击**终止按钮**——断言：回合停止（desktop 不再收到新操作）、已产出内容保留、终态"已终止"、输入立即可用；如预先排队了消息，断言排队消息以历史用户消息形态出现在对话流且未触发回合；
6. 刷新页面——断言：分段与流式结束时形态一致（回合折叠：最终答案独立+思考过程区可展开）、无内容丢失；
7. 断开 desktop——断言：连接状态 ≤10s 变"未连接"；再次发起游戏指令时工具结果为明确错误（无游戏状态）；
8. preset 管理页：新建/编辑为独占视图（无列表残留）、保存/取消返回；session 侧栏 `···` 弹出**带卡片容器**的菜单、两步删除可用。

排查入口（步骤 4 异常时，按 [research.md](research.md) D10 playbook）：signoz 查询该请求 trace——`desktop connection attached` 日志、dispatch 结果、turn_end 终态与 error 内容。

## 4. 验收对照

| SC | 验证面 |
|---|---|
| SC-001 | §3 步骤 1–4/7（执行证据留存） |
| SC-002 | §1 组件用例 + §3 步骤 4/6 |
| SC-003 | §1（注入失败用例）+ §3 步骤 6 |
| SC-004 | §1 cancel 用例 + §3 步骤 5 |
| SC-005 | §1 连接状态用例 + §3 步骤 2/7 |
| SC-006 | §1（目录/视图/菜单用例）+ §3 步骤 3/8 |
| SC-007 | §2 testplan 全量 + §1 既有用例零回归 |
