# Research: sts2 场景模板脚手架与 dsh-plugins 通用/专用分层

> Feature: [specs/079-sts2-template-scaffold/spec.md](spec.md)
> 调研输入：`survey/sts2-game-control.md`、`survey/sts2-mod-poc.md`、`survey/sts2-llm-agent-landscape.md`、代码库实读（本文件引用均为仓库相对路径）。
> 本文件解决 spec 显式遗留的三个计划级决策（挂载形式、占位词表形态、执行顺序）与全部实现路径问题；两项已经用户裁定（D3、D8）。

## D1: template 注册面 = 双端常量集合，无运行时注册表

**Decision**: `sts2` 的注册仅扩容两处固定集合，不引入任何注册表/配置机制：

- Go 侧（唯一事实源）：`projects/game/pkg/gameconst/const.go` 新增 `var Sts2Template = game.TemplateName{TemplateID: "sts2"}` 并加入 `knownTemplateIDs`；`projects/game/pkg/gameconst/const_test.go` 补 sts2 用例。
- TS 侧镜像：`projects/game/agent_v2/src/server.ts` 的 `KNOWN_TEMPLATES` 加入 `"sts2"`（注释已声明其为 gameconst 的 TS-side mirror）。

**Rationale**: `specs/031-team-template-mode/spec.md` FR-001 裁定 Template 是无 CRUD 的资源消息、具体值为常量（非 proto enum）；session/memory/wiki/proxy handler 均经 `ValidateTemplateName`/`IsKnownTemplateID` 校验（`projects/game/session/handler/handler.go:52,92,110,131` 等），白名单扩容即自动支持 sts2，四个 Go 服务与 gateway 零改动（gateway 路由由 proto `templates/*` 通配生成）。

**Alternatives considered**:
- proto enum / ListTemplates RPC——031 已明确否决（违反 FR-001/FR-024）。
- 配置文件驱动的模板集合——引入第二事实源，与"固定集合常量"裁定冲突，无收益。

## D2: 场景表（`SceneSpec`）泛化 = agent_v2 内一张按 template 索引的场景表

**Decision**: 在 agent_v2 新增场景定义模块（建议 `src/scenes.ts`，与 `server.ts`/`session.ts` 同级），以一张只读表承载宿主侧全部场景差异：

- 字段（最小集）：
  - `roster`：该场景 team 物化的固定成员角色集合（saolei = `["player","planner"]`；sts2 = `[]`），成员数/角色集合校验与成员键均取自本字段；
  - `presetRoles`：该场景 preset 角色词表（saolei = `["player","planner"]`；sts2 = `[]`）；
  - `rolePoolTemplate`：角色 → preset-authoring 池模板 id 映射（saolei = `{player:"player", planner:"planner"}`，即现 `ROLE_TEMPLATE`）；
  - `orchestratable`：team 编排是否接线（saolei = `true`；sts2 = `false`）；
  - `teamGoal`：宿主侧共享 team section 的任务目标文案（saolei = 现 `TEAM_GOAL` 原文零变化，`projects/game/agent_v2/src/session.ts:91`；sts2 不适用）。
- 消费点改造（等价迁移，saolei 行为零变化）：
  - `server.ts`：`PresetRole`/`isSceneRole`/`ROLE_TEMPLATE`/`SAOLEI_TEMPLATE`（`server.ts:242-260`）改为按请求 parent template 查场景表；preset 资源名构造（`server.ts:264`）改用请求的 parent template 段（当前唯一可接线场景为 saolei，产出字符串不变）。
  - `session.ts`：materialize 入口在结构/roster 校验之前按 `parsedSession.template` 查 `orchestratable`（false → `INVALID_ARGUMENT` fail-loud）；roster 校验（原 `validateSaoleiMembers`，`session.ts:999-1013`）改由 `roster` 供给成员数与角色集合（错误文案由 template + roster 构造，saolei 逐字节不变）；`TEAM_GOAL` 常量改由场景表提供。

**Rationale**: 现状散落三处 saolei 假设（词表/池映射、资源父名、roster 校验 + 任务文案），集中为一张表后 sts2 的"未接线"是数据（空 roster/词表 + `orchestratable: false`）而非散布的 if 分支；后续场景 feature 只需为 sts2 增填字段/接入编排，通用代码不再动（spec FR-003、SC-005）。

