# Quickstart: 验证指南（memory 拆分 + 终局折叠 + remain 澄清）

> Phase 1 输出。运行级验证场景；实现细节见 tasks.md（Phase 2 产出）。契约与数据模型：[contracts/dsh-plugins.md](contracts/dsh-plugins.md)、[contracts/web-ui.md](contracts/web-ui.md)、[contracts/saolei-plugins.md](contracts/saolei-plugins.md)、[data-model.md](data-model.md)。

## 前置

- 仓库根目录执行；bazel 可用（`bazel --version`）。
- 代码规范：`style/javascript.md`（全部涉改文件为 TS/YAML）。
- 大型测试需 guitar CLI（`tools/test/guitar`，testplan skill）。

## 场景 1：memory 拆分——组合面与行为零回归

```bash
# 1. 全量构建（新包进入闭包）
bazel build //...

# 2. 两个插件包单测（域核心迁移 + 工具行）
bazel test //common/js/dsh-plugins/memory-service:lib_test
bazel test //common/js/dsh-plugins/memory:lib_test

# 3. agent_v2 组合面断言（host 行/模板行/规则终值 + 物化回归）
bazel test //projects/game/agent_v2:lib_test
```

**预期**：全部通过；`projects/game/agent_v2/cordis.yml` host memory 行 = `@dominion/dsh-memory-service`、templateRules player.forbidden 仅 `['@dominion/dsh-memory']`、planner 模板行 = `@dominion/dsh-memory`（SC-001/SC-002）。

## 场景 2：终局回合折叠——组件级

```bash
bazel test //projects/game/web/frontend:lib_test
```

**预期**：新增用例通过——终局收束回合（末步 THINK|TEXT|TOOL、工具块 SUCCEEDED 带 result、无 interrupted）渲染 `turn-process-toggle`、标签计数 = 过程步数与过程内工具块数、末步（含工具卡片）可见；interrupted 回合与最终答案回合既有用例零回归（SC-003）。

## 场景 3：remain 语义——文本契约

```bash
bazel test //common/js/dsh-plugins/saolei-loop:lib_test
bazel test //common/js/dsh-plugins/saolei:lib_test
```

**预期**：remain 结果体断言含 legend 关键词（mines still unmarked / NOT the count of flags / Columns are x and rows are y）；既有前缀断言（`saolei_remain → computed\ngame status: playing`）零破坏；description/规则 section 措辞断言更新后通过（SC-004）。

## 场景 4：端到端回归（大型测试，验收门禁）

```bash
# testplan skill：guitar run（部署→测试→清理闭环；仓库唯一计划 system_test.yaml）
guitar run projects/game/testplan/system_test.yaml                      # 四个 suite 全量
guitar run projects/game/testplan/system_test.yaml --suite game-system  # game 主线：4-turn 链/终局收束/复盘 + conversation 面（cancel/backfill/失败中断）
guitar run projects/game/testplan/system_test.yaml --suite game-memory-down   # memory 服务缺失的物化 fail-loud
# 其余 suite（game-disconnect 断开收敛 / game-stall 流停滞看门狗）随全量执行
```

**预期**：全部用例通过（宪法 VI：全量绿才算验收；仅构建不构成验收）。memory 拆分不改服务目标与编排，预期零影响（SC-005）。webUI 侧终局折叠的断言面由场景 2 组件测试等效锚定（SC-005 等价路径），不进入 guitar 用例。

## 场景 5：手工抽查（可选，真实环境）

1. 部署 agent_v2 后创建 team 会话，玩一局至终局。
2. WebUI 团队视图：player 终局回合折叠（"思考过程（n 步骤 · m 次工具调用）"开关 + 末步终局棋盘工具卡片可见）；planner 复盘回合折叠形态同型。
3. 展开过程，找 `saolei_remain` 工具卡片：结果体网格前有 legend 行，数值语义自明。
