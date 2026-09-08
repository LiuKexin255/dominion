# Implementation Plan: 057 agent-v2 web/desktop 界面修复二期

**Branch**: `057-agent-v2-ui-fixes-2` | **Date**: 2026-09-06 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/057-agent-v2-ui-fixes-2/spec.md`

## Summary

修复 055 交付后的 3 个遗留缺陷：(1) **重建同步**——空闲 agent 经 UpdateAgent 重建后服务端历史已清空，但 web 的 `onApplied` 不触达 ChatStore，旧对话停留直至手动刷新页面；修复为提取挂载回填为共享 `runBackfill`（epoch 守卫 `sentSinceBackfill` 复位 + `listHistory` → `loadHistory` 全量重建），应用成功后调用，空闲/忙时 ABORTED/首次物化三路径收敛同一干净终态（research D1，data-model §1.3 收敛矩阵）。(2) **长名悬停自动滚动**——悬停切换 `scrollable` 类已存在，但滚动条隐藏 + 浏览器不为仅横向可滚动容器重定向垂直滚轮，滚动实际不可达；修复为组件内定时器步进 `scrollLeft`（延迟 250ms、3px/30ms ≈100px/s、单程到尾 hold、移出复位），短名与 `prefers-reduced-motion: reduce` 不启动（research D2~D4，spec Q1 裁定的 marquee 模型）。(3) **desktop 手动刷新**——sessions-toolbar 右侧新增 `Refresh` 文本按钮（`disabled={loading}` 幂等），复用既有 `handleRefresh`（只写 sessions/loading/error 三态），并作为 051 退化清单"移除刷新"条目的显式修订登记（research D5，contracts §3.4）。无服务端/proto/依赖变更；测试沿用 vitest + jsdom（stub 滚动几何参照上游 attachment-rail 模式 + fake timers；desktop 对齐 055 App.test 模式）。决策与根因复核见 [research.md](./research.md)，交互契约见 [contracts/ui-interactions.md](./contracts/ui-interactions.md)。

## Technical Context

**Language/Version**: TypeScript（web: React 18 + vite；desktop: Svelte 5 + vite）

**Primary Dependencies**: 无新增。web 复用 `@deepseek-ai/dsh-client-ui-primitives` 既有引入（本 feature 不新增 primitives 用量）；测试复用 vitest + jsdom（desktop jsdom 已于 055 引入）

**Storage**: 无持久化变更（纯前端行为修复；数据形状不变，见 [data-model.md](./data-model.md)）

**Testing**: vitest：`bazel test //projects/game/web/frontend:lib_test //projects/game/desktop/frontend:lib_test`；marquee 断言 = jsdom stub 滚动几何（`scrollWidth`/`clientWidth`/`scrollLeft` defineProperty，模式参照 [deepseek-harness attachment-rail.client.spec.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-attachment/tests/attachment-rail.client.spec.tsx)）+ `vi.useFakeTimers()`；重建同步断言 = mock api 层驱动 `onApplied`；desktop 断言 = 055 "App sessions refresh" 同套；真实滚动可达性与重建体验以部署环境人工验证记录闭合（spec A3，对齐 055 A5）

**Target Platform**: web（浏览器，深色单一主题）+ desktop（wails 桌面端）

**Project Type**: 既有 web/desktop 前端缺陷修复（无新服务、无接口变更——contracts §4 零改动面登记）

**Performance Goals**: marquee 为悬停期单元素短生命周期定时器（30ms 步进），无逐帧布局读扩散；`runBackfill` 为事件驱动单次请求，无轮询

**Constraints**: 051/054/055 交付行为零回归（`.session-name.scrollable` 类切换与 mouseleave 复位断言、ChatStore 归约不变式、desktop handleRefresh 三态写集）；交互参数以 contracts §1 登记值为终态基线（250ms/3px/30ms/hold）