**Alternatives considered**:
- 场景差异下沉到各场景插件包（由插件提供词表/校验）——插件 currently 无法参与 RPC 入参校验（preset/team 校验发生在 handler 层），且骨架期为空插件无从提供；过度设计。
- 仅加 `if (template === "sts2")` 分支——违背 spec FR-003（通用路径不得残留场景专属硬编码）与宪法原则 II。

## D3: 组合清单挂载 sts2 惰性行（用户裁定 2026-10-22）

**Decision**: `projects/game/agent_v2/cordis.yml` 在 `saolei` 领域行旁新增一行 `- { id: sts2, name: '@dominion/dsh-sts2' }`（带注释标明脚手架占位、空 apply、场景 feature 填充实现）。同步修订：

- `projects/game/agent_v2/src/dsh.test.ts` 组合清单 pinned 断言 22 行 → 23 行（`dsh.test.ts:607-660` 的 id 与 name 双列表）；
- `specs/070-dsh-upgrade/contracts/composition-manifest.md` 契约同步为 23 行终态（宪法原则 VII：契约表述当前终态）。

**Rationale**: 每次服务启动与大型测试部署都实际走一遍 sts2 包的"打包 → 部署 → Loader 裸名解析 → 挂载"通道，打包通道在脚手架期即被持续验证；后续场景 feature 只改插件包内容、不动宿主清单与断言。

**Alternatives considered**:
- 不挂载（包仅进依赖闭包）——加载通道缺运行时验证，场景 feature 还需再动宿主清单+断言+契约；被用户否决。

## D4: sts2 场景化操作统一 fail-loud 语义 = `INVALID_ARGUMENT` "scene not wired"

**Decision**: 未接线场景（本期 sts2）的全部场景化 RPC 返回 `INVALID_ARGUMENT`，错误文案遵循现有句式（对齐 `server.ts:816` 的 `"...must be a known saolei scene role..."` 风格），建议统一模式：`scene "sts2" is not wired yet` / `template "sts2" has no preset vocabulary`。覆盖面：

- AgentService `UpdateTeam`（team 物化，经 `sessions.materialize` 场景分派）；
- PresetService `CreatePreset`/`ListPresets`/`GetPreset`/`UpdatePreset`/`DeletePreset`（parent 或资源名 template 段为 sts2）；
- 数据面 RPC（SessionService/MemoryService/WikiService CRUD、AgentService 会话流）不受影响——模板解析经 `KNOWN_TEMPLATES`/gameconst 白名单接受，行为与 saolei 同构。

**Rationale**: FR-004 要求 fail-loud 且不产生半成品状态；`INVALID_ARGUMENT` 与既有场景校验错误码一致（`TeamSessionErrorCode`，`session.ts:96`），不引入新错误码。preset 存储当前为全局单库（`deps.authoring.list(roleFilter)` 无 template 维度，`server.ts:858` 附近），sts2 拒绝在入口层完成，存储模型零改动。

**Alternatives considered**:
- sts2 preset 返回空列表而非报错——空列表是"合法但无内容"的语义，会诱导调用方认为场景已接线；与 fail-loud 裁定冲突。
- 为 preset 存储增加 template 分区——本期无 sts2 preset 语义，属场景 feature 范围（spec FR-009 负向边界）。

## D5: 插件归组目标布局与同步清单（基础包落位 `saolei/game`，用户裁定）

**Decision**: 目标布局（包名全部不变，仅目录移动，`git mv` 保历史）：

```text
common/js/dsh-plugins/
├── desktop-bridge/  grpc-runtime/  llm-glm/  llm-opencode-go/  preset-authoring/  team/   # 通用插件（不动）
├── saolei/                                                                                  # saolei 专用分组
│   ├── game/    # @dominion/dsh-saolei（游戏基础，specs/075 裁定的 game foundation）
│   ├── loop/    # @dominion/dsh-saolei-loop
│   ├── tools/   # @dominion/dsh-saolei-tools
│   ├── memory/  # @dominion/dsh-saolei-memory
│   └── hooks/   # @dominion/dsh-saolei-hooks
└── sts2/                                                                                    # sts2 专用分组（D6）
    └── game/    # @dominion/dsh-sts2（骨架包）
```

