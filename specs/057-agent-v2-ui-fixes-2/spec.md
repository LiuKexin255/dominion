# Feature Specification: agent-v2 web/desktop 界面修复二期（055 交付后遗留缺陷）

**Feature Branch**: `057-agent-v2-ui-fixes-2`

**Created**: 2026-09-06

**Status**: Draft

**Input**: User description: "继 @specs/055-agent-v2-ui-fixes/ 对 agent-v2 和web 进行修复
1. web session 列表长 id 当前已经渐隐，但鼠标放上去不会左右滚动。
2. 为 desktop session 列表页增加一个刷新按钮
3. 空闲中的 agent 如果使用 UpdateAgent 重建，agent 对话内容不会立刻刷新，而是需要刷新页面。"

## Motivation

`specs/055-agent-v2-ui-fixes/` 交付后，用户在实际环境使用中发现 3 个遗留缺陷（2026-09-06）：1 个 web 状态一致性缺陷（agent 重建后对话视图停留在已不存在的旧内容上）、1 个 web 可用性缺陷（session 列表长 id 悬停后无法滚动阅读）、1 个 desktop 便利性缺口（列表页无手动刷新入口）。本 feature 修复全部缺陷。

缺陷定位（调研结论，作为修复背景）：

- **UpdateAgent 重建后对话不刷新**：UpdateAgent 的服务端语义是清理重建——旧 agent 被 dispose（在途回合收 `turn_end{ABORTED}`、排队作废），**history 随旧 agent 一起清空**，新 agent 以全新空历史物化（`projects/game/agent_v2/src/session.ts` 的 materialize：`const history = new SessionHistory()`；契约 `specs/051-agent-v2-dsh-migration/contracts/agent-api.md` §2.1"无论配置是否变化都执行清理重建"）。web 侧应用成功回调 `onApplied`（`projects/game/web/frontend/src/App.tsx`）仅更新 agent 配置状态与关闭面板，**不重新回填对话历史**。空闲 agent（无在途 Send 流）没有任何事件通知客户端——store 里的旧对话持续呈现，与服务端空历史不一致；此后发送的新消息还会追加在旧内容之后，进一步加深误导。当前唯一恢复途径是刷新页面（重挂载触发 ListAgentMessages 回填）。忙时 agent 因在途流的 `turn_end{ABORTED}` 归约清空（`projects/game/web/frontend/src/store/chat.ts`）而自愈，空闲时则完全无同步路径——即用户报告的场景。
- **web session 列表长 id 悬停不滚动**：长名条目已实现"未悬停右侧渐隐遮罩 + 悬停切换 `scrollable` 类"（`projects/game/web/frontend/src/components/SessionList.tsx` 悬停态切换 + `projects/game/web/frontend/src/theme.css` `.session-name` / `.session-name.scrollable`，051 契约 web-frontend.md §1 FR-004）。悬停态确实把容器置为 `overflow-x: auto`，但**滚动条被刻意隐藏**（`scrollbar-width: none` + `::-webkit-scrollbar { display: none }`），而主流浏览器对"仅横向可滚动容器"默认**不把垂直滚轮映射为横向滚动**（wheel 默认动作在该容器上无效果；Firefox 的 auto-dir 实验特性默认关闭，见 [w3c/csswg-drafts#4380](https://github.com/w3c/csswg-drafts/issues/4380) 与 [Stack Overflow: Propagate wheel event from a scrollable container](https://stackoverflow.com/questions/68677344/propagate-wheel-event-from-a-scrollable-container-to-the-window)）——普通鼠标用户悬停后既看不到滚动条、滚轮也无动作，"横向滚动可看全名"的契约意图实际不可达。精确根因确认（含目标浏览器实际行为复核）以 plan 阶段源码事实链 + 外部权威引用为准（research §1.2），真实浏览器行为以部署环境人工验证记录闭合，spec 只约束可观测结果。
- **desktop sessions 列表页无刷新按钮**：051 迁移将 desktop 定位为只读控制面并移除了列表管理操作（含刷新，`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §5 退化清单）；055 FR-007 补回了"返回列表自动刷新"。当前 sessions 页工具栏仅有模板标识（`projects/game/desktop/frontend/src/App.svelte` 的 sessions-toolbar），用户**停留在列表页期间**外部变更（web 侧新建/删除）无法主动获取，只能靠退出重进。本 feature 恢复一个手动刷新入口（仅刷新，不恢复新建/删除——对 051 契约的显式修订，见 Assumptions A4）。

## Clarifications

### Session 2026-09-06

- Q: web session 长名悬停揭示的交互模型（自动滚动 vs 滚轮驱动）？ → A: **悬停自动滚动揭示（marquee 式）**：鼠标悬停即自动逐步滚动展示全名，移出复位；不引入"悬停 + 滚轮驱动横向滚动"的约定（可发现性弱，2026-09-06 用户裁定，选项 A）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - agent 重建后对话视图即时同步 (Priority: P1) 🎯 MVP

用户在 web 会话内通过 agent 设置面板重新应用（UpdateAgent 物化/刷新）成功后，当前会话的对话视图**立即反映重建后 agent 的真实状态**：旧对话内容、排队指示、错误/终止标识一并清除，呈现重建后的干净对话面——与面板提示的"清空短期记忆"语义一致，无需刷新页面或重新进入会话。重建后立即发送的新消息正常进入新对话，不被同步过程覆盖或干扰。

**Why this priority**: 状态一致性缺陷：重建是 051 交付的正式能力（刷新语义），但空闲 agent 重建后 UI 持续呈现服务端已不存在的旧对话——用户会误以为短期记忆仍在（与刷新语义直接矛盾），此后新消息还追加在旧内容之后，误导持续加深。对话视图是 web 的主 surface，"所见即服务端状态"是最基本的信任基础；另两项缺陷均为便利性/可读性问题。

**Independent Test**: 组件级验证（驱动面板应用成功回调）：断言 (a) 应用成功后重新拉取历史并重建对话状态（旧内容清空、queue/live/error/canceled 全部复位）；(b) 同步与紧随其后的发送兼容（新消息不被同步回填覆盖）；(c) 同步失败呈现既有回填错误语义、不清空为空白；忙时 agent（在途回合被 ABORTED）与首次物化两条路径收敛到同一终态。

**Acceptance Scenarios**:

1. **Given** 一个已物化、空闲（无在途回合）且有多轮历史对话的 agent，**When** 用户在设置面板再次应用（重建），**Then** 应用成功后对话视图立即清空为干净对话面（旧历史不残留），无需刷新页面。
2. **Given** 应用成功后，**When** 用户查看会话状态区，**Then** 物化状态/模型标识与重建后配置一致（既有 onApplied 语义零回归）。
3. **Given** 应用成功后，**When** 用户立即发送新消息，**Then** 新消息进入新对话并正常流式输出，不被同步过程覆盖、丢失或重复。
4. **Given** 应用时该 agent 正有回合在输出（忙时），**When** 重建落地（在途回合 ABORTED + 应用成功），**Then** 对话视图收敛到与场景 1 相同的干净终态，无重复/冲突中间态残留。
5. **Given** 首次物化（未物化 → 物化），**When** 应用成功，**Then** 引导态按既有语义消退，对话面为空，无异常状态。
6. **Given** 应用成功但随后的历史同步请求失败，**When** 失败发生，**Then** 按既有回填错误语义呈现提示（可重试），视图不静默停留在误导态。
7. **Given** 重建前 store 中存在排队指示、"已终止"标识或错误提示，**When** 应用成功同步完成，**Then** 上述残留全部清除。

---

### User Story 2 - web session 长名悬停可读全名 (Priority: P2)

web 侧栏 session 列表中，名称超宽（长 id）的条目：未悬停时保持右侧渐隐遮罩（现状零回归）；**悬停时内容自动左右滚动揭示完整名称**（marquee 式：悬停即滚，无需任何额外操作——2026-09-06 澄清 Q1 裁定），移出悬停后滚动位置复位（scrollLeft 归零）。悬停滚动只发生在条目名称容器内，不引起列表或页面其他滚动副作用。

**Why this priority**: session id 是用户识别/对照会话的唯一标识（扫雷链路中 web 与 desktop 需按 id 对照）；当前长 id 尾部完全不可读，且"悬停滚动"契约意图（051 FR-004）在普通鼠标下实际不可用。修复面小、独立可交付；但相对 US1 的状态一致性缺陷，属可读性便利问题。

**Independent Test**: 组件级验证（stub 滚动几何，参照上游 dsh 对 scrollWidth/clientWidth/scrollLeft 的 stub 模式 [deepseek-harness attachment-rail.client.spec.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-attachment/tests/attachment-rail.client.spec.tsx)）：断言 (a) 悬停触发自动滚动揭示（滚动启动/推进，scrollLeft 变化）；(b) 移出后 scrollLeft 复位为 0；(c) 短名条目悬停无副作用；(d) 未悬停渐隐遮罩样式零回归。滚动可达性（真实浏览器几何）以部署环境人工验证记录闭合。

**Acceptance Scenarios**:

1. **Given** 一个名称超宽的 session 条目，**When** 用户悬停于该条目，**Then** 内容自动滚动逐步揭示，完整名称可读（无需滚轮/拖动等额外操作）。
2. **Given** 条目处于悬停滚动中，**When** 用户移出指针，**Then** 滚动位置复位，条目回到渐隐默认态。
3. **Given** 名称不超宽的条目，**When** 悬停，**Then** 无滚动副作用（无布局跳动、无遮罩闪烁）。
4. **Given** 用户在长名条目上操作滚动，**When** 滚动发生，**Then** 滚动仅限于名称容器内，列表垂直滚动与页面滚动不受劫持。
5. **Given** 快速移动指针掠过多个长名条目，**When** 停留在任一条目，**Then** 各条目滚动状态无残留错位（复位正确）。

---

### User Story 3 - desktop sessions 列表页手动刷新 (Priority: P3)

desktop sessions 列表页提供**显式刷新按钮**：点击即重新拉取当前模板的会话列表，外部（web 侧等）新建/删除的会话无需退出重进即可反映。刷新中的加载指示、失败错误呈现与既有刷新时机（进入列表、模板切换）语义一致；失败不清空已呈现列表；刷新不干扰导航与选择。

**Why this priority**: 便利性补充：055 已交付"返回列表自动刷新"覆盖了主要路径（进入列表即新鲜）；手动按钮覆盖"停留在列表页期间外部变更"的窄场景。修复独立、面小。

**Independent Test**: 组件级验证（对齐 055 交付的 desktop 刷新测试模式 `projects/game/desktop/frontend/src/App.test.ts` 的"App sessions refresh"块）：断言 (a) 列表页存在刷新入口且点击触发重新拉取；(b) 刷新失败呈现错误且不清空既有列表；(c) 既有 onMount/返回导航/模板切换刷新语义零回归；(d) 刷新进行中重复交互不产生叠加错误状态。

**Acceptance Scenarios**:

1. **Given** 用户位于 sessions 列表页，**When** 外部新建/删除了会话后用户点击刷新按钮，**Then** 列表反映最新会话集合。
2. **Given** 刷新请求失败，**When** 失败发生，**Then** 按既有错误语义呈现（错误提示 + 日志），已呈现列表不被清空。
3. **Given** 刷新进行中，**When** 用户点击某会话进入，**Then** 选择与进入流程不受干扰（对齐 055 FR-007 并发语义）。
4. **Given** 应用启动加载、返回列表、模板切换三个既有刷新时机，**When** 触发，**Then** 行为与 055 交付一致（零回归）。
5. **Given** 刷新进行中，**When** 用户重复点击刷新入口，**Then** 不叠加产生错误状态（加载态禁用或幂等，由 plan 决策）。

---

### Edge Cases

- **重建同步与并发发送竞态**：应用成功后同步请求在途时用户即发送新消息——新回合（属于新 agent）不得被同步回填覆盖；复用既有"回填让位于发送"的守卫语义，方向与既有回填一致。
- **同步请求与 ABORTED 流事件并发**（忙时重建）：`turn_end{ABORTED}` 的 store 清空与应用触发的同步回填并发落地，最终收敛到同一空态，无重复/冲突中间态。
- **重建同步失败后的重试路径**：同步失败不清空对话（保持可重试），用户重新进入会话或再次应用即恢复一致。
- **多标签页/外部 API 触发的重建**：非发起应用的客户端不获推送通知，停留在旧视图直至其自身回填（重进/刷新）——既有已知多标签页限制的延续（`specs/049-agent-v2-dsh-init/research.md` 已知限制节），不纳入本 feature。
- **悬停揭示的可达性**：条目本身仍是可聚焦按钮（既有键盘可达零回归）；悬停揭示是增强而非唯一阅读途径（是否补充 title 等静态途径由 plan 决策）。
- **悬停期间列表数据刷新**（如侧栏刷新使条目集合变化）：悬停滚动状态随条目重挂载自然复位，无滚动错位残留。
- **自动滚动到端点后的行为**：滚动到达尾部后的停留/往返/回绕节奏由 plan 决策约束，spec 只要求"全名可读"（尾部内容到达可读位置）且不产生布局抖动；自动滚动 MUST NOT 干扰条目既有交互（选择、`···` 菜单）。
- **desktop 刷新与模板切换并发**：同时发生时以最后一次操作的目标模板列表为准，不重复叠加产生错误状态（对齐 055 Edge Cases 既有语义）。
- **desktop 刷新进行中的加载呈现**：不阻塞页面交互（既有 loading 语义），列表加载态呈现与既有时机一致。

## Requirements *(mandatory)*

### Functional Requirements

#### 重建后对话同步（用户问题 3）

- **FR-001**: web 会话内 agent 设置面板应用成功（UpdateAgent 物化/重建）后，当前会话的对话视图 MUST 立即同步为重建后 agent 的实际状态：旧历史、排队指示、错误提示与"已终止"标识 MUST 清除，无需刷新页面或重新进入会话；同步 MUST NOT 影响重建后立即发送的新消息（新对话内容不被同步覆盖）；同步失败 MUST 按既有回填错误语义呈现且不静默清空。空闲与忙时（在途回合 ABORTED）两条路径 MUST 收敛到同一终态；首次物化路径行为一致无异常。

#### 长名悬停揭示（用户问题 1）

- **FR-002**: web session 列表超宽名称条目悬停时 MUST 自动滚动揭示完整名称（悬停即滚、无需额外操作，2026-09-06 澄清 Q1 裁定）；未悬停的渐隐遮罩呈现与移出复位（scrollLeft 归零）零回归；悬停滚动 MUST NOT 劫持列表垂直滚动或页面滚动。短名（不超宽）条目悬停 MUST 无副作用（不启动滚动）。

#### desktop 手动刷新（用户问题 2）

- **FR-003**: desktop sessions 列表页 MUST 提供显式刷新入口：点击即重新拉取当前模板的会话列表；加载/失败语义与既有刷新时机一致，失败不清空已呈现列表，刷新不干扰导航与选择（055 FR-007 并发语义延续）；既有启动/返回/模板切换刷新零回归；重复交互不产生叠加错误状态。列表仍为只读控制面（不引入新建/删除操作）。

#### 范围与验收

- **FR-004**: 全部修复 MUST 附带组件级测试：重建同步（应用后回填重建、并发发送让位、失败语义、忙时/首次物化路径）、长名悬停揭示（滚动触发、复位、渐隐零回归）、desktop 手动刷新（触发、失败、既有语义回归）；真实浏览器/桌面环境的滚动可达性与重建同步体验以人工验证记录闭合（对齐 055 A5 口径，不引入浏览器 E2E 基建）；049/051/054/055 既有组件与 store 测试零回归，054 既有 testplan suites 零回归。

### Key Entities

- **重建同步（Rebuild Sync）**: UpdateAgent 应用成功后，发起客户端的对话视图向重建后服务端状态（空历史、无排队/错误/终止残留）收敛的行为；与在途 ABORTED 流事件、紧随发送的并发关系是其边界语义。
- **长名揭示（Long-Name Reveal）**: 侧栏 session 条目名的截断呈现（渐隐遮罩）与悬停自动滚动揭示（触发、节奏、复位）的配对行为。
- **手动刷新（Manual Refresh）**: desktop sessions 页的用户主动重取列表入口；与既有自动刷新时机（启动/返回/模板切换）共用加载与错误语义。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 重建同步闭环：空闲 agent 重建后无需刷新页面，对话视图立即反映重建后状态（组件级断言 + 人工验证记录）；忙时重建与首次物化路径收敛一致；重建后立即发送的新消息完整保留。
- **SC-002**: 长名可读：悬停超宽 session 条目可读完整名称（组件级断言滚动触发与复位 + 人工验证记录真实滚动可达）；未悬停渐隐与复位行为零回归。
- **SC-003**: desktop 手动刷新可用：点击刷新即时反映外部会话集合变更（人工验证记录）；既有三个自动刷新时机与 055 交付一致（组件级断言零回归）。
- **SC-004**: 既有行为零回归：049/051/054/055 既有组件与 store 测试全部通过；对话语义（分段/折叠/markdown/终止/内容保留/排队/回填让位）、preset 管理、session 删除编排、054 testplan suites 与 055 交付一致。

## Assumptions

- **A1（悬停揭示交互模型，2026-09-06 用户裁定）**: 悬停揭示采用自动滚动模型（marquee 式）：悬停即自动逐步滚动展示全名，移出复位；不引入"悬停 + 滚轮驱动横向滚动"的约定（可发现性弱）。滚动节奏与到端点行为等细节由 plan 决策；无论细节取何，可观测约束不变：悬停可读全名、移出复位、无滚动副作用。
- **A2（重建同步范围）**: 同步范围为**发起应用的客户端**（web 会话面板应用路径）；多标签页或外部 API 触发的重建不引入推送/轮询通知（延续 `specs/049-agent-v2-dsh-init/research.md` 已知多标签页限制），其他客户端经自身回填时机（重进/刷新）收敛。
- **A3（验收口径，对齐 055 A5）**: 本 feature 为 web/desktop 前端缺陷修复，不改变服务行为；验收以组件级测试 + 人工验证记录为主，不引入浏览器 E2E 基建；054 既有 testplan suites 作为回归面零回归。
- **A4（desktop 只读边界修订）**: 手动刷新入口是对 051 契约退化清单（`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §5 移除"刷新"）的显式修订：仅恢复刷新（读操作），不恢复新建/删除管理操作；`specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §5 只读控制面语义不变。
- **A5（长名根因确认口径）**: "悬停不滚动"的根因（滚动条隐藏 + 主流浏览器不为仅横向可滚动容器重定向垂直滚轮）以源码事实链 + 外部权威引用确认（research §1.2），真实浏览器滚动可达性以部署环境人工验证记录闭合；spec 只约束可观测结果（全名可读、无副作用），不预设实现手段。
