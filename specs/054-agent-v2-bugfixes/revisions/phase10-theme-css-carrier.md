# Revision: token CSS 引入载体重设计（phase10-theme-css-carrier）

**Feature**: [spec.md](../spec.md) | **日期**: 2026-09-04 | **性质**: 执行前载体重设计（Phase 10 T020 原方案的引入方式假设与 npm tarball 实态不符；本文为替代设计的权威描述，不含代码变更）

**状态**: 本文裁定 token CSS 的引入载体并给出终态设计。[contracts/web-ui.md](../contracts/web-ui.md) §1 与 [tasks.md](../tasks.md) T020 已同步为终态表述（constitution VII：契约/tasks 只写终态，演进与候选否决记录由本文承载）；[research.md](../research.md) D4 附修正注指向本文。

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

结论：原引入方式不可执行；需重新裁定载体。spec 层无冲突——A6 明确"是否整体采用组件库官方主题 token 集由 plan 阶段决策，验收只约束视觉结果"（[spec.md](../spec.md) A6），采用官方 token 集的决策不变，变的只是**载体**。

## 1. 调研结论（载体相关事实）

### 1.1 npm 包的可编程消费面与提取确定性

包的可编程入口仅三个（tarball package.json exports）：`.`（host 半：settings 注册 + webserver 注入，需 cordis/dsh-settings）、`./invariant`、`./client`（ModuleLoader 格式，不可 import）。CSS 的**唯一可编程获取面**是 `lib/client.js` 中的字符串常量。

提取确定性验证（本调研实际执行）：

- 以 `//#region \0dsh-inline-css:\S*?src/styles/([a-z-]+\.css)\.mjs\s*\n\s*var \w+ = ("...")` 锚点定位 + JSON 反转义，从 0.1.1-rc.2 `lib/client.js` 提取五个 sheet，字节长度 base 455 / design-platform 15540 / scrollbar 565 / gradient-shadow-text 9336 / shiki 705，与逐字节核对一致；
- `STYLES` 数组可同法提取**官方顺序**（顺序由包自身声明，无需依赖 README 记忆）；
- 对 0.1.2-rc.1 以同一方法提取成功，且检出**第 6 个 sheet**（`corner-shape.css`，141B）与 `gradient-shadow-text.css` 内容变化（9336 → 11569）——提取法跨版本稳健，且版本升级带来的 sheet 集变化**可被机器检出**（这是 §3.6 同步门禁的可行性基础）。

### 1.2 sheets 结构与深色激活（实读）

- token 定义在 **`body` 作用域**（非 `:root`）：`design-platform.css` 为 light/dark 成对块（static 层 73 vars + alias 层 89 vars，各一对）；shadow/gradient/markdown 字体 token 在 `gradient-shadow-text.css`；`scrollbar.css` 是滚动条 rebinding 契约的消费规则（`--dsh-scrollbar-*` 绑定 + `::-webkit-scrollbar` 伪元素样式）；`shiki.css` 为 `:root` + dark 成对。
- 深色激活 = **`body[data-ds-dark-theme]` 布尔属性存在性选择器**（属性存在即 dark，无值语义）。官方 bootstrap（`lib/index.js` 的 `bootThemeScript`）成对设置 `documentElement.style.colorScheme` 与 body 属性——深色单一主题的应用可以静态形态等价表达。
- 除 `scrollbar.css` 的滚动条伪元素规则外，四个 sheet **只含 custom-property 声明**（逐块解析核实，无普通声明）→ 与 `theme.css` 的布局规则零级联冲突，引入顺序对 `var()` 消费无影响（custom property 在计算值期解析）。
- 细节：`--dsw-shadow-lv3` 定义于 `gradient-shadow-text.css` 的 `body` 块（light/dark 共用一次定义）；`--dsw-specific-menu`/`--dsw-alias-border-inverted` 在 dark 块有专属值（dark 菜单卡片 = `--dsw-alias-bg-layer-3` 高表面 + `#ffffff0f` 发丝边框 + lv3 阴影）。

