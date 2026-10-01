# Quickstart: gRPC client 基建插件、memory 能力化与 preset 模板简化 验证指南

**Feature**: [spec.md](spec.md) | 日期: 2026-09-30

本文是端到端验证指南：证明特性按 [spec.md](spec.md) Success Criteria 生效。实现细节见 [tasks.md](tasks.md)；契约引用见 [contracts/](contracts/)。

## 前置条件

- Bazel 构建环境可用（`bazel` / `bazel run //:go` / `bazel run @pnpm` 等命令约定见 `AGENTS.md`）。
- 大型测试经 testplan skill 执行（skill `testplan`；规范 `style/large_test.md`）。
- 迁移分步（research.md D11 P1–P4）完成后做全量验收；每步的中间门禁见 research.md D11 表。

## 1. 构建与单测（宪章原则 IV：每次变更执行）

```bash
bazel build //...
bazel test //common/js/dsh-plugins/grpc-runtime/... \
           //common/js/dsh-plugins/saolei-memory/... \
           //common/js/dsh-plugins/saolei/... \
           //common/js/dsh-plugins/saolei-loop/... \
           //common/js/dsh-plugins/preset-authoring/... \
           //common/js/memory/... \
           //projects/game/agent_v2/...
```

**预期结果**：

- 全部通过（bazel 退出码 0）。
- 重组后单测覆盖（SC-001 单测部分、SC-005、SC-008）：基建缓存/共享/策略/teardown/otel 顺序/canonical protoPath 解析；memory 领域语义等价迁移用例（原 memory-service `operations.test`/`wiki-operations.test` 文件级迁入公共库）；`buildMemoryCapabilities` 单元完整性（faces/tools/sections/ready 原子产出）；saoleiTools 提供方测试（静态单元内容、无全局注册）；memoryMount 提供方测试（config 驱动 client 构造、build(scope) 显式传参）；saolei-loop 依赖装配测试（ctx.plugin 挂载、提供缺失 fail-loud）与编排消费测试（注册序、player/planner 分支、rollback 传播）；缺面 fail-loud 断言（三类：未注册单元工具执行、预取失败回滚、依赖装配失败）。

## 2. 终态静态验证（SC-002 / SC-003 / SC-006）

```bash
# memory-service 零残留（代码+文档）；旧 dsh-memory 插件包名清零（已退化迁址为 common-js-memory）
grep -rn "dsh-memory-service\|memory-service" --include="*.ts" --include="*.md" \
  common/ projects/ testplan/ | grep -v "specs/"
grep -rn "plannerMemory\|@dominion/dsh-memory'" common/ projects/ --include="*.ts" --include="*.md" --include="*.yml" | grep -v "specs/"
# templateRules 零残留
grep -rn "templateRules\|validateTemplateRows\|TemplateRowRules" common/ projects/ --include="*.ts" --include="*.yml" | grep -v "specs/"
```

**预期结果**：三组检索均零命中（`specs/` 内本特性文档除外）。

组合清单终态核对（`projects/game/agent_v2/cordis.yml`，对照 [contracts/preset-templates.md](contracts/preset-templates.md) §5）：含 `grpc-runtime` 行；无 `memory` host 行；saolei-loop config 含**扁平**能力依赖列表（`@dominion/dsh-saolei`；`@dominion/dsh-saolei-memory` + target/protoPath config，无角色分组）；模板 plugins 仅 persona 行；无 templateRules。另核对旧包清零：`common/js/dsh-plugins/memory/` 与 `common/js/dsh-plugins/memory-service/` 目录不存在，`common/js/memory/` 公共库存在。

## 3. 大型测试验收（SC-001 / SC-007；宪章原则 VI：实际执行、全用例通过）

经 testplan skill 执行四 suite（`projects/game/testplan/system_test.yaml`：game-system / game-disconnect / game-memory-down / game-stall），完成部署→测试→清理闭环：

```text
guitar run projects/game/testplan/system_test.yaml
```

**预期结果**（与改造前对照零回归）：

- **game-system**：preset 闭环（CRUD/物化/模型目录）、agent_v2 对话面、game 回路、memory/wiki 存储 API 全用例通过——覆盖 SC-007（preset 回路不回归）与 planner 记忆回路。
- **game-memory-down**：memory 服务故障路径用例通过——planner 物化期 fail-loud 预取/回滚行为在真实故障拓扑下成立（行为差异契约 [team-assembly.md](contracts/team-assembly.md) §4③ 的端到端验证）。
- **game-disconnect / game-stall**：player 回路与流停滞路径不回归（组装时序变更对 player 零影响）。
- 全部用例 passed（failed/flaky 均为验收未通过，须修复重跑）。

## 4. 连接共享验证（SC-004）

单测断言：同一组合内构造 MemoryService 与 WikiService 两个 client，断言两者共享同一底层 channel（基建 channel 缓存命中；实现于 grpc-runtime 单测）。可观测面：otel/日志中同 target 单 channel。

## 5. 交互回路抽查（可选人工验证）

1. web 建两个 planner preset（persona 不同）→ 分别物化 → 两 planner 均具备 memory/wiki 四工具与快照 section（SC-008：组装完整性）。
2. player 成员对话 → 工具面仅 saolei 三工具，prompt 无 memory 痕迹（spec FR-017）。
3. planner 写入 memory → 新 session 同 preset 物化 → 快照含该条目（specs/071 跨 session 持久语义不回归）。
