# Quickstart: Game Agent v2 — dsh 迁移 Step 2 验证指引

**Feature**: [spec.md](spec.md) | **契约**: [contracts/agent-api.md](contracts/agent-api.md) · [contracts/desktop-bridge.md](contracts/desktop-bridge.md) · [contracts/saolei-plugins.md](contracts/saolei-plugins.md) · [contracts/web-frontend.md](contracts/web-frontend.md) | **数据模型**: [data-model.md](data-model.md)

本文是**验证/运行指引**（证明 feature 端到端可用），不含实现细节（实现见 tasks.md）。三个层级：单测/编译 → 大型测试（验收）→ 手工冒烟。

## 0. 前置

- bazel 环境 + `bazel run //:deploy_install`、`bazel run //:guitar_install`（testplan skill，`.opencode/skills/testplan/SKILL.md`）
- 大型测试零外部依赖：fake LLM（`projects/game/fake-llm/` `/v1/responses`）+ fake desktop（`projects/game/fake-desktop/`）替代真实端点
- 真实 desktop 冒烟需 Windows + wails 构建链（可选，US3 冒烟记录）

## 1. 编译 + 单测（每次变更，constitution 原则 IV）

```bash
bazel build //...
bazel test //common/js/dsh-plugins/... //projects/game/... 
```

覆盖（各包 vitest + Go test，随实现交付）：saolei-loop 驱动器与 GameRuntime 契约、desktop-bridge 连接语义、saolei 工具面、glm listModels/工具序列化、agent-v2 物化/preset/事件映射、gateway/proxy 路由、web 组件（侧栏四项/preset/物化/tool_result）、desktop 保留面。清单见 [contracts/saolei-plugins.md](contracts/saolei-plugins.md) §7、[contracts/web-frontend.md](contracts/web-frontend.md) §6。

## 2. 大型测试（验收门禁，constitution 原则 VI —— 必须实际执行）

```bash
# 全量（部署→测试→清理闭环；全部用例必须 green，零 failed/flaky）
guitar run projects/game/testplan/system_test.yaml

# 单套件定位（例）
guitar run projects/game/testplan/system_test.yaml --suite agent-v2-game
```

套件与断言要点（FR-020 / SC-001/002，拓扑 = `deploy_agent_v2.yaml`：mongo + session + memory + fake-llm + fake-desktop + proxy + agent-v2-test + web + gateway）：

| 套件 | 证明 |
|---|---|
| `agent-v2-conversation` | 049 零回归：流式 text/think、多轮、排队、历史回填（经更名后 ListAgentMessages）；工具调用块流式可见（真实工具链路首次兑现） |
| `agent-v2-preset` | US2：preset CRUD/持久化（重启 agent-v2 后 preset 仍在）/物化与模型选择/Update 刷新（记忆清空）/未物化 Send 拒绝/未知模型拒绝/空 prompt 回退 base |
| `agent-v2-game` | US1（won 拓扑）：fake-llm 模板驱动"开始一局扫雷"→ saolei_init/operate/remain 工具链 → fake-desktop 执行 + 棋盘回传 → 文本棋盘契约（`board size`/`game status:` 行）→ 终局（won）与终局后拒绝；desktop 缺席分支；多 session 隔离；**两流独立性** |
| `agent-v2-game-disconnect` | US1 断连分支（drop 拓扑，deploy = `deploy_agent_v2_drop.yaml`：fake-desktop 以 progressive + disconnect-after-ops env 部署）：mid-game 断连三局序列——断连不可见 → 断连后 init FAILED 回合存活 → 重连后完整重播（US1 场景 5/6） |
| `desktop-flow` | US3：fake-desktop 连接/探测/接管/操作回执（真 desktop 冒烟可作补充记录） |
| `session` / `memory` | gateway `/api/v1` 保留面回归 |

失败排查：signoz skill 查 tracing/log（SKILL.md 指引）；fake-llm/fake-desktop 行为由模板/配置驱动（`projects/game/fake-llm/service/testdata/agent_v2*.yaml`）。

## 3. 手工冒烟（部署环境，可选补充）

1. **部署**：`guitar`/deploy 工具按 `projects/game/deploy.yaml` 起 game 环境（agent-v2 含 glm secret；desktop 本机运行）。
2. **web**（`https://game.liukexin.com/`）：
   - 侧栏四项交互目检（计数单行/图标按钮/`···` 删除/长名虚化+悬停滚动）；
   - Presets 视图新建 preset（如 `p1`，提示词"你是扫雷玩家"）；
   - 新建 session → 对话页引导物化（选 `p1` + 模型）→ 发送消息（流式回复）。
3. **desktop**：配置 GatewayURL → 只读选择该 session → 连接（探测通过）→ 绑定窗口。
4. **游戏闭环（US1 手工版）**：web 发"开始一局扫雷"→ web 可见工具调用块（棋盘文本）→ desktop 执行按键/点击并回传截图 → 直至终局；过程中 desktop 不展示任何对话内容。
5. **验证 v1 处置（SC-005）**：`/api/v1` team/prompt 路由与 WS connect 返回 404/不存在；memory 路由仍可用；部署清单无 prompt/v1 agent 服务。

## 4. 预期结果对照

- 全部套件 green（§2）＝ SC-001；049 用例零回归 ＝ SC-002；组件用例 ＝ SC-003；desktop 能力清单 ＝ SC-004；v1 下线 ＝ SC-005；交付物 grep 无明文 token ＝ SC-006。
