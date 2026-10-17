# Research: dsh 升级到 0.2.0 线（core 底座 + 官方插件 + web 前端 webUI）

**Feature**: specs/073-dsh-0.2-upgrade/spec.md
**Date**: 2026-09-29
**引用约定**: 文中不带路径的 `070 D<n>`/`070 T<n>` 均指 `specs/070-dsh-upgrade/research.md` 与 `specs/070-dsh-upgrade/tasks.md`；本特性自身的 `D<n>` 编号见本文档。
**方法**: npm registry 实测（dist-tag / version / peerDependencies 元数据）+ 28 个消费包 `0.2.0-rc.2` tarball 与仓库已装 `0.1.7-rc.2` 的逐字节 diff（070 调研方法的延续，`specs/070-dsh-upgrade/research.md`）+ 上游 GitHub 两 tag tree 对比（`dsh-v0.1.7-rc.2`...`dsh-v0.2.0-rc.2`，448 commits）+ release notes 与 `.agents/notes/` 架构注记 + 前端消费面源码核实。

---

## D1 — 目标版本线与互锁矩阵（0.2.0-rc.2，cordis 值不变）

**Decision**: 全仓库 dsh 闭包统一升级到 `0.2.0-rc.2`（registry `next` tag，2026-09-29 实测；0.2.0 GA 未发布，仅有 0.2.0-rc.1 2026-09-28 与 0.2.0-rc.2 2026-09-29）。rc 形态取法沿 070 先例（`0.1.7-rc.2`）。

| 包 | 0.1.7 线现值 | 0.2.0 线值 | 依据 |
|---|---|---|---|
| `@deepseek-ai/dsh-*`（26 个 catalog 条目） | `0.1.7-rc.2` | `0.2.0-rc.2` | lockstep 发布线 |
| `@deepseek-ai/cordis` | `~4.0.4`（解析 4.0.4） | **不变**（上游无新版本，registry 最高 4.0.4） | dsh-app-boot/agent/loop 等 peer `~4.0.4`，与 0.1.7 线一致 |
| `@deepseek-ai/cordis-plugin-loader` | `1.0.5` | **不变**（最高 1.0.5） | peer `~1.0.5` |
| `@deepseek-ai/cordis-plugin-include` | `1.0.9` | **不变**（最高 1.0.9） | peer `~1.0.9` |
| `@deepseek-ai/cordis-plugin-group` | `1.0.4` | **不变**（最高 1.0.4） | peer `~1.0.4` |
| `@deepseek-ai/cordis-plugin-timer` | `1.1.6` | **不变**（最高 1.1.6） | peer `~1.1.6` |
| `node-addon-require-builtin` | `0.1.6` | **不变**（最高 0.1.6） | app-boot dep `^0.1.6` |
| `@deepseek-ai/schemastery` | `^3.18.4` | **不变** | agent-loop/preset-registry dep `~3.18.4` |
| `@deepseek-ai/dsh-client-ui-theme`（vendored，非 catalog） | vendored 0.1.7-rc.2 | 重取 `0.2.0-rc.2` | 同线发布（D4） |

**Rationale**: 0.2.0 线 peer 互锁值与 0.1.7 线逐字段相同（registry 实测 `peerDependencies`），cordis 家族无新版本可解析——catalog 中这些条目零改动即落在线内，天然避免双 cordis 副本风险（070 Edge Case 先例）。

**Alternatives considered**: 等 0.2.0 GA——发布时间不可控且 rc 与 GA 差异通常小（rc.1→rc.2 仅一天）；用户输入为现在升级。

---

## D2 — 28 包 tarball 逐字节 diff 结论（适配面：零适配预期）

**Decision**: 调研覆盖 28 包 = 26 个 catalog dsh 条目 + `dsh-client-store` + `dsh-client-ui-theme`（packageExtensions 与 vendoring 面牵涉）。对每包 `0.2.0-rc.2` tarball 与仓库 `.pnpm` 内 `0.1.7-rc.2` 的 `lib/` 与 `package.json` 做递归 diff：

**逐字节相同（20 个）**：dsh-home-paths、dsh-invariants、dsh-launch-environment、dsh-system-prompt、dsh-agent、dsh-brand、dsh-llm-retry、dsh-persona、dsh-ptc-runtime、dsh-sandbox、dsh-sandbox-policy、dsh-scope、dsh-session-persistence、dsh-session-projection、dsh-settings、dsh-timeout、dsh-tools、dsh-typert-protocol、dsh-user-approval、dsh-client-store。

**变更（8 个）**，全部向后兼容增量：

