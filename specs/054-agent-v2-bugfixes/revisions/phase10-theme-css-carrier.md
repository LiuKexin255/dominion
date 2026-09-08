# Revision: token CSS 引入载体重设计（phase10-theme-css-carrier）

**Feature**: [spec.md](../spec.md) | **日期**: 2026-09-04 | **性质**: 执行前载体重设计（Phase 10 T020 原方案的引入方式假设与 npm tarball 实态不符；本文为替代设计的权威描述，不含代码变更）

**状态**: 本文裁定 token CSS 的引入载体并给出终态设计。终态为：**官方 token sheets 以源码形态 vendored 于 frontend `src/dsh-theme/`**（无 npm 依赖、无提取脚本、无同步门禁；升级走 README 记录的人工流程）。[contracts/web-ui.md](../contracts/web-ui.md) §1、[tasks.md](../tasks.md) T020、[research.md](../research.md) D4 修正注与 [plan.md](../plan.md) 已同步为终态表述（constitution VII：契约/tasks/plan 只写终态，候选否决记录由本文 §2 承载）。

---

## 0. 问题定性：原方案假设与 tarball 实态不符

原设计（[research.md](../research.md) D4 / contracts/web-ui.md §1 旧版）：按官方顺序以 vite 直接 import `@deepseek-ai/dsh-client-ui-theme/src/styles/` 五个 CSS（base → design-platform → scrollbar → gradient-shadow-text → shiki），依据是该包 package.json exports 含 `"./src/*"` 且 README 声明 `src/styles/` 为 sheet 所在。

**已验证事实**（npm registry 下载 tarball 逐文件实读；0.1.1-rc.2 与 0.1.2-rc.1 双版本复核；复现方式：`npm pack @deepseek-ai/dsh-client-ui-theme@0.1.1-rc.2` 后 `tar -tzf` 列文件）：

| # | 原假设 | tarball 实态 |
|---|---|---|
| 1 | `src/styles/*.css` 可 import | 0.1.1-rc.2 tarball 共 17 个文件，**不含 `src/` 目录**；exports 的 `"./src/*"` 指向不存在路径（0.1.2-rc.1 共 14 个文件，同样不含） |
| 2 | CSS 是包内独立文件 | 五个 sheet 以 **JS 字符串常量内嵌**在 `lib/client.js`（80KB），由 `//#region \0dsh-inline-css:...src/styles/<name>.css.mjs` 区域标记分隔；`STYLES` 数组声明文件清单与官方顺序；package.json `files` 虽声明 `lib/styles` 但任何已发布版本都未产出该目录 |
| 3 | 包可被标准模块系统加载 | `lib/client.js` 是 **dsh ModuleLoader 格式**（`window.__ModuleLoader__.load({...})` 顶层执行，非标准 ESM/CJS；Node 侧 require 实测抛 `ReferenceError: window is not defined`），vite/Node 无法 import |
| 4 | （未验证）或有其他 CSS 出口 | registry 全部 16 个版本（0.0.1-rc.1 … 0.1.2-rc.1）均无 css 文件；官方注入路径是 cordis 客户端插件 `installThemeStyles(ctx)`（peerDependencies：`@deepseek-ai/cordis` + 8 个 dsh 包——api-remotes/client-connection/client-locale/client-runtime/ui-settings/invariants/host-webserver/settings），与契约"不引入 cordis runtime"约束直接冲突 |

结论：直接 import 该 npm 包不可行；须裁定其他载体。spec 层无冲突——A6 明确"是否整体采用组件库官方主题 token 集由 plan 阶段决策，验收只约束视觉结果"（[spec.md](../spec.md) A6），采用官方 token 集的决策不变，变的只是**载体**。

## 1. 调研结论（载体相关事实）

### 1.1 npm 包的可编程消费面与 CSS 获取方式

包的可编程入口仅三个（tarball package.json exports）：`.`（host 半：settings 注册 + webserver 注入，需 cordis/dsh-settings）、`./invariant`、`./client`（ModuleLoader 格式，不可 import）。CSS 的**唯一可编程获取面**是 `lib/client.js` 中的字符串常量。

人工获取方式（已实际执行验证，是 §3.2 README 升级流程的方法依据）：

