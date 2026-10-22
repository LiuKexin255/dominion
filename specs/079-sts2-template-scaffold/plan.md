# Implementation Plan: sts2 场景模板脚手架与 dsh-plugins 通用/专用分层

**Branch**: `079-sts2-template-scaffold` | **Date**: 2026-10-22 | **Spec**: [specs/079-sts2-template-scaffold/spec.md](spec.md)

**Input**: Feature specification from `/specs/079-sts2-template-scaffold/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

## Summary

为 `projects/game` 引入第二个场景模板 `sts2`（杀戮尖塔2）的**脚手架**：双端注册 template id（Go `gameconst` 白名单 + agent_v2 TS 镜像）、将 agent_v2 中散落的 saolei 场景假设泛化为按 template 索引的场景表（saolei 行为零变化，sts2 场景化操作 fail-loud；TS 模板镜像 `KNOWN_TEMPLATES` 与场景表同批落地，避免注册先行时 preset 资源名仍拼 saolei 常量造成跨模板写入）、创建 `@dominion/dsh-sts2` 惰性骨架插件并挂载组合清单惰性行；同时将 5 个 saolei 专用 dsh 插件归组到 `common/js/dsh-plugins/saolei/{game,loop,tools,memory,hooks}`（基础包落位 `saolei/game`，用户裁定），实现通用/专用插件的目录分层。正式场景接入（工具面、desktop 桥 relay、fake-sts2、preset 池模板）明确排除（spec FR-009）。执行顺序裁定：归组 → 注册+泛化 → 骨架（`specs/079-sts2-template-scaffold/research.md` D8）。

## Technical Context

**Language/Version**: TypeScript（ESM / NodeNext，`style/javascript.md`）+ Go（`style/golang.md`）；构建/测试入口 bazel；JS workspace 为 pnpm（catalog 统一管版本）。

**Primary Dependencies**: cordis/dsh 插件体系（`@deepseek-ai/cordis`、dsh-app-boot 裸名 Loader）、aspect_rules_ts / aspect_rules_js（`ts_project`/`js_library`/`js_runtime_library`/`npm_translate_lock`）、grpc（agent_v2.proto handlers）、Wails v2（desktop 前端）、Vitest（JS 测试）。

**Storage**: MongoDB——session/memory/wiki/proxy 均已按 template 键分区（`projects/game/session/runtime/mongo/model.go` 等），本期零改动、零迁移。

**Testing**: JS 单测 Vitest（`tools/dev/js/vitest_test.bzl`）；Go 单测 `go test`；大型测试 `projects/game/testplan`（testplan skill / guitar，`style/large_test.md`）。

**Target Platform**: Linux 服务（gateway/session/memory/proxy/agent_v2/web）+ Windows 桌面（desktop，Wails）。

**Project Type**: monorepo 服务型应用（多服务 + 前端 + JS 插件包）。

**Performance Goals**: N/A——本期为结构性脚手架，无运行时行为变化（saolei 零回归即目标）。

**Constraints**: saolei 全链路行为零变化（spec FR-003/FR-010）；组合清单 pinned 断言与 `specs/070-dsh-upgrade/contracts/composition-manifest.md` 契约必须同步（22→23 行）；workspace 包仅可经 `runtime_deps` 打包通道交付（禁入 `npm_deps`）；pnpm-lock.yaml 禁止手改（经 pnpm 重生成）。

**Scale/Scope**: 2 个场景模板（saolei、sts2）；5 个 saolei 专用插件包归组 + 1 个 sts2 骨架包新建（6 个通用插件包不动）；agent_v2/desktop/web 三端常量与校验泛化。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁（宪法原则） | 评估 | 状态 |
|---|---|---|
| 1. 文档阅读门禁（V） | tasks.md 阶段将按三分类显式列出（含 `style/javascript.md`、`style/golang.md`、`style/large_test.md` 及其外部引用、`survey/sts2-*`、本目录 research/contracts） | ✅ 通过 |
| 2. 实现门禁（II 重构式 / III 接口优先） | 泛化即重构式变更（场景假设收敛为场景表，而非打分支补丁，`specs/079-sts2-template-scaffold/research.md` D2）；新增对外行为（sts2 fail-loud 语义、目录布局契约）先定契约（`specs/079-sts2-template-scaffold/contracts/scene-dispatch.md`、`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md`）再实现 | ✅ 通过 |
| 3. 编译+单测门禁（IV） | 每阶段内嵌 bazel build + 相关单测；大型测试按 VI 单列验收 task | ✅ 通过 |
| 4. 引用、图表与终态门禁（I / VII / VIII） | 全部产物引用仓库相对路径/完整 URL；迁移包 README/注释路径终态化（specs/survey 历史文档不改写）；图表统一 Mermaid | ✅ 通过 |
| 5. 大型测试验收门禁（VI） | 阶段①末（纯移动零回归）与终验收（全部用例通过，guitar 实跑闭环）单列 task | ✅ 通过 |

无违规项——无需 Complexity Tracking 豁免条目。Phase 1 设计后复核：契约（scene-dispatch / dsh-plugins-layout）已按终态表述、无迭代残留，门禁维持通过。

## Project Structure

### Documentation (this feature)

```text
specs/079-sts2-template-scaffold/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── scene-dispatch.md      # agent_v2 场景分派契约（模板集合/能力矩阵/fail-loud 语义）
│   └── dsh-plugins-layout.md  # dsh-plugins 目录布局契约（通用/专用分组/包名映射/清单行）
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
# 变更落点（均为仓库既有目录，本期不新建顶层目录）
common/js/dsh-plugins/
├── desktop-bridge/  grpc-runtime/  llm-glm/  llm-opencode-go/  preset-authoring/  team/  # 通用插件（不动）
├── saolei/                      # [新分组] saolei 专用：基础包 saolei/ 下移为 game/，平铺的 saolei-{loop,tools,memory,hooks} 迁入对应子目录
│   ├── game/    # @dominion/dsh-saolei（自 dsh-plugins/saolei/ 整体下移）
│   ├── loop/    # @dominion/dsh-saolei-loop
│   ├── tools/   # @dominion/dsh-saolei-tools
│   ├── memory/  # @dominion/dsh-saolei-memory
│   └── hooks/   # @dominion/dsh-saolei-hooks
└── sts2/                        # [新分组] sts2 专用
    └── game/    # @dominion/dsh-sts2（新建骨架包，`specs/079-sts2-template-scaffold/research.md` D6）

