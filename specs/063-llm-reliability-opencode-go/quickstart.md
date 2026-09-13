# Quickstart: LLM 请求发送可靠性修复与 opencode-go 模型接入——验证指南

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **契约**: [contracts/](contracts/)

本文档是端到端验证指南：证明 feature 按 [spec.md](spec.md) 的 Success Criteria 工作。实现细节见 tasks.md（`/speckit.tasks` 产出）。

## 前置

- 仓库构建/测试入口为 bazel：`bazel build //...` / `bazel test //...`（`AGENTS.md`）。
- 大型测试经 testplan skill 执行（`tools/test/guitar`，`style/large_test.md`）：`guitar run <plan.yaml>`，完成部署→测试→清理闭环。**宪章 VI：构建检查不构成大型测试验收，必须实际执行且全部用例通过。**
- Go 大型测试基础拓扑：`projects/game/testplan/system_test.yaml`（fake-llm + fake-desktop + agent-v2-test + web/gateway/proxy/memory）。

## 1. 单测（快速反馈，随各 task 执行）

```bash
# llm-glm 失败分类/看护/空补全/清理
bazel test //common/js/dsh-plugins/llm-glm:lib_test

# opencode-go 新插件（序列化/wire/分类）
bazel test //common/js/dsh-plugins/llm-opencode-go:lib_test

# 编排器 turn 结果观察与成员保持
bazel test //common/js/dsh-plugins/saolei-loop:lib_test

# agent_v2 选择面/联合目录/bootstrap
bazel test //projects/game/agent_v2:lib_test

# web 选择面板
bazel test //projects/game/web/frontend:all

# fake-llm 注入设施
bazel test //projects/game/fake-llm/service:all
```

关键单测断言点（对应契约测试义务）：失败码映射全分支（[llm-failure-taxonomy.md §4](contracts/llm-failure-taxonomy.md)）、复合标识解析与联合目录（[model-selection.md §6](contracts/model-selection.md)）、`failCurrentTurn` 保持语义（[orchestrator-turn-outcome.md §5](contracts/orchestrator-turn-outcome.md)）、transient 计数（[fake-llm-fault-injection.md §4](contracts/fake-llm-fault-injection.md)）。

## 2. 大型测试验收（宪章 VI，SC-001..005）

统一入口（含全部新用例的 plan）：

```bash
guitar run projects/game/testplan/system_test.yaml
```

新用例（部署 env 与 fixture 细节见 [fake-llm-fault-injection.md §5](contracts/fake-llm-fault-injection.md)）：

| 场景 | 验证内容 | 通过标准 |
|---|---|---|
| SC-001 瞬时恢复 | planner 触发词命中 `transient:{times:1, http_status:503}` | turn 帧 COMPLETED、无 ERROR、session log `llm/retry` 恰 1 条 |
| SC-002 失败保持 | `transient:{times:6, http_status:500}`（恰耗尽默认重试预算） | turn_end ERROR；GetTeam activation=planner；再 Send 由 planner 应答且（times 耗尽后）正常完成规划并切 player |
| SC-004a 停滞看护 | 独立 `game-stall` suite：`deploy_agent_v2_stall.yaml`（`GLM_STREAM_IDLE_TIMEOUT_MS=2000`）+ responses `stall` 模板 | 超时类失败呈现（经重试有界收敛），用例在整体超时内完成 |
| SC-005 非瞬时零重试 | 配额：`http_status:429` + 体文案 `insufficient quota`；认证：`http_status:401` | 零 `llm/retry` 事件；恰一次 ERROR turn |
| SC-003 opencode-go | `OPENCODE_LLM_TARGET` → fake-llm chat 端点；合成 `OPENCODE_API_KEY`（非真实 secret） | ListModels 含两 provider 复合标识（同名 `glm-5.3` 消歧）；`opencode-go/<model>` 成员完成含工具调用的多轮会话；合成 token 值零出现 |

**SC-004b 结构化日志**：SC-002 场景运行期间经 signoz 查询 `game/agent-v2`（测试 env）error 日志：存在 `{session, phase, member, code, error}` 字段的失败记录，`error` 文本无 token 值。

## 3. 手工冒烟（可选，真实端点）

1. 配置 `OPENCODE_API_KEY`（opencode 订阅 key）与默认 endpoint（不设 `OPENCODE_BASE_URL`）。
2. 启动 agent_v2，web 打开 Team 设置面板：模型下拉出现 `glm-responses/*` 与 `opencode-go/*` 复合条目。
3. 选择 `opencode-go/glm-5.3`（或 kimi-k3），发送消息：正常推理/文本流、工具调用往返、usage 呈现。
4. 故意不设 key 重启：请求照常发出，真实端点 401 → 可见 AUTH 失败（不重试风暴）。

## 4. 预期结果总览（与 SC 对应）

- 单次瞬时请求发起失败 100% 经自动重试无感恢复（SC-001）。
- planner 首请求持续失败时，100% 后续 Send 由 planner 应答、激活成员恒为 planner（SC-002）。
- opencode-go 模型可完成完整会话，token 零泄漏（SC-003）。
- 失败 turn 100% 伴随含稳定码的 error 日志（SC-004b）；停滞在配置窗口内 100% 检出（SC-004a）。
- 配额/认证类失败零重试风暴（SC-005）。