- 以 `//#region \0dsh-inline-css:\S*?src/styles/([a-z-]+\.css)\.mjs\s*\n\s*var \w+ = ("...")` 锚点定位 + JSON 反转义，从 0.1.1-rc.2 `lib/client.js` 提取五个 sheet，字节长度 base 455 / design-platform 15540 / scrollbar 565 / gradient-shadow-text 9336 / shiki 705，与逐字节核对一致；
- `STYLES` 数组（`[[name, ident], ...]` 嵌套数组）声明**官方顺序**（base → design-platform → scrollbar → gradient-shadow-text → shiki）；
- 对 0.1.2-rc.1 以同一方法提取成功，且检出**第 6 个 sheet**（`corner-shape.css`，141B）与 `gradient-shadow-text.css` 内容变化（9336 → 11569）——升级时 sheet 集与内容变化须人工核对引入。

### 1.2 sheets 结构与深色激活（实读）

- token 定义在 **`body` 作用域**（非 `:root`）：`design-platform.css` 为 light/dark 成对块（static 层 73 vars + alias 层 89 vars，各一对）；shadow/gradient/markdown 字体 token 在 `gradient-shadow-text.css`；`scrollbar.css` 是滚动条 rebinding 契约的消费规则（`--dsh-scrollbar-*` 绑定 + `::-webkit-scrollbar` 伪元素样式）；`shiki.css` 为 `:root` + dark 成对。
- 深色激活 = **`body[data-ds-dark-theme]` 布尔属性存在性选择器**（属性存在即 dark，无值语义）。官方 bootstrap（`lib/index.js` 的 `bootThemeScript`）成对设置 `documentElement.style.colorScheme` 与 body 属性——深色单一主题的应用可以静态形态等价表达。
- 除 `scrollbar.css` 的滚动条伪元素规则外，四个 sheet **只含 custom-property 声明**（逐块解析核实，无普通声明）→ 与 `theme.css` 的布局规则零级联冲突，引入顺序对 `var()` 消费无影响（custom property 在计算值期解析）；`index.css` 的顺序保真仅为官方形态一致，无功能约束。
- 细节：`--dsw-shadow-lv3` 定义于 `gradient-shadow-text.css` 的 `body` 块（light/dark 共用一次定义）；`--dsw-specific-menu`/`--dsw-alias-border-inverted` 在 dark 块有专属值（dark 菜单卡片 = `--dsw-alias-bg-layer-3` 高表面 + `#ffffff0f` 发丝边框 + lv3 阴影）。

### 1.3 token 覆盖量化（FR-022 基线）

- primitives（`lib/**/*.css`，含 markdown 目录）消费 **55 个唯一 `--dsw-*`**；54 个由 sheets 定义；**唯一缺口 `--dsw-hovercard-bg`**——仅 `HoverCard.module.css` 消费，本应用未使用 HoverCard（frontend src 全目录 grep 零引用），官方栈中该 token 由 ui-layout 侧定义（不在 ui-theme sheets 内）。
- `theme.css` 消费 10 个唯一 `--dsw-*`，全部在 sheets 中；`:root` 手写的 11 个 `--dsw-*` 声明（`theme.css:7-17`）**全部被 sheets 同名覆盖**（值差异 = sheets 的 dark 值更贴近官方，属预期修复方向而非回归）。

## 2. 候选评估与决策

裁定输入：用户反馈（2026-09-04）给定两个候选方向——(A) 包以源码方式放入 `third_party/`；(B) CSS 以源码方式 copy 进项目 src——并否定此前实现的"npm 依赖 + 提取脚本 + 防漂移门禁"形态（原话："现在的做法复杂且不稳定"）。

### 2.1 选定（B）：官方 token sheets 以源码形态直接 vendored 于 `src/dsh-theme/`

**方案**：五个 token sheet css（字节精确取自 0.1.1-rc.2 包内字符串常量）+ `index.css`（按官方顺序 `@import`）+ `README.md`（溯源与人工升级流程）作为**普通源文件**置于 `projects/game/web/frontend/src/dsh-theme/`；不声明 npm 依赖、无提取脚本、无同步门禁；升级 = 按 README 记录的人工流程重新获取并 diff review。

**工程论证**：

