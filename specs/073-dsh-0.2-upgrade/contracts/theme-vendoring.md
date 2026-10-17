# Contract: vendored theme 重取与 FR-022 消费覆盖（FR-022 定义：`specs/054-agent-v2-bugfixes/spec.md`）

**Feature**: specs/073-dsh-0.2-upgrade/spec.md（FR-009、US3 场景 2/5）
**依据**: specs/073-dsh-0.2-upgrade/research.md D4

## 1. 重取契约（`projects/game/web/frontend/src/dsh-theme/`）

**来源**：`@deepseek-ai/dsh-client-ui-theme@0.2.0-rc.2` npm tarball 的 `lib/client.js`（**唯一权威**——GitHub master 的 css 已漂移，`src/dsh-theme/README.md`「获取源实态」）。npm tarball 不含独立 css 文件，sheet 以字符串常量内嵌于 region 锚点：

```text
//#region \0dsh-inline-css:.../src/styles/<name>.css.mjs
var <ident> = "<json-escaped css">
```

**步骤**：

1. `npm pack @deepseek-ai/dsh-client-ui-theme@0.2.0-rc.2` 并解包；
2. 按 region 锚点提取全部 8 张 sheet（JSON 反转义后按字节落盘），覆盖本目录同名 css；
3. 核对 STYLES 顺序（0.2.0-rc.2 与 0.1.7-rc.2 相同：base → corner-shape → design-platform → focus → onboarding → scrollbar → gradient-shadow-text → shiki），`index.css` 无需变化；
4. `README.md` 版本记录更新为 0.2.0-rc.2。

**预期 diff**：7 张 sheet 逐字节不变；仅 `design-platform.css` 变化（新增 8 个 token 的定义，明暗双份；零删除、零既有值变更）。若实测出现其他 sheet 变化或 token 删除，停下核对 tarball（可能取错版本）。

## 2. 新增 token 清单（`design-platform.css`）

| token | light | dark | 消费方（primitives css） | 本应用采用 |
|---|---|---|---|---|
| `--dsw-alias-label-shimmer` | `color-mix(neutral-1000 30%, transparent)` | `color-mix(neutral-00 45%, transparent)` | `TextShimmer.module.css` | ✅（shimmer 采用契约） |
| `--dsw-alias-menu-group-header-fill` | `#f8f9faf0` | `#303136f0` | `MenuGroup.module.css` | 否（defer） |
| `--dsw-alias-switch-thumb` | `neutral-bluish-00` | `neutral-bluish-400` | `Switch.module.css` | 否（defer） |
| `--dsw-alias-label-deep-diving` | `deepseek-500 70% + blue-950` | `deepseek-450 55% + neutral-bluish-400` | app 层 | 否（defer） |
| `--dsw-alias-label-deep-diving-shimmer` | `deepseek-500 30% + blue-950` | `blue-300 65% + deepseek-400` | app 层 | 否（defer） |
| `--dsw-alias-bg-document-selection` | `blue-500 40% + transparent` | 同 light | app 层 | 否（defer） |
| `--dsw-alias-turn-trigger-bg` | `= markdown-code-block` | `= interactive-bg-hover` | app 层 | 否（defer） |
| `--dsw-alias-turn-trigger-bg-hover` | `= interactive-bg-hover` | `= interactive-bg-active` | app 层 | 否（defer） |

## 3. FR-022 断言语义（`src/dsh-theme.test.ts`，测试不改）

断言不变式：`consumed(primitives lib css ∪ theme.css) ⊆ defined(vendored sheets) ∪ exempt`。

- **升级信号路径**：primitives 0.2.0 新消费 `--dsw-alias-label-shimmer`、`--dsw-alias-menu-group-header-fill`、`--dsw-alias-switch-thumb` 三 token —— theme 未重取时断言失败（失败信息指向 `src/dsh-theme/README.md` 人工升级流程）；重取后三者在 `design-platform.css` 定义中闭合，断言通过。
- **豁免集合冻结**：`--dsw-hovercard-bg`、`--dsw-alias-bg-layer-4`、`--dsw-alias-label-error` 三项继续有效（均为本应用未渲染承载面的消费项）；**不得**以扩大豁免替代重取（spec FR-009）。
- shimmer 采用后自有 `theme.css` 若引用 `--dsw-alias-label-shimmer`，同样由 sheets 定义闭合（该 token 已在 §2 清单内）。

## 4. 交付物一致性

`src/dsh-theme/README.md` 的「vendored 版本」记录 = catalog 的 ui-primitives 条目版本 = `0.2.0-rc.2`（SC-006 残留检查覆盖此文件）。