| 包 | 变更内容 | 对本仓库影响 |
|---|---|---|
| `dsh-session` | `lib/types/repair.d.ts` 重构：新增导出 `ToolCallRecovery` 类（pending 工具结果恢复：`observe(event)` / `results()`，"shared by failed live steps, interrupted logs, and fork seeds"）；`index.d.ts` 增 re-export | additive；无既有消费方（`ToolCallRecovery` 由 agent-loop 内部消费） |
| `dsh-agent-loop` | step driver 失败时经 `ToolCallRecovery` 补记保守恢复 `tool/result` 后再抛出（`lib/index.js` 行为变更 + `tool-calls.d.ts` 文档更新："a terminal scheduler failure rejects after draining; the owning step records conservative recovery results before closing"） | 行为变化（见 D6）；无类型面破坏 |
| `dsh-app-boot` | `OPTIONAL_BUNDLES` 数组 + `@deepseek-ai/dsh-experimental-schedule-bundle`（自动化任务改可选插件包） | 无关本仓库组合（不含 OPTIONAL_BUNDLES 面） |
| `dsh-util-values` | `hasIntrinsicConstructor` 以 `Function.prototype.toString` 交叉对比替代硬编码 `[native code]` 字符串（引擎兼容修复） | 内部修复，自动生效 |
| `dsh-llm` | 仅 `lib/typert.host.js` 协议快照：`MessageSourceMap` + `'user-question-reply'` 源（异步问答模式结算） | 协议元数据；无 `.d.ts` 面变化（`user-question-reply` 仅存在于快照 JSON，全部 tarball `.d.ts` 零命中） |
| `dsh-agent-preset-registry` | 仅 `lib/typert.host.js` 协议快照（同上 + `SessionEventMap` 声明顺序调整，**事件类型集合不变**） | preset 模型稳定；070 成果零改动 |
| `dsh-client-ui-primitives` | 组件符号面 diff 见本节「primitives 组件符号面 diff」表（新增组件 + 可选 props 增量） | 前端向后兼容 |
| `dsh-client-ui-theme` | 见 D4（`design-platform.css` +8 token） | 前端向后兼容 |

**primitives 组件符号面 diff（`dsh-client-ui-primitives` 0.1.7-rc.2 → 0.2.0-rc.2；依据 unpkg `https://unpkg.com/@deepseek-ai/dsh-client-ui-primitives@0.2.0-rc.2/lib/types/index.d.ts` 与仓库已装 0.1.7 类型面逐符号核对）**：

| 符号面 | 0.2.0 实测 | 本仓库影响 |
|---|---|---|
| `Icon*` 集合 | 逐字节相同（尺寸后缀档位沿用） | `projects/game/web/frontend/src/**` 图标引用零改动 |
| `Input` | 转 `forwardRef` | props 兼容，零改动 |
| `Tooltip` / `DisclosureRow` / `TextShimmer` | 可选 prop 增量（`DisclosureRow.running` 语义为整头动画；`TextShimmer.active` 为共享高光） | 仅 ReasoningRow 采用点（D5） |
| `MenuGroup` | 新增组件（消费 `--dsw-alias-menu-group-header-fill`） | 本应用未采用（D7 defer） |
| `Switch` | 消费 `--dsw-alias-switch-thumb` | 本应用未渲染（D7 defer） |
| 既有消费符号（Button/Menu/MenuEntry/MarkdownText/JsonBlock/StateDot 等） | 全部存活、签名兼容 | 零适配；若编译/类型面暴露失配，按 FR-008 最小适配 |

**全部 28 包 `package.json` diff 仅为版本字符串互换**（`0.1.7-rc.2` ↔ `0.2.0-rc.2` 与个别键序）——**零新增 peer 义务、零 exports 变化、devDependencies 集合零变化**（primitives/client-store 双向核对确认）。

**packageExtensions 嵌套 pin 面（本仓库声明，不在 28 包内）**：`@deepseek-ai/dsh-client-ui-primitives` 的 devDependencies 精确 pin `dsh-client-store`/`dsh-util-code-language`/`dsh-util-workspace-path`（`pnpm-workspace.yaml` 的 `packageExtensions` 镜像这三处），升级后镜像值必须同步为 `0.2.0-rc.2`：两处 extension 键的**键**与其**嵌套 `@deepseek-ai/*` 值**都随线更新；只改键会导入旧线副本、破坏版本唯一（SC-001）。这三个包不在 28 包 diff 范围，0.2.0-rc.2 可用性由「遗留验证点」5 实测确认。