1. **产物性质**：纯静态 CSS（五个共 ~26KB，四个 sheet 仅含 custom-property 声明），无任何 JS import 面、无版本解析需求——不需要包管理机制承载；
2. **构建零改动**：唯一消费者是 frontend；`vite_build` 与 `vitest_test` 的 `glob(["src/**"])`（`projects/game/web/frontend/BUILD.bazel:9/:32`）自动覆盖 vendored 目录，零配置；
3. **溯源完整**：目录 README（版本、获取源实态、升级步骤、MIT、上游链接）+ frontend README attribution；升级走人工 diff review，与仓库 third_party 本地化依赖（`third_party/github.com/wailsapp/wails/` 的人工升级形态）等价，但无跨目录构建代价；
4. **简单、稳定**（对齐裁定输入）：机制面归零——无 lock 膨胀、无对上游内部构建结构（region 标记）的运行时/CI 依赖；对上游结构变化的暴露仅剩"升级时的人工核对"。

### 2.2 否决候选（constitution VII：必要时记录，防重复踩坑）

| 候选 | 内容 | 否决理由 |
|---|---|---|
| (a) npm 依赖 + 提取脚本 + 防漂移门禁 | frontend 声明 catalog 依赖使 tarball 进入 node_modules；脚本 `scripts/sync-dsh-theme.mjs` 从 `lib/client.js` region 锚点提取并 vendor；`src/dsh-theme.test.ts` 独立重提取做字节比对 | **用户裁定否决**（2026-09-04 反馈："现在的做法复杂且不稳定"）：lock 引入 731 行 peer 闭包（cordis + 8 个 dsh 包）；region 正则解析耦合上游内部构建结构；门禁以独立重实现提取的方式自证——机制成本与"消费五段静态 CSS"的问题规模不匹配 |
| (A) third_party 源码化 | 包（或其实际所需部分）以源码方式放入 `third_party/`（用户候选之一） | **third_party 惯例错配**：该目录语义是"本地化的第三方依赖**源码**"（`third_party/README.md`；wails = 完整源码副本 + 版本 README、`third_party/dsh/core/` = 重组 workspace 包），而五个 css 并非上游包源码组成部分（tarball 无独立 css 文件，§0-2）——放入即组织出上游不存在的伪源码包，溯源更绕；**跨目录构建代价**：frontend 构建以包根 `glob(["src/**"])` 为界，从 third_party import 需 vite dev `server.fs.allow` 放开、`vite_build` srcs 跨包 label 引用（bazel glob 不能跨包边界）、vitest data 跨目录条目三处机制配置——复杂度回归（与候选 a 同病） |
| (c) cordis 客户端插件注入 | 经 `installThemeStyles(ctx)` 官方路径注入 | 前端需引入 cordis 客户端运行时 + 8 个 dsh peer 包，而 frontend 是无插件宿主架构的 React 应用（[research.md](../research.md) D1 已否决 L4 完整插件栈，同一 rationale）；且 `./client` 入口本身是 ModuleLoader 格式、并无标准 import 通路。为注入五段静态 CSS 引入整套插件宿主，是 constitution II 意义上的不可论证复杂度 |
| (d) GitHub 源仓库获取 | 从 https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/client/ui-theme/src/styles 取 css | **版本不一致**：master 已漂移（6 个 css，含新增 `corner-shape.css`；`design-platform.css` 19020B vs 锁定版 15540B、`scrollbar.css` 4081B vs 565B，https://api.github.com/repos/deepseek-ai/deepseek-harness/contents/packages/client/ui-theme/src/styles 实读），与 primitives 0.1.1-rc.2 线不匹配；按 commit 钉版本依赖 gitHead 考古且脱离 npm 版本语义。该否决与载体无关、对人工升级流程同样成立——再获取必须走 npm tarball（§3.2） |
| (e) 构建期 vite 插件提取 | vite.config 加插件，构建时解析 `lib/client.js` 生成虚拟 CSS 模块 | 向 vite.config 注入字符串解析式的构建魔法，与仓库 vite 链路刻意保持薄的做法（`tools/dev/js/vite.bzl` 仅 cd + 调用）不一致；CSS 不以可 review 的文件形态存在（diff 不可审、排查多一跳）；测试期仍需自行提取（jsdom 无法消费虚拟模块）——机制成本高于候选 (a)，一并否决 |
| (f) 手写补齐缺失变量 / 等待上游修复打包 | theme.css 手写 `--dsw-*` 扩到 55 个；或等上游产出真实 css 文件 | 手写：D4 既有否决（"逐组件打补丁，遗漏面大且与官方漂移"），55 个 token 的手抄副本无权威校验；等待：16 个已发布版本均未产出、无修复时间线，不可作为本 feature 路径（作为 §7 简化触发器记录） |

