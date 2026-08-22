# 调研：DeepSeek Harness（dsh）B1 模式的插件工作机制与打包分发

> **状态**：调研完成，尚未做出任何采用/重构决策
> **日期**：2026-08-15（主体调研，基于 0.1.0-rc.6）；**2026-08-22 增量核对至 0.1.1-rc.2**（新版本动态、npm 分发完整性实证、社区生态更新；各节以"2026-08-22 核对/更新/实证"标注，引用见 §9）
> **前置调研**：`survey/deepseek-harness-framework.md`（框架总体架构、§5 嵌入方式）、`survey/deepseek-harness-preset.md`（preset 机制）、`survey/deepseek-harness-integration-modes.md`（§3 集成拓扑分类：A/B1/B2；§2.2 B1 = `boot()` in-process 主路线）
> **范围**：B1 模式（业务进程 import dsh-app-boot 进程内组装）下插件的工作机制（boot 管线、Loader 模块解析四路径、`bareModuleBaseUrl` 语义、插件包格式）、插件能否随业务 package 一起打包分发（官方三条打包路线的事实记录：bin-only 外部 config、closed runtime 闭包、单文件 SEA）、`node-addon-require-builtin` native 依赖的形态与风险、peer 依赖闭包约束、与本仓库 Bazel/pnpm catalog 工具链的兼容性输入、"dsh 底座 × 插件增量"的拆分分析框架（解析面 × 启用面、双树拼合模型）
> **说明**：本文为纯调研材料，记录源码级事实与分析结论；不含采用决策、迁移方案或未来方向设计。信息源以 dsh 官方仓库源码与设计文档为主（dsh npm 包 2026-08-10 起才公开发布，社区尚无第三方 B1 嵌入实践可查，见 §7 风险 5）。

---

## 1. 背景与调研问题

前置调研（integration-modes §3）确立了三种集成拓扑：A（harness 为宿主）、B1（库内嵌，业务进程拥有进程）、B2（子进程包装）。并确认 dsh 的 TS 集成主路线是 in-process：`import @deepseek-ai/dsh-app-boot`，用 `boot()`/`loadProfile()`/`composeEntries()` 组合插件树（dsh CLI 本身即这些 helper 上的 "thin self-executing composition"）。本次调研围绕 B1 模式继续下钻两个问题：

1. **B1 模式下，插件如何工作？**——业务进程内 `boot()` 之后，组合清单里的每个插件行（`{id, name, config}`）经历什么？模块如何被解析、导入、挂载、卸载？插件包本身需要满足什么格式？
2. **插件能否随 package 一起打包？**——插件能否作为业务包的普通 npm 依赖，随服务构建产物一起分发，而不依赖 `$DSH_HOME` profile 目录 / `dsh plugin add` 那套用户侧安装机制？解析锚点在哪里、如何控制？

信息源见 §9。

---

## 2. B1 模式下插件的工作机制

