# Quickstart: dsh 0.1.7 升级端到端验证

**Feature**: specs/070-dsh-upgrade/spec.md
**Contracts**: [composition-manifest.md](contracts/composition-manifest.md) · [preset-model-mapping.md](contracts/preset-model-mapping.md) · [mongo-preset-migration.md](contracts/mongo-preset-migration.md)
**Data model**: [data-model.md](data-model.md)

本指南给出可执行的验证场景，证明升级后系统与升级前行为一致（SC-001–SC-006）。实现细节见 tasks.md（后续 `/speckit.tasks`）。

## 前置

- 仓库构建入口：bazel（`AGENTS.md`）；pnpm 经 `bazel run @pnpm -- --dir <abs path>`。
- 大型测试经 testplan skill（`tools/test/guitar`，`style/large_test.md`）。
- 环境变量按各 deploy yaml（fake-llm / mongo / fake-desktop）。

## 场景 1：版本线与声明面（SC-001/SC-004）

```bash
# 底座与全仓构建
bazel build //third_party/dsh/core:runtime_pkg //projects/game/agent_v2/... \
  //experimental/dsh/demo/... //common/js/dsh-plugins/... //projects/game/web/frontend/...

# lockfile 同线检查（人工 + 断言）
grep -c "0.1.7-rc.2" pnpm-lock.yaml        # @deepseek-ai/* 全部同线
grep -c "dsh-agent-presets" pnpm-lock.yaml # 期望 0（包已移除）
grep -n "'@deepseek-ai/cordis@4.0.4'" pnpm-lock.yaml  # cordis 唯一线内版本

# closure audit（随 demo testplan，或单独）
bazel test //experimental/dsh/demo/testplan:closure_audit_test
```

**预期**：构建全绿；audit ①（核心 plugin-free）②（声明可追溯）③（版本唯一）通过。

## 场景 2：agent_v2 行为不回归（SC-002/SC-003）

```bash
bazel test //projects/game/agent_v2/... //common/js/dsh-plugins/...
```

**预期**：单测全绿——含重写后的 `dsh.test.ts` 组合清单断言（registry `default: ''`、`templates` 快照断言、persona `prefix`）、preset-authoring register/unregister 序列断言、新增"激活失败留 roster 拒新绑定"与"update 修订保留"用例（[preset-model-mapping.md](contracts/preset-model-mapping.md) §5）。

## 场景 3：game 四拓扑系统测试（宪法 VI，SC-003）

```bash
# 经 testplan skill 实际执行（部署→测试→清理闭环）
guitar run projects/game/testplan/system_test.yaml
```

**预期**：四拓扑（game-system / game-disconnect / game-memory-down / game-stall）全部用例通过——对话、preset 创建/派生/校验（templateRules 拒绝路径）、team 群聊（player/planner 工具隔离）、planner memory、流停滞看护，与升级前一致。

## 场景 4：demo 接口测试与 preset 演示（SC-004）

```bash
guitar run experimental/dsh/demo/testplan/interface_test.yaml
```

**预期**：`testplan_test`/`multiturn_test`/`preset_test`（16 用例，含 preset 组合/创作生命周期）全绿；preset 演示在新模型（registry + `templates` 快照）下语义等价。

## 场景 5：web 前端（SC-002）

```bash
bazel test //projects/game/web/frontend/...
```

**预期**：构建与组件测试全绿（ui-primitives 0.1.7 + 重取 theme；`src/theme.css` 的 CodeBlock.module.css 契约重验）。

## 场景 6：Mongo preset 迁移演练（SC-005/FR-012，最后执行）

> 裁定注记（2026-09-28）：存量记录实测已为新模型形态，数据改写经裁定跳过；导出
> 存档保留至发布窗口，0-diff + 物化冒烟随发布部署复核（[tasks.md](tasks.md) T027 注记）。

前置：场景 1–5 全部通过。按 [mongo-preset-migration.md](contracts/mongo-preset-migration.md) 执行：

1. 旧版本 `ListPresets` 导出存档；
2. 部署新版本；
3. 试插探测 → 回灌（4a）或删后重建（4b）；
4. `ListPresets` 全量 diff + team 物化冒烟（player/planner 各一）。

**预期**：0 diff；冒烟通过；执行证据记录于 PR。

## 收尾检查（SC-005/SC-006）

- 仓库无旧线残留：`grep -rn "0.1.1-rc.2" --include="*.md" --include="*.ts" --include="*.yml" specs/070 之外的相关声明面`（版本快照注释已更新）。
- 交付文档引用完整（research/contracts/data-model 互链 + 上游来源 URL）。
