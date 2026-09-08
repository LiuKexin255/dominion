# Contract: testplan 重构（两拓扑两 suite 归并）

**Feature**: 用户指令②（2026-09-03）+ FR-024 | **Research**: [research.md](../research.md) D11（含两拓扑修正注） | **裁定**: [revisions/phase11-two-deploy-topologies.md](../revisions/phase11-two-deploy-topologies.md) | **规范**: `style/large_test.md`

现状基线（调研实证）：`projects/game/testplan/system_test.yaml` 单计划 7 suite / 2 deploy——6 个 suite 复用**逐服务完全相同**的 `deploy_agent_v2.yaml` 却各自独立部署（7 次部署 + 7×60s settle，超 guitar 默认 10m 预算）；`deploy_agent_v2_drop.yaml` 与主 deploy 唯一差异是 fake-desktop 的 env。测试文件/helper 组织已合规（无反模式），仅编排需重构。deploy 维持两拓扑（用户裁定 2026-09-04，回归 051 directive 意见 1 形态——不将 won/drop 两个同名服务合并进同一 deploy）。

## 1. Deploy：两拓扑保持（零变更）

| 文件 | fake-desktop 实例 | env | 服务的 suite |
|---|---|---|---|
| `deploy_agent_v2.yaml` | `fake-desktop`（单实例） | `FAKE_DESKTOP_SESSION=desktop-e2e-won`、`FAKE_DESKTOP_SCENARIO=won` | `game-system` |
| `deploy_agent_v2_drop.yaml` | `fake-desktop`（单实例） | `FAKE_DESKTOP_SESSION=desktop-e2e-drop`、`FAKE_DESKTOP_SCENARIO=progressive`、`FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS=3` | `game-disconnect` |

- 两份 deploy 文件保持既有形态（服务清单逐项相同、仅 fake-desktop env 不同，051 directive §1.3），**零变更**。
- 服务名不改 `fake-desktop-won`/`fake-desktop-drop`：guitar 为每个 suite 生成独立环境（`game.<runID>`），两 deploy 服务名空间互不可见；单 deploy 内区分两实例的改名诉求不存在（裁定依据见 revision §2.1）。

## 2. Suite 归并（7→2）

`system_test.yaml`：7 suite → **2 suite**：

```yaml
suites:
  - name: game-system
    deploy: //projects/game/testplan/deploy_agent_v2.yaml
    endpoint: { http: { public: https://game.liukexin.com } }
    cases:   # 顺序执行（guitar 串行），配置面→对话面→游戏面→桌面面
      - //projects/game/testplan:testplan_test          # session 模块
      - //projects/game/testplan:memory_test            # memory 模块
      - //projects/game/testplan:web_test               # web 静态托管
      - //projects/game/testplan:agent_v2_conversation_test
      - //projects/game/testplan:agent_v2_preset_test
      - //projects/game/testplan:agent_v2_game_test     # won 拓扑游戏面
      - //projects/game/testplan:desktop_flow_test
  - name: game-disconnect
    deploy: //projects/game/testplan/deploy_agent_v2_drop.yaml
    endpoint: { http: { public: https://game.liukexin.com } }
    cases:
      - //projects/game/testplan:agent_v2_game_disconnect_test  # mid-game 断连三局序列
```

- **部署次数 7→2**；cases 串行执行（`tools/test/guitar/README.md`：suites/cases 按 YAML 顺序串行）。
- suite 顺序：主 suite 先、断连 suite 后——游戏面的故障分支殿后；guitar suite 串行、失败即停，主干回归优先暴露。
- desktop_flow 用例留在主 suite：其用例测试自驱（扮演 desktop 客户端），对 deploy 中 fake-desktop 实例无依赖（051 directive §1.5 核实）。
- 跨 case 隔离：各 case 使用唯一 session/preset 资源名前缀（既有 helper 已具备唯一名构造的，统一走 helper）。
- 执行预算：测试净时长不变；省 5 次部署 + 5×60s settle（预期总时长显著回落；`--timeout` 参数按重构后实测校准并更新 README 预算说明）。

## 3. Binary：8 个 target 全部保持（不归并）

- `agent_v2_game_disconnect_test` 维持独立文件与独立 target（用例绑定 `desktop-e2e-drop` session，依赖 drop 拓扑）。guitar 无测试函数筛选能力：`tools/test/guitar/pkg/config/config.go` 的 `Cases []string` 仅承载 target 路径，`tools/test/guitar/pkg/run/run.go` `runTests` 将 cases 作为位置参数直接拼进单条 `bazel test --config=largetest` 命令——若 disconnect 函数并入 game 文件，主 suite 在 won 拓扑执行 game target 时会运行依赖 drop 拓扑的函数而必然失败（裁定依据见 revision §1）。
- gazelle 默认名约束不变（`testplan_test` 保持默认名 target）。

## 4. 本 feature 新增/更新的测试用例（FR-024 大型测试面）

按模块归位（`style/large_test.md` "按模块拆分"）：

| 模块文件 | 新增/更新 |
|---|---|
| `agent_v2_conversation_test.go` | +step 字段分段断言（NDJSON 事件携带 step、回填每 step 一条）；+ERROR 回合已产出内容回填可见（注入失败）；+:cancel 全语义（终止/CANCELED 终态/排队落地/幂等/后续 Send 可用）；+GetAgent desktop_connected（连接/无连接——"有连接"经主拓扑 `desktop-e2e-won` session，主 suite 内可测） |
| `agent_v2_preset_test.go` | 模型目录断言更新（glm-5.3/glm-5.3-flash、默认值、未知 id 拒绝） |
| `agent_v2_game_disconnect_test.go` | 既有断连三局序列零回归（drop 拓扑独立 suite 载体） |
| 其余（session/memory/web/game/desktop_flow） | 零变化（回归面） |

## 5. 义务与验收锚点

1. 重构后 `guitar run projects/game/testplan/system_test.yaml` 实际执行（两 suite 各自部署→测试→清理闭环）**全部用例通过**（constitution 原则 VI；SC-001/007 的回归面载体）。
2. 无被否决合并形态残留：`rg 'fake-desktop-won|fake-desktop-drop'` 全仓零命中（YAML/注释/README 全查）。
3. helper 顺带优化（**非必须**）：`agent_v2_helpers_test.go`（1032 行）可按模块拆分为 conversation/preset/flow helper 文件——仅在自然触碰时做，不单列 phase。
4. suite description 更新：两 suite 各自按模块职能/关注点重述（不按 spec 场景编号），不残留"双 fake-desktop 实例"表述与已不存在的 suite 名引用。