## 3. 终态设计

### 3.1 vendored 目录 `projects/game/web/frontend/src/dsh-theme/`

| 文件 | 内容 | 形态 |
|---|---|---|
| `base.css` / `design-platform.css` / `scrollbar.css` / `gradient-shadow-text.css` / `shiki.css` | 官方 token sheet 原文（0.1.1-rc.2） | 字节精确取自包内字符串常量的**普通源文件**（不加头注释——溯源集中于 README；字节精确保证与官方视觉零漂移的 diff 基线） |
| `index.css` | 依官方顺序的 `@import './<name>.css';` 逐行（五行） | 顺序忠实于包内 `STYLES` 数组（§1.1）；vite（postcss-import）在 build/dev 两种模式内联相对 `@import`，无配置需求 |
| `README.md` | 溯源与人工升级流程 | 手写文档（§3.2） |

目录是 vendored 静态资产（非生成物、无"勿手改"标记——它就是当前源码形态的权威）；升级时的变更 = 人工重新获取后的 diff review。

### 3.2 溯源 README 与人工升级流程（替代一切自动化机制）

`src/dsh-theme/README.md` 必须记录：

- **来源**：npm 包 `@deepseek-ai/dsh-client-ui-theme`，vendored 版本 0.1.1-rc.2（与 `pnpm-workspace.yaml` catalog 的 primitives 条目同线）；包主页 https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-theme ；
- **获取源实态**：npm tarball 不含独立 css 文件；css 以字符串常量内嵌于 `lib/client.js` 的 `//#region \0dsh-inline-css:...src/styles/<name>.css.mjs` 区域标记后（`var <ident> = "<json-escaped css>"`）；引入顺序由包内 `STYLES` 数组声明；GitHub master 已漂移、不可替代 tarball（§2.2 d）；
- **人工升级步骤**：① `npm pack @deepseek-ai/dsh-client-ui-theme@<目标版本>` 并解包；② 按 region 锚点从 `lib/client.js` 提取全部 sheet（JSON 反转义；锚点形态见上）覆盖五个 css——sheet 集变化（如 0.1.2-rc.1 新增 `corner-shape.css`）须一并评估引入；③ 按新版本 `STYLES` 数组核对 `index.css` 顺序；④ 更新本 README 的版本记录；⑤ diff review 提交；
- **许可证与上游**：MIT；上游仓库 https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/client/ui-theme 。

### 3.3 引入与深色激活

- `src/App.tsx`（`theme.css` 的既有 import 所在）：在 `import './theme.css'` **之前**新增 `import './dsh-theme/index.css'`（`var()` 解析与顺序无关，先后仅表达 token→布局的意图；App 是全部 CSS 的既有入口，不引入第二入口）。
- `index.html`：`<body>` 置静态布尔属性 `data-ds-dark-theme`（深色单一主题，无切换面；与 sheets 的存在性选择器、官方 bootstrap 的 `toggleAttribute` 语义一致）。
- `theme.css` `:root` 新增 `color-scheme: dark;`（官方 bootstrap 对 html 的 `colorScheme` 与 body 属性成对设置；静态深色应用以 CSS 表达前者，UA 表单控件/滚动条基色随之正确）。

### 3.4 theme.css 删除面

- `:root` 删除 11 个手写 `--dsw-*` 声明；保留 `--app-bg`/`--app-panel`/`--app-border` 并新增 `color-scheme: dark`；
- **消费面零改动**：theme.css 内 10 个唯一 token 的全部 `var(--dsw-*)` 消费（30+ 处）在 sheets 中均有定义（§1.3），custom property 计算期解析，无需改动任何布局规则；
- 头注释更新：`--dsw-*` 权威来源改为 `src/dsh-theme/`（token sheets），移除"清单照 049 契约手写"的旧表述。

### 3.5 依赖与 BUILD