同步清单（全部必改，缺一则构建/校验必坏）：

| # | 文件 | 变更 |
|---|---|---|
| 1 | `pnpm-lock.yaml` | 经 `bazel run @pnpm -- --dir <仓库根绝对路径> up` 重新生成（importer 键与 `link:` 相对路径随目录变化；禁止手改） |
| 2 | `projects/game/agent_v2/BUILD.bazel`（runtime_deps） | 5 个 saolei 包 label 改为 `//common/js/dsh-plugins/saolei/<子目录>:runtime_pkg` |
| 3 | `common/js/dsh-plugins/saolei/loop/BUILD.bazel` | 对基础包的 runtime 依赖改指 `//common/js/dsh-plugins/saolei/game:runtime_pkg`（team 依赖不变） |
| 4 | `.bazelignore` | 删 5 条旧 `common/js/dsh-plugins/saolei*/node_modules` 与残留 `common/js/dsh-plugins/saolei-interrupt/node_modules`；增 5 条新路径（`MODULE.bazel` `npm_translate_lock.verify_node_modules_ignored` 强校验） |
| 5 | 各包 `tsconfig.json` paths | 相对路径按新层级重算（saolei 组内互引与对 `@dominion/game-saolei-board` 的映射、`projects/game/agent_v2/tsconfig.json` 的 saolei-loop 条目） |
| 6 | 迁移包自身 README/源码注释中的旧路径 | 终态化（宪法原则 VII）；`specs/`、`survey/` 历史文档不改写 |

**Rationale**: `pnpm-workspace.yaml` 的 `common/js/**` 递归 glob 已覆盖嵌套包（现有 `common/js/grpc/otel` 等先例），workspace 零改动；运行时按包名解析（cordis.yml 裸名 + 宿主 node_modules 链接），目录移动对加载零影响——影响面全部是构建期结构性配置。子目录名 `game/loop/tools/memory/hooks` 与 075 裁定的分层术语一致。

**Alternatives considered**:
- `saolei/saolei/`（目录名=包名）——双重命名冗余，用户已裁定 `saolei/game`。
- saolei-* 各包平铺改前缀名（如 `saolei-game/`）——不形成分组目录，无法容纳"通用/专用"二级结构，sts2 组亦无处安放。

## D6: 骨架包形态与接线

**Decision**: `common/js/dsh-plugins/sts2/game/` 建包 `@dominion/dsh-sts2`：

- 形态对齐既有插件包（参照 `common/js/dsh-plugins/saolei/package.json` + `BUILD.bazel`）：`package.json`（name/private/type:module/main/types/exports；peerDeps 仅 `@deepseek-ai/cordis: catalog:` 类型面）、`tsconfig.json`、`BUILD.bazel`（`ts_config` + `ts_project :lib` + `js_library :pkg`（srcs 含 package.json，裸名解析需 manifest）+ `js_runtime_library :runtime_pkg`（package_name `@dominion/dsh-sts2`）+ `vitest_test` 骨架）、`src/index.ts`、`src/index.test.ts`、`README.md`。
- `src/index.ts`：**惰性 cordis 插件**——导出符合插件形态的对象（`name: "sts2"`、`inject: []`、空 `apply`），不 provide 任何服务、不注册工具/section。README 记录定位（sts2 场景基础占位）、空实现现状与演进入口（引用 `survey/sts2-game-control.md` §5、`survey/sts2-mod-poc.md` §5）。
- 接线（对齐 saolei-hooks/memory/tools 的"清单挂载+宿主声明"模式，`cordis.yml:9-17` 注释）：
  - `projects/game/agent_v2/package.json` 增 `"@dominion/dsh-sts2": "workspace:*"`；
  - `projects/game/agent_v2/BUILD.bazel` `server_pkg.runtime_deps` 增 `//common/js/dsh-plugins/sts2/game:runtime_pkg`（workspace 包严禁进 `npm_deps`）；
  - `cordis.yml` 挂惰性行（D3）；`.bazelignore` 增 `common/js/dsh-plugins/sts2/game/node_modules`。
- agent_v2 源码**不 import** 骨架包（无类型/值引用）——仅清单挂载，故 `server_lib.deps` 无需变更。

