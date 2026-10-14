# Quickstart: agent_v2 优化验证指南

**Feature**: [spec.md](spec.md) | **契约**: [contracts/](contracts/) | **数据模型**: [data-model.md](data-model.md)

按落地批次（research.md D2）组织的端到端验证场景。每批验证 = 编译 + 单测（随变更执行）+ 大型测试（testplan skill 实际执行，宪法 VI）；全部通过才算批次完成。

## 前置条件

- bazel 构建环境可用（`bazel build //...` / `bazel test //...`）。
- 大型测试经 testplan skill 执行（`guitar run projects/game/testplan/system_test.yaml --suite <name>`，完整部署→测试→清理闭环；规范 `style/large_test.md`；`projects/game/testplan/deploy_agent_v2*.yaml` 为部署配置、不可作为执行入口）。fake-llm / fake-desktop 基建见 `projects/game/testplan/`。
- web 手工验证需要可访问的 gateway 部署（大型测试环境或本地起服务）。

## 批1：wiki 注入移除 + memory 库分层 + unary 策略注入

**单测**：

```bash
bazel test //common/js/memory/... //common/js/dsh-plugins/saolei-memory/...
```

预期（契约 [memory-library-layering.md](contracts/memory-library-layering.md) §5）：

- memory 单元 / wiki 单元各自独立挂载测试通过（faces/tools/sections/ready 可用、工具解析自洽：`ctx.memory` / `ctx.wiki`）。
- 组合层组合产物与现状输出等价（工具集 ×4、section 名/order 逐项一致）。
- memory 单元无 guidance section（planner 能力 prompt 仅含 `memory:snapshot`）；saolei-memory 不构造 wiki client、策略函数经组装传入；库内无私有 callUnary/probeChannel 副本。

**端到端验证（planner 能力面，spec SC-002）**：物化 team 后经 GetTeamMember 读取 planner 的 output-only `system_prompt`——组装结果**不含** wiki 工具、不含 `wiki:guidance` section、不含任何 wiki 文本引用；工具清单仅 `memory`。

**wiki 基建零改动回归（spec SC-004）**：

```bash
# 经 testplan skill 执行（wiki_test 与 conversation 同属 system_test.yaml 的 game-system 套件）
guitar run projects/game/testplan/system_test.yaml --suite game-system
```

预期：`//projects/game/testplan:wiki_test` 全部用例通过（gateway wiki HTTP 面、web WikiView 行为不变）。

## 批2：announcer 更名 + 成员化呈现

**单测**：

```bash
bazel test //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/... //projects/game/web/...
```

预期（契约 [announcer-member.md](contracts/announcer-member.md) §6）：三成员名册视图；GetTeamMember 服务 announcer（system_prompt 空值）；请求侧校验保持两 LLM 成员；导出面无 `saolei` 残留（`ANNOUNCER_MEMBER_ROLE` 等）。

**端到端验证（spec SC-006）**：

```bash
guitar run projects/game/testplan/system_test.yaml --suite game-system   # conversation 场景（断言含 member="announcer"）
```

预期：终局交接后团队归并序列恰有一条 `member="announcer"` 统计消息；`GetTeam` 名册含三成员（announcer 无 preset/model）；web 名册与激活指示渲染 announcer。

**手工 web 验证**：物化 team → 名册显示 announcer（无 preset/model 形态）→ 完成一局 → 观察播报消息标签为 `announcer`、播报窗口激活指示可达。

## 批3：flow 标准化重构

**单测（行为保持基准 + 新增结构测试）**：

```bash
bazel test //common/js/dsh-plugins/saolei-loop/... //common/js/dsh-plugins/team/... //projects/game/agent_v2/...
```

预期（契约 [flow-orchestration.md](contracts/flow-orchestration.md) §8）：

- `orchestrator.test.ts` 全量用例通过，两点显式调整成立：多消息时序按 061 方向（mid-turn 进入，参考 `specs/061-team-queue-steer/spec.md` SC）；cancel 打断 review 在途的竞态按 aborted 冻结语义修正（记录不被消费）。
- flow 结构测试：F-Moore 边表（8 边）逐边可判定；last-outcome 分流（settled/resting → 边判定；failed/aborted → 冻结重启）；错误契约六码映射表；扩展演示（spec SC-007）——测试形态新增示例活动/边，flow 运行时与执行层零改动。

**端到端验证（spec SC-005/SC-008）**：

```bash
guitar run projects/game/testplan/system_test.yaml --suite game-system        # conversation（含在途消息 mid-turn 进入断言）
guitar run projects/game/testplan/system_test.yaml --suite game-stall         # 流停滞恢复
guitar run projects/game/testplan/system_test.yaml --suite game-memory-down   # memory 服务不可达（物化回滚/fail-loud）
```

预期：全部用例通过；用户在成员回合在途发送的消息于下一个 step 边界进入成员上下文（不再等待整个回合收束）；错误呈现按码携带用户动作文案。

**手工 web 验证**：物化 → 发消息 → 观察 planner→player 切换与激活指示；turn 在途再发消息 → 观察排队 chip 在 mid-turn 消费时消除（061 `web-queue-ui.md` chip 生命周期）；游戏终局 → 播报+复盘链路正常；Cancel → 再发消息恢复同成员；五轮后自然停（静止，发消息恢复）。

## 关键回归对照（全部批次共用）

- 现状 S1–S10 场景映射保持：`survey/saolei-member-switch-flow.md` §9 表。
- 063 流收束 origin 二分保持：`specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md` §2。
- 068 自然停语义保持：`specs/068-wiki-round-pause/contracts/saolei-loop-round-stop.md`。