### 1.3 token 覆盖量化（FR-022 基线）

- primitives（`lib/**/*.css`，含 markdown 目录）消费 **55 个唯一 `--dsw-*`**；54 个由 sheets 定义；**唯一缺口 `--dsw-hovercard-bg`**——仅 `HoverCard.module.css` 消费，本应用未使用 HoverCard（frontend src 全目录 grep 零引用），官方栈中该 token 由 ui-layout 侧定义（不在 ui-theme sheets 内）。
- `theme.css` 消费 10 个唯一 `--dsw-*`，全部在 sheets 中；`:root` 手写的 11 个 `--dsw-*` 声明（`theme.css:7-17`）**全部被 sheets 同名覆盖**（值差异 = sheets 的 dark 值更贴近官方，属预期修复方向而非回归）。

## 2. 候选评估与决策

### 2.1 选定（a）：从安装包提取 vendor + 同步门禁

**方案**：frontend 声明 `@deepseek-ai/dsh-client-ui-theme: catalog:`（0.1.1-rc.2，与 primitives 同线）使 tarball 进入 node_modules；同步脚本从安装包 `lib/client.js` 提取五个 sheet，以**字节精确**形态 vendor 进 `src/dsh-theme/`（含生成 `index.css` 按官方顺序 `@import` 与溯源 `README.md`）；`src/dsh-theme.test.ts` 防漂移门禁在测试期独立重提取并与 vendored 文件比对（§3.6）。

**为何该形态是"token 唯一权威"目标的落地**：

1. **来源一致性**：提取源 = pnpm 按锁文件安装的 tarball 字节，与 primitives 在同一条 catalog 版本线上——不存在第二版本源；
2. **权威可执行**：vendored 文件与安装包字节一致由测试强制；catalog 升级而未重新同步时测试即失败（0.1.2-rc.1 实测会检出 sheet 集与内容变化）；"唯一权威"从口头约束变为 `bazel test` 门禁；
3. **顺序权威**：`index.css` 的引入顺序由 `STYLES` 数组生成，非人工记忆；
4. **工程零新机制**：vite 静态 import 普通 css 文件（`vite_build` 的 `glob(["src/**"])` 自动覆盖，无需 BUILD 改动）；vitest `css:false` 对新 import 零影响；文件内容断言沿用 `SessionList.test.tsx` 既有 readFileSync 模式（cwd 候选覆盖 bazel runfiles 与包目录两种执行环境）。

### 2.2 否决候选（constitution VII：必要时记录，防重复踩坑）

