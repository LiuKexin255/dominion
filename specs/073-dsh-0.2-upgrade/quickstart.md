# Quickstart: dsh 升级到 0.2.0 线 验证指南

**Feature**: specs/073-dsh-0.2-upgrade/spec.md

按依赖序排列的可执行验证场景；每场景给出命令与预期结果。实现细节见 `tasks.md`（由 `/speckit.tasks` 生成），契约细节见 `contracts/`。

## 前置

- 仓库根目录执行；bazel 可用；`pnpm` 经 `bazel run @pnpm -- --dir /mnt/code/dominion` 调用（AGENTS.md）。
- 升级前基线：全部相关 target 在 `0.1.7-rc.2` 线下绿。

## 场景 1 — lockfile 同线检查（FR-010、SC-001）

```bash
grep -c '0\.1\.7-rc\.2' pnpm-lock.yaml   # 预期 0（@deepseek-ai 段零残留）
grep -n "'@deepseek-ai/dsh-agent@0\.2\.0-rc\.2'" pnpm-lock.yaml | head -1   # 预期命中
ls node_modules/.pnpm/ | grep -c '^@deepseek-ai+cordis@'   # 预期 1（唯一物理副本）
grep -A8 "node-addon-require-builtin@0.1.6:" pnpm-lock.yaml | grep -c "node-addon-require-builtin-linux-x64-gnu"   # 预期 ≥1
```

## 场景 2 — 底座构建与断言①（FR-004）

```bash
bazel build //third_party/dsh/core:runtime_pkg   # 通过
bazel test //experimental/dsh/demo/testplan:closure_audit_test   # 断言① plugin-free、②③ 声明可追溯/版本唯一
```

（审计 target：`experimental/dsh/demo/testplan/closure_audit_test.go`，expected set 由 package.json 声明自动推导——`specs/070-dsh-upgrade/research.md` D12 机制零改动。）

## 场景 3 — 服务与生态单测全绿（FR-006、SC-002）

```bash
bazel test //projects/game/agent_v2/... //experimental/dsh/demo/agent/... \
  //common/js/dsh-plugins/... --nocache_test_results   # 全部通过
```

预期零源码适配即绿；若失败路径断言失配（dsh-agent-loop 失败 step 补记 `tool/result`，research.md D6），按新事实重写断言后必须全绿——不存在删除/跳过用例。

## 场景 4 — 前端资产对齐 + shimmer 采用（FR-008/009/013、SC-005）

```bash
bazel build //projects/game/web/frontend/...          # 含 :dist Vite 生产构建
bazel test //projects/game/web/frontend/...           # vitest 全绿（含 FR-022 与 ReasoningRow shimmer 用例）
cd projects/game/web/frontend && ./node_modules/.bin/tsc --noEmit && cd -   # 类型面零错误（bazel 门禁不含 tsc）
grep -n "0\.2\.0-rc\.2" projects/game/web/frontend/src/dsh-theme/README.md   # 版本记录已更新
```

页面 smoke（大型测试覆盖）：`projects/game/testplan/system_test.yaml` 的 `web_test` suite 经 `/api/v1` + `/api/v2` 完成管理闭环；shimmer 视觉属组件测试面（jsdom 断言 DOM/属性语义）。

## 场景 5 — 大型测试验收（FR-007/FR-012、SC-003/SC-004；宪法 VI）

经 testplan skill 实际执行（部署→测试→清理闭环，全部用例通过）：

```text
projects/game/testplan/system_test.yaml      # agent_v2 四拓扑：game-system 对话/preset/team/memory + game-disconnect + game-memory-down + game-stall
experimental/dsh/demo/testplan/interface_test.yaml   # demo 接口面（closure audit ①②③ 为独立 go_unittest，经 T013 单独执行，不属本计划用例）
```

## 场景 6 — 残留清查（SC-006）

```bash
grep -rn "0\.1\.7" \
  --include="*.json" --include="*.yaml" --include="*.ts" --include="*.tsx" \
  --include="*.bazel" --include="*.bzl" --include="*.md" --include="*.go" \
  AGENTS.md README.md package.json pnpm-workspace.yaml \
  third_party/ projects/ experimental/ common/ tools/ survey/ | grep -v specs/   # 预期零输出
```

（豁免：`specs/` 下历史文档与本特性交付文档中的版本线对比表述。判据为 `0.1.7` 版本串全量（`0.1.7-rc.2` 为其子集）：覆盖声明面、`pnpm-workspace.yaml` 说明注释、`BUILD.bazel` 声明与注释、`version.ts`、版本化 URL，以及 README/源码/测试注释中的版本线表述；按原则 VII 改写为终态表述，不留"从 0.1.7 演进到现在"的叙述。）

## 回退

若实现期发现 0.2.0-rc.2 破坏性变更且无法适配（spec Edge Cases）：整体回退 catalog 至 `0.1.7-rc.2` 并 `pnpm up` 刷新，差异记录于交付文档。