> **2026-08-22 核对**：app-boot 源码与 vendored Loader（`vendor/loader/src`）自 2026-08-11 后无功能性提交，rc.6 → 0.1.1-rc.2 间 app-boot 仅有 release bump 与 docs i18n 提交。本节机制记录（boot 管线、四路径解析、`bareModuleBaseUrl` 锚定）在 0.1.1-rc.2 上逐字有效。（来源：[commits/master/packages/boot/app-boot](https://github.com/deepseek-ai/deepseek-harness/commits/master/packages/boot/app-boot)、[commits/master/vendor/loader/src](https://github.com/deepseek-ai/deepseek-harness/commits/master/vendor/loader/src)）

### 2.1 boot() 管线（源码级事实）

（[packages/boot/app-boot/src/index.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/boot/app-boot/src/index.ts)）`boot()` 的完整流程，每步都是可核对的行为：

```ts
export async function boot(binName, absoluteConfigPath, patches?, prepare?, bareModuleBaseUrl?): Promise<Context> {
  const ctx = new Context()                                    // ① Cordis root context
  ctx.baseUrl = pathToFileURL(dirname(absoluteConfigPath)).href + '/'  // ② 相对名锚定 config 目录
  ctx.provide('dshHomePath', dshHomePath)                       // ③ !!js 表达式可用 dshHomePath()
  await ctx.plugin(Loader)                                      // ④ Loader 作为普通插件挂载
  await prepare?.(ctx)                                          // ⑤ host 准备钩子（loader 可用后、config 树挂载前）
  await mountRootInclude(ctx, absoluteConfigPath, patches, bareModuleBaseUrl)  // ⑥ 挂根 Include
  await ctx.get('loader')?.await()                              // ⑦ 等整棵树 settle
  await assertEntriesActivated(ctx, binName)                    // ⑧ fail-loud 审计
  return ctx                                                    // ⑨ 返回 root context；失败则 dispose 部分树并 reject
}
```

要点：

- **④ Loader 本身是普通 Cordis 插件**（vendored `@cordisjs/plugin-loader`，见 §2.2），不是框架特权组件。`ctx.plugin(Loader)` 之后 `ctx.loader` 服务可用。
- **⑥ 根 Include** 是把 `cordis.yml` 挂进树的方式：`mountRootInclude` 注册 `cordis:include`/`cordis:group` 两个 **builtin**（`ctx.loader.builtins.include/group`，静态 import 而非 specifier 解析——所以 config 树永远能用到这两个），然后 `ctx.loader.create({id: 'include', name: 'cordis:include', config: {path, patches}})` 创建根条目。
- **⑦⑧ settle + 审计**：`loader.await()` 等所有 import 与生命周期任务完成；`assertEntriesActivated` 对每个 enabled entry 检查 fiber 状态（loaded → activated；pending 会列出它等待的服务名，failed 会附原始 stack）——**插件解析失败是启动失败**，不静默跳过。
- **⑨ 失败语义**：任一步失败都会 `ctx.fiber.dispose()` 部分树（各插件注册的 effect 自动回滚）再抛带 binName 前缀的错误。
- **B1 最小官方样板**就是 jsonrpc-demo 的 runner（[packages/examples/jsonrpc-demo/src/runner.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/examples/jsonrpc-demo/src/runner.ts)）：

```ts
installFailLoud(NAME); loadEnv(NAME)
const ctx = await boot(NAME, configPath, undefined, undefined, bareModuleBaseUrl)
process.stdin.on('end', () => { void disposeAndExit(0) })   // stdin EOF → ctx.fiber.dispose() → exit
process.on('SIGTERM', ...); process.on('SIGINT', ...)        // 信号同样 dispose 后退出
```

即：**fail-loud 守卫 + config 发现 + boot + 持有 ctx + 退出时 dispose**——全部业务侧可控，无 CLI、无 profile、无 `$DSH_HOME` 依赖（`dshHomePath` 只是暴露给 `!!js` 表达式的路径助手）。

- **config 的两个来源**（jsonrpc-demo 约定）：`$DSH_CORDIS_CONFIG` 环境变量 > argv 位置参数；两者皆缺则 usage + exit(1)，**无内置 fallback**（"the plugins actually booted are decided by an external cordis.yml" 是 runtime 的硬语义）。对 B1 嵌入方，config 路径由业务代码自定（完全可以直接传产物内携带的静态 YAML 路径，甚至运行时生成后写临时文件再传路径）。

### 2.2 插件解析：EntryTree.import 的四条路径（核心机制）

（[vendor/loader/src/config/tree.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/vendor/loader/src/config/tree.ts)）组合清单里每行的 `name`（module specifier）由 `EntryTree.import()` 解析：

```ts
import(name: string, getOuterStack?) {
  if (name.startsWith('cordis:')) {
    return this.ctx.loader.builtins[name.slice(7)]      // ① cordis: 内置（include/group）
  }
  return composeError(async (info) => {
    if (this.ctx.loader.internal) {
      return await this.ctx.loader.internal.import(name, this.ctx.baseUrl!, {})  // ② Node 内部加载器，parentURL = ctx.baseUrl
    } else if (name.startsWith('.')) {
      return await import(new URL(name, this.ctx.baseUrl).href)  // ③ 相对路径：URL 拼接，锚定 baseUrl
    } else {
      return await import(name)                          // ④ 裸包名 fallback：普通动态 import
    }
  }, getOuterStack)
}
```

| 路径 | 条件 | 解析锚点 | 语义 |
|---|---|---|---|
| ① `cordis:` | 以 `cordis:` 开头 | `loader.builtins` 字典 | 框架内置（`cordis:include`/`cordis:group`），不走模块解析 |
| ② internal loader | `loader.internal` 可用 | **`ctx.baseUrl`**（默认 = config 文件目录；可被 `bareModuleBaseUrl` 覆盖，见 §2.3） | Node 内部 ESM 加载器（`internal/modules/esm/loader`），从锚点按 Node 标准规则向上找 `node_modules` |
| ③ 相对路径 | 无 internal 且以 `.` 开头 | `ctx.baseUrl` | `new URL(name, baseUrl)` 后普通 import |
| ④ 裸包名 fallback | 无 internal | **loader 模块自身的 URL**（`import(name)` 的词法位置，即 `@deepseek-ai/cordis-plugin-loader/lib/config/tree.js` 所在处） | 从 loader 包位置向上找 `node_modules` |

**`loader.internal` 是什么**：Loader 用 Node **内部模块加载器**执行 import（[vendor/loader/src/internal.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/vendor/loader/src/internal.ts)）：`requireInternal('internal/modules/esm/loader')` 拿到 cascaded loader 实例（Node 22 为 v1 接口、Node 24 为 v2 接口）。获取内部模块有两条通道：

1. Node 以 `--expose-internals` 启动（dev/测试场景）；
2. **`node-addon-require-builtin`**（native addon，`requireBuiltin(id)` 可加载任意 Node 内部模块；无白名单限制的变体）。

两条都不可用时 `loader.internal === undefined`，退化到 ③④。这正是 app-boot README 反复强调的约束（原文）："**Bare plugin specifiers in a config (`@deepseek-ai/dsh-*`, npm packages) resolve through the Cordis Loader's internal module loader. They resolve from the config directory by default; a closed runtime passes `bareModuleBaseUrl` to `boot` or `mountRootInclude` so its installed package tree remains authoritative even when the config lives inside another Node project. Relative specifiers always resolve against the config directory. Repository bins install Loader's optional `node-addon-require-builtin` peer; external callers must supply it or install plugins where plain Node import resolution can find them.**" 以及已知限制："**Bare package specifiers depend on Loader internals** — production bins need Loader's optional native helper; an in-process caller without it must use resolvable relative/file specifiers or provide its own module-resolution hook."（[packages/boot/app-boot/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/boot/app-boot/README.md)）

**为什么不用普通 `import()` 而要内部加载器**：普通动态 import 的解析锚点是 import 调用所在模块的位置（loader 包自己），无法按 config 目录或宿主意图锚定；内部加载器把 parentURL 作为参数（`internal.import(specifier, parentURL, {})`），锚点可控。此外 v8 cache/错误栈的 offset 处理也在 composeError 里对齐了。

### 2.3 `bareModuleBaseUrl`：裸包名锚点的两种模式（回答"能否随 package 打包"的机制核心）

（[packages/boot/app-boot/src/index.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/boot/app-boot/src/index.ts) 的 `mountRootInclude`）`boot(..., bareModuleBaseUrl)` 传入后，根 Include 被替换为一个子类：

```ts
ctx.loader.builtins.include = bareModuleBaseUrl === undefined
  ? Include                                    // 模式 A：config-owned（默认）
  : class HostResolvedRootInclude extends Include {
    override import(name, getOuterStack) {
      const specifier = isAbsolute(name) ? pathToFileURL(name).href : name
      if (name.startsWith('.') || name.startsWith('cordis:')) return super.import(specifier, getOuterStack)
      const internal = this.ctx.loader.internal
      if (internal === undefined) return super.import(specifier, getOuterStack)
      return internal.import(specifier, bareModuleBaseUrl, {})   // 模式 B：host-owned
    }
  }
```

| | 模式 A：config-owned（不传 `bareModuleBaseUrl`） | 模式 B：host-owned（传 `bareModuleBaseUrl`，如 `import.meta.url`） |
|---|---|---|
| 裸包名解析锚 | config 文件目录（向上找 node_modules） | **宿主包自己的位置**（向上找 node_modules → 宿主的依赖树） |
| 相对路径解析锚 | config 文件目录（不变） | config 文件目录（不变，`startsWith('.')` 分支保留 super） |
| 绝对路径 | 转 `file://` URL 后走 super | 同左 |
| 官方对应物 | jsonrpc-demo 发布的 `dsh-jsonrpc-agent` bin（"resolves bare plugins from the configuration project"） | `packaged-bin.ts`：`runJsonrpcAgent(import.meta.url)`（"packaged bare plugins resolve from its closed runtime tree, while relative plugins remain configuration-relative"） |
| 适用 | config 放在一个有自己的 node_modules 的"配置工程"里 | **插件全部随宿主 package 打包、config 可以放在任何地方**（部署工件内、临时目录、运行时生成） |

关键语义（单文件 SEA 设计文档原文，[2026-07-10-single-file-executable-sdk-runtime-distribution.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/architecture/2026-07-10-single-file-executable-sdk-runtime-distribution.md)）："**Bare specifiers in the packaged entry resolve upward along `node_modules` from the entry's position inside the VFS and land inside the VFS naturally. The closed set needs no allowlist code — the set is whatever the VFS has installed, and importing a name outside the set fails.**"——闭包集合不需要 allowlist 代码：装了什么就有什么，import 集合外的名字直接解析失败（fail loud）。

**这意味着"插件随 package 打包"的官方答案：能。** 把组合里用到的全部 dsh 插件包声明为自己 package 的 `dependencies`，部署产物携带完整 node_modules；`boot()` 时传 `import.meta.url`（宿主模块 URL）作为 `bareModuleBaseUrl`，组合 YAML 里照写裸包名（`@deepseek-ai/dsh-llm-deepseek` 等）。业务自己的插件则有两种写法：相对路径（`./my-plugin.js`，相对 config）或也发布成包名（走同一闭包）。

### 2.4 插件的形态与挂载

- **插件 = 普通 ESM 模块**。入口导出 plugin 对象：带 `inject`（服务依赖声明）与 `apply(ctx)`（或 `apply(ctx, config)`）的函数对象，或 `Service` 子类。Loader 拿到模块后 `unwrapExports`（剥 `.default` 与 `__esModule` 互操作两层），然后 `ctx.registry.plugin(plugin, config, stack)` 挂载（[vendor/loader/src/config/entry.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/vendor/loader/src/config/entry.ts) 的 `Entry._start`）。
- **加载顺序不是声明的**：`inject` 声明的服务就绪才启动（依赖注入驱动），失败/挂起在 §2.1 ⑦⑧ 审计处 fail loud。
- **插件包没有任何 dsh 特有 manifest 要求**。以 `@deepseek-ai/dsh-tool-cordis` 的 package.json 为例：普通 `"type": "module"` + `lib/index.js` 入口；dsh 生态内的包全部以 **peerDependencies** 声明 cordis 与相邻 dsh 包（见 §4.2 的闭包含义）。`dsh.bundle.patch` manifest 字段只在"作为 bundle 分发、走 profile 叠层安装"时才需要（`dsh plugin add` 路线，[docs/user/develop/basic/publish.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/basic/publish.md)；"A package without the `dsh.bundle` declaration still installs, but only as a plain dependency"）。**B1 的 closed-runtime 路线不需要 bundle manifest**：插件的启用与否完全由组合 YAML 行决定，与包的 manifest 无关。
- **官方插件形态样例**（[docs/cookbook/extension-cookbook.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/extension-cookbook.md)）：hook 插件（`ctx.on('tools/pre-execute', ...)`）、工具插件（`ctx.tools.register(defineTool(...))`）、UI/协议驱动插件（`ctx.on('session/event')` + `ctx.agents`）、本地相对路径插件（官方 Cordis 教程直接用 `- name: './tool-logger.ts'` 相对行加载本地 TypeScript 文件，[docs/cordis-tutorial/07-into-the-harness.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cordis-tutorial/07-into-the-harness.md)）。
- **config 值的插值**：行的 `config` 里 `!!js` 表达式在该行声明的注入激活后、基于该行自己的插件 context 求值（Loader 的 lazy config resolution，vendored 修改记录 #15）；组合 YAML 与业务代码之间靠 `!!js`（读 env / 读注入服务）传参。也有纯数据传参：业务代码可以直接改 YAML 生成，或用 `boot` 的 `patches` 参数叠加 patch 层。

### 2.5 进程内多组合与生命周期

- **`boot()` 可以多次调用**：每次 `new Context()` 独立成树（插件 fiber、注册 effect 各自独立）。但 Node 模块缓存进程级共享——同一插件模块 URL 只实例化一次 JS 模块，多个 Context 复用模块级状态、各自拥有插件实例。Cordis 的注册-即-effect 模型保证 dispose 时各树独立回滚。
- **运行时改树**：`ctx.loader.create/remove/update` 是公共 API（EntryTree，[vendor/loader/src/config/tree.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/vendor/loader/src/config/tree.ts)）；Loader 根树的 `write()` 是 no-op（"Loader's root tree is in-memory"）——即**不经 YAML、纯编程式创建插件行也是一等公民用法**（preset 机制内部正是这么用的：agent factory 把 preset 目录 `agent.cordis.yml` 作为 include 子树挂到 agent scope，见前置调研 preset §2.2）。
- **HMR/watch 可选**：`watchUserPatches` 依赖可选 peer `@deepseek-ai/cordis-plugin-hmr`；不装 HMR、不调 watch，树就是静态的——容器化 B1 服务不需要文件监听。
- **退出**：`ctx.fiber.dispose()` 达到 quiescence；jsonrpc-demo 把它挂在 stdin EOF/SIGTERM/SIGINT 上（B1 服务对应自己的 shutdown 钩子）。

---

## 3. 打包分发：官方三条路线的事实记录

### 3.1 路线一：bin-only + 外部 config（config-owned）

`@deepseek-ai/dsh-sdk-jsonrpc-demo`（[packages/examples/jsonrpc-demo/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/examples/jsonrpc-demo/README.md)）：bin 只做"boot 一个外部 cordis.yml"；**dependencies 里只有 `dsh-app-boot` 一个**，peers 只有 cordis + dsh-invariants——插件包不由它拥有，由 config 所在的"配置工程"自己安装。这是发布给外部用户、让用户自带组合的形态（对应用户侧 profile/`--config` 用法）。

### 3.2 路线二：closed runtime 闭包（host-owned）——对 B1 服务最相关

Python SDK 的 bundled runtime（[python/sdk-runtime/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/python/sdk-runtime/README.md) + [单文件分发设计文档](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/architecture/2026-07-10-single-file-executable-sdk-runtime-distribution.md)）确立了"插件随 package 打包"的完整官方工艺：

1. **纯依赖清单作为 deploy root**：`python/sdk-runtime/package.json`（包名 `dsh-jsonrpc-agent-pkg`，"Dependency-only deploy root"）声明约 100 个 `@deepseek-ai/*` 依赖，**自身零代码**。设计文档原文："the [package.json] at this package's root is the deploy root of the single-exe pipeline — **a pure dependency manifest (no code of its own) whose dependency closure IS both the plugin set compiled into the exe and the tree materialized into `runtime/node/`. Adding a plugin to the distribution means adding one dependency line there and rebuilding.**"
2. **物化 node_modules**：`pnpm --filter dsh-jsonrpc-agent-pkg deploy --legacy --prod --config.node-linker=hoisted --config.auto-install-peers=false --config.link-workspace-packages=true` 直接 deploy 到目标目录；随后把所有 symlink 替换为真实字节（pkg 的 VFS 不支持 symlink）。四个 deploy flag 均有实测依据（`--legacy` 是关掉 inject-workspace-packages 后的必经路径；hoisted 给 pkg 稳定单实例布局；禁自动装 peer 防止未声明 peer 扩闭包；link-workspace-packages 选直接 workspace 依赖）。
3. **native addon 单独 stage**：`node-pty` 的 `pty.node`（Linux 从源码构建于 manylinux 2.28 容器）与 macOS `-spawn-helper` 侧车。
4. **闭包校验**：`scripts/verify-runtime-closure.ts` 遍历清单覆盖的每个 workspace 包，要求每个非可选 peer 都出现在 runtime 根——CI 静态检查 + 打包前必跑（`pnpm run hygiene`）。**peer 缺失会让插件在目标环境 PENDING（等待服务）或解析失败**，所以闭包校验是分发质量门。
5. **入口**：`node_modules/@deepseek-ai/dsh-sdk-jsonrpc-demo/lib/packaged-bin.js` → `runJsonrpcAgent(import.meta.url)`——即 §2.3 的模式 B。

**对 B1（业务服务进程）的对应关系**：不需要 SEA/单文件；等价做法是"业务 package（或一个独立的组合清单 package）声明插件依赖闭包 + 容器镜像内携带物化后的 node_modules + 服务启动时 `boot(name, configYml, undefined, undefined, import.meta.url)`"。config YAML 是部署工件的一部分（相对路径行相对 config 解析，裸包名行锚定宿主依赖树）。**profile 机制（`$DSH_HOME/profiles`）、bundle 叠层、`dsh plugin add`、home 级 patch 全部不参与**——这些是终端用户产品的安装形态，不是嵌入形态。

一个刻意的设计否决值得记录（设计文档 Alternatives 一节）：**"jsonrpc-agent 自己声明全部闭包依赖"被否决**——app bin 声明 53+ 个它从不 import 的依赖是"a packaging manifest masquerading as real dependency relationships"（伪装成依赖关系的打包清单），迫使仓库约束开两个例外。最终闭包清单独立成一个零代码 manifest 包。这对 B1 嵌入方的启示：**组合的依赖闭包与业务代码的真实依赖，官方实践是分开声明**（业务包只依赖它真正 import 的 app-boot/服务面类型；插件闭包放在一个独立的纯依赖清单包/或部署 manifest 里）。

**2026-08-22 更新**：rc.8 起 Python SDK runtime 闭包显著扩充（release note："Python SDK 依赖配置覆盖 4 个内置 Agent 预设，并包含 `rg` / glob 搜索和 MCP stdio 工具所需依赖"，[dsh-v0.1.0-rc.8](https://github.com/deepseek-ai/deepseek-harness/releases/tag/dsh-v0.1.0-rc.8)）。master 上的 [python/sdk-runtime/package.json](https://github.com/deepseek-ai/deepseek-harness/blob/master/python/sdk-runtime/package.json)（127 行）现已覆盖全部官方插件包：全工具集（含 `dsh-tool-fs-search`、`dsh-tool-jobs`、`dsh-tool-workflow`）、四个 subagent 驱动（acp / fork-in-process / in-process-driver / spawn-in-process）、`dsh-session-persistence-jsonl`/`-sqlite`、`dsh-hooks-claude-code`/`-codex`、三个 web-search provider（deepseek/exa/perplexity）、`dsh-mcp-client` 等。这是 §6.3 表格中"全集闭包（runtime wheel 路线）"的实际强化：官方 SDK 分发选择了"装全集、组合自由度优先"。

### 3.3 路线三：单文件 SEA（B2 形态，记录其否决项对 B1 的启示）

Python wheel 生产载体是 `@yao-pkg/pkg --sea` 打出的单文件可执行（~174MB，含 Node 24 runtime + VFS 内真实 package tree）。对本调研有价值的是被否决的替代方案，因为它们划定了"插件打包"的能力边界：

- **裸 Node 原生 SEA 被否**：注入的 main 必须是单个 CJS 文件，blob 无文件系统无模块解析，**裸包名动态 import 无物可解析**——只能把插件静态编译进主脚本手工注册，"bypassing standard module resolution and hardcoding the plugin set, contrary to 'configuration decides everything'"。
- **pkg 标准 mode 被实测杀死**：ESM→CJS+字节码后所有动态 `import()` 抛 `ERR_VM_DYNAMIC_IMPORT_CALLBACK_MISSING`。
- **"预打包每包 ESM→CJS"未启用**：--sea 直接通过测量，无需降级模块格式。

**对 B1 的直接推论**：Loader 的价值建立在**运行时按 Node 标准规则解析真实 node_modules 树**之上。因此 B1 服务的构建产物不能把 dsh 插件 bundle 进单文件 JS（esbuild/tsup 打平会消灭裸包名解析）——**产物形态必须是真实 node_modules 树**（容器镜像层）。这一点与 Bazel 的 hermetic 产物习惯有张力，见 §5。

### 3.4 三路线对照总表

| | 路线一 bin-only | 路线二 closed runtime 闭包 | 路线三 SEA 单文件 |
|---|---|---|---|
| 插件归属 | config 工程 | 宿主 package 的 dependencies | VFS 内 package tree |
| `bareModuleBaseUrl` | 不传 | `import.meta.url` | `import.meta.url`（VFS 内） |
| 产物 | bin（1 个依赖：app-boot） | node_modules 树 + config | 单文件 ~174MB |
| 官方实例 | `dsh-jsonrpc-agent` bin | packaged-bin / dev node carrier | Python runtime wheels |
| 对 B1 服务的映射 | 低（谁拥有 config 谁装插件） | **高**（服务镜像携带闭包） | 不适用（进程已存在，无需嵌 Node） |

---

## 4. 关键依赖与约束

### 4.1 `node-addon-require-builtin`（裸包名稳定解析的实际前提）

（npm registry 元数据 + [vendor/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/vendor/README.md)）

- **是什么**：Node-API addon，`requireBuiltin(id)` 可加载任意 Node 内部模块（无白名单变体）。JS 入口仅 ~4KB unpacked；native 二进制以 **optionalDependencies 平台包**分发（darwin-x64/arm64、linux-x64-gnu、linux-arm64-gnu、win32 三件），无需现场编译；无匹配平台包时 **fail closed**（不编译未验证的本地源码）。
- **谁依赖它**：vendored loader 的普通依赖（`loader requires node-addon-require-builtin@^0.1.4`）；dsh 官方 app bins 全部安装它（"Repository bins install Loader's optional `node-addon-require-builtin` peer"）。
- **缺失时的行为**：`loader.internal === undefined` → 相对路径仍工作（锚定 baseUrl）；**裸包名 fallback 到 `import(name)`，锚点变成 loader 包自己在 node_modules 里的位置**。在 hoisted 布局（npm/`--config.node-linker=hoisted`）下通常仍能解析到根 node_modules；在 pnpm 默认严格布局（`.pnpm` 隔离树）下，从 loader 的虚包位置向上只能看到 loader 自己声明过的依赖，**裸包名大概率解析失败**。
- **风险记录**：个人维护者（imccyu，也是 dsh npm 包的 publisher 之一）、2026-07-09 才首发、0.1.x。属于"loader 内部机制的供应链单点"。用 `bareModuleBaseUrl`（模式 B）也**依赖 internal loader 可用**（HostResolvedRootInclude 在 `internal === undefined` 时退回 super.import，锚点语义丢失）——所以 B1 + 裸包名场景实际上**需要**这个 addon。
- **2026-08-22 更新**：最新版 0.1.5（2026-08-14，与 0.1.0-rc.7 同日发布）。0.1.2 起**移除了 install script**（0.0.11/0.1.0 尚有 `scripts.install`，之后 fileCount 7→5、JS 部分缩至 ~4.3KB），原生二进制选择完全经由 `node-addon-native-custom-loader` 依赖通道，**无安装期代码执行**——供应链面改善。发版与 dsh rc 线同节奏（每个 dsh rc 伴随一个 addon 版本），"个人单点"定性可修正为"dsh npm publisher 本人维护的配套仓库，但仍是 0.1.x 早期版本"。另核实 `@deepseek-ai/dsh@0.1.1-rc.2` 主包以**直接 dependencies** 声明 `node-addon-require-builtin ^0.1.4`（"Repository bins install Loader's optional peer"的 manifest 层印证）。（来源：[registry](https://registry.npmjs.org/node-addon-require-builtin)、[dsh 0.1.1-rc.2 manifest](https://registry.npmjs.org/@deepseek-ai/dsh/0.1.1-rc.2)）

### 4.2 peer 依赖闭包：B1 消费者要装齐什么

（npm registry 上 `@deepseek-ai/dsh-app-boot@0.1.0-rc.6` 的 manifest + 仓库内 package.json）

`dsh-app-boot` 的直接 runtime 依赖只有 `js-yaml`；peer 是：`@deepseek-ai/cordis`、`cordis-plugin-loader`、`cordis-plugin-include`、`cordis-plugin-group`、`cordis-plugin-hmr`（optional）、`dsh-home-paths`、`dsh-invariants`、`dsh-launch-environment`、`dsh-system-prompt`。而组合清单里每个插件行（如 `dsh-tool-todo`）又把自己的 dsh 依赖全部声明为 peers。

含义：**B1 消费者的安装闭包 = app-boot 的 peers ∪ 组合中每个插件的 peer 闭包**。pnpm 严格模式下未满足的 peer 要么装不上要么运行期 PENDING（Loader 审计会报 "pending (waiting for services: ...)"）。官方对策是 §3.2 的闭包清单 + `verify-runtime-closure` 校验；嵌入方要么复用同样工艺，要么把 node-linker 设为 hoisted（官方 deploy 即如此）换取"根 node_modules 全可见"的宽松解析。

**2026-08-22 实证更新（peer 闭包断裂在 npm 消费端系统性发生）**：[Discussion #1032](https://github.com/deepseek-ai/deepseek-harness/discussions/1032)（2026-08-14）证实**官方自己的 npm 分发路线在干净环境不闭合**：`dsh-app-boot` 的 `lib/index.js` 顶层静态 import `@deepseek-ai/cordis-plugin-group`，但该包只声明在 peerDependencies（dependencies 仅 `js-yaml`）；`@deepseek-ai/dsh` 主包 dependencies 亦未包含它——npm/pnpm 对 peer 的自动安装不保证，`npx @deepseek-ai/dsh web` 干净环境启动即崩 `ERR_MODULE_NOT_FOUND`（[#982](https://github.com/deepseek-ai/deepseek-harness/discussions/982)/[#1030](https://github.com/deepseek-ai/deepseek-harness/discussions/1030) macOS/Windows 双平台独立复现）。跟进事实（#1032 评论）：rc.7 仍可复现；`cordis-plugin-group` 只是**16 个缺失包之首**，其余 15 个是 dsh-base 等 bundle 的传递 peers（dsh-sandbox、dsh-scope、dsh-subprocess、dsh-timeout、dsh-shell、dsh-spill、dsh-session-telemetry 等），且**部分 dsh-\* 包的 npm `latest` dist-tag 停在陈旧 0.0.1-rc.x 线**（exports 不匹配，如 `SessionTelemetryBackend` 缺失），必须逐包精确 pin 到同一 rc。2026-08-22 核对 `@deepseek-ai/dsh@0.1.1-rc.2` manifest：dependencies（60 余项）仍无 `cordis-plugin-group`，上述 15 个传递 peers 亦全部不在——**截至 0.1.1-rc.2 未修复，且讨论无任何官方回应**。同族发布完整性问题：[#273](https://github.com/deepseek-ai/deepseek-harness/discussions/273)（app bin 漏 `dsh-app-boot` 依赖）、[#984](https://github.com/deepseek-ai/deepseek-harness/discussions/984)（`dsh-type-meta` 未发布 npm，社区插件 pnpm 安装 404）。对 B1 的三点操作含义：① 闭包 manifest 必须把 peer 集合**迭代到不动点**（每个列出的包自身的 peers 也要装），`verify-runtime-closure` 式静态校验是硬性门禁而非最佳实践；② 版本锁定必须**全家桶精确 pin**——dist-tag 不可信（不同包的 latest 停在不同版本线），且 prerelease semver range 不跨 `[major, minor, patch]` 元组匹配（`^0.1.0-rc.x` 不匹配 `0.1.1-*`），跨 minor 升级 = 整闭包同步 bump；③ "peer 由消费者装齐"是 app-boot README 的**设计立场**（§2.2 引文），断裂点在官方 npx 分发自身未装齐——B1 嵌入方以自管闭包对冲正是该立场的要求，而非权宜之计。

### 4.3 版本与发布状态

- `@deepseek-ai/dsh-app-boot` npm 首版发布 **2026-08-10**（0.0.1-rc.1），主体调研基线 0.1.0-rc.6（2026-08-13）；**2026-08-22 核对**：已推进到 0.1.0-rc.7（08-17）→ 0.1.0-rc.8（08-19）→ 0.1.1-rc.1/rc.2（08-21），发布节奏约 2 天一版。早期版本 `publishConfig.access: "restricted"`，**rc.6 起 public**。版本全在 0.x-rc 区间，README 明示 developer preview、破坏性变更预期。
- **dist-tag 分化（2026-08-22 核对）**：`latest` 停在 **0.1.0-rc.6**，`next` 走到 **0.1.1-rc.2**——08-17 起的发布全部打在 `next`；且据 [#1032](https://github.com/deepseek-ai/deepseek-harness/discussions/1032) 的 workaround 记录，部分 dsh-\* 包的 `latest` 甚至停在 0.0.1-rc.x 旧线。**任何 dist-tag 或裸 semver 范围都不可作为安装依据，必须精确 pin**（prerelease range 语义见 §4.2 实证更新）。
- rc.6 → 0.1.1-rc.2 间 app-boot 的 peer 结构未变：cordis 系（cordis/loader/include/group/hmr-optional）稳定在 `^4.0.1`/`^1.0.x` 非 rc 范围，dsh 系四个包（home-paths/invariants/system-prompt/launch-environment）精确到 rc 号（`^0.1.1-rc.2` 等）——"cordis 底座稳定、dsh 包互锁到同一 rc 线"的分层愈发清晰。
- license 字段从 BSD-3-Clause（8 月 10-12 日版本）变为 MIT（0.1.0-rc.2 起，与仓库整体 MIT 一致）。
- Cordis 是 vendor 进仓库的（`vendor/README.md`：auditable/patchable/pinned，全部改 scope 到 `@deepseek-ai`；`pnpm-workspace.yaml#linkWorkspacePackages` 让保留的 semver 范围解析到 pinned workspace）。对 npm 消费者表现为正常发布的 `@deepseek-ai/cordis@^4.0.1` 等真实包。
- 0.1.x 新版本速览（仅列与 B1 相关项，[releases](https://github.com/deepseek-ai/deepseek-harness/releases)）：rc.7 升级 node-pty 1.2 beta（PTY 平台兼容性改善）；rc.8 SQLite 会话后端**数据结构不兼容变更**、Claude Code/Codex 子代理改为 Profile Bundle 按需安装（A 模式用户侧路线的扩展，不影响 B1 闭包结论）、Python SDK 闭包扩充（见 §3.2 更新）；0.1.1-rc.1 修复 Bubblewrap 沙箱经 `/proc/<pid>/root` 的逃逸（B1 若启用 dsh-sandbox 的 shell 工具，**0.1.1-rc.1+ 是安全基线**）。

### 4.4 HMR 与文件监听

closed runtime 不含 HMR 依赖也能工作（hmr 是 optional peer）；`watchUserPatches` 仅在显式调用且 hmr 服务存在时生效。容器化 B1 服务（镜像只读层）应视为静态组合——修改组合 = 改 YAML + 重新部署。

---

## 5. 与本仓库工具链的兼容性输入（事实对照，非决策）

本仓库现状：`projects/game/agent/` 为 pnpm catalog 管理（`pnpm-workspace.yaml`）+ Bazel 构建（`MODULE.bazel:125` `node.toolchain(n = "24.14.0")`）+ oci_image 部署（`projects/game/agent/service.yaml`）。

| 维度 | 事实 | 与 B1 的关系 |
|---|---|---|
| Node 版本 | dsh 要求 `^22.19 \|\| >=24`；本仓库 Bazel toolchain 24.14.0、`@types/node` ^22.20.1 | 兼容（24.14 满足；internal loader 走 Node 24 v2 接口） |
| 依赖管理 | TS 版本统一在 `pnpm-workspace.yaml` catalog | dsh 的 `@deepseek-ai/*` npm 包可入 catalog；但 dsh 包全在 0.x-rc 高频漂移，与 catalog 的"统一版本"治理节奏存在摩擦（每 rc 升级都是全组合联动） |
| 产物形态 | Bazel + oci_image | **Loader 依赖运行时真实 node_modules 解析（§3.3）**：产物必须携带物化 node_modules 树（pnpm deploy / pnpm install --prod 或等价物进镜像层）；不能把插件 bundle 进单文件。Bazel 的 hermetic runfiles 树不适用于运行时 Loader 解析，运行时环境必须脱离 bazel 沙箱 |
| native 依赖 | 本仓库已有 node-gyp/build 体系 | `node-addon-require-builtin`（平台 optionalDeps，0.1.2 起无 install script，见 §4.1 更新）+ 若用 PTY 工具则 `node-pty`（官方 rc.7 起用 1.2 beta）——容器镜像需按平台选好 |
| 闭包校验 | 无对应物 | 官方 `verify-runtime-closure` 的思路（遍历组合闭包校验 peers）可移植为镜像构建检查 |

---

## 6. 拆分视角：dsh 底座 × 插件增量（分析框架）

一个自然的简化：把 B1 打包拆成"dsh 部分"（固定版本、打包方式固定）与"插件部分"（显式指定）。**该拆分成立，且与官方 closed-runtime 工艺同构**（runtime wheel 的纯依赖 manifest 即"dsh 部分一次锁定"的极致形态）——但拆分线需要精确化。

### 6.1 拆分线不在"包的归属"，而在"解析面 × 启用面"

npm 包维度上"dsh vs 插件"没有硬边界（自研插件也是普通 npm 包；官方插件也只是 peer 闭包的一员；也不存在一个叫 `dsh` 的单包——"dsh 部分" = app-boot + peers + 选定的官方插件闭包）。真正正交的两层是：

| 面 | 载体 | 性质 | 变更频率 |
|---|---|---|---|
| **解析面**（什么可被解析） | 物化的 node_modules 闭包 | 可一次锁定为**不可变快照** | 随 dsh 版本升级（整体动作） |
| **启用面**（什么实际挂载） | `cordis.yml` 的行 | 显式、每行一个插件 | 随业务组合调整 |

**安装 ≠ 启用**：快照里装了全部官方包，minimal 组合也只挂载 12 行；未挂载的包不占 fiber、不注册服务、不进 prompt（jsonrpc-agent 的 minimal 变体正是跑在装了 ~100 包的 bundled runtime 里的）。因此"固定 dsh"可以安全地取**全集闭包**——付出的只是镜像体积，不是运行面复杂度。

### 6.2 简化后的四层模型

```
层 1  Cordis 底座（cordis + loader/include/group[/hmr]）   ┐
层 2  dsh 服务包（~100 个 @deepseek-ai/*，全集或裁剪）      ├─ 底座快照：一次锁定、整体升级
（native addon：node-addon-require-builtin [+ node-pty]）  ┘
层 3  自研插件（自己的 npm 包，显式依赖声明）                ─ 增量：随业务版本节奏
层 4  组合清单 cordis.yml                                    ─ 部署工件：显式指定启用哪些行

产物 = 层 1+2 物化 node_modules + 层 3 正常依赖安装 + 层 4 YAML
启动 = boot(name, configYml, …, import.meta.url)
```

决策从"理解 ~100 个包的依赖关系"降为三个独立动作：

- **决策 A（一次性）**：底座快照取全集还是精确闭包（§6.3）；
- **决策 B（业务侧）**：组合清单写哪些行；
- **增量检查**：自研插件的 peer ⊆ 底座快照（`verify-runtime-closure` 思路收窄为增量校验）。

"固定版本的 dsh 打包方式固定"之所以成立：官方包同 monorepo、同发布流水线、peer 互锁到同一 rc 线——它们天然是一个**原子快照**，无需逐包理解，整体锁定/整体升级即可。

### 6.3 底座快照的两个自由度

| | 全集闭包（runtime wheel 路线） | 精确闭包（jsonrpc-agent 路线） |
|---|---|---|
| 闭包 manifest | 列全部官方包（~100） | 只列组合需要的（minimal ≈ 12 行对应包） |
| 改组合加官方插件 | 只改 YAML，不动镜像 | 需改 manifest + 重建镜像 |
| 镜像体积 / 校验面 | 大 / 全集一次校验 | 小 / 每次改组合重校验 |
| 适用 | 配置自由度优先（面向外部用户） | 组合固定（面向自有服务） |

组合固定的自有服务倾向精确闭包；但 preview 期组合本身在探索，全集闭包可减少"改一行 YAML 重建镜像"的摩擦——这是一个可后置的权衡，甚至可以先全集、稳定后收缩。

### 6.4 拆分后仍然存在的耦合（不可被拆分消除）

1. **不能 bundle 的约束作用于整棵树**：层 1–3 都依赖运行时真实 node_modules 解析（§3.3 否决项），产物形态约束不变。
2. **peer 版本互锁随快照升级联动**：dsh 0.x-rc 期 peer 范围精确到 rc 号，底座快照升级 = 层 3 自研插件的 peer 范围同步调整 + 重跑增量校验。拆分把这件事显式化为"快照升级 → 增量重验"两步，但没有消除。
3. **native addon 归入固定部分**：`node-addon-require-builtin` 随快照走、目标容器内一次验证即可——拆分后它反而更简单了（不再需要每个业务方单独关注）。

### 6.5 双树模型：底座树 × 插件树，各自维护 node_modules、发布时拼合（§6.2 的深化）

上文的四层模型仍隐含"一棵物化树"（§3.2 官方工艺）。更进一步的拆分是**两棵各自独立维护 node_modules 的依赖树，发布时只做目录拼合**——dsh 与插件说到底都是 node_modules 里的 TS 产物，两棵树之间唯一的耦合界面是 **peer semver 范围**（+ 组合 YAML 行）。这个形态 Node 原生支持（就是 nested node_modules 布局），且与 dsh 包的设计精确吻合：

```
deploy 产物/
├── node_modules/                 ← Part 1 底座树（独立 manifest/lockfile 维护、整体升级）
│   ├── @deepseek-ai/dsh-app-boot
│   ├── @deepseek-ai/dsh-llm-deepseek        （官方插件闭包）
│   └── node-addon-require-builtin           （native addon 归此树）
├── plugins/
│   ├── my-plugin-a/
│   │   ├── package.json                       （dsh 相关全部声明为 peer，不入本地 node_modules）
│   │   ├── node_modules/                     ← Part 2 插件自己的真实依赖（独立维护）
│   │   └── lib/index.js
│   └── my-plugin-b/…
├── cordis.yml                    ← 官方插件行 = 裸包名；自研插件行 = './plugins/my-plugin-a'
└── server.mjs                    ← boot(name, configYml, …, import.meta.url)
```

**为什么恰好可行——两条解析通道各归各树**（对应 §2.2 四路径）：

| 解析需求 | 通道 | 锚点 | 落到哪棵树 |
|---|---|---|---|
| YAML 里官方插件的裸包名 | ② internal loader + `bareModuleBaseUrl` | `server.mjs` 位置（产物根） | Part 1 根 node_modules |
| 自研插件模块 | ③ 相对路径（`./plugins/...`） | config 目录 | 插件包自身 |
| 插件代码内部的 `import '@deepseek-ai/dsh-tools'` | 普通 Node ESM 解析（不经 Loader） | 插件模块文件位置，向上走 | 自己的 node_modules 没有（peer 不安装）→ 向上命中 Part 1 根 ✓ |
| 插件自己的非 dsh 依赖 | 同上 | 同上 | 命中插件自己的 node_modules ✓ |

即：**Loader 只负责解析 YAML 行（锚 Part 1），插件内部依赖由 Node 标准向上解析（先自己、后 Part 1）**——两棵树的边界与两条解析通道的边界重合。

**三个成立条件**：

1. **单例硬约束（最关键）**：`cordis`/`cordis-plugin-loader` 等"框架核心"只允许存在于 Part 1。app-boot README 原文："The built `dsh-app-boot` artifact embeds the statically mounted Include implementation while leaving Loader external, **so the include tree and host bind to one Loader peer**"。若插件树里重复安装了 cordis/loader，会出现两个 Loader 实例、`ctx.registry` 类型合并失效、fiber 体系分裂。**操作面**：插件把全部 dsh 相关依赖声明为 `peerDependencies`（不实际安装），并关闭 pnpm `auto-install-peers`——这正是 dsh 官方包自己的做法（§2.4：官方插件包全部 peer 化声明），也是官方 deploy 脚本显式 `--config.auto-install-peers=false` 的理由（"prevents undeclared peers from expanding the closure"）。install 时 unmet peer 仅告警，运行时靠目录布局向上解析满足。
2. **YAML 两类行各用各的通道**：官方插件行写裸包名（锚 Part 1 根）；自研插件行写相对路径 `./plugins/<pkg>`（相对 config 解析到插件包入口，插件内部 import 再从插件位置向上）。不把自研插件混入 Part 1 的根 node_modules——否则两棵树又退化为一棵。
3. **版本契约**：每个插件 peer 范围 ⊆ Part 1 实际安装版本（`verify-runtime-closure` 思路收窄为"Part 1 升级 → 对 Part 2 逐插件跑一次 peer 范围校验"）。

**与官方 hoisted 工艺的差异说明**：官方 deploy 用 `node-linker=hoisted` + symlink 物化，是 **pkg SEA 的约束**（VFS 不支持 symlink，§3.2 第 2 步），不是 dsh Loader 的要求。B1 容器场景无 symlink-free 需求，**可保留 pnpm 默认 symlink 布局**：Node 默认 `preserveSymlinks=false` 会 realpath 化模块位置，`.pnpm` 内部的依赖解析（插件自身依赖）与从产物根锚定的裸包名解析（Loader 走同一 Node 内部加载器，语义与普通 import 相同）在 symlink 布局下均正常。

**比单棵快照树多出的收益**：

- 两部分**独立 CI/独立版本节奏**：底座树低频变更 → 独立 docker layer 缓存（厚层不变，插件层薄、重建快）；
- 镜像体积按需：底座树可取全集闭包（§6.3），插件树只携带真实增量；
- 契约面收窄：Part 1 升级的兼容性检查对象从"整棵树"变为"Part 2 各插件的 peer 范围声明"；
- 开发期同构：本仓库 pnpm workspace 里，底座树 = 一个纯依赖 manifest workspace 包（python/sdk-runtime 模式），插件 = 普通 workspace 包，workspace link 天然满足 peer；发布期的"拼合"与开发期的"workspace 聚合"是同一语义的两种物化。

**不因双树而消失的**：bundle 禁令与 native addon 归属（§6.4 原样成立）；0.x-rc 期 peer 范围精确到 rc 号，Part 1 minor 级升级即可能 break Part 2 的 `^0.x` 范围——双树把这件事显式化为契约检查，但没有消除（§7 风险 3）。

---

## 7. 风险与限制记录（调研发现，非决策）

1. **`node-addon-require-builtin` 是裸包名解析的单点**：个人维护、0.1.x、2026-07 首发；缺失时 pnpm 严格布局下裸包名 fallback 大概率失败（§4.1）。任何 B1 PoC 应第一步验证该 addon 在目标容器内的安装与加载。
2. **"Everything is a Plugin" 的代价落在依赖面上**：B1 消费者面对的是 app-boot 的 9 个 peers + 组合内每个插件的 peer 树（§4.2）；官方用零代码闭包 manifest 包 + CI 校验管理。组合越裁剪（jsonrpc-agent minimal 12 行 vs 官方 full ~100 包），闭包维护越是显式工作。
3. **0.x-rc 高速漂移**：npm 包发布仅数日（2026-08-10 起，约 2 天一版），peer 范围精确到 rc 版本号（如 `^0.1.0-rc.6`）；升级 = 整闭包联动，无 semver 稳定承诺。**漂移已出现不兼容实例**：rc.8 的 SQLite 会话后端数据结构不兼容（§4.3 速览）；dist-tag 不同步使裸范围/dist-tag 安装不可行（§4.2 实证更新）。
4. **产物形态约束**：插件不可 bundle 进单文件（§3.3 否决项），必须是真实 node_modules 树——与"瘦镜像/单文件产物"的偏好冲突；官方 SEA 路线产物 ~174MB 可作体积量级参考（含 Node runtime；B1 复用服务进程 Node，体积 = node_modules 闭包本身）。
5. **无社区 B1 实践先例（2026-08-22 复核：结论不变，A 模式生态在萌芽）**：仍未检索到第三方"boot() 进程内嵌入自有服务"的实践或坑记录；官方自身的 B1 消费者（`dsh` CLI、acp-demo、jsonrpc-demo bin）全部在仓库 monorepo 内（workspace 依赖形态），**跨仓库 npm 消费的公开先例仅 Python/TS SDK 的 B2 子进程形态**（runtime wheel / spawn）。但 A 模式（profile 安装）生态一周内快速成形：第三方插件 [anweat/dsh-browser](https://github.com/anweat/dsh-browser)（Playwright 浏览器服务插件，**把 Playwright/OpenCLI 打包为插件本地依赖、chromium 内核走共享缓存复用**——与 §6.5"插件树自带 node_modules"同构的社区实例）及其消费者 `dsh-web-search-pro`；社区手册 [Electricitysheep/dsh-handbook](https://github.com/Electricitysheep/dsh-handbook)（已系统整理依赖/发布坑位）；第三方文档站 deepseekdocs.com、dsh-in-depth.com。这些实践全部发生在 profile/`dsh plugin add` 路线（A 模式），进一步佐证 B1 仍是无人区。
6. **B1 路线未被官方文档作为一级场景运营**：`dsh-app-boot` README 面向 "app bins"；embedding 场景只在个别处提及（如 cmdline 包的 "An embedding host with no command line provides an empty list"）。in-process 嵌入目前是"机制上完全支持、官方叙事上未承诺"的状态。
7. **`boot()` 的 bin 语义残留**：参数名 `binName`（诊断前缀）、`installFailLoud`（进程级 unhandledRejection → exit(1)）、`loadEnv`（读 cwd 的 .env）都是 bin 场景假设；B1 服务嵌入时应只取 `boot()` 本体，env/进程守卫按服务自身生命周期自理（jsonrpc-demo runner 已示范这种取用方式）。
8. **官方 npm 分发链当前不可靠（2026-08-22 新增）**：发布完整性缺陷成系列出现且无官方回应（§4.2 实证更新：#1032 静态 import 声明为 peer、16 包缺失、dist-tag 停旧线、#984 包未发布）。B1 消费必须以四件套对冲：**自管闭包 manifest（迭代 peer 到不动点）+ 全家桶精确 pin + 安装后静态校验全部可解析 + boot 全组合冒烟**；底座快照每次升级需重跑全部校验。

---

## 8. 对 `projects/game/agent/` 的调研性含义（非决策）

1. **机制层面两个问题的答案都已闭环**：B1 下插件 = 普通 ESM npm 包，由 Loader 按"锚定 baseUrl 的 Node 标准解析"动态 import 并以 Cordis 插件挂载；随 package 打包 = 依赖闭包进产物 + `boot(..., import.meta.url)`（模式 B），有官方 closed-runtime 工艺（纯依赖清单 + pnpm deploy + 闭包校验）可对照复刻；进一步可按 §6.5 双树模型把"底座树 × 插件树"独立维护、发布拼合。**若做 PoC，验证顺序建议**：① 目标容器内装 `node-addon-require-builtin` 并确认加载；② 最小组合（参照 `examples/jsonrpc-agent/minimal.cordis.yml` 的 12 行）+ hoisted 物化 node_modules；③ `boot()` + `import.meta.url` 锚定跑通裸包名；④ 再评估 pnpm 严格布局（双树 symlink 布局）或 Bazel 产物路径。
2. **与前置调研结论的关系**：integration-modes §4.4 判定"A 模式服务化缺口大、B2 是标准姿势但 dsh 无现成长驻表面"；本次补上了 B1 的机制细节——**dsh 的 B1（boot in-process）在机制上不存在 A 模式的那些缺口**（无 loopback-pinned 特权面、无单用户信任假设、无 idle eviction 问题——树的生命周期由业务进程自理），代价是全部依赖面（§4.2）与 preview 期内部 API 风险（§7 风险列表 3、6）落在自己进程内。三拓扑对比因此更完整：A（dsh 当应用框架）、B1（dsh 当库：app-boot + 闭包）、B2（dsh 当子进程：SDK/runtime）。
3. **B1 不改变 preset 调研的 N 计价结论**（preset §7.3、§9.3）：嵌入 dsh 的理由仍然不会是 prompt/preset 机制本身；若未来评估，B1 的对照收益点在 integration-modes §4.1 记录的插件增量能力（`agent.inject()`、事件流、execution world 替换）直接进程内可用、无 IPC 桥接损耗。
4. **2026-08-22 增量核对对本调研结论与 PoC 建议的修订**：① 机制层结论（§2、§3）经源码提交历史核实全部在 0.1.1-rc.2 上有效，无需返工；② PoC 起步版本应取 **0.1.1-rc.2**（含 Bubblewrap 逃逸修复），但**全闭包精确 pin、不使用任何 dist-tag**（§4.2/§4.3）；③ 验证顺序在原"① 容器内装 node-addon-require-builtin"之前增加**步骤⓪：闭包 manifest 迭代 peer 到不动点 + 安装后静态校验全部可解析**——官方 npx 路线在干净环境的断裂（#1032）证明这一步不能假设安装器会代劳；④ 官方 SDK runtime 闭包已扩至全集（§3.2 更新），若走"全集闭包"快照可直接以 master 的 `python/sdk-runtime/package.json` 为参照清单（将其 `workspace:^` 替换为精确版本）。
5. **后续调研**：`survey/deepseek-harness-b1-bazel-packaging.md`（2026-08-22）——在本文机制结论之上回答"B1 服务的原材料构成与 bazel 打包可行性"：原材料四类模型（底座闭包/cordis.yml/自研插件/bootstrap）、现有 `artifact_pkg_js` 链路的 172 包闭包拍平实证、底座封装为闭包清单 workspace 包（`js_runtime_library` 单点引用模式）、`dsh_pkg` 作为 `artifact_pkg_js` 变体 macro 的设计与收敛面。

---

## 9. 引用来源汇总

仓库外（官方文档/仓库/源码）：

- https://github.com/deepseek-ai/deepseek-harness
  - packages/boot/app-boot/README.md（boot/mountRootInclude/bareModuleBaseUrl 语义、bare specifier 解析规则、Known Limitations）
  - packages/boot/app-boot/src/index.ts（boot 与 mountRootInclude 源码：HostResolvedRootInclude 的锚定实现）
  - packages/boot/app-boot/package.json（peer 依赖清单）
  - vendor/loader/src/index.ts、vendor/loader/src/internal.ts、vendor/loader/src/config/tree.ts、vendor/loader/src/config/entry.ts（Loader 与 EntryTree.import 四路径、internal module loader、Node 内部加载器获取通道）
  - vendor/README.md（Cordis vendor 化、`@deepseek-ai` rescope、node-addon-require-builtin 为 vendored 第三方依赖）
  - packages/examples/jsonrpc-demo/README.md、src/runner.ts、src/bin.ts、src/packaged-bin.ts、package.json（B1 最小样板；bin-only 与 closed-runtime 双入口；"only dependency is app-boot"）
  - examples/jsonrpc-agent/README.md、cordis.yml、package.json（无人值守组合与运行时变量）
  - python/README.md、python/sdk-runtime/README.md、python/sdk-runtime/package.json（runtime wheel、纯依赖清单 deploy root、双 carrier）
  - .agents/notes/implemented/architecture/2026-07-10-single-file-executable-sdk-runtime-distribution.md（SEA 打包路线、闭包即插件集、被否替代方案、pnpm deploy 参数依据）
  - docs/cookbook/extension-cookbook.md（插件形态样例：hook/tool/UI/协议驱动）
  - docs/cordis-tutorial/07-into-the-harness.md（相对路径本地插件、组合 YAML 写法）
  - docs/user/develop/basic/publish.md（dsh.bundle manifest 与 profile 安装路线——B1 不需要的另一条路）
  - packages/sdk/server/README.md（jsonrpc 插件、stdout 纪律、shutdown 语义）
- https://registry.npmjs.org/node-addon-require-builtin（native addon 元数据：平台 optionalDeps、fail-closed、版本时间线；2026-08-22 核对 0.1.5、install script 移除）
- https://registry.npmjs.org/@deepseek-ai/dsh-app-boot（npm 发布状态：2026-08-10 起的 rc 版本线、peer 范围、license 变更；2026-08-22 核对 dist-tag latest=0.1.0-rc.6 / next=0.1.1-rc.2）
- https://github.com/deepseek-ai/deepseek-harness/releases（2026-08-22 核对：dsh-v0.1.0-rc.7/rc.8、dsh-v0.1.1-rc.1/rc.2 release notes——node-pty 1.2 beta、SQLite 不兼容变更、Profile Bundle、Python SDK 闭包扩充、Bubblewrap 修复）
- https://github.com/deepseek-ai/deepseek-harness/discussions/1032（2026-08-14：干净环境 npx 崩溃根因——静态 import 声明为 peer、16 包缺失、dist-tag 停旧线；2026-08-22 核对 0.1.1-rc.2 未修复、无官方回应；同族 #982/#1030/#273/#984）
- https://registry.npmjs.org/@deepseek-ai/dsh/0.1.1-rc.2（主包 dependencies 核对：60 余项中无 cordis-plugin-group 及 15 个传递 peers）
- https://github.com/anweat/dsh-browser（第三方插件：插件本地 node_modules 打包 Playwright/OpenCLI、内核共享缓存复用——A 模式生态与 §6.5 同构实例）
- https://github.com/Electricitysheep/dsh-handbook（社区手册：依赖/发布坑位汇总）
- https://deepseek-harness.github.io/deepseek-harness/reference/cordis-primer（Cordis 入门：插件五概念、Loader 配置）
- https://thenewstack.io/deepseek-harness-open-source-plugins/（开源时间线报道）
- https://yage.ai/share/dsh-deep-analysis-en-20260813.html（第三方深评：in-process 命令式插件模型与复杂度权衡）

仓库内：

- `survey/deepseek-harness-framework.md`（前置调研：框架架构、§5 嵌入方式）
- `survey/deepseek-harness-preset.md`（前置调研：preset 机制、§2.4 包名解析、§9 概念解构）
- `survey/deepseek-harness-integration-modes.md`（前置调研：A/B1/B2 拓扑、§2.2 TS in-process 主路线）
- `pnpm-workspace.yaml`、`MODULE.bazel`（本仓库 Node 工具链与 catalog 现状，§5 对照）
- `projects/game/agent/package.json`、`projects/game/agent/service.yaml`（现状对照）
