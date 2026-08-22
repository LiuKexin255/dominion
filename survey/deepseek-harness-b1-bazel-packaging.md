# 调研：B1 服务原语与 Bazel 打包（dsh 底座 × 插件 × 组合清单）

> **状态**：调研完成。**已锁定决策（2026-08-22，用户确认，以 B1 采纳为前提）**：① dsh baseline 仅含**框架核心**（app-boot + cordis 家族 + native addon，≈10 包），全部插件（含官方）按需添加（§5.4.1"精确混合"粒度）；② 支持在 `third_party/dsh` 下定义**多个 baseline target**（框架核心 + 场景 baseline 二次包装，§5.6）；③ 产物形态为**单 tar**（与现有 `artifact_pkg_js` 一致，不做底座/服务分层，§5.6.3 记录否决理由）。整体 B1 采纳/迁移决策仍未做出。
> **日期**：2026-08-22
> **前置调研**：`survey/deepseek-harness-b1-plugin-packaging.md`（B1 插件机制与打包分发：boot 管线、Loader 四路径解析、`bareModuleBaseUrl` 模式 A/B、官方三条打包路线、§6 底座×插件拆分框架；本文简称 **B1 调研**）、`survey/deepseek-harness-integration-modes.md`（A/B1/B2 拓扑）
> **研究问题**：假设 B1 可行（业务进程内 `boot()` 组装插件树），一个嵌入 dsh 的服务最终由什么"原材料"构成？设想的形态——"dsh 作为 base layer + 插件列表 + 配置文件，通过 bazel rule 打包到一起"——是否可行？若可行，rule 应长什么样、与本仓库现有 JS 构建（`artifact_pkg_js`/`artifact_image`）能收敛到何种地步？
> **说明**：本文为纯调研材料，含一个方向性分析（rule 架构候选对比），不含采用决策。所有本仓库事实均经源码阅读 + 构建产物实证（2026-08-22）。

---

## 1. 结论先行（TL;DR）

**思路可行，且本仓库现有链路已隐式验证了其中最难的一环；底座封装不需要任何新机制——照 `common-js-otel` 模式做一个"闭包清单 workspace 包"即可。**具体结论：

1. **原材料 = 四类**（§2）：① dsh 底座闭包（精确 pin 的 node_modules 包集合，"解析面"）、② 组合清单 `cordis.yml`（"启用面"）、③ 自研插件包（可选）、④ 服务入口 bootstrap（`boot()` + 进程生命周期）。运行时不再需要任何别的输入——没有 profile、没有 `$DSH_HOME`、没有 `dsh plugin add`。
2. **打包机制已被本仓库实证**（§3）：现有 `artifact_pkg_js` 产物 tar 里，~26 个直接 npm 依赖 link target 物化出 **172 个顶层包**的完整传递闭包（标准 `node_modules/<pkg>/` 布局、无 pnpm 虚拟存储残留）——rules_js link target 收集 + `cp -aL` 拍平的机制**今天就在生产链路上工作**。dsh 的 ~100 包闭包走同一机制，没有原理性障碍。
3. **底座的正确封装形态 = 闭包清单 workspace 包**（§5.1，用户"逻辑层"直觉的落地）：一个零代码 workspace 包（package.json 精确 pin）+ `js_runtime_library(npm_deps=[link target 枚举])`；服务侧 `runtime_deps = ["//third_party/dsh/core:runtime_pkg"]` **一行引用**，与今天引用 `//common/js/otel:runtime_pkg` 完全同构。不是 grpc-js 式"一个 target 传递全闭包"（实证：link target 只按直接依赖生成、workspace 包 target 只含自身文件），而是仓库既有的 runtime_pkg 模式。**已锁定（2026-08-22）**：baseline = 框架核心 ≈10 包；插件全部按需（服务级或场景 baseline 级，§5.6 多 baseline 分层零改动成立）。
4. **`dsh_pkg` 作为 `artifact_pkg_js` 的变体 macro 成立**（§5.3）：最小路径下甚至**零新 rule**——closure 走 `runtime_deps`、cordis.yml 走 `data_files`、bootstrap 走 `ts_project`，`artifact_pkg_js` 原样可用（**产物单 tar，与现状一致——已锁定**）；macro 的增量价值是①组合清单作为一等属性 ②闭包校验 target 内联展开。
5. **与现有 JS 构建收敛度：~95%**（§6）：构建图几乎全复用（`npm_translate_lock`/`npm_link_all_packages`/`ts_project+swc`/`JsRuntimePackageInfo` 闭包走行/`artifact_image` 全套/`service.yaml` 部署约定）；新增仅闭包清单包（+ 可脚本生成的 BUILD 枚举）、校验 target、bootstrap 模板、可选的 `dsh_pkg` macro。
6. **闭包粒度可按需收缩（§5.4）**：官方插件无特权、可移出 baseline 作为服务级依赖（"精确混合"粒度：底座收缩为框架核心 ≈10 包）；框架核心零 agent 能力，最小 chat demo = **两行组合（agent spine 行 + LLM adapter 行）+ 零服务面**，物理闭包 ≈40±10 包（spine 26 peers + adapter 12 peers 实证）；纯 chat 的最大实际限制是**无 compaction**（长会话无限增长）与无会话持久化。
7. **多 baseline 二次包装零改动成立（§5.6）**：场景 baseline = `js_runtime_library(runtime_deps=[core], npm_deps=[增量])`——`_collect_runtime_closure` 的 BFS 天然穿透继承；`third_party/dsh/` 下 core/coding/chat 多 target 并存，项目私有组合可就地定义。新增两个校验面：物化集合同名包版本唯一（Phase 3 后写胜出的静默覆盖风险）、场景闭包 peer 到不动点。

---

## 2. 原材料模型：一个 B1 服务的最终镜像解剖

### 2.1 最终形态（单前缀单树布局）

