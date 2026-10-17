# Quickstart: 078 saolei JS hook 验证指南

**Spec**: [spec.md](spec.md) | **契约**: [contracts/board-change-event.md](contracts/board-change-event.md)、[contracts/saolei-hooks-plugin.md](contracts/saolei-hooks-plugin.md)、[contracts/hook-announcement.md](contracts/hook-announcement.md) | **数据模型**: [data-model.md](data-model.md) | **决策**: [research.md](research.md)

本 feature 验证 = **新机制单测（四包）+ 既有行为回归 + 真实端到端大型测试（077 推迟项，宪法 VI 实际执行闭环）**。无新对外 API；web 前端零改动。

## 1. 前置条件

- 仓库按 `AGENTS.md` 环境可用（bazel 入口、pnpm workspace、testplan skill/guitar）。
- 阅读顺序建议：spec（含 Clarifications）→ research.md（D1–D9 裁定）→ 三份契约 → data-model.md。

## 2. 编译与单测（每次代码变更执行；宪法 IV）

```bash
bazel build //common/js/dsh-plugins/saolei/... //common/js/dsh-plugins/saolei-hooks/... //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/...
bazel test  //common/js/dsh-plugins/saolei/... //common/js/dsh-plugins/saolei-hooks/... //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/...
```

| 验证点 | 位置 | 预期（断言面见各契约"验收面"节） |
|---|---|---|
| 事件 seam 触发条件七项 | `@dominion/dsh-saolei` `runtime.test.ts` | 落账且 executed>0 且 playing → 恰一次、载荷同值；全 skip/终局/init/无落账 → 零回调；seam 缺省回归不变；回调抛错不影响 operate 返回值（[board-change-event.md](contracts/board-change-event.md) §5） |
| 工具校验/注册表/沙箱/处理器 | `@dominion/dsh-saolei-hooks`（新） | 校验八层、describe 八要素 + 零 section、沙箱六项（宿主 API 白名单/死循环在 `EXECUTION_TIMEOUT_MS` 内中断（timeout 覆盖函数体）/输出截断/跨执行无状态/`board` 快照隔离/序列化）、同名覆盖保序、注册序执行 + 空输出零播报（[saolei-hooks-plugin.md](contracts/saolei-hooks-plugin.md) §6） |
| 播报文本格式 | saolei-hooks 单测 | 格式表逐行；announce seam mock 断言（[hook-announcement.md](contracts/hook-announcement.md) §5） |
| 装配与可选挂载 | saolei-loop 单测 | hooks 行在 → 三面装配（tools/sections（空）/onBoardChange/announce 绑定）；planner 注册的 sections 集不含 hook 项且 saolei-tools 面逐字不变；依赖边静态断言（无 saolei-tools/saolei-loop/team/announcer 边）；行缺 → 既有用例回归不变；行在面缺 → 装载失败 |
| 组合清单 | agent_v2 装载测试 | cordis.yml 含 hooks 行后组合装载成功 |

## 3. 大型测试（功能验收；宪法 VI——实际执行部署→测试→清理闭环）

新增 hook 端到端场景（fake-llm keyword 模板 + 测试自带 desktop half，`projects/game/testplan/`）：

```text
场景脚本（fake-llm 模板驱动；非终局链路——终局 operate 不触发 hook）:
  1. planner 请求含关键词（如"固化"）→ tool_call 模板: saolei_hook {action:"set", name:"safe-cells", script:"...扫描 board 返回安全格列表..."}
  2. player 请求含关键词 → tool_call 模板: saolei_operate {operations:[...有效操作...]}（棋盘仍 playing）
     · 非终局局面由**测试自带 desktop half** 提供（openDesktopStream + serveTeamFlowScript；won 拓扑的 fake-desktop
       执行器跑 `won` 终局场景、开局即终局，不能承载本场景）。配对固定为 16×16：
       initBoards = saoleiBoardInitPNG（saolei_1，16×16 全 INITIAL）→ stepBoards = saolei_3 的 16×16 部分翻开板
       （同尺寸、已翻开不回退、仍 `playing`），使该次 operate `executed > 0` 且条目 `status: "playing"`；
       9×9 家族不可用——其唯一既有后继 saolei_10 是终局 won 板，按 FR-001 不触发。
       saolei_3 取自 projects/game/fake-desktop/service/testdata/saolei_3.png（复制进 projects/game/testplan/testdata/）
  3. desktop half 派发成功（SUCCEEDED + 截图）→ 识别回棋盘 → 事件触发 → hook 执行 → announce
断言:
  a. web ListTeamMessages 与实时流出现 member=announcer 的 "[hook safe-cells] ..." 消息
  b. 快路下一 step 携带：player 下一 step 的驱动以前述播报文本为锚定关键词命中模板，且该模板驱动的后续行为是唯一成功路径；fake-llm 无请求体读取面（无匹配请求回退随机文本模板），故断言语义为"锚定模板命中且恰一次"，不得以"模板未命中"为判据
  c. planner 后续驱动经其锚定模板命中同一播报（drain 可见）
  d. player 执行全 skip 批量 / 终局批量 → 零 hook 播报
  e. 跨成员发言级顺序一致（共享消息相对顺序 = 生产序）
  f. player 的 saolei_operate 结果文本与基线逐字相等（不含 "[hook" 前缀——hook 分析不经工具结果回流，FR-010）
```

执行（testplan skill，含既有套件全量回归）：

```bash
guitar run projects/game/testplan/system_test.yaml
```

**通过标准**：全部用例通过（含既有 game-system/game-stall/game-memory-down/game-disconnect 与新增 hook 场景；任何 failed/flaky 即验收未通过，修复后重跑至全绿）。

## 4. 手工冒烟（可选，部署环境）

1. 部署 agent_v2 + web，创建 team；
2. 以 planner 身份对话"把这个策略固化成 hook：……"→ 确认 `saolei_hook` set 成功返回；
3. 观察 player 对局中每次有效 operate 后 web 即时出现 `[hook ...]` 播报；
4. `saolei_hook {action:"list"}` 确认注册项；UpdateTeam 刷新后 list 为空（无持久化）。
