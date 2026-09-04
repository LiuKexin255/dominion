# Revision: Phase 11 testplan 重构回归两部署拓扑（用户裁定 2026-09-04）

**Feature**: [spec.md](../spec.md) | **契约**: [contracts/testplan.md](../contracts/testplan.md) | **日期**: 2026-09-04 | **性质**: 推翻 Phase 11 原设计的"deploy 合并"半，回归 051 directive 意见 1 处置形态；含工作区未提交实现的返工处置（设计任务，不含代码变更）

## 0. 裁定输入与冲突定位

用户裁定（2026-09-04，原话）：

> 同一个 deploy 部署两个同名服务之前已经被否决，需要拆分为两个部署拓扑

历史否决（051 directive 意见 1，[specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md](../../051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md) §0/§1）：

> deploy 不能配置两个相同名称的 service；fake-desktop won/drop 的区别只在环境变量，不应改名单部署两个相同服务，应拆成两个 suite 搭配不同 deploy 配置

054 原 `contracts/testplan.md` §1 设计的"既有实例改名 `fake-desktop-won` + 新增 `fake-desktop-drop` 实例进**同一 deploy**"（[research.md](../research.md) D11 决策 1 同源）正是该否决的形态——054 设计时误读约束为仅"同一 deploy 内服务不能同名"，实际否决的是"改名单部署两个相同服务"这一形态本身。用户裁定回归两拓扑：deploy 不合并，suite 按 deploy 拆分。用户指令②的核心要求（"将使用相同部署拓扑的模块测试用例合并到同一 suite，减少测试执行时间"，[plan.md](../plan.md) Input）在两拓扑形态下依然成立——6 个 won 拓扑 suite 归并为 1。

## 1. Binary 归并可行性核实（裁定：保持独立 target）

原 T023"disconnect 用例并入 game 文件 + 删 target"的前提（拓扑合一）随 deploy 合并被否决而失效。对备选方向"复用同一 binary 按测试函数筛选"（`style/large_test.md` "按 suite 编排"一节提及"不同 suite 可以复用同一个测试 binary，通过不同的测试函数筛选关注点"）按 guitar 实现核实：

- `tools/test/guitar/pkg/config/config.go`：`Suite.Cases` 为 `[]string`，仅承载 bazel target 路径，无 per-case 结构。
- `tools/test/guitar/pkg/run/run.go` `runTests`：`args = append(args, suite.Cases...)`——cases 作为**位置参数**直接拼进单条 `bazel test --config=largetest` 命令，无任何 per-case/per-suite 测试函数过滤机制（无 `-run`/`--test_filter` 通道）。
- 将 bazel flag 塞入 case 字符串不构成受支持的能力（flag 作用于整条 bazel 调用的全部 target，且违背 case=target 语义）。

**裁定**：维持 051 directive §1.4 的拆分形态——disconnect 用例独立文件（`agent_v2_game_disconnect_test.go`）+ 独立 target（`agent_v2_game_disconnect_test`）+ 独立 suite（drop deploy）。反向验证：若 disconnect 函数并入 game 文件且 game target 含该文件，主 suite 在 won 拓扑执行 game target 时会运行依赖 drop 拓扑的函数而必然失败。

> 附注：`style/large_test.md` "复用 binary 按函数筛选"的表述与 guitar 实现不符（如上证据）。风格文档修订不在本 feature 范围，本设计以 guitar 实现与 051 先例为准。

## 2. 终态设计

### 2.1 Deploy：两拓扑保持，服务不改名

| 文件 | fake-desktop 实例 | env | 服务的 suite |
|---|---|---|---|
| `deploy_agent_v2.yaml` | `fake-desktop`（单实例） | `FAKE_DESKTOP_SESSION=desktop-e2e-won`、`FAKE_DESKTOP_SCENARIO=won` | `game-system` |
| `deploy_agent_v2_drop.yaml` | `fake-desktop`（单实例） | `FAKE_DESKTOP_SESSION=desktop-e2e-drop`、`FAKE_DESKTOP_SCENARIO=progressive`、`FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS=3` | `game-disconnect` |

- 服务名**不改为** `fake-desktop-won`/`fake-desktop-drop`：guitar 为每个 suite 生成独立环境（`game.<runID>`），两 deploy 的服务名空间互不可见；改名仅为"单 deploy 内区分两实例"服务，两拓扑形态下无此诉求（051 directive §1.2 单服务终态延续）。
- 两份 deploy 文件的既有（HEAD）内容即终态，零变更。