**Rationale**: 070 升级需要 7 处源码适配 + preset 重构；本次实测适配面为零——升级工作收敛为声明面切换 + 验证。若实现期实测推翻（发现破坏性变更），按 spec Edge Cases 回退条款处理。

**Sources**: registry tarball（`https://registry.npmjs.org/@deepseek-ai/<pkg>/-/<pkg>-0.2.0-rc.2.tgz`）；本仓库 `node_modules/.pnpm/@deepseek-ai+<pkg>@0.1.7-rc.2*`。

---

## D3 — 声明面变更清单（contracts/declaration-surface.md 的依据）

**Decision**: 变更面枚举（全部为值切换，无集合变化）：

1. **catalog**（`pnpm-workspace.yaml`）：26 个 `@deepseek-ai/dsh-*` 条目 `0.1.7-rc.2` → `0.2.0-rc.2`；互锁条目（cordis 家族/schemastery/node-addon-require-builtin）零改动。
2. **packageExtensions**（`pnpm-workspace.yaml`）：两处键 `@deepseek-ai/dsh-client-ui-primitives@0.1.7-rc.2` / `@deepseek-ai/dsh-client-store@0.1.7-rc.2` → `@0.2.0-rc.2`，且 primitives 键内三处嵌套 `@deepseek-ai/*` 值（client-store、util-code-language、util-workspace-path）同步 `0.2.0-rc.2`；键集合与其余依赖集合不变——D2 实测两包 devDeps 键集合零变化。
3. **底座**（`third_party/dsh/core`）：`package.json` 5 个 `dsh-*` 条目切 `0.2.0-rc.2`、互锁 6 条值不变（11 包闭包）；`BUILD.bazel` npm_deps（gazelle 刷新）与 `runtime_pkg` 闭包注释随线；`version.ts` 快照标识 → `dsh-core@0.2.0-rc.2`（注释中 cordis 互锁值描述与 0.2.0 线事实一致，仅版本串与依据引用更新——改引 research D1）。
4. **消费 package.json**：agent_v2 / demo / preset-authoring / web frontend 的 dsh 依赖集合**零变化**（`catalog:` 协议下经 catalog 切值自动生效；spec FR-005）。
5. **lockfile**：`bazel run @pnpm -- --dir /mnt/code/dominion up` 官方流程刷新（不手改）；刷新后验证全部 `@deepseek-ai/*` 解析 `0.2.0-rc.2`、cordis 唯一、`node-addon-require-builtin@0.1.6` 的 `linux-x64-gnu` optionalDep 在场。

**Rationale**: 070 T001–T006 的声明面流程直接复用（`specs/070-dsh-upgrade/tasks.md` Phase 1）；closure audit 断言①②③ 的 expected set 数据驱动推导（070 D12），随 package.json 版本自动重算。

**Alternatives considered**: 无——流程既定。

---

## D4 — vendored theme 重取与 FR-022 闭合（FR-022 定义：`specs/054-agent-v2-bugfixes/spec.md`）

**Decision**: `@deepseek-ai/dsh-client-ui-theme@0.2.0-rc.2` 的 8 张 sheet（region 锚点提取自 `lib/client.js`，流程见 `projects/game/web/frontend/src/dsh-theme/README.md`）**名称与 STYLES 顺序均不变**：base → corner-shape → design-platform → focus → onboarding → scrollbar → gradient-shadow-text → shiki。逐字节比对：7 张相同，**仅 `design-platform.css` 变化**（17732B → 19081B）。

**新增 8 个 token（明暗双份，零删除、零既有值变更）**：

| token | 上游对应改进 | 本仓库消费方 |
|---|---|---|
| `--dsw-alias-label-shimmer` | 文字流式 shimmer 动画 | **本特性采用**（ReasoningRow，D5）+ primitives `TextShimmer.module.css` |
| `--dsw-alias-menu-group-header-fill` | 菜单分组粘性标题 | primitives `MenuGroup.module.css`（新组件，本应用未采用） |
| `--dsw-alias-switch-thumb` | 深色主题开关对比度 | primitives `Switch.module.css`（本应用未渲染 Switch） |
| `--dsw-alias-label-deep-diving(-shimmer)` | 深度思考标签色 | app 层（web 客户端），本应用无承载 |
| `--dsw-alias-bg-document-selection` | Office/PDF 预览文字选区 | app 层，本应用无承载 |
| `--dsw-alias-turn-trigger-bg(-hover)` | 轮次跳转按钮 | app 层，本应用无承载 |

