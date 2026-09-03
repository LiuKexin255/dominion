# Contract: testplan 重构（deploy 合并 + suite 归并）

**Feature**: 用户指令②（2026-09-03）+ FR-024 | **Research**: [research.md](../research.md) D11 | **规范**: `style/large_test.md`

现状基线（调研实证）：`projects/game/testplan/system_test.yaml` 单计划 7 suite / 2 deploy——6 个 suite 复用**逐服务完全相同**的 `deploy_agent_v2.yaml` 却各自独立部署（7 次部署 + 7×60s settle，超 guitar 默认 10m 预算）；`deploy_agent_v2_drop.yaml` 与主 deploy 唯一差异是 fake-desktop 的 env。测试文件/helper 组织已合规（无反模式），仅编排需重构。

## 1. Deploy 合并

`deploy_agent_v2.yaml` 服务清单变更：

| 变更 | 内容 |
|---|---|
| +1 服务 | `fake-desktop-drop`（artifact 同 `//projects/game/fake-desktop/service.yaml`）：env `FAKE_DESKTOP_SESSION=desktop-e2e-drop`、`FAKE_DESKTOP_SCENARIO=progressive`、`FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS=3` |
| 改名 | 既有 fake-desktop 实例名改 `fake-desktop-won`（env 不变：session `desktop-e2e-won`、scenario won），实例名与服务发现名变更不消费（executor 主动外拨，无被寻址面） |
| 删除 | `deploy_agent_v2_drop.yaml` 整文件（拓扑并入） |

隔离依据：两执行器绑定**不同 session 资源名**，各自应答对应 session 的操作帧（bridge 按 session 键连接注册表），互不干扰（051 既有"多 session 隔离"用例的同一原理）。

## 2. Suite 归并

`system_test.yaml`：7 suite → **1 suite**（`game-system`）：

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
      - //projects/game/testplan:agent_v2_game_test     # 含 disconnect 用例（§3）
      - //projects/game/testplan:desktop_flow_test
```

- **部署次数 7→1**；cases 串行执行（`tools/test/guitar/README.md`：suites/cases 按 YAML 顺序串行）。
- 跨 case 隔离：各 case 使用唯一 session/preset 资源名前缀（既有 helper 已具备唯一名构造的，统一走 helper）。
- 执行预算：测试净时长不变；省 6 次部署 + 6×60s settle（预期总时长显著回落；`--timeout` 参数按重构后实测校准并更新 README 预算说明）。

## 3. Binary 归并（target 8→7）

- `agent_v2_game_disconnect_test` 的用例并入 `agent_v2_game_test`（同文件新增测试函数；用例分别绑定 `desktop-e2e-won` / `desktop-e2e-drop` session）——拓扑分离的既有前提（051 directive §1.4：guitar 无函数筛选，binary 绑拓扑）随 deploy 合并消失。
- 删除 `agent_v2_game_disconnect_test` target 与文件（内容并入后无残留）。
- gazelle 默认名约束不变（`testplan_test` 保持默认名 target）。

## 4. 本 feature 新增/更新的测试用例（FR-024 大型测试面）

按模块归位（`style/large_test.md` "按模块拆分"）：

| 模块文件 | 新增/更新 |
|---|---|
| `agent_v2_conversation_test.go` | +step 字段分段断言（NDJSON 事件携带 step、回填每 step 一条）；+ERROR 回合已产出内容回填可见（注入失败）；+:cancel 全语义（终止/CANCELED 终态/排队落地/幂等/后续 Send 可用）；+GetAgent desktop_connected（连接/无连接） |
| `agent_v2_preset_test.go` | 模型目录断言更新（glm-5.3/glm-5.3-flash、默认值、未知 id 拒绝） |
| `agent_v2_game_test.go` | 既有 won/drop 链路零回归（并入 disconnect 用例后） |
| 其余（session/memory/web/desktop_flow） | 零变化（回归面） |

## 5. 义务与验收锚点

1. 重构后 `guitar run projects/game/testplan/system_test.yaml` 实际执行（部署→测试→清理闭环）**全部用例通过**（constitution 原则 VI；SC-001/007 的回归面载体）。
2. 无平行测试计划残留：`deploy_agent_v2_drop.yaml` 删除后无引用（YAML/gazelle/BUILD 全查）。
3. helper 顺带优化（**非必须**）：`agent_v2_helpers_test.go`（1032 行）可按模块拆分为 conversation/preset/flow helper 文件——仅在自然触碰时做，不单列 phase。
4. suite description 更新：移除对已删除 deploy 与 disconnect 独立 suite 的引用；描述按模块职能重述（不按 spec 场景编号）。
