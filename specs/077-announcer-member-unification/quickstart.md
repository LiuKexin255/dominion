# Quickstart: 077 成员统一性补缺验证指南

**Spec**: [spec.md](spec.md) | **契约**: [contracts/member-event-stream.md](contracts/member-event-stream.md)、[contracts/inflight-broadcast-fastpath.md](contracts/inflight-broadcast-fastpath.md)、[contracts/host-member-parity.md](contracts/host-member-parity.md) | **数据模型**: [data-model.md](data-model.md)

本 feature 为内部结构重构 + 惰性机制交付：验证 = **行为等价回归 + 新机制单测 + 特例代码消失检查 + 大型测试全量回归**。无新对外 API，无需手工运行面。

## 1. 前置条件

- 仓库按 `AGENTS.md` 环境可用（bazel 入口、pnpm workspace）。
- 阅读顺序建议：spec → research.md（D1–D9 裁定）→ 三份契约 → data-model.md。

## 2. 编译与单测（每次代码变更执行；宪法 IV）

```bash
bazel build //common/js/dsh-plugins/team/... //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/...
bazel test //common/js/dsh-plugins/team/... //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/...
```

预期结果（对应 spec SC-001–SC-005，断言面见各契约"验收面"节）：

| 验证点 | 位置 | 预期 |
|---|---|---|
| 统一流信封/同源扇出/live 通道 | team 插件单测 | role/memberId/channel 标注正确；log 通道与 relay 同事件同序；announce-only 成员无 live 事件；dispose 后无事件（[member-event-stream.md](contracts/member-event-stream.md) §6） |
| 快路行为（SC-003 四项） | team + saolei-loop 单测 | 在途接收下一 step 携带（同锚点同构造）；恰好一次（pending 自愈剔除）；静止照旧 pending；终局播报无快路（[inflight-broadcast-fastpath.md](contracts/inflight-broadcast-fastpath.md) §7） |
| 顺序一致性 | team 单测 | 发言级序 = 生产序；工具单元级偏差仅限单 step 窗口（§4 披露形态） |
| 驱动零影响 | saolei-loop 单测 | inject 不唤醒（无新 turn/whenIdle 不变）；steered/queued 计数不含快路消息 |
| 投影行为等价 | `projects/game/agent_v2/src/session.test.ts` | 全量断言经统一流驱动通过（驱动面切换、断言不变） |
| 成员记账机械（输入入口化） | `projects/game/agent_v2/src/history.test.ts` | 全量断言经 `applyEvent` 入口驱动通过（驱动面切换、断言不变；与上行同属**原子变更组 A**——同批完成后统一校验） |
| announcer 同权回归 | 同上 | 076 验收面（三成员名册/成员读取/请求侧校验）+ 刷新无帧泄漏（[host-member-parity.md](contracts/host-member-parity.md) §4） |

## 3. 特例代码消失检查（代码面；spec SC-002）

```bash
grep -rn "announcerMemberView\|announceOff" projects/game/agent_v2/src/   # 预期无匹配
grep -n "isAgentMemberRole\|activeAgentMember" projects/game/agent_v2/src/session.ts   # 预期无匹配
```

## 4. 大型测试回归（testplan skill 实际执行；宪法 VI + spec SC-006）

使用 testplan skill（`tools/test/guitar`，规范 `style/large_test.md`）执行部署→测试→清理闭环：

```text
guitar run projects/game/testplan/system_test.yaml
```

预期：全部套件（game-system（含 conversation/game/wiki/web）、game-stall、game-memory-down、game-disconnect）**全部用例通过**——对外行为零变化（快路生产行为惰性；spec Clarifications 2026-10-16：不新增快路专用大型场景，真实端到端快路场景由后续 hook feature 携带）。

## 5. 通过标准

- §2 全部 bazel target 绿；§3 grep 空；§4 全部用例通过。
- 契约修订落盘（四份契约文件终态修订 + README 更新，spec FR-013/SC-007）：`specs/065-agent-v2-team-refine/contracts/team-member-source.md`、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`、`specs/076-agent-v2-optimize/contracts/flow-orchestration.md`、`specs/076-agent-v2-optimize/contracts/announcer-member.md`、`projects/game/agent_v2/README.md`。