```
/dominion/<app>/<service>/                 ← 与现有 artifact_image 约定一致的前缀
├── node_modules/                          ← 原材料①：dsh 底座闭包（解析面）
│   ├── @deepseek-ai/dsh-app-boot/
│   ├── @deepseek-ai/…                     （组合所需官方插件 + 全部 peer 闭包，精确 pin）
│   └── node-addon-require-builtin/        （native addon；平台 optionalDeps 随闭包）
├── plugins/                               ← 原材料③：自研插件（可选）
│   └── <own-plugin>/
│       ├── package.json                   （dsh 相关依赖声明为 peer，不实装）
│       ├── node_modules/                  （可选嵌套：插件私有依赖，双树模型 §6.5 退化进单前缀）
│       └── lib/index.js                   （ESM 插件入口）
├── cordis.yml                             ← 原材料②：组合清单（启用面：每行 {id, name, config}）
└── src/bootstrap.js                       ← 原材料④：服务入口（boot + 信号/dispose 生命周期）
```

产物形态：**单个 tar**（`artifact_pkg_js` 现有输出形态，2026-08-22 锁定，不做底座/服务分层）——上述四类原材料物化于同一 `/dominion/<app>/<service>/` 树内，由 `artifact_image` 作为单层挂载进 OCI 镜像。

### 2.2 四类原材料的职责与来源

| 原材料 | 职责 | 变更频率 | 来源（bazel 视角） |
|---|---|---|---|
| ① 底座闭包 | 决定**什么可被解析**（B1 调研 §6.1 解析面） | 随 dsh 版本升级（整体动作） | pnpm workspace 闭包清单包 + 根 `pnpm-lock.yaml`（版本唯一源）→ `npm_translate_lock` store → link targets |
| ② cordis.yml | 决定**什么实际挂载**（启用面）+ 每行 config | 随业务组合调整 | 服务目录内数据文件（现有 `data_files` 机制即可携带） |
| ③ 自研插件 | 业务扩展能力 | 随业务版本 | workspace TS 包（`ts_project` + `js_runtime_library`，现有机制） |
| ④ bootstrap | `boot(NAME, configPath, …, anchor)` + SIGTERM/SIGINT → dispose + 健康退出 | 几乎不变 | 服务入口 TS（现有 `entrypoint` 机制；jsonrpc-demo runner 为官方样板，B1 调研 §2.1） |

**安装 ≠ 启用**（B1 调研 §6.1）：闭包里装了但未在 YAML 启用的包不占 fiber、不注册服务、不进 prompt（jsonrpc-agent 的 minimal 变体正是跑在装了 ~100 包的 bundled runtime 里的）——解析面超集只付出镜像体积，不付运行面复杂度。这正是锁定决策（§5.4.1 精确混合）敢于把插件按需化的机制前提。

### 2.3 锚定验证：每条运行时解析路径落在哪里

对照 B1 调研 §2.2 四路径，逐条验证 §2.1 布局下的解析落点：

| 运行时解析需求 | 通道 | 锚点 | 落点 | 验证 |
|---|---|---|---|---|
| YAML 官方插件行（裸包名 `@deepseek-ai/dsh-*`） | ② internal loader + `bareModuleBaseUrl` | bootstrap 文件位置 | `<service root>/node_modules/` | ✓ bootstrap 在服务根，向上一步命中 |
| YAML 自研插件行（相对路径 `./plugins/<pkg>`） | ③ 相对路径 | cordis.yml 所在目录 | `<service root>/plugins/<pkg>` | ✓ 同目录 |
| 自研插件内部 `import '@deepseek-ai/dsh-tools'` | 普通 Node ESM 向上解析（不经 Loader） | 插件模块文件位置 | 先查插件嵌套 node_modules（若有，peer 不实装则 miss）→ 向上命中 `<service root>/node_modules/` | ✓ plugins 在服务根下 |
| 插件私有依赖 | 同上 | 同上 | 插件嵌套 `node_modules/` | ✓ |
| bootstrap `import '@deepseek-ai/dsh-app-boot'` | 普通 Node ESM 向上解析 | bootstrap 位置 | `<service root>/node_modules/` | ✓ |

**关键布局约束**：底座 `node_modules` 必须位于 bootstrap 与 plugins/ 的**公共祖先目录**（即服务根）。这不是 `bareModuleBaseUrl` 的限制（它可传任意 URL，底座理论上可放别处），而是插件内部 import 走 Node 标准向上解析所要求的——单前缀单树是最简且无例外的布局。

### 2.4 一个集成细节：入口模块格式

- 现有服务链路编译为 **CJS**（`projects/game/agent/.swcrc` 的 `module.type: commonjs`；tar 内无服务根 package.json）。
- dsh 包全部 ESM（自带 `"type": "module"` 的 package.json；实证 §3.2：npm 包的 package.json 在收集范围内，随包物化）。CJS 入口 `await import()` ESM 包是标准路径，不受影响。
- **锚点的两条路**：ESM 入口用 `import.meta.url`；CJS 入口用 `pathToFileURL(__filename).href`（`boot` 的第 5 参只要求一个 URL 字符串）。自研插件建议编译为 ESM（与 dsh 生态一致；Loader 的 `unwrapExports` 亦有 CJS interop，B1 调研 §2.4）。
- **native addon 前提不变**（B1 调研 §4.1）：裸包名稳定解析依赖 `node-addon-require-builtin` 可加载——PoC 必须在目标镜像（distroless nodejs24-debian12，glibc 2.36）内实测。

---

## 3. 现有 JS 打包链路的事实（含构建产物实证）

### 3.1 链路全貌

（`MODULE.bazel`、`tools/release/defs.bzl`、`projects/game/agent/BUILD.bazel`）

```
根 pnpm-lock.yaml ──npm_translate_lock──▶ @npm 虚拟存储（node_modules/.aspect_rules_js/<name>@<ver>/...）
                                                   │ npm_link_all_packages
                                                   ▼
服务 BUILD.bazel: :node_modules/<pkg> link targets（每包一个）
                                                   │
ts_project(+swc) ─▶ :lib ─┐                         │
js_runtime_library 包闭包 ├─▶ artifact_pkg_js ─▶ tar ─▶ artifact_image ─▶ OCI（base=distroless_nodejs24）
runtime_protos ───────────┘   （五阶段：ts 文件 / workspace 闭包 / protos / npm 拍平 / data_files）
```

