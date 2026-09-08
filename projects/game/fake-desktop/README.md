# game fake-desktop

fake-desktop 是 game 大型测试专用的**确定性 desktop 执行器**：对接收到的
FlowPart 操作以固定图集截图回执，替代真实 Windows desktop 进被测系统
（`specs/051-agent-v2-dsh-migration/research.md` D15，spec A9）。仅被
testplan 部署引用（`projects/game/testplan/deploy_agent_v2.yaml` /
`deploy_agent_v2_drop.yaml` 各部署一个实例，服务名同为 `fake-desktop`、
行为差异全由 env 驱动），不进生产部署。

## 进程形态

`cmd/main.go`：按配置拨号 gateway 的 `/api/v2` flow WebSocket（单一
session），执行收到的操作帧并回传截图与回执；`:8080` 提供健康探针端点。
连接管理（探测、接管）与操作派发语义见 `service/session.go` 与
`service/executor.go`。

## 确定性棋盘模型（图集映射）

确定性来自**固定图集映射**而非运行时渲染（research.md D15：图集优先）：
`Scenario` 是一局固定游戏——F2 新局回执一张 `InitPNG`，其后的单元格操作按
`ScenarioStep`（操作类型 → 截图）顺序消费；不匹配待定步骤类型的操作不改变
棋盘（幂等重读，同一局内重复当前图总是安全的）。

内嵌截图（`service/testdata/`）是权威识别 fixture
`projects/game/pkg/saolei-board/testdata`（golden 校验于 `golden.test.ts`）的
cmp 相同副本——大型测试中 agent 侧运行**真实识别器**，执行器回传的每张截图
必须是一局内可识别、状态单调前进的扫雷棋盘（尺寸 init 固定、已翻开不回退，
`projects/game/pkg/saolei-board/src/core/recognize.ts`）。场景族
（`service/scenarios.go`）：

| scenario | 图集序列 |
|---|---|
| `won`（默认） | `saolei_10`（9×9 胜局，counter 000；F2 回执即终局） |
| `lost` | `saolei_5`（16×16 败局，HIT_MINE/MINE） |
| `progressive` | `saolei_1`（16×16 全 INITIAL）→ click → `saolei_3`（部分翻开）→ flag → `saolei_4`（`saolei_3` 加旗） |

## 环境变量

由 testplan 部署注入（`cmd/main.go`）：

| 变量 | 含义 |
|---|---|
| `FAKE_DESKTOP_GATEWAY_URL` | gateway 基址；`http(s)://` 直用或 Dominion target（`dominion:///game/gateway:80`，经服务注册表解析） |
| `FAKE_DESKTOP_SESSION` | flow session 资源 id（必填） |
| `FAKE_DESKTOP_TEMPLATE` | flow 模板（默认 `saolei`） |
| `FAKE_DESKTOP_SCENARIO` | `won`（默认）/ `lost` / `progressive` |
| `FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS` | 故障注入：N 次回执后断开连接（drop 拓扑用） |
| `FAKE_DESKTOP_FAULT_OMIT_SCREENSHOT` | 故障注入：置 `1` 回执不带截图 |
| `FAKE_DESKTOP_FAULT_FAILED_STATUS` | 故障注入：置 `1` 对操作回执 FAILED |

## 大型测试中的用法

两个部署各含一个 `fake-desktop` 实例（服务名不改——guitar 为每个 suite
生成独立环境，两 deploy 的服务名空间互不可见），以 `FAKE_DESKTOP_SESSION`
绑定各自的 session 资源、以 `FAKE_DESKTOP_SCENARIO` 选择场景
（套件-拓扑对照见 `projects/game/testplan/README.md` §2）：

- won 拓扑（`deploy_agent_v2.yaml`，suite `game-system`）：
  `FAKE_DESKTOP_SESSION=desktop-e2e-won` + `won` 场景，服务游戏面的胜局
  链路（agent_v2_game_test）与 desktop-flow 面的执行器依赖。
- drop 拓扑（`deploy_agent_v2_drop.yaml`，suite `game-disconnect`）：
  `FAKE_DESKTOP_SESSION=desktop-e2e-drop` + `progressive` 场景 +
  `FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS=3`（init F2 + 两次单元格操作后
  断开），服务 agent_v2_game_disconnect_test 的三局断连恢复序列。