**Rationale**: 与 saolei 分层（game foundation）同构的占位；空 apply 惰性行保证加载通道验证不引入任何行为；测试骨架断言插件形态（name/inject/apply 可调用）防漂移。

**Alternatives considered**:
- 骨架包直接放 `dsh-plugins/sts2/` 根（无 game 子目录）——与 saolei 组不对称，场景 feature 增 tools/loop 时还要再挪一次。
- 骨架包提供 `ctx.sts2` 空 service 面——过度设计；场景 feature 定义真实服务面时反而要拆。

## D7: 前端常量扩展

**Decision**:

- desktop：`projects/game/desktop/frontend/src/api.ts` 增 `TEMPLATE_STS2 = 'sts2'` 并入 `TEMPLATES`（注释由 "saolei only" 终态化为双模板）；`App.svelte` 模板下拉随 `TEMPLATES` 自动呈现 sts2。desktop Go 后端零改动（`app.go` 的 template 参数全透传，无本地校验）。
- web：`projects/game/web/frontend/src/App.tsx` 的模板常量参数化为集合 + 当前场景选择常量（值仍 `saolei`，5 处使用点 `App.tsx:574,589,722-724` 与常量定义 `:47`）；**不加场景切换 UI**（spec FR-005）。`PresetsView` 的 `ROLE_OPTIONS` 与 `MEMBER_VIEWS` 不动（web 场景 = saolei，词表正确）。
- `projects/game/web/frontend/src/scene-words.ts` 场景词表增 `sts2`、`杀戮尖塔`（管理视图"场景无关"扫描覆盖新场景词）。
- `projects/game/fake-desktop/cmd/main.go` 的 `FAKE_DESKTOP_TEMPLATE` 默认值不动（saolei）。

**Rationale**: desktop 是真机控制面，下拉是既有本地常量机制（031 FR-024：禁止模板列表请求）；web 管理界面本期仍以 saolei 为唯一场景，仅做常量结构与扫描表准备，避免 UI 范围膨胀。

**Alternatives considered**:
- web 同步加场景切换 UI——spec Assumptions 明确排除，属场景 feature。

## D8: 执行顺序 = 归组 → 注册+泛化 → 骨架（用户裁定 2026-10-22）

**Decision**: 三个工作包按 ①插件归组（纯机械移动+结构配置同步，零行为变化）→ ②sts2 注册+场景泛化（逻辑改动）→ ③骨架包+清单接线 顺序执行；①③为结构态、②为逻辑态，各自独立可验证，阶段边界即 review 边界。

**Rationale**: 目录大挪移与逻辑改动不混杂（两次 review 都干净）；①完成后 sts2/ 分组目录与通用/专用布局先落定，③的骨架包直接建在终态布局上；②的泛化只动源码与测试，不被①的路径噪声干扰。可中断恢复性：每阶段结束全绿，中断后从下一阶段继续。

**Alternatives considered**:
- 先泛化后归组——泛化阶段触碰的 BUILD/路径会在归组时二次变更，review 噪声加倍；被用户否决。
- 归组+骨架合并一阶段——单阶段体积过大（移动+新建+配置同步混合）；被用户否决。

## D9: 验证策略

**Decision**:

- 每阶段例行门禁：`bazel build //...` + 相关 target 单测（宪法原则 IV，开发任务内嵌，不单列 task）。
- 阶段①末与阶段③末各跑一次完整大型测试（`projects/game/testplan`，经 testplan skill `guitar run` 部署→测试→清理闭环）：①证明"纯移动零回归"，③为 feature 终验收（宪法原则 VI：全部用例通过，failed/flaky 即不通过）。
- SC-001/SC-004 的产物级验证：构建产物含 `node_modules/@dominion/dsh-sts2`；sts2 数据面 CRUD 与场景化 fail-loud 的运行时验证由单测（agent_v2 handler 测试，对齐 `server.test.ts`/`session.test.ts` 既有 fake deps 模式）与 quickstart 手工场景承载。

**Rationale**: ①是"零行为变化"承诺的高风险点，必须有端到端证据；②的泛化正确性由既有 saolei 单测全量回归覆盖（行为不变即测试不变）；③验收对齐宪法 VI。

**Alternatives considered**:
- 仅终期跑一次大型测试——①若引入回归要跨两个阶段回溯定位，排查成本高。