关键事实：

- **版本唯一源已经是根 `pnpm-lock.yaml`**：`npm.npm_translate_lock(pnpm_lock = "//:pnpm-lock.yaml")`（MODULE.bazel:128）。dsh 全家桶 pin 进 lockfile 后 store 与 link targets 自动可用——**不需要任何新的依赖供给机制**。
- `artifact_pkg_js` 的 Phase 3 对每个 npm dep target 的文件做两步变换：定位 `short_path` 中第一个 `node_modules/`，然后**重写 `.aspect_rules_js/<name>@<ver>/` 虚拟存储前缀**为标准布局，最终 `cp -aL`（**解引用 symlink**）实体化（defs.bzl:446-471、533-548）。
- `artifact_image` 的 js 约定：`ENTRYPOINT=/nodejs/bin/node`、`CMD=/dominion/{app}/{service}/{entry}`（base = distroless_nodejs24-debian12，已通过 `oci.pull` 拉取）。dsh 服务沿用同一约定即可。

### 3.2 构建产物实证（2026-08-22，`//projects/game/agent:server_pkg`）

对现有产物 tar 的直接检查结果：

| 检查项 | 结果 |
|---|---|
| 顶层包数量 | **172 个**（由闭包走行收集的 ~26 个直接 npm link target 物化而来：agent 自身 14 个 `npm_deps` + `runtime_deps` 各 workspace 包（otel 12 个等）的 `npm_deps`） |
| express 传递依赖 | `accepts`/`body-parser`/`cookie`/`serve-static` 的 package.json + 代码**全部在场**，标准 `node_modules/<pkg>/` 布局 |
| pnpm 虚拟存储残留 | 仅 49 条，全部是 `@langchain/langgraph-sdk` **发布包自带的**嵌套 `dist/node_modules/.pnpm/`（包内容如此，非拍平缺陷） |
| npm 包 package.json | 在收集范围内（`node_modules/<pkg>/package.json` 均在场）——dsh 包的 `type: module`/`exports` 元数据完整保留 |

**机制解释**：rules_js 的 `:node_modules/<pkg>` link target，其 `DefaultInfo.files` 覆盖该包在虚拟存储中的子树（含其依赖的 symlink 布局），`cp -aL` 解引用后即得到"该包 + 其传递依赖"的实体化标准树。

### 3.3 link target 生成与传递语义（2026-08-22 cquery 实证）

三个决定"底座封装形态"的事实：

| 事实 | 实证 | 含义 |
|---|---|---|
| **link target 只为直接声明的依赖生成** | `//projects/game/agent:node_modules/accepts` **不存在**（accepts 是 express 的传递依赖，非 agent 直接依赖）；agent BUILD 列的 14 个 npm_deps 恰好是 agent package.json 的直接依赖 | 传递依赖**不是**独立 target，而是经直接依赖的 link target files 携带物化（172 包实证） |
| **registry 包 link target 的 files 携带传递闭包** | 14 个直接 target → 172 个顶层包全部物化 | 枚举"组合直接需要的包"即得全闭包——不需要递归枚举 |
| **workspace 包 link target 只含自身源文件** | `:node_modules/@dominion/common-js-config` 的 files = **13 个文件**（该包源码），不含其任何 npm 依赖 | workspace 包不自带依赖；**仓库既有解法** = `js_runtime_library(npm_deps=[直接依赖 link target])`，由消费方 `runtime_deps` 闭包走行收集 |

**仓库既有模式**（`common/js/otel/BUILD.bazel`）：workspace 包的 BUILD 在 `js_runtime_library.npm_deps` 里列**自己的直接 npm 依赖**（otel 列 12 个 `@opentelemetry/*`）；服务侧只需 `runtime_deps = ["//common/js/otel:runtime_pkg"]` 一行，`_collect_runtime_closure` BFS 自动收集其 npm_deps。**这正是"底座作为逻辑层被单点引用"的现成机制**（§5.1）。

**对本调研的含义**：dsh 底座闭包（~100 包）以"闭包清单 workspace 包"形态进入该机制即可——package.json 精确 pin 全部依赖（pnpm 侧单点）、BUILD 的 `npm_deps` 枚举全部 link target（bazel 侧单点，可从 package.json 脚本生成）。原先的"peer 链接语义"风险（§7 风险 1）因此**基本消解**：全部包直接枚举 ⇒ 每个包自身的文件必然物化，peer 是否被 pnpm 链进某包的 store 子树不再影响完整性；该问题退化为"能否少枚举"的优化题（PoC 可验证裁剪）。

### 3.4 官方文档佐证（rules_js）