| 候选 | 内容 | 否决理由 |
|---|---|---|
| (b) cordis 客户端插件注入 | 经 `installThemeStyles(ctx)` 官方路径注入 | 前端需引入 cordis 客户端运行时 + 8 个 dsh peer 包（connection/runtime/locale/ui-settings/api-remotes/host-webserver/settings/invariants），而 frontend 是无插件宿主架构的 React 应用（[research.md](../research.md) D1 已否决 L4 完整插件栈，同一 rationale）；且 `./client` 入口本身是 ModuleLoader 格式、并无标准 import 通路。为注入五段静态 CSS 引入整套插件宿主，是 constitution II 意义上的不可论证复杂度 |
| (c) GitHub 源仓库获取（http_archive/子模块/手动 vendor） | 从 https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/client/ui-theme/src/styles 取 css | **版本不一致**：master 已漂移（6 个 css，含新增 `corner-shape.css`；`design-platform.css` 19020B vs 锁定版 15540B、`scrollbar.css` 4081B vs 565B，https://api.github.com/repos/deepseek-ai/deepseek-harness/contents/packages/client/ui-theme/src/styles 实读），与 catalog 锁定的 primitives 0.1.1-rc.2 线不匹配；按 commit 钉版本依赖 gitHead 考古且脱离 npm 版本语义；为 pnpm workspace 仓库引入第二依赖体系（http_archive/子模块），违背依赖统一 catalog 治理（`survey/deepseek-harness-b1-bazel-packaging.md` §4：dsh 闭包整体即一个版本单元） |
| (d1) 构建期 vite 插件提取 | vite.config 加插件，构建时解析 `lib/client.js` 生成虚拟 CSS 模块 | 构建期恒同步（无 vendored 副本、无同步步骤）是其优点，但：向 vite.config 注入字符串解析式的构建魔法，与仓库 vite 链路刻意保持薄的做法（`tools/dev/js/vite.bzl` 仅 cd + 调用）不一致；CSS 不以可 review 的文件形态存在（diff 不可审、排查多一跳）；FR-022 覆盖断言与 Menu 断言仍需在测试期自行提取（jsdom 无法消费虚拟模块）——最终同样要写提取逻辑，还额外背上构建机制。vendor + 门禁以一个生成脚本 + 一个测试换来构建零改动，复杂度更低且可审 |
| (d2) 手写补齐缺失变量 | 将 theme.css 的 11 个 `--dsw-*` 扩到 55 个 | D4 既有否决（"逐组件打补丁，遗漏面大且与官方漂移"）；量化后更不成立：55 个 token 的手抄副本无任何权威校验 |
| (d3) 等待上游修复打包 | 上游若按 `files` 声明真正产出 `lib/styles/*.css`，则退化为直接 import | 16 个已发布版本均未产出，无修复时间线；不可作为本 feature 的路径。作为**未来简化触发器**记录：若某版本开始产出真实 css 文件，同步脚本退化为复制，载体与门禁形态不变（§7） |

## 3. 终态设计

### 3.1 生成目录 `projects/game/web/frontend/src/dsh-theme/`

| 文件 | 内容 | 生成规则 |
|---|---|---|
| `base.css` / `design-platform.css` / `scrollbar.css` / `gradient-shadow-text.css` / `shiki.css` | 官方 token sheet 原文 | **字节精确**等于包内字符串常量（不加头注释——同步比对按字节；溯源集中于 README） |
| `index.css` | 依官方顺序的 `@import './<name>.css';` 逐行 | 顺序取自 `STYLES` 数组；vite（postcss-import）在 build/dev 两种模式内联相对 `@import`，无配置需求 |
| `README.md` | 溯源：来源包名、提取时的版本号、提取源（`lib/client.js` 内嵌字符串）、再生成命令、许可证（MIT）与上游链接 | 版本号取自安装包 `package.json`——版本即漂移标记 |

目录整体为**生成物**（git 提交生成结果以便 review 与构建确定性）；升级时的变更 = 重跑脚本后的 diff。

### 3.2 同步脚本 `projects/game/web/frontend/scripts/sync-dsh-theme.mjs`

- 无第三方依赖的 node 脚本；以 `import.meta.url` 定位包根，读 `node_modules/@deepseek-ai/dsh-client-ui-theme/lib/client.js` 与其 `package.json`；
- region 锚点正则提取 `{name → css}` 与 `STYLES` 顺序（§1.1 已验证），写 §3.1 全部文件；
- 手工执行（node 是 pnpm/vite 工具链的前置条件，仓库不另设 node 包装 target）：`cd projects/game/web/frontend && node scripts/sync-dsh-theme.mjs`；
- **升级流程**：改 `pnpm-workspace.yaml` catalog 版本 → `bazel run @pnpm -- --dir /mnt/code/dominion install` → 跑本脚本 → 提交 diff。未跑脚本时由 §3.6 门禁测试拦截。

### 3.3 引入与深色激活

- `src/App.tsx`（`theme.css` 的既有 import 所在，`App.tsx:11`）：在 `import './theme.css'` **之前**新增 `import './dsh-theme/index.css'`（`var()` 解理与顺序无关，先后仅表达 token→布局的意图；App 是全部 CSS 的既有入口，不引入第二入口）。
- `index.html`：`<body>` 置静态布尔属性 `data-ds-dark-theme`（深色单一主题，无切换面；与 sheets 的存在性选择器、官方 bootstrap 的 `toggleAttribute` 语义一致）。
- `theme.css` `:root` 新增 `color-scheme: dark;`（官方 bootstrap 对 html 的 `colorScheme` 与 body 属性成对设置；静态深色应用以 CSS 表达前者，UA 表单控件/滚动条基色随之正确）。