**FR-022 闭合矩阵**（`projects/game/web/frontend/src/dsh-theme.test.ts`）：primitives 0.2.0 新消费 3 个 token（`label-shimmer` ← TextShimmer.module.css、`menu-group-header-fill` ← MenuGroup.module.css、`switch-thumb` ← Switch.module.css）——primitives 升级而 theme 未重取时断言**必然失败**（升级信号，设计使然）；重取后三者在 `design-platform.css` 新增定义中闭合；既有 3 项豁免（`--dsw-hovercard-bg`、`--dsw-alias-bg-layer-4`、`--dsw-alias-label-error`）继续有效，豁免集合不得扩大（spec FR-009）。`index.css` 的 `@import` 顺序不变。

**Rationale**: 与 070 D9 的 vendoring 方法完全一致（npm tarball 为唯一权威，GitHub master css 已漂移不可替代）。

---

## D5 — shimmer 采用设计（ReasoningRow 单点）

**Decision**: 设计跟进采用点收敛为 `projects/game/web/frontend/src/components/ReasoningRow.tsx` 单组件（含其作为 ChatView THINK 尾块渲染器的场景）：

1. **折叠摘要 shimmer**：`.reasoning-summary-text` 内容（running 时为 `latestLine(text)`）以 `<TextShimmer active={running}>` 包裹——0.2.0 TextShimmer 契约：`children` 纯文字/presentational、`active` 控制动画（inactive 文字保持同一节点可选中）、动画为共享高光（`--dsw-alias-label-shimmer` 由 vendored sheets 定义）。既有 `data-follow-end` 纯 CSS 跟尾机制保留（滚动语义与动画语义正交）。
2. **DisclosureRow 头部动画**：向 DisclosureRow 传 `running={running}`（prop 在 0.1.7 已存在但未使用；0.2.0 语义升级为"animate the complete header"，CSS 随包演进）——一行采用，头部与摘要动画同源。
3. **TEXT 尾块不采用**（实测依据）：ChatView 的 TEXT 尾块渲染 MarkdownText 流式正文（`projects/game/web/frontend/src/components/ChatView.tsx` AgentStep），markdown 内容不符合 TextShimmer 契约（"Keep icons outside"、children 为 presentational）；其流式语义已由 MarkdownText `streaming` 表达，叠加 shimmer 属重复表达——按用户裁定（无承载面不引入）defer。
4. **无障碍守恒**：既有 `{running && <span className="visually-hidden">正在生成</span>}` 保留——进行中语义不得仅由动画承载（spec Edge Cases）；shimmer 装饰层不影响文字可选性与布局稳定性。
5. **测试**：`ReasoningRow.test.tsx` 增补 shimmer 断言（running 态渲染动画元素/active 语义、完结态 active=false 文字节点保留、visually-hidden 语义在场）。

**Rationale**: 用户裁定"仅应用目前 game/web 有对应承载的功能"（spec Clarifications）；ReasoningRow 是唯一同时具备（a）纯文字运行态元素、（b）DisclosureRow 头部、（c）shimmer token 消费闭环的组件。

**Alternatives considered**: MarkdownText 正文流加 shimmer——违反组件契约且重复表达；SessionList 菜单分组（MenuGroup）——菜单仅 2–3 项/条目，无分组承载需求；Tooltip `openOnClick`——本应用未消费 Tooltip。全部 defer（D7）。

---

## D6 — 上游行为变化：工具调度失败恢复（免费健壮性 + 断言影响面）

**Decision**: 0.1.7 → 0.2.0 唯一框架行为变化：已提交 assistant 消息的工具请求在调度失败时，0.1.7 留下未配对 `tool/call`（对要求配对的 provider 后续请求非法、会话卡死）；0.2.0 由 AgentLoop step driver 在关闭失败 step 前经 `ToolCallRecovery` 补记保守恢复结果（有 `tool/call` 无结果 → `TOOL_OUTCOME_UNKNOWN`，文本警告不可盲目重试可能已有副作用的操作；无 call 记录 → `TOOL_NOT_STARTED`），然后传播原始失败。

- **自动获得**：agent_v2 长工具流、team 编排、saolei-loop 的健壮性增强，无需代码。
- **断言影响面**：若 `common/js/dsh-plugins/team`/`saolei-loop` 或 agent_v2 单测对失败路径事件序列有精确断言，0.2.0 会补记 `tool/result` 事件使断言失配——按新事实重写断言（重写≠放松，070 D8 先例）；spec FR-006/FR-007 约束语义守恒 + 新行为有测试覆盖。
- **Mongo preset 零迁移**：`dsh-agent-preset-registry` 仅协议快照变化，070 preset-authoring 重构成果（register/disposer 物化、templates 快照）与 `PresetRecord` schema 完全不动。