- `npm_translate_lock` 消费 pnpm-lock.yaml 自动生成 `npm_import`，是官方推荐的整 lockfile 导入方式（[docs/pnpm.md](https://github.com/aspect-build/rules_js/blob/main/docs/pnpm.md)）；lockfile 更新走 `bazel run @pnpm//:pnpm --dir $PWD install --lockfile-only`——与本仓库 AGENTS.md 的 pnpm 流程同构。
- "运行时 require 由 `data = [":node_modules/<pkg>"]` 满足"是官方声明的模式（[docs/troubleshooting.md](https://github.com/aspect-build/rules_js/blob/main/docs/troubleshooting.md)）；`artifact_pkg_js` Phase 3 与其同源。
- `lifecycle_hooks` 默认对包跑 preinstall/install/postinstall（no-sandbox）。dsh 生态包均无 install script（含 `node-addon-require-builtin`，0.1.2 起已移除，B1 调研 §4.1 更新）——无 hook 执行面风险。

---

## 4. 版本与依赖治理输入（B1 调研结论在本仓库的落点）

1. **全家桶精确 pin、不用 dist-tag**（B1 调研 §4.2/§4.3：#1032 实证部分包 latest 停在旧线；prerelease semver range 不跨 minor 匹配）。pnpm workspace 的 `pnpm-lock.yaml` 天然承载精确 pin——**本仓库的 lockfile 流程（AGENTS.md）与该要求完全同构，无新增机制**。
2. **catalog 治理的例外**：宪法要求 TS 依赖版本统一在 `pnpm-workspace.yaml` catalog；dsh 闭包是 60-100 个版本互锁到同一 rc 线的包集合，逐包入 catalog 是噪音——闭包清单包内**直接精确版本**（`"x.y.z-rc.n"` 无前缀）+ 文档记录为 catalog 例外，更符合 catalog"统一治理"的本意（闭包整体即一个版本单元）。
3. **闭包校验是硬门禁**（B1 调研 §7 风险 8）：官方 npx 路线在干净环境的断裂证明安装器不会代劳。bazel 侧以校验 target 承接（§5.3-②）：pin 一致性、组合 YAML 行 ⊆ 物化 node_modules、每个列出包的非可选 peer 在场（`verify-runtime-closure` 思路，B1 调研 §3.2 第 4 步）。

---

## 5. Rule 架构候选

### 5.1 底座封装：闭包清单 workspace 包（"逻辑层"的落地形态，回答问题 1）

> **决策落点（2026-08-22 锁定）**：此处的闭包清单 = **框架核心 ≈10 包**（见 §5.4.2），不含官方插件；插件按需声明在服务或场景 baseline（§5.6）。下列结构按此收缩。

**结论：不需要新的打包机制，也不需要单独的供给机制——底座就是一个普通的 workspace 包，照 `common/js/otel` 的既有模式做。**与 grpc-js 的类比精确成立：grpc-js 的传递依赖（accepts 等）不在任何 BUILD 里枚举，由 express/…的直接 link target 携带物化；dsh 底座同理，只是"直接依赖列表"本身是一个包的 package.json：

```
third_party/dsh/core/                   ← 框架核心 baseline（唯一枚举点，决策后收缩为 ~10 包）
├── package.json                        ← pnpm 侧单点：框架核心精确 pin
│                                          （app-boot + cordis/loader/include/group[/timer] + 4 个 dsh peers 包
│                                           + node-addon-require-builtin；catalog 例外，§4-2）
├── BUILD.bazel                         ← bazel 侧单点：
│   js_runtime_library(
│       name = "runtime_pkg",
│       package_name = "@dominion/dsh-core",
│       lib = ":version",               ← 琐碎 target（导出 pin 快照号；js_runtime_library 要求 lib）
│       npm_deps = [                    ← ~10 个 link target 枚举（可从 package.json 脚本生成）
│           ":node_modules/@deepseek-ai/dsh-app-boot",
│           ":node_modules/@deepseek-ai/cordis",
│           …
│       ],
│   )
└── version.ts                          ←（可选）导出核心快照标识，运行时可自省底座版本
```

服务侧引用（**一行**，与今天引用 otel 完全同构；用到的插件另行声明，§5.4.1 精确混合）：

- pnpm：服务 package.json 声明 `"@dominion/dsh-core": "workspace:*"` + 用到的 dsh 插件（spine、adapter 等，与服务代码直接 import 的包按仓库惯例照常声明——link target 只为直接依赖生成，§3.3 实证）；
- bazel：`artifact_pkg_js(runtime_deps = ["//third_party/dsh/core:runtime_pkg", "//plugins/…:runtime_pkg", …], npm_deps = [用到的 dsh 插件 link targets])`——`_collect_runtime_closure` BFS 自动聚合核心包 + 服务增量。

**为什么必须显式枚举——与 grpc-js 的三层差异**（"传递携带"机制本身对任何包都成立，差异在声明形态与启用语义）：

1. **机制层（两者相同）**：link target 的 files 携带的是该包在 store 子树中的内容 = 该包的**普通 dependencies**（物理布局决定）。grpc-js 的 `@grpc/proto-loader`/`@js-sdsl/ordered-map` 是普通 deps（store 实证），express 的 accepts/body-parser… 同理——所以一个 target 传递全闭包（172 包实证）。
2. **dsh 声明形态（设计使然，非机制缺陷）**：dsh 家族包互相声明为 **peerDependencies**（npm 元数据实证：`dsh-app-boot` 的 dependencies 仅 `js-yaml`，9 个 dsh/cordis 包全在 peers）。pnpm 严格布局下 peers 从祖先解析、不进被依赖者子树 ⇒ app-boot 的 link target files ≈ app-boot + js-yaml，传递携带结构上失效。这正是官方 **#1032 事故的根因**（app-boot 静态 import `cordis-plugin-group`（peer）→ npx 干净环境不传递 → `ERR_MODULE_NOT_FOUND`；官方 deploy 脚本显式 `--config.auto-install-peers=false`）。"消费者组装闭包"是 dsh 的刻意设计（B1 调研 §4.2 引文），代价由消费者承担。
3. **启用面不在依赖图里**：组合要启用的插件（`dsh-tool-todo`、`dsh-llm-deepseek`…）**不是 app-boot 的依赖（任何形式都不是）**——它们由 cordis.yml 行启用。"哪些包可被解析"（解析面）是策略决定，从任何单包的依赖图都推导不出来。所以闭包天然是一个 **manifest**——官方自己的做法就是零代码纯依赖清单包（`python/sdk-runtime/package.json`，127 行全量枚举，B1 调研 §3.2）。

即：grpc-js 类比在"枚举直接依赖"这点上仍成立（agent 的 package.json 也列了 14 项直接依赖）；特殊的是 dsh 的"直接依赖集合"（= 解析面闭包）有 ~100 项、版本互锁——因为它的家族依赖全 peer 化、且启用面独立于依赖图。

**枚举全量 vs 传递裁剪**：全量枚举（至闭包不动点）保证每个包自身文件必然物化，peer 语义无关紧要（§3.3）；若 PoC 证实 pnpm（v8+ 默认 `auto-install-peers=true`）把 peers 部分链入子树，可裁剪清单——纯优化项，不阻塞。

### 5.2 打包形态：最小路径 = 零新 rule

在 §5.1 之上，dsh 服务用**现有 `artifact_pkg_js` 原样**即可打包：

```
artifact_pkg_js(
    name = "agent_pkg",
    app = "game", service = "agent",
    ts_project = ":lib",                       # bootstrap.ts + 业务代码（现有机制）
    entrypoint = "src/bootstrap.js",           # boot(NAME, config, …, anchor) + 信号 dispose
    runtime_deps = [
        "//third_party/dsh:runtime_pkg",       # 底座闭包（一行）
        "//plugins/my-plugin:runtime_pkg",     # 自研插件（现有 workspace 包机制）
        "//common/js/otel:runtime_pkg", …      # 其他公共包照旧
    ],
    npm_deps = [ ":node_modules/@deepseek-ai/dsh-app-boot", … ],  # 服务直接 import 的包（typecheck/编译一致性）
    data_files = ["cordis.yml"],               # 组合清单（现有机制）
)
artifact_image(name = "cmd_image", …)          # 原样：distroless_nodejs24 + /dominion/a/s/entry
```

单 tar 内含：node_modules（底座闭包 ∪ 服务依赖 ∪ 插件闭包，均经 Phase 3 拍平到服务根）+ cordis.yml + 编译产物——恰好是 §2.1 的原材料布局（plugins/ 以 `node_modules/<plugin>` 形态在场，YAML 行写裸包名即可；或插件走相对路径布局见 §2.3，两种都成立）。**这是可行性的最小证明：现有构建图无需任何改动就能产出 B1 服务镜像。**

### 5.3 `dsh_pkg`：`artifact_pkg_js` 的变体 macro（回答问题 2）

在最小路径之上，macro 提供两项增量（按需取用，非可行性前提；**产物形态为单 tar，与现有 `artifact_pkg_js` 一致——2026-08-22 锁定，不做底座/服务分层**）：

**① 组合清单一等属性**：`composition` 显式声明（而非混在 data_files），供校验与文档化。

**② 闭包校验 target 内联展开**（每个 dsh_pkg 自动附带 `name + "_closure_test"`）：校验 cordis.yml 的每个裸包名行 ⊆ 底座物化集合；遍历闭包清单每个包的非可选 peer ⊆ 物化集合；校验 pin 同 rc 线（`verify-runtime-closure` 思路，B1 调研 §3.2 第 4 步；静态、离线、跑在 tar/清单上）。

### 5.4 闭包粒度：官方插件按需打包 与 最小 chat 能力距离（2026-08-22 补充调研）

#### 5.4.1 问题一：官方插件能否移出 baseline、按需打包？——能，且有两种粒度

机制上**没有任何东西赋予官方插件特权**：它们与自研插件同为 npm 包、同由 cordis.yml 行启用（B1 调研 §6.1"dsh vs 插件无硬边界"）。baseline（闭包清单）本就是**我们自己的工件**——取多少是策略问题，即 B1 调研 §6.3"全集闭包 vs 精确闭包"的选择，两种落地粒度：

| 粒度 | 底座闭包清单包含 | 服务侧 | 加官方插件 | 镜像体积 |
|---|---|---|---|---|
| **全集**（官方 runtime wheel 模式） | 全部 ~100 包 | 只改 YAML | 只改 YAML | 大 |
| **精确混合（2026-08-22 已锁定）** | 仅框架核心 ≈10 包（见 §5.4.2）：app-boot + cordis 家族 + addon | 用到的插件（spine、adapter、tool-*、persistence）声明进**服务自己的 package.json / `npm_deps`** 或引用**场景 baseline**（§5.6）——与 grpc-js 等普通依赖同地位，`runtime_deps`/Phase 3 机制原样消费 | 服务依赖 + YAML 各加一行 | 按服务最小 |

- 精确模式的约束不变：**启用行 ⊆ 物化集合**（每个启用插件 + 其 peer 闭包到不动点）——由 §5.3-② 校验 target 保证；peer 互锁版本由单 lockfile 统一承载（dsh 家族同 rc 线，服务级声明也不破坏一致性，因为版本解析全走同一 `pnpm-lock.yaml`）。
- 官方先例只有"装全集、启用 12 行"（jsonrpc-agent minimal 跑在 ~100 包 runtime 里）；无官方"精确闭包分发"先例，但机制完全支持——`安装 ≠ 启用`的反向（`启用 ⊆ 安装`）正是闭包校验的职责。
- 与 §5.1 的关系：`third_party/dsh` 闭包清单包可随之**收缩为"框架核心包"**（~10 项枚举），插件下沉为服务依赖；§5.1 的单点引用模式不变。

#### 5.4.2 问题二：只剩框架核心，距"最小 chat 服务 demo"还差多少？

**框架核心（不可再少的底座，≈10 包）**：`cordis` + `cordis-plugin-loader/include/group`（+timer，见下）+ `node-addon-require-builtin` + `dsh-app-boot` + 其 peers 中的 4 个 dsh 包（home-paths/invariants/system-prompt/launch-environment）。这层之上 `boot()` 空 cordis.yml 会**成功启动但零能力**——一个没有注册任何服务的 Context（"A config without dsh-sdk-jsonrpc-server is valid and serves nothing" 的更极端情形，B1 调研 §3.1）。

**从零能力到最小 chat，官方量度的差 = 两行组合 + 零服务面**：

| 增量 | 内容 | peer 代价（npm manifest 实证，0.1.1-rc.2） |
|---|---|---|
| ① agent spine 行 | `dsh-agent-spine-demo`：agent 服务 + 会话 + 主循环 + persona/system-prompt + LLM 路由 + retry + 会话标题（"executor-less/UI-less agent spine"，官方无人值守组合的核心行） | **26 项 peers**：dsh-agent、dsh-agent-loop、dsh-agent-instructions、dsh-goal、dsh-goal-round-driver、dsh-scope、dsh-session、dsh-session-title、dsh-skill、dsh-skill-filesystem、dsh-jobs-local、dsh-shell-env、dsh-tools、**dsh-tool-bash/skill/jobs/goal（工具包，config 关闭也必须在场——spine 静态导入、按 config 条件挂载）**、dsh-llm-retry、cordis-plugin-timer、cordis、llm、invariants、home-paths、system-prompt 等 |
| ② LLM adapter 行 | `dsh-llm-deepseek`：真实模型 wire（chat-completions + Files API） | **12 项 peers**：dsh-llm、dsh-timeout、dsh-settings、dsh-attachment、dsh-credentials、dsh-anonymous-user-id、dsh-atomic-write、dsh-brand、dsh-launch-environment、dsh-home-paths、dsh-invariants、cordis（+deps：eventsource-parser、schemastery） |
| ③ 服务面 | **0**——B1 的结构性优势：业务进程直接 `ctx.agents` 驱动（`agents` 服务由 spine 提供，jsonrpc-server 只是消费者之一）；要标准 wire 才需 `dsh-sdk-jsonrpc-server` 行 | （B2 对照：jsonrpc-server + sdk-protocol 等数包） |

**合计 ≈ 40±10 包**（两行的新增 peer 并集 ≈ 30 项，与框架核心有重叠；±10 覆盖 dsh-session/dsh-agent 等**二阶 peers**未逐包展开——PoC 时以闭包校验 target 迭代到不动点为准），对照官方全集 ~100 包。即：**概念上差"两行 YAML"；物理上差约 30 个包；服务面差零**。

**"纯 chat"明确放弃的能力**（对照官方 full 组合，按体感排序）：
1. **compaction/上下文压缩**——官方 minimal 组合注释明示"Runtime-context injection and context compaction are absent"：长会话上下文无限增长直至超窗，**这是纯 chat 最大的实际限制**；
2. **会话持久化**——不挂 jsonl/sqlite persistence 行，会话仅内存态，进程重启即失（demo 可接受，服务化不可）；
3. 工具执行（bash/editor/web…）、MCP、subagent、审批/权限流、遥测/会话查询——chat 本身不依赖。

**Spine 之下的逃生舱（记录，非建议）**：spine 本身只是一个"组装件"插件；理论上可绕过它直接组 `dsh-agent` + `dsh-agent-loop` + `dsh-session` 等核心件，把 26-peer 闭包中不需要的部分（goal/skill/jobs/shell-env/工具包）剔出解析面。但**无官方先例、无文档承诺**（官方所有无人值守组合都经 spine），属 PoC 探索项而非设计依赖。另注：spine 在 `packages/examples/agent-spine-demo`（examples 目录）——官方把它放在示例区，本身就是"组装方式可自定"的信号，但 0.x 期内部 API 无稳定性承诺。

### 5.5 已否决的替代方案（记录理由，避免重提）

- **预构建 dsh base OCI image**（底座在 bazel 外的流水线构建、`oci.pull` 按 digest 消费）：两套构建系统；版本升级 = 换 digest，与 lockfile PR 流程割裂；闭包校验落在 bazel 外；当前无跨仓库共享底座场景。且 §5.1/§5.2 已证明 bazel 内单点引用 + 现有拍平机制足够，base image 的唯一残余价值（跨仓库共享）暂无需求。
- **bazel action 内跑 `pnpm deploy`**：非 hermetic（网络/store），与 rules_js 虚拟存储重复建设；其差异化能力（hoisted 布局、symlink 实体化）已被 Phase 3 覆盖。

### 5.6 多 baseline 分层：场景 baseline 的二次包装（2026-08-22 补充调研）

> 对应锁定决策的后半部分：`third_party/dsh` 下可有多个 baseline target——框架核心 baseline 之上，把某场景常用的插件集合再包装为一个新 baseline（如"coding baseline = 核心 + spine + adapter + 常用工具 + persistence"），服务引用场景 baseline 而非逐个声明插件。

#### 5.6.1 机制可行性：现有闭包走行零改动支持继承

核心机制事实（`tools/release/defs.bzl` 的 `_collect_runtime_closure`，§3.3 已引）：BFS 以 `runtime_deps` 为边、`npm_deps` 为载荷，**穿透任意深度**——每个 runtime_pkg 贡献自己的 npm_deps，再继续走它的 runtime_deps。因此**场景 baseline 只是一个"runtime_deps 指向核心 baseline + npm_deps 列增量插件"的普通 runtime_pkg**：

```
third_party/dsh/
├── core/                                ← 框架核心（§5.1，~10 包枚举）
│   ├── package.json                     ← 核心 pin
│   └── BUILD.bazel                      ← js_runtime_library(name="runtime_pkg", npm_deps=[~10 核心])
├── coding/                              ← 场景 baseline：核心 + 无人值守编码插件集
│   ├── package.json                     ← deps: "@dominion/dsh-core": "workspace:*"
│   │                                      + spine/adapter/tool-bash/editor/persistence… 精确 pin
│   └── BUILD.bazel
│       js_runtime_library(
│           name = "runtime_pkg",
│           package_name = "@dominion/dsh-coding",
│           lib = ":version",
│           runtime_deps = ["//third_party/dsh/core:runtime_pkg"],   # ← 继承：BFS 继续走
│           npm_deps = [ ":node_modules/@deepseek-ai/dsh-agent-spine-demo", … 增量 ],  # 本包只枚举增量（link target 按本包 package.json 生成，§3.3）
│       )
└── chat/                                ← 另一场景 baseline：核心 + spine + adapter（§5.4.2 的最小 chat）
    └── …同构…
```

服务引用 `//third_party/dsh/coding:runtime_pkg` 一行，物化结果 = 核心 ∪ coding 增量（单前缀单树，§2.1 布局不变）。**已核对的支持性事实**：

- **继承传递性**：`queue.extend(info.runtime_deps)` 任意深度（源码行为）——三层以上叠加（core → coding → 某项目特化）同样成立；
- **去重语义**：workspace 包按 `package_name` 先见者胜（core 的包名先入队，不会重复）；npm target 按 label 去重（core 的 ~10 个与场景增量不重叠时无影响）；
- **枚举分治**：link target 只为**本包 package.json 的直接依赖**生成（§3.3 实证）——core 枚举留在 core 的 BUILD，场景包只枚举自己的增量。每个 package.json 是一层 pin 快照，`workspace:*` 边缘不携带版本（版本仍由各包精确 pin + 根 lockfile 统一解析）。

**baseline 是相对概念**：`runtime_deps` 图上任何 runtime_pkg 节点都是一个可引用的 baseline。可复用的（core/coding/chat）放 `third_party/dsh/`；项目私有的组合直接在项目 BUILD 里定义（`runtime_deps = ["//third_party/dsh/coding:runtime_pkg"], npm_deps = [项目特有插件]`）——不需要为此建新目录或新机制。

#### 5.6.2 已识别的真实坑（多 baseline 特有，校验面新增两项）

1. **manifest 包自身会物化进 node_modules**：闭包走行把每个 runtime_pkg（含 core/coding 自身）作为 workspace 包放进 `node_modules/@dominion/dsh-core/` 等（源码行为：`workspace_pkgs` 全部落 tar）。无害（内容仅 version.ts），且**可正面利用**——运行时可 `require('@dominion/dsh-coding/package.json')` 自省 baseline 快照版本，诊断友好。
2. **同名包多版本静默覆盖（最重要）**：pnpm 允许不同 importer（= 不同 baseline 的 package.json）pin 同一包的不同版本（如 core pin `dsh-session@0.1.1-rc.2`、某项目特化层 pin `rc.3`，lockfile 两条共存）。物化时 Phase 3 的 `rm -rf + cp -aL` **后写胜出、无告警**——若两版本落在同一服务的闭包里，最终镜像里是谁取决于 target 顺序（本质未定义）。**缓解**：① 校验 target 增加查重项——同一服务的物化集合中同名包必须版本唯一（从 store 路径 `name@version` 提取）；② 治理约定——dsh 家族包全仓库锁同一条 rc 线（单 lockfile 下天然倾向，违反时 CI 查重即报）。注意该风险**不是多 baseline 引入的**（服务自列依赖同样可能撞），但多 baseline 让枚举分散、更依赖校验兜底。
3. **场景 baseline 的 peer 完整性**：场景增量插件的 peer 闭包必须到不动点（spine 的 26 peers 等二阶展开）——校验 target 的既有职责（§5.3-②），多 baseline 下校验对象 = "服务引用的 baseline 根 + 服务自身增量"的合并闭包，语义不变。

#### 5.6.3 已否决：按 baseline 边界的 OCI 多层 tar（记录理由，避免重提）

曾考虑 `dsh_pkg` 按 baseline 边界产多层 tar（core 层 + coding 层 + 服务层）以获取 registry 层去重收益（core 层 digest 跨场景/服务共享）。**否决（2026-08-22 锁定单 tar）**：收益仅为镜像 pull 的层复用，代价是 `artifact_pkg_js.ts_project` 可选化、`ArtifactPkgInfo` 双 tar 语义、tar 字节级确定性归一化（`cp` 不保留 mtime、`tar -cf` 记录拷贝时刻，需 `--mtime=@0 --sort=name`）三项实现负担；当前镜像分发规模下无实际瓶颈，正确性也不依赖分层。若未来镜像体积/pull 流量成为真实问题可再评估（BFS 走行天然携带 provenance，分层信息免费）。

---

## 6. 与现有 JS 构建的收敛面

### 6.1 可直接复用（构建图 ~95%）

| 环节 | 现有物 | dsh 服务的使用方式 |
|---|---|---|
| 依赖供给 | `npm_translate_lock` + 根 `pnpm-lock.yaml` | 原样（dsh 闭包经闭包清单包入 lockfile 即可用） |
| npm 链接 | `npm_link_all_packages` → `:node_modules/<pkg>` | 原样（闭包 link targets 由清单包 BUILD 枚举） |
| TS 编译 | `ts_project` + swc | 原样（bootstrap/插件/服务代码） |
| workspace 包闭包 | `JsRuntimePackageInfo` + `_collect_runtime_closure` BFS | **原样且是核心复用点**（底座清单包与自研插件都作为 runtime_pkg 走行，otel 模式） |
| 打包 | `artifact_pkg_js` 五阶段 | **原样可用**（§5.2 最小路径）；macro 变体仅增量（§5.3） |
| 拍平物化 | Phase 3（虚拟存储前缀重写 + `cp -aL`） | 原样（172 包实证） |
| 镜像与部署 | `artifact_image`（entrypoint/cmd/repository/push/metadata）+ `service.yaml` | 约定原样（`/nodejs/bin/node` + `/dominion/a/s/entry`） |
| 单测 | `vitest_test` | 原样 |
| 数据文件 | `data_files` | cordis.yml 走此机制即可（机制已在） |

### 6.2 需要新增（按最小路径 → macro 增量排序）

1. **baseline workspace 包**（§5.1/§5.6，唯一必须项）：`third_party/dsh/core`（框架核心 ≈10 包精确 pin，catalog 例外记录）+ 按需的场景 baseline 包（如 `coding`，`runtime_deps=[core]` + 增量枚举；枚举可从各自 package.json 脚本生成）。
2. bootstrap 模板（boot + 生命周期；官方 jsonrpc-demo runner 的仓库化，B1 调研 §2.1）。
3. 闭包校验 target（§5.3-②；独立 target 亦可，macro 内联只是便利）。
4. （可选便利）`dsh_pkg` macro：`composition` 一等属性 + 闭包校验内联展开（§5.3）；产物单 tar 与现状一致。

### 6.3 差异点（不收敛但可控）

- 入口语义：现有服务 bootstrap 自持 gRPC server 主循环；dsh 服务 bootstrap 调 `boot()` 后**持有 ctx 等待信号**（结构仍相似：init → 动态 import → 信号 → 优雅退出）。
- 模块格式：建议 dsh 服务入口/插件走 ESM（§2.4 两条路都通，ESM 与 dsh 生态一致性更好）。
- 产物形态约束：**不得 bundle**（B1 调研 §3.3）——现状链路本来就是 per-file 编译不 bundle，无冲突。
- `runtime_protos` 等 agent 特有属性在 dsh 场景无关（macro 面收窄）。

---

## 7. 风险与开放问题（PoC 变量清单）

1. **link target 的 peer 链接语义（已降级为优化项）**：§3.3 实证 link target 只按直接依赖生成、闭包经 files 传递物化；§5.1 的全量枚举设计使"peers 是否连带物化"不再影响完整性。残余问题仅为"清单能否裁剪到非全量"——PoC 顺手验证（单列 `dsh-app-boot` 看 peers 是否在场）。
2. **native 平台包形态**：lockfile 含全平台 optionalDeps（darwin/win32/linux），store/拍平产物中 linux-x64-gnu 必须在场（其余平台为冗余体积）；且需在 distroless nodejs24-debian12（glibc 2.36）内实测 `requireBuiltin` 加载。PoC 验证。
3. **入口 ESM 化**：`.swcrc` 按服务区分 module 类型，或 bootstrap 用 `.mjs`/CJS 锚点（§2.4）。
4. **镜像体积量化**：精确闭包 + 平台冗余的实际体积（官方 SEA 全产物 ~174MB 含 Node runtime 可作量级参考，B1 调研 §7 风险 4）；PoC 输出。
5. **boot 失败 = 启动失败**（fail-loud 审计）与容器启动探针/重启策略的衔接：组合错误会导致进程退出，部署面按崩溃重启处理即可，但需要意识到"半启动"状态不存在。
6. **cordis.yml 的 config 注入与部署配置机制衔接**：`service.yaml` configs（如现有 `agent_timeouts` 注入 env/yaml）→ `!!js` 读 env 或生成 YAML 的路径设计——属于后续 plan 阶段的设计题，非可行性阻塞。
7. **多 baseline 同名包版本冲突（§5.6.2-2）**：不同 baseline 的 package.json 可 pin 同一包的不同版本（lockfile 允许共存），Phase 3 物化后写胜出且无告警。对策：校验 target 查重项（服务闭包内同名包版本唯一，从 store 路径 `name@version` 提取）+ "dsh 家族全仓库锁同一 rc 线"治理约定；PoC 校验脚本应覆盖此项。
8. **0.x-rc 漂移成本**：每次底座升级 = 全家桶 pin 同步 + 校验重跑（B1 调研 §7 风险 3/8）；现有方案使其收敛为常规 lockfile PR，但频率由上游决定。多 baseline 下升级动作 = core 与各场景 baseline 的 pin 同步（仍是一个 lockfile PR）。

---

## 8. 引用来源汇总

仓库内：

- `MODULE.bazel`（npm_translate_lock/root pnpm-lock.yaml、distroless_nodejs24 拉取、node toolchain 24.14.0）
- `tools/release/defs.bzl`（`artifact_pkg_js` 五阶段实现、`artifact_image` js 约定、`JsRuntimePackageInfo`/`_collect_runtime_closure`）
- `tools/release/js_runtime_library.bzl`（workspace 包闭包暴露机制）
- `common/js/otel/BUILD.bazel`、`common/js/otel/package.json`（**底座封装的既有范式**：`js_runtime_library(npm_deps=[直接依赖])` + 服务 `runtime_deps` 单点引用；ts_project 可选化的参照）
- `projects/game/agent/BUILD.bazel`、`projects/game/agent/.swcrc`、`projects/game/agent/package.json`、`projects/game/agent/service.yaml`（现状服务样板）
- `//projects/game/agent:server_pkg` 产物 tar（2026-08-22 实证：172 包闭包拍平、包自带 .pnpm 残留、package.json 在场）
- cquery 实证（2026-08-22）：`//projects/game/agent:node_modules/accepts` 不存在（link target 只按直接依赖生成）；`//projects/game/agent:node_modules/@dominion/common-js-config` files=13（workspace 包 target 只含自身文件）
- store 实证（2026-08-22）：`projects/game/agent/node_modules/@grpc/grpc-js/package.json` 的 dependencies = `@grpc/proto-loader` + `@js-sdsl/ordered-map`（普通 deps，无 peers）——对照 `@deepseek-ai/dsh-app-boot@0.1.1-rc.2` npm 元数据（dependencies 仅 js-yaml、9 包在 peers）构成 §5.1 三层差异的证据
- `survey/deepseek-harness-b1-plugin-packaging.md`（前置调研：§2 机制、§3 官方打包路线、§4.2 peer 闭包与 #1032、§6 底座×插件拆分/双树模型）
- `survey/deepseek-harness-integration-modes.md`（A/B1/B2 拓扑）

仓库外（官方文档/仓库）：

- https://github.com/aspect-build/rules_js/blob/main/docs/pnpm.md（npm_translate_lock、lockfile 更新流程、lifecycle hooks）
- https://github.com/aspect-build/rules_js/blob/main/docs/troubleshooting.md（`:node_modules/<pkg>` 作为运行时 data 依赖的官方模式、npm_exclude_package_contents）
- https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/examples/jsonrpc-agent/minimal.cordis.yml（官方最小无人值守组合 12 行：serving/adapter/sandbox×2/subprocess/pty/terminal/fs/spine/2 工具/persistence；"context compaction are absent" 注释）
- https://registry.npmjs.org/@deepseek-ai/dsh-agent-spine-demo（spine 包 0.1.1-rc.2 的 26 项 peerDependencies、位于 packages/examples/agent-spine-demo 的仓库目录字段）
- https://registry.npmjs.org/@deepseek-ai/dsh-llm-deepseek（adapter 包 0.1.1-rc.2 的 12 项 peerDependencies、deps 仅 eventsource-parser/schemastery）
- https://github.com/deepseek-ai/deepseek-harness（经前置调研间接引用：app-boot/runner 样板、python/sdk-runtime 纯依赖清单、verify-runtime-closure、pnpm deploy 工艺）
- https://pnpm.io/settings（`node-linker`/`auto-install-peers` 语义，经 B1 调研 §3.2/§6.5 引用）
