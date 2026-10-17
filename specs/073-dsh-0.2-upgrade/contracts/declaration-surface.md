# Contract: 声明面变更（catalog / 底座 / lockfile / packageExtensions）

**Feature**: specs/073-dsh-0.2-upgrade/spec.md（FR-001、FR-002、FR-004、FR-005、FR-010）
**依据**: specs/073-dsh-0.2-upgrade/research.md D1（互锁矩阵）、D3（变更清单）

## 1. catalog（`pnpm-workspace.yaml`）

**变更**：26 个 `@deepseek-ai/dsh-*` 条目值 `0.1.7-rc.2` → `0.2.0-rc.2`（`catalog:` 协议，消费者自动跟随）。

**零改动项（契约性不变）**：

- 互锁条目：`@deepseek-ai/cordis`、cordis-plugin-{loader,include,group,timer}、`node-addon-require-builtin`、`@deepseek-ai/schemastery` ——值不变（0.2.0 线 peer 与 0.1.7 逐字段相同，research.md D1）。
- 条目集合：不增不删（0.2.0 线无新增 peer 义务，spec FR-005）。

## 2. packageExtensions（`pnpm-workspace.yaml`）

**变更**：两处键的版本段，以及 primitives 键内三处嵌套 `@deepseek-ai/*` 值：

```text
"@deepseek-ai/dsh-client-ui-primitives@0.1.7-rc.2" → "@0.2.0-rc.2"
"@deepseek-ai/dsh-client-store@0.1.7-rc.2"          → "@0.2.0-rc.2"

primitives 键内嵌套值：
  "@deepseek-ai/dsh-client-store":        "0.1.7-rc.2" → "0.2.0-rc.2"
  "@deepseek-ai/dsh-util-code-language":  "0.1.7-rc.2" → "0.2.0-rc.2"
  "@deepseek-ai/dsh-util-workspace-path": "0.1.7-rc.2" → "0.2.0-rc.2"
```

**不变**：两键的依赖键集合与其余依赖集合（两包 devDependencies 键集合 0.1.7 → 0.2.0 零变化，tarball 实测）。嵌套 `@deepseek-ai/*` 值是 primitives devDependencies 的镜像，**值必须随线**——只改键会让 pnpm 链接旧线副本，破坏 SC-001 版本唯一与 FR-010「全部 `@deepseek-ai/*` 解析 `0.2.0-rc.2`」（research.md D2/D3）。键未随线更新 ⇒ pnpm 不应用扩展 ⇒ primitives 运行时导入解析失败（构建期暴露）。

**同文件说明注释**：`packageExtensions` 前导说明注释中的 `0.1.7-rc.2` 版本串随线更新为 `0.2.0-rc.2`（SC-006 覆盖 `pnpm-workspace.yaml` 注释）。

## 3. 框架核心底座（`third_party/dsh/core`）

| 面 | 变更 |
|---|---|
| `package.json` | 5 个 `dsh-*` 条目切 `0.2.0-rc.2`（app-boot、home-paths、invariants、launch-environment、system-prompt）；cordis 家族 5 条与 node-addon-require-builtin 的 catalog 值不变（共 11 包闭包，FR-002）；依赖键集合不变 |
| `BUILD.bazel` | `npm_deps` 标签集合随 `bazel run //:gazelle third_party/dsh/core` 刷新；`runtime_pkg` 上方闭包注释同步 0.2.0-rc.2 线事实并改引 `specs/073-dsh-0.2-upgrade/research.md` D1（070 先例 `specs/070-dsh-upgrade/tasks.md` T012；注释不随 gazelle 变化） |
| `version.ts` | `DSH_CORE_SNAPSHOT = 'dsh-core@0.2.0-rc.2'`；注释中 cordis 互锁值描述与 0.2.0 线事实一致（值未变），仅 dsh 版本串与快照说明更新 |

**零插件约束**：11 包清单不变（closure audit 断言①，`experimental/dsh/demo/testplan/closure_audit_test.go` `d6CorePackages` 口径）。

## 4. lockfile 刷新与验证

**流程**（不得手改 `pnpm-lock.yaml`）：

1. `bazel run @pnpm -- --dir /mnt/code/dominion up`
2. 受影响目录 `bazel run //:gazelle <dir>`（third_party/dsh/core、projects/game/agent_v2、experimental/dsh/demo、common/js/dsh-plugins、projects/game/web/frontend）
3. `bazel mod tidy`

**刷新后验证断言**：

- 全部 `@deepseek-ai/*` 条目解析 `0.2.0-rc.2`（零 `0.1.7-rc.2` 残留）；
- `@deepseek-ai/cordis` 全仓库唯一物理版本（4.0.4）；
- `node-addon-require-builtin@0.1.6` 的 `linux-x64-gnu` optionalDep 在场。

## 5. 消费 package.json（零变化契约）

`projects/game/agent_v2/package.json`、`experimental/dsh/demo/agent/package.json`、`common/js/dsh-plugins/*/package.json`、`projects/game/web/frontend/package.json` 的 dsh 依赖**键集合与协议**不变（全部 `catalog:`）——版本切换仅经 catalog 生效。任何向这些文件新增/删除 dsh 依赖键的改动都违反 FR-005（除非实现期发现破坏性变更，按 spec Edge Cases 回退条款整体处理）。