projects/game/
├── pkg/gameconst/const.go(+const_test.go)          # [改] Sts2Template + 白名单（D1）
├── agent_v2/
│   ├── src/scenes.ts                               # [新] 场景表（D2）
│   ├── src/scenes.test.ts                          # [新] 场景表内容单测（D2）
│   ├── src/server.ts (+server.test.ts)             # [改] KNOWN_TEMPLATES/preset 词表/资源父名泛化
│   ├── src/session.ts (+session.test.ts)           # [改] roster 校验/TEAM_GOAL 场景分派
│   ├── src/dsh.test.ts                             # [改] 组合清单断言 22→23 行（D3）
│   ├── cordis.yml                                  # [改] sts2 惰性行（D3）
│   └── package.json / BUILD.bazel                  # [改] dsh-sts2 依赖 + runtime_deps（D6）
├── desktop/frontend/src/{api.ts,api.test.ts,App.test.ts}   # [改] TEMPLATE_STS2 + TEMPLATES + 下拉断言（D7）
└── web/frontend/src/{App.tsx,scene-words.ts}       # [改] 模板常量集合化 + 场景词（D7）

# 根配置
.bazelignore                # [改] saolei 组新路径 + sts2/game + 清理 saolei-interrupt 残留（D5）
pnpm-lock.yaml              # [改] pnpm 重生成（D5）
specs/070-dsh-upgrade/contracts/composition-manifest.md  # [改] 23 行终态同步（D3，宪法 VII）
```

**Structure Decision**: 不引入新顶层结构；`dsh-plugins` 内采用"通用平铺 + 专用分组（`saolei/`、`sts2/`）"二级布局，分组目录非 Bazel 包（无 BUILD.bazel，各子包独立 target），pnpm workspace `common/js/**` 递归 glob 天然覆盖（`specs/079-sts2-template-scaffold/research.md` D5）。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无宪法违规项，本表为空。
