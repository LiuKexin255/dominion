# FR-001 排查记录：思考输出期间布局缺陷（T007）

**Feature**: `specs/055-agent-v2-ui-fixes/spec.md`（FR-001 / US1 / T007 排查回路）
**日期**: 2026-09-05（部署验证反馈与复验，同日两轮）
**结论**: 三项断点均已定位并修复；待部署环境复验（见"待验证项"）。

## 现象

第一轮（已修复，用户复验确认，session_id: `7556a80de1f851381d98ec5c4ec356cb`）：

1. 思考过程输出期间，step 卡片完全空白无任何内容。
2. 刷新页面后（历史回填）该 step 仍空白。
3. 思考完成后 message 正文正常输出。

第二轮（复验发现的残留问题）：

4. 展开思考体时浏览器窗口出现滚动条：固定长度、视口越短溢出越长、可把整页滚到消失。
5. 思考展开体中的 markdown code 块被强行拉到卡片同宽——短代码也绘制满宽背景块。

## 根因

### 断点 0：`contain: size` × fit-content 卡片（第一轮）

`.msg-agent`（step 卡片容器，`projects/game/web/frontend/src/theme.css` 的 `.msg-agent` 规则）是 `align-self: flex-start; max-width: 80%` 的 **fit-content 宽度**卡片——卡片宽度由子元素 intrinsic 宽度决定（本地自有卡片设计，无上游对应）。折叠围栏若含 `contain: size`，`.reasoning-row` 按"无内容"计算 intrinsic 尺寸，对卡片宽度的贡献为 0：思考输出期间的 step 只含 THINK 块（ReasoningRow 是唯一子元素），卡片 fit-content 宽度塌缩为 0（仅剩 padding）→ 整卡不可见；TEXT 块到达后其内容宽度撑起卡片 → 正文正常；刷新后历史首个 step 仍只含 THINK → 依旧空白。上游同款 `contain: size layout`（[ReasoningRow.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.module.css)）安全的原因：dsh-web 消息流是全宽 block 布局，宽度不依赖子元素 intrinsic 尺寸。

### 断点 1：running 播报 span 绝对定位逃逸到 ICB（第二轮）

诊断数据：文档溢出 759px，但 `.chat-messages` 外无任何越界元素 → 溢出来自滚动容器内部**逃逸裁剪**的绝对定位元素。running 态渲染的无障碍播报 span（`projects/game/web/frontend/src/components/ReasoningRow.tsx`，`visually-hidden` 类，`projects/game/web/frontend/src/theme.css` 中 `position: absolute` 且偏移为 auto）：

- 折叠态：围栏 `contain: layout` 使 `.reasoning-row` 成为该 abspos 的 containing block → 被包含（用户实测收起正常）。
- 展开态：无围栏，且 `.chat-messages` 与所有中间祖先均未定位（`position: static`）→ containing block 直达初始包含块（ICB）→ span 被布局在未随滚动调整的静态位置（内容坐标深处，绝对底部恒定）→ 根滚动器滚动范围被撑大 → 视口越短溢出越大。
- 该根因同时解释 054 原始缺陷"折叠/展开两态均有窗口滚动条"（当时无 contain，两态均逃逸）；`specs/055-agent-v2-ui-fixes/research.md` §2.3 候选 1（running sweep 动画）证伪。
- 上游对照：上游 `visuallyHidden` 同为 `position: absolute`（[accessibility.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/accessibility.module.css)），但其 ChatView `.root` 是 `position: relative`（[ChatView.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ChatView.module.css)）承接了逃逸的 abspos。

### 断点 2：code 块块级填宽 × fit-content 卡片（第二轮）

诊断数据：卡片 1270px = 消息区内容宽 1588px × 80%，精确命中 `.msg-agent` 的 `max-width: 80%` 上限；pre 1222px = 卡片内宽。包内 CodeBlock 包装是块级盒子（`@deepseek-ai/dsh-client-ui-primitives` lib/markdown/CodeBlock.module.css `.block`，渲染为 `<div className={clsx(css.block, "md-code-block", className)}>`，全局类 `.md-code-block`），块级填宽使其在 fit-content 卡片内自动占满剩余宽度——短代码也绘制满宽背景块。用户裁定：代码块不应强行与卡片同宽，应贴合自身内容宽度、上限受卡片约束。

## 修复

- **断点 0（第一轮）**：`projects/game/web/frontend/src/theme.css` 折叠围栏 `contain: size layout` → `contain: layout`（保留 `height: 24px` 垂直围栏）；断言与契约/文档终态同步（`theme-fence.test.ts`、`specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md` §2.1、`specs/055-agent-v2-ui-fixes/data-model.md` §2、`specs/055-agent-v2-ui-fixes/research.md` §1.2）。
- **断点 1（第二轮）**：`.chat-messages` 增加 `position: relative`——滚动容器成为其内部所有绝对定位后代的 containing block（随内容滚动、被滚动容器裁剪），结构性消除逃逸；对 ToolCard/JsonBlock 等包内组件潜在的 abspos 一并生效。契约登记于 `specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md` §1.5。`.visually-hidden` span 保留（无障碍播报是正当需求，逃逸由容器定位修复，不改 DOM）。
- **断点 2（第二轮）**：theme.css 新增 `.msg-agent .md-code-block { width: fit-content; max-width: 100% }`——短代码块贴合内容宽，长代码受卡片内容宽约束换行（包内 `pre` 为 `pre-wrap + break-all`）。属本地卡片布局适配，不登记契约。
- DOM 结构与组件逻辑不变（ReasoningRow.tsx / ChatView.tsx 无改动）；组件级测试断言全部有效。

## 待验证项（部署环境，待用户复验）

1. 展开思考体（含 DevTools 打开缩短视口的极端场景）窗口无滚动条、页面整体不可滚动。
2. 折叠态回归确认：思考折叠输出期间卡片可见、无窗口滚动条（第一轮修复不回归）。
3. code 块贴合内容宽：短代码不满宽；长代码受卡片宽度约束并换行（`pre-wrap + break-all`）。
4. 若窗口滚动条仍复现，下一个探针：暂时禁用包内 code 块 `.bannerWrap` 的 `position: sticky`（Chromium sticky 扩大根滚动范围的已知怪癖类别，`CodeBlock.module.css .bannerWrap`），再回报现象。