### 2.2 Suite：7→2

- 主 suite `game-system`（`deploy_agent_v2.yaml`）：7 个 case 按"配置面→对话面→游戏面→桌面面"顺序串行执行（session → memory → web → conversation → preset → game → desktop-flow；desktop-flow 用例测试自驱，对 fake-desktop 实例无依赖）。
- 断连 suite `game-disconnect`（`deploy_agent_v2_drop.yaml`）：`agent_v2_game_disconnect_test` 单 case，置于主 suite 之后（游戏面的故障分支殿后；guitar suite 串行、失败即停，主干回归优先暴露）。
- 细节（YAML 骨架、描述要求）见 [contracts/testplan.md](../contracts/testplan.md) §2。

### 2.3 Binary：8 个 target 全部保持

不归并、不删除（依据本文 §1）；gazelle 默认名约束不变（`testplan_test` 保持默认名 target）。

### 2.4 执行预算

部署次数 7→2（省 5 次部署 + 5×60s settle）；`--timeout` 按重构后实测校准（README 预算说明同步）。

## 3. 验收锚点调整

- 原"全仓清理目标：`rg deploy_agent_v2_drop` 零命中"**作废**（drop deploy 保留）。
- 替换锚点：`rg 'fake-desktop-won|fake-desktop-drop'` 全仓零命中——被否决合并形态的实例名不得残留于 YAML/注释/README/文档。
- Phase 11 Goal 的预算表述由"部署次数 7→1"修正为 **7→2**。

## 4. 工作区返工处置

工作区存在按已否决"单 deploy 合并"形态完成、**未提交**的 Phase 11 实现，逐文件处置：

| 文件 | 工作区现状（否决形态） | 处置 |
|---|---|---|
| `projects/game/testplan/deploy_agent_v2.yaml` | 双实例（`fake-desktop-won` + `fake-desktop-drop`） | **git restore**（HEAD 即终态：单实例 `fake-desktop` + won env） |
| `projects/game/testplan/deploy_agent_v2_drop.yaml` | 已删除 | **git restore**（恢复） |
| `projects/game/testplan/agent_v2_game_disconnect_test.go` | 已删除（用例并入 game 文件） | **git restore**（恢复；已核实迁入 game 文件的函数体为逐字搬迁，无内容需保留） |
| `projects/game/testplan/BUILD.bazel` | 删除 disconnect target、改写 game target 注释 | **git restore**（两 target 均恢复） |
| `projects/game/testplan/agent_v2_game_test.go` | 并入 disconnect 函数与 `gameFlowReconnectWait` + 头注释改双实例表述 | **git restore**（disconnect 内容回独立文件） |
| `projects/game/testplan/agent_v2_helpers_test.go` | 常量注释改双实例表述 | **git restore**（恢复两 deploy 表述） |
| `projects/game/testplan/system_test.yaml` | 1 suite（合并形态） | **改写**为 2 suite 终态（主 suite 名/case 顺序可复用，描述需去除双实例与 disconnect 表述并新增断连 suite；T022） |
| `projects/game/agent_v2/README.md` | 单 suite 单部署表述 | **改写**为两 suite 两拓扑表述（T025） |
| `projects/game/fake-desktop/README.md` | 单部署双实例表述 | **改写**为两拓扑表述（套件名按新编排更新；T025） |
| `projects/game/testplan/README.md` | 未改（仍为 7 suite 表述） | **改写**：§2 suite 表 7→2、§5 执行预算 7→2（T025） |

## 5. 上游文档同步清单

| 文档 | 变更 |
|---|---|
| [contracts/testplan.md](../contracts/testplan.md) | §1–§3/§5 按两拓扑两 suite 终态改写（§4 用例面不变） |
| [research.md](../research.md) D11 | 追加两拓扑修正注（指向本文） |
| [data-model.md](../data-model.md) §6 | 两拓扑两 suite 终态改写 |
| [plan.md](../plan.md) | Summary 第 5 点、Testing、Performance Goals、Project Structure、Constitution Check VII、下游执行建议第 6 条 |
| [quickstart.md](../quickstart.md) §2 | 预期改为两 suite 两部署、预算 7→2 |
| [tasks.md](../tasks.md) Phase 11 | Goal/Independent Test/文档清单/T021–T025 按本文重写 |