- **无新增 npm 依赖**：frontend `package.json`、根 `pnpm-workspace.yaml` catalog、`pnpm-lock.yaml` 均不含 `@deepseek-ai/dsh-client-ui-theme`（vendored 源文件不参与包解析；无消费者的 catalog 条目属悬空配置，不保留）；
- `projects/game/web/frontend/BUILD.bazel`：`vite_build` 的 `srcs = glob(["src/**"])` 与 `vitest_test` 的 `data = glob(["src/**"])` 自动覆盖 vendored 目录；vitest data 仅新增 `index.html` 一项（SessionList 对 body 激活属性的内容断言读取它）；无 node_modules target/gazelle 相关变更。

### 3.6 FR-022 消费覆盖断言 `src/dsh-theme.test.ts`

唯一保留的 dsh-theme 专项测试（防漂移字节比对、`@import` 顺序、README 版本一致三组同步断言随机制移除——vendored 源文件形态下无"另一份权威"可比对）：

- **断言**：`node_modules/@deepseek-ai/dsh-client-ui-primitives/lib/**/*.css` 与 `src/theme.css` 消费的全部 `--dsw-*` ⊆ `src/dsh-theme/*.css` 定义集；豁免表 `['--dsw-hovercard-bg']`（HoverCard 未使用，官方由 ui-layout 定义——§1.3，注释说明）；
- **读取面均为既有稳定输入**：primitives 是运行时依赖（vitest data 已含其 node_modules target）；sheets 是 src 源文件（data glob 已含）。无提取逻辑、无 theme 安装包依赖；
- **失败语义**：primitives catalog 升级引入新消费 token 而 vendored sheets 未跟随时，此断言失败——失败信息指向 `src/dsh-theme/README.md` 的人工升级流程（这是升级信号，不是自动同步）；
- 文件定位沿用 `SessionList.test.tsx` 的 cwd 候选模式（bazel runfiles 根 + 包目录两种执行环境）。

### 3.7 frontend README（attribution 与依赖说明）

- Attribution 节增条目：`src/dsh-theme/`（官方 token sheets，以源码形态 vendored）来自 npm 包 `@deepseek-ai/dsh-client-ui-theme`（MIT，上游 https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/client/ui-theme ）；溯源与升级流程见 `src/dsh-theme/README.md`；
- "依赖"节：全部依赖统一经根 `pnpm-workspace.yaml` catalog 管理（顺带修正 primitives"直接 pin（catalog 例外）"的过时表述——Phase 1 已迁 catalog）；注明 `@deepseek-ai/dsh-client-ui-theme` 不作为依赖引入、其 token sheets 以源码形态 vendored（指向上述 attribution）。

## 4. 测试义务（T020 内嵌，constitution IV）

| 层 | 文件 | 断言 |
|---|---|---|
| FR-022 覆盖 | `src/dsh-theme.test.ts` | §3.6：消费（primitives lib css + theme.css）⊆ vendored sheets 定义，豁免 `--dsw-hovercard-bg` |
| Menu 视觉 | `src/components/SessionList.test.tsx` | （i）vendored sheets 定义 Menu 卡片消费的三 token（`--dsw-specific-menu`/`--dsw-alias-border-inverted`/`--dsw-shadow-lv3`——union 面，dark 专属值在 dark 块）；（ii）sheets 含 `body[data-ds-dark-theme]` 激活选择器；（iii）`index.html` 的 `<body>` 含 `data-ds-dark-theme` 属性（readFileSync 内容断言） |
| 手写权威清除 | 同上 | `theme.css` 内容不再含任何 `--dsw-*:` 声明（`--app-*` 与 `color-scheme` 保留断言可选） |
| 回归 | 既有全部用例 | theme.css 布局断言（`SessionList.test.tsx` 既有用例）与组件用例零回归；`bazel test //projects/game/web/frontend/...` 全绿 |

**断言面说明（jsdom 约束）**：jsdom 不应用外部样式表、vitest 默认 `css:false` 将 .css 模块替换为空串、且 jsdom 的 `getComputedStyle` 不解析 custom property `var()` 引用——计算样式断言不可达；文件内容断言（既有 loadThemeCss 模式扩展到 sheets/index.html）是本仓库可行断言面上限，契约 §1 验收按此表述。真实视觉效果由 Phase 10 Independent Test 的人工浏览器目验兜底。

