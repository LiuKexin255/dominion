# Quickstart: Agent v2 team 模式优化 验证指南

> 端到端验证场景。大型测试统一经 testplan skill 执行（`guitar run <plan.yaml>`，部署→测试→清理闭环，constitution 原则 VI——仅构建通过不构成验收）；单测面由各实现任务随附（`bazel test`）。
> 主套件 `projects/game/testplan/system_test.yaml`（game-system / game-disconnect / game-memory-down 三 suite）承载下列场景的自动化断言；V6/V7 含手工观测面。

## 前置

- 全仓构建与单测通过：`bazel build //...`、`bazel test //...`
- 大型测试：加载 testplan skill 后 `guitar run projects/game/testplan/system_test.yaml`（won 拓扑 fake-llm + fake-desktop，零外部网络；三 suite 全量通过为验收标准）

## V1 部署配置收敛与产物位置变量

1. 部署后 agent-v2 容器 env 含 `DOMINION_ARTIFACT_DIR=/dominion/game/agent-v2`（平台注入；与 `tools/release/deploy/README.md` 保留清单一致）。
2. `projects/game/deploy.yaml` 与三份 testplan 拓扑无任何 preset 路径 env（代码检索断言零残留）；服务 boot 成功、preset 模板可列出与物化（模板根经变量派生）。
3. 本地单测面：`PRESET_TEMPLATES_ROOT` 显式覆盖仍可用；两变量皆缺时 boot fail-loud（错误含两个变量名）。

## V2 preset 唯一事实源化

1. 经 API 创建/编辑 preset → 断言仅 store 变更（部署环境无被维护副本目录；代码检索断言 copy-then-patch 维护路径移除）。
2. 重启 agent-v2（Pod 重建语义）→ 直接物化引用既有 preset 的 team 成功；成员 system prompt persona 与 store 记录一致（compose 派生幂等，无"副本重建"路径）。
3. 删除任意临时目录内容 → 不影响后续物化正确性（派生物可再生）。

## V3 常量库

1. `common/gopkg/constants` 与 `@dominion/common-js-constants` 存在且收录 12 个保留变量名（含新增）。
2. builder.go/agent_v2 指定消费点引用常量库（范围内字面量零残留，代码检索断言）；builder 注入行为回归（env 数量/顺序断言更新后通过）。

## V4 webUI 实时流修复（核心验收）

1. **用户输入实时可见**：物化 team → 发送首条消息 → Send 流出现 `member_view` 帧（planner 消费用户输入即达，先于/伴随其回合事件）；前端 planner 视角实时呈现该输入（无需等待回合结束）。
2. **工具结果即时更新**：player 游戏回合中每次 `tool_result` 帧到达 → 团队视图与 player 视角对应工具卡即时转为终态并显示结果文本（新增 store 单测以 059 真实帧序覆盖：blockStart 无 toolId → blockEnd 带 toolId → team_message 固化 → tool_result 三投影面 settle）。
3. **回归**：断开收敛（投影 + List 回填对齐）、并发流按锚去重、既有三 suite 全量通过。

## V5 提示词分层

1. planner 成员 system prompt 含 `saolei:game`（玩法 + 可用操作；内容与权威玩法一致、操作与三工具对齐）；player 含同一 section（同源同文）。
2. player 的 `saolei:guidance` 仅工具用法（无数字含义/级联/胜负判定等玩法陈述——关键词断言）；persona 无操作清单描述。
3. fake-llm 夹具关键词随分层更新后多局闭环全量通过（won 链路、终局复盘、续驱、排队/取消）。

## V6 广播净化（自动 + 手工）

1. 自动：成员间广播注入文本（fake-llm 驱动多局后经 ListMemberMessages 或夹具输入断言）——不含 think 内容；正文恰好出现一次（无头行复述）；工具单元为 `<{role}-tool-call>` 包裹的 `context:`/`tool:`/`args:`/`result:` 行，args/result 全文。
2. 手工：web 成员视角 relay 条目（`user: [sender]` 前缀 + 注入原文，XML 标签对保留）无头行、正文仅出现一次。

## V7 team 状态呈现（自动 + 手工）

1. 自动：物化后 `GET .../team` 响应含 `activeMember: "planner"`；多局循环期间随阶段流转（player 回合 = player、复盘 = planner）。
2. 手工：对话页工具条实时显示激活成员（回合切换无需刷新）；成员清单区每成员可点开 system prompt 全文（不打开设置面板）；设置面板内入口仍在。