**Scale/Scope**: 变更面——web：`src/App.tsx`（runBackfill 提取 + onApplied 追加）、`src/components/SessionList.tsx`（marquee 定时器 + title 静态途径）、对应 `App.test.tsx` / `SessionList.test.tsx` 扩展、`src/components/AgentSettingsPanel.test.tsx`（"App 未物化引导"用例 messages mock 按物化状态区分 404 / 200 空集合——测试 fixture 修正，非生产行为变更，[contracts/ui-interactions.md](./contracts/ui-interactions.md) §2.7/§4）；desktop：`src/App.svelte`（toolbar 按钮）、`src/App.test.ts` 扩展

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 检查 | 结论 |
| --- | --- | --- |
| I. 引用溯源 | research/data-model/contracts 均带仓库相对路径或完整 URL（源码定位、051/049 契约引用、上游测试模式与 w3c/SO 外部事实链接） | ✅ |
| II. 重构式变更 | US1 以"提取共享 runBackfill"收敛两处回填为单一函数（消除挂载/应用两路径分叉），非在 onApplied 打补丁式清空；US2 延伸既有 scrollLeft 复位链路而非另起 CSS 双副本机制 | ✅ |
| III. 接口优先设计 | 无服务接口变更；UI 交互契约先行（contracts/ui-interactions.md §1~§3，含测试口径与参数基线） | ✅ |
| IV. 测试颗粒度 | 编译+单测随代码变更执行（quickstart §2）；无大型测试面变更（054 suites 回归，spec A3） | ✅ |
| V. 编码前阅读文档 | tasks 阶段按 phase 声明三分类文档清单；本 plan 引用的文档均已实际阅读核验（research §1 事实链逐条源码确认） | ✅（tasks 落实） |
| VI. 大型测试验收 | 前端 UI 缺陷修复、不改服务行为；A3 声明以组件级测试 + 人工验证记录为主、054 既有 suites 为回归面 | ✅ |
| VII. 终态表述 | 交付物只表述终态（参数基线、收敛矩阵、契约修订登记为当前状态对照）；被否决方案（滚轮驱动/CSS 双副本/本地直接清空）仅在 research 记录必要理由 | ✅ |

Phase 1 后复查：设计产物（research/data-model/contracts/quickstart）与上述判定无冲突——无新依赖、无接口变更、无架构调整，通过。

## Project Structure

### Documentation (this feature)

```text
specs/057-agent-v2-ui-fixes-2/
├── plan.md              # 本文件
├── research.md          # Phase 0：根因复核 + 决策 D1~D6 + 影响面确认
├── data-model.md        # Phase 1：回填 epoch/收敛矩阵 + marquee 状态机 + 刷新状态复用
├── quickstart.md        # Phase 1：验证指南（自动化 + 人工场景）
├── contracts/
│   └── ui-interactions.md  # Phase 1：§1 长名自动滚动 / §2 重建同步 / §3 手动刷新（含 051 契约修订登记）/ §4 零改动面
└── tasks.md             # Phase 2 输出（/speckit.tasks 生成）
```

### Source Code (repository root)

```text
projects/game/web/frontend/
└── src/
    ├── App.tsx                      # ChatPanel：提取 runBackfill（挂载 effect 复用），
    │                                 # onApplied 追加 void runBackfill()（FR-001，research D1）
    ├── App.test.tsx                 # 重建同步断言（触发/让位/失败/应用失败/忙时收敛/首次物化，contracts §2.7）
    ├── components/
    │   ├── AgentSettingsPanel.test.tsx  # 既有"App 未物化引导"用例 messages mock
    │   │                                # 按物化状态区分 404 / 200 空集合
    │   │                                # （契约 §2.7/§4，fixture 修正）
    │   ├── SessionList.tsx          # marquee：悬停定时器步进 scrollLeft + title 静态途径（FR-002，research D2~D4，契约 §1）
    │   └── SessionList.test.tsx     # stub 几何 + fake timers 断言（contracts §1.7）；
    │                                 # 既有 hover/复位断言零回归
    └── theme.css                    # 零改动（scrollable 类既有承载；如实现需要微调仅限注释同步）

projects/game/desktop/frontend/
└── src/
    ├── App.svelte                   # sessions-toolbar 右侧 Refresh 按钮（FR-003，research D5）
    └── App.test.ts                  # 手动刷新断言（触发/失败/禁用/既有语义回归，contracts §3.5）
```

**Structure Decision**: 全部为既有目录内变更，无新模块/包/依赖；web 变更收敛于 App.tsx 与 SessionList.tsx 两文件及其测试，另含一处既有测试 fixture 对齐（`AgentSettingsPanel.test.tsx` messages mock 按服务端契约区分物化前后，契约 §2.7/§4）；desktop 收敛于 App.svelte 及其测试（055 交付的 jsdom 测试基建同套复用）。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规，无需论证。