## 5. 对 tasks/契约/调研/plan 的修订说明（已随本文应用）

| 文档 | 修订 |
|---|---|
| [contracts/web-ui.md](../contracts/web-ui.md) | §1 "引入"行替换为 vendored 源码形态终态；"自有样式"/"验收"行的覆盖断言表述同步（FR-022 覆盖断言仍在，形态为 §3.6） |
| [tasks.md](../tasks.md) | T020 按终态重写（含对工作区未提交实现的返工处置——保留/删除/回退/改写）；Phase 10 Independent Test 与文档清单措辞同步 |
| [research.md](../research.md) | D4 修正注更新为源码 vendored 载体终态（指向本文） |
| [plan.md](../plan.md) | Approach/结构图中的载体表述同步（去掉"新增 npm 依赖/src/styles import"的过时描述） |

spec.md 零修改（A6 本就将引入方式留给 plan/执行阶段决策，验收只约束视觉结果）。

## 6. 下游执行指引（对未提交实现的返工处置，分步可恢复）

当前工作区已有按已否决候选 (a) 完成的未提交实现（五个 css 产物字节精确、可直接复用）。返工处置：

**保留（与终态一致）**：

- `src/dsh-theme/` 五个 css 与 `index.css`（字节精确产物即终态内容）；
- `src/App.tsx` 的 `import './dsh-theme/index.css'`、`index.html` 的 `<body data-ds-dark-theme>`、`src/theme.css` 删除面与头注释；
- `SessionList.test.tsx` 的 Menu 卡片视觉断言（读 src 文件与 index.html，不依赖 theme 安装包）及其加载 helper（`loadDshThemeSheets`/`loadIndexHtml`）；
- `BUILD.bazel` vitest data 的 `index.html` 条目。

**删除**：

- `projects/game/web/frontend/scripts/` 目录整体（`sync-dsh-theme.mjs`）；
- `package.json` dependencies 的 `"@deepseek-ai/dsh-client-ui-theme": "catalog:"`；
- `BUILD.bazel` vitest data 的 `:node_modules/@deepseek-ai/dsh-client-ui-theme` 条目；
- `src/dsh-theme.test.ts` 的字节同步/`@import` 顺序/README 版本三组断言与安装包提取逻辑（文件保留，内容改写为 §3.6 单断言）。

**回退**：

- `pnpm-workspace.yaml` catalog 的 `"@deepseek-ai/dsh-client-ui-theme": "0.1.1-rc.2"` 条目（无消费者后为悬空配置）→ `bazel run @pnpm -- --dir /mnt/code/dominion install` 回退 `pnpm-lock.yaml`（-731 行）→ `bazel run //:gazelle projects/game/web/frontend` → `bazel build //projects/game/web/frontend/...` + `bazel test //projects/game/web/frontend/...` 验证。

**改写**：

- `src/dsh-theme/README.md`：去掉"生成目录勿手改/再生成命令"表述，改写为 §3.2 溯源与人工升级流程；
- `projects/game/web/frontend/README.md`："依赖"节去掉 ui-theme catalog 依赖表述（保留 primitives 已迁 catalog 的修正）、attribution 条目去掉"由脚本生成"表述（§3.7）。

**分步**（每步可独立提交/中断恢复）：

1. 机制删除与依赖回退（上述删除+回退项；验证：lock 无 ui-theme、`bazel build`/`bazel test` 通过）；
2. `src/dsh-theme/README.md` 与 `src/dsh-theme.test.ts` 改写（§3.2/§3.6；验证：test 全绿）；
3. frontend README 改写（§3.7）；
4. 浏览器目验 `···` 菜单卡片（背景/边框/阴影）与既有组件无视觉缺失（`bazel build //projects/game/web/frontend:dist` 后人工核查；token 值向官方 dark 值靠拢属预期修复非回归）。

## 7. 上游简化触发器（记录，非本 feature 事项）

若上游未来版本按其 `files` 声明真正产出 `lib/styles/*.css`（或恢复 `src/` 发布），§3.2 人工升级流程退化为"从 tarball 复制 css 文件"——载体（vendored 源文件）与全部断言形态不变。即使 npm 未来出现可 import 的 css 入口，"不引入该依赖"的裁定不变：单消费者静态资产无需包解析机制。