### 3.4 theme.css 删除面

- `:root` 删除 11 个手写 `--dsw-*` 声明（`theme.css:7-17`）；保留 `--app-bg`/`--app-panel`/`--app-border`（:19-21）并新增 `color-scheme: dark`；
- **消费面零改动**：theme.css 内 10 个唯一 token 的全部 `var(--dsw-*)` 消费（30+ 处）在 sheets 中均有定义（§1.3），custom property 计算期解析，无需改动任何布局规则；
- 头注释更新：`--dsw-*` 权威来源改为 `src/dsh-theme/`（token sheets），移除"清单照 049 契约手写"的旧表述。

### 3.5 依赖与 BUILD

- `projects/game/web/frontend/package.json` `dependencies` 增 `"@deepseek-ai/dsh-client-ui-theme": "catalog:"`（catalog 条目 Phase 1 已建，`pnpm-workspace.yaml:24`，0.1.1-rc.2）。归入 `dependencies` 而非 devDependencies：vendored 产物即该包的发布内容、与 primitives 同线配对在同一 section 可见（运行时不 import 该包，两种归类对安装无差异，此为表述选择）；
- `bazel run @pnpm -- --dir /mnt/code/dominion install` 更新 lock（禁手改 `pnpm-lock.yaml`）；
- `bazel run //:gazelle projects/game/web/frontend` 生成 `:node_modules/@deepseek-ai/dsh-client-ui-theme` link target；
- `projects/game/web/frontend/BUILD.bazel` 的 `vitest_test` `data` 增该 target（门禁测试需读安装包文件；沿用 primitives 的 data 先例，`BUILD.bazel:49` 一带）。

### 3.6 防漂移门禁 `src/dsh-theme.test.ts`

四组断言（提取逻辑在测试内**独立重实现**，不 import 同步脚本——共享模块会使测试对提取器自身缺陷免疫，独立重推导才是真校验）：

1. **字节同步**：从安装包 `lib/client.js` 提取的 sheet 集与 `src/dsh-theme/*.css` 逐文件字节相等；文件集不一致（多/少文件）同样失败——捕获 sheet 集变化（0.1.2-rc.1 增第 6 个 sheet 的情形）；
2. **顺序**：`index.css` 的 `@import` 行序 === 安装包 `STYLES` 顺序；
3. **版本一致**：`src/dsh-theme/README.md` 所记版本 === 安装包 `package.json` 版本；
4. **FR-022 覆盖**：primitives `node_modules/@deepseek-ai/dsh-client-ui-primitives/lib/**/*.css` 与 `src/theme.css` 消费的全部 `--dsw-*` ⊆ vendored sheets 定义集；豁免表 `['--dsw-hovercard-bg']`（HoverCard 未使用，官方由 ui-layout 定义——注释说明）。

文件定位沿用 `SessionList.test.tsx` 的 cwd 候选模式（bazel runfiles 根 + 包目录两种执行环境）；安装包路径以候选 `projects/game/web/frontend/node_modules/...` / `node_modules/...` 解析。

失败信息指向再生成命令（§3.2），形成"升级必同步"的闭环。

### 3.7 README（attribution）

`projects/game/web/frontend/README.md`：Attribution 节增 vendored theme sheets 条目（来源包 + MIT + 上游仓库链接 `https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/client/ui-theme`）；顺带修正"依赖 pin 决策"节中 primitives"直接 pin（catalog 例外）"的过时表述（Phase 1 已迁 catalog，与 T020 无关但同节失实，一并纠正）。

## 4. 测试义务（T020 内嵌，constitution IV）

