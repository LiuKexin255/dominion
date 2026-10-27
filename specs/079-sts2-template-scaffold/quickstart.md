# Quickstart: sts2 场景模板脚手架验证指南

> Feature: [specs/079-sts2-template-scaffold/spec.md](spec.md) | 契约：[contracts/scene-dispatch.md](contracts/scene-dispatch.md)、[contracts/dsh-plugins-layout.md](contracts/dsh-plugins-layout.md) | 数据模型：[data-model.md](data-model.md)
> 本指南给出端到端验证场景；实现细节见 tasks.md（`/speckit.tasks` 产出）。

## 前置条件

- 仓库可执行 bazel（构建/测试入口，AGENTS.md）。
- 大型测试需 testplan skill（`tools/test/guitar`），规范见 `style/large_test.md`。
- 手工 API 验证需一个运行中的 game 栈（可由 testplan 部署提供，或 `projects/game/deploy.yaml` 拓扑）；下文以 `GATEWAY` 代指 gateway 的 `http://<host>:<port>`。

## V1: 构建与单测（每阶段例行）

```bash
bazel build //...
bazel test //common/js/dsh-plugins/... //projects/game/... //experimental/dsh/demo/...
```

**预期**：全部通过；`projects/game/pkg/gameconst/const_test.go` 含 sts2 白名单用例；`agent_v2` 的 `dsh.test.ts` 断言 24 行组合清单（含 `- { id: sts2, name: '@dominion/dsh-sts2' }` 与 080 的 `- { id: sts2-loop, name: '@dominion/dsh-sts2-loop' }`）。

## V2: 目录布局与打包通道

```bash
ls common/js/dsh-plugins/                 # 顶层 = 6 个通用插件目录 + saolei/ + sts2/
ls common/js/dsh-plugins/saolei/          # game loop tools memory hooks
ls common/js/dsh-plugins/sts2/game/       # 骨架包（package.json/BUILD.bazel/src/README.md）
ls common/js/dsh-plugins/sts2/loop/       # 080 起：最小编排包（@dominion/dsh-sts2-loop）
```

**预期**：与 [contracts/dsh-plugins-layout.md](contracts/dsh-plugins-layout.md) §1/§2 布局一致；`.bazelignore` 无 `saolei-interrupt` 残留、含 saolei 组与 `sts2/game`、`sts2/loop` 的 node_modules 新条目；构建产物闭包含 `node_modules/@dominion/dsh-sts2` 与 `node_modules/@dominion/dsh-sts2-loop`（V1 构建成功即隐式验证）。

## V3: sts2 数据面接受（SC-001）

```bash
# Session 全生命周期（创建 → 读取 → 列出 → 删除）
curl -s -X POST "$GATEWAY/api/v1/templates/sts2/sessions" -H 'Content-Type: application/json' \
  -d '{"session_id":"sts2-scaffold-check"}'
curl -s "$GATEWAY/api/v1/templates/sts2/sessions/sts2-scaffold-check"
curl -s "$GATEWAY/api/v1/templates/sts2/sessions"
curl -s -X DELETE "$GATEWAY/api/v1/templates/sts2/sessions/sts2-scaffold-check"
# Wiki 的 template 作用域 CRUD 同法（/api/v1/templates/sts2/wikis）。
# Memory 为 preset 作用域（/api/v1/templates/sts2/presets/{preset}/memories）：
# 服务层按 (template, preset) 资源名结构接受（不校验 preset 是否存在）；080 起
# sts2 preset 词汇为 main（080 FR-009），结构接受由 T009 的 memory handler sts2
# 用例（单测）验证，手工验收不产生业务 memory 资源。
# proxy 为 gRPC 转发服务、无对外 CRUD 面，其模板校验由 Go 单测（proxy handler 既有用例）覆盖。
```

**预期**：与 saolei 同构成功（数据落在 sts2 分区）；`templates/unknown/...` 仍被 `INVALID_ARGUMENT`/对应错误拒绝。

## V4: sts2 场景化操作（契约 §1/§3；080 起已接线）

sts2 的 `UpdateTeam` 与 preset 面自 080-host-main-sub-model 起开放（单 `main` 成员 / role `main`），与 saolei 同构成功：

```bash
# preset 创建（role 为 query 参数——agent_v2.proto CreatePresetRequest）；persona 留空保留 main 池模板 base
curl -s -X POST "$GATEWAY/api/v2/templates/sts2/presets?preset_id=x&role=main" \
  -H 'Content-Type: application/json' -d '{"persona":""}'
# team 物化（单 main 成员，AIP-134 create-or-update）
curl -s -X PATCH "$GATEWAY/api/v2/templates/sts2/sessions/<session>/team?allow_missing=true" \
  -H 'Content-Type: application/json' \
  -d '{"members":[{"role":"main","preset":"templates/sts2/presets/x"}]}'
# Get/Update/Delete preset 同法成功：资源名 templates/sts2/presets/{preset}。
```

**预期**：上述请求成功（`GetTeam.members` 恰 `main`）；fail-loud（`INVALID_ARGUMENT`）仅对**未知模板**（`templates/unknown/...`）生效，机制见 [contracts/scene-dispatch.md](contracts/scene-dispatch.md) §3；同一批请求对 `templates/saolei/...` 行为与现状完全一致（saolei 零变化）。

## V5: 组合清单与启动（SC-002/SC-004）

- 单测层：`dsh.test.ts` 24 行断言通过（V1 已含）。
- 运行时层：任一次大型测试部署成功启动即验证 sts2 惰性行的"打包 → 部署 → 裸名解析 → 挂载"通道。

## V6: 大型测试验收（FR-010，宪法原则 VI）

按 `style/large_test.md` 经 testplan skill 实际执行（部署→测试→清理闭环）：

```bash
# 经 testplan skill（guitar CLI）执行 projects/game/testplan/system_test.yaml 的五个 suite
```

**预期**：**全部用例通过**（存在 failed/flaky 即验收不通过）；saolei 全链路（游戏循环/断连恢复/memory 宕机/停滞恢复四 suite）行为与迁移前一致，080 新增 `game-sts2` suite 同批全部通过（五 suite）。

## V7: 泛化结构检查（SC-005）

```bash
grep -n 'saolei\|"player"\|"planner"' projects/game/agent_v2/src/server.ts projects/game/agent_v2/src/session.ts
```

**预期**：命中仅限注释（模板白名单 `server.ts` 的 `KNOWN_TEMPLATES` 自 080-host-main-sub-model 起派生自场景表键集，已无 `saolei` 字面量）；**不得**出现 `"player"`/`"planner"` 代码字面量，也不得出现 `templates/saolei` 常量拼接——roster/角色词表/资源父名/任务目标均经场景表 `scenes.ts`（错误文案由场景表字段构造，saolei 产出逐字节不变）。

> 080-host-main-sub-model 起 `@dominion/dsh-saolei-loop` 导入已移出 `session.ts`（saolei 适配器落在 `scene-saolei.ts`）；本节对导入路径的旧白名单失效，以 080 [quickstart.md V3](../080-host-main-sub-model/quickstart.md) 的零命中硬门禁为准。
