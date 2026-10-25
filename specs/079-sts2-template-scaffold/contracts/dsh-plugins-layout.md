# Contract: dsh-plugins 目录布局（通用/专用分组）

> Feature: [specs/079-sts2-template-scaffold/spec.md](../spec.md)（FR-005~FR-008）| 决策：[research.md](../research.md) D5/D6（用户裁定：基础包落位 `saolei/game`）
> 本契约定义 `common/js/dsh-plugins/` 的目录布局规则、包名映射与打包通道不变量——后续所有插件（通用或场景专用）的落位以本契约为准。

## §1 布局规则

```text
common/js/dsh-plugins/
├── <通用插件>/…            # 平铺；场景无关、可被任意场景组合消费
├── saolei/                 # saolei 场景专用分组
│   ├── game/               #   游戏基础（game foundation，specs/075 分层）
│   ├── loop/               #   team 编排
│   ├── tools/              #   模型工具面
│   ├── memory/             #   memory 能力 adapter
│   └── hooks/              #   hook 引擎
└── sts2/                   # sts2 场景专用分组（本期仅骨架）
    └── game/               #   游戏基础骨架（空 apply）
```

1. **分组判据**：插件若仅服务单一游戏场景 → 归入 `<场景>/` 分组；场景无关（基建/LLM/team 原语/preset 基座等）→ 顶层平铺。
2. **分组目录不是包**：分组目录无 `package.json`、无 `BUILD.bazel`，仅承载子包；每个子目录是完整 workspace 包（`package.json` + `BUILD.bazel` + `tsconfig.json`，README 按需）。
3. **分组内命名**：以场景内的**能力角色**命名子目录（`game`/`loop`/`tools`/`memory`/`hooks`），不重复场景名。

## §2 包名映射（目录 ≠ 包名）

| 目录 | 包名 | 归属 | 迁移来源 |
|---|---|---|---|
| `dsh-plugins/saolei/game/` | `@dominion/dsh-saolei` | saolei 专用 | `dsh-plugins/saolei/`（整体下移） |
| `dsh-plugins/saolei/loop/` | `@dominion/dsh-saolei-loop` | saolei 专用 | `dsh-plugins/saolei-loop/` |
| `dsh-plugins/saolei/tools/` | `@dominion/dsh-saolei-tools` | saolei 专用 | `dsh-plugins/saolei-tools/` |
| `dsh-plugins/saolei/memory/` | `@dominion/dsh-saolei-memory` | saolei 专用 | `dsh-plugins/saolei-memory/` |
| `dsh-plugins/saolei/hooks/` | `@dominion/dsh-saolei-hooks` | saolei 专用 | `dsh-plugins/saolei-hooks/` |
| `dsh-plugins/sts2/game/` | `@dominion/dsh-sts2` | sts2 专用 | 新建（骨架） |
| `dsh-plugins/{desktop-bridge,grpc-runtime,llm-glm,llm-opencode-go,preset-authoring,team}/` | `@dominion/dsh-*` 同名 | 通用 | 不动 |

**不变量**：

- 全部包名在迁移前后不变——组合清单（`projects/game/agent_v2/cordis.yml`）按**裸包名**经宿主 node_modules 解析，与目录路径无关。
- 依赖方向维持 specs/075 裁定：`loop`/`tools`/`hooks` → `game`（游戏基础不依赖上层）；跨场景不互相依赖。
- pnpm workspace glob `common/js/**`（`pnpm-workspace.yaml`）递归覆盖分组子包，无需修改。

## §3 组合清单 sts2 惰性行（用户裁定 2026-10-22）

- `projects/game/agent_v2/cordis.yml` 新增：`- { id: sts2, name: '@dominion/dsh-sts2' }`（置于 `saolei` 领域行邻近，注释标明脚手架占位）。
- 骨架插件形态：插件导出 `name: "sts2"`（与清单行 `id` 一致）、`inject: []`、空 `apply`——不 provide 服务、不注册工具/prompt section；清单行 `name` 为包名 `@dominion/dsh-sts2`；测试断言插件形态防漂移。
- pinned 断言与契约同步：`projects/game/agent_v2/src/dsh.test.ts` 22→23 行；`specs/070-dsh-upgrade/contracts/composition-manifest.md` 修订为 23 行终态。

## §4 打包通道不变量（迁移/新建必须满足）

1. **workspace 包交付通道**：进入 agent_v2 部署闭包的唯一途径是 `js_runtime_library` `:runtime_pkg` 经 `server_pkg.runtime_deps`（`projects/game/agent_v2/BUILD.bazel`）；workspace 包**严禁**列入 `npm_deps`（部署缺文件的既定教训，见 BUILD 注释）。
2. **裸名解析需要 manifest**：`js_library :pkg` 的 `srcs` 必须含 `package.json`（组合清单按裸包名动态 import 依赖它，`dsh-plugins/saolei/game/BUILD.bazel` 注释先例）。
3. **node_modules 忽略校验**：每个包目录的 `node_modules` 必须列入根 `.bazelignore`（`MODULE.bazel` 以 `npm_translate_lock.verify_node_modules_ignored = "//:.bazelignore"` 声明；rules_js 文档将该属性标注为 Bazel 7.x 语义，若当前 Bazel 版本不再触发，则以 `bazel build //...` 无 node_modules 告警为准）——迁移后旧条目删除、新条目补齐，并清理残留的 `common/js/dsh-plugins/saolei-interrupt/node_modules`。
4. **lockfile 重生成**：目录移动后 `pnpm-lock.yaml` 的 importer 键与 `link:` 相对路径必须经 `bazel run @pnpm -- --dir <仓库根绝对路径> up` 重算，禁止手改（AGENTS.md）。
5. **git 历史**：目录迁移一律 `git mv`（含基础包两跳下移 `saolei/` → `saolei/game/`），禁止复制重建。

## §5 骨架包验收面（`@dominion/dsh-sts2`）

- 可独立构建（`ts_project` 类型检查通过）与测试（`vitest_test` 断言插件形态：name/inject 字段值、apply 为可调用函数）。
- 构建产物出现在 agent_v2 部署闭包 `node_modules/@dominion/dsh-sts2`（`artifact_pkg_js` 打包面）。
- 挂载惰性行后服务正常启动（每次大型测试部署即运行时验证）；README 记录定位与演进入口（`survey/sts2-game-control.md` §5、`survey/sts2-mod-poc.md` §5）。