| 层 | 文件 | 断言 |
|---|---|---|
| 防漂移 | `src/dsh-theme.test.ts`（新增） | §3.6 四组：字节同步（含文件集）、@import 顺序=STYLES、README 版本=安装包版本、FR-022 消费覆盖+豁免表 |
| Menu 视觉 | `src/components/SessionList.test.tsx` | （i）vendored sheets 定义 Menu 卡片消费的三 token（`--dsw-specific-menu`/`--dsw-alias-border-inverted`/`--dsw-shadow-lv3`——union 面，dark 专属值在 dark 块）；（ii）sheets 含 `body[data-ds-dark-theme]` 激活选择器；（iii）`index.html` 的 `<body>` 含 `data-ds-dark-theme` 属性（readFileSync 内容断言） |
| 手写权威清除 | 同上 | `theme.css` 内容不再含任何 `--dsw-*:` 声明（`--app-*` 与 `color-scheme` 保留断言可选） |
| 回归 | 既有全部用例 | theme.css 布局断言（`SessionList.test.tsx:169/:306-308` 等）与组件用例零回归；`bazel test //projects/game/web/frontend/...` 全绿 |

**断言面说明（jsdom 约束）**：jsdom 不应用外部样式表、vitest 默认 `css:false` 将 .css 模块替换为空串、且 jsdom 的 `getComputedStyle` 不解析 custom property `var()` 引用——计算样式断言不可达；文件内容断言（既有 loadThemeCss 模式扩展到 sheets/index.html）是本仓库可行断言面上限，契约 §1 验收按此表述。真实视觉效果由 Phase 10 Independent Test 的人工浏览器目验兜底。

## 5. 对 tasks/契约/调研的修订说明（已随本文应用）

| 文档 | 修订 |
|---|---|
| [contracts/web-ui.md](../contracts/web-ui.md) | §1 表整体替换为终态（载体/主题形态/自有样式/验收四行）；"官方对齐基线"中 ui-theme bullet 标注 README `src/styles/` 描述与 tarball 实态不符、载体见 §1 与本文 |
| [tasks.md](../tasks.md) | T020 按 §3 六步重写（依赖/脚本与生成目录/引入与激活/删除面/BUILD/测试与核查）；Phase 10 文档清单增本文与 D4 修正注；Independent Test 增同步门禁口径 |
| [research.md](../research.md) | D4 追加载体重设计修正注（指向本文）；开放项表中"dark 激活选择器"行标记已由本文 §1.2 落定 |

spec.md 零修改（A6 本就将引入方式留给 plan/执行阶段决策，验收只约束视觉结果）。

## 6. 下游执行指引（分步可恢复）

1. **依赖**（§3.5）：package.json 声明 → pnpm install → gazelle → BUILD data；验证：`ls node_modules/@deepseek-ai/dsh-client-ui-theme/lib/client.js` 存在。
2. **生成**（§3.1–3.2）：写同步脚本并执行，产出 `src/dsh-theme/`（5 css + index.css + README）；人工抽查 1-2 个文件与 tarball 内容一致。
3. **接入**（§3.3–3.4）：App.tsx import、index.html body 属性、theme.css 删除面与头注释。
4. **门禁与断言**（§3.6/§4）：`dsh-theme.test.ts` + SessionList 视觉断言；README attribution（§3.7）。
5. **验证**：`bazel test //projects/game/web/frontend/...` 全绿（含 `:dist` 构建 `bazel build //projects/game/web/frontend:dist`）；浏览器目验 `···` 菜单卡片（背景/边框/阴影）与既有组件无视觉缺失。
6. 每步均可独立提交/中断恢复；步骤 1 未完成时步骤 2 脚本无法运行（无 node_modules 来源），步骤 4 未完成时无防漂移门禁（不阻塞视觉修复本身，但交付定义包含门禁）。

## 7. 上游简化触发器（记录，非本 feature 事项）

若上游未来版本按其 `files` 声明真正产出 `lib/styles/*.css`（或恢复 `src/` 发布），同步脚本退化为复制、`index.css` 顺序改读目录清单——载体（vendored + 门禁）与全部断言形态不变，仅提取实现简化。触发时无需重开设计。