**Sources**: https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/.agents/notes/implemented/bug-fix/2026-09-19-failed-step-tool-results.md （决策全文）；tarball `dsh-agent-loop/lib/index.js` diff（step driver catch 块补记 `tool/result` 的实现）。

---

## D7 — defer 清单（无承载面新能力，FR-013 要求记录）

| 候选 | 不采用理由 | 再评估条件 |
|---|---|---|
| `MenuGroup` + `observeStickyMenuGroups` 菜单分组 | SessionList 菜单仅 2–3 项/条目，无分组承载 | 菜单项增长到需要分组时 |
| `Tooltip` `openOnClick`/`focusDelayMs` | 本应用未消费 Tooltip | 引入 Tooltip 消费时 |
| `--dsw-alias-turn-trigger-*` 轮次跳转 | 本应用无轮次导航 UI | 前端引入轮次导航时 |
| `--dsw-alias-switch-thumb` / 文档选区 / deep-diving 标签 | 对应承载面（Switch/文档预览/深潜标签）本应用未渲染 | 渲染对应组件时 |
| MarkdownText 正文流 shimmer | 违反 TextShimmer 组件契约（markdown 内容），且 streaming 已表达 | 上游提供 markdown 级 shimmer 原语时 |
| 异步问答模式（`user-question-reply`） | 仅 typert 协议快照 + app 层实现，框架包无 API 面 | 上游在框架包暴露 API 且有产品需求时 |

**Rationale**: 070 D10 先例——复用决策显式记录避免重复调研。

---

## D8 — 交付顺序建议（给 tasks 规划的输入）

**Decision**: 建议 phase 划分（每 phase 可独立验证；070 D14 的收缩版）：

1. **P1 catalog/lockfile/底座**：catalog 26 条切值 + packageExtensions 2 键 + `pnpm up` + gazelle + `bazel mod tidy` + core 三面（package.json/BUILD/version.ts）+ lockfile 验证。门禁：`bazel build //third_party/dsh/core:runtime_pkg` + 断言①；lockfile 全 `@deepseek-ai/*` 同线。
2. **P2 服务与生态回归**：agent_v2/demo/dsh-plugins 构建 + 全量单测（预期零适配；失配断言按 D6 重写）+ closure audit ②③。门禁：`bazel test //projects/game/agent_v2/... //experimental/dsh/demo/... //common/js/dsh-plugins/... --nocache_test_results` 全绿。
3. **P3 前端资产对齐**：ui-primitives 升级（经 catalog，P1 已切）+ theme 重取（D4）+ README 版本记录 + tsc 手动核验。门禁：前端 build + vitest + FR-022 通过。
4. **P4 shimmer 采用**：ReasoningRow 改造 + 测试增补（D5）。门禁：`bazel test //projects/game/web/frontend/...` 全绿。
5. **P5 大型测试验收**：`system_test.yaml`（agent_v2 四拓扑）+ `interface_test.yaml`（demo）实际执行全通过（宪法 VI）。
6. **P6 残留清查**：全仓 grep `0.1.7` 版本串（全量判据，`0.1.7-rc.2` 为其子集；SC-006；豁免：`specs/` 历史文档与本特性交付文档中的版本线对比表述）。

**Rationale**: 依赖顺序（声明面→验证→前端→验收）；P3/P4 可并行（不同文件）；每 phase 有 bazel/test 门禁。

---

## 遗留验证点（实现期门禁，非阻塞）

1. P1 后 `pnpm-lock.yaml` 内 `@deepseek-ai/*` 全部 `0.2.0-rc.2`、无 `0.1.7-rc.2` 残留、cordis 唯一物理副本（quickstart 场景 1 命令）。
2. P2 单测若出现失败路径断言失配，确认失配源于 D6 行为（补记 `tool/result`）而非其他破坏，再按新事实重写。
3. P3 前端 `tsc --noEmit` 零错误（bazel 门禁不含类型检查，070 D9 门禁先例）。
4. P4 shimmer 组件测试在沙箱 vitest 下全绿（jsdom 无动画帧环境，断言以 DOM/属性语义为准，不断言动画帧）。
5. packageExtensions 嵌套 pin 可用性：`npm pack @deepseek-ai/{dsh-client-store,dsh-util-code-language,dsh-util-workspace-path}@0.2.0-rc.2` 成功（若某包未随 0.2.0 线发布，按 spec Edge Cases 回退条款评估）。
6. SC-006 残留清查覆盖 README 与源码注释中的版本串/版本化 URL（quickstart 场景 6 命令含 `--include="*.md"`）。
